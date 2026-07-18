package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/labelscore"
	"github.com/dotcommander/distill/internal/prompts"
)

// labelFlags holds the resolved flags for `label`.
type labelFlags struct {
	task        string
	fixtures    string
	models      string
	baseURL     string
	out         string
	concurrency int
	timeout     int
	seed        int
	local       bool
	deepseek    bool
}

func runLabel(cmd *runContext, f *labelFlags) error {
	if f.task == "" || f.fixtures == "" || f.models == "" {
		return errors.New("label requires --task, --fixtures, and --models")
	}
	if f.task != "classification" && f.task != "sentiment" {
		return fmt.Errorf("label: --task must be classification or sentiment, got %q", f.task)
	}
	fx, err := labelscore.LoadFixture(f.fixtures)
	if err != nil {
		return err
	}
	if fx.Task != f.task {
		return fmt.Errorf("label: --task %q does not match fixture task %q", f.task, fx.Task)
	}
	roster := splitCandidates(f.models)
	if len(roster) == 0 {
		return errors.New("label: --models lists no models")
	}

	p, err := prompts.Load()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	profile, err := profileFromFlags(f.local, f.deepseek)
	if err != nil {
		return err
	}
	baseCtx := cmd.Context()

	out := cmd.OutOrStdout()
	var scores []labelscore.ModelScore
	var failed []string
	clientCache := newTextClientCache()
	for _, model := range roster {
		client, _, _, cerr := clientCache.Client(cfg, profile, model, f.baseURL)
		if cerr != nil {
			fmt.Fprintf(out, "FAILED  %-40s client: %v\n", model, cerr)
			failed = append(failed, model)
			continue
		}
		fmt.Fprintf(out, "Scoring %-40s (%d items)...\n", model, len(fx.Items))
		// Per-model timeout: each model gets its OWN budget, so one slow model
		// can't starve the deadline for the rest of the roster.
		runCtx := baseCtx
		cancel := func() {}
		if f.timeout > 0 {
			runCtx, cancel = context.WithTimeout(baseCtx, time.Duration(f.timeout)*time.Second)
		}
		ms, rerr := labelscore.RunModel(runCtx, model, client, p, fx, labelscore.RunOptions{Concurrency: f.concurrency})
		cancel()
		if rerr != nil {
			if ai.IsSystemic(rerr) {
				return fmt.Errorf("ABORTING sweep: systemic failure on %s — %w (bad API key / endpoint / quota; not a per-model issue)", model, rerr)
			}
			// One model's timeout/transport error must not abort the sweep;
			// record it and keep scoring the rest (mirrors the digest
			// pipeline recording per-chunk failures instead of aborting).
			fmt.Fprintf(out, "FAILED  %-40s %v\n", model, rerr)
			failed = append(failed, model)
			continue
		}
		scores = append(scores, ms)
	}
	if len(scores) == 0 {
		return fmt.Errorf("RUN FAILED: all %d models failed — check API key / endpoint / quota", len(roster))
	}
	if len(failed)*2 >= len(roster) {
		return fmt.Errorf("RUN FAILED: %d/%d models failed (>=50%%) — refusing to emit a misleading report; investigate before trusting", len(failed), len(roster))
	}
	labelscore.RankByMacroF1(scores)

	fmt.Fprintf(out, "\n%-4s %-40s %8s %8s %8s %6s %8s\n", "RANK", "MODEL", "MACRO-F1", "ACC", "OOT", "UNPARS", "MS/ITEM")
	for i, s := range scores {
		perItem := 0.0
		if s.N > 0 {
			perItem = float64(s.ElapsedMS) / float64(s.N)
		}
		fmt.Fprintf(out, "%-4d %-40s %8.3f %7.1f%% %8d %6d %8.0f\n", i+1, s.Model, s.MacroF1, 100*s.Accuracy, s.OutOfVocab, s.Unparseable, perItem)
	}

	if len(failed) > 0 {
		fmt.Fprintf(out, "\nFailed/skipped (%d): %s\n", len(failed), strings.Join(failed, ", "))
	}

	if f.out != "" {
		report := labelscore.RenderHTML(fx.Task, fx.Allowed, scores)
		if werr := writeReport(f.out, report); werr != nil {
			return werr
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}
