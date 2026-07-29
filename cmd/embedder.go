package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/embedcache"
)

type cachedEmbedderOptions struct {
	ctx          context.Context
	cfg          *config.Config
	local        bool
	providerFlag string
	modelFlag    string
	budget       *ai.RequestBudget
}

func buildCachedEmbedder(opts cachedEmbedderOptions) (embedcache.BatchEmbedder, string, error) {
	effModel, effEndpoint := opts.cfg.EffectiveEmbedding(opts.local)
	model := firstNonEmpty(opts.modelFlag, os.Getenv("DISTILL_EMBEDDING_MODEL"), effModel)
	if model == "" {
		return nil, "", errors.New("embeddings require --embedding-model, $DISTILL_EMBEDDING_MODEL, or config")
	}
	if os.Getenv("DISTILL_EMBEDDING_ENDPOINT") != "" && !opts.local {
		return nil, "", errors.New("custom remote embedding endpoints are disabled; use a built-in Wormhole provider or --local")
	}
	baseURL := firstNonEmpty(os.Getenv("DISTILL_EMBEDDING_ENDPOINT"), effEndpoint)
	baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/embeddings")
	provider := embeddingProvider(opts.providerFlag, baseURL, opts.local)
	if provider == "" {
		return nil, "", fmt.Errorf("embedding endpoint %q is not a built-in Wormhole provider; use --local for local OpenAI-compatible endpoints", baseURL)
	}
	if provider == "openrouter" && !strings.Contains(strings.ToLower(baseURL), "openrouter.ai") {
		return nil, "", fmt.Errorf("embedding provider openrouter requires an OpenRouter endpoint, got %q", baseURL)
	}
	if provider == "openai" || provider == "gemini" {
		baseURL = ""
	}
	embedder, err := ai.New(ai.Config{
		Provider:       provider,
		BaseURL:        baseURL,
		APIKey:         ai.APIKeyForProvider(provider),
		EmbeddingModel: model,
		RequestBudget:  opts.budget,
		NoRetries:      opts.budget != nil && opts.budget.Limit() > 0,
	})
	if err != nil {
		return nil, "", fmt.Errorf("creating embedder: %w", err)
	}
	if _, perr := embedder.EmbedBatch(opts.ctx, []string{"ping"}); perr != nil {
		return nil, "", fmt.Errorf("embedding provider unreachable (model %q via %s): %w", model, provider, perr)
	}
	cached, err := embedcache.NewEmbeddingCache(embedder, model, provider, baseURL)
	if err != nil {
		return nil, "", fmt.Errorf("creating embedding cache: %w", err)
	}
	return cached, model, nil
}
