package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/fsutil"
)

// digestScoreFlags holds the resolved --digest-score flag values for one
// invocation, closure-scoped in newDigestScoreCmd (no package globals).
type digestScoreFlags struct {
	expected    string
	checks      string
	source      string
	digests     string
	out         string
	copyPenalty float64
}

func runDigestScore(cmd *runContext, f *digestScoreFlags) error {
	facts, err := extractscore.FlattenFacts(f.expected)
	if err != nil {
		return fmt.Errorf("loading facts: %w", err)
	}
	checks, err := extractscore.LoadDigestChecks(f.checks)
	if err != nil {
		return fmt.Errorf("loading checks: %w", err)
	}
	source, err := os.ReadFile(f.source)
	if err != nil {
		return fmt.Errorf("loading source: %w", err)
	}
	matches, err := filepath.Glob(filepath.Join(f.digests, "*", "digest.md"))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("no */digest.md found under %s", f.digests)
	}
	sort.Strings(matches)

	var results []extractscore.DigestResult
	for _, p := range matches {
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		name := filepath.Base(filepath.Dir(p))
		results = append(results, extractscore.ScoreDigest(name, string(content), string(source), facts, checks))
	}

	// Rank by composite (recall minus copy penalty) so a digest that earns high
	// recall by lifting source text verbatim loses to one that genuinely rewrote
	// it. Ties break on tensions kept, then fewest hygiene flags.
	composite := func(r extractscore.DigestResult) float64 { return r.Recall() - f.copyPenalty*r.Overlap }
	sort.SliceStable(results, func(i, j int) bool {
		ci, cj := composite(results[i]), composite(results[j])
		if ci != cj {
			return ci > cj
		}
		if results[i].TensionsKept != results[j].TensionsKept {
			return results[i].TensionsKept > results[j].TensionsKept
		}
		fi := len(results[i].Preamble) + len(results[i].Artifacts)
		fj := len(results[j].Preamble) + len(results[j].Artifacts)
		return fi < fj
	})

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%-48s %6s  %9s  %5s  %6s  %5s  %s\n", "MODEL", "WORDS", "RECALL", "TENS", "COPY", "SCORE", "FLAGS")
	for _, r := range results {
		var flags string
		if len(r.Preamble) > 0 {
			flags += "preamble "
		}
		if len(r.Artifacts) > 0 {
			flags += "artifact "
		}
		if !r.WordBandOK {
			flags += "wordband "
		}
		fmt.Fprintf(out, "%-48s %6d  %3d/%-3d %3.0f%%  %5.1f%%  %5.3f  %s\n",
			r.Name, r.Words, r.Covered, r.Total, 100*r.Recall(), 100*r.Overlap, composite(r), flags)
	}

	if f.out != "" {
		report := extractscore.RenderDigestHTML(results, f.copyPenalty)
		if err := fsutil.WriteFile(f.out, []byte(report), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}
