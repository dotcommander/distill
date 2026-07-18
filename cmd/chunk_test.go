package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/distill/internal/manifest"
	"github.com/dotcommander/reliquary/pipeline/chunking"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunChunkRejectsOversizedFile uses readCappedInput directly against a
// real file to prove the chunk command's input gate fires. We do not exercise
// runChunk end-to-end here (it depends on a tokenizer + CLI plumbing); the
// load-bearing invariant for this task is that an oversized file produces
// errCountInputTooLarge from the cap, not an unbounded read.
func TestRunChunkRejectsOversizedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	// Use a tiny synthetic cap inside the test so we don't have to write
	// 100 MiB to disk. The production cap (countMaxInputBytes) is exercised
	// by TestCountMaxInputBytesConstant; this test asserts the *behavior*.
	const testCap = 64
	payload := bytes.Repeat([]byte("x"), testCap+1)
	require.NoError(t, os.WriteFile(path, payload, 0o600))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = readCappedInput(f, testCap)
	require.ErrorIs(t, err, errCountInputTooLarge)
	assert.Contains(t, err.Error(), "input exceeds maximum size",
		"error message should mention the cap")
}

func TestRunChunkReadsStdin(t *testing.T) {
	dir := t.TempDir()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString("## Title\n\nhello from stdin\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})

	f := chunkFlags{
		mode:      "headings",
		maxTokens: 1000,
		outDir:    dir,
	}
	err = runChunk(&runContext{ctx: context.Background(), in: r, out: io.Discard, errOut: io.Discard}, []string{"-"}, &f)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m manifest.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "stdin", m.Source)
	assert.Equal(t, 1, m.TotalChunks)
	assert.True(t, m.TokenCountsAvailable)
	assert.Equal(t, "cl100k_base", m.Tokenizer)
	require.NotNil(t, m.TotalTokens)
	assert.Positive(t, *m.TotalTokens)

	chunk, err := os.ReadFile(filepath.Join(dir, "001.md"))
	require.NoError(t, err)
	assert.Contains(t, string(chunk), "hello from stdin")
}

func TestWriteChunksMarksUnavailableTokenCounts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chunks := []chunking.Chunk{{ID: 0, Text: "nonempty local model chunk"}}

	m, err := writeChunks(dir, "source.md", chunks[0].Text, chunks, nil, "headings")
	require.NoError(t, err)
	require.False(t, m.TokenCountsAvailable)
	require.Empty(t, m.Tokenizer)
	require.Nil(t, m.TotalTokens)
	require.Len(t, m.Chunks, 1)
	require.Nil(t, m.Chunks[0].Tokens)
}
