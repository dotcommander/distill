package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureSlog installs a buffer-backed JSON handler as the process default
// logger for the duration of the test and returns the buffer. It mutates
// global slog state, so callers must NOT run in parallel.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// decodeLogLines parses newline-delimited JSON slog records from buf.
func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var recs []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		recs = append(recs, m)
	}
	return recs
}

// TestRunCountEmitsStructuredLogs verifies runCount logs entry and exit
// records with the documented attrs. Not parallel: captureSlog mutates the
// global default logger.
func TestRunCountEmitsStructuredLogs(t *testing.T) {
	buf := captureSlog(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	require.NoError(t, os.WriteFile(path, []byte("hello world\nsecond line\n"), 0o600))

	// Discard stdout JSON; we are asserting on logs, not the result payload.
	require.NoError(t, execute(context.Background(), []string{"count", "--format", "json", path}, strings.NewReader(""), new(bytes.Buffer), new(bytes.Buffer)))

	recs := decodeLogLines(t, buf)
	require.GreaterOrEqual(t, len(recs), 2, "expected at least entry + exit log records")

	var sawStart, sawDone bool
	for _, r := range recs {
		switch r["msg"] {
		case "count start":
			sawStart = true
			assert.Equal(t, path, r["file"])
		case "count done":
			sawDone = true
			assert.Equal(t, path, r["file"])
			assert.Contains(t, r, "bytes")
			assert.Contains(t, r, "tokens")
			assert.Contains(t, r, "lines")
			assert.Contains(t, r, "duration")
		}
	}
	assert.True(t, sawStart, "missing 'count start' log record")
	assert.True(t, sawDone, "missing 'count done' log record")
}
