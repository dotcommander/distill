package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadDigestInputSingleFilePreservesSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "one.md")
	require.NoError(t, os.WriteFile(path, []byte("# One\n\nalpha\n"), 0o600))

	input, err := readDigestInput(strings.NewReader(""), []string{path})
	require.NoError(t, err)
	assert.Equal(t, path, input.Source)
	assert.False(t, input.Multi)
	assert.Equal(t, "# One\n\nalpha\n", input.Text)
}

func TestReadDigestInputCombinesPathspecsWithBoundaries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	require.NoError(t, os.WriteFile(a, []byte("alpha\n"), 0o600))
	require.NoError(t, os.WriteFile(b, []byte("beta\n"), 0o600))

	input, err := readDigestInput(strings.NewReader(""), []string{filepath.Join(dir, "*.md")})
	require.NoError(t, err)
	assert.Equal(t, "2 files", input.Source)
	assert.True(t, input.Multi)
	assert.Contains(t, input.Text, "# Source 01\n\nalpha")
	assert.Contains(t, input.Text, "# Source 02\n\nbeta")
	assert.NotContains(t, input.Text, dir)
	require.Len(t, input.Parts, 2)
	assert.Equal(t, a, input.Parts[0].Path)
	assert.Equal(t, b, input.Parts[1].Path)
	assert.Less(t, strings.Index(input.Text, "alpha"), strings.Index(input.Text, "beta"))
}

func TestReadDigestInputDirectoryUsesMarkdownFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("alpha\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("skip\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("beta\n"), 0o600))

	input, err := readDigestInput(strings.NewReader(""), []string{dir})
	require.NoError(t, err)
	assert.True(t, input.Multi)
	assert.Contains(t, input.Text, "alpha")
	assert.Contains(t, input.Text, "beta")
	assert.NotContains(t, input.Text, "skip")
}

func TestReadDigestInputDirectoryUsesManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001.md"), []byte("alpha\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "002.md"), []byte("beta\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.md"), []byte("must not be read\n"), 0o600))
	require.NoError(t, manifest.WriteManifest(&manifest.Manifest{
		Chunks:      []manifest.ChunkInfo{{File: "002.md"}, {File: "001.md"}},
		TotalChunks: 2,
	}, dir))

	input, err := readDigestInput(strings.NewReader(""), []string{dir})
	require.NoError(t, err)
	assert.True(t, input.Multi)
	assert.Less(t, strings.Index(input.Text, "beta"), strings.Index(input.Text, "alpha"))
	assert.NotContains(t, input.Text, "must not be read")
}

func TestReadDigestInputRejectsInvalidManifest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		manifest manifest.Manifest
		want     string
	}{
		{
			name:     "missing listed chunk",
			manifest: manifest.Manifest{Chunks: []manifest.ChunkInfo{{File: "missing.md"}}, TotalChunks: 1},
			want:     "reading manifest chunk",
		},
		{
			name:     "inconsistent total",
			manifest: manifest.Manifest{Chunks: []manifest.ChunkInfo{{File: "001.md"}}, TotalChunks: 2},
			want:     "total_chunks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "001.md"), []byte("fallback must not run"), 0o600))
			data, err := json.Marshal(tt.manifest)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600))

			_, err = readDigestInput(strings.NewReader(""), []string{dir})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestExpandDigestInputPathspecRetainsManifestProvenance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001.md"), []byte("alpha"), 0o600))
	require.NoError(t, manifest.WriteManifest(&manifest.Manifest{
		Chunks:      []manifest.ChunkInfo{{File: "001.md"}},
		TotalChunks: 1,
	}, dir))

	paths, err := expandDigestInputPathspec(dir)
	require.NoError(t, err)
	require.Equal(t, []digestPath{{
		path:         filepath.Join(dir, "001.md"),
		fromManifest: true,
	}}, paths)
}

func TestExpandDigestPathspecDoesNotInterpretManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listed := filepath.Join(dir, "001.md")
	unlisted := filepath.Join(dir, "stale.md")
	require.NoError(t, os.WriteFile(listed, []byte("listed"), 0o600))
	require.NoError(t, os.WriteFile(unlisted, []byte("unlisted"), 0o600))
	require.NoError(t, manifest.WriteManifest(&manifest.Manifest{
		Chunks:      []manifest.ChunkInfo{{File: "001.md"}},
		TotalChunks: 1,
	}, dir))

	paths, err := expandDigestPathspec(dir)
	require.NoError(t, err)
	require.Equal(t, []string{listed, unlisted}, paths)
}

func TestExpandDigestPathspecSupportsRecursiveMarkdownGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	rootFile := filepath.Join(dir, "root.md")
	nestedFile := filepath.Join(nested, "child.md")
	require.NoError(t, os.WriteFile(rootFile, []byte("root\n"), 0o600))
	require.NoError(t, os.WriteFile(nestedFile, []byte("child\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "skip.txt"), []byte("skip\n"), 0o600))

	paths, err := expandDigestPathspec(filepath.Join(dir, "**", "*.md"))
	require.NoError(t, err)
	assert.Equal(t, []string{nestedFile, rootFile}, paths)
}
