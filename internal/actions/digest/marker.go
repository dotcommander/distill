package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/distill/internal/fsutil"
)

// markerName is the artifact-binding marker written at the root of
// ArtifactDir. Its presence asserts that every pipeline-generated artifact in
// the directory (responses/, chunks/, compiled facts) was produced from the
// source identified by the marker — artifact reuse is allowed only under a
// matching marker.
const markerName = "source.json"

// markerVersion is bumped if the marker layout or binding semantics change;
// struct equality then fails and old artifacts regenerate.
const markerVersion = 1

// runMarker binds an artifact directory to the exact inputs that determine
// chunk identity: the post-clean source bytes and the chunk geometry. Chunking
// is deterministic, so a matching marker guarantees index-based artifact reuse
// refers to identical chunks.
type runMarker struct {
	Version      int    `json:"version"`
	SourceSHA256 string `json:"source_sha256"`
	ChunkSize    int    `json:"chunk_size"`
	MaxTokens    int    `json:"max_tokens"`
}

func markerFor(source string, chunkSize, maxTokens int) runMarker {
	sum := sha256.Sum256([]byte(source))
	return runMarker{
		Version:      markerVersion,
		SourceSHA256: hex.EncodeToString(sum[:]),
		ChunkSize:    chunkSize,
		MaxTokens:    maxTokens,
	}
}

// readRunMarker returns the marker stored in dir; ok is false when the marker
// is absent or unparsable.
func readRunMarker(dir string) (m runMarker, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil {
		return runMarker{}, false
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return runMarker{}, false
	}
	return m, true
}

func writeRunMarker(dir string, m runMarker) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("digest: encoding artifact marker: %w", err)
	}
	return fsutil.WriteFile(filepath.Join(dir, markerName), data, 0o644)
}

// ArtifactsMatchSource reports whether artifactDir carries a marker binding
// its artifacts to (source, chunkSize, maxTokens). Read-only; used by CLI
// dry-run and call planning to decide whether resume reuse will count.
func ArtifactsMatchSource(artifactDir, source string, chunkSize, maxTokens int) bool {
	if artifactDir == "" {
		return false
	}
	got, ok := readRunMarker(artifactDir)
	return ok && got == markerFor(source, chunkSize, maxTokens)
}

// pathWithin reports whether path lies inside dir (or equals it) after both
// are made absolute and cleaned. Empty arguments report false.
func pathWithin(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dirAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ensureArtifactBinding verifies that ArtifactDir's artifacts were produced
// from source. On a missing or mismatched marker it removes the
// pipeline-generated artifacts (responses/, chunks/, facts.fused.md, and the
// compiled facts when they live inside ArtifactDir and are not explicitly
// reused), then stamps the current marker — the marker may only ever exist
// alongside artifacts written under it, so invalidation is delete-then-stamp.
// reuseOK reports whether artifact reuse is safe this run. A deletion or
// marker-write failure is a hard error: proceeding would let a future resume
// pick up stale artifacts under a fresh marker.
func ensureArtifactBinding(ctx context.Context, opts Options, source string) (reuseOK bool, err error) {
	if opts.ArtifactDir == "" {
		return false, nil
	}
	want := markerFor(source, opts.ChunkSize, opts.MaxTokens)
	got, ok := readRunMarker(opts.ArtifactDir)
	if ok && got == want {
		return true, nil
	}
	reason := "missing"
	if ok {
		reason = "mismatched"
	}
	slog.WarnContext(ctx, "digest: artifact dir not bound to current source; invalidating artifacts", "dir", opts.ArtifactDir, "marker", reason)
	for _, sub := range []string{"responses", "chunks"} {
		if rerr := os.RemoveAll(filepath.Join(opts.ArtifactDir, sub)); rerr != nil {
			return false, fmt.Errorf("digest: invalidating artifacts %s: %w", sub, rerr)
		}
	}
	fused := filepath.Join(opts.ArtifactDir, "facts.fused.md")
	if rerr := os.Remove(fused); rerr != nil && !os.IsNotExist(rerr) {
		return false, fmt.Errorf("digest: invalidating artifact %s: %w", fused, rerr)
	}
	if !opts.ReuseFacts && pathWithin(opts.ArtifactDir, opts.FactsPath) {
		if rerr := os.Remove(opts.FactsPath); rerr != nil && !os.IsNotExist(rerr) {
			return false, fmt.Errorf("digest: invalidating compiled facts %s: %w", opts.FactsPath, rerr)
		}
	}
	if werr := writeRunMarker(opts.ArtifactDir, want); werr != nil {
		return false, werr
	}
	return false, nil
}
