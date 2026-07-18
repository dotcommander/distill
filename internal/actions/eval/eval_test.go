package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/prompts"
)

type fakeJudge struct{ resp string }

func (f fakeJudge) Complete(_ context.Context, _ string) (string, error) { return f.resp, nil }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunJudgesAndRanks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	chunks := filepath.Join(root, "chunks")
	ref := filepath.Join(root, "reference")
	cand := filepath.Join(root, "modelA", "responses")
	out := filepath.Join(root, "out")
	for _, id := range []string{"chunk-001", "chunk-002"} {
		writeFile(t, filepath.Join(chunks, id+".md"), "source "+id)
		writeFile(t, filepath.Join(ref, id+".md"), "- ref fact")
		writeFile(t, filepath.Join(cand, id+".md"), "- cand fact")
	}
	llm := fakeJudge{resp: "```json\n{\"candidate_fact_verdicts\":[{\"fact\":\"f\",\"verdict\":\"SUPPORTED\"}],\"missed_reference_facts\":[],\"summary\":\"ok\"}\n```"}
	p := &prompts.Set{Judge: "JUDGE {{CHUNK_ID}} {{CANDIDATE}} {{SOURCE}} {{REFERENCE}} {{CANDIDATE_EXTRACTION}}"}

	results, err := Run(context.Background(), llm, p, Options{
		ChunksDir: chunks, ReferenceDir: ref, CandidateDirs: []string{cand}, OutDir: out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(results))
	}
	if results[0].Name != "modelA" {
		t.Fatalf("candidate name = %q, want modelA", results[0].Name)
	}
	if results[0].Metrics.Supported != 2 {
		t.Fatalf("supported = %d, want 2", results[0].Metrics.Supported)
	}
	if !approx(results[0].Metrics.F1, 1.0) {
		t.Fatalf("F1 = %v, want 1.0", results[0].Metrics.F1)
	}
	for _, rel := range []string{"evaluations/INDEX.md", "evaluations/modelA/judgments.jsonl", "evaluations/modelA/summary.md"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Fatalf("missing artifact %s: %v", rel, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(out, "evaluations", "modelA", "judgments.jsonl"))
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n"); lines != 1 {
		t.Fatalf("expected 2 jsonl lines (1 interior newline), got %d", lines)
	}
}
