// Package codeeval loads coding-problem fixtures and orchestrates per-model code
// generation + DETERMINISTIC pass-rate scoring (compile + go test), the code
// counterpart to the label/comedy evals. NO LLM judge. Executing model output is
// gated behind an explicit opt-in at the command layer (see cmd/code.go).
package codeeval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Problem is one self-contained coding task. Signature is the exact Go func the
// model must implement; FuncName is that function's name (used by the generated
// test). Exactly one of Cases (hidden I/O pairs) or Harness (literal test body).
type Problem struct {
	ID        string `json:"id"`
	Prompt    string `json:"prompt"`
	Signature string `json:"signature"`
	FuncName  string `json:"func_name"`
	Cases     []Case `json:"cases,omitempty"`
	Harness   string `json:"harness,omitempty"`
}

// Case is one hidden test: a Go expression calling the function and the expected
// value it must equal (compared with reflect.DeepEqual in the generated test).
type Case struct {
	Call string `json:"call"`
	Want string `json:"want"`
}

// ProblemSet is the loaded fixture. Cost maps a model slug to $/MTok for the
// optional cost column.
type ProblemSet struct {
	Problems []Problem          `json:"problems"`
	Cost     map[string]float64 `json:"cost_per_mtok,omitempty"`
}

// LoadProblems reads and validates a problems fixture: ≥1 problem; unique
// non-empty ids; non-empty prompt/signature/func_name; exactly one of cases
// (each with non-empty call+want) or harness per problem.
func LoadProblems(path string) (ProblemSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProblemSet{}, err
	}
	var ps ProblemSet
	if err := json.Unmarshal(data, &ps); err != nil {
		return ProblemSet{}, fmt.Errorf("codeeval: parsing %s: %w", path, err)
	}
	if len(ps.Problems) == 0 {
		return ProblemSet{}, fmt.Errorf("codeeval: %s: no problems", path)
	}
	seen := map[string]bool{}
	for _, p := range ps.Problems {
		if strings.TrimSpace(p.ID) == "" {
			return ProblemSet{}, fmt.Errorf("codeeval: %s: problem with empty id", path)
		}
		if seen[p.ID] {
			return ProblemSet{}, fmt.Errorf("codeeval: %s: duplicate problem id %q", path, p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Prompt) == "" || strings.TrimSpace(p.Signature) == "" || strings.TrimSpace(p.FuncName) == "" {
			return ProblemSet{}, fmt.Errorf("codeeval: %s: problem %q missing prompt/signature/func_name", path, p.ID)
		}
		hasCases := len(p.Cases) > 0
		hasHarness := strings.TrimSpace(p.Harness) != ""
		if hasCases == hasHarness {
			return ProblemSet{}, fmt.Errorf("codeeval: %s: problem %q must have exactly one of cases or harness", path, p.ID)
		}
		for i, c := range p.Cases {
			if strings.TrimSpace(c.Call) == "" || strings.TrimSpace(c.Want) == "" {
				return ProblemSet{}, fmt.Errorf("codeeval: %s: problem %q case %d missing call/want", path, p.ID, i)
			}
		}
	}
	return ps, nil
}
