package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "002.md"), []byte("two"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001.md"), []byte("one"), 0o600))
	require.NoError(t, WriteManifest(&Manifest{
		Chunks:      []ChunkInfo{{File: "002.md"}, {File: "001.md"}},
		TotalChunks: 2,
	}, dir))

	m, err := ReadManifest(dir)
	require.NoError(t, err)
	require.Equal(t, []ChunkInfo{{File: "002.md"}, {File: "001.md"}}, m.Chunks)
}

func TestReadManifestRejectsInvalidGenerationSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		chunks   []ChunkInfo
		total    int
		setup    func(*testing.T, string)
		wantText string
	}{
		{name: "empty", total: 0, wantText: "no chunks"},
		{name: "total mismatch", chunks: []ChunkInfo{{File: "001.md"}}, total: 2, setup: writeManifestChunks("001.md"), wantText: "total_chunks"},
		{name: "unsafe path", chunks: []ChunkInfo{{File: "../001.md"}}, total: 1, wantText: "unsafe filename"},
		{name: "not markdown", chunks: []ChunkInfo{{File: "001.txt"}}, total: 1, wantText: "unsafe filename"},
		{name: "duplicate", chunks: []ChunkInfo{{File: "001.md"}, {File: "001.md"}}, total: 2, setup: writeManifestChunks("001.md"), wantText: "duplicates"},
		{name: "missing", chunks: []ChunkInfo{{File: "missing.md"}}, total: 1, wantText: "reading manifest chunk"},
		{name: "directory", chunks: []ChunkInfo{{File: "001.md"}}, total: 1, setup: func(t *testing.T, dir string) {
			require.NoError(t, os.Mkdir(filepath.Join(dir, "001.md"), 0o700))
		}, wantText: "not a regular file"},
		{name: "symlink", chunks: []ChunkInfo{{File: "001.md"}}, total: 1, setup: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "target.md"), []byte("target"), 0o600))
			require.NoError(t, os.Symlink("target.md", filepath.Join(dir, "001.md")))
		}, wantText: "not a regular file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			require.NoError(t, WriteManifest(&Manifest{Chunks: tt.chunks, TotalChunks: tt.total}, dir))
			_, err := ReadManifest(dir)
			require.ErrorContains(t, err, tt.wantText)
		})
	}
}

func TestReadManifestRejectsOversizedManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data := `{"chunks":[],"padding":"` + strings.Repeat("x", maxManifestBytes) + `"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(data), 0o600))

	_, err := ReadManifest(dir)
	require.ErrorContains(t, err, "exceeds maximum size")
}

func TestReadManifestRejectsNonRegularManifest(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "manifest.json")
			switch kind {
			case "directory":
				require.NoError(t, os.Mkdir(path, 0o700))
			case "symlink":
				target := filepath.Join(dir, "target.json")
				require.NoError(t, os.WriteFile(target, []byte(`{}`), 0o600))
				require.NoError(t, os.Symlink(target, path))
			}
			_, err := ReadManifest(dir)
			require.ErrorContains(t, err, "not a regular file")
		})
	}
}

func TestOpenChunkRejectsSymlinkReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chunkPath := filepath.Join(dir, "001.md")
	targetPath := filepath.Join(dir, "target.md")
	require.NoError(t, os.WriteFile(chunkPath, []byte("safe"), 0o600))
	require.NoError(t, os.WriteFile(targetPath, []byte("secret"), 0o600))

	file, err := openVerifiedChunk(dir, "001.md", func() {
		require.NoError(t, os.Remove(chunkPath))
		require.NoError(t, os.Symlink("target.md", chunkPath))
	})
	if file != nil {
		require.NoError(t, file.Close())
	}
	require.ErrorContains(t, err, "changed while opening")
}

func writeManifestChunks(names ...string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		for _, name := range names {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600))
		}
	}
}
