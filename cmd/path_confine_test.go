package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssertUnderDirAllowsChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	child := filepath.Join(dir, "001.md")
	assert.NoError(t, assertUnderDir(dir, child), "expected child to be allowed")
}

func TestAssertUnderDirRejectsParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parent := filepath.Dir(dir)
	outside := filepath.Join(parent, "outside.md")
	t.Cleanup(func() { _ = os.Remove(outside) })

	err := assertUnderDir(dir, outside)
	require.ErrorIs(t, err, errPathEscape)
}

func TestAssertUnderDirRejectsTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	traversal := filepath.Join(dir, "..", "escape.md")
	err := assertUnderDir(dir, traversal)
	require.ErrorIs(t, err, errPathEscape, "expected rejection of '..' traversal")
}

func TestAssertUnderDirEmptyDirAllowsAnything(t *testing.T) {
	t.Parallel()
	// Empty dir is an explicit opt-out — callers must pass a real root to
	// enable confinement. Mirrors lsp.isUnderCWD's contract.
	assert.NoError(t, assertUnderDir("", "/anywhere/at/all.md"), "expected nil for empty dir")
}
