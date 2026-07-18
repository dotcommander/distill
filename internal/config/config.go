// Package config loads distill's user configuration. Defaults live in an
// embedded config.yaml (config data, not Go literals); on first run they are
// materialized to <configDir>/distill/config.yaml so they can be edited, and are
// always read back from there. Secrets (API keys) are never stored here — they
// come from the environment.
package config

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dotcommander/distill/internal/fsutil"

	"gopkg.in/yaml.v3"
)

//go:embed defaults/config.yaml
var defaults embed.FS

// Config holds environment-specific defaults for distill's AI-backed commands.
// CLI flags and DISTILL_* env vars take precedence over these values.
type Config struct {
	Model                   string            `yaml:"model"`
	BaseURL                 string            `yaml:"base_url"`
	EmbeddingModel          string            `yaml:"embedding_model"`
	EmbeddingEndpoint       string            `yaml:"embedding_endpoint"`
	LocalModel              string            `yaml:"local_model"`
	LocalBaseURL            string            `yaml:"local_base_url"`
	DeepSeekModel           string            `yaml:"deepseek_model"`
	DeepSeekBaseURL         string            `yaml:"deepseek_base_url"`
	ZAIBaseURL              string            `yaml:"zai_base_url"`
	LocalEmbeddingModel     string            `yaml:"local_embedding_model"`
	LocalEmbeddingEndpoint  string            `yaml:"local_embedding_endpoint"`
	Style                   string            `yaml:"style"`
	Styles                  map[string]string `yaml:"styles"`
	RequestTimeoutSeconds   int               `yaml:"request_timeout_seconds"`
	ExtractConcurrency      int               `yaml:"extract_concurrency"`
	RequestRetries          int               `yaml:"request_retries"`
	PrecisionBatchSize      int               `yaml:"precision_batch_size"`
	MaxSections             int               `yaml:"max_sections"`
	ResearchModel           string            `yaml:"research_model"`
	ResearchEscalationModel string            `yaml:"research_escalation_model"`
	FuseModel               string            `yaml:"fuse_model"`
	WriteModel              string            `yaml:"write_model"`
	OutlineModel            string            `yaml:"outline_model"`
	FallbackModel           string            `yaml:"fallback_model"`
	EditModel               string            `yaml:"edit_model"`
	JudgeModel              string            `yaml:"judge_model"`
	EvalJudgeModel          string            `yaml:"eval_judge_model"`
	MeritJudgeModel         string            `yaml:"merit_judge_model"`
	PanelJudges             string            `yaml:"panel_judges"`
	RecognizeJudges         string            `yaml:"recognize_judges"`
	CascadeMinCapture       float64           `yaml:"cascade_min_capture"`
	MergeFactsThreshold     float64           `yaml:"merge_facts_threshold"`
	MinSectionFacts         int               `yaml:"min_section_facts"`
	ClusterBalanceFactor    float64           `yaml:"cluster_balance_factor"`
}

// Profile selects one of distill's configured endpoint/model profiles.
type Profile string

const (
	ProfileRemote   Profile = "remote"
	ProfileLocal    Profile = "local"
	ProfileDeepSeek Profile = "deepseek"
)

// EffectiveProfile returns the text model and base URL for the named profile.
// The default remote profile remains OpenRouter; local and deepseek are explicit
// opt-ins so their model IDs do not leak into normal runs.
func (c *Config) EffectiveProfile(profile Profile) (model, baseURL string) {
	switch profile {
	case ProfileRemote:
		return c.Model, c.BaseURL
	case ProfileLocal:
		return c.LocalModel, c.LocalBaseURL
	case ProfileDeepSeek:
		return c.DeepSeekModel, c.DeepSeekBaseURL
	default:
		return c.Model, c.BaseURL
	}
}

// Effective returns the text model and base URL to use, swapping to the
// local_* profile when local is true (the --local flag). Local models are
// strictly avoided unless --local is passed.
func (c *Config) Effective(local bool) (model, baseURL string) {
	if local {
		return c.EffectiveProfile(ProfileLocal)
	}
	return c.EffectiveProfile(ProfileRemote)
}

// EffectiveEmbedding mirrors Effective for the embedding model/endpoint.
func (c *Config) EffectiveEmbedding(local bool) (model, endpoint string) {
	if local {
		return c.LocalEmbeddingModel, c.LocalEmbeddingEndpoint
	}
	return c.EmbeddingModel, c.EmbeddingEndpoint
}

