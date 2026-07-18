package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errPathEscape signals a write path resolved outside its allowed root. Used
// as a defense-in-depth tripwire: callers should construct paths so this
// error is unreachable; if it ever fires, an upstream filename-derivation
// bug just funnelled user content into a system path.
var errPathEscape = errors.New("path escapes allowed directory")

// assertUnderDir returns errPathEscape if path resolves (after symlink
// evaluation) outside dir. An empty dir disables the check — callers must
// pass a real directory to enable confinement.
func assertUnderDir(dir, path string) error {
	if dir == "" {
		return nil
	}
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	// Evaluate symlinks on the root (it must exist by contract). On
	// platforms where /tmp is a symlink (macOS: /var → /private/var) the
	// path's parent shares the resolved prefix, but the path itself does
	// not exist yet (we're about to write it). Walk up to the first
	// existing ancestor of pathAbs, resolve that, then re-attach the
	// remainder lexically so the prefixes line up.
	if evaluated, cerr := filepath.EvalSymlinks(rootAbs); cerr == nil {
		rootAbs = evaluated
	}
	pathAbs = resolveExistingPrefix(pathAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("relativize: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s not under %s", errPathEscape, pathAbs, rootAbs)
	}
	return nil
}

// resolveExistingPrefix returns p with its longest existing ancestor's
// symlinks evaluated and the non-existent remainder appended lexically.
// Enables symlink-aware confinement on paths that have not been created
// yet (the common case for "about to write" assertions).
func resolveExistingPrefix(p string) string {
	suffix := ""
	cur := p
	for {
		if _, err := os.Lstat(cur); err == nil {
			if resolved, err := filepath.EvalSymlinks(cur); err == nil {
				return filepath.Join(resolved, suffix)
			}
			return p
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		suffix = filepath.Join(filepath.Base(cur), suffix)
		cur = parent
	}
}
