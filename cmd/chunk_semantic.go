package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/embedcache"

	"github.com/dotcommander/reliquary/pipeline/chunking"
)

// chunkSemantic builds a remote OpenAI-compatible embedder (resolving model and
// endpoint from flags → env → config), runs reliquary's SemanticChunker over the
// disk-backed embedding cache, then applies the optional cl100k_base preflight
// budget. Embeddings are the only network dependency in the chunk command.
func chunkSemantic(ctx context.Context, f *chunkFlags, text string, size, overlap int, tc chunking.TokenCounter) ([]chunking.Chunk, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cached, model, err := buildCachedEmbedder(ctx, cfg, f.local, f.provider, f.embeddingModel)
	if err != nil {
		return nil, fmt.Errorf("semantic mode: %w", err)
	}
	latch := &latchEmbedder{inner: cached}
	sc, err := chunking.NewSemanticChunker(latch, chunking.SemanticOpts{
		BreakSensitivity: f.threshold,
		SmoothingWindow:  f.smoothingWindow,
		CoherenceWindow:  f.coherenceWindow,
		MinChunkChars:    f.minChunkChars,
		MaxChunkChars:    f.maxChunkChars,
	})
	if err != nil {
		return nil, fmt.Errorf("creating semantic chunker: %w", err)
	}
	embedStart := time.Now()
	chunks := sc.ChunkSemantic(ctx, text, size, overlap)
	// reliquary silently falls back to boundary chunking when a mid-run
	// EmbedBatch fails; the latch turns that into a hard error so the output
	// (and its manifest "mode": "semantic") never lies about provenance.
	if lerr := latch.Err(); lerr != nil {
		return nil, fmt.Errorf("semantic mode: embedding failed mid-run; refusing to emit fallback chunks as semantic: %w", lerr)
	}
	slog.InfoContext(ctx, "semantic embedding complete",
		"model", model,
		"embed_duration", time.Since(embedStart),
	)
	if tc == nil {
		return chunks, nil
	}
	return chunking.EnforceTokenLimits(chunks, tc), nil
}

func embeddingProvider(flagProvider, baseURL string, local bool) string {
	if local {
		return "local"
	}
	if flagProvider != "" {
		switch flagProvider {
		case "openai", "openrouter", "gemini":
			return flagProvider
		default:
			return ""
		}
	}
	lower := strings.ToLower(baseURL)
	switch {
	case strings.Contains(lower, "openrouter.ai"):
		return "openrouter"
	case strings.Contains(lower, "generativelanguage.googleapis.com"):
		return "gemini"
	case strings.Contains(lower, "api.openai.com"):
		return "openai"
	default:
		return ""
	}
}

// latchEmbedder wraps the embedding cache and records the first inner
// EmbedBatch error. reliquary's SemanticChunker swallows embedding errors and
// silently falls back to boundary chunking; the latch lets chunkSemantic
// distinguish a genuine semantic run from a degraded one after the fact.
type latchEmbedder struct {
	inner embedcache.BatchEmbedder
	mu    sync.Mutex
	err   error
}

func (l *latchEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vecs, err := l.inner.EmbedBatch(ctx, texts)
	if err != nil {
		l.mu.Lock()
		if l.err == nil {
			l.err = err
		}
		l.mu.Unlock()
	}
	return vecs, err
}

// Err returns the first embedding error observed, or nil.
func (l *latchEmbedder) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}
