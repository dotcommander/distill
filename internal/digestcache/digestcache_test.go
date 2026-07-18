package digestcache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/distill/internal/actions/digest"
	"github.com/dotcommander/distill/internal/extractscore"
)

func baseInputs() KeyInputs {
	return KeyInputs{
		Source:                  "the source document body",
		Profile:                 "default",
		BaseURL:                 "",
		ResearchModel:           "research-model",
		ResearchEscalationModel: "research-escalation-model",
		EmbeddingModel:          "embedding-model",
		OutlineModel:            "outline-model",
		WriteModel:              "write-model",
		FuseModel:               "fuse-model",
		EditModel:               "edit-model",
		Style:                   "brief",
		ChunkSize:               6000,
		MaxTokens:               4000,
		Fuse:                    true,
		Edit:                    true,
		Appendix:                true,
		Repair:                  true,
		DocContext:              true,
		Cite:                    true,
		Cascade:                 true,
		CascadeThreshold:        0.55,
		MergeFacts:              true,
		MergeThreshold:          0.90,
		OutlineFromClusters:     true,
		TargetFacts:             12,
		Context:                 "steering text",
		ResearchPrompt:          "RESEARCH {{CHUNK}}",
		OutlinePrompt:           "OUTLINE {{FACTS}}",
		SectionPrompt:           "SECTION {{FACTS}}",
		FusePrompt:              "FUSE {{NOTES}}",
		EditPrompt:              "EDIT {{ARTICLE}}",
		RepairPrompt:            "REPAIR {{ARTICLE}} {{MISSING}}",
		DocContextPrompt:        "DOC {{EXCERPT}} {{HEADINGS}}",
		DocHeaderPreamblePrompt: "HEADER {{HEADER}}",
		CiteSectionPrompt:       "CITE SECTION",
		CiteEditPrompt:          "CITE EDIT",
		CiteRepairPrompt:        "CITE REPAIR",
		MergeFactsPrompt:        "MERGE FACTS",
		ClusterLabelsPrompt:     "CLUSTER LABELS",
		ContextPrompt:           "CTX {{CONTEXT}}",
	}
}

func TestKeyStableForSameInputs(t *testing.T) {
	t.Parallel()
	a := Key(baseInputs())
	b := Key(baseInputs())
	if a != b {
		t.Fatalf("identical inputs produced different keys:\n a=%s\n b=%s", a, b)
	}
}

