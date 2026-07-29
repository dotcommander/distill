package digest

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/fsutil"
)

func checkpointTestOptions(dir string) Options {
	return Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "artifacts", "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Concurrency: 1,
		Edit:        true,
	}
}

func checkpointRoles(llm Completer) RoleCompleters {
	return RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}
}

func bindCheckpointArtifacts(t *testing.T, opts Options, source string) {
	t.Helper()
	plan, err := PlanPackedSources([]SourcePart{{Ordinal: 1, Text: source}}, opts.ChunkSize, opts.MaxTokens)
	if err != nil {
		t.Fatalf("PlanPackedSources: %v", err)
	}
	if _, err := PrepareArtifactBindingPlan(opts.ArtifactDir, plan); err != nil {
		t.Fatalf("PrepareArtifactBindingPlan: %v", err)
	}
}

func failCheckpointPath(path string, failAt int) func(string, []byte, fs.FileMode) error {
	seen := 0
	return func(got string, data []byte, perm fs.FileMode) error {
		if got == path {
			seen++
			if seen == failAt {
				return errors.New("injected checkpoint failure")
			}
		}
		return fsutil.WriteFile(got, data, perm)
	}
}

func failPublicationRename(path string, failAt int) func(string, string) error {
	seen := 0
	return func(oldpath, newpath string) error {
		if newpath == path {
			seen++
			if seen == failAt {
				return errors.New("injected publication rename failure")
			}
		}
		return os.Rename(oldpath, newpath)
	}
}

func TestRunFailsForRequiredCheckpointWrites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*Options, *fakeLLM)
		path      func(Options) string
		stage     string
		failAt    int
	}{
		{"source chunk", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "chunks", "chunk-001.md") }, "source chunk", 1},
		{"research response", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "chunk-001.md") }, "research response", 1},
		{"compiled facts", nil, func(o Options) string { return o.FactsPath }, "compiled facts", 1},
		{"fused facts", func(o *Options, _ *fakeLLM) { o.Fuse = true }, func(o Options) string { return filepath.Join(o.ArtifactDir, "facts.fused.md") }, "fused facts", 1},
		{"merged facts", func(o *Options, l *fakeLLM) {
			o.MergeFacts = true
			o.MergeThreshold = 0.9
			o.Embedder = fakeEmbedder{vecs: [][]float32{{1, 0}, {0.9, 0.1}, {0, 1}}}
			l.research = "- Alpha 101.\n- Beta 202.\n- Gamma 303."
		}, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "facts.merged.md") }, "merged facts", 1},
		{"selected facts", func(o *Options, l *fakeLLM) {
			o.TargetFacts = 1
			o.Embedder = fakeEmbedder{vecs: [][]float32{{1, 0}, {0, 1}, {-1, 0}}}
			l.research = "- Alpha 101.\n- Beta 202.\n- Gamma 303."
		}, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "facts.selected.md") }, "selected facts", 1},
		{"document context", func(o *Options, _ *fakeLLM) { o.DocContext = true }, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "doc-context.md") }, "document context", 1},
		{"outline", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "outline.md") }, "outline", 1},
		{"section draft", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "section-001.md") }, "section draft", 1},
		{"draft", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "draft.md") }, "draft", 1},
		{"section edit", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "section-001.edited.md") }, "section edit", 1},
		{"cited rewrite", func(o *Options, _ *fakeLLM) { o.Cite = true }, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "rewrite.cited.md") }, "cited rewrite", 1},
		{"citations", func(o *Options, _ *fakeLLM) { o.Cite = true }, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "citations.json") }, "citations", 1},
		{"coverage", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "coverage.json") }, "coverage", 1},
		{"precision", func(o *Options, _ *fakeLLM) { o.CheckPrecision = true }, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "precision.json") }, "precision", 1},
		{"coverage replacement after repair", func(o *Options, l *fakeLLM) {
			o.Edit = false
			o.Repair = true
			l.research = "- The Alpha project cost 42 dollars."
			l.section = "The Alpha project proceeded."
			l.repair = "The Alpha project cost 42 dollars."
		}, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "coverage.json") }, "coverage", 2},
		{"citations replacement after repair", func(o *Options, l *fakeLLM) {
			o.Edit = false
			o.Cite = true
			o.Repair = true
			l.research = "- Alpha fact 42.\n- Beta fact 99."
			l.outline = "# Draft\n\n## Summary\nFacts: F1, F2\nsummary"
			l.section = "Alpha fact 42. [F1]"
			l.repair = "Alpha fact 42. [F1] Beta fact 99. [F2]"
		}, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "citations.json") }, "citations", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := checkpointTestOptions(dir)
			llm := &fakeLLM{}
			if tc.configure != nil {
				tc.configure(&opts, llm)
			}
			path := tc.path(opts)
			opts.writeFile = failCheckpointPath(path, tc.failAt)
			_, err := Run(context.Background(), checkpointRoles(llm), testPrompts(), "# Title\n\nbody", opts)
			if err == nil || !strings.Contains(err.Error(), tc.stage) || !strings.Contains(err.Error(), path) {
				t.Fatalf("Run error = %v, want %q checkpoint path %q", err, tc.stage, path)
			}
		})
	}
}

