package digest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/prompts"
)

// fakeLLM returns canned research/fuse/write/edit output, distinguished by the
// rendered prompt prefix, and counts calls.
type fakeLLM struct {
	calls            int
	prompts          []string
	research         string
	docHeader        string
	fuse             string
	outline          string
	section          string
	edit             string
	repair           string
	precision        string
	precisionRepair  string
	precisionRecords []string
}

func (f *fakeLLM) Complete(_ context.Context, prompt string) (string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	switch {
	case strings.HasPrefix(prompt, "DOC_CONTEXT"):
		return defaultString(f.docHeader, "TITLE: Test Document\nSYNOPSIS: A compact test document."), nil
	case strings.HasPrefix(prompt, "CITE_SECTION"):
		return defaultString(f.section, "written body [F1]"), nil
	case strings.HasPrefix(prompt, "CITE_EDIT"):
		return defaultString(f.edit, "edited body [F1]"), nil
	case strings.HasPrefix(prompt, "CITE_REPAIR"):
		return defaultString(f.repair, "repaired body [F1]"), nil
	case strings.HasPrefix(prompt, "PRECISION_REPAIR"):
		return defaultString(f.precisionRepair, defaultString(f.repair, "repaired body [F1]")), nil
	case strings.HasPrefix(prompt, "PRECISION"):
		return f.nextPrecisionResponse(), nil
	case strings.HasPrefix(prompt, "FUSE"):
		return defaultString(f.fuse, "## topic\n\n- a fact"), nil
	case strings.HasPrefix(prompt, "OUTLINE"):
		return defaultString(f.outline, "# Draft Title\n\n## Section One\nwhat one covers\n\n## Section Two\nwhat two covers"), nil
	case strings.HasPrefix(prompt, "SECTION"):
		return defaultString(f.section, "written body"), nil
	case strings.HasPrefix(prompt, "EDIT"):
		return defaultString(f.edit, "edited body"), nil
	case strings.HasPrefix(prompt, "REPAIR"):
		return defaultString(f.repair, "repaired body"), nil
	default:
		return defaultString(f.research, "- a fact"), nil
	}
}

func (f *fakeLLM) nextPrecisionResponse() string {
	if len(f.precisionRecords) > 0 {
		value := f.precisionRecords[0]
		f.precisionRecords = f.precisionRecords[1:]
		return value
	}
	return defaultString(f.precision, `{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":true,"reason":""},{"i":3,"supported":true,"reason":""}]}`)
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

type memCache struct {
	articles map[string]string
	metas    map[string]CacheMeta
	stores   int
}

func newMemCache() *memCache {
	return &memCache{articles: make(map[string]string), metas: make(map[string]CacheMeta)}
}

func (m *memCache) Load(key string) (string, CacheMeta, bool) {
	article, ok := m.articles[key]
	return article, m.metas[key], ok
}

func (m *memCache) Store(key, article string, meta CacheMeta) {
	m.stores++
	m.articles[key] = article
	m.metas[key] = meta
}

type memResearchCache struct {
	hits   map[string]string
	stores map[string]string
}

func newMemResearchCache() *memResearchCache {
	return &memResearchCache{hits: make(map[string]string), stores: make(map[string]string)}
}

func (m *memResearchCache) Load(chunkText string) (string, bool) {
	v, ok := m.hits[chunkText]
	return v, ok
}

func (m *memResearchCache) Store(chunkText, response string) {
	m.stores[chunkText] = response
}

func testPrompts() *prompts.Set {
	return &prompts.Set{
		Research:          "RESEARCH {{CHUNK_ID}}: {{CHUNK}}",
		Fuse:              "FUSE: {{NOTES}}",
		Outline:           "OUTLINE [{{STYLE}}]: {{FACTS}}",
		Section:           "SECTION [{{STYLE}}] {{HEADING}}|{{INTENT}}|{{OUTLINE}}|{{PRIOR}}|{{FACTS}}",
		EditSection:       "EDIT [{{STYLE}}] {{HEADING}} :: {{ARTICLE}} :: PRIOR {{PRIOR_ACCEPTED}} :: {{FACTS}}",
		Repair:            "REPAIR {{ARTICLE}} :: {{MISSING}}",
		DocContext:        "DOC_CONTEXT\nHEADINGS:\n{{HEADINGS}}\nEXCERPT:\n{{EXCERPT}}",
		DocHeaderPreamble: "DOC_HEADER\n{{HEADER}}\n---\n",
		CiteSection:       "CITE_SECTION\n",
		CiteEdit:          "CITE_EDIT\n",
		CiteRepair:        "CITE_REPAIR\n",
		Precision:         "PRECISION FACTS:\n{{FACTS}}\nSENTENCES:\n{{SENTENCES}}",
		PrecisionRepair:   "PRECISION_REPAIR FACTS:\n{{FACTS}}\nFLAGGED:\n{{FLAGGED}}\nARTICLE:\n{{ARTICLE}}",
		MergeFacts:        "MERGE {{CLUSTER_ID}}\n{{FACTS}}",
		ClusterLabels:     "CLUSTER_LABELS\n{{CLUSTERS}}",
	}
}

func TestCompileFactsDedupesNormalizedFactLines(t *testing.T) {
	t.Parallel()
	outs := []string{
		"- Alpha fact.\n- Beta fact.",
		"  alpha   FACT  \n- Alpha fact.\n- Beta fact is different.",
	}

	facts, failed, reused := compileFacts(outs, []bool{false, false}, []bool{false, false})

	if len(failed) != 0 {
		t.Fatalf("unexpected failed chunks: %v", failed)
	}
	if reused != 0 {
		t.Fatalf("unexpected reused count: %d", reused)
	}
	expected := "## chunk-001\n\n- Alpha fact.\n- Beta fact." + factSeparator + "## chunk-002\n\n- Beta fact is different."
	if facts != expected {
		t.Fatalf("compiled facts mismatch:\nwant: %q\n got: %q", expected, facts)
	}
}

func TestRunPipeline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{}
	source := "# Title\n\nSome content paragraph one.\n\n## Section\n\nMore content here."
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Fuse:        true,
		Edit:        true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, statErr := os.Stat(opts.FactsPath); statErr != nil {
		t.Fatalf("facts not written: %v", statErr)
	}
	final, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("out not written: %v", err)
	}
	if !strings.Contains(string(final), "edited") {
		t.Fatalf("unexpected final output: %q", final)
	}
	if _, statErr := os.Stat(filepath.Join(opts.ArtifactDir, "facts.fused.md")); statErr != nil {
		t.Fatalf("fused notes artifact missing: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(opts.ArtifactDir, "responses", "draft.md")); statErr != nil {
		t.Fatalf("draft artifact missing: %v", statErr)
	}
	if res.ChunkCount < 1 {
		t.Fatalf("expected >=1 chunk, got %d", res.ChunkCount)
	}
	if len(res.FailedChunks) != 0 {
		t.Fatalf("unexpected failed chunks: %v", res.FailedChunks)
	}
	if _, statErr := os.Stat(filepath.Join(opts.ArtifactDir, "chunks", "chunk-001.md")); statErr != nil {
		t.Fatalf("chunk artifact missing: %v", statErr)
	}
}

