package ai

import (
	"context"
	"fmt"

	"github.com/garyblankenship/wormhole/pkg/types"
)

// embedBatchSize bounds inputs per embeddings request, matching the prior remote
// embedder's sub-batching to stay within provider input limits.
const embedBatchSize = 64

// EmbedBatch embeds texts and returns vectors in input order. It satisfies the
// chunker's BatchEmbedder interface: EmbedBatch(ctx, []string) ([][]float32, error).
// wormhole returns []float64 carrying a per-item Index; results are placed by
// Index and narrowed to float32.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]
		resp, err := c.wh.Embeddings().Model(c.embeddingModel).Input(batch...).Generate(ctx)
		if err != nil {
			return nil, fmt.Errorf("ai embed batch [%d:%d] (%s): %w", start, end, types.ClassifyError(err), err)
		}
		if len(resp.Embeddings) != len(batch) {
			return nil, fmt.Errorf("ai embed batch: got %d vectors for %d inputs", len(resp.Embeddings), len(batch))
		}
		for _, e := range resp.Embeddings {
			if e.Index < 0 || e.Index >= len(batch) {
				return nil, fmt.Errorf("ai embed batch: response index %d out of range [0,%d)", e.Index, len(batch))
			}
			vec := make([]float32, len(e.Embedding))
			for i, v := range e.Embedding {
				vec[i] = float32(v)
			}
			out[start+e.Index] = vec
		}
	}
	return out, nil
}
