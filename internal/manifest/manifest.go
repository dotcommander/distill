package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxManifestBytes = 8 << 20

type ChunkInfo struct {
	File      string `json:"file"`
	Tokens    *int   `json:"tokens,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Manifest struct {
	Source               string      `json:"source"`
	Mode                 string      `json:"mode"`
	Tokenizer            string      `json:"tokenizer,omitempty"`
	TokenCountsAvailable bool        `json:"token_counts_available"`
	Chunks               []ChunkInfo `json:"chunks"`
	TotalTokens          *int        `json:"total_tokens,omitempty"`
	TotalChunks          int         `json:"total_chunks"`
}

func WriteManifest(m *Manifest, dir string) error {
	data, err := ToJSON(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600)
}

// ReadManifest loads and validates the generation set described by
// dir/manifest.json. Chunk entries must be unique flat Markdown filenames
// naming regular, non-symlink files in dir.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := readManifestData(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if err := validateManifestChunks(dir, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// OpenChunk opens one validated flat Markdown chunk without following a
// replacement symlink between validation and open.
func OpenChunk(dir, name string) (*os.File, error) {
	return openVerifiedChunk(dir, name, nil)
}

func openVerifiedChunk(dir, name string, beforeOpen func()) (*os.File, error) {
	if name == "" || filepath.IsAbs(name) || filepath.Base(name) != name ||
		filepath.Clean(name) != name || filepath.Ext(name) != ".md" {
		return nil, fmt.Errorf("unsafe manifest chunk filename %q", name)
	}
	path := filepath.Join(dir, name)
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspecting manifest chunk %q: %w", name, err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest chunk %q is not a regular file", name)
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening manifest chunk %q: %w", name, err)
	}
	openInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspecting open manifest chunk %q: %w", name, statErr)
	}
	if !openInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("manifest chunk %q changed while opening", name)
	}
	return file, nil
}

func readManifestData(path string) ([]byte, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspecting manifest: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, errors.New("manifest is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening manifest: %w", err)
	}
	openInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspecting open manifest: %w", statErr)
	}
	if !openInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openInfo) {
		_ = file.Close()
		return nil, errors.New("manifest changed while opening")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("reading manifest: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing manifest: %w", closeErr)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds maximum size of %d bytes", maxManifestBytes)
	}
	return data, nil
}

func validateManifestChunks(dir string, m *Manifest) error {
	if len(m.Chunks) == 0 {
		return errors.New("manifest contains no chunks")
	}
	if m.TotalChunks != len(m.Chunks) {
		return fmt.Errorf("manifest total_chunks is %d, want %d", m.TotalChunks, len(m.Chunks))
	}

	seen := make(map[string]struct{}, len(m.Chunks))
	for i, chunk := range m.Chunks {
		name := chunk.File
		if name == "" || filepath.IsAbs(name) || filepath.Base(name) != name ||
			filepath.Clean(name) != name || filepath.Ext(name) != ".md" {
			return fmt.Errorf("manifest chunk %d has unsafe filename %q", i+1, name)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("manifest chunk %d duplicates filename %q", i+1, name)
		}
		seen[name] = struct{}{}

		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("reading manifest chunk %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("manifest chunk %q is not a regular file", name)
		}
	}
	return nil
}

func ToJSON(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
