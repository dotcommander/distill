// Package researchcache stores per-chunk research responses across digest runs.
// It is an optimization only: misses and write failures fall back to normal
// provider calls without changing digest semantics.
package researchcache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/distill/internal/fsutil"
)

// Cache stores research responses under a namespace derived from the provider,
// endpoint, model, and research prompt template.
type Cache struct {
	dir string
}

const keyVersion = "researchcache/v1\n"

// New returns a cache rooted at
// <os.UserCacheDir>/distill/research/<provider-endpoint-model-prompt-hash>/.
func New(provider, endpoint, model, promptTemplate string) (*Cache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("researchcache: resolving user cache dir: %w", err)
	}
	namespace := hashKey(keyVersion + provider + "\x00" + endpoint + "\x00" + model + "\x00" + hashKey(promptTemplate))
	return newDir(filepath.Join(base, "distill", "research", namespace))
}

// newDir is the directory-explicit constructor used by tests.
func newDir(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("researchcache: creating dir: %w", err)
	}
	return &Cache{dir: dir}, nil
}

func (c *Cache) path(chunkText string) string {
	return filepath.Join(c.dir, hashKey(keyVersion+chunkText)+".md")
}

// Load returns a cached non-empty research response for chunkText.
func (c *Cache) Load(chunkText string) (string, bool) {
	data, err := os.ReadFile(c.path(chunkText))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "", false
	}
	return string(data), true
}

// Store writes a research response best-effort. Empty responses are not cached.
func (c *Cache) Store(chunkText, response string) {
	if strings.TrimSpace(response) == "" {
		return
	}
	_ = fsutil.WriteFile(c.path(chunkText), []byte(response), 0o600)
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