func TestRunCachesAndServesArticle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := newMemCache()
	source := "# Title\n\nSome content paragraph one."
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
		Cache:       cache,
		CacheKey:    "cache-key",
		CacheRead:   true,
	}
	llm := &fakeLLM{}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run miss: %v", err)
	}
	if res.CacheHit {
		t.Fatal("first run unexpectedly reported a cache hit")
	}
	if llm.calls == 0 {
		t.Fatal("expected LLM calls on cache miss")
	}
	if cache.stores != 1 {
		t.Fatalf("expected one cache store, got %d", cache.stores)
	}
	cachedArticle, storedMeta, ok := cache.Load(opts.CacheKey)
	if !ok {
		t.Fatal("article was not stored")
	}
	if storedMeta.Words == 0 {
		t.Fatal("cache store must carry the article word count")
	}
	written, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("out not written on cache miss: %v", err)
	}
	if string(written) != cachedArticle {
		t.Fatalf("cached article mismatch: %q != %q", written, cachedArticle)
	}

	llm = &fakeLLM{}
	opts.OutPath = filepath.Join(dir, "cached-out.md")
	res, err = Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run hit: %v", err)
	}
	if !res.CacheHit {
		t.Fatal("second run did not report a cache hit")
	}
	if res.Words != storedMeta.Words || res.Coverage.Total != storedMeta.Coverage.Total || res.Coverage.Covered != storedMeta.Coverage.Covered {
		t.Fatalf("cache hit must return stored metrics, got words=%d coverage=%+v want words=%d coverage=%+v", res.Words, res.Coverage, storedMeta.Words, storedMeta.Coverage)
	}
	if llm.calls != 0 {
		t.Fatalf("expected zero LLM calls on cache hit, got %d", llm.calls)
	}
	written, err = os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("out not written on cache hit: %v", err)
	}
	if string(written) != cachedArticle {
		t.Fatalf("cache hit output mismatch: %q != %q", written, cachedArticle)
	}
}

func TestRunCacheReadDisabledRecomputes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := newMemCache()
	cache.Store("cache-key", "cached article", CacheMeta{Words: 2})
	llm := &fakeLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
		Cache:       cache,
		CacheKey:    "cache-key",
		CacheRead:   false,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nSome content paragraph one.", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.CacheHit {
		t.Fatal("CacheRead=false run unexpectedly reported a cache hit")
	}
	if llm.calls == 0 {
		t.Fatal("expected LLM calls when cache reads are disabled")
	}
}

func TestDigestAppendix(t *testing.T) {
	t.Parallel()
	const marker = "Model #1 Genetic Fit is 2.414."
	research := "* " + marker + "\n* Model #2 Genetic Fit is 1.618.\n* Model #3 Genetic Fit is 0.577."
	source := "# Title\n\nSome content paragraph one."

	for _, tc := range []struct {
		name     string
		appendix bool
	}{
		{name: "disabled", appendix: false},
		{name: "enabled", appendix: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			llm := &fakeLLM{
				research: research,
				outline:  "# Draft Title\n\n## Summary\nshort prose only",
				section:  "summary prose without captured table rows",
				edit:     "polished prose without captured table rows",
			}
			opts := Options{
				Style:       "brief",
				OutPath:     filepath.Join(dir, "out.md"),
				FactsPath:   filepath.Join(dir, "facts.compiled.md"),
				ArtifactDir: filepath.Join(dir, "artifacts"),
				ChunkSize:   6000,
				Edit:        true,
				Appendix:    tc.appendix,
			}

			_, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			final, err := os.ReadFile(opts.OutPath)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			output := string(final)
			if strings.Contains("polished prose without captured table rows", "Model #1") {
				t.Fatal("test prose fixture must not contain the research marker")
			}
			if tc.appendix {
				if !strings.Contains(output, "# Appendix: Extracted Facts") {
					t.Fatalf("expected appendix heading, got: %q", output)
				}
				if !strings.Contains(output, marker) {
					t.Fatalf("expected research marker in appendix, got: %q", output)
				}
				return
			}
			if strings.Contains(output, "# Appendix: Extracted Facts") {
				t.Fatalf("did not expect appendix heading, got: %q", output)
			}
		})
	}
}

func TestDigestAppendixTables(t *testing.T) {
	t.Parallel()
	const tableCell = "Orbital Eel"
	source := "# Title\n\n| Specimen | Score |\n|---|---:|\n| " + tableCell + " | 9.5 |\n| River Moth | 7.1 |\n\nSome content paragraph one."
	dir := t.TempDir()
	llm := &fakeLLM{
		research: "* Extracted fact without table cells.",
		outline:  "# Draft Title\n\n## Summary\nshort prose only",
		section:  "summary prose without captured table rows",
		edit:     "polished prose without captured table rows",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
		Appendix:    true,
	}

	_, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	final, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	output := string(final)
	if !strings.Contains(output, "# Appendix: Extracted Facts") {
		t.Fatalf("expected appendix heading, got: %q", output)
	}
	if !strings.Contains(output, tableCell) {
		t.Fatalf("expected rendered table cell in appendix, got: %q", output)
	}
	if !strings.Contains(output, "## Research Notes") {
		t.Fatalf("expected research notes heading after structured tables, got: %q", output)
	}
}

func TestRunReuseFactsSkipsExtraction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	factsPath := filepath.Join(dir, "facts.compiled.md")
	if err := os.WriteFile(factsPath, []byte("## chunk-001\n\n- fact"), 0o644); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	llm := &fakeLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   factsPath,
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		ReuseFacts:  true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "ignored source", opts)
	if err != nil {
		t.Fatalf("Run reuse: %v", err)
	}
	if res.ChunkCount != 0 {
		t.Fatalf("reuse must not chunk, got ChunkCount=%d", res.ChunkCount)
	}
	if llm.calls != 3 {
		t.Fatalf("reuse must call LLM 3x (outline + 2 sections), got %d", llm.calls)
	}
	if !res.UnverifiedFacts {
		t.Fatal("explicit facts reuse without a matching marker must be flagged UnverifiedFacts")
	}
}

func TestRunResumeReusesArtifactsAndWritesLedger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")
	if err := os.MkdirAll(filepath.Join(artifactDir, "responses"), 0o755); err != nil {
		t.Fatalf("mkdir responses: %v", err)
	}
	if err := writeRunMarker(artifactDir, markerFor("# Title\n\nbody", 6000, 0)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	seed := map[string]string{
		factsPath: "## chunk-001\n\n- seeded fact",
		filepath.Join(artifactDir, "responses", "outline.md"):            "# Draft Title\n\n## Section One\nwhat one covers\n\n## Section Two\nwhat two covers",
		filepath.Join(artifactDir, "responses", "section-001.md"):        "seeded section one",
		filepath.Join(artifactDir, "responses", "section-002.md"):        "seeded section two",
		filepath.Join(artifactDir, "responses", "section-001.edited.md"): "seeded edit one",
		filepath.Join(artifactDir, "responses", "section-002.edited.md"): "seeded edit two",
	}
	for path, content := range seed {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}

	llm := &fakeLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   factsPath,
		ArtifactDir: artifactDir,
		ChunkSize:   6000,
		Edit:        true,
		Resume:      true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run resume: %v", err)
	}
	if llm.calls != 0 {
		t.Fatalf("resume should not call LLM for complete artifacts, got %d calls", llm.calls)
	}
	if !res.ReusedFacts || !res.ReusedOutline || res.ReusedSections != 2 || res.ReusedEdits != 2 {
		t.Fatalf("unexpected reuse result: %+v", res)
	}
	final, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(final), "seeded edit one") || !strings.Contains(string(final), "seeded edit two") {
		t.Fatalf("final output did not use resumed edits: %q", final)
	}
	ledger, err := os.ReadFile(filepath.Join(artifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledger), `"action":"reuse"`) {
		t.Fatalf("ledger should record reuse events, got: %s", ledger)
	}
}

