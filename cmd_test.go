package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/manifest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test that builds the binary in -short mode")
	}
	bin := filepath.Join(t.TempDir(), "distill")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "build failed: %s", out)
	return bin
}

// writeTestFile creates a temp file with content and returns its path.
func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	testFile := filepath.Join(t.TempDir(), "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644), "writing test file")
	return testFile
}

// runChunkCommand executes the chunk command and parses the manifest output.
// Returns the parsed manifest.
func runChunkCommand(t *testing.T, bin, testFile string, extraArgs ...string) *manifest.Manifest {
	t.Helper()
	args := append([]string{"chunk", testFile}, extraArgs...)
	cmd := exec.Command(bin, args...)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		require.NoErrorf(t, err, "chunk failed\nstderr: %s", stderr)
	}
	var m manifest.Manifest
	require.NoErrorf(t, json.Unmarshal(out, &m), "invalid manifest JSON\nraw: %s", out)
	return &m
}

func TestCountStdin(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)

	cmd := exec.Command(bin, "count", "-")
	cmd.Stdin = strings.NewReader("Hello, world! This is a test.")
	out, err := cmd.Output()
	require.NoError(t, err, "count failed")

	var result struct {
		Tokens int `json:"tokens"`
		Chars  int `json:"chars"`
		Lines  int `json:"lines"`
	}
	require.NoErrorf(t, json.Unmarshal(out, &result), "invalid JSON output\nraw: %s", out)

	assert.Positive(t, result.Tokens, "expected tokens > 0")
	assert.Equal(t, 29, result.Chars)
	assert.Equal(t, 1, result.Lines)
}

func TestChunkCramit(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	outDir := t.TempDir()

	content := strings.Repeat("This is a test sentence. ", 100)
	testFile := writeTestFile(t, content)

	m := runChunkCommand(t, bin, testFile, "--mode", "cramit", "--max-tokens", "100", "--overlap", "0", "--out-dir", outDir)

	assert.NotZero(t, m.TotalChunks, "expected at least one chunk")
	assert.Equal(t, "cramit", m.Mode)

	for _, c := range m.Chunks {
		path := filepath.Join(outDir, c.File)
		assert.FileExists(t, path, "chunk file does not exist")
	}

	manifestPath := filepath.Join(outDir, "manifest.json")
	assert.FileExists(t, manifestPath, "manifest.json does not exist")
}

func TestChunkHeadings(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	outDir := t.TempDir()

	content := "# Section One\n\nThis is the first section with some content.\nIt has multiple lines of text here.\n\n# Section Two\n\nThis is the second section.\nMore content follows here.\n\n# Section Three\n\nThe third and final section.\n"
	testFile := writeTestFile(t, content)

	m := runChunkCommand(t, bin, testFile, "--mode", "headings", "--max-tokens", "4000", "--overlap", "0", "--out-dir", outDir)

	assert.GreaterOrEqual(t, m.TotalChunks, 3, "expected at least 3 chunks for 3 headings")
	assert.Equal(t, "headings", m.Mode)

	firstChunk, err := os.ReadFile(filepath.Join(outDir, "001.md"))
	require.NoError(t, err, "reading first chunk")
	assert.Contains(t, string(firstChunk), "Section One", "first chunk should contain 'Section One'")
}

func TestSemanticRejectsCustomRemoteEmbeddingEndpoint(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	testFile := writeTestFile(t, "test content")

	cmd := exec.Command(bin, "chunk", testFile, "--mode", "semantic")
	cmd.Env = append(os.Environ(), "DISTILL_EMBEDDING_ENDPOINT=https://api.example.com/v1")
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "expected error for custom remote semantic endpoint")

	assert.Contains(t, string(out), "custom remote embedding endpoints are disabled")
}

func TestDigestRejectsTinyChunkSize(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	testFile := writeTestFile(t, "# Title\n\nbody content here")

	// --chunk-size below the 1000 minimum is rejected before any config load or
	// model call, so this stays offline and deterministic.
	cmd := exec.Command(bin, "digest", testFile, "--chunk-size", "500")
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "expected error for chunk-size below the minimum")

	assert.Contains(t, string(out), "chunk-size", "error should mention chunk-size")
}
