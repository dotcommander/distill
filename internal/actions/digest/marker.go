package digest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
const markerVersion = 2

// runMarker binds an artifact directory to the exact inputs that determine
// chunk identity: the post-clean source bytes and the chunk geometry. Chunking
// is deterministic, so a matching marker guarantees index-based artifact reuse
// refers to identical chunks.
type runMarker struct {
	Version               int            `json:"version"`
	Sources               []markerSource `json:"sources"`
	Chunks                []markerChunk  `json:"chunks"`
	ChunkSize             int            `json:"chunk_size"`
	MaxTokens             int            `json:"max_tokens"`
	NormalizationVersion  string         `json:"normalization_version"`
	ChunkAlgorithmVersion string         `json:"chunk_algorithm_version"`
}

type markerSource struct {
	Ordinal int    `json:"ordinal"`
	SHA256  string `json:"sha256"`
}

type markerChunk struct {
	ID            string `json:"id"`
	SourceOrdinal int    `json:"source_ordinal"`
	SHA256        string `json:"sha256"`
	Characters    int    `json:"characters"`
	Tokens        int    `json:"tokens"`
}

func markerFor(source string, chunkSize, maxTokens int) runMarker {
	plan, err := PlanPackedSources([]SourcePart{{Ordinal: 1, Text: source}}, chunkSize, maxTokens)
	if err != nil {
		// This legacy helper is retained for tests and only accepts arguments
		// already validated at the public boundary.
		return runMarker{Version: markerVersion, ChunkSize: chunkSize, MaxTokens: maxTokens}
	}
	return markerForPlan(plan)
}

func markerForPlan(plan PackedPlan) runMarker {
	m := runMarker{
		Version:               markerVersion,
		ChunkSize:             plan.ChunkSize,
		MaxTokens:             plan.MaxTokens,
		NormalizationVersion:  plan.NormalizationVersion,
		ChunkAlgorithmVersion: plan.ChunkAlgorithmVersion,
		Sources:               make([]markerSource, len(plan.Sources)),
		Chunks:                make([]markerChunk, len(plan.Chunks)),
	}
	for i, source := range plan.Sources {
		m.Sources[i] = markerSource{Ordinal: source.Ordinal, SHA256: source.Hash}
	}
	for i, chunk := range plan.Chunks {
		m.Chunks[i] = markerChunk{ID: chunk.ID, SourceOrdinal: chunk.SourceOrdinal, SHA256: chunk.Hash, Characters: chunk.Characters, Tokens: chunk.Tokens}
	}
	return m
}

func writeRunMarker(dir string, m runMarker) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("digest: encoding artifact marker: %w", err)
	}
	return fsutil.WriteFile(filepath.Join(dir, markerName), data, 0o644)
}

// claimRunMarker installs a fully written binding without replacing an
// existing claim. The private temporary inode lives inside artifactDir, so the
// final hard-link is an atomic no-replace publication on the same filesystem
// without requiring write access to the directory's parent.
func claimRunMarker(dir string, m runMarker) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("digest: encoding artifact marker: %w", err)
	}
	if mkdirErr := os.MkdirAll(dir, 0o750); mkdirErr != nil {
		return fmt.Errorf("digest: creating artifact directory: %w", mkdirErr)
	}
	f, err := os.CreateTemp(dir, ".distill-marker-*")
	if err != nil {
		return fmt.Errorf("digest: creating artifact marker claim: %w", err)
	}
	tempPath := f.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("digest: writing artifact marker: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("digest: syncing artifact marker: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("digest: closing artifact marker: %w", err)
	}
	if err := os.Link(tempPath, filepath.Join(dir, markerName)); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// ArtifactsMatchSource reports whether artifactDir carries a marker binding
// its artifacts to (source, chunkSize, maxTokens). Read-only; used by CLI
// dry-run and call planning to decide whether resume reuse will count.
func ArtifactsMatchSource(artifactDir, source string, chunkSize, maxTokens int) bool {
	if artifactDir == "" {
		return false
	}
	reuseOK, err := ValidateArtifactBinding(artifactDir, source, chunkSize, maxTokens)
	return err == nil && reuseOK
}