func TestRunFailsWhenResumeResearchResponseCannotBeRecheckpointed(t *testing.T) {
	dir := t.TempDir()
	opts := checkpointTestOptions(dir)
	llm := &fakeLLM{}
	if _, err := Run(context.Background(), checkpointRoles(llm), testPrompts(), "# Title\n\nbody", opts); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	if err := os.Remove(opts.FactsPath); err != nil {
		t.Fatalf("remove compiled facts to exercise response resume: %v", err)
	}
	response := filepath.Join(opts.ArtifactDir, "responses", "chunk-001.md")
	opts.Resume = true
	opts.writeFile = failCheckpointPath(response, 1)
	_, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), "resumed research response") || !strings.Contains(err.Error(), response) {
		t.Fatalf("Run error = %v, want resumed response checkpoint path %q", err, response)
	}
}

func TestRunFailsWhenCachedResearchResponseCannotBeMaterialized(t *testing.T) {
	dir := t.TempDir()
	opts := checkpointTestOptions(dir)
	chunks, err := ChunkSource("# Title\n\nbody", opts.ChunkSize, opts.MaxTokens)
	if err != nil {
		t.Fatalf("ChunkSource: %v", err)
	}
	cache := newMemResearchCache()
	cache.hits[chunks[0].Text] = "- cached fact"
	opts.ResearchCache = cache
	response := filepath.Join(opts.ArtifactDir, "responses", "chunk-001.md")
	opts.writeFile = failCheckpointPath(response, 1)
	_, err = Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), "cached research response") || !strings.Contains(err.Error(), response) {
		t.Fatalf("Run error = %v, want cached response checkpoint path %q", err, response)
	}
}

func TestWriteJSONCheckpointReportsEncodingAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	err := writeJSONCheckpoint(Options{writeFile: fsutil.WriteFile}, "test JSON", path, math.Inf(1))
	if err == nil || !strings.Contains(err.Error(), "encode test JSON checkpoint") || !strings.Contains(err.Error(), path) {
		t.Fatalf("writeJSONCheckpoint error = %v", err)
	}
}

func TestRunFinalPublicationFailuresRemoveBothFinals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stage string
		path  func(Options) string
	}{
		{name: "output rename", stage: "publish final output", path: func(o Options) string { return o.OutPath }},
		{name: "artifact rename", stage: "publish final artifact", path: func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "rewrite.md") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertFinalPublicationFailure(t, tc.stage, tc.path)
		})
	}
}

