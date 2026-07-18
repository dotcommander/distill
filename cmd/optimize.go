package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/fsutil"
	"github.com/dotcommander/distill/internal/promptopt"
	"github.com/dotcommander/distill/internal/prompts"

	"golang.org/x/sync/errgroup"
)

type optimizeFlags struct {
	chunks       string
	expected     string
	seed         string
	out          string
	iterations   int
	budgetCalls  int
	concurrency  int
	model        string
	mutatorModel string
	holdout      string
}

func runOptimize(ctx context.Context, deps *Deps, f optimizeFlags) error {
	_ = deps
	if strings.TrimSpace(f.out) == "" {
		return errors.New("--out is required")
	}
	if f.iterations < 0 {
		return errors.New("--iterations must be >= 0")
	}
	if f.concurrency < 1 {
		f.concurrency = 1
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	p, err := prompts.Load()
	if err != nil {
		return err
	}
	seedPrompt := p.Research
	if f.seed != "" {
		data, rerr := os.ReadFile(f.seed)
		if rerr != nil {
			return fmt.Errorf("reading seed prompt: %w", rerr)
		}
		seedPrompt = string(data)
	}
	if cerr := promptopt.ValidatePrompt(seedPrompt); cerr != nil {
		return fmt.Errorf("seed prompt: %w", cerr)
	}
	if err2 := os.MkdirAll(f.out, 0o750); err2 != nil {
		return fmt.Errorf("creating output dir: %w", err2)
	}
	model := firstNonEmpty(f.model, os.Getenv("DISTILL_MODEL"), cfg.Model)
	if model == "" {
		return errors.New("optimize requires --model, $DISTILL_MODEL, or config model")
	}
	mutatorModel := firstNonEmpty(f.mutatorModel, model)
	extractor, err := buildOptimizeClient(cfg, model)
	if err != nil {
		return err
	}
	mutator, err := buildOptimizeClient(cfg, mutatorModel)
	if err != nil {
		return err
	}
	var calls atomic.Int64
	logPath := filepath.Join(f.out, "score-log.jsonl")
	bestPrompt := seedPrompt
	bestScore, err := evaluatePrompt(ctx, extractor, bestPrompt, f, &calls)
	if err != nil {
		return err
	}
	if err := promptopt.AppendLog(logPath, promptopt.LogRecord{Iter: 0, PromptSHA256: promptopt.HashPrompt(bestPrompt), Operator: "seed", Recall: bestScore.Recall(), CallsUsed: int(calls.Load()), Accepted: true}); err != nil {
		return err
	}
	operators := []string{"rewrite-instructions", "add-directive-from-missing-anchors", "fewshot-swap", "compress"}
	for i := 1; i <= f.iterations; i++ {
		if f.budgetCalls > 0 && int(calls.Load()) >= f.budgetCalls {
			break
		}
		op := operators[(i-1)%len(operators)]
		report := scoreReport(bestScore)
		mutated, merr := mutator.Complete(ctx, p.RenderOptimizeMutate(op, bestPrompt, report))
		calls.Add(1)
		if merr != nil {
			return fmt.Errorf("mutate iteration %d: %w", i, merr)
		}
		mutated = strings.TrimSpace(mutated)
		if err := promptopt.ValidatePrompt(mutated); err != nil {
			if werr := promptopt.AppendLog(logPath, promptopt.LogRecord{Iter: i, PromptSHA256: promptopt.HashPrompt(mutated), Operator: op, Recall: bestScore.Recall(), CallsUsed: int(calls.Load()), Accepted: false}); werr != nil {
				return werr
			}
			continue
		}
		score, err := evaluatePrompt(ctx, extractor, mutated, f, &calls)
		if err != nil {
			return err
		}
		holdoutRecall := 0.0
		if f.holdout != "" {
			holdout, herr := extractscore.ScoreRun(f.holdout, candidateResponsesDir(f.out, mutated))
			if herr == nil {
				holdoutRecall = holdout.Recall()
			}
		}
		accepted := promptopt.ShouldAccept(score.Recall(), mutated, bestScore.Recall(), bestPrompt)
		if accepted {
			bestPrompt = mutated
			bestScore = score
		}
		if err := promptopt.AppendLog(logPath, promptopt.LogRecord{Iter: i, PromptSHA256: promptopt.HashPrompt(mutated), Operator: op, Recall: score.Recall(), HoldoutRecall: holdoutRecall, CallsUsed: int(calls.Load()), Accepted: accepted}); err != nil {
			return err
		}
	}
	if err := fsutil.WriteFile(filepath.Join(f.out, "best-prompt.md"), []byte(bestPrompt), 0o644); err != nil {
		return fmt.Errorf("writing best prompt: %w", err)
	}
	results := fmt.Sprintf("best_recall: %.4f\ncalls_used: %d\nbest_prompt: best-prompt.md\ninstall: review best-prompt.md, then copy it into your configured research prompt if desired.\n", bestScore.Recall(), calls.Load())
	return fsutil.WriteFile(filepath.Join(f.out, "RESULTS.md"), []byte(results), 0o644)
}

func buildOptimizeClient(cfg *config.Config, model string) (*ai.Client, error) {
	resolved, err := endpointForTextModel(cfg, config.ProfileRemote, model, "")
	if err != nil {
		return nil, err
	}
	return ai.New(ai.Config{
		Provider:  resolved.provider,
		BaseURL:   resolved.baseURL,
		APIKey:    ai.APIKeyForProvider(resolved.provider),
		TextModel: resolved.model,
	})
}

func evaluatePrompt(ctx context.Context, client *ai.Client, prompt string, f optimizeFlags, calls *atomic.Int64) (extractscore.RunResult, error) {
	respDir := candidateResponsesDir(f.out, prompt)
	if err := os.MkdirAll(respDir, 0o750); err != nil {
		return extractscore.RunResult{}, fmt.Errorf("creating responses dir: %w", err)
	}
	chunks, err := filepath.Glob(filepath.Join(f.chunks, "chunk-*.md"))
	if err != nil {
		return extractscore.RunResult{}, err
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(f.concurrency)
	for _, chunkPath := range chunks {
		g.Go(func() error {
			id := strings.TrimSuffix(filepath.Base(chunkPath), filepath.Ext(chunkPath))
			outPath := filepath.Join(respDir, id+".md")
			if _, err := os.Stat(outPath); err == nil {
				return nil
			}
			data, err := os.ReadFile(chunkPath)
			if err != nil {
				return fmt.Errorf("reading chunk %s: %w", chunkPath, err)
			}
			if f.budgetCalls > 0 && int(calls.Load()) >= f.budgetCalls {
				return nil
			}
			rendered := (&prompts.Set{Research: prompt}).RenderResearch(id, string(data))
			out, err := client.Complete(gctx, rendered)
			calls.Add(1)
			if err != nil {
				return fmt.Errorf("extract %s: %w", id, err)
			}
			return fsutil.WriteFile(outPath, []byte(strings.TrimSpace(out)), 0o644)
		})
	}
	if err := g.Wait(); err != nil {
		return extractscore.RunResult{}, err
	}
	return extractscore.ScoreRun(f.expected, respDir)
}

func candidateResponsesDir(out, prompt string) string {
	return filepath.Join(out, "candidates", promptopt.HashPrompt(prompt), "responses")
}

func scoreReport(run extractscore.RunResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "recall=%.4f covered=%d total=%d\n", run.Recall(), run.Covered, run.Total)
	for _, c := range run.Chunks {
		if len(c.Missing) > 0 {
			fmt.Fprintf(&b, "%s missing: %s\n", c.Chunk, strings.Join(c.Missing, ", "))
		}
	}
	return b.String()
}
