package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/fsutil"
	"github.com/dotcommander/distill/internal/gradecal"
	"github.com/dotcommander/distill/internal/prompts"
)

// digestTourFlags holds the resolved flags for `digest-grade tournament`.
type digestTourFlags struct {
	digests    string
	source     string
	expected   string
	checks     string
	top        int
	models     string
	all        bool
	judgeModel string
	baseURL    string
	audit      int
	out        string
	dryRun     bool
	local      bool
	deepseek   bool
}

func runTournament(cmd *runContext, f *digestTourFlags) error {
	source, err := os.ReadFile(f.source)
	if err != nil {
		return fmt.Errorf("loading source: %w", err)
	}
	out := cmd.OutOrStdout()

	var digests []gradecal.Digest
	var eliminated []extractscore.Elimination
	switch {
	case f.models != "":
		digests, err = loadDigestsOrdered(f.digests, f.models, 0)
	case f.all:
		digests, err = loadDigestsOrdered(f.digests, "", f.top)
	default:
		digests, eliminated, err = selectCandidates(f.digests, f.expected, f.checks, string(source))
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

	if f.dryRun {
		fmt.Fprintf(out, "%d candidates would be judged (seed order):\n", len(digests))
		for i, d := range digests {
			fmt.Fprintf(out, "  %2d. %s\n", i+1, d.Name)
		}
		// Merge sort does between n*ceil(log2 n)-2^ceil(log2 n)+1 and n*ceil(log2 n)
		// comparisons; report the upper bound. Each comparison = 2 calls.
		n := len(digests)
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
	judge, err := buildMeritJudgeT(f.judgeModel, f.baseURL, f.local, f.deepseek)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Ranking %d candidates by merit (merge sort, both orders, +%d-triple audit)...\n\n", len(digests), f.audit)
	res := gradecal.RunTournament(cmd.Context(), judge, p, string(source), digests, f.audit)

	printMeritTrust(out, res)

	if f.out != "" {
		note := selfPreferenceNote(res.Ranking, f.judgeModel)
		report := gradecal.RenderTournamentHTML(res, judgeLabel(f.judgeModel), note)
		if err := writeReport(f.out, report); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}

// printMeritTrust writes the rank table and trust diagnostics for a merit
// tournament result. Shared by the digest tournament and panel commands;
// the comedy command intentionally prints a divergent variant.
func printMeritTrust(out io.Writer, res gradecal.TournamentResult) {
	fmt.Fprintf(out, "%-4s %-46s %3s %3s %3s\n", "RANK", "MODEL", "W", "L", "T")
	for i, name := range res.Ranking {
		w := res.Records[name]
		if w == nil {
			w = &gradecal.WL{}
		}
		fmt.Fprintf(out, "%-4d %-46s %3d %3d %3d\n", i+1, name, w.Wins, w.Losses, w.Ties)
	}
	fmt.Fprintf(out, "\n=== TRUST ===\n")
	fmt.Fprintf(out, "Comparisons : %d (%d judge calls, %d errored)\n", res.Comparisons, res.Comparisons*2, res.Errors)
	fmt.Fprintf(out, "Flip rate   : %.1f%%  (judge disagreed across slot order; want ≤25%%)\n", 100*res.FlipRate())
	fmt.Fprintf(out, "Cycle rate  : %.1f%%  (%d triples intransitive; guessing ≈ 25%%, want ≤15%%)\n", 100*res.CycleRate(), res.CycleTriples)
	fmt.Fprintf(out, "Grounded    : %.1f%%  (edges with a verdict quoting the winner)\n", 100*res.GroundedRate())
	if res.Trustworthy() {
		fmt.Fprintf(out, "\nVERDICT: TRUSTWORTHY — coherent order, safe to read as a merit ranking.\n")
	} else {
		fmt.Fprintf(out, "\nVERDICT: SUSPECT — too many flips/cycles; treat the order as indicative only.\n")
	}
}

// selectCandidates scores every digest deterministically, composite-sorts them
// (recall - 2*overlap, matching digest-score), then applies the gate so the paid
// judge only ever sees candidates. Returns candidates in composite order plus
// the eliminations (with reasons) for reporting.
func selectCandidates(digestsDir, expectedDir, checksPath, source string) ([]gradecal.Digest, []extractscore.Elimination, error) {
	facts, err := extractscore.FlattenFacts(expectedDir)
	if err != nil {
		return nil, nil, fmt.Errorf("loading facts: %w", err)
	}
	checks, err := extractscore.LoadDigestChecks(checksPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading checks: %w", err)
	}
	matches, err := filepath.Glob(filepath.Join(digestsDir, "*", "digest.md"))
	if err != nil {
		return nil, nil, err
	}
	if len(matches) == 0 {
		return nil, nil, fmt.Errorf("no */digest.md under %s", digestsDir)
	}
	sort.Strings(matches)
	text := map[string]string{}
	var results []extractscore.DigestResult
	for _, p := range matches {
		content, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil, nil, rerr
		}
		name := filepath.Base(filepath.Dir(p))
		text[name] = string(content)
		results = append(results, extractscore.ScoreDigest(name, string(content), source, facts, checks))
	}
	const copyPenalty = 2.0 // matches digest-score's default composite
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Recall()-copyPenalty*results[i].Overlap > results[j].Recall()-copyPenalty*results[j].Overlap
	})
	cands, elim := extractscore.Gate(results, checks.Gate)
	digests := make([]gradecal.Digest, 0, len(cands))
	for _, c := range cands {
		digests = append(digests, gradecal.Digest{Name: c.Name, Text: text[c.Name]})
	}
	return digests, elim, nil
}

