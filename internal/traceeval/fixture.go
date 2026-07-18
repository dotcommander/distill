package traceeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Task is one trusted, single-file Go program whose final stdout line is the
// exact answer the model must predict.
type Task struct {
	ID       string `json:"id"`
	Category string `json:"category,omitempty"`
	Program  string `json:"program"`
	Gold     string `json:"gold,omitempty"`
}

// Fixture is the gold set for Go program tracing. Cost maps a model slug to
// $/MTok for the optional report column.
type Fixture struct {
	Tasks []Task             `json:"tasks"`
	Cost  map[string]float64 `json:"cost_per_mtok,omitempty"`
}

// LoadFixture reads, validates, and gold-verifies a trace fixture. Empty gold
// values are filled from trusted execution; non-empty gold must match execution.
func LoadFixture(ctx context.Context, path string, timeout time.Duration) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, err
	}
	var fx Fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return Fixture{}, fmt.Errorf("traceeval: parsing %s: %w", path, err)
	}
	if len(fx.Tasks) == 0 {
		return Fixture{}, fmt.Errorf("traceeval: %s: no tasks", path)
	}
	seen := map[string]bool{}
	for i := range fx.Tasks {
		t := &fx.Tasks[i]
		if strings.TrimSpace(t.ID) == "" {
			return Fixture{}, fmt.Errorf("traceeval: %s: task with empty id", path)
		}
		if seen[t.ID] {
			return Fixture{}, fmt.Errorf("traceeval: %s: duplicate task id %q", path, t.ID)
		}
		seen[t.ID] = true
		if strings.TrimSpace(t.Program) == "" {
			return Fixture{}, fmt.Errorf("traceeval: %s: task %q has empty program", path, t.ID)
		}
		if err := verifyTaskGold(ctx, t, timeout); err != nil {
			return Fixture{}, fmt.Errorf("traceeval: %s: task %q: %w", path, t.ID, err)
		}
	}
	return fx, nil
}

// SelectTasks returns a copy of fx filtered by task id. The include filter is
// applied first when present; the exclude filter always wins. Task order is
// preserved.
func SelectTasks(fx Fixture, include, exclude []string) (Fixture, error) {
	known := make(map[string]bool, len(fx.Tasks))
	for _, task := range fx.Tasks {
		known[task.ID] = true
	}
	includeSet, err := taskIDSet("include", include, known)
	if err != nil {
		return Fixture{}, err
	}
	excludeSet, err := taskIDSet("exclude", exclude, known)
	if err != nil {
		return Fixture{}, err
	}

	out := Fixture{Cost: fx.Cost}
	for _, task := range fx.Tasks {
		if len(includeSet) > 0 && !includeSet[task.ID] {
			continue
		}
		if excludeSet[task.ID] {
			continue
		}
		out.Tasks = append(out.Tasks, task)
	}
	if len(out.Tasks) == 0 {
		return Fixture{}, errors.New("traceeval: task filters selected no tasks")
	}
	return out, nil
}

func taskIDSet(name string, ids []string, known map[string]bool) (map[string]bool, error) {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !known[id] {
			return nil, fmt.Errorf("traceeval: %s task %q not found in fixture", name, id)
		}
		set[id] = true
	}
	return set, nil
}

func verifyTaskGold(ctx context.Context, t *Task, timeout time.Duration) error {
	gold, err := RunProgram(ctx, *t, timeout)
	if err != nil {
		return err
	}
	if t.Gold == "" {
		t.Gold = gold
		return nil
	}
	if t.Gold != gold {
		return fmt.Errorf("gold %q does not match go run stdout %q", t.Gold, gold)
	}
	return nil
}