func TestRunResumeReusesPartialResearchWithoutCompiledFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(filepath.Join(artifactDir, "responses"), 0o755); err != nil {
		t.Fatalf("mkdir responses: %v", err)
	}
	source := "# One\n\n" + strings.Repeat("alpha ", 250) + "\n\n# Two\n\n" + strings.Repeat("beta ", 250)
	chunks, err := ChunkSource(source, 1000, 0)
	if err != nil {
		t.Fatalf("chunk source: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("test source produced %d chunk(s), want at least 2", len(chunks))
	}
	if cerr := os.WriteFile(filepath.Join(artifactDir, "responses", "chunk-001.md"), []byte("- reused fact"), 0o644); cerr != nil {
		t.Fatalf("seed research response: %v", cerr)
	}
	if cerr := writeRunMarker(artifactDir, markerFor(source, 1000, 0)); cerr != nil {
		t.Fatalf("seed marker: %v", cerr)
	}

	llm := &fakeLLM{research: "- fresh fact", section: "draft body"}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: artifactDir,
		ChunkSize:   1000,
		MaxTokens:   0,
		Resume:      true,
		Edit:        false,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run resume partial research: %v", err)
	}
	if res.ReusedFacts {
		t.Fatal("facts checkpoint was absent, must not report ReusedFacts")
	}
	if res.ReusedChunks != 1 {
		t.Fatalf("ReusedChunks = %d, want 1", res.ReusedChunks)
	}
	facts, err := os.ReadFile(opts.FactsPath)
	if err != nil {
		t.Fatalf("read compiled facts: %v", err)
	}
	if !strings.Contains(string(facts), "- reused fact") || !strings.Contains(string(facts), "- fresh fact") {
		t.Fatalf("compiled facts should contain reused and fresh research, got: %s", facts)
	}
	ledger, err := os.ReadFile(filepath.Join(artifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledger), `"stage":"research"`) || !strings.Contains(string(ledger), `"action":"reuse"`) || !strings.Contains(string(ledger), `"action":"call"`) {
		t.Fatalf("ledger should record research reuse and calls, got: %s", ledger)
	}
}

func TestResearchCacheHitSkipsResearchCallAndWritesArtifact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# One\n\nbody"
	chunks, err := ChunkSource(source, 1000, 0)
	if err != nil {
		t.Fatalf("ChunkSource: %v", err)
	}
	cache := newMemResearchCache()
	cache.hits[chunks[0].Text] = "- cached fact"
	llm := &fakeLLM{section: "draft body"}
	opts := Options{
		Style:         "brief",
		OutPath:       filepath.Join(dir, "out.md"),
		FactsPath:     filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:   filepath.Join(dir, "artifacts"),
		ChunkSize:     1000,
		MaxTokens:     0,
		Concurrency:   1,
		Edit:          false,
		ResearchCache: cache,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ReusedChunks != 1 {
		t.Fatalf("ReusedChunks = %d, want 1", res.ReusedChunks)
	}
	for _, prompt := range llm.prompts {
		if strings.Contains(prompt, "RESEARCH chunk-") {
			t.Fatalf("research prompt should not be called on cache hit: %s", prompt)
		}
	}
	artifact, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "chunk-001.md"))
	if err != nil {
		t.Fatalf("read research artifact: %v", err)
	}
	if string(artifact) != "- cached fact" {
		t.Fatalf("research artifact = %q, want cached fact", artifact)
	}
	ledger, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledger), `"stage":"research"`) || !strings.Contains(string(ledger), `"action":"cache"`) {
		t.Fatalf("ledger should record research cache hit, got: %s", ledger)
	}
}

func TestResearchCacheStoresSuccessfulResearch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# One\n\nbody"
	chunks, err := ChunkSource(source, 1000, 0)
	if err != nil {
		t.Fatalf("ChunkSource: %v", err)
	}
	cache := newMemResearchCache()
	llm := &fakeLLM{research: "- fresh fact", section: "draft body"}
	opts := Options{
		Style:         "brief",
		OutPath:       filepath.Join(dir, "out.md"),
		FactsPath:     filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:   filepath.Join(dir, "artifacts"),
		ChunkSize:     1000,
		MaxTokens:     0,
		Concurrency:   1,
		Edit:          false,
		ResearchCache: cache,
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := cache.stores[chunks[0].Text]; got != "- fresh fact" {
		t.Fatalf("stored research = %q, want fresh fact", got)
	}
}

func TestCascadeEscalatesWeakFreshResearch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# Mission\n\nThe Alpha Beta mission recorded 101 kg, 202 km, 303 volts, 404 amps, 505 watts, NASA, and Cape Canaveral."
	research := &fakeLLM{research: "- The Alpha Beta mission recorded 101 kg.", section: "draft body"}
	escalation := &fakeLLM{research: "- The Alpha Beta mission recorded 202 km, 303 volts, 404 amps, and 505 watts.", section: "unused"}
	writer := &fakeLLM{section: "draft body"}
	opts := Options{
		Style:            "brief",
		OutPath:          filepath.Join(dir, "out.md"),
		FactsPath:        filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:      filepath.Join(dir, "artifacts"),
		ChunkSize:        6000,
		MaxTokens:        0,
		Concurrency:      1,
		Edit:             false,
		Cascade:          true,
		CascadeThreshold: 0.9,
	}
	res, err := Run(context.Background(), RoleCompleters{
		Research:           research,
		ResearchEscalation: escalation,
		Fuse:               writer,
		Outline:            writer,
		Section:            writer,
		Edit:               writer,
	}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ReusedChunks != 0 {
		t.Fatalf("ReusedChunks = %d, want 0", res.ReusedChunks)
	}
	if escalation.calls != 1 {
		t.Fatalf("escalation calls = %d, want 1", escalation.calls)
	}
	facts, err := os.ReadFile(opts.FactsPath)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	for _, want := range []string{"101 kg", "202 km", "303 volts", "505 watts"} {
		if !strings.Contains(string(facts), want) {
			t.Fatalf("compiled facts missing %q:\n%s", want, facts)
		}
	}
	ledger, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledger), `"stage":"research"`) || !strings.Contains(string(ledger), `"action":"escalate"`) {
		t.Fatalf("ledger should record research escalation, got: %s", ledger)
	}
}

