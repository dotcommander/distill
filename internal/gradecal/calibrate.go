package gradecal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Completer is the judge backend (satisfied by the wormhole ai.Client and by an
// external-command judge — the same shape the eval command uses).
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Renderer builds the pairwise prompt for a (source, versionA, versionB) triple.
// RenderComedyJudge is the same shape with a "which is funnier" criterion; the
// comparator selects which one judgeOnce uses via its criterion field.
type Renderer interface {
	RenderMeritJudge(source, versionA, versionB string) string
	RenderComedyJudge(source, versionA, versionB string) string
	RenderPublishJudge(source, versionA, versionB string) string
}

// Digest is one candidate read.
type Digest struct {
	Name string
	Text string
}

type verdict struct {
	Winner string `json:"winner"`
	Reason string `json:"reason"`
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// parseVerdict salvages the JSON object from a judge reply and validates the
// winner is exactly "A" or "B".
func parseVerdict(raw string) (verdict, error) {
	m := jsonObjRe.FindString(raw)
	if m == "" {
		return verdict{}, fmt.Errorf("no JSON object in judge reply: %q", trunc(raw))
	}
	var v verdict
	if err := json.Unmarshal([]byte(m), &v); err != nil {
		return verdict{}, fmt.Errorf("parsing judge reply %q: %w", trunc(m), err)
	}
	v.Winner = strings.ToUpper(strings.TrimSpace(v.Winner))
	if v.Winner != "A" && v.Winner != "B" {
		return verdict{}, fmt.Errorf("judge winner not A/B: %q", v.Winner)
	}
	return v, nil
}

// judgeOnce runs one matchup in a fixed slot order and returns the verdict.
// criterion selects the rubric: "comedy" uses the funnier-bit prompt, anything
// else (incl. "") uses the merit/faithfulness prompt.
func judgeOnce(ctx context.Context, judge Completer, r Renderer, criterion, source, slotA, slotB string) (verdict, error) {
	var prompt string
	switch criterion {
	case "comedy":
		prompt = r.RenderComedyJudge(source, slotA, slotB)
	case "publish":
		prompt = r.RenderPublishJudge(source, slotA, slotB)
	default:
		prompt = r.RenderMeritJudge(source, slotA, slotB)
	}
	raw, err := judge.Complete(ctx, prompt)
	if err != nil {
		return verdict{}, err
	}
	return parseVerdict(raw)
}

// PlantedResult is one planted pair (intact digest vs a known-worse variant)
// judged in BOTH slot orders.
type PlantedResult struct {
	Pair            string // "<model>/<sabotage>"
	GoodWonForward  bool   // good in slot A, judge picked A
	GoodWonReverse  bool   // good in slot B, judge picked B
	SwapRobust      bool   // good won in BOTH orders (the trustworthy "correct")
	PositionBias    bool   // judge picked the SAME slot both times (ignored content)
	GroundedForward bool   // forward reason quotes a phrase that exists in the winning text
	GroundedReverse bool   // reverse reason quotes a phrase that exists in the winning text
	ReasonForward   string
	ReasonReverse   string
	Err             string
}

// Metrics aggregates the planted results into the trust numbers.
type Metrics struct {
	Pairs            int
	SwapRobustAcc    float64 // good wins both orders — the headline trust number
	NaiveAcc         float64 // good wins forward order only (what a naive harness would report)
	PositionBiasRate float64 // judge picked the same slot regardless of content
	GroundedRate     float64 // fraction of individual verdicts whose reason quotes the winner (anti-confabulation)
	Errors           int
}

// Trustworthy reports whether the judge discriminated merit rather than guessed:
// high swap-robust accuracy and low position bias. Thresholds match the
// guessing signature (a coin flip yields ~0.25 swap-robust, ~0.5 position bias).
func (m Metrics) Trustworthy() bool {
	return m.Pairs > 0 && m.SwapRobustAcc >= 0.9 && m.PositionBiasRate <= 0.2
}

// RunCalibration judges every (digest x sabotage) planted pair in both slot
// orders and returns the per-pair results plus aggregate metrics. When
// includeSource is set, each digest is also pitted against the raw source brief
// (the unrefined read the digest must beat).
func RunCalibration(ctx context.Context, judge Completer, r Renderer, source string, digests []Digest, sabotages []Sabotage, includeSource bool) ([]PlantedResult, Metrics) {
	var results []PlantedResult
	for _, d := range digests {
		type variant struct{ name, bad string }
		variants := make([]variant, 0, len(sabotages)+1)
		for _, s := range sabotages {
			variants = append(variants, variant{s.Name, s.Apply(d.Text)})
		}
		if includeSource {
			variants = append(variants, variant{"vs-source", source})
		}
		for _, v := range variants {
			pr := PlantedResult{Pair: d.Name + "/" + v.name}
			// Forward: good in slot A (correct pick = "A").
			vf, ef := judgeOnce(ctx, judge, r, "", source, d.Text, v.bad)
			// Reverse: good in slot B (correct pick = "B").
			vr, er := judgeOnce(ctx, judge, r, "", source, v.bad, d.Text)
			if ef != nil || er != nil {
				pr.Err = firstErr(ef, er)
				results = append(results, pr)
				continue
			}
			pr.GoodWonForward = vf.Winner == "A"
			pr.GoodWonReverse = vr.Winner == "B"
			pr.SwapRobust = pr.GoodWonForward && pr.GoodWonReverse
			pr.PositionBias = vf.Winner == vr.Winner // same slot both times
			// Grounding: did the reason quote a phrase from the text the judge
			// actually picked (slot A=good/B=bad forward; A=bad/B=good reverse)?
			pr.GroundedForward = groundedInWinner(vf.Reason, pickText(vf.Winner, d.Text, v.bad))
			pr.GroundedReverse = groundedInWinner(vr.Reason, pickText(vr.Winner, v.bad, d.Text))
			pr.ReasonForward = vf.Reason
			pr.ReasonReverse = vr.Reason
			results = append(results, pr)
		}
	}
	return results, computeMetrics(results)
}

func computeMetrics(results []PlantedResult) Metrics {
	var m Metrics
	var scored int
	for _, r := range results {
		if r.Err != "" {
			m.Errors++
			continue
		}
		scored++
		if r.SwapRobust {
			m.SwapRobustAcc++
		}
		if r.GoodWonForward {
			m.NaiveAcc++
		}
		if r.PositionBias {
			m.PositionBiasRate++
		}
		if r.GroundedForward {
			m.GroundedRate++
		}
		if r.GroundedReverse {
			m.GroundedRate++
		}
	}
	m.Pairs = scored
	if scored > 0 {
		f := float64(scored)
		m.SwapRobustAcc /= f
		m.NaiveAcc /= f
		m.PositionBiasRate /= f
		m.GroundedRate /= 2 * f // two verdicts (forward + reverse) per scored pair
	}
	return m
}

// pickText returns the text the judge selected: first when winner=="A", else second.
func pickText(winner, slotA, slotB string) string {
	if winner == "A" {
		return slotA
	}
	return slotB
}

// quoteRe captures a quoted span of >=8 chars (straight or smart quotes) from a
// judge's reason — the phrase it claims to be citing.
var quoteRe = regexp.MustCompile(`["“”']([^"“”']{8,})["“”']`)

// groundedInWinner reports whether the reason quotes a phrase that actually
// appears in the winning text — a cheap confabulation guard. A reason that
// quotes nothing (or quotes text not on the page) is ungrounded.
func groundedInWinner(reason, winnerText string) bool {
	win := normWS(strings.ToLower(winnerText))
	for _, m := range quoteRe.FindAllStringSubmatch(reason, -1) {
		q := normWS(strings.ToLower(m[1]))
		if len(q) >= 8 && strings.Contains(win, q) {
			return true
		}
	}
	return false
}

var wsRe = regexp.MustCompile(`\s+`)

func normWS(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

func firstErr(errs ...error) string {
	for _, e := range errs {
		if e != nil {
			return e.Error()
		}
	}
	return ""
}

func trunc(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 120 {
		return s
	}
	return s[:120] + "…"
}
