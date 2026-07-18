package digest

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/prompts"
)

// maxRepairPasses bounds the verify→repair loop: one corrective pass by default,
// mirroring the digest playbook's retry_policy. A pass that fails to raise
// coverage ends the loop early.
const maxRepairPasses = 1

// repairMissing runs up to maxRepairPasses targeted reinsert passes against the
// edit-role model: each pass asks it to weave the still-missing specifics back
// into the article without altering existing wording, then recomputes coverage
// deterministically. It keeps the best-by-Covered article, so a repair pass can
// only raise coverage, never lower it; an errored or non-improving pass ends the
// loop with the best article so far. Coverage is recomputed against factsAppendix
// (the pre-fuse fact snapshot) exactly as the initial self-check does.
func repairMissing(ctx context.Context, llm Completer, p *prompts.Set, factsAppendix, article string, cov extractscore.SpecificsResult, ledger *runLedger, timeout time.Duration, attempts int, backoff time.Duration) (string, extractscore.SpecificsResult) {
	bestArticle, bestCov := article, cov
	for pass := 0; pass < maxRepairPasses && len(bestCov.Missing) > 0; pass++ {
		slog.InfoContext(ctx, "digest repair start", "missing", len(bestCov.Missing), "pass", pass+1)
		started := time.Now()
		beforeUsage := ledger.usageNow()
		repaired, err := retryComplete(ctx, "repair", llm, p.RenderRepair(bestArticle, strings.Join(bestCov.Missing, "\n")), timeout, attempts, backoff)
		ledger.Record("repair", "", "call", started, err, beforeUsage)
		if err != nil {
			// A polish-stage failure must never discard the article: keep best.
			slog.WarnContext(ctx, "digest repair failed, keeping pre-repair article", "err", err)
			break
		}
		newCov := extractscore.SpecificsCoverage(factsAppendix, repaired)
		if newCov.Covered <= bestCov.Covered {
			// Best-of: never let a repair pass lower coverage.
			slog.InfoContext(ctx, "digest repair did not improve coverage, discarding", "covered", newCov.Covered, "best", bestCov.Covered)
			break
		}
		slog.InfoContext(ctx, "digest repair improved coverage", "covered", newCov.Covered, "was", bestCov.Covered, "missing", len(newCov.Missing))
		bestArticle, bestCov = repaired, newCov
	}
	return bestArticle, bestCov
}

func repairMissingCited(ctx context.Context, llm Completer, p *prompts.Set, units []factUnit, article string, citations CitationResult, ledger *runLedger, timeout time.Duration, attempts int, backoff time.Duration) (string, CitationResult) {
	bestArticle, bestCitations := article, citations
	for pass := 0; pass < maxRepairPasses && len(bestCitations.MissingIDs) > 0; pass++ {
		missing := selectFactsTagged(units, bestCitations.MissingIDs)
		if strings.TrimSpace(missing) == "" {
			break
		}
		slog.InfoContext(ctx, "digest cited repair start", "missing", len(bestCitations.MissingIDs), "pass", pass+1)
		started := time.Now()
		beforeUsage := ledger.usageNow()
		repaired, err := retryComplete(ctx, "cite-repair", llm, p.CiteRepair+p.RenderRepair(bestArticle, missing), timeout, attempts, backoff)
		ledger.Record("cite-repair", "", "call", started, err, beforeUsage)
		if err != nil {
			slog.WarnContext(ctx, "digest cited repair failed, keeping pre-repair article", "err", err)
			break
		}
		newCitations := computeCitations(units, repaired)
		if newCitations.Covered <= bestCitations.Covered {
			slog.InfoContext(ctx, "digest cited repair did not improve coverage, discarding", "covered", newCitations.Covered, "best", bestCitations.Covered)
			break
		}
		slog.InfoContext(ctx, "digest cited repair improved coverage", "covered", newCitations.Covered, "was", bestCitations.Covered, "missing", len(newCitations.MissingIDs))
		bestArticle, bestCitations = repaired, newCitations
	}
	return bestArticle, bestCitations
}
