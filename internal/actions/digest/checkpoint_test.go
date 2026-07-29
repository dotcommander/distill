package digest

import (
	"context"
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
		{"final output", nil, func(o Options) string { return o.OutPath }, "final output", 1},
		{"final artifact", nil, func(o Options) string { return filepath.Join(o.ArtifactDir, "responses", "rewrite.md") }, "final artifact", 1},
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

func TestRunFinalArtifactFailureKeepsDurableOutput(t *testing.T) {
	dir := t.TempDir()
	opts := checkpointTestOptions(dir)
	artifact := filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
	opts.writeFile = failCheckpointPath(artifact, 1)
	_, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), "survives at") || !strings.Contains(err.Error(), opts.OutPath) {
		t.Fatalf("Run error = %v, want surviving output diagnostic", err)
	}
	if _, err := os.Stat(opts.OutPath); err != nil {
		t.Fatalf("durable output missing after artifact failure: %v", err)
	}
}

func TestRunCachedArtifactFailureKeepsDurableOutputAndOrdersWrites(t *testing.T) {
	dir := t.TempDir()
	cache := newMemCache()
	cache.Store("cached", "cached article", CacheMeta{Words: 2})
	opts := checkpointTestOptions(dir)
	opts.Cache, opts.CacheKey, opts.CacheRead = cache, "cached", true
	artifact := filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
	var writes []string
	writer := failCheckpointPath(artifact, 1)
	opts.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		writes = append(writes, path)
		return writer(path, data, perm)
	}
	_, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts)
	if err == nil || !strings.Contains(err.Error(), "survives at") || !strings.Contains(err.Error(), opts.OutPath) {
		t.Fatalf("Run error = %v, want surviving cached output diagnostic", err)
	}
	if len(writes) != 2 || writes[0] != opts.OutPath || writes[1] != artifact {
		t.Fatalf("cached write order = %v, want [%q %q]", writes, opts.OutPath, artifact)
	}
	if data, err := os.ReadFile(opts.OutPath); err != nil || string(data) != "cached article" {
		t.Fatalf("cached durable output = %q, %v", data, err)
	}
}

func TestRunContinuesWhenLedgerCannotInitialize(t *testing.T) {
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
	if _, err := Run(context.Background(), checkpointRoles(&fakeLLM{}), testPrompts(), "# Title\n\nbody", opts); err != nil {
		t.Fatalf("Run with unavailable ledger: %v", err)
	}
	if articleCache.stores != 1 || len(researchCache.stores) != 1 {
		t.Fatalf("best-effort cache stores = article %d, research %d; want one each", articleCache.stores, len(researchCache.stores))
	}
}