func TestCascadeThresholdZeroDoesNotEscalate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# Mission\n\nThe Alpha Beta mission recorded 101 kg, 202 km, 303 volts, 404 amps, 505 watts, NASA, and Cape Canaveral."
	research := &fakeLLM{research: "- The Alpha Beta mission recorded 101 kg.", section: "draft body"}
	escalation := &fakeLLM{research: "- The Alpha Beta mission recorded 202 km.", section: "unused"}
	writer := &fakeLLM{section: "draft body"}
	opts := Options{
		Style:            "brief",
		OutPath:          filepath.Join(dir, "out.md"),
		FactsPath:        filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:      filepath.Join(dir, "artifacts"),
		ChunkSize:        6000,
		MaxTokens:        0,
		Concurrency:      1,
		Edit:             false,
		Cascade:          true,
		CascadeThreshold: 0,
	}
	if _, err := Run(context.Background(), RoleCompleters{
		Research:           research,
		ResearchEscalation: escalation,
		Fuse:               writer,
		Outline:            writer,
		Section:            writer,
		Edit:               writer,
	}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if escalation.calls != 0 {
		t.Fatalf("escalation calls = %d, want 0", escalation.calls)
	}
}

func TestDocContextPrependsSameHeaderToResearchPrompts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# Alpha\n\n" + strings.Repeat("alpha ", 260) + "\n\n## Beta\n\n" + strings.Repeat("beta ", 260)
	llm := &fakeLLM{
		docHeader: "TITLE: Alpha\nSYNOPSIS: Alpha and beta are covered.",
		research:  "- extracted fact",
		section:   "draft body",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   1000,
		MaxTokens:   0,
		Concurrency: 1,
		Edit:        false,
		DocContext:  true,
	}

	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	headerPrefix := "DOC_HEADER\nTITLE: Alpha\nSYNOPSIS: Alpha and beta are covered.\n---\n"
	researchPrompts := 0
	for _, prompt := range llm.prompts {
		if !strings.Contains(prompt, "RESEARCH chunk-") {
			continue
		}
		researchPrompts++
		if !strings.HasPrefix(prompt, headerPrefix) {
			t.Fatalf("research prompt missing shared header prefix:\n%s", prompt)
		}
	}
	if researchPrompts < 2 {
		t.Fatalf("expected at least two research prompts, got %d (%v)", researchPrompts, llm.prompts)
	}
	headerArtifact, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "doc-context.md"))
	if err != nil {
		t.Fatalf("read header artifact: %v", err)
	}
	if !strings.Contains(string(headerArtifact), "TITLE: Alpha") {
		t.Fatalf("unexpected header artifact: %s", headerArtifact)
	}
	ledger, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledger), `"stage":"doc-context"`) || !strings.Contains(string(ledger), `"action":"call"`) {
		t.Fatalf("ledger should record doc-context call, got: %s", ledger)
	}
}

func TestDocContextResumeReusesHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	responsesDir := filepath.Join(artifactDir, "responses")
	if err := os.MkdirAll(responsesDir, 0o755); err != nil {
		t.Fatalf("mkdir responses: %v", err)
	}
	source := "# Alpha\n\n" + strings.Repeat("alpha ", 260) + "\n\n## Beta\n\n" + strings.Repeat("beta ", 260)
	if err := writeRunMarker(artifactDir, markerFor(source, 1000, 0)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(responsesDir, "doc-context.md"), []byte("TITLE: Reused\nSYNOPSIS: Reused header."), 0o644); err != nil {
		t.Fatalf("seed doc-context: %v", err)
	}
	llm := &fakeLLM{research: "- extracted fact", section: "draft body"}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: artifactDir,
		ChunkSize:   1000,
		MaxTokens:   0,
		Concurrency: 1,
		Resume:      true,
		Edit:        false,
		DocContext:  true,
	}

	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "DOC_CONTEXT") {
			t.Fatalf("resume should not regenerate doc-context, prompts: %v", llm.prompts)
		}
		if strings.Contains(prompt, "RESEARCH chunk-") && !strings.HasPrefix(prompt, "DOC_HEADER\nTITLE: Reused\nSYNOPSIS: Reused header.\n---\n") {
			t.Fatalf("research prompt did not use reused header:\n%s", prompt)
		}
	}
	ledger, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledger), `"stage":"doc-context"`) || !strings.Contains(string(ledger), `"action":"reuse"`) {
		t.Fatalf("ledger should record doc-context reuse, got: %s", ledger)
	}
}

func TestRunResumeIgnoresPlaceholderArtifacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(filepath.Join(artifactDir, "responses"), 0o755); err != nil {
		t.Fatalf("mkdir responses: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "responses", "chunk-001.md"), []byte("_(this section could not be generated)_"), 0o644); err != nil {
		t.Fatalf("seed placeholder: %v", err)
	}
	if err := writeRunMarker(artifactDir, markerFor("# One\n\nbody", 1000, 0)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	llm := &fakeLLM{research: "- fresh fact", section: "draft body"}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: artifactDir,
		ChunkSize:   1000,
		MaxTokens:   0,
		Resume:      true,
		Edit:        false,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# One\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run resume placeholder: %v", err)
	}
	if res.ReusedChunks != 0 {
		t.Fatalf("ReusedChunks = %d, want 0 for placeholder", res.ReusedChunks)
	}
	facts, err := os.ReadFile(opts.FactsPath)
	if err != nil {
		t.Fatalf("read compiled facts: %v", err)
	}
	if strings.Contains(string(facts), "could not be generated") {
		t.Fatalf("placeholder leaked into compiled facts: %s", facts)
	}
	if !strings.Contains(string(facts), "- fresh fact") {
		t.Fatalf("fresh research missing from compiled facts: %s", facts)
	}
}

func TestResearchHardErrorFailsFast(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &errLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
	}
	_, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody one.", opts)
	if err == nil {
		t.Fatal("expected hard extract error to abort the run, got nil")
	}
	if !strings.Contains(err.Error(), "research") {
		t.Fatalf("expected research error, got: %v", err)
	}
}

// errLLM fails every research call with a hard error (exercises fail-fast).
type errLLM struct{}

func (errLLM) Complete(_ context.Context, _ string) (string, error) {
	return "", errors.New("boom")
}

// emptyLLM returns the empty-response sentinel for every call (exercises the
// soft-skip path: an empty model response must be recorded, not abort the run).
type emptyLLM struct{}

func (emptyLLM) Complete(_ context.Context, _ string) (string, error) {
	return "", ai.ErrEmptyResponse
}

func TestResearchEmptyResponseIsSoftSkip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
	}
	llm := emptyLLM{}
	_, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody one.", opts)
	if err == nil {
		t.Fatal("expected a no-facts error when every chunk is empty, got nil")
	}
	// Empty responses must NOT surface as a hard research abort; they are
	// skipped, leaving the run to fail later with "no facts extracted".
	if strings.Contains(err.Error(), "research") {
		t.Fatalf("empty response should be soft-skipped, not a research abort: %v", err)
	}
	if !strings.Contains(err.Error(), "no facts extracted") {
		t.Fatalf("expected no-facts error, got: %v", err)
	}
}

