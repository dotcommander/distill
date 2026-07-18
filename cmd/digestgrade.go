package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/gradecal"
	"github.com/dotcommander/distill/internal/prompts"
)

// digestGradeFlags holds the resolved flags for `digest-grade calibrate`.
type digestGradeFlags struct {
	digests       string
	source        string
	limit         int
	models        string
	judgeModel    string
	baseURL       string
	includeSource bool
	local         bool
	deepseek      bool
}

func runCalibrate(cmd *runContext, f *digestGradeFlags) error {
	source, err := os.ReadFile(f.source)
	if err != nil {
		return fmt.Errorf("loading source: %w", err)
	}
	digests, err := loadDigests(f.digests, f.models, f.limit)
	if err != nil {
		return err
	}
	p, err := prompts.Load()
	if err != nil {
		return err
	}
	judge, err := buildMeritJudge(f)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Calibrating judge on %d digests x %d sabotages, both orders = %d matchups...\n\n",
		len(digests), len(gradecal.Sabotages()), len(digests)*len(gradecal.Sabotages())*2)

	results, m := gradecal.RunCalibration(cmd.Context(), judge, p, string(source), digests, gradecal.Sabotages(), f.includeSource)

	fmt.Fprintf(out, "%-34s %-9s %-9s %s\n", "PLANTED PAIR (intact vs …)", "FORWARD", "REVERSE", "VERDICT")
	for _, r := range results {
		if r.Err != "" {
			fmt.Fprintf(out, "%-34s ERROR: %s\n", r.Pair, r.Err)
			continue
		}
		verdict := "guess"
		switch {
		case r.SwapRobust:
			verdict = "✓ intact won both"
		case r.PositionBias:
			verdict = "✗ position bias (same slot)"
		}
		fmt.Fprintf(out, "%-34s %-9s %-9s %s\n", r.Pair, won(r.GoodWonForward), won(r.GoodWonReverse), verdict)
	}

	fmt.Fprintf(out, "\n=== RELIABILITY ===\n")
	fmt.Fprintf(out, "Planted pairs scored : %d (%d errored)\n", m.Pairs, m.Errors)
	fmt.Fprintf(out, "Swap-robust accuracy : %.1f%%   (intact wins in BOTH orders — the trustworthy number; guessing ≈ 25%%)\n", 100*m.SwapRobustAcc)
	fmt.Fprintf(out, "Naive accuracy       : %.1f%%   (forward order only — what a one-sided harness would over-report)\n", 100*m.NaiveAcc)
	fmt.Fprintf(out, "Position-bias rate   : %.1f%%   (same slot regardless of content; guessing ≈ 50%%, want ≤20%%)\n", 100*m.PositionBiasRate)
	fmt.Fprintf(out, "Grounded-reason rate : %.1f%%   (verdict quotes a phrase that exists in the winner; low = confabulating)\n", 100*m.GroundedRate)
	if m.Trustworthy() {
		fmt.Fprintf(out, "\nVERDICT: TRUSTWORTHY — the judge reads merit; safe to run the real tournament.\n")
	} else {
		fmt.Fprintf(out, "\nVERDICT: SUSPECT — judge does not reliably beat sabotage. Do NOT trust a merit ranking from it yet.\n")
	}
	return nil
}

func won(b bool) string {
	if b {
		return "intact"
	}
	return "bad"
}

// loadDigests globs <dir>/*/digest.md and selects either the named models or the
// first `limit` (alphabetical; 0 = all).
func loadDigests(dir, models string, limit int) ([]gradecal.Digest, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*", "digest.md"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no */digest.md under %s", dir)
	}
	sort.Strings(matches)

	var want map[string]bool
	if strings.TrimSpace(models) != "" {
		want = map[string]bool{}
		for _, m := range strings.Split(models, ",") {
			if m = strings.TrimSpace(m); m != "" {
				want[m] = true
			}
		}
	}

	var digests []gradecal.Digest
	for _, path := range matches {
		name := filepath.Base(filepath.Dir(path))
		if want != nil && !want[name] {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		digests = append(digests, gradecal.Digest{Name: name, Text: string(content)})
		if want == nil && limit > 0 && len(digests) >= limit {
			break
		}
	}
	if len(digests) == 0 {
		return nil, errors.New("no digests selected (check --models names)")
	}
	return digests, nil
}

// buildMeritJudge resolves the judge model/endpoint for the calibrate command.
func buildMeritJudge(f *digestGradeFlags) (gradecal.Completer, error) {
	return buildMeritJudgeT(f.judgeModel, f.baseURL, f.local, f.deepseek)
}

// buildMeritJudgeT resolves the judge model/endpoint (flag -> env -> config ->
// default) and constructs the wormhole-backed client.
func buildMeritJudgeT(judgeModel, baseURLFlag string, local, deepseek bool) (gradecal.Completer, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	profile, err := profileFromFlags(local, deepseek)
	if err != nil {
		return nil, err
	}
	model := firstNonEmpty(judgeModel, os.Getenv("DISTILL_MODEL"), cfg.EffectiveMeritJudgeProfile(profile))
	if model == "" {
		return nil, errors.New("a judge model is required (--judge-model, $DISTILL_MODEL, or config)")
	}
	resolved, err := endpointForTextModel(cfg, profile, model, baseURLFlag)
	if err != nil {
		return nil, err
	}
	client, err := ai.New(ai.Config{
		Provider:  resolved.provider,
		BaseURL:   resolved.baseURL,
		APIKey:    ai.APIKeyForProvider(resolved.provider),
		TextModel: resolved.model,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ai client: %w", err)
	}
	return client, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
