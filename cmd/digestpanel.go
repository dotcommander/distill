package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/gradecal"
	"github.com/dotcommander/distill/internal/prompts"
)

const (
	defaultPanelJudges     = "anthropic/claude-opus-4.8,openai/gpt-5.5,deepseek/deepseek-v4-pro,z-ai/glm-5.2"
	defaultRecognizeJudges = "anthropic/claude-opus-4.8,openai/gpt-5.5"
)

// digestPanelFlags holds the resolved flags for `digest-grade panel`.
type digestPanelFlags struct {
	digests   string
	source    string
	expected  string
	checks    string
	models    string
	criterion string
	all       bool
	dryRun    bool
	seed      int64
	baseURL   string
	out       string
	audit     int
	judges    string
	top       int
	local     bool
	deepseek  bool
}

func runPanel(deps *Deps, cmd *runContext, f *digestPanelFlags) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	f.judges = resolveJudges(f.judges, cfg.DeepSeekModel, cfg.PanelJudges, cmd.FlagChanged("judges"), f.deepseek)

	source, err := os.ReadFile(f.source)
	if err != nil {
		return fmt.Errorf("loading source: %w", err)
	}
	out := cmd.OutOrStdout()

	var cands []gradecal.Digest
	var eliminated []extractscore.Elimination
	switch {
	case f.models != "":
		cands, err = loadDigestsOrdered(f.digests, f.models, 0)
	case f.all:
		cands, err = loadDigestsOrdered(f.digests, "", f.top)
	default:
		cands, eliminated, err = selectCandidates(f.digests, f.expected, f.checks, string(source))
	}
	if err != nil {
		return err
	}
	if len(eliminated) > 0 {
		fmt.Fprintf(out, "Candidate gate eliminated %d non-candidates (never judged):\n", len(eliminated))
		for _, e := range eliminated {
			fmt.Fprintf(out, "  - %-46s %s\n", e.Name, strings.Join(e.Reasons, "; "))
		}
		_, _ = fmt.Fprintln(out)
	}

	panelDigests := make([]gradecal.Digest, len(cands))
	for i, d := range cands {
		panelDigests[i] = gradecal.Digest{Name: d.Name, Text: gradecal.Canonicalize(d.Text)}
	}

	judgeIDs := parseJudgeIDs(f.judges)
	judgeOrder := make([]string, 0, len(judgeIDs))
	for _, id := range judgeIDs {
		judgeOrder = append(judgeOrder, strings.ReplaceAll(id, "/", "-"))
	}
	if len(judgeOrder) == 0 {
		return errors.New("at least one judge is required")
	}

	if f.dryRun {
		_, _ = fmt.Fprintln(out, "Panel judges:")
		for i, slug := range judgeOrder {
			fmt.Fprintf(out, "  - %s ← %s\n", slug, judgeIDs[i])
		}
		fmt.Fprintf(out, "\n%d candidates would be judged (seed order):\n", len(cands))
		for i, d := range cands {
			fmt.Fprintf(out, "  %2d. %s\n", i+1, d.Name)
		}
		// Merge sort does between n*ceil(log2 n)-2^ceil(log2 n)+1 and n*ceil(log2 n)
		// comparisons; report the upper bound. Each comparison = 2 calls.
		n := len(cands)
		bits := 0
		for (1 << bits) < n {
			bits++
		}
		maxCmp := n * bits
		fmt.Fprintf(out, "\nEstimated ≤%d comparisons + %d audit triples ≈ ≤%d judge calls (~$%.2f–$%.2f at 2–5¢).\n",
			maxCmp, f.audit, 2*(maxCmp+f.audit), 0.02*float64(2*(maxCmp+f.audit)), 0.05*float64(2*(maxCmp+f.audit)))
		return nil
	}

	p, err := prompts.Load()
	if err != nil {
		return err
	}
	panel := make(map[string]gradecal.Completer, len(judgeIDs))
	for i, id := range judgeIDs {
		judge, err := buildMeritJudgeT(id, f.baseURL, f.local, f.deepseek)
		if err != nil {
			return err
		}
		panel[judgeOrder[i]] = judge
	}

	fmt.Fprintf(out, "Ranking %d candidates by merit (de-biased panel, merge sort, both orders, +%d-triple audit)...\n\n", len(panelDigests), f.audit)
	var res gradecal.TournamentResult
	switch f.criterion {
	case "comedy":
		res = gradecal.RunComedyPanelTournament(cmd.Context(), panel, judgeOrder, p, string(source), panelDigests, f.audit)
	case "publish":
		res = gradecal.RunPublishPanelTournament(cmd.Context(), panel, judgeOrder, p, string(source), panelDigests, f.audit)
	default:
		res = gradecal.RunPanelTournament(cmd.Context(), panel, judgeOrder, p, string(source), panelDigests, f.audit)
	}

	printMeritTrust(out, res)

	fmt.Fprintf(out, "\n=== JUDGE PARTICIPATION ===\n")
	seen := map[string]bool{}
	for _, name := range judgeOrder {
		seen[name] = true
		fmt.Fprintf(out, "%-46s %3d comparisons\n", name, res.JudgeCounts[name])
	}
	var extras []string
	for name := range res.JudgeCounts {
		if !seen[name] {
			extras = append(extras, name)
		}
	}
	sort.Strings(extras)
	for _, name := range extras {
		fmt.Fprintf(out, "%-46s %3d comparisons\n", name, res.JudgeCounts[name])
	}

	if f.out != "" {
		report := gradecal.RenderPanelHTML(res, judgeOrder, fmt.Sprintf("de-biased panel run (%s criterion)", f.criterion))
		if err := writeReport(f.out, report); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}