// transientErr is a generic (non-systemic) error: retryComplete must retry it.
var transientErr = errors.New("transient blip")

// flakySectionLLM fails the first failN SECTION calls with a transient error,
// then succeeds; other roles always succeed. Exercises retry-then-recover.
type flakySectionLLM struct {
	failN int
}

func (f *flakySectionLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "OUTLINE"):
		return "# T\n\n## Only Section\nthe one section", nil
	case strings.HasPrefix(prompt, "SECTION"):
		if f.failN > 0 {
			f.failN--
			return "", transientErr
		}
		return "written body", nil
	case strings.HasPrefix(prompt, "EDIT"):
		return "edited body", nil
	default:
		return "- a fact", nil
	}
}

func TestSectionRetrySucceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &flakySectionLLM{failN: 2}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Retries:     3,
		RetryDelay:  time.Millisecond,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody one.", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.FailedSections) != 0 {
		t.Fatalf("retry should have recovered, got FailedSections=%v", res.FailedSections)
	}
	out, _ := os.ReadFile(opts.OutPath)
	if !strings.Contains(string(out), "written body") {
		t.Fatalf("expected recovered section body, got: %q", out)
	}
}

// deadSectionLLM always fails SECTION calls with a transient error so retries
// exhaust and the section degrades; other roles succeed.
type deadSectionLLM struct{}

func (deadSectionLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "OUTLINE"):
		return "# T\n\n## Only Section\nthe one section", nil
	case strings.HasPrefix(prompt, "SECTION"):
		return "", transientErr
	default:
		return "- a fact", nil
	}
}

func TestSectionDegradesAfterRetries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := deadSectionLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Retries:     2,
		RetryDelay:  time.Millisecond,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody one.", opts)
	if err != nil {
		t.Fatalf("degradation must not abort the run: %v", err)
	}
	if len(res.FailedSections) != 1 {
		t.Fatalf("expected 1 failed section recorded, got %v", res.FailedSections)
	}
	out, _ := os.ReadFile(opts.OutPath)
	if !strings.Contains(string(out), "could not be generated") {
		t.Fatalf("expected stub placeholder in output, got: %q", out)
	}
}

// cancelingEditLLM simulates the user interrupting the command while the edit
// stage is in flight. Cancellation must abort the run, not degrade every
// remaining edit and report success.
type cancelingEditLLM struct {
	cancel context.CancelFunc
}

func (f cancelingEditLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "OUTLINE"):
		return "# T\n\n## Only Section\nthe one section", nil
	case strings.HasPrefix(prompt, "SECTION"):
		return "written body", nil
	case strings.HasPrefix(prompt, "EDIT"):
		f.cancel()
		return "", context.Canceled
	default:
		return "- a fact", nil
	}
}

func TestEditCancellationAbortsRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	llm := cancelingEditLLM{cancel: cancel}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Retries:     2,
		RetryDelay:  time.Millisecond,
		Edit:        true,
	}

	_, err := Run(ctx, RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody one.", opts)
	if err == nil {
		t.Fatal("expected cancellation to abort the run, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if !strings.Contains(err.Error(), "edit section") {
		t.Fatalf("expected edit-stage error context, got: %v", err)
	}
	if _, statErr := os.Stat(opts.OutPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final output should not be written after cancellation, stat err: %v", statErr)
	}
}

// capturingEditLLM records each EDIT call's full rendered prompt (which contains
// the {{ARTICLE}} context) and returns distinct write/edit bodies keyed by
// section heading, so a test can verify the edit stage compounds prior edits.
type capturingEditLLM struct {
	editPrompts []string
}

func (f *capturingEditLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "OUTLINE"):
		return "# Draft Title\n\n## Section One\nwhat one covers\n\n## Section Two\nwhat two covers", nil
	case strings.HasPrefix(prompt, "SECTION"):
		// "SECTION [style] HEADING|INTENT|..." — distinct write body per section.
		return "WRITE-" + delimited(prompt, "] ", "|"), nil
	case strings.HasPrefix(prompt, "EDIT"):
		// "EDIT [style] HEADING :: ARTICLE :: FACTS"
		f.editPrompts = append(f.editPrompts, prompt)
		return "EDITED-" + delimited(prompt, "] ", " :: "), nil
	default:
		return "- a fact", nil
	}
}

// delimited returns the substring of s after the first occurrence of after, up
// to the next occurrence of before.
func delimited(s, after, before string) string {
	rest := s[strings.Index(s, after)+len(after):]
	return rest[:strings.Index(rest, before)]
}

// TestEditStageCompoundsPriorEdits guards against the edit stage rewriting each
// section against a frozen original draft: section N's edit must see prior
// sections' EDITED bodies, not their stale write-stage bodies.
func TestEditUsesStableDraft(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &capturingEditLLM{}
	source := "# Title\n\nSome content paragraph one.\n\n## Section\n\nMore content here."
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(llm.editPrompts) < 2 {
		t.Fatalf("expected >=2 edit calls, got %d", len(llm.editPrompts))
	}
	// Cache-priority: every edit call sees the SAME original draft, so the 2nd
	// edit's full-draft context still shows section one's ORIGINAL write body.
	second := llm.editPrompts[1]
	if !strings.Contains(second, "WRITE-Section One") {
		t.Fatalf("edit draft not stable: 2nd edit context missing original write body; got: %q", second)
	}
	if priorStart := strings.Index(second, " :: PRIOR "); priorStart < 0 {
		t.Fatalf("edit prompt missing prior accepted block: %q", second)
	} else {
		draftBlock := second[:priorStart]
		priorBlock := second[priorStart:]
		if strings.Contains(draftBlock, "EDITED-Section One") {
			t.Fatalf("edit draft changed between calls (not cacheable): 2nd edit saw prior edit in draft block; got: %q", second)
		}
		if !strings.Contains(priorBlock, "EDITED-Section One") {
			t.Fatalf("edit prompt missing accepted prior edit: %q", second)
		}
	}
}

func TestNumberFacts(t *testing.T) {
	t.Parallel()
	in := "## chunk-001\n\n- alpha\n- beta\n\n---\n\n## chunk-002\n\n- gamma"
	units, numbered := numberFacts(in)
	if len(units) != 3 {
		t.Fatalf("units = %d, want 3", len(units))
	}
	if units[0].line != "- alpha" || units[2].line != "- gamma" {
		t.Fatalf("unexpected unit lines: %+v", units)
	}
	for _, want := range []string{"[F1] alpha", "[F2] beta", "[F3] gamma", "## chunk-001"} {
		if !strings.Contains(numbered, want) {
			t.Fatalf("numbered missing %q:\n%s", want, numbered)
		}
	}
}

func TestParseOutlineFactIDs(t *testing.T) {
	t.Parallel()
	out := "# Title\n\n## One\nwhat one covers\nFacts: F1, F3\n\n## Two\nwhat two covers\nfacts: F2\n"
	title, secs := parseOutline(out)
	if title != "Title" || len(secs) != 2 {
		t.Fatalf("title=%q secs=%d", title, len(secs))
	}
	if secs[0].intent != "what one covers" {
		t.Fatalf("intent leaked facts line: %q", secs[0].intent)
	}
	if len(secs[0].factIDs) != 2 || secs[0].factIDs[0] != 1 || secs[0].factIDs[1] != 3 {
		t.Fatalf("sec0 factIDs = %v, want [1 3]", secs[0].factIDs)
	}
	if len(secs[1].factIDs) != 1 || secs[1].factIDs[0] != 2 {
		t.Fatalf("sec1 factIDs = %v, want [2]", secs[1].factIDs)
	}
}

