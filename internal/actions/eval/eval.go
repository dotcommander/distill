package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/distill/internal/fsutil"
	"github.com/dotcommander/distill/internal/prompts"
)

// Completer runs a single LLM completion. *ai.Client satisfies it.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// ChunkEvaluation is the judge's full record for one (candidate, chunk) pair.
type ChunkEvaluation struct {
	ChunkID               string        `json:"chunk_id"`
	Candidate             string        `json:"candidate"`
	CandidateFactVerdicts []FactVerdict `json:"candidate_fact_verdicts"`
	MissedReferenceFacts  []MissedFact  `json:"missed_reference_facts"`
	Summary               string        `json:"summary"`
	Metrics               Metrics       `json:"metrics"`
	ParseError            string        `json:"parse_error,omitempty"`
}

// Options configures one eval run.
type Options struct {
	ChunksDir     string   // source chunks (chunk-NNN.md)
	ReferenceDir  string   // reference extraction responses (chunk-NNN.md)
	CandidateDirs []string // one or more candidate extraction response dirs
	OutDir        string   // evaluations/ is written under here
}

// CandidateResult is the aggregate score for one candidate, used for ranking.
// ParseErrors counts chunks whose judge response could not be parsed (those
// contribute zero counts to the aggregate, so a nonzero value means the F1 is
// based on fewer chunks than the candidate has).
type CandidateResult struct {
	Name        string  `json:"name"`
	Metrics     Metrics `json:"metrics"`
	ParseErrors int     `json:"parse_errors"`
}

// Run judges every candidate's extractions chunk-by-chunk against the reference,
// writes per-candidate judgments.jsonl + summary.md, and a ranked INDEX.md, and
// returns the candidates sorted by F1 (descending).
func Run(ctx context.Context, llm Completer, p *prompts.Set, opts Options) ([]CandidateResult, error) {
	ids, err := chunkIDs(opts.ChunksDir)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("eval: no chunk-*.md files in %s", opts.ChunksDir)
	}

	var results []CandidateResult
	for _, dir := range opts.CandidateDirs {
		name := candidateName(dir)
		var evals []ChunkEvaluation
		var perChunk []Metrics
		for _, id := range ids {
			src := readFileOrEmpty(filepath.Join(opts.ChunksDir, id+".md"))
			ref := readFileOrEmpty(filepath.Join(opts.ReferenceDir, id+".md"))
			cand := readFileOrEmpty(filepath.Join(dir, id+".md"))
			ev := judgeChunk(ctx, llm, p, judgeInput{id: id, name: name, source: src, reference: ref, candidate: cand})
			evals = append(evals, ev)
			perChunk = append(perChunk, ev.Metrics)
		}
		agg := aggregate(perChunk)
		parseErrors := 0
		for _, ev := range evals {
			if ev.ParseError != "" {
				parseErrors++
			}
		}
		if err := writeJudgments(opts.OutDir, name, evals); err != nil {
			return nil, err
		}
		if err := writeSummary(opts.OutDir, name, evals, agg); err != nil {
			return nil, err
		}
		results = append(results, CandidateResult{Name: name, Metrics: agg, ParseErrors: parseErrors})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Metrics.F1 != results[j].Metrics.F1 {
			return results[i].Metrics.F1 > results[j].Metrics.F1
		}
		return results[i].Name < results[j].Name
	})
	if err := writeIndex(opts.OutDir, results); err != nil {
		return nil, err
	}
	return results, nil
}

// judgeInput bundles the per-(candidate, chunk) texts for one judge call.
type judgeInput struct {
	id, name, source, reference, candidate string
}

