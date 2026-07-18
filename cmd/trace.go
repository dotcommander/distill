package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/prompts"
	"github.com/dotcommander/distill/internal/traceeval"
)

type traceFlags struct {
	fixtures      string
	models        string
	baseURL       string
	only          string
	skip          string
	out           string
	concurrency   int
	timeout       int
	taskTimeout   int
	verifyTimeout int
	local         bool
	deepseek      bool
}

func runTrace(cmd *runContext, f *traceFlags) error {
	if f.models == "" {
		return errors.New("trace-go requires --models")
	}
	verifyTimeout := time.Duration(f.verifyTimeout) * time.Second
	fx, err := traceeval.LoadFixture(cmd.Context(), f.fixtures, verifyTimeout)
	if err != nil {
		return err
	}
	fx, err = traceeval.SelectTasks(fx, splitCandidates(f.only), splitCandidates(f.skip))
	if err != nil {
		return err
	}
	roster := splitCandidates(f.models)
	if len(roster) == 0 {
		return errors.New("trace-go: --models lists no models")
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
	var outMu sync.Mutex
	var scores []traceeval.ModelScore
	var failed []string
	clientCache := newTextClientCache()
	for _, model := range roster {
		client, _, _, cerr := clientCache.Client(cfg, profile, model, f.baseURL)
		if cerr != nil {
			fmt.Fprintf(out, "FAILED  %-40s client: %v\n", model, cerr)
			failed = append(failed, model)
			continue
		}
		fmt.Fprintf(out, "Tracing %-40s (%d tasks)...\n", model, len(fx.Tasks))
		runCtx := baseCtx
		cancel := func() {}
		if f.timeout > 0 {
			runCtx, cancel = context.WithTimeout(baseCtx, time.Duration(f.timeout)*time.Second)
		}
		taskTimeout := time.Duration(f.taskTimeout) * time.Second
		ms, rerr := traceeval.RunModel(runCtx, model, greedyCompleter{client}, p, fx, traceeval.RunOptions{
			Concurrency: f.concurrency,
			PerTask:     taskTimeout,
			OnPrediction: func(pred traceeval.Prediction) {
				outMu.Lock()
				defer outMu.Unlock()
				fmt.Fprintf(out, "  %-11s %-24s %7dms", pred.Status(), pred.ID, pred.ElapsedMS)
				switch pred.Status() {
				case "wrong":
					fmt.Fprintf(out, " got=%q want=%q", clipTraceValue(pred.Pred), clipTraceValue(pred.Gold))
				case "error":
					fmt.Fprintf(out, " err=%q", clipTraceValue(pred.Error))
				}
				_, _ = fmt.Fprintln(out)
			},
		})
		cancel()
		if rerr != nil {
			if ai.IsSystemic(rerr) {
				return fmt.Errorf("ABORTING sweep: systemic failure on %s — %w (bad API key / endpoint / quota; not a per-model issue)", model, rerr)
			}
			fmt.Fprintf(out, "FAILED  %-40s %v\n", model, rerr)
			failed = append(failed, model)
			continue
		}
		scores = append(scores, ms)
	}
	if len(scores) == 0 {
		return fmt.Errorf("RUN FAILED: all %d models failed — check API key, endpoint, quota, model name, or task timeout", len(roster))
	}
	if len(failed)*2 >= len(roster) {
		return fmt.Errorf("RUN FAILED: %d/%d models failed (>=50%%) — refusing to emit a misleading report; investigate before trusting", len(failed), len(roster))
	}
	traceeval.RankByAccuracy(scores)

	fmt.Fprintf(out, "\n%-4s %-40s %8s %8s %8s %7s %8s\n", "RANK", "MODEL", "ACC", "CORRECT", "UNPARS", "SKIPPED", "MS/TASK")
	for i, s := range scores {
		perTask := 0.0
		if s.N > 0 {
			perTask = float64(s.ElapsedMS) / float64(s.N)
		}
		fmt.Fprintf(out, "%-4d %-40s %7.1f%% %5d/%-2d %8d %7d %8.0f\n", i+1, s.Model, 100*s.Accuracy, s.Correct, s.N, s.Unparseable, s.Skipped, perTask)
	}
	if len(failed) > 0 {
		fmt.Fprintf(out, "\nFailed/skipped (%d): %s\n", len(failed), strings.Join(failed, ", "))
	}
	if f.out != "" {
		if werr := writeReport(f.out, traceeval.RenderHTML(scores)); werr != nil {
			return werr
		}
		fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}

func clipTraceValue(s string) string {
	s = strings.TrimSpace(s)
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// greedyCompleter preserves the trace-go contract: exact-fidelity tasks use
// temperature 0 without changing the repository-wide ai.Client default.
type greedyCompleter struct {
	client *ai.Client
}

func (c greedyCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	return c.client.CompleteWithTemperature(ctx, prompt, 0)
}