func assertFinalPublicationFailure(t *testing.T, stage string, failedPath func(Options) string) {
	t.Helper()
	dir := t.TempDir()
	opts := checkpointTestOptions(dir)
	artifact := filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
	bindCheckpointArtifacts(t, opts, "# Title\n\nbody")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.OutPath, []byte("stale output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("stale artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.renameFile = failPublicationRename(failedPath(opts), 1)
	_, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), stage) {
		t.Fatalf("Run error = %v, want %q", err, stage)
	}
	for _, finalPath := range finalPublicationPaths(opts) {
		if _, statErr := os.Stat(finalPath); !os.IsNotExist(statErr) {
			t.Fatalf("failed run left final publication %q: %v", finalPath, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(opts.ArtifactDir, "responses", "draft.md")); statErr != nil {
		t.Fatalf("failed publication removed resumable checkpoint: %v", statErr)
	}
}

func TestRunCachedPublicationFailureRemovesBothFinals(t *testing.T) {
	dir := t.TempDir()
	cache := newMemCache()
	cache.Store("cached", "cached article", CacheMeta{Words: 2})
	opts := checkpointTestOptions(dir)
	opts.Cache, opts.CacheKey, opts.CacheRead = cache, "cached", true
	artifact := filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
	bindCheckpointArtifacts(t, opts, "# Title\n\nbody")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.OutPath, []byte("stale output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("stale artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.renameFile = failPublicationRename(artifact, 1)
	_, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), "publish final artifact") {
		t.Fatalf("Run error = %v, want artifact publication failure", err)
	}
	for _, finalPath := range finalPublicationPaths(opts) {
		if _, statErr := os.Stat(finalPath); !os.IsNotExist(statErr) {
			t.Fatalf("failed cache publication left final %q: %v", finalPath, statErr)
		}
	}
}

func TestRunPublishesSharedOutputAndRewritePathAtomically(t *testing.T) {
	dir := t.TempDir()
	opts := checkpointTestOptions(dir)
	opts.OutPath = filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
	if _, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts); err != nil {
		t.Fatalf("Run shared final path: %v", err)
	}
	data, err := os.ReadFile(opts.OutPath)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("shared final output = %q, %v", data, err)
	}
}

func TestRunCacheHitFinalGateFailureLeavesNoFinalsAndWritesFailureSummary(t *testing.T) {
	dir := t.TempDir()
	cache := newMemCache()
	cache.Store("lax", "cached article", CacheMeta{Words: 2})
	opts := checkpointTestOptions(dir)
	opts.Cache, opts.CacheKey, opts.CacheRead = cache, "lax", true
	opts.FinalGate = func(*Result) error { return errors.New("strict quality threshold") }
	artifact := filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
	bindCheckpointArtifacts(t, opts, "# Title\n\nbody")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.OutPath, []byte("stale output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("stale artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), "quality-gate") {
		t.Fatalf("Run error = %v, want quality gate failure", err)
	}
	if res == nil || !res.CacheHit {
		t.Fatalf("cache gate result = %#v, want cache hit result", res)
	}
	for _, finalPath := range finalPublicationPaths(opts) {
		if _, statErr := os.Stat(finalPath); !os.IsNotExist(statErr) {
			t.Fatalf("rejected cache hit left final %q: %v", finalPath, statErr)
		}
	}
	data, readErr := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-summary.json"))
	if readErr != nil {
		t.Fatalf("read failure summary: %v", readErr)
	}
	var summary runSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("parse failure summary: %v", err)
	}
	if summary.Status != "failed" || summary.FailingStage != "quality-gate" || summary.OutputSHA256 != "" {
		t.Fatalf("failure summary = %+v", summary)
	}
}

func TestRunFailsWhenLedgerCannotInitialize(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file"), 0o644); err != nil {
		t.Fatalf("seed blocked ledger parent: %v", err)
	}
	opts := checkpointTestOptions(dir)
	opts.LedgerPath = filepath.Join(blocked, "run-ledger.jsonl")
	articleCache := newMemCache()
	researchCache := newMemResearchCache()
	opts.Cache, opts.CacheKey = articleCache, "checkpoint-control"
	opts.ResearchCache = researchCache
	if _, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts); err == nil {
		t.Fatal("Run with unavailable ledger succeeded")
	}
	if _, err := os.Stat(opts.OutPath); !os.IsNotExist(err) {
		t.Fatalf("unavailable ledger must prevent output publication: %v", err)
	}
	if articleCache.stores != 0 || len(researchCache.stores) != 0 {
		t.Fatalf("cache stores after ledger initialization failure = article %d, research %d; want zero", articleCache.stores, len(researchCache.stores))
	}
}
