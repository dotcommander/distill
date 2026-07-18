package cmd

import (
	"testing"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
)

func TestProfileFromFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		local    bool
		deepseek bool
		want     config.Profile
		wantErr  bool
	}{
		{name: "remote", want: config.ProfileRemote},
		{name: "local", local: true, want: config.ProfileLocal},
		{name: "deepseek", deepseek: true, want: config.ProfileDeepSeek},
		{name: "conflict", local: true, deepseek: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := profileFromFlags(tt.local, tt.deepseek)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("profileFromFlags: %v", err)
			}
			if got != tt.want {
				t.Fatalf("profileFromFlags = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEffectivePreflightMaxTokens(t *testing.T) {
	t.Parallel()

	if got := effectivePreflightMaxTokens(false, 4000); got != 4000 {
		t.Fatalf("remote preflight budget = %d, want 4000", got)
	}
	if got := effectivePreflightMaxTokens(true, 4000); got != 0 {
		t.Fatalf("local preflight budget = %d, want disabled", got)
	}
}

func TestAPIKeyForProvider(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	t.Setenv("GEMINI_API_KEY", "gemini-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")

	tests := []struct {
		name     string
		provider string
		want     string
	}{
		{name: "deepseek", provider: "deepseek", want: "deepseek-key"},
		{name: "openrouter", provider: "openrouter", want: "openrouter-key"},
		{name: "gemini", provider: "gemini", want: "gemini-key"},
		{name: "openai", provider: "openai", want: "openai-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ai.APIKeyForProvider(tt.provider); got != tt.want {
				t.Fatalf("apiKeyForProvider = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpointForTextModelRoutesDeepSeekToDirectProvider(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		BaseURL:         "https://openrouter.ai/api/v1",
		DeepSeekBaseURL: "https://api.deepseek.com",
	}

	tests := []struct {
		model string
		want  string
	}{
		{model: "deepseek/deepseek-v4-pro", want: "deepseek-v4-pro"},
		{model: "DeepSeek/deepseek-v4-pro", want: "deepseek-v4-pro"},
		{model: "deepseek-v4-pro", want: "deepseek-v4-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got, err := endpointForTextModel(cfg, config.ProfileRemote, tt.model, "")
			if err != nil {
				t.Fatalf("endpointForTextModel: %v", err)
			}
			if got.model != tt.want || got.provider != "deepseek" || got.baseURL != "https://api.deepseek.com" {
				t.Fatalf("endpointForTextModel = %#v, want model %q via direct DeepSeek", got, tt.want)
			}
		})
	}
}

func TestEndpointForTextModelRejectsRemoteExplicitBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		BaseURL:         "https://openrouter.ai/api/v1",
		DeepSeekBaseURL: "https://api.deepseek.com",
	}

	if _, err := endpointForTextModel(cfg, config.ProfileRemote, "deepseek/deepseek-v4-pro", "https://openrouter.ai/api/v1"); err == nil {
		t.Fatal("expected custom remote base URL to be rejected")
	}
}

func TestResolveJudgesPresenceMatrix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct { name string; current, configDefault string; changed, deepseek bool; want string }{
		{"explicit beats deepseek", "explicit", "configured", true, true, "explicit"},
		{"deepseek beats config", defaultPanelJudges, "configured", false, true, "deepseek-direct"},
		{"config replaces built-in default", defaultPanelJudges, "configured", false, false, "configured"},
		{"built-in survives empty config", defaultPanelJudges, "", false, false, defaultPanelJudges},
	} {
		t.Run(tc.name, func(t *testing.T) { if got:=resolveJudges(tc.current,"deepseek-direct",tc.configDefault,tc.changed,tc.deepseek); got!=tc.want { t.Fatalf("resolveJudges = %q, want %q",got,tc.want) } })
	}
}

func TestTextClientCacheReusesEndpointPerBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Model:   "model-a",
		BaseURL: "https://openrouter.ai/api/v1",
	}
	cache := newTextClientCache()

	first, firstModel, firstBaseURL, err := cache.Client(cfg, config.ProfileRemote, "model-a", "")
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	second, secondModel, secondBaseURL, err := cache.Client(cfg, config.ProfileRemote, "model-a", "")
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	if first != second {
		t.Fatal("same model/base URL should reuse the cached client wrapper")
	}
	if firstModel != "model-a" || secondModel != "model-a" {
		t.Fatalf("models = (%q, %q), want model-a", firstModel, secondModel)
	}
	if firstBaseURL != cfg.BaseURL || secondBaseURL != cfg.BaseURL {
		t.Fatalf("base URLs = (%q, %q), want %q", firstBaseURL, secondBaseURL, cfg.BaseURL)
	}

	third, thirdModel, _, err := cache.Client(cfg, config.ProfileRemote, "model-b", "")
	if err != nil {
		t.Fatalf("third client: %v", err)
	}
	if third == first {
		t.Fatal("different model should get a distinct client wrapper")
	}
	if thirdModel != "model-b" {
		t.Fatalf("third model = %q, want model-b", thirdModel)
	}
	if len(cache.endpoints) != 1 {
		t.Fatalf("endpoints = %d, want one shared endpoint", len(cache.endpoints))
	}
	if len(cache.clients) != 2 {
		t.Fatalf("clients = %d, want one per model", len(cache.clients))
	}
}

func TestOpenRouterProviderOptionsAttachStableSessionID(t *testing.T) {
	t.Parallel()
	sourcePath := "testdata/source.md"

	first := providerOptionsForDigest("openrouter", sourcePath)
	second := providerOptionsForDigest("openrouter", sourcePath)
	if first == nil || second == nil {
		t.Fatal("OpenRouter provider options should include a session_id")
	}
	if first["session_id"] == "" {
		t.Fatal("session_id is empty")
	}
	if first["session_id"] != second["session_id"] {
		t.Fatalf("session_id changed: %v vs %v", first["session_id"], second["session_id"])
	}
}

func TestDeepSeekProviderOptionsAttachStableUserID(t *testing.T) {
	t.Parallel()
	sourcePath := "testdata/source.md"

	first := providerOptionsForDigest("deepseek", sourcePath)
	second := providerOptionsForDigest("deepseek", sourcePath)
	if first == nil || second == nil {
		t.Fatal("DeepSeek provider options should include a user_id")
	}
	if first["user_id"] == "" {
		t.Fatal("user_id is empty")
	}
	if first["user_id"] != second["user_id"] {
		t.Fatalf("user_id changed: %v vs %v", first["user_id"], second["user_id"])
	}
	if got := providerOptionsForDigest("gemini", sourcePath); got != nil {
		t.Fatalf("generic provider options = %#v, want nil", got)
	}
}

func TestZAITextModelRouting(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		ZAIBaseURL: "https://api.z.ai/api/coding/paas/v4",
		BaseURL:    "https://openrouter.ai/api/v1",
	}
	if got, err := endpointForTextModel(cfg, config.ProfileRemote, "z-ai/glm-5.2", ""); err != nil || got.model != "glm-5.2" || got.provider != "zai" || got.baseURL != "https://api.z.ai/api/coding/paas/v4" {
		t.Errorf("z-ai/glm-5.2 routed to (%#v,%v), want glm-5.2 via zai", got, err)
	}
	if got, err := endpointForTextModel(cfg, config.ProfileRemote, "glm-5.2", ""); err != nil || got.model != "glm-5.2" || got.provider != "zai" || got.baseURL != "https://api.z.ai/api/coding/paas/v4" {
		t.Errorf("bare glm-5.2 routed to (%#v,%v), want glm-5.2 via zai", got, err)
	}
	if _, err := endpointForTextModel(cfg, config.ProfileRemote, "z-ai/glm-5.2", "http://override"); err == nil {
		t.Error("explicit remote base-url override should be rejected")
	}
}

func TestGeminiTextModelRouting(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{BaseURL: "https://openrouter.ai/api/v1"}

	tests := []struct {
		name  string
		model string
		want  string
	}{
		{name: "stable", model: "google/gemini-2.5-flash-lite", want: "gemini-2.5-flash-lite"},
		{name: "stale preview", model: "google/gemini-2.5-flash-lite-preview-09-2025", want: "gemini-2.5-flash-lite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := endpointForTextModel(cfg, config.ProfileRemote, tt.model, "")
			if err != nil {
				t.Fatalf("endpointForTextModel: %v", err)
			}
			if got.model != tt.want || got.provider != "gemini" || got.baseURL != "" {
				t.Fatalf("gemini routed to %#v, want native gemini provider model %q", got, tt.want)
			}
		})
	}
}
