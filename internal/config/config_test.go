package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromMaterializesDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")

	c, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if c.Model == "" {
		t.Fatalf("expected a default model, got empty")
	}
	if c.BaseURL == "" {
		t.Fatalf("expected a default base_url, got empty")
	}
	if c.EmbeddingModel == "" || c.EmbeddingEndpoint == "" {
		t.Fatalf("expected embedding defaults, got model=%q endpoint=%q", c.EmbeddingModel, c.EmbeddingEndpoint)
	}
	if c.DeepSeekModel == "" || c.DeepSeekBaseURL == "" {
		t.Fatalf("expected DeepSeek profile defaults, got model=%q base_url=%q", c.DeepSeekModel, c.DeepSeekBaseURL)
	}
	if c.Style == "" {
		t.Fatalf("expected a default style, got empty")
	}
	if c.RequestTimeoutSeconds <= 0 {
		t.Fatalf("expected a positive request_timeout_seconds, got %d", c.RequestTimeoutSeconds)
	}
	if c.ExtractConcurrency <= 0 {
		t.Fatalf("expected a positive extract_concurrency, got %d", c.ExtractConcurrency)
	}

	// Idempotent: a second load reads the existing file.
	if _, err := loadFrom(path); err != nil {
		t.Fatalf("second loadFrom: %v", err)
	}
}

func TestEffectiveProfileDeepSeekUsesDirectModelIDs(t *testing.T) {
	t.Parallel()
	c := Config{
		Model:           "z-ai/glm-5.2",
		BaseURL:         "https://openrouter.ai/api/v1",
		ResearchModel:   "google/gemini-2.5-flash-lite-preview-09-2025",
		JudgeModel:      "deepseek/deepseek-v4-pro",
		FallbackModel:   "z-ai/glm-5.2",
		DeepSeekModel:   "deepseek-v4-pro",
		DeepSeekBaseURL: "https://api.deepseek.com",
	}

	model, baseURL := c.EffectiveProfile(ProfileDeepSeek)
	if model != "deepseek-v4-pro" || baseURL != "https://api.deepseek.com" {
		t.Fatalf("deepseek profile = (%q, %q)", model, baseURL)
	}
	if got := c.EffectiveRoleProfile("research", ProfileDeepSeek); got != "deepseek-v4-pro" {
		t.Fatalf("deepseek research model = %q", got)
	}
	if got := c.EffectiveEvalJudgeProfile(ProfileDeepSeek); got != "deepseek-v4-pro" {
		t.Fatalf("deepseek judge model = %q", got)
	}
	if got := c.EffectiveFallbackProfile(ProfileDeepSeek); got != "" {
		t.Fatalf("deepseek fallback model = %q", got)
	}
}

func TestLoadFromMergesDefaultsIntoExistingConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("model: custom-model\nbase_url: https://example.test/v1\n"), 0o644); err != nil {
		t.Fatalf("write old config: %v", err)
	}

	c, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if c.Model != "custom-model" || c.BaseURL != "https://example.test/v1" {
		t.Fatalf("existing overrides lost: model=%q base_url=%q", c.Model, c.BaseURL)
	}
	if c.DeepSeekModel == "" || c.DeepSeekBaseURL == "" {
		t.Fatalf("expected default DeepSeek profile to be merged, got model=%q base_url=%q", c.DeepSeekModel, c.DeepSeekBaseURL)
	}
	if c.RequestTimeoutSeconds <= 0 || c.ExtractConcurrency <= 0 {
		t.Fatalf("expected operational defaults to be merged, timeout=%d concurrency=%d", c.RequestTimeoutSeconds, c.ExtractConcurrency)
	}
}

func TestLoadFromMergesPartialStyleMap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("styles:\n  custom: concise custom prose\n"), 0o644); err != nil {
		t.Fatalf("write partial config: %v", err)
	}
	c, err := loadFrom(path)
	if err != nil { t.Fatalf("loadFrom: %v", err) }
	if got := c.Styles["custom"]; got != "concise custom prose" { t.Fatalf("custom style = %q", got) }
	for _, inherited := range []string{"narrative", "brief", "faq", "reference"} {
		if c.Styles[inherited] == "" { t.Errorf("default style %q was lost during partial map overlay", inherited) }
	}
}

func TestEffectiveJudgeSplit(t *testing.T) {
	t.Parallel()
	c := &Config{JudgeModel: "legacy", EvalJudgeModel: "evalX", MeritJudgeModel: "meritY"}
	if got := c.EffectiveEvalJudgeProfile(ProfileRemote); got != "evalX" {
		t.Errorf("eval judge = %q, want evalX", got)
	}
	if got := c.EffectiveMeritJudgeProfile(ProfileRemote); got != "meritY" {
		t.Errorf("merit judge = %q, want meritY", got)
	}
	f := &Config{JudgeModel: "legacy"}
	if got := f.EffectiveEvalJudgeProfile(ProfileRemote); got != "legacy" {
		t.Errorf("eval fallback = %q, want legacy", got)
	}
	if got := f.EffectiveMeritJudgeProfile(ProfileRemote); got != "legacy" {
		t.Errorf("merit fallback = %q, want legacy", got)
	}
}
