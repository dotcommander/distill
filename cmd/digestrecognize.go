package cmd

import (
	"fmt"
	"strings"

	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/gradecal"
	"github.com/dotcommander/distill/internal/prompts"
)

// digestRecognizeFlags holds the resolved flags for `digest-grade recognize`.
type digestRecognizeFlags struct {
	digests  string
	judges   string
	trials   int
	setSize  int
	seed     int64
	baseURL  string
	out      string
	dryRun   bool
	deepseek bool
}

func runRecognize(cmd *runContext, f *digestRecognizeFlags) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	f.judges = resolveJudges(f.judges, cfg.DeepSeekModel, cfg.RecognizeJudges, cmd.FlagChanged("judges"), f.deepseek)

	field, err := loadDigestsOrdered(f.digests, "", 0)
	if err != nil {
		return err
	}
	judgeIDs := parseJudgeIDs(f.judges)
	judgeNames := make([]string, 0, len(judgeIDs))
	for _, id := range judgeIDs {
		judgeNames = append(judgeNames, strings.ReplaceAll(id, "/", "-"))
	}

	out := cmd.OutOrStdout()
	if f.dryRun {
		calls := len(judgeIDs) * f.trials
		fmt.Fprintf(out, "%d judges x %d trials = %d calls ~= $%.2f\n", len(judgeIDs), f.trials, calls, float64(calls)*0.03)
		_, _ = fmt.Fprintln(out, "Matching digests:")
		for i, slug := range judgeNames {
			status := "missing"
			if hasDigest(field, slug) {
				status = "found"
			}
			fmt.Fprintf(out, "  - %-46s %s (%s)\n", slug, status, judgeIDs[i])
		}
		return nil
	}

	p, err := prompts.Load()
	if err != nil {
		return err
	}
	judges := make(map[string]gradecal.Completer, len(judgeIDs))
	for i, id := range judgeIDs {
		judge, err := buildMeritJudgeT(id, f.baseURL, false, f.deepseek)
		if err != nil {
			return err
		}
		judges[judgeNames[i]] = judge
	}

	results := gradecal.RunRecognition(cmd.Context(), judges, p, field, judgeNames, f.trials, f.setSize, f.seed)

	fmt.Fprintf(out, "%-46s %9s %9s %8s %s\n", "MODEL", "HITS", "ACCURACY", "CHANCE", "VERDICT")
	for _, r := range results {
		fmt.Fprintf(out, "%-46s %3d/%-5d %8.1f%% %7.1f%% %s\n", r.Model, r.Hits, r.Trials, 100*r.Accuracy(), 100*r.Chance(), r.Verdict())
	}

	if f.out != "" {
		report := gradecal.RenderRecognitionHTML(results, f.seed, f.setSize)
		if err := writeReport(f.out, report); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}

func parseJudgeIDs(raw string) []string {
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func hasDigest(field []gradecal.Digest, name string) bool {
	for _, d := range field {
		if d.Name == name {
			return true
		}
	}
	return false
}
