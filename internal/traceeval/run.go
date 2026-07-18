package traceeval

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/dotcommander/distill/internal/ai"

	"golang.org/x/sync/errgroup"
)

// Completer is the model backend, satisfied by *ai.Client.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Renderer builds the trace prompt, satisfied by *prompts.Set.
type Renderer interface {
	RenderTraceGo(program string) string
}

// RunOptions bounds per-task model calls.
type RunOptions struct {
	Concurrency int
	PerTask     time.Duration
	// OnPrediction is called from worker goroutines as tasks complete.
	OnPrediction func(Prediction)
}

type Prediction struct {
	ID          string
	Category    string
	Gold        string
	Pred        string
	Correct     bool
	Unparseable bool
	Skipped     bool
	Error       string
	ElapsedMS   int64
}

func (p Prediction) Status() string {
	switch {
	case p.Correct:
		return "ok"
	case p.Unparseable:
		return "unparseable"
	case p.Skipped && p.Error != "":
		return "error"
	case p.Skipped:
		return "skipped"
	default:
		return "wrong"
	}
}

type ModelScore struct {
	Model       string
	Accuracy    float64
	Correct     int
	Unparseable int
	Skipped     int
	N           int
	CostPerMTok float64
	ElapsedMS   int64
	Predictions []Prediction
}

// RunModel scores one model over the fixture by exact match against generated
// gold. Per-task model errors are recorded as misses so one slow trace does not
// hide the rest of the model's outcomes.
func RunModel(ctx context.Context, model string, c Completer, r Renderer, fx Fixture, opts RunOptions) (ModelScore, error) {
	preds := make([]Prediction, len(fx.Tasks))
	limit := opts.Concurrency
	if limit < 1 {
		limit = 1
	}
	start := time.Now()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, task := range fx.Tasks {
		g.Go(func() error {
			taskStart := time.Now()
			pred := Prediction{ID: task.ID, Category: task.Category, Gold: task.Gold}
			taskCtx := gctx
			cancel := func() {}
			if opts.PerTask > 0 {
				taskCtx, cancel = context.WithTimeout(gctx, opts.PerTask)
			}
			defer cancel()

			raw, err := c.Complete(taskCtx, r.RenderTraceGo(task.Program))
			if err != nil {
				pred.Skipped = true
				if !errors.Is(err, ai.ErrEmptyResponse) {
					pred.Error = err.Error()
				}
				recordPrediction(preds, i, pred, taskStart, opts.OnPrediction)
				return nil
			}
			line, ok := LastNonEmptyLine(raw)
			if !ok {
				pred.Unparseable = true
				recordPrediction(preds, i, pred, taskStart, opts.OnPrediction)
				return nil
			}
			pred.Pred = line
			pred.Correct = line == task.Gold
			recordPrediction(preds, i, pred, taskStart, opts.OnPrediction)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return ModelScore{}, err
	}

	ms := ModelScore{Model: model, N: len(fx.Tasks), Predictions: preds, ElapsedMS: time.Since(start).Milliseconds()}
	for _, p := range preds {
		if p.Correct {
			ms.Correct++
		}
		if p.Unparseable {
			ms.Unparseable++
		}
		if p.Skipped {
			ms.Skipped++
		}
	}
	if ms.N > 0 {
		ms.Accuracy = float64(ms.Correct) / float64(ms.N)
	}
	if fx.Cost != nil {
		ms.CostPerMTok = fx.Cost[model]
	}
	return ms, nil
}

func recordPrediction(preds []Prediction, i int, pred Prediction, start time.Time, onPrediction func(Prediction)) {
	pred.ElapsedMS = time.Since(start).Milliseconds()
	preds[i] = pred
	if onPrediction != nil {
		onPrediction(pred)
	}
}

// RankByAccuracy sorts model scores best-first.
func RankByAccuracy(scores []ModelScore) {
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Accuracy != scores[j].Accuracy {
			return scores[i].Accuracy > scores[j].Accuracy
		}
		if scores[i].Correct != scores[j].Correct {
			return scores[i].Correct > scores[j].Correct
		}
		return scores[i].Model < scores[j].Model
	})
}
