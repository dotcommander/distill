package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	assert.Contains(t, input.Text, "# Source: "+a+"\n\nalpha")
	assert.Contains(t, input.Text, "# Source: "+b+"\n\nbeta")
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
