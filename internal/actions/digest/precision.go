package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/llmjson"
	"github.com/dotcommander/distill/internal/prompts"
)

// PrecisionResult reports sentence-level support judged against extracted facts.
type PrecisionResult struct {
	Supported   int                   `json:"supported"`
	Total       int                   `json:"total"`
	Precision   float64               `json:"precision"`
	Unsupported []UnsupportedSentence `json:"unsupported,omitempty"`
}

type UnsupportedSentence struct {
	Index    int    `json:"i"`
	Sentence string `json:"sentence"`
	Reason   string `json:"reason,omitempty"`
}

type precisionVerdict struct {
	Index     int    `json:"i"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}

type precisionResponse struct {
	Verdicts []precisionVerdict `json:"verdicts"`
}

func checkPrecision(ctx context.Context, llm Completer, p *prompts.Set, facts string, sentences []string, batchSize int, ledger *runLedger, timeout time.Duration, attempts int, backoff time.Duration) (PrecisionResult, error) {
	if batchSize < 1 {
		batchSize = 80
	}
	result := PrecisionResult{Total: len(sentences)}
	for start := 0; start < len(sentences); start += batchSize {
		end := start + batchSize
		if end > len(sentences) {
			end = len(sentences)
		}
		batch := sentences[start:end]
		name := fmt.Sprintf("batch-%03d", start/batchSize+1)
		started := time.Now()
		beforeUsage := ledger.usageNow()
		out, err := retryComplete(ctx, "precision "+name, llm, p.RenderPrecision(facts, numberedSentences(start, batch)), timeout, attempts, backoff)
		ledger.Record("precision", name, "call", started, err, beforeUsage)
		if err != nil {
			return result, fmt.Errorf("digest: precision %s: %w", name, err)
		}
		verdicts, ok := parsePrecisionVerdicts(out)
		if !ok {
			slog.WarnContext(ctx, "digest precision returned malformed JSON; marking batch unsupported", "batch", name)
			for i, sentence := range batch {
				result.Unsupported = append(result.Unsupported, UnsupportedSentence{Index: start + i + 1, Sentence: sentence, Reason: "precision judge returned malformed JSON"})
			}
			continue
		}
		byIndex := make(map[int]precisionVerdict, len(verdicts))
		for _, v := range verdicts {
			byIndex[v.Index] = v
		}
		for i, sentence := range batch {
			idx := start + i + 1
			v, ok := byIndex[idx]
			if !ok {
				result.Unsupported = append(result.Unsupported, UnsupportedSentence{Index: idx, Sentence: sentence, Reason: "missing verdict"})
				continue
			}
			if v.Supported {
				result.Supported++
				continue
			}
			result.Unsupported = append(result.Unsupported, UnsupportedSentence{Index: idx, Sentence: sentence, Reason: strings.TrimSpace(v.Reason)})
		}
	}
	if result.Total > 0 {
		result.Precision = float64(result.Supported) / float64(result.Total)
	}
	return result, nil
}

func parsePrecisionVerdicts(s string) ([]precisionVerdict, bool) {
	obj := llmjson.ExtractObject(s)
	if obj == "" {
		return nil, false
	}
	var parsed precisionResponse
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return nil, false
	}
	if parsed.Verdicts == nil {
		return nil, false
	}
	return parsed.Verdicts, true
}

func numberedSentences(start int, sentences []string) string {
	var b strings.Builder
	for i, sentence := range sentences {
		fmt.Fprintf(&b, "%d. %s\n", start+i+1, strings.TrimSpace(sentence))
	}
	return strings.TrimRight(b.String(), "\n")
}

// repairPrecision runs one precision-targeted repair pass: it sends the flagged
// unsupported sentences to the LLM for deletion or grounding, then guards the
// result with deterministic fact-coverage. On hard error, empty response, or
// coverage degradation, it returns the pre-repair article unchanged.
func repairPrecision(ctx context.Context, llm Completer, p *prompts.Set, facts string, flagged []UnsupportedSentence, article string, coverageBase string, preCov extractscore.SpecificsResult, ledger *runLedger, timeout time.Duration, attempts int, backoff time.Duration) (string, extractscore.SpecificsResult, error) {
	flaggedBlock := renderFlaggedSentences(flagged)
	if flaggedBlock == "" {
		return article, preCov, nil
	}

	started := time.Now()
	beforeUsage := ledger.usageNow()
	repaired, err := retryComplete(ctx, "precision-repair", llm, p.RenderPrecisionRepair(facts, flaggedBlock, article), timeout, attempts, backoff)
	ledger.Record("precision-repair", "", "call", started, err, beforeUsage)
	if err != nil {
		slog.WarnContext(ctx, "digest precision repair failed, keeping pre-repair article", "err", err)
		return article, preCov, err
	}

	repaired = strings.TrimSpace(repaired)
	if repaired == "" {
		err := errors.New("precision repair returned empty response")
		slog.WarnContext(ctx, "digest precision repair failed, keeping pre-repair article", "err", err)
		return article, preCov, err
	}

	newCov := extractscore.SpecificsCoverage(coverageBase, repaired)
	if newCov.Covered < preCov.Covered {
		slog.WarnContext(ctx, "digest precision repair degraded coverage, reverting", "covered", newCov.Covered, "was", preCov.Covered)
		return article, preCov, fmt.Errorf("precision repair decreased coverage from %d to %d", preCov.Covered, newCov.Covered)
	}

	return repaired, newCov, nil
}

func renderFlaggedSentences(flagged []UnsupportedSentence) string {
	var b strings.Builder
	for _, us := range flagged {
		fmt.Fprintf(&b, "%d. %s\n", us.Index, strings.TrimSpace(us.Sentence))
	}
	return strings.TrimRight(b.String(), "\n")
}