// EffectiveRole returns the model for a digest pipeline role (research, fuse,
// outline, write, edit), falling back to the shared Model when the per-role key is unset.
// Local mode uses the single on-box model and ignores per-role overrides.
func (c *Config) EffectiveRole(role string, local bool) string {
	return c.EffectiveRoleProfile(role, profileFromLocal(local))
}

// EffectiveRoleProfile returns the model for a digest pipeline role (research,
// fuse, outline, write, edit), falling back to the shared profile model when the
// per-role key is unset. Single-model profiles intentionally ignore remote
// per-role overrides because their model IDs belong to a different provider.
func (c *Config) EffectiveRoleProfile(role string, profile Profile) string {
	base, _ := c.EffectiveProfile(profile)
	if profile == ProfileLocal || profile == ProfileDeepSeek {
		return base
	}
	var override string
	switch role {
	case "research":
		override = c.ResearchModel
	case "fuse":
		override = c.FuseModel
	case "write":
		override = c.WriteModel
	case "outline":
		override = c.OutlineModel
	case "edit":
		override = c.EditModel
	}
	if override != "" {
		return override
	}
	return base
}

// judgeForProfile applies the remote-profile override for a resolved judge
// model, falling back to the profile's base model off-remote or when empty.
func (c *Config) judgeForProfile(profile Profile, model string) string {
	base, _ := c.EffectiveProfile(profile)
	if profile == ProfileRemote && model != "" {
		return model
	}
	return base
}

// EffectiveEvalJudge returns the fidelity (eval) judge model for the local flag.
func (c *Config) EffectiveEvalJudge(local bool) string {
	return c.EffectiveEvalJudgeProfile(profileFromLocal(local))
}

// EffectiveMeritJudge returns the readability (merit) judge model for the local flag.
func (c *Config) EffectiveMeritJudge(local bool) string {
	return c.EffectiveMeritJudgeProfile(profileFromLocal(local))
}

// EffectiveEvalJudgeProfile returns the eval (precision/recall/F1) judge model,
// preferring EvalJudgeModel and falling back to the legacy JudgeModel.
func (c *Config) EffectiveEvalJudgeProfile(profile Profile) string {
	model := c.EvalJudgeModel
	if model == "" {
		model = c.JudgeModel
	}
	return c.judgeForProfile(profile, model)
}

// EffectiveMeritJudgeProfile returns the merit (pairwise readability) judge model,
// preferring MeritJudgeModel and falling back to the legacy JudgeModel.
func (c *Config) EffectiveMeritJudgeProfile(profile Profile) string {
	model := c.MeritJudgeModel
	if model == "" {
		model = c.JudgeModel
	}
	return c.judgeForProfile(profile, model)
}

// EffectiveFallback returns the secondary model to try when a per-call retry
// budget is exhausted, or "" for no fallback. Local mode has a single on-box
// model, so there is no cross-model fallback.
func (c *Config) EffectiveFallback(local bool) string {
	return c.EffectiveFallbackProfile(profileFromLocal(local))
}

// EffectiveFallbackProfile returns the secondary model to try for profiles that
// support native cross-model fallback. Direct/local profiles keep one model.
func (c *Config) EffectiveFallbackProfile(profile Profile) string {
	if profile != ProfileRemote {
		return ""
	}
	return c.FallbackModel
}

func profileFromLocal(local bool) Profile {
	if local {
		return ProfileLocal
	}
	return ProfileRemote
}

// Path returns <XDG_CONFIG_HOME or ~/.config>/distill/config.yaml.
func Path() (string, error) {
	return configPath()
}

// Load materializes the default config under <configDir>/distill/config.yaml on
// first run, then reads the (possibly user-edited) config back.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return loadFrom(path)
}

// loadFrom reads config from path, writing the embedded default first if absent.
// Separated from Load so tests can target a temp path.
func loadFrom(path string) (*Config, error) {
	def, derr := defaults.ReadFile("defaults/config.yaml")
	if derr != nil {
		return nil, fmt.Errorf("config: reading embedded default: %w", derr)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if werr := fsutil.WriteFile(path, def, 0o644); werr != nil {
			return nil, fmt.Errorf("config: materializing default: %w", werr)
		}
	}
	var c Config
	if err := yaml.Unmarshal(def, &c); err != nil {
		return nil, fmt.Errorf("config: parsing embedded default: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return &c, nil
}

// configPath returns <XDG_CONFIG_HOME or ~/.config>/distill/config.yaml.
func configPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: resolving home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "distill", "config.yaml"), nil
}
