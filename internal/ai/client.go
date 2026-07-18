package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/garyblankenship/wormhole/pkg/types"
	"github.com/garyblankenship/wormhole/pkg/wormhole"
)

// ErrEmptyResponse is returned by Complete when the model produces no content.
// Callers that can tolerate a missing answer (e.g. per-chunk extraction) match
// it with errors.Is to skip rather than abort; callers that require output
// (fuse/write/edit) treat it like any other error.
var ErrEmptyResponse = errors.New("ai complete: empty response")

// IsSystemic reports whether err is a run-fatal failure — bad credentials,
// exhausted quota, or a connectivity/endpoint problem (auth, quota, network) —
// where every model in a sweep will fail the same way. Callers should abort the
// whole run loudly rather than grind through the roster. Per-call transients
// (timeout, rate_limit, transient) and an ambiguous single-model config error
// are NOT systemic — skip those per-model and let a failure-rate gate catch a
// systemic config/endpoint fault.
func IsSystemic(err error) bool {
	switch types.ClassifyError(err) {
	case types.ErrorClassAuth, types.ErrorClassQuota, types.ErrorClassNetwork:
		return true
	case types.ErrorClassTransient, types.ErrorClassRateLimit, types.ErrorClassConfig, types.ErrorClassTimeout, types.ErrorClassUnknown:
		return false
	}
	return false
}

// Config configures the wormhole-backed AI client. Provider selects a built-in
// Wormhole provider; BaseURL is used only by named OpenAI-compatible providers
// and local endpoints. TextModel and EmbeddingModel select the model per
// operation.
type Config struct {
	Provider        string
	BaseURL         string
	APIKey          string
	TextModel       string
	FallbackModel   string
	EmbeddingModel  string
	ProviderOptions map[string]any
}

// Client wraps a wormhole client. It serves text completion (Complete) and
// batch embeddings (EmbedBatch, which satisfies the chunker's BatchEmbedder).
type Client struct {
	wh              *wormhole.Wormhole
	provider        string
	textModel       string
	embeddingModel  string
	fallbackModel   string
	providerOptions map[string]any
	openRouter      bool

	promptTokens atomic.Int64
	cachedTokens atomic.Int64
	outputTokens atomic.Int64
}

// Usage returns cumulative token counts across all completions made by this
// client: total prompt tokens, of which cached (prompt-cache hits), plus output.
func (c *Client) Usage() (prompt, cached, output int64) {
	return c.promptTokens.Load(), c.cachedTokens.Load(), c.outputTokens.Load()
}

// Endpoint owns the provider/runtime for one named Wormhole provider. Build it
// once when a command will use several models on the same provider/base URL so
// the underlying provider and HTTP transport are reused across model clients.
type Endpoint struct {
	wh         *wormhole.Wormhole
	provider   string
	openRouter bool
}

// New builds a Client against a single built-in Wormhole provider. Custom remote
// API endpoints are intentionally rejected by NewEndpoint; use the local profile
// for local OpenAI-compatible servers.
func New(cfg Config) (*Client, error) {
	endpoint, err := NewEndpoint(cfg)
	if err != nil {
		return nil, err
	}
	return endpoint.Client(cfg), nil
}

// NewEndpoint builds a reusable provider endpoint. Model-specific fields in cfg
// are intentionally ignored here; pass them to Endpoint.Client for each role or
// candidate model.
func NewEndpoint(cfg Config) (*Endpoint, error) {
	prov := cfg.Provider
	if prov == "" {
		prov = providerForBaseURL(cfg.BaseURL)
	}
	if prov == "" {
		return nil, fmt.Errorf("ai: unsupported provider endpoint %q; use a built-in Wormhole provider or --local", cfg.BaseURL)
	}
	if cfg.BaseURL == "" && providerNeedsBaseURL(prov) {
		return nil, fmt.Errorf("ai: base URL is required for provider %q", prov)
	}
	var opts []wormhole.Option
	switch prov {
	case "gemini":
		apiKey := firstNonEmpty(cfg.APIKey, os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
		providerConfig := types.NewProviderConfig(apiKey)
		if cfg.BaseURL != "" {
			providerConfig = providerConfig.WithBaseURL(cfg.BaseURL)
		}
		opts = []wormhole.Option{
			wormhole.WithGemini(apiKey, providerConfig),
			wormhole.WithDefaultProvider("gemini"),
		}
	case "openai":
		apiKey := firstNonEmpty(cfg.APIKey, os.Getenv("OPENAI_API_KEY"))
		providerConfig := types.NewProviderConfig(apiKey)
		if cfg.BaseURL != "" {
			providerConfig = providerConfig.WithBaseURL(cfg.BaseURL)
		}
		opts = []wormhole.Option{
			wormhole.WithOpenAI(apiKey, providerConfig),
			wormhole.WithDefaultProvider("openai"),
		}
	case "openrouter", "deepseek", "zai":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = APIKeyForProvider(prov)
		}
		providerConfig := types.NewProviderConfig(apiKey).WithBaseURL(cfg.BaseURL)
		opts = []wormhole.Option{
			wormhole.WithProfiledOpenAICompatible(prov, providerConfig),
			wormhole.WithDefaultProvider(prov),
		}
	case "local":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = APIKeyForProvider(prov)
		}
		opts = []wormhole.Option{
			wormhole.WithOpenAICompatible(prov, cfg.BaseURL, types.NewProviderConfig(apiKey)),
			wormhole.WithDefaultProvider(prov),
		}
	default:
		return nil, fmt.Errorf("ai: unsupported provider %q", prov)
	}
	wh := wormhole.New(opts...)
	return &Endpoint{
		wh:         wh,
		provider:   prov,
		openRouter: prov == "openrouter",
	}, nil
}

