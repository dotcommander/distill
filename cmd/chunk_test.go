package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/manifest"
	"github.com/dotcommander/reliquary/chunking"

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
	outDir := filepath.Join(t.TempDir(), "chunks")
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
		outDir:    outDir,
	}
	err = runChunk(&runContext{ctx: context.Background(), in: r, out: io.Discard, errOut: io.Discard}, []string{"-"}, &f)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	require.NoError(t, err)
	var m manifest.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "stdin", m.Source)
	assert.Equal(t, 1, m.TotalChunks)
	assert.True(t, m.TokenCountsAvailable)
	assert.Equal(t, "cl100k_base", m.Tokenizer)
	require.NotNil(t, m.TotalTokens)
	assert.Positive(t, *m.TotalTokens)

	chunk, err := os.ReadFile(filepath.Join(outDir, "001.md"))
	require.NoError(t, err)
	assert.Contains(t, string(chunk), "hello from stdin")
}

func TestRunChunkRejectsExistingOutputPathWithoutMutation(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	outDir := filepath.Join(parent, "chunks")
	require.NoError(t, os.Mkdir(outDir, 0o750))
	stalePath := filepath.Join(outDir, "stale.md")
	const stale = "keep this exact content\n"
	require.NoError(t, os.WriteFile(stalePath, []byte(stale), 0o600))

	f := chunkFlags{mode: "headings", maxTokens: 1000, outDir: outDir}
	err := runChunk(
		&runContext{ctx: context.Background(), in: strings.NewReader("# Title\n\ncontent\n"), out: io.Discard, errOut: io.Discard},
		[]string{"-"},
		&f,
	)
	require.ErrorContains(t, err, `already exists; choose a new --out-dir`)
	data, readErr := os.ReadFile(stalePath)
	require.NoError(t, readErr)
	assert.Equal(t, stale, string(data))
	entries, readErr := os.ReadDir(outDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	assert.Equal(t, "stale.md", entries[0].Name())
	assert.Empty(t, stagingDirs(t, parent, outDir))
}

func TestPrepareChunkOutputRejectsExistingTargets(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	filePath := filepath.Join(parent, "file")
	require.NoError(t, os.WriteFile(filePath, []byte("keep"), 0o600))
	symlinkPath := filepath.Join(parent, "link")
	require.NoError(t, os.Symlink(filePath, symlinkPath))
	dirPath := filepath.Join(parent, "empty-dir")
	require.NoError(t, os.Mkdir(dirPath, 0o750))

	for _, target := range []string{filePath, symlinkPath, dirPath} {
		_, err := prepareChunkOutput(target)
		require.ErrorContains(t, err, "already exists; choose a new --out-dir")
	}
}

func TestRunChunkPublishesCompleteGeneration(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	outDir := filepath.Join(parent, "chunks")
	f := chunkFlags{mode: "headings", maxTokens: 1000, outDir: outDir}
	err := runChunk(
		&runContext{ctx: context.Background(), in: strings.NewReader("# Title\n\ncontent\n"), out: io.Discard, errOut: io.Discard},
		[]string{"-"},
		&f,
	)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(outDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 2)
	assert.Equal(t, "001.md", entries[0].Name())
	assert.Equal(t, "manifest.json", entries[1].Name())
	m, readErr := manifest.ReadManifest(outDir)
	require.NoError(t, readErr)
	require.Equal(t, 1, m.TotalChunks)
	info, statErr := os.Stat(outDir)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())
	assert.Empty(t, stagingDirs(t, parent, outDir))
}

func TestChunkOutputAbortRemovesStagingAndLeavesTargetAbsent(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	outDir := filepath.Join(parent, "chunks")
	output, err := prepareChunkOutput(outDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(output.workDir, "partial.md"), []byte("partial"), 0o600))

	output.abort()

	_, err = os.Stat(outDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, stagingDirs(t, parent, outDir))
}

func TestChunkOutputPublishFailureLeavesConcurrentTargetUntouched(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	outDir := filepath.Join(parent, "chunks")
	output, err := prepareChunkOutput(outDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(output.workDir, "partial.md"), []byte("partial"), 0o600))
	require.NoError(t, os.Mkdir(outDir, 0o750))
	concurrentPath := filepath.Join(outDir, "concurrent.md")
	require.NoError(t, os.WriteFile(concurrentPath, []byte("keep"), 0o600))

	err = output.publish()
	require.ErrorContains(t, err, "already exists")
	output.abort()

	data, err := os.ReadFile(concurrentPath)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
	assert.Empty(t, stagingDirs(t, parent, outDir))
}

func TestChunkOutputPublishInterpositionDoesNotDeleteReplacement(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	outDir := filepath.Join(parent, "chunks")
	output, err := prepareChunkOutput(outDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(output.workDir, "complete.md"), []byte("complete"), 0o600))
	output.beforeRename = func() {
		require.NoError(t, os.Mkdir(outDir, 0o710))
	}

	err = output.publish()
	require.ErrorContains(t, err, "already exists")
	output.abort()

	info, err := os.Stat(outDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o710), info.Mode().Perm())
	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Empty(t, stagingDirs(t, parent, outDir))
}

func stagingDirs(t *testing.T, parent, outDir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(parent, "."+filepath.Base(outDir)+".distill-staging-*"))
	require.NoError(t, err)
	return matches
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
