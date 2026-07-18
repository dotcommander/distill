// Package labelscore deterministically scores single-label model outputs
// (classification / sentiment) against hand-curated gold labels. Scoring is
// offline and exact: a model's raw answer is normalized (lowercased, trimmed,
// punctuation stripped, whitespace collapsed) and compared to the gold label
// drawn from a fixed allowed set. The only nondeterminism in the harness is the
// model call itself (driven elsewhere); this file is pure.
package labelscore

import (
	"regexp"
	"sort"
	"strings"
)

// Item is one labeled example. Pred is filled by the runner from the model's
// raw answer; GoldLabel and the fixture's allowed set are the ground truth.
type Item struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	GoldLabel string `json:"gold_label"`
}

// Prediction pairs a scored item with the model's normalized answer.
type Prediction struct {
	ID      string
	Gold    string
	Pred    string // normalized; "" when unparseable
	InVocab bool   // Pred is one of the allowed labels
}

// ClassMetrics is per-class precision/recall/F1.
type ClassMetrics struct {
	Label     string
	Support   int // gold occurrences of this label
	TP        int
	FP        int
	FN        int
	Precision float64
	Recall    float64
	F1        float64
}

// ModelScore aggregates one model's run over the fixture.
type ModelScore struct {
	Model       string
	Accuracy    float64
	MacroF1     float64
	PerClass    []ClassMetrics // sorted by label
	N           int            // items scored
	Unparseable int            // empty/blank answers
	OutOfVocab  int            // answers not in the allowed set
	CostPerMTok float64        // 0 when unknown; surfaced only when >0
	ElapsedMS   int64          // total wall-time for this model's run (latency axis)
	Predictions []Prediction
}

var punctRe = regexp.MustCompile(`[^\p{L}\p{N}\s]+`)
var wsRe = regexp.MustCompile(`\s+`)

// Normalize lowercases, strips punctuation, and collapses whitespace so a
// model's "Positive." matches the gold "positive".
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = punctRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// ParseLabel extracts a single allowed label from a model's raw answer. It
// returns the normalized matching label and true when exactly one allowed label
// is found as a whitespace-bounded token; otherwise "" and false (unparseable
// or out-of-taxonomy). allowed must already be normalized.
func ParseLabel(raw string, allowed []string) (string, bool) {
	norm := Normalize(raw)
	if norm == "" {
		return "", false
	}
	if contains(allowed, norm) {
		return norm, true
	}
	// Fall back to the single allowed label appearing as a token in the answer.
	tokens := map[string]bool{}
	for _, t := range strings.Fields(norm) {
		tokens[t] = true
	}
	var hits []string
	for _, a := range allowed {
		if a != "" && (tokens[a] || containsLabel(norm, a)) {
			hits = append(hits, a)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

func containsLabel(norm, label string) bool {
	for start := 0; start <= len(norm); {
		idx := strings.Index(norm[start:], label)
		if idx < 0 {
			return false
		}
		p := start + idx
		q := p + len(label)
		leftOK := p == 0 || norm[p-1] == ' '
		rightOK := q == len(norm) || norm[q] == ' '
		if leftOK && rightOK {
			return true
		}
		start = p + 1
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Score computes accuracy, per-class P/R/F1, and macro-F1 from predictions over
// the allowed label set. Unparseable/out-of-vocab predictions count as wrong
// (they match no gold) and are tallied separately. allowed must be normalized.
func Score(model string, preds []Prediction, allowed []string) ModelScore {
	ms := ModelScore{Model: model, N: len(preds), Predictions: preds}
	correct := 0
	tp := map[string]int{}
	fp := map[string]int{}
	fn := map[string]int{}
	support := map[string]int{}
	for _, p := range preds {
		support[p.Gold]++
		switch {
		case p.Pred == "":
			ms.Unparseable++
			fn[p.Gold]++
		case !p.InVocab:
			ms.OutOfVocab++
			fn[p.Gold]++
		case p.Pred == p.Gold:
			correct++
			tp[p.Gold]++
		default:
			fp[p.Pred]++
			fn[p.Gold]++
		}
	}
	if ms.N > 0 {
		ms.Accuracy = float64(correct) / float64(ms.N)
	}
	labels := append([]string(nil), allowed...)
	sort.Strings(labels)
	var f1sum float64
	for _, l := range labels {
		cm := ClassMetrics{Label: l, Support: support[l], TP: tp[l], FP: fp[l], FN: fn[l]}
		if cm.TP+cm.FP > 0 {
			cm.Precision = float64(cm.TP) / float64(cm.TP+cm.FP)
		}
		if cm.TP+cm.FN > 0 {
			cm.Recall = float64(cm.TP) / float64(cm.TP+cm.FN)
		}
		if cm.Precision+cm.Recall > 0 {
			cm.F1 = 2 * cm.Precision * cm.Recall / (cm.Precision + cm.Recall)
		}
		ms.PerClass = append(ms.PerClass, cm)
		f1sum += cm.F1
	}
	if len(labels) > 0 {
		ms.MacroF1 = f1sum / float64(len(labels))
	}
	return ms
}

// RankByMacroF1 sorts model scores best-first (macro-F1, then accuracy,
// then model name for stable order).
func RankByMacroF1(scores []ModelScore) {
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].MacroF1 != scores[j].MacroF1 {
			return scores[i].MacroF1 > scores[j].MacroF1
		}
		if scores[i].Accuracy != scores[j].Accuracy {
			return scores[i].Accuracy > scores[j].Accuracy
		}
		return scores[i].Model < scores[j].Model
	})
}