// Client returns a lightweight model-specific wrapper over the shared endpoint.
func (e *Endpoint) Client(cfg Config) *Client {
	return &Client{
		wh:              e.wh,
		provider:        e.provider,
		textModel:       cfg.TextModel,
		embeddingModel:  cfg.EmbeddingModel,
		fallbackModel:   cfg.FallbackModel,
		providerOptions: cloneProviderOptions(cfg.ProviderOptions),
		openRouter:      e.openRouter,
	}
}

// providerForBaseURL maps legacy configured URLs to built-in provider names.
// Unknown remote endpoints are rejected by NewEndpoint.
func providerForBaseURL(baseURL string) string {
	lower := strings.ToLower(baseURL)
	switch {
	case baseURL == "":
		return ""
	case strings.Contains(lower, "openrouter.ai"):
		return "openrouter"
	case strings.Contains(lower, "deepseek.com"):
		return "deepseek"
	case strings.Contains(lower, "z.ai"):
		return "zai"
	case strings.Contains(lower, "generativelanguage.googleapis.com"):
		return "gemini"
	default:
		return ""
	}
}

func providerNeedsBaseURL(provider string) bool {
	switch provider {
	case "openrouter", "deepseek", "zai", "local":
		return true
	default:
		return false
	}
}

// APIKeyForProvider resolves the API key for a built-in provider from the
// environment. It is the single source of truth for provider→key routing.
// The "local" provider deliberately never falls back to OPENAI_API_KEY: a
// local OpenAI-compatible server does not need a real paid credential, and a
// mistyped or tampered base URL must not receive one. Set
// DISTILL_LOCAL_API_KEY for local proxies that validate a key.
func APIKeyForProvider(provider string) string {
	switch provider {
	case "deepseek":
		return firstNonEmpty(os.Getenv("DEEPSEEK_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	case "openrouter":
		return firstNonEmpty(os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	case "zai":
		return firstNonEmpty(os.Getenv("ZAI_API_KEY"), os.Getenv("ZHIPU_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	case "gemini":
		return firstNonEmpty(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	case "local":
		return firstNonEmpty(os.Getenv("DISTILL_LOCAL_API_KEY"), "local")
	default:
		return os.Getenv("OPENAI_API_KEY")
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Complete runs a single non-streaming completion at temperature 0.2 and returns
// the response text. The error is annotated with wormhole's error class.
func (c *Client) Complete(ctx context.Context, prompt string) (string, error) {
	return c.CompleteWithTemperature(ctx, prompt, 0.2)
}

// CompleteWithTemperature runs a single non-streaming completion at the supplied
// temperature and returns the response text. Use Complete for the repository
// default; exact-fidelity evals can pass 0 for greedy decoding.
func (c *Client) CompleteWithTemperature(ctx context.Context, prompt string, temperature float32) (string, error) {
	req := c.wh.Text().Model(c.textModel).Prompt(prompt).Temperature(temperature)
	providerOptions := cloneProviderOptions(c.providerOptions)
	// OpenRouter native fallback: when a secondary model is configured, send the
	// models array so OpenRouter routes server-side to the fallback model if the
	// primary errors. Only OpenRouter supports this; other providers would ignore
	// (or reject) the field, so it is gated on the OpenRouter provider.
	if c.openRouter && c.fallbackModel != "" {
		if providerOptions == nil {
			providerOptions = map[string]any{}
		}
		providerOptions["models"] = []string{c.textModel, c.fallbackModel}
		providerOptions["route"] = "fallback"
	}
	if providerOptions != nil {
		req = req.ProviderOptions(providerOptions)
	}
	resp, err := req.Generate(ctx)
	if err != nil {
		return "", fmt.Errorf("ai complete (%s): %w", types.ClassifyError(err), err)
	}
	if resp.Usage != nil {
		c.promptTokens.Add(int64(resp.Usage.PromptTokens))
		c.cachedTokens.Add(int64(resp.Usage.CacheReadTokens))
		c.outputTokens.Add(int64(resp.Usage.CompletionTokens))
	}
	text := resp.Content()
	if text == "" {
		return "", ErrEmptyResponse
	}
	return text, nil
}

func cloneProviderOptions(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