func TestKeyChangesWhenAnyInputChanges(t *testing.T) {
	t.Parallel()
	base := Key(baseInputs())
	mutators := map[string]func(*KeyInputs){
		"source":                     func(k *KeyInputs) { k.Source = "different source" },
		"profile":                    func(k *KeyInputs) { k.Profile = "local" },
		"baseurl":                    func(k *KeyInputs) { k.BaseURL = "http://127.0.0.1:8000/v1" },
		"research_model":             func(k *KeyInputs) { k.ResearchModel = "other" },
		"research_escalation_model":  func(k *KeyInputs) { k.ResearchEscalationModel = "other" },
		"embedding_model":            func(k *KeyInputs) { k.EmbeddingModel = "other" },
		"outline_model":              func(k *KeyInputs) { k.OutlineModel = "other" },
		"write_model":                func(k *KeyInputs) { k.WriteModel = "other" },
		"fuse_model":                 func(k *KeyInputs) { k.FuseModel = "other" },
		"edit_model":                 func(k *KeyInputs) { k.EditModel = "other" },
		"style":                      func(k *KeyInputs) { k.Style = "verbose" },
		"chunk_size":                 func(k *KeyInputs) { k.ChunkSize = 7000 },
		"max_tokens":                 func(k *KeyInputs) { k.MaxTokens = 8000 },
		"fuse":                       func(k *KeyInputs) { k.Fuse = false },
		"edit":                       func(k *KeyInputs) { k.Edit = false },
		"appendix":                   func(k *KeyInputs) { k.Appendix = false },
		"doc_context":                func(k *KeyInputs) { k.DocContext = false },
		"cite":                       func(k *KeyInputs) { k.Cite = false },
		"cascade":                    func(k *KeyInputs) { k.Cascade = false },
		"cascade_threshold":          func(k *KeyInputs) { k.CascadeThreshold = 0.75 },
		"merge_facts":                func(k *KeyInputs) { k.MergeFacts = false },
		"merge_threshold":            func(k *KeyInputs) { k.MergeThreshold = 0.80 },
		"outline_from_clusters":      func(k *KeyInputs) { k.OutlineFromClusters = false },
		"target_facts":               func(k *KeyInputs) { k.TargetFacts = 8 },
		"context":                    func(k *KeyInputs) { k.Context = "other steering" },
		"research_prompt":            func(k *KeyInputs) { k.ResearchPrompt = "OTHER" },
		"outline_prompt":             func(k *KeyInputs) { k.OutlinePrompt = "OTHER" },
		"section_prompt":             func(k *KeyInputs) { k.SectionPrompt = "OTHER" },
		"fuse_prompt":                func(k *KeyInputs) { k.FusePrompt = "OTHER" },
		"edit_prompt":                func(k *KeyInputs) { k.EditPrompt = "OTHER" },
		"repair":                     func(k *KeyInputs) { k.Repair = false },
		"repair_prompt":              func(k *KeyInputs) { k.RepairPrompt = "OTHER" },
		"doc_context_prompt":         func(k *KeyInputs) { k.DocContextPrompt = "OTHER" },
		"doc_header_preamble_prompt": func(k *KeyInputs) { k.DocHeaderPreamblePrompt = "OTHER" },
		"cite_section_prompt":        func(k *KeyInputs) { k.CiteSectionPrompt = "OTHER" },
		"cite_edit_prompt":           func(k *KeyInputs) { k.CiteEditPrompt = "OTHER" },
		"cite_repair_prompt":         func(k *KeyInputs) { k.CiteRepairPrompt = "OTHER" },
		"merge_facts_prompt":         func(k *KeyInputs) { k.MergeFactsPrompt = "OTHER" },
		"cluster_labels_prompt":      func(k *KeyInputs) { k.ClusterLabelsPrompt = "OTHER" },
		"context_prompt":             func(k *KeyInputs) { k.ContextPrompt = "OTHER" },
	}
	for name, mut := range mutators {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := baseInputs()
			mut(&in)
			if got := Key(in); got == base {
				t.Fatalf("changing %s did not change the key (got base key)", name)
			}
		})
	}
}

func TestKeyIgnoresCascadeFieldsWhenDisabled(t *testing.T) {
	t.Parallel()
	in := baseInputs()
	in.Cascade = false
	in.CascadeThreshold = 0
	in.ResearchEscalationModel = ""
	base := Key(in)

	in.CascadeThreshold = 0.9
	in.ResearchEscalationModel = "changed-model"
	if got := Key(in); got != base {
		t.Fatalf("disabled cascade fields changed key:\nbase=%s\n got=%s", base, got)
	}

	in.Cascade = true
	if got := Key(in); got == base {
		t.Fatal("enabling cascade did not change the key")
	}
}

func TestKeyIgnoresMergeFieldsWhenDisabled(t *testing.T) {
	t.Parallel()
	in := baseInputs()
	in.MergeFacts = false
	in.MergeThreshold = 0
	in.OutlineFromClusters = false
	in.TargetFacts = 0
	in.EmbeddingModel = ""
	in.MergeFactsPrompt = ""
	in.ClusterLabelsPrompt = ""
	base := Key(in)

	in.MergeThreshold = 0.8
	in.OutlineFromClusters = true
	in.EmbeddingModel = "changed-embedding"
	in.MergeFactsPrompt = "changed merge"
	in.ClusterLabelsPrompt = "changed labels"
	if got := Key(in); got != base {
		t.Fatalf("disabled merge fields changed key:\nbase=%s\n got=%s", base, got)
	}

	in.MergeFacts = true
	if got := Key(in); got == base {
		t.Fatal("enabling merge-facts did not change the key")
	}
}