// capturingSectionLLM records each SECTION call's full rendered prompt and
// returns a caller-supplied outline, so tests can assert which facts each
// section write received.
type capturingSectionLLM struct {
	outline        string
	sectionPrompts []string
}

func (f *capturingSectionLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "OUTLINE"):
		return f.outline, nil
	case strings.HasPrefix(prompt, "SECTION"):
		f.sectionPrompts = append(f.sectionPrompts, prompt)
		return "written body", nil
	case strings.HasPrefix(prompt, "EDIT"):
		return "edited body", nil
	default:
		return "- alpha\n- beta\n- gamma", nil
	}
}

func TestSectionFactRoutingOn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &capturingSectionLLM{
		outline: "# Title\n\n## One\ncovers one\nFacts: F1\n\n## Two\ncovers two\nFacts: F2\n",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# T\n\nbody paragraph.", opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// F3 (gamma) is unassigned -> catch-all section -> 3 write calls.
	if len(llm.sectionPrompts) != 3 {
		t.Fatalf("section calls = %d, want 3 (incl catch-all)", len(llm.sectionPrompts))
	}
	if !strings.Contains(llm.sectionPrompts[0], "alpha") || strings.Contains(llm.sectionPrompts[0], "beta") || strings.Contains(llm.sectionPrompts[0], "gamma") {
		t.Fatalf("section One should get only alpha; got: %q", llm.sectionPrompts[0])
	}
	if !strings.Contains(llm.sectionPrompts[1], "beta") || strings.Contains(llm.sectionPrompts[1], "alpha") {
		t.Fatalf("section Two should get only beta; got: %q", llm.sectionPrompts[1])
	}
	if !strings.Contains(llm.sectionPrompts[2], "gamma") {
		t.Fatalf("catch-all should get gamma; got: %q", llm.sectionPrompts[2])
	}
	final, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if !strings.Contains(string(final), "## Additional details") {
		t.Fatalf("missing catch-all section in output:\n%s", final)
	}
}

func TestRouteSectionFactsFirstHomeWins(t *testing.T) {
	t.Parallel()
	// Same fact ID in two sections: only the first keeps it.
	assigned := make(map[int]bool)
	ids, dropped := routeSectionFacts(assigned, []int{1, 2, 3})
	if dropped != 0 || !slicesEq(ids, []int{1, 2, 3}) {
		t.Fatalf("first section: dropped=%d ids=%v, want 0 / [1 2 3]", dropped, ids)
	}
	ids, dropped = routeSectionFacts(assigned, []int{3, 4})
	if dropped != 1 || !slicesEq(ids, []int{4}) {
		t.Fatalf("second section: dropped=%d ids=%v, want 1 / [4]", dropped, ids)
	}
}

func slicesEq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSectionFactRoutingOff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &capturingSectionLLM{
		outline: "# Title\n\n## One\ncovers one\n\n## Two\ncovers two\n",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# T\n\nbody paragraph.", opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(llm.sectionPrompts) != 2 {
		t.Fatalf("section calls = %d, want 2 (no catch-all when routing off)", len(llm.sectionPrompts))
	}
	for i, p := range llm.sectionPrompts {
		for _, want := range []string{"alpha", "beta", "gamma"} {
			if !strings.Contains(p, want) {
				t.Fatalf("routing-off section %d missing %q (should get all facts); got: %q", i, want, p)
			}
		}
	}
}

func TestRunPrependsContextToWritingStages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &ctxCapturingLLM{}
	set := testPrompts()
	set.ContextPreamble = "CTXPREAMBLE[{{CONTEXT}}]"
	source := "# Title\n\nSome content paragraph one.\n\n## Section\n\nMore content here."
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
		Context:     "STEER-ME",
	}
	_, err := Run(context.Background(),
		RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm},
		set, source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	const marker = "CTXPREAMBLE[STEER-ME]"
	if len(llm.outline) == 0 || len(llm.section) == 0 || len(llm.edit) == 0 {
		t.Fatalf("expected outline/section/edit calls, got o=%d s=%d e=%d", len(llm.outline), len(llm.section), len(llm.edit))
	}
	for _, p := range llm.outline {
		if !strings.Contains(p, marker) {
			t.Fatalf("outline prompt missing context marker: %q", p)
		}
	}
	for _, p := range llm.section {
		if !strings.Contains(p, marker) {
			t.Fatalf("section prompt missing context marker: %q", p)
		}
	}
	for _, p := range llm.edit {
		if !strings.Contains(p, marker) {
			t.Fatalf("edit prompt missing context marker: %q", p)
		}
	}
	for _, p := range llm.research {
		if strings.Contains(p, marker) {
			t.Fatalf("research prompt unexpectedly contains context marker: %q", p)
		}
	}
}

type ctxCapturingLLM struct {
	research, outline, section, edit []string
}

func (f *ctxCapturingLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.Contains(prompt, "OUTLINE"):
		f.outline = append(f.outline, prompt)
		return "# Draft Title\n\n## Section One\nwhat one covers\n\n## Section Two\nwhat two covers", nil
	case strings.Contains(prompt, "SECTION"):
		f.section = append(f.section, prompt)
		return "written body", nil
	case strings.Contains(prompt, "EDIT"):
		f.edit = append(f.edit, prompt)
		return "edited body", nil
	default:
		f.research = append(f.research, prompt)
		return "- a fact", nil
	}
}

func TestRunRepairRaisesCoverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Research carries specifics ($1,200 / 2021 / Acme Corp); the write+edit
	// bodies drop them, so the pre-repair article is lossy. The repair pass
	// returns an article that reinstates them, raising Covered and clearing
	// Missing.
	llm := &fakeLLM{
		research: "- Revenue was $1,200 in 2021 at Acme Corp.",
		outline:  "# Draft Title\n\n## Summary\nshort prose only",
		section:  "A summary with no figures whatsoever.",
		edit:     "A polished summary with no figures whatsoever.",
		repair:   "# Draft Title\n\n## Summary\n\nRevenue was $1,200 in 2021 at Acme Corp.",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
		Repair:      true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody.", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Coverage.Covered == 0 {
		t.Fatalf("repair should have raised covered specifics, got %+v", res.Coverage)
	}
	if len(res.Coverage.Missing) != 0 {
		t.Fatalf("repair should have cleared missing specifics, got %v", res.Coverage.Missing)
	}
	if res.Words == 0 {
		t.Fatal("expected res.Words to be set")
	}
	out, _ := os.ReadFile(opts.OutPath)
	if !strings.Contains(string(out), "$1,200") || !strings.Contains(string(out), "Acme Corp") {
		t.Fatalf("repaired specifics missing from output: %q", out)
	}
}

func TestRunLedgerRecordsTokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{}
	source := "# Title\n\nSome content paragraph one."
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        true,
		// fakeLLM never touches ai.Client, so synthesize cumulative usage that
		// grows by a fixed amount per Complete call; each serial stage's delta is
		// then non-zero, proving the ledger records per-stage token usage.
		Usage: func() (prompt, cached, output int64) {
			n := int64(llm.calls)
			return n * 10, n * 2, n * 5
		},
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var sawCallWithTokens bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev ledgerEvent
		if uerr := json.Unmarshal([]byte(line), &ev); uerr != nil {
			t.Fatalf("unmarshal ledger line %q: %v", line, uerr)
		}
		if ev.Action == "call" && ev.OutputTokens > 0 {
			sawCallWithTokens = true
			if ev.PromptTokens <= 0 {
				t.Fatalf("call event has output tokens but no prompt tokens: %q", line)
			}
		}
	}
	if !sawCallWithTokens {
		t.Fatalf("expected a call event with non-zero output_tokens, ledger:\n%s", data)
	}
}

func TestResumeInvalidatedWhenMarkerMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(filepath.Join(artifactDir, "responses"), 0o755); err != nil {
		t.Fatalf("mkdir responses: %v", err)
	}
	stale := filepath.Join(artifactDir, "responses", "chunk-001.md")
	if err := os.WriteFile(stale, []byte("- stale fact from another source"), 0o644); err != nil {
		t.Fatalf("seed stale response: %v", err)
	}
	llm := &fakeLLM{research: "- fresh fact", section: "draft body"}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(artifactDir, "facts.compiled.md"),
		ArtifactDir: artifactDir,
		ChunkSize:   1000,
		Resume:      true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# One\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ReusedChunks != 0 || res.ReusedFacts {
		t.Fatalf("missing marker must disable reuse, got %+v", res)
	}
	facts, err := os.ReadFile(opts.FactsPath)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if strings.Contains(string(facts), "stale fact") {
		t.Fatalf("stale artifact leaked into compiled facts: %s", facts)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, markerName)); err != nil {
		t.Fatalf("marker not stamped after invalidation: %v", err)
	}
}

func TestResumeInvalidatedWhenSourceChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(filepath.Join(artifactDir, "responses"), 0o755); err != nil {
		t.Fatalf("mkdir responses: %v", err)
	}
	if err := writeRunMarker(artifactDir, markerFor("# Old\n\nold body", 1000, 0)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	staleFacts := filepath.Join(artifactDir, "facts.compiled.md")
	if err := os.WriteFile(staleFacts, []byte("## chunk-001\n\n- old-source fact"), 0o644); err != nil {
		t.Fatalf("seed stale facts: %v", err)
	}
	llm := &fakeLLM{research: "- fresh fact", section: "draft body"}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   staleFacts,
		ArtifactDir: artifactDir,
		ChunkSize:   1000,
		Resume:      true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# New\n\nnew body", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ReusedFacts || res.ReusedChunks != 0 {
		t.Fatalf("mismatched marker must disable reuse, got %+v", res)
	}
	facts, err := os.ReadFile(staleFacts)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if strings.Contains(string(facts), "old-source fact") {
		t.Fatalf("stale facts survived invalidation: %s", facts)
	}
	if !ArtifactsMatchSource(artifactDir, "# New\n\nnew body", 1000, 0) {
		t.Fatal("marker not rebound to the new source")
	}
}

func TestResumeIgnoresFactsOutsideArtifactDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := writeRunMarker(artifactDir, markerFor("# One\n\nbody", 1000, 0)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	externalFacts := filepath.Join(dir, "facts.compiled.md")
	if err := os.WriteFile(externalFacts, []byte("## chunk-001\n\n- external fact"), 0o644); err != nil {
		t.Fatalf("seed external facts: %v", err)
	}
	llm := &fakeLLM{research: "- fresh fact", section: "draft body"}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   externalFacts,
		ArtifactDir: artifactDir,
		ChunkSize:   1000,
		Resume:      true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# One\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ReusedFacts {
		t.Fatal("resume must not reuse a facts checkpoint outside the artifact dir")
	}
	facts, err := os.ReadFile(externalFacts)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if !strings.Contains(string(facts), "- fresh fact") {
		t.Fatalf("extraction should have overwritten the external checkpoint, got: %s", facts)
	}
}

func TestReuseFactsExplicitBypassesMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := writeRunMarker(artifactDir, markerFor("# Other\n\nsource", 6000, 0)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")
	if err := os.WriteFile(factsPath, []byte("## chunk-001\n\n- kept fact"), 0o644); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	llm := &fakeLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   factsPath,
		ArtifactDir: artifactDir,
		ChunkSize:   6000,
		ReuseFacts:  true,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "ignored source", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.ReusedFacts {
		t.Fatal("explicit --reuse-facts must be honored regardless of the marker")
	}
	if !res.UnverifiedFacts {
		t.Fatal("facts reused under a mismatched marker must be flagged unverified")
	}
	if _, err := os.Stat(factsPath); err != nil {
		t.Fatalf("explicitly reused facts file must never be deleted: %v", err)
	}
}

func TestShouldStoreArticle(t *testing.T) {
	t.Parallel()
	cache := newMemCache()
	base := Options{Cache: cache, CacheKey: "k"}
	cases := []struct {
		name string
		opts Options
		res  Result
		want bool
	}{
		{"nil StoreOK stores", base, Result{}, true},
		{"accepting StoreOK stores", withStoreOK(base, true), Result{}, true},
		{"rejecting StoreOK skips", withStoreOK(base, false), Result{}, false},
		{"failed chunk skips", base, Result{FailedChunks: []string{"chunk-001"}}, false},
		{"failed section skips", base, Result{FailedSections: []string{"s"}}, false},
		{"failed edit skips", base, Result{FailedEdits: []string{"s"}}, false},
		{"unverified facts skip", base, Result{UnverifiedFacts: true}, false},
		{"nil cache skips", Options{CacheKey: "k"}, Result{}, false},
		{"empty key skips", Options{Cache: cache}, Result{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldStoreArticle(tc.opts, &tc.res); got != tc.want {
				t.Fatalf("shouldStoreArticle = %v, want %v", got, tc.want)
			}
		})
	}
}

func withStoreOK(opts Options, ok bool) Options {
	opts.StoreOK = func(*Result) bool { return ok }
	return opts
}

func TestRunSkipsStoreWhenStoreOKRejects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := newMemCache()
	var sawWords int
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Cache:       cache,
		CacheKey:    "cache-key",
		CacheRead:   true,
		StoreOK: func(r *Result) bool {
			sawWords = r.Words
			return false
		},
	}
	llm := &fakeLLM{}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nSome content paragraph one.", opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cache.stores != 0 {
		t.Fatalf("rejected article must not be stored, got %d stores", cache.stores)
	}
	if sawWords == 0 {
		t.Fatal("StoreOK must see the populated result (Words was zero)")
	}
}

