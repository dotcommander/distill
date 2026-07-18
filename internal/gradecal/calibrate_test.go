package gradecal

import (
	"context"
	"strings"
	"testing"
)

// fakeRenderer pipes the two versions through verbatim so a fake judge can
// inspect their content via the prompt.
type fakeRenderer struct{}

func (fakeRenderer) RenderMeritJudge(source, a, b string) string {
	return "A=<<" + a + ">>\nB=<<" + b + ">>"
}

func (f fakeRenderer) RenderComedyJudge(source, a, b string) string {
	return f.RenderMeritJudge(source, a, b)
}

func (f fakeRenderer) RenderPublishJudge(source, a, b string) string {
	return f.RenderMeritJudge(source, a, b)
}

// judgeFunc adapts a function to the Completer interface.
type judgeFunc func(prompt string) (string, error)

func (f judgeFunc) Complete(_ context.Context, prompt string) (string, error) { return f(prompt) }

func digests() []Digest {
	return []Digest{
		{Name: "m1", Text: "First sentence opens the piece. Second sentence builds on it. Third extends the idea further.\n\nFourth sentence starts paragraph two. Fifth sentence closes paragraph two."},
		{Name: "m2", Text: "Alpha begins the story here. Beta follows alpha closely. Gamma comes next in line.\n\nDelta starts the second part. Epsilon brings it to an end."},
	}
}

// perfectJudgeFor returns a judge that identifies the intact digest by identity
// (it closes over the known-good texts) — a stand-in for a judge that genuinely
// reads merit, robust to every sabotage including order-only shuffles.
func perfectJudgeFor(ds []Digest) judgeFunc {
	good := map[string]bool{}
	for _, d := range ds {
		good[strings.TrimSpace(d.Text)] = true
	}
	return func(prompt string) (string, error) {
		a, b := splitAB(prompt)
		if good[strings.TrimSpace(a)] {
			return `{"winner":"A","reason":"intact"}`, nil
		}
		if good[strings.TrimSpace(b)] {
			return `{"winner":"B","reason":"intact"}`, nil
		}
		return `{"winner":"A","reason":"fallback"}`, nil
	}
}

// slotAJudge always picks slot A regardless of content — a pure position-biased
// guesser.
var slotAJudge = judgeFunc(func(string) (string, error) {
	return `{"winner":"A","reason":"always A"}`, nil
})

// splitAB recovers the A and B payloads from fakeRenderer's prompt.
func splitAB(prompt string) (string, string) {
	a := between(prompt, "A=<<", ">>")
	b := between(prompt, "B=<<", ">>")
	return a, b
}

func between(s, lo, hi string) string {
	i := strings.Index(s, lo)
	if i < 0 {
		return ""
	}
	s = s[i+len(lo):]
	j := strings.Index(s, hi)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func TestCalibration_PerfectJudge(t *testing.T) {
	t.Parallel()
	ds := digests()
	_, m := RunCalibration(context.Background(), perfectJudgeFor(ds), fakeRenderer{}, "SRC", ds, Sabotages(), false)
	if m.Pairs != 2*len(Sabotages()) {
		t.Fatalf("pairs = %d, want %d", m.Pairs, 2*len(Sabotages()))
	}
	if m.SwapRobustAcc != 1.0 {
		t.Fatalf("SwapRobustAcc = %v, want 1.0", m.SwapRobustAcc)
	}
	if m.PositionBiasRate != 0.0 {
		t.Fatalf("PositionBiasRate = %v, want 0.0", m.PositionBiasRate)
	}
	if !m.Trustworthy() {
		t.Fatal("perfect judge should be Trustworthy")
	}
}

func TestCalibration_PositionBiasedGuesser(t *testing.T) {
	t.Parallel()
	_, m := RunCalibration(context.Background(), slotAJudge, fakeRenderer{}, "SRC", digests(), Sabotages(), false)
	// Always-A: good wins forward (good is in slot A) but never reverse, so
	// swap-robust accuracy is 0, naive accuracy is 1, and position bias is total.
	if m.SwapRobustAcc != 0.0 {
		t.Fatalf("SwapRobustAcc = %v, want 0.0", m.SwapRobustAcc)
	}
	if m.NaiveAcc != 1.0 {
		t.Fatalf("NaiveAcc = %v, want 1.0 (this is the trap a naive harness falls into)", m.NaiveAcc)
	}
	if m.PositionBiasRate != 1.0 {
		t.Fatalf("PositionBiasRate = %v, want 1.0", m.PositionBiasRate)
	}
	if m.Trustworthy() {
		t.Fatal("position-biased guesser must NOT be Trustworthy")
	}
}

func TestGroundedInWinner(t *testing.T) {
	t.Parallel()
	win := "The hardware sets the stage, but the unresolved details decide it."
	cases := []struct {
		name, reason string
		want         bool
	}{
		{"quotes_real_phrase", `A wins: 'the hardware sets the stage' is a strong close`, true},
		{"quotes_absent_phrase", `A wins: "a completely invented sentence here" lands well`, false},
		{"no_quote_at_all", `A is simply the better read overall`, false},
		{"quote_too_short", `A nails the word "the"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := groundedInWinner(tc.reason, win); got != tc.want {
				t.Fatalf("groundedInWinner(%q) = %v, want %v", tc.reason, got, tc.want)
			}
		})
	}
}

func TestSabotage_DegradesText(t *testing.T) {
	t.Parallel()
	src := "First sentence here. Second sentence follows.\n\nA second paragraph entirely."
	for _, s := range Sabotages() {
		got := s.Apply(src)
		if strings.TrimSpace(got) == strings.TrimSpace(src) {
			t.Errorf("sabotage %q did not change the text", s.Name)
		}
	}
	if n := len(strings.Fields(truncateText(src))); n >= len(strings.Fields(src)) {
		t.Errorf("truncate did not shorten: %d words", n)
	}
	if !strings.Contains(listify(src), "- ") {
		t.Error("listify did not produce bullets")
	}
}
