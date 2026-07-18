// Package rankings loads distill's remote model ranking data.
package rankings

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dotcommander/distill/internal/fsutil"

	"gopkg.in/yaml.v3"
)

//go:embed defaults/rankings.yaml
var defaultRankings []byte

type Rankings struct {
	Roster []string            `yaml:"roster"`
	Boards map[string]Board    `yaml:"boards"`
	Roles  map[string]RoleRule `yaml:"roles"`
}

type Board struct {
	SourceURL     string             `yaml:"source_url"`
	Metric        string             `yaml:"metric"`
	LowerIsBetter bool               `yaml:"lower_is_better"`
	Render        bool               `yaml:"render"`
	ModelCol      int                `yaml:"model_col"`
	ScoreCol      int                `yaml:"score_col"`
	Aliases       map[string]string  `yaml:"aliases"`
	Scores        map[string]float64 `yaml:"scores"`
}

type RoleRule struct {
	Board           string `yaml:"board"`
	ConfigKey       string `yaml:"config_key"`
	CrossFamilyWith string `yaml:"cross_family_with,omitempty"`
}

// Path returns <XDG_CONFIG_HOME or ~/.config>/distill/rankings.yaml.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("rankings: resolving home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "distill", "rankings.yaml"), nil
}

// Load materializes the default rankings under <configDir>/distill/rankings.yaml
// on first run, then reads the (possibly user-edited) rankings back.
func Load() (*Rankings, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if werr := fsutil.WriteFile(path, defaultRankings, 0o644); werr != nil {
			return nil, fmt.Errorf("rankings: materializing default: %w", werr)
		}
	}
	return loadFrom(path)
}

func loadFrom(path string) (*Rankings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rankings: reading %s: %w", path, err)
	}
	var r Rankings
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("rankings: parsing %s: %w", path, err)
	}
	return &r, nil
}