func TestRunSkipsStoreOnUnverifiedFactsReuse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")
	if err := os.WriteFile(factsPath, []byte("## chunk-001\n\n- kept fact"), 0o644); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	cache := newMemCache()
	llm := &fakeLLM{}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   factsPath,
		ArtifactDir: artifactDir,
		ChunkSize:   6000,
		ReuseFacts:  true,
		Cache:       cache,
		CacheKey:    "cache-key",
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "ignored source", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.UnverifiedFacts {
		t.Fatal("expected UnverifiedFacts for markerless explicit reuse")
	}
	if cache.stores != 0 {
		t.Fatalf("unverified-facts article must not be cached, got %d stores", cache.stores)
	}
}

// lockedUsageLLM wraps fakeLLM with a mutex so concurrent research calls are
// race-free, and synthesizes cumulative usage: +10 prompt, +2 cached,
// +5 output per completed call.
type lockedUsageLLM struct {
	mu    sync.Mutex
	inner fakeLLM
	calls int64
}

func (l *lockedUsageLLM) Complete(ctx context.Context, prompt string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return l.inner.Complete(ctx, prompt)
}

func (l *lockedUsageLLM) usage() (prompt, cached, output int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls * 10, l.calls * 2, l.calls * 5
}

func TestLedgerTokenConservationUnderConcurrency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &lockedUsageLLM{}
	source := "# One\n\n" + strings.Repeat("alpha ", 300) + "\n\n# Two\n\n" + strings.Repeat("beta ", 300) + "\n\n# Three\n\n" + strings.Repeat("gamma ", 300)
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   1000,
		Concurrency: 4,
		Edit:        true,
		Usage:       llm.usage,
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var sumPrompt, sumCached, sumOutput int64
	var phaseEvents int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev ledgerEvent
		if uerr := json.Unmarshal([]byte(line), &ev); uerr != nil {
			t.Fatalf("unmarshal ledger line %q: %v", line, uerr)
		}
		if ev.Stage == "research" && ev.Action == "call" && (ev.PromptTokens != 0 || ev.OutputTokens != 0) {
			t.Fatalf("concurrent research call event must carry zero token deltas: %q", line)
		}
		if ev.Stage == "research" && ev.Action == "phase" {
			phaseEvents++
			if ev.PromptTokens <= 0 || ev.OutputTokens <= 0 {
				t.Fatalf("research phase event must carry the stage token delta: %q", line)
			}
		}
		sumPrompt += ev.PromptTokens
		sumCached += ev.CachedTokens
		sumOutput += ev.OutputTokens
	}
	if phaseEvents != 1 {
		t.Fatalf("expected exactly one research phase event, got %d", phaseEvents)
	}
	wantPrompt, wantCached, wantOutput := llm.usage()
	if sumPrompt != wantPrompt || sumCached != wantCached || sumOutput != wantOutput {
		t.Fatalf("ledger deltas not conserved: sum=(%d,%d,%d) want=(%d,%d,%d)",
			sumPrompt, sumCached, sumOutput, wantPrompt, wantCached, wantOutput)
	}
}

func TestDedupeFactsKeepsContinuationWithItsBullet(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	first := "- Revenue grew 12% in Q3\n  continuing into Q4 as well\n- Headcount reached 400"
	second := "- Revenue grew 12% in Q3\n  a different continuation that must not survive\n- New fact"

	got1 := dedupeFacts(first, seen)
	if got1 != first {
		t.Fatalf("first pass changed unseen facts: got %q, want %q", got1, first)
	}

	got2 := dedupeFacts(second, seen)
	if strings.Contains(got2, "different continuation") {
		t.Fatalf("dropped-duplicate bullet's continuation line survived orphaned: got %q", got2)
	}
	if !strings.Contains(got2, "New fact") {
		t.Fatalf("expected non-duplicate bullet to survive: got %q", got2)
	}
	if strings.Contains(got2, "Revenue grew 12%") {
		t.Fatalf("expected duplicate bullet to be dropped: got %q", got2)
	}
}

func TestDedupeFactsFlagsNearDuplicatesWithoutDropping(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	first := "- Gary Douglas Blankenship was born in Kentucky in 1966."
	second := "- Gary D. Blankenship was born in 1966 in Kentucky."

	got1 := dedupeFacts(first, seen)
	if got1 != first {
		t.Fatalf("first pass changed unseen fact: got %q, want %q", got1, first)
	}

	got2 := dedupeFacts(second, seen)
	if !strings.Contains(got2, nearDuplicateFactFlag) {
		t.Fatalf("near duplicate was not flagged: %q", got2)
	}
	if !strings.Contains(got2, second) {
		t.Fatalf("near duplicate fact was dropped instead of retained: %q", got2)
	}
}

func TestDedupeFactsStillDropsExactDuplicates(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	first := "- Exact fact with enough tokens to resemble itself."
	second := "- Exact fact with enough tokens to resemble itself."

	if got := dedupeFacts(first, seen); got != first {
		t.Fatalf("first pass changed unseen fact: got %q, want %q", got, first)
	}
	if got := dedupeFacts(second, seen); got != "" {
		t.Fatalf("exact duplicate should still be dropped, got %q", got)
	}
}

func TestMergeDuplicateSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []section
		want  []section
	}{
		{
			name: "exact duplicate titles",
			input: []section{
				{title: "Key Personnel", factIDs: []int{1, 2}},
				{title: "Key Personnel", factIDs: []int{3}},
				{title: "Budget", factIDs: []int{4}},
			},
			want: []section{
				{title: "Key Personnel", factIDs: []int{1, 2, 3}},
				{title: "Budget", factIDs: []int{4}},
			},
		},
		{
			name: "case and punctuation variant duplicates",
			input: []section{
				{title: "Key Personnel", factIDs: []int{1}},
				{title: "Budget Overview", factIDs: []int{2}},
				{title: "key personnel:", factIDs: []int{3}},
			},
			want: []section{
				{title: "Key Personnel", factIDs: []int{1, 3}},
				{title: "Budget Overview", factIDs: []int{2}},
			},
		},
		{
			name: "no duplicates unchanged",
			input: []section{
				{title: "Key Personnel", factIDs: []int{1}},
				{title: "Budget", factIDs: []int{2}},
				{title: "Timeline", factIDs: []int{3}},
			},
			want: []section{
				{title: "Key Personnel", factIDs: []int{1}},
				{title: "Budget", factIDs: []int{2}},
				{title: "Timeline", factIDs: []int{3}},
			},
		},
		{
			name: "fact ID order preserved after merge",
			input: []section{
				{title: "Key Personnel", factIDs: []int{3, 1}},
				{title: "Budget", factIDs: []int{10}},
				{title: "Key Personnel", factIDs: []int{2, 4}},
			},
			want: []section{
				{title: "Key Personnel", factIDs: []int{3, 1, 2, 4}},
				{title: "Budget", factIDs: []int{10}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			got := mergeDuplicateSections(ctx, tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i].title != tc.want[i].title {
					t.Fatalf("section[%d].title: got %q, want %q", i, got[i].title, tc.want[i].title)
				}
				if !equalIntSlices(got[i].factIDs, tc.want[i].factIDs) {
					t.Fatalf("section[%d].factIDs: got %v, want %v", i, got[i].factIDs, tc.want[i].factIDs)
				}
			}
		})
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
