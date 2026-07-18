package gradecal

import (
	"context"
	"testing"
)

// lenJudge prefers the longer of the two versions — an order-robust, transitive
// comparator, so the tournament should produce a clean longest-to-shortest
// ranking with no flips and no cycles.
var lenJudge = judgeFunc(func(prompt string) (string, error) {
	a, b := splitAB(prompt)
	if len(a) >= len(b) {
		return `{"winner":"A","reason":"longer"}`, nil
	}
	return `{"winner":"B","reason":"longer"}`, nil
})

func TestTournament_TransitiveJudge(t *testing.T) {
	t.Parallel()
	ds := []Digest{
		{Name: "short", Text: "tiny."},
		{Name: "long", Text: "this is by far the longest of the three candidate texts here."},
		{Name: "mid", Text: "a medium length entry."},
	}
	res := RunTournament(context.Background(), lenJudge, fakeRenderer{}, "SRC", ds, 6)

	want := []string{"long", "mid", "short"}
	for i, w := range want {
		if res.Ranking[i] != w {
			t.Fatalf("ranking = %v, want %v", res.Ranking, want)
		}
	}
	if res.Flips != 0 {
		t.Errorf("Flips = %d, want 0 (length judge is order-robust)", res.Flips)
	}
	if res.Cycles != 0 {
		t.Errorf("Cycles = %d, want 0 (length judge is transitive)", res.Cycles)
	}
	if !res.Trustworthy() {
		t.Errorf("transitive judge should yield a Trustworthy tournament")
	}
	if w := res.Records["long"]; w == nil || w.Wins == 0 {
		t.Errorf("winner should have recorded wins, got %+v", w)
	}
}

func TestTournament_FlipOnGuesser(t *testing.T) {
	t.Parallel()
	ds := []Digest{
		{Name: "a", Text: "alpha text one."},
		{Name: "b", Text: "beta text two."},
		{Name: "c", Text: "gamma text three."},
	}
	// slotAJudge always picks slot A -> every comparison flips on the swap.
	res := RunTournament(context.Background(), slotAJudge, fakeRenderer{}, "SRC", ds, 6)
	if res.Flips != res.Comparisons {
		t.Fatalf("Flips = %d, want all %d comparisons to flip", res.Flips, res.Comparisons)
	}
	if res.Trustworthy() {
		t.Fatal("a pure position-biased guesser must NOT yield a Trustworthy tournament")
	}
}