// judgeChunk runs the judge for one (candidate, chunk) and parses the result.
// A failed call or unparseable response is recorded in ParseError with zero metrics.
func judgeChunk(ctx context.Context, llm Completer, p *prompts.Set, in judgeInput) ChunkEvaluation {
	ev := ChunkEvaluation{ChunkID: in.id, Candidate: in.name}
	out, err := llm.Complete(ctx, p.RenderJudge(in.id, in.name, in.source, in.reference, in.candidate))
	if err != nil {
		ev.ParseError = "judge call failed: " + err.Error()
		return ev
	}
	raw := extractJSONObject(out)
	var jr JudgeResult
	if raw == "" || json.Unmarshal([]byte(raw), &jr) != nil {
		ev.ParseError = "could not parse judge JSON; raw response: " + strings.TrimSpace(out)
		return ev
	}
	ev.CandidateFactVerdicts = jr.CandidateFactVerdicts
	ev.MissedReferenceFacts = jr.MissedReferenceFacts
	ev.Summary = jr.Summary
	ev.Metrics = computeMetrics(jr)
	return ev
}

// chunkIDs returns sorted chunk ids ("chunk-001", …) from the chunks dir.
func chunkIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: reading chunks dir %s: %w", dir, err)
	}
	var ids []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "chunk-") && strings.HasSuffix(n, ".md") {
			ids = append(ids, strings.TrimSuffix(n, ".md"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// candidateName derives a label from a response dir: the dir name, or its parent
// when the dir is literally "responses" (digest's artifact layout).
func candidateName(dir string) string {
	clean := filepath.Clean(dir)
	base := filepath.Base(clean)
	if base == "responses" {
		return filepath.Base(filepath.Dir(clean))
	}
	return base
}

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func writeJudgments(outDir, name string, evals []ChunkEvaluation) error {
	var b strings.Builder
	for _, ev := range evals {
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("eval: marshaling judgment for %s: %w", ev.ChunkID, err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return fsutil.WriteFile(filepath.Join(outDir, "evaluations", name, "judgments.jsonl"), []byte(b.String()), 0o644)
}

func writeSummary(outDir, name string, evals []ChunkEvaluation, agg Metrics) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evaluation: %s\n\n", name)
	b.WriteString("| Chunk | Facts | Supported | Contradicted | NotInSource | Missed | P | R | F1 |\n")
	b.WriteString("|-------|-------|-----------|--------------|-------------|--------|---|---|----|\n")
	for _, ev := range evals {
		m := ev.Metrics
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %.3f | %.3f | %.3f |\n",
			ev.ChunkID, m.CandidateFacts, m.Supported, m.Contradicted, m.NotInSource, m.MissedReference, m.Precision, m.Recall, m.F1)
	}
	fmt.Fprint(&b, "\n## Aggregate (micro-average)\n\n")
	fmt.Fprintf(&b, "- Candidate facts: %d\n- Supported: %d\n- Contradicted: %d\n- Not in source: %d\n- Missed reference: %d\n",
		agg.CandidateFacts, agg.Supported, agg.Contradicted, agg.NotInSource, agg.MissedReference)
	fmt.Fprintf(&b, "- **Precision: %.3f · Recall: %.3f · F1: %.3f**\n", agg.Precision, agg.Recall, agg.F1)
	return fsutil.WriteFile(filepath.Join(outDir, "evaluations", name, "summary.md"), []byte(b.String()), 0o644)
}

func writeIndex(outDir string, results []CandidateResult) error {
	var b strings.Builder
	b.WriteString("# Evaluation Index\n\nCandidates ranked by F1 (descending).\n\n")
	b.WriteString("| Rank | Candidate | Precision | Recall | F1 | Parse failures |\n")
	b.WriteString("|------|-----------|-----------|--------|----|----------------|\n")
	for i, r := range results {
		fmt.Fprintf(&b, "| %d | %s | %.3f | %.3f | %.3f | %d |\n", i+1, r.Name, r.Metrics.Precision, r.Metrics.Recall, r.Metrics.F1, r.ParseErrors)
	}
	return fsutil.WriteFile(filepath.Join(outDir, "evaluations", "INDEX.md"), []byte(b.String()), 0o644)
}
