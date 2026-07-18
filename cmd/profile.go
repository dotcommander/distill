package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
)

func effectivePreflightMaxTokens(local bool, configured int) int {
	if local {
		return 0
	}
	return configured
}

type resolvedEndpoint struct {
	model    string
	provider string
	baseURL  string
}

func profileFromFlags(local, deepseek bool) (config.Profile, error) {
	switch {
	case local && deepseek:
		return "", errors.New("--local and --deepseek are mutually exclusive")
	case local:
		return config.ProfileLocal, nil
	case deepseek:
		return config.ProfileDeepSeek, nil
	default:
		return config.ProfileRemote, nil
	}
}

func endpointForTextModel(cfg *config.Config, profile config.Profile, model, baseURLFlag string) (resolvedEndpoint, error) {
	if explicitBaseURL := firstNonEmpty(baseURLFlag, os.Getenv("DISTILL_BASE_URL")); explicitBaseURL != "" {
		if profile != config.ProfileLocal {
			return resolvedEndpoint{}, errors.New("custom AI base URLs are disabled; use a built-in Wormhole provider or --local")
		}
		return resolvedEndpoint{model: model, provider: "local", baseURL: explicitBaseURL}, nil
	}
	if profile == config.ProfileLocal {
		_, baseURL := cfg.EffectiveProfile(profile)
		return resolvedEndpoint{model: model, provider: "local", baseURL: baseURL}, nil
	}
	if profile == config.ProfileDeepSeek || (profile == config.ProfileRemote && isDeepSeekTextModel(model)) {
		return resolvedEndpoint{model: directDeepSeekModelID(model), provider: "deepseek", baseURL: cfg.DeepSeekBaseURL}, nil
	}
	if profile == config.ProfileRemote && isZAITextModel(model) {
		return resolvedEndpoint{model: directZAIModelID(model), provider: "zai", baseURL: cfg.ZAIBaseURL}, nil
	}
	if profile == config.ProfileRemote && isGeminiTextModel(model) {
		return resolvedEndpoint{model: directGeminiModelID(model), provider: "gemini"}, nil
	}
	_, baseURL := cfg.EffectiveProfile(profile)
	if !isOpenRouterBaseURL(baseURL) {
		return resolvedEndpoint{}, fmt.Errorf("remote profile base_url %q is not allowed; use OpenRouter, a direct built-in provider model prefix, or --local", baseURL)
	}
	return resolvedEndpoint{model: model, provider: "openrouter", baseURL: baseURL}, nil
}

func isOpenRouterBaseURL(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), "openrouter.ai")
}

func isDeepSeekTextModel(model string) bool {
	lower := strings.ToLower(model)
	return lower == "deepseek" || strings.HasPrefix(lower, "deepseek/") || strings.HasPrefix(lower, "deepseek-")
}

func directDeepSeekModelID(model string) string {
	if strings.HasPrefix(strings.ToLower(model), "deepseek/") {
		return model[len("deepseek/"):]
	}
	return model
}

func isGeminiTextModel(model string) bool {
	lower := strings.ToLower(model)
	return strings.HasPrefix(lower, "google/gemini") || strings.HasPrefix(lower, "gemini-") || strings.HasPrefix(lower, "models/gemini-")
}

func directGeminiModelID(model string) string {
	lower := strings.ToLower(model)
	const flashLitePreview = "gemini-2.5-flash-lite-preview-09-2025"
	switch {
	case strings.HasPrefix(lower, "google/"):
		model = model[len("google/"):]
	case strings.HasPrefix(lower, "models/"):
		model = model[len("models/"):]
	}
	if strings.EqualFold(model, flashLitePreview) {
		return "gemini-2.5-flash-lite"
	}
	return model
}

func isZAITextModel(model string) bool {
	lower := strings.ToLower(model)
	return strings.HasPrefix(lower, "z-ai/") || strings.HasPrefix(lower, "zai/") ||
		strings.HasPrefix(lower, "glm-") || strings.HasPrefix(lower, "glm/")
}

func directZAIModelID(model string) string {
	if i := strings.IndexByte(model, '/'); i >= 0 {
		switch strings.ToLower(model[:i]) {
		case "z-ai", "zai":
			return model[i+1:]
		}
	}
	return model
}

// resolveJudges picks the judge list when --judges was not set explicitly:
// --deepseek selects deepseekModel, otherwise the configured default if any.
// An explicit --judges flag (judgesChanged) always keeps current.
func resolveJudges(current, deepseekModel, configDefault string, judgesChanged, deepseek bool) string {
	switch {
	case judgesChanged:
		return current
	case deepseek:
		return deepseekModel
	case configDefault != "":
		return configDefault
	default:
		return current
	}
}

type textClientCache struct {
	mu        sync.Mutex
	endpoints map[string]*ai.Endpoint
	clients   map[string]*ai.Client
}

func newTextClientCache() *textClientCache {
	return &textClientCache{
		endpoints: map[string]*ai.Endpoint{},
		clients:   map[string]*ai.Client{},
	}
}

func (c *textClientCache) Client(cfg *config.Config, profile config.Profile, model, baseURLFlag string) (*ai.Client, string, string, error) {
	resolved, err := endpointForTextModel(cfg, profile, model, baseURLFlag)
	if err != nil {
		return nil, "", "", err
	}
	textModel, provider, baseURL := resolved.model, resolved.provider, resolved.baseURL

	c.mu.Lock()
	defer c.mu.Unlock()

	clientKey := provider + "\x00" + baseURL + "\x00" + textModel
	if client, ok := c.clients[clientKey]; ok {
		return client, textModel, baseURL, nil
	}

	endpointKey := provider + "\x00" + baseURL
	endpoint, ok := c.endpoints[endpointKey]
	if !ok {
		var err error
		endpoint, err = ai.NewEndpoint(ai.Config{
			Provider: provider,
			BaseURL:  baseURL,
			APIKey:   ai.APIKeyForProvider(provider),
		})
		if err != nil {
			return nil, "", "", fmt.Errorf("creating ai endpoint: %w", err)
		}
		c.endpoints[endpointKey] = endpoint
	}

	client := endpoint.Client(ai.Config{TextModel: textModel})
	c.clients[clientKey] = client
	return client, textModel, baseURL, nil
}
