package labelscore

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LabelFixture is the gold set for one label task. Task is "classification" or
// "sentiment". Allowed is the closed label taxonomy. Cost maps a model name to
// its $/MTok for the optional cost column (omit to leave cost blank).
type LabelFixture struct {
	Task    string             `json:"task"`
	Allowed []string           `json:"allowed_labels"`
	Items   []Item             `json:"items"`
	Cost    map[string]float64 `json:"cost_per_mtok,omitempty"`
}

// NormalizedAllowed returns the allowed labels normalized for matching.
func (fx LabelFixture) NormalizedAllowed() []string {
	out := make([]string, 0, len(fx.Allowed))
	for _, a := range fx.Allowed {
		out = append(out, Normalize(a))
	}
	return out
}

// LoadFixture reads and validates a label fixture: task and allowed set must be
// non-empty, item ids unique and non-empty, and every gold label must be in the
// allowed set (a typo in gold otherwise silently tanks a class's recall).
func LoadFixture(path string) (LabelFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LabelFixture{}, err
	}
	var fx LabelFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return LabelFixture{}, fmt.Errorf("labelscore: parsing %s: %w", path, err)
	}
	if strings.TrimSpace(fx.Task) == "" {
		return LabelFixture{}, fmt.Errorf("labelscore: %s: empty task", path)
	}
	if len(fx.Allowed) == 0 {
		return LabelFixture{}, fmt.Errorf("labelscore: %s: empty allowed_labels", path)
	}
	allowed := map[string]bool{}
	for _, a := range fx.NormalizedAllowed() {
		if a == "" {
			return LabelFixture{}, fmt.Errorf("labelscore: %s: blank allowed label", path)
		}
		allowed[a] = true
	}
	seen := map[string]bool{}
	for _, it := range fx.Items {
		if strings.TrimSpace(it.ID) == "" {
			return LabelFixture{}, fmt.Errorf("labelscore: %s: item with empty id", path)
		}
		if seen[it.ID] {
			return LabelFixture{}, fmt.Errorf("labelscore: %s: duplicate item id %q", path, it.ID)
		}
		seen[it.ID] = true
		if !allowed[Normalize(it.GoldLabel)] {
			return LabelFixture{}, fmt.Errorf("labelscore: %s: item %q gold_label %q not in allowed_labels", path, it.ID, it.GoldLabel)
		}
	}
	return fx, nil
}
