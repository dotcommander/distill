package labelscore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/ai"

	"golang.org/x/sync/errgroup"
)

// Completer is the model backend — same shape as ai.Client.Complete, declared
// here so callers can inject a fake in tests.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Renderer builds the per-item task prompt (satisfied by prompts.Set).
type Renderer interface {
	RenderLabel(task, allowed, text string) string
}

// RunOptions bounds the run. Concurrency caps parallel item calls (<1 -> 1).
type RunOptions struct {
	Concurrency int
}

// RunModel scores one model over the fixture: for each item it renders the task
// prompt (constrained to the allowed set), calls the model, parses a single
// label, and scores. Every call uses ctx. Item order is preserved. An empty
// model answer (ai.ErrEmptyResponse) is a recorded miss; any other error fails
// the run fast.
func RunModel(ctx context.Context, model string, c Completer, r Renderer, fx LabelFixture, opts RunOptions) (ModelScore, error) {
	allowed := fx.NormalizedAllowed()
	allowedCSV := joinAllowed(fx.Allowed)
	preds := make([]Prediction, len(fx.Items))

	limit := opts.Concurrency
	if limit < 1 {
		limit = 1
	}
	start := time.Now()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, it := range fx.Items {
		g.Go(func() error {
			prompt := r.RenderLabel(fx.Task, allowedCSV, it.Text)
			raw, err := c.Complete(gctx, prompt)
			if err != nil {
				if errors.Is(err, ai.ErrEmptyResponse) {
					preds[i] = Prediction{ID: it.ID, Gold: Normalize(it.GoldLabel)}
					return nil
				}
				return fmt.Errorf("labelscore: model %s item %s: %w", model, it.ID, err)
			}
			lbl, ok := ParseLabel(raw, allowed)
			preds[i] = Prediction{ID: it.ID, Gold: Normalize(it.GoldLabel), Pred: lbl, InVocab: ok}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return ModelScore{}, err
	}
	ms := Score(model, preds, allowed)
	ms.ElapsedMS = time.Since(start).Milliseconds()
	if fx.Cost != nil {
		ms.CostPerMTok = fx.Cost[model]
	}
	return ms, nil
}

// joinAllowed renders the allowed set as a sorted, comma-joined string for the
// prompt's {{ALLOWED}} slot.
func joinAllowed(allowed []string) string {
	cp := append([]string(nil), allowed...)
	sort.Strings(cp)
	return strings.Join(cp, ", ")
}