// ArtifactsMatchPlan is the structured-source equivalent of
// ArtifactsMatchSource. It is read-only and rejects all v1 directories.
func ArtifactsMatchPlan(artifactDir string, plan PackedPlan) bool {
	reuseOK, err := ValidateArtifactBindingPlan(artifactDir, plan)
	return err == nil && reuseOK
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

type artifactBindingState uint8

const (
	artifactBindingAbsent artifactBindingState = iota
	artifactBindingEmpty
	artifactBindingMatch
)

// ValidateArtifactBinding reports whether artifact reuse is safe for source.
// It never creates, removes, or changes the artifact directory. An absent or
// empty directory is valid for a new run but has no reusable artifacts.
func ValidateArtifactBinding(artifactDir, source string, chunkSize, maxTokens int) (reuseOK bool, err error) {
	plan, err := PlanPackedSources([]SourcePart{{Ordinal: 1, Text: source}}, chunkSize, maxTokens)
	if err != nil {
		return false, err
	}
	return ValidateArtifactBindingPlan(artifactDir, plan)
}

// ValidateArtifactBindingPlan reports whether the artifact directory belongs to
// this exact ordered structured-source plan. It never creates files.
func ValidateArtifactBindingPlan(artifactDir string, plan PackedPlan) (reuseOK bool, err error) {
	if artifactDir == "" {
		return false, nil
	}
	state, err := classifyArtifactBinding(artifactDir, markerForPlan(plan))
	if err != nil {
		return false, err
	}
	return state == artifactBindingMatch, nil
}

// PrepareArtifactBinding validates artifact ownership and, only for an absent
// or empty directory, stamps the current marker. Existing non-empty
// directories must already have an exact matching marker; they are never
// modified on an invalid binding.
func PrepareArtifactBinding(artifactDir, source string, chunkSize, maxTokens int) (reuseOK bool, err error) {
	plan, err := PlanPackedSources([]SourcePart{{Ordinal: 1, Text: source}}, chunkSize, maxTokens)
	if err != nil {
		return false, err
	}
	return PrepareArtifactBindingPlan(artifactDir, plan)
}

// PrepareArtifactBindingPlan validates then atomically binds a new/empty
// directory to plan. Existing schema v1 directories are intentionally left
// untouched and require a fresh --artifacts path.
func PrepareArtifactBindingPlan(artifactDir string, plan PackedPlan) (reuseOK bool, err error) {
	if artifactDir == "" {
		return false, nil
	}
	want := markerForPlan(plan)
	state, err := classifyArtifactBinding(artifactDir, want)
	if err != nil {
		return false, err
	}
	if state == artifactBindingMatch {
		return true, nil
	}
	if err := claimRunMarker(artifactDir, want); err != nil {
		if os.IsExist(err) {
			state, classifyErr := classifyArtifactBinding(artifactDir, want)
			if classifyErr != nil {
				return false, classifyErr
			}
			return state == artifactBindingMatch, nil
		}
		return false, fmt.Errorf("digest: writing artifact marker in %q: %w", artifactDir, err)
	}
	return false, nil
}

// classifyArtifactBinding is the shared fail-closed classifier behind
// validation and preparation. It makes no filesystem changes.
func classifyArtifactBinding(artifactDir string, want runMarker) (artifactBindingState, error) {
	info, err := os.Stat(artifactDir)
	if err != nil {
		if os.IsNotExist(err) {
			return artifactBindingAbsent, nil
		}
		return 0, fmt.Errorf("digest: artifacts %q: stat directory: %w", artifactDir, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("digest: artifacts %q: not a directory; refusing to modify non-empty directory", artifactDir)
	}
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		return 0, fmt.Errorf("digest: artifacts %q: read directory: %w", artifactDir, err)
	}
	if len(entries) == 0 {
		return artifactBindingEmpty, nil
	}

	markerPath := filepath.Join(artifactDir, markerName)
	markerInfo, err := os.Lstat(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, invalidArtifactBinding(artifactDir, "missing")
		}
		return 0, invalidArtifactBinding(artifactDir, "unreadable")
	}
	if !markerInfo.Mode().IsRegular() {
		return 0, invalidArtifactBinding(artifactDir, "not a regular file")
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return 0, invalidArtifactBinding(artifactDir, "unreadable")
	}
	var got runMarker
	if err := json.Unmarshal(data, &got); err != nil {
		return 0, invalidArtifactBinding(artifactDir, "corrupt")
	}
	if got.Version == 1 {
		return 0, fmt.Errorf("digest: artifacts %q: source marker schema v1 is incompatible; use a fresh artifact directory", artifactDir)
	}
	if got.Version != markerVersion {
		return 0, invalidArtifactBinding(artifactDir, "version mismatch")
	}
	if !reflect.DeepEqual(got, want) {
		return 0, invalidArtifactBinding(artifactDir, "mismatch")
	}
	return artifactBindingMatch, nil
}

func invalidArtifactBinding(artifactDir, reason string) error {
	return fmt.Errorf("digest: artifacts %q: source marker %s; refusing to modify non-empty directory", artifactDir, reason)
}