func TestKeyIgnoresTargetFieldsWhenDisabled(t *testing.T) {
	t.Parallel()
	in := baseInputs()
	in.MergeFacts = false
	in.OutlineFromClusters = false
	in.TargetFacts = 0
	in.EmbeddingModel = ""
	base := Key(in)

	in.EmbeddingModel = "changed-embedding"
	if got := Key(in); got != base {
		t.Fatalf("disabled target fields changed key:\nbase=%s\n got=%s", base, got)
	}

	in.TargetFacts = 5
	if got := Key(in); got == base {
		t.Fatal("enabling target-facts did not change the key")
	}
}

func TestKeyIgnoresDocContextPromptsWhenDisabled(t *testing.T) {
	t.Parallel()
	in := baseInputs()
	in.DocContext = false
	in.DocContextPrompt = ""
	in.DocHeaderPreamblePrompt = ""
	base := Key(in)

	in.DocContextPrompt = "changed doc prompt"
	in.DocHeaderPreamblePrompt = "changed header prompt"
	if got := Key(in); got != base {
		t.Fatalf("disabled doc-context prompts changed key:\nbase=%s\n got=%s", base, got)
	}

	in.DocContext = true
	if got := Key(in); got == base {
		t.Fatal("enabling doc-context did not change the key")
	}
}

func TestKeyIgnoresCitePromptsWhenDisabled(t *testing.T) {
	t.Parallel()
	in := baseInputs()
	in.Cite = false
	in.CiteSectionPrompt = ""
	in.CiteEditPrompt = ""
	in.CiteRepairPrompt = ""
	base := Key(in)

	in.CiteSectionPrompt = "changed section prompt"
	in.CiteEditPrompt = "changed edit prompt"
	in.CiteRepairPrompt = "changed repair prompt"
	if got := Key(in); got != base {
		t.Fatalf("disabled cite prompts changed key:\nbase=%s\n got=%s", base, got)
	}

	in.Cite = true
	if got := Key(in); got == base {
		t.Fatal("enabling cite did not change the key")
	}
}

func TestStoreLoadRoundTrip(t *testing.T) {
	t.Parallel()
	c, err := newDir(filepath.Join(t.TempDir(), "digests"))
	if err != nil {
		t.Fatalf("newDir: %v", err)
	}
	const key = "abc123"
	const article = "# Title\n\nbody"
	meta := digest.CacheMeta{Coverage: extractscore.SpecificsResult{Covered: 6, Total: 10, Missing: []string{"42%"}}, Words: 512}
	if _, _, ok := c.Load(key); ok {
		t.Fatal("expected miss before store")
	}
	c.Store(key, article, meta)
	got, gotMeta, ok := c.Load(key)
	if !ok {
		t.Fatal("expected hit after store")
	}
	if got != article {
		t.Fatalf("round-trip mismatch: got %q want %q", got, article)
	}
	if gotMeta.Words != meta.Words || gotMeta.Coverage.Covered != meta.Coverage.Covered || gotMeta.Coverage.Total != meta.Coverage.Total || len(gotMeta.Coverage.Missing) != 1 {
		t.Fatalf("meta round-trip mismatch: got %+v want %+v", gotMeta, meta)
	}
}

func TestLoadMissesWithoutSidecar(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "digests")
	c, err := newDir(dir)
	if err != nil {
		t.Fatalf("newDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.md"), []byte("pre-sidecar article"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Load("legacy"); ok {
		t.Fatal("a pre-sidecar entry (article without metadata) must be a miss")
	}
}

func TestLoadMissesOnBadSidecarVersion(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "digests")
	c, err := newDir(dir)
	if err != nil {
		t.Fatalf("newDir: %v", err)
	}
	c.Store("k", "article", digest.CacheMeta{Words: 3})
	if err := os.WriteFile(filepath.Join(dir, "k.json"), []byte(`{"version":999,"meta":{"words":3}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Load("k"); ok {
		t.Fatal("a sidecar with an unknown version must be a miss")
	}
}
