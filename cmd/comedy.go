package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/comedyeval"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/gradecal"
	"github.com/dotcommander/distill/internal/prompts"

	"golang.org/x/sync/errgroup"
)

// comedyFlags holds the resolved flags for `comedy`.
type comedyFlags struct {
	topics      string
	models      string
	judges      string
	baseURL     string
	out         string
	concurrency int
	timeout     int
	audit       int
	seed        int64
	local       bool
	deepseek    bool
}

func runComedy(cmd *runContext, f *comedyFlags) error {
	if f.models == "" {
		return errors.New("comedy requires --models")
	}
	ts, err := comedyeval.LoadTopics(f.topics)
	if err != nil {
		return err
	}
	roster := splitCandidates(f.models)
	if len(roster) == 0 {
		return errors.New("comedy: --models lists no models")
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

	// --- Generation: each model writes a bit per topic. Models run CONCURRENTLY
	// (bounded by --concurrency); each gets its own per-model timeout and a
	// failed model is skipped, not fatal. Results indexed by roster position so
	// no lock is needed; order is collected deterministically after Wait. ---
	type genResult struct {
		digest gradecal.Digest
		ok     bool
		model  string
		err    error
	}
	results := make([]genResult, len(roster))
	limit := f.concurrency
	if limit < 1 {
		limit = 1
	}
	_, _ = fmt.Fprintf(out, "Writing bits: %d models x %d topics (concurrency %d)...\n", len(roster), len(ts.Topics), limit)
	clientCache := newTextClientCache()
	g := new(errgroup.Group)
	g.SetLimit(limit)
	for i, model := range roster {
		g.Go(func() error {
			client, _, _, cerr := clientCache.Client(cfg, profile, model, f.baseURL)
			if cerr != nil {
				results[i] = genResult{model: model, err: cerr}
				//nolint:nilerr // intentional: per-model failure recorded, run continues
				return nil
			}
			// Per-topic timeout (inside GenerateBits); one slow topic no longer
			// dooms the model. f.timeout is the per-topic budget here.
			text, gerr := comedyeval.GenerateBits(baseCtx, client, ts, time.Duration(f.timeout)*time.Second)
			if gerr != nil {
				results[i] = genResult{model: model, err: gerr}
				//nolint:nilerr // intentional: per-model failure recorded, run continues
				return nil
			}
			results[i] = genResult{digest: gradecal.Digest{Name: strings.ReplaceAll(model, "/", "-"), Text: text}, ok: true, model: model}
			return nil
		})
	}
	_ = g.Wait()

	var bits []gradecal.Digest
	var failed []string
	for _, r := range results {
		if r.ok {
			bits = append(bits, r.digest)
			continue
		}
		if ai.IsSystemic(r.err) {
			return fmt.Errorf("ABORTING sweep: systemic failure on %s — %w (bad API key / endpoint / quota; not a per-model issue)", r.model, r.err)
		}
		_, _ = fmt.Fprintf(out, "FAILED  %-40s %v\n", r.model, r.err)
		failed = append(failed, r.model)
	}
	if len(failed)*2 >= len(roster) {
		return fmt.Errorf("RUN FAILED: %d/%d models failed (>=50%%) — refusing to emit a misleading report; investigate before trusting", len(failed), len(roster))
	}
	if len(bits) < 2 {
		return fmt.Errorf("need at least 2 models with bits to rank; got %d", len(bits))
	}

	// --- Judging: same de-biased panel, funnier-bit criterion. ---
	f.judges = resolveJudges(f.judges, cfg.DeepSeekModel, cfg.PanelJudges, cmd.FlagChanged("judges"), f.deepseek)
	judgeIDs := splitCandidates(f.judges)
	judgeOrder := make([]string, 0, len(judgeIDs))
	for _, id := range judgeIDs {
		judgeOrder = append(judgeOrder, strings.ReplaceAll(id, "/", "-"))
	}
	if len(judgeOrder) == 0 {
		return errors.New("at least one judge is required")
	}
	panel := make(map[string]gradecal.Completer, len(judgeIDs))
	for i, id := range judgeIDs {
		judge, jerr := buildMeritJudgeT(id, f.baseURL, f.local, f.deepseek)
		if jerr != nil {
			return jerr
		}
		panel[judgeOrder[i]] = judge
	}

	_, _ = fmt.Fprintf(out, "\nRanking %d comedy sets (de-biased panel, both orders, +%d-triple audit)...\n\n", len(bits), f.audit)
	res := gradecal.RunComedyPanelTournament(baseCtx, panel, judgeOrder, p, "", bits, f.audit)

	_, _ = fmt.Fprintf(out, "%-4s %-46s %3s %3s %3s\n", "RANK", "MODEL", "W", "L", "T")
	for i, name := range res.Ranking {
		w := res.Records[name]
		if w == nil {
			w = &gradecal.WL{}
		}
		_, _ = fmt.Fprintf(out, "%-4d %-46s %3d %3d %3d\n", i+1, name, w.Wins, w.Losses, w.Ties)
	}
	_, _ = fmt.Fprintf(out, "\n=== TRUST ===\n")
	_, _ = fmt.Fprintf(out, "Comparisons : %d (%d judge calls, %d errored)\n", res.Comparisons, res.Comparisons*2, res.Errors)
	_, _ = fmt.Fprintf(out, "Flip rate   : %.1f%%  (judge disagreed across slot order; want <=25%%)\n", 100*res.FlipRate())
	_, _ = fmt.Fprintf(out, "Cycle rate  : %.1f%%  (%d triples intransitive; guessing ~ 25%%, want <=15%%)\n", 100*res.CycleRate(), res.CycleTriples)
	if res.Trustworthy() {
		_, _ = fmt.Fprintf(out, "\nVERDICT: TRUSTWORTHY — coherent order, safe to read as a funniness ranking.\n")
	} else {
		_, _ = fmt.Fprintf(out, "\nVERDICT: SUSPECT — too many flips/cycles; treat the order as indicative only.\n")
	}

	if len(failed) > 0 {
		_, _ = fmt.Fprintf(out, "\nFailed/skipped (%d): %s\n", len(failed), strings.Join(failed, ", "))
	}

	if f.out != "" {
		report := gradecal.RenderPanelHTML(res, judgeOrder, "comedy tournament — funnier-bit criterion")
		if werr := writeReport(f.out, report); werr != nil {
			return werr
		}
		_, _ = fmt.Fprintf(out, "\nHTML report: %s\n", f.out)
	}
	return nil
}
