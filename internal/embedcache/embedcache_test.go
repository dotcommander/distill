package embedcache

import (
	"context"
	"testing"
)

// countingEmbedder records how many texts it was asked to embed.
type countingEmbedder struct {
	texts int
}

func (e *countingEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	e.texts += len(texts)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i), 1.5, -2.0}
	}
	return out, nil
}

func TestEmbeddingCacheServesSecondRunFromDisk(t *testing.T) {
	t.Parallel()
	inner := &countingEmbedder{}
	dir := t.TempDir()
	ctx := context.Background()
	texts := []string{"alpha", "beta", "gamma"}

	c, err := newEmbeddingCacheDir(inner, "test-model", "test-provider", "test-endpoint", dir)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	first, err := c.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("first embed: %v", err)
	}
	if inner.texts != 3 {
		t.Fatalf("expected 3 inner embeds on first run, got %d", inner.texts)
	}

	// A fresh cache instance over the same dir: the second run must be all hits.
	c2, err := newEmbeddingCacheDir(inner, "test-model", "test-provider", "test-endpoint", dir)
	if err != nil {
		t.Fatalf("new cache 2: %v", err)
	}
	second, err := c2.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("second embed: %v", err)
	}
	if inner.texts != 3 {
		t.Fatalf("expected 0 additional inner embeds (total still 3), got %d", inner.texts)
	}
	for i := range texts {
		if len(first[i]) != len(second[i]) {
			t.Fatalf("vector %d length mismatch: %d vs %d", i, len(first[i]), len(second[i]))
		}
		for j := range first[i] {
			if first[i][j] != second[i][j] {
				t.Fatalf("vector %d[%d] mismatch: %v vs %v", i, j, first[i][j], second[i][j])
			}
		}
	}
}

func TestEmbeddingCacheTextKeyNamespacedByProviderAndEndpoint(t *testing.T) {
	t.Parallel()
	base := &EmbeddingCache{model: "same-model", provider: "provider-a", endpoint: "https://a.example/v1"}
	alt := &EmbeddingCache{model: "same-model", provider: "provider-b", endpoint: "https://a.example/v1"}
	altEndpoint := &EmbeddingCache{model: "same-model", provider: "provider-a", endpoint: "https://b.example/v1"}

	key := base.textKey("hello")
	if got := alt.textKey("hello"); got == key {
		t.Fatalf("textKey ignored provider: got same key %q for different providers", got)
	}
	if got := altEndpoint.textKey("hello"); got == key {
		t.Fatalf("textKey ignored endpoint: got same key %q for different endpoints", got)
	}
}