// loadDigestsOrdered loads digests for the tournament. With --models it returns
// them in the given seed order; otherwise alphabetical, first `top` (0 = all).
func loadDigestsOrdered(dir, models string, top int) ([]gradecal.Digest, error) {
	read := func(name string) (gradecal.Digest, error) {
		content, err := os.ReadFile(filepath.Join(dir, name, "digest.md"))
		if err != nil {
			return gradecal.Digest{}, err
		}
		return gradecal.Digest{Name: name, Text: string(content)}, nil
	}
	if strings.TrimSpace(models) != "" {
		var digests []gradecal.Digest
		for _, m := range strings.Split(models, ",") {
			if m = strings.TrimSpace(m); m == "" {
				continue
			}
			d, err := read(m)
			if err != nil {
				return nil, fmt.Errorf("reading digest %q: %w", m, err)
			}
			digests = append(digests, d)
		}
		if len(digests) == 0 {
			return nil, errors.New("no digests selected (check --models names)")
		}
		return digests, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*", "digest.md"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no */digest.md under %s", dir)
	}
	sort.Strings(matches)
	var digests []gradecal.Digest
	for _, path := range matches {
		d, err := read(filepath.Base(filepath.Dir(path)))
		if err != nil {
			return nil, err
		}
		digests = append(digests, d)
		if top > 0 && len(digests) >= top {
			break
		}
	}
	return digests, nil
}

// judgeLabel describes the judge for the report header.
func judgeLabel(judgeModel string) string {
	if m := firstNonEmpty(judgeModel, os.Getenv("DISTILL_MODEL")); m != "" {
		return m
	}
	return "the configured judge"
}

// selfPreferenceNote warns when the judge model is itself a candidate in the
// ranking (a self-preference bias risk).
func selfPreferenceNote(ranking []string, judgeModel string) string {
	jm := strings.ToLower(firstNonEmpty(judgeModel, os.Getenv("DISTILL_MODEL")))
	if jm == "" {
		return ""
	}
	slug := strings.ReplaceAll(jm, "/", "-")
	for _, name := range ranking {
		if strings.EqualFold(name, slug) {
			return "Judge model is also a candidate (" + name + ") — its own rank may be inflated by self-preference."
		}
	}
	return ""
}

// writeReport writes an HTML report via the durable atomic writer.
func writeReport(path, content string) error {
	return fsutil.WriteFile(path, []byte(content), 0o644)
}
