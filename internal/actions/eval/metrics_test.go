package eval

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestComputeMetrics(t *testing.T) {
	t.Parallel()
	j := JudgeResult{
		CandidateFactVerdicts: []FactVerdict{
			{Verdict: "SUPPORTED"},
			{Verdict: "supported"}, // case-insensitive
			{Verdict: "CONTRADICTED"},
			{Verdict: "NOT_IN_SOURCE"},
			{Verdict: "garbage"}, // unknown -> NotInSource
		},
		MissedReferenceFacts: []MissedFact{{}, {}}, // 2 missed
	}
	m := computeMetrics(j)
	if m.Supported != 2 || m.Contradicted != 1 || m.NotInSource != 2 {
		t.Fatalf("counts: supported=%d contradicted=%d notInSource=%d", m.Supported, m.Contradicted, m.NotInSource)
	}
	if m.MissedReference != 2 || m.CandidateFacts != 5 {
		t.Fatalf("missed=%d candidateFacts=%d", m.MissedReference, m.CandidateFacts)
	}
	if !approx(m.Precision, 0.4) || !approx(m.Recall, 0.5) {
		t.Fatalf("precision=%v recall=%v", m.Precision, m.Recall)
	}
	if !approx(m.F1, 2*0.4*0.5/(0.4+0.5)) {
		t.Fatalf("f1=%v", m.F1)
	}
}

func TestComputeMetricsZero(t *testing.T) {
	t.Parallel()
	m := computeMetrics(JudgeResult{})
	if m.Precision != 0 || m.Recall != 0 || m.F1 != 0 {
		t.Fatalf("expected zero metrics, got %+v", m)
	}
}

func TestAggregateMicroAverage(t *testing.T) {
	t.Parallel()
	a := Metrics{Supported: 1, NotInSource: 1, CandidateFacts: 2}
	b := Metrics{Supported: 3, Contradicted: 1, MissedReference: 2, CandidateFacts: 4}
	agg := aggregate([]Metrics{a, b})
	if agg.Supported != 4 || agg.MissedReference != 2 {
		t.Fatalf("agg counts: %+v", agg)
	}
	if !approx(agg.Precision, 4.0/6.0) || !approx(agg.Recall, 4.0/6.0) {
		t.Fatalf("agg P=%v R=%v", agg.Precision, agg.Recall)
	}
}

func TestExtractJSONObject(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":          `{"a":1}`,
		"prose before {\"x\":2} after":     `{"x":2}`,
		"no json here":                     "",
		`{"nested":{"y":3}}`:               `{"nested":{"y":3}}`,
		"codex\n{\"r\":1}\ntokens used\n9": `{"r":1}`, // trailing chatter
		"a {\"k\":1} mid {\"k\":2} end":    `{"k":2}`, // last object wins
		// Braces inside string values must not corrupt depth (the F7 fix).
		`{"rationale":"the set {a,b} is closed"}`: `{"rationale":"the set {a,b} is closed"}`,
		// Escaped quote inside a string, plus a brace inside that string.
		"{\"x\":\"a \\\" b}\"}": "{\"x\":\"a \\\" b}\"}",
		// Unbalanced (no matching close) salvages nothing.
		`prose {"a":1`: "",
	}
	for in, want := range cases {
		if got := extractJSONObject(in); got != want {
			t.Fatalf("extractJSONObject(%q) = %q, want %q", in, got, want)
		}
	}
}
