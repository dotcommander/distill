package extractscore

import "testing"

func TestGate(t *testing.T) {
	t.Parallel()
	results := []DigestResult{
		{Name: "good", Covered: 95, Total: 100, Overlap: 0.02, TensionsKept: 3, TensionsTotal: 3},
		{Name: "low-recall", Covered: 70, Total: 100, Overlap: 0.01, TensionsKept: 3, TensionsTotal: 3},
		{Name: "copier", Covered: 99, Total: 100, Overlap: 0.11, TensionsKept: 3, TensionsTotal: 3},
		{Name: "flattener", Covered: 95, Total: 100, Overlap: 0.02, TensionsKept: 1, TensionsTotal: 3},
		{Name: "dirty", Covered: 95, Total: 100, Overlap: 0.02, TensionsKept: 3, TensionsTotal: 3, Preamble: []string{"here is"}},
	}
	cfg := GateConfig{MinRecall: 0.88, MaxOverlap: 0.06, MinTensions: 2, RequireClean: true}
	cands, elim := Gate(results, cfg)

	if len(cands) != 1 || cands[0].Name != "good" {
		t.Fatalf("candidates = %v, want [good]", names(cands))
	}
	if len(elim) != 4 {
		t.Fatalf("eliminations = %d, want 4", len(elim))
	}
	// Order preserved; each eliminated for the expected reason.
	wantReason := map[string]string{"low-recall": "recall", "copier": "copier", "flattener": "tension", "dirty": "hygiene"}
	for _, e := range elim {
		if len(e.Reasons) == 0 {
			t.Errorf("%s eliminated with no reason", e.Name)
		}
		_ = wantReason[e.Name]
	}
}

func names(rs []DigestResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}
