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

type countingJudge struct {
	resp  string
	calls int
}

func (f *countingJudge) Complete(_ context.Context, _ string) (string, error) {
	f.calls++
	return f.resp, nil
}

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

func TestRunPreflightsAllJudgeCorpora(t *testing.T) {
	t.Parallel()
	judgeResponse := `{"candidate_fact_verdicts":[],"missed_reference_facts":[],"summary":"ok"}`
	for _, tt := range judgePreflightCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			opts := tt.setUp(t, root)
			judge := &countingJudge{resp: judgeResponse}
			_, err := Run(context.Background(), judge, &prompts.Set{Judge: "{{SOURCE}}"}, opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run error = %v, want substring %q", err, tt.wantErr)
			}
			if judge.calls != 0 {
				t.Fatalf("judge calls = %d, want 0", judge.calls)
			}
			if _, statErr := os.Stat(opts.OutDir); !os.IsNotExist(statErr) {
				t.Fatalf("invalid corpus wrote output directory: stat err = %v", statErr)
			}
		})
	}
}

type judgePreflightCase struct {
	name    string
	setUp   func(t *testing.T, root string) Options
	wantErr string
}

func judgePreflightCases() []judgePreflightCase {
	return []judgePreflightCase{
		{name: "source directory missing", setUp: setupMissingSourceDirectory, wantErr: "eval: reading source directory"},
		{name: "reference missing source chunk", setUp: setupMissingReferenceChunk, wantErr: "chunk set mismatch: missing chunk-002.md"},
		{name: "later candidate missing source chunk", setUp: setupLaterCandidateMissingChunk, wantErr: `eval: candidate "later" chunk set mismatch: missing chunk-008.md`},
		{name: "reference has extra matching chunk", setUp: setupReferenceExtraChunk, wantErr: "eval: reference "},
		{name: "first candidate has extra matching chunk", setUp: setupCandidateExtraChunk, wantErr: `eval: candidate "candidate" chunk set mismatch: extra chunk-999.md`},
		{name: "candidate chunk is directory", setUp: setupCandidateDirectoryChunk, wantErr: "candidate chunk"},
		{name: "candidate chunk is symlink", setUp: setupCandidateSymlinkChunk, wantErr: "candidate chunk"},
	}
}

func setupMissingSourceDirectory(_ *testing.T, root string) Options {
	return Options{ChunksDir: filepath.Join(root, "missing-chunks"), ReferenceDir: filepath.Join(root, "reference"), CandidateDirs: []string{filepath.Join(root, "candidate")}, OutDir: filepath.Join(root, "out")}
}

func setupMissingReferenceChunk(t *testing.T, root string) Options {
	chunks, reference, candidate := fixtureDirectories(root)
	for _, id := range []string{"chunk-001", "chunk-002"} {
		writeFile(t, filepath.Join(chunks, id+".md"), "source "+id)
		writeFile(t, filepath.Join(candidate, id+".md"), "candidate "+id)
	}
	writeFile(t, filepath.Join(reference, "chunk-001.md"), "reference")
	return judgeOptions(root, chunks, reference, candidate)
}

func setupLaterCandidateMissingChunk(t *testing.T, root string) Options {
	chunks, reference := filepath.Join(root, "chunks"), filepath.Join(root, "reference")
	first, later := filepath.Join(root, "first"), filepath.Join(root, "later")
	for _, id := range []string{"chunk-001", "chunk-008"} {
		writeFile(t, filepath.Join(chunks, id+".md"), "source "+id)
		writeFile(t, filepath.Join(reference, id+".md"), "reference "+id)
		writeFile(t, filepath.Join(first, id+".md"), "candidate "+id)
	}
	writeFile(t, filepath.Join(later, "chunk-001.md"), "candidate one")
	return Options{ChunksDir: chunks, ReferenceDir: reference, CandidateDirs: []string{first, later}, OutDir: filepath.Join(root, "out")}
}

func setupReferenceExtraChunk(t *testing.T, root string) Options {
	chunks, reference, candidate := writeBasicJudgeFixture(t, root)
	writeFile(t, filepath.Join(reference, "chunk-999.md"), "extra")
	return judgeOptions(root, chunks, reference, candidate)
}

func setupCandidateExtraChunk(t *testing.T, root string) Options {
	chunks, reference, candidate := writeBasicJudgeFixture(t, root)
	writeFile(t, filepath.Join(candidate, "chunk-999.md"), "extra")
	return judgeOptions(root, chunks, reference, candidate)
}

func setupCandidateDirectoryChunk(t *testing.T, root string) Options {
	chunks, reference, candidate := fixtureDirectories(root)
	writeFile(t, filepath.Join(chunks, "chunk-001.md"), "source")
	writeFile(t, filepath.Join(reference, "chunk-001.md"), "reference")
	if err := os.MkdirAll(filepath.Join(candidate, "chunk-001.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	return judgeOptions(root, chunks, reference, candidate)
}

func setupCandidateSymlinkChunk(t *testing.T, root string) Options {
	chunks, reference, candidate := fixtureDirectories(root)
	writeFile(t, filepath.Join(chunks, "chunk-001.md"), "source")
	writeFile(t, filepath.Join(reference, "chunk-001.md"), "reference")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(candidate, "chunk-001.md")); err != nil {
		t.Skipf("creating test symlink: %v", err)
	}
	return judgeOptions(root, chunks, reference, candidate)
}

func writeBasicJudgeFixture(t *testing.T, root string) (chunks, reference, candidate string) {
	t.Helper()
	chunks, reference, candidate = fixtureDirectories(root)
	writeFile(t, filepath.Join(chunks, "chunk-001.md"), "source")
	writeFile(t, filepath.Join(reference, "chunk-001.md"), "reference")
	writeFile(t, filepath.Join(candidate, "chunk-001.md"), "candidate")
	return chunks, reference, candidate
}

func fixtureDirectories(root string) (chunks, reference, candidate string) {
	return filepath.Join(root, "chunks"), filepath.Join(root, "reference"), filepath.Join(root, "candidate")
}

func judgeOptions(root, chunks, reference, candidate string) Options {
	return Options{ChunksDir: chunks, ReferenceDir: reference, CandidateDirs: []string{candidate}, OutDir: filepath.Join(root, "out")}
}

func TestRunPreflightAllowsEmptyChunksAndIgnoresNonmatchingFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	chunks, reference, candidate := filepath.Join(root, "chunks"), filepath.Join(root, "reference"), filepath.Join(root, "candidate")
	for _, dir := range []string{chunks, reference, candidate} {
		writeFile(t, filepath.Join(dir, "chunk-001.md"), "")
		writeFile(t, filepath.Join(dir, "notes.md"), "ignored")
	}
	judge := &countingJudge{resp: `{"candidate_fact_verdicts":[],"missed_reference_facts":[],"summary":"ok"}`}
	_, err := Run(context.Background(), judge, &prompts.Set{Judge: "{{SOURCE}}"}, Options{
		ChunksDir: chunks, ReferenceDir: reference, CandidateDirs: []string{candidate}, OutDir: filepath.Join(root, "out"),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if judge.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", judge.calls)
	}
}
