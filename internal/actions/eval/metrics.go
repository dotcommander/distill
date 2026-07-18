// Package eval implements distill's offline extraction-quality harness: an LLM
// judge scores each candidate extraction against the source chunk (using a
// reference extraction as a coverage checklist), yielding precision/recall/F1.
package eval

import (
	"strings"

	"github.com/dotcommander/distill/internal/llmjson"
)

// FactVerdict is the judge's ruling on one candidate fact.
type FactVerdict struct {
	Fact             string `json:"fact"`
	Verdict          string `json:"verdict"`
	Rationale        string `json:"rationale"`
	MatchedReference string `json:"matched_reference"`
}

// MissedFact is a reference fact the candidate omitted.
type MissedFact struct {
	ReferenceFact string `json:"reference_fact"`
	Rationale     string `json:"rationale"`
}

// JudgeResult is the parsed JSON object the judge returns for one chunk.
type JudgeResult struct {
	CandidateFactVerdicts []FactVerdict `json:"candidate_fact_verdicts"`
	MissedReferenceFacts  []MissedFact  `json:"missed_reference_facts"`
	Summary               string        `json:"summary"`
}

// Metrics are the per-chunk (or aggregated) extraction-quality scores.
type Metrics struct {
	CandidateFacts  int     `json:"candidate_facts"`
	Supported       int     `json:"supported"`
	Contradicted    int     `json:"contradicted"`
	NotInSource     int     `json:"not_in_source"`
	MissedReference int     `json:"missed_reference"`
	Precision       float64 `json:"precision"`
	Recall          float64 `json:"recall"`
	F1              float64 `json:"f1"`
}

// computeMetrics scores one judge result. Unknown verdicts bucket as NotInSource.
func computeMetrics(j JudgeResult) Metrics {
	m := Metrics{
		CandidateFacts:  len(j.CandidateFactVerdicts),
		MissedReference: len(j.MissedReferenceFacts),
	}
	for _, v := range j.CandidateFactVerdicts {
		switch strings.ToUpper(strings.TrimSpace(v.Verdict)) {
		case "SUPPORTED":
			m.Supported++
		case "CONTRADICTED":
			m.Contradicted++
		default:
			m.NotInSource++
		}
	}
	m.finalize()
	return m
}

// finalize derives precision/recall/f1 from the counts.
// Precision = supported/(supported+contradicted+not_in_source);
// Recall = supported/(supported+missed_reference); F1 is their harmonic mean.
func (m *Metrics) finalize() {
	falsePositives := m.Contradicted + m.NotInSource
	if denom := m.Supported + falsePositives; denom > 0 {
		m.Precision = float64(m.Supported) / float64(denom)
	}
	if denom := m.Supported + m.MissedReference; denom > 0 {
		m.Recall = float64(m.Supported) / float64(denom)
	}
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}
}

// aggregate sums per-chunk counts and recomputes precision/recall/f1 from the
// totals (micro-average).
func aggregate(per []Metrics) Metrics {
	var agg Metrics
	for _, m := range per {
		agg.CandidateFacts += m.CandidateFacts
		agg.Supported += m.Supported
		agg.Contradicted += m.Contradicted
		agg.NotInSource += m.NotInSource
		agg.MissedReference += m.MissedReference
	}
	agg.finalize()
	return agg
}

// extractJSONObject salvages the last balanced top-level JSON object from an
// LLM/CLI response that may wrap it in prose, ```json fences, or trailing
// chatter (a CLI judge's header + token-usage footer, or an echoed duplicate).
// It forward-scans tracking string-literal state (with backslash escapes), so a
// '{' or '}' inside a string value — e.g. "rationale":"the set {a,b}" — does not
// corrupt brace depth. Returns "" when no balanced object is present.
func extractJSONObject(s string) string {
	return llmjson.ExtractObject(s)
}
