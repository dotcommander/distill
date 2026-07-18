package embedcache

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/dotcommander/distill/internal/fsutil"
)

// BatchEmbedder is the embedder this cache wraps; it matches the chunker's
// BatchEmbedder interface (EmbedBatch(ctx, []string) ([][]float32, error)).
type BatchEmbedder interface {
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingCache wraps a BatchEmbedder with a disk cache keyed by
// provider+endpoint+model+text. Repeated runs over the same content (e.g.
// tuning --threshold) reuse cached vectors instead of re-calling the embedding
// API. The cache directory and the per-text key are all namespaced by
// provider, endpoint, and model, so switching provider/endpoint or model never
// returns stale vectors from a different backend.
type EmbeddingCache struct {
	inner    BatchEmbedder
	model    string
	provider string
	endpoint string
	dir      string
}

// NewEmbeddingCache wraps inner, storing vectors under
// <os.UserCacheDir>/distill/embeddings/<provider-endpoint-model-hash>/.
func NewEmbeddingCache(inner BatchEmbedder, model, provider, endpoint string) (*EmbeddingCache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("cache: resolving user cache dir: %w", err)
	}
	dir := filepath.Join(base, "distill", "embeddings", hashKey(provider+"\x00"+endpoint+"\x00"+model))
	return newEmbeddingCacheDir(inner, model, provider, endpoint, dir)
}

// newEmbeddingCacheDir is the directory-explicit constructor used by tests.
func newEmbeddingCacheDir(inner BatchEmbedder, model, provider, endpoint, dir string) (*EmbeddingCache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("cache: creating dir: %w", err)
	}
	return &EmbeddingCache{inner: inner, model: model, provider: provider, endpoint: endpoint, dir: dir}, nil
}

// EmbedBatch returns vectors for texts in input order, serving hits from disk and
// embedding only the misses. It satisfies the chunker's BatchEmbedder interface.
func (c *EmbeddingCache) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missTexts []string
	var missIdx []int
	for i, t := range texts {
		if vec, ok := c.load(t); ok {
			out[i] = vec
			continue
		}
		missTexts = append(missTexts, t)
		missIdx = append(missIdx, i)
	}
	if len(missTexts) == 0 {
		return out, nil
	}
	vecs, err := c.inner.EmbedBatch(ctx, missTexts)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(missTexts) {
		return nil, fmt.Errorf("cache: embedder returned %d vectors for %d inputs", len(vecs), len(missTexts))
	}
	for j, idx := range missIdx {
		out[idx] = vecs[j]
		c.store(missTexts[j], vecs[j]) // best-effort; a write failure must not fail embedding
	}
	return out, nil
}

// textKey returns the cache key for one input: hex sha256 of provider ‖
// endpoint ‖ model ‖ text.
func (c *EmbeddingCache) textKey(text string) string {
	return hashKey(c.provider + "\x00" + c.endpoint + "\x00" + c.model + "\x00" + text)
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (c *EmbeddingCache) path(text string) string {
	return filepath.Join(c.dir, c.textKey(text)+".vec")
}

// load reads a cached vector (packed little-endian float32s); ok is false on any
// miss or unreadable/missized file.
func (c *EmbeddingCache) load(text string) (vec []float32, ok bool) {
	data, err := os.ReadFile(c.path(text))
	if err != nil || len(data) == 0 || len(data)%4 != 0 {
		return nil, false
	}
	vec = make([]float32, len(data)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return vec, true
}

// store writes a vector to disk atomically as packed little-endian float32s.
// Errors are swallowed: the cache is an optimization, never a correctness
// dependency.
func (c *EmbeddingCache) store(text string, vec []float32) {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:i*4+4], math.Float32bits(v))
	}
	_ = fsutil.WriteFile(c.path(text), buf, 0o600)
}
