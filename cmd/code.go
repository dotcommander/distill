package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/codeeval"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/prompts"

	"golang.org/x/sync/errgroup"
)

type codeFlags struct {
	problems    string
	models      string
	baseURL     string
	out         string
	concurrency int
	timeout     int
	seed        int64
	local       bool
	deepseek    bool
	allowExec   bool
}

func runCode(cmd *runContext, f *codeFlags) error {
	if f.models == "" {
		return errors.New("code requires --models")
	}
	if !f.allowExec {
		return errors.New("code: this command COMPILES AND RUNS model-generated Go on your machine (arbitrary code execution). Re-run with --i-understand-code-execution to proceed. The default runner is temp-dir + offline + timeout + static-scan — defense-in-depth, NOT a sandbox")
	}
	ps, err := codeeval.LoadProblems(f.problems)
	if err != nil {
		return err
	}
	roster := splitCandidates(f.models)
	if len(roster) == 0 {
		return errors.New("code: --models lists no models")
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

	type res struct {
		ms    codeeval.ModelScore
		ok    bool
		model string
		err   error
	}
	results := make([]res, len(roster))
	limit := f.concurrency
	if limit < 1 {
		limit = 1
	}
	_, _ = fmt.Fprintf(out, "Evaluating %d models x %d problems (concurrency %d)...\n", len(roster), len(ps.Problems), limit)
	clientCache := newTextClientCache()
	g := new(errgroup.Group)
	g.SetLimit(limit)
	for i, model := range roster {
		g.Go(func() error {
			client, _, _, cerr := clientCache.Client(cfg, profile, model, f.baseURL)
			if cerr != nil {
				results[i] = res{model: model, err: cerr}
				//nolint:nilerr // intentional: per-model failure recorded, run continues
				return nil
			}
			ms, rerr := codeeval.RunModel(baseCtx, model, client, p, codeeval.LocalRunner{}, ps, codeeval.RunOptions{Concurrency: f.concurrency, PerProblem: time.Duration(f.timeout) * time.Second})
			if rerr != nil {
				results[i] = res{model: model, err: rerr}
				//nolint:nilerr // intentional: per-model failure recorded, run continues
				return nil
			}
			results[i] = res{ms: ms, ok: true, model: model}
			return nil
		})
	}
	_ = g.Wait()

	var scores []codeeval.ModelScore
	var failed []string
	for _, r := range results {
		if r.ok {
			scores = append(scores, r.ms)
			continue
		}
		if ai.IsSystemic(r.err) {
			return fmt.Errorf("ABORTING sweep: systemic failure on %s — %w (bad API key / endpoint / quota; not a per-model issue)", r.model, r.err)
		}
		_, _ = fmt.Fprintf(out, "FAILED  %-40s %v\n", r.model, r.err)
		failed = append(failed, r.model)
	}
	if len(scores) == 0 {
		return errors.New("code: no models produced a score")
	}
	if len(failed)*2 >= len(roster) {
		return fmt.Errorf("RUN FAILED: %d/%d models failed (>=50%%) — refusing to emit a misleading report; investigate before trusting", len(failed), len(roster))
	}
	codeeval.RankByPassRate(scores)

	_, _ = fmt.Fprintf(out, "\n%-4s %-40s %8s %8s %6s %7s %9s\n", "RANK", "MODEL", "PASS%", "SOLVED", "CFAIL", "BLOCKED", "MS/PROB")
	for i, s := range scores {
		perProblem := 0.0
		if s.N > 0 {
			perProblem = float64(s.ElapsedMS) / float64(s.N)
		}
		_, _ = fmt.Fprintf(out, "%-4d %-40s %7.1f%% %5d/%-2d %6d %7d %9.0f\n", i+1, s.Model, 100*s.PassRate, s.Solved, s.N, s.CompileFails, s.Blocked, perProblem)
	}
	if len(failed) > 0 {
		_, _ = fmt.Fprintf(out, "\nFailed/skipped (%d): %s\n", len(failed), strings.Join(failed, ", "))
	}
	if f.out != "" {
		if werr := writeReport(f.out, codeeval.RenderHTML(scores)); werr != nil {
			return werr
		}
		_, _ = fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}
