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
	corpus, err := loadJudgeCorpora(opts)
	if err != nil {
		return nil, err
	}
	if len(corpus.ids) == 0 {
		return nil, fmt.Errorf("eval: no chunk-*.md files in %s", opts.ChunksDir)
	}

	var results []CandidateResult
	for _, candidate := range corpus.candidates {
		var evals []ChunkEvaluation
		var perChunk []Metrics
		for _, id := range corpus.ids {
			ev := judgeChunk(ctx, llm, p, judgeInput{
				id:        id,
				name:      candidate.name,
				source:    corpus.source[id],
				reference: corpus.reference[id],
				candidate: candidate.chunks[id],
			})
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
		if err := writeJudgments(opts.OutDir, candidate.name, evals); err != nil {
			return nil, err
		}
		if err := writeSummary(opts.OutDir, candidate.name, evals, agg); err != nil {
			return nil, err
		}
		results = append(results, CandidateResult{Name: candidate.name, Metrics: agg, ParseErrors: parseErrors})
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

// judgeCorpus holds every input needed for a judge run. Loading it before any
// completion or output write makes malformed corpora fail without partial,
// paid evaluation artifacts.
type judgeCorpus struct {
	ids        []string
	source     map[string]string
	reference  map[string]string
	candidates []loadedCandidate
}

type loadedCandidate struct {
	name   string
	chunks map[string]string
}

// loadJudgeCorpora inventories and reads every input corpus. The source chunk
// names define the exact required set for the reference and every candidate.
func loadJudgeCorpora(opts Options) (judgeCorpus, error) {
	source, err := loadChunkSet("source", opts.ChunksDir)
	if err != nil {
		return judgeCorpus{}, err
	}
	ids := sortedChunkIDs(source)

	reference, err := loadChunkSet("reference", opts.ReferenceDir)
	if err != nil {
		return judgeCorpus{}, err
	}
	if err := validateChunkSet("reference", opts.ReferenceDir, "", ids, reference); err != nil {
		return judgeCorpus{}, err
	}

	corpus := judgeCorpus{ids: ids, source: source, reference: reference}
	for _, dir := range opts.CandidateDirs {
		name := candidateName(dir)
		chunks, err := loadChunkSet("candidate", dir)
		if err != nil {
			return judgeCorpus{}, fmt.Errorf("eval: candidate %q: %w", name, err)
		}
		if err := validateChunkSet("candidate", dir, name, ids, chunks); err != nil {
			return judgeCorpus{}, err
		}
		corpus.candidates = append(corpus.candidates, loadedCandidate{name: name, chunks: chunks})
	}
	return corpus, nil
}

// loadChunkSet reads regular chunk-*.md files only. Empty files are preserved
// as valid empty strings; nonmatching names are deliberately ignored.
func loadChunkSet(kind, dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: reading %s directory %q: %w", kind, dir, err)
	}
	chunks := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if !isChunkFile(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("eval: reading %s chunk %q: %w", kind, path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("eval: %s chunk %q is not a readable regular file", kind, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("eval: reading %s chunk %q: %w", kind, path, err)
		}
		chunks[strings.TrimSuffix(name, ".md")] = string(data)
	}
	return chunks, nil
}

func isChunkFile(name string) bool {
	return strings.HasPrefix(name, "chunk-") && strings.HasSuffix(name, ".md")
}

// validateChunkSet requires the reference or candidate corpus to have exactly
// the source chunk names. It reports the first stable difference.
func validateChunkSet(kind, dir, candidate string, expected []string, actual map[string]string) error {
	for _, id := range expected {
		if _, ok := actual[id]; !ok {
			return chunkSetMismatch(kind, dir, candidate, "missing "+id+".md")
		}
	}
	for _, id := range sortedChunkIDs(actual) {
		if !containsChunkID(expected, id) {
			return chunkSetMismatch(kind, dir, candidate, "extra "+id+".md")
		}
	}
	return nil
}

func chunkSetMismatch(kind, dir, candidate, detail string) error {
	if kind == "candidate" {
		return fmt.Errorf("eval: candidate %q chunk set mismatch: %s", candidate, detail)
	}
	return fmt.Errorf("eval: %s %q chunk set mismatch: %s", kind, dir, detail)
}

func sortedChunkIDs(chunks map[string]string) []string {
	ids := make([]string, 0, len(chunks))
	for id := range chunks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func containsChunkID(ids []string, want string) bool {
	i := sort.SearchStrings(ids, want)
	return i < len(ids) && ids[i] == want
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
