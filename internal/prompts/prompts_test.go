package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/fsutil"
)

func TestLoadFromMaterializesAndRenders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	set, err := loadFrom(dir)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	for _, n := range []string{"research.md", "fuse.md", "judge.md", "outline.md", "section.md", "edit-section.md", "repair.md", "trace-go.md", "doc-context.md", "doc-header-preamble.md", "cite-section.md", "cite-edit.md", "cite-repair.md", "precision.md", "precision-repair.md", "merge-facts.md", "cluster-labels.md", "optimize-mutate.md"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("expected %s materialized: %v", n, err)
		}
	}

	got := set.RenderResearch("chunk-001", "BODY-TEXT")
	if !strings.Contains(got, "chunk-001") || !strings.Contains(got, "BODY-TEXT") || strings.Contains(got, "{{CHUNK") {
		t.Fatalf("research not rendered: %q", got)
	}
	got3 := set.RenderFuse("NOTES-Z")
	if !strings.Contains(got3, "NOTES-Z") || strings.Contains(got3, "{{NOTES") {
		t.Fatalf("fuse not rendered: %q", got3)
	}
	got5 := set.RenderOutline("STYLE-X", "FACTS-Y")
	if !strings.Contains(got5, "STYLE-X") || !strings.Contains(got5, "FACTS-Y") || strings.Contains(got5, "{{STYLE") {
		t.Fatalf("outline not rendered: %q", got5)
	}
	got6 := set.RenderSection("STYLE-X", "OUTLINE-O", "FACTS-Y", "PRIOR-P", "HEADING-H", "INTENT-I")
	if !strings.Contains(got6, "HEADING-H") || !strings.Contains(got6, "INTENT-I") || !strings.Contains(got6, "PRIOR-P") || strings.Contains(got6, "{{HEADING") {
		t.Fatalf("section not rendered: %q", got6)
	}
	got7 := set.RenderEditSection("STYLE-X", "ARTICLE-A", "PRIOR-P", "FACTS-Y", "HEADING-H")
	if !strings.Contains(got7, "ARTICLE-A") || !strings.Contains(got7, "PRIOR-P") || !strings.Contains(got7, "HEADING-H") || strings.Contains(got7, "{{ARTICLE") {
		t.Fatalf("edit-section not rendered: %q", got7)
	}
	got8 := set.RenderTraceGo("package main\nfunc main(){}")
	if !strings.Contains(got8, "package main") || strings.Contains(got8, "{{PROGRAM") {
		t.Fatalf("trace-go not rendered: %q", got8)
	}
	got9 := set.RenderRepair("ARTICLE-A", "MISSING-M")
	if !strings.Contains(got9, "ARTICLE-A") || !strings.Contains(got9, "MISSING-M") || strings.Contains(got9, "{{ARTICLE") {
		t.Fatalf("repair not rendered: %q", got9)
	}

	got9b := set.RenderPrecisionRepair("FACTS-F", "FLAGGED-G", "ARTICLE-H")
	if !strings.Contains(got9b, "FACTS-F") || !strings.Contains(got9b, "FLAGGED-G") || !strings.Contains(got9b, "ARTICLE-H") || strings.Contains(got9b, "{{FACTS") {
		t.Fatalf("precision-repair not rendered: %q", got9b)
	}

	got10 := set.RenderMergeFacts("C7", "- Fact A")
	if !strings.Contains(got10, "C7") || !strings.Contains(got10, "- Fact A") || strings.Contains(got10, "{{FACTS") {
		t.Fatalf("merge-facts not rendered: %q", got10)
	}
	got11 := set.RenderClusterLabels("C1:\n- Fact A")
	if !strings.Contains(got11, "C1:") || strings.Contains(got11, "{{CLUSTERS") {
		t.Fatalf("cluster-labels not rendered: %q", got11)
	}
	got12 := set.RenderOptimizeMutate("compress", "PROMPT {{CHUNK}}", "recall=0.5")
	if !strings.Contains(got12, "compress") || !strings.Contains(got12, "PROMPT {{CHUNK}}") || strings.Contains(got12, "{{OPERATOR") {
		t.Fatalf("optimize-mutate not rendered: %q", got12)
	}

	// Idempotent: a second load over the same dir reads the existing files.
	if _, err := loadFrom(dir); err != nil {
		t.Fatalf("second loadFrom: %v", err)
	}
}

func TestLoadFromRefreshesOnlyUneditedMaterializedDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := loadFrom(dir); err != nil {
		t.Fatalf("initial loadFrom: %v", err)
	}
	meta, err := loadPromptMeta(dir)
	if err != nil {
		t.Fatalf("load meta: %v", err)
	}
	oldDefault := []byte("old embedded default\n")
	oldHash := hashPromptBytes(oldDefault)
	meta["edit-section.md"] = oldHash
	if cerr := fsutil.WriteFile(filepath.Join(dir, "edit-section.md"), oldDefault, 0o644); cerr != nil {
		t.Fatalf("seed old default: %v", cerr)
	}
	if cerr := writePromptMeta(dir, meta); cerr != nil {
		t.Fatalf("write meta: %v", cerr)
	}
	set, err := loadFrom(dir)
	if err != nil {
		t.Fatalf("refresh loadFrom: %v", err)
	}
	if !strings.Contains(set.EditSection, "{{PRIOR_ACCEPTED}}") {
		t.Fatalf("unedited stale default was not refreshed:\n%s", set.EditSection)
	}

	custom := "custom edit prompt"
	meta, err = loadPromptMeta(dir)
	if err != nil {
		t.Fatalf("reload meta: %v", err)
	}
	meta["edit-section.md"] = oldHash
	if cerr := fsutil.WriteFile(filepath.Join(dir, "edit-section.md"), []byte(custom), 0o644); cerr != nil {
		t.Fatalf("seed custom prompt: %v", cerr)
	}
	if cerr := writePromptMeta(dir, meta); cerr != nil {
		t.Fatalf("write stale meta: %v", cerr)
	}
	set, err = loadFrom(dir)
	if err != nil {
		t.Fatalf("custom loadFrom: %v", err)
	}
	if set.EditSection != custom {
		t.Fatalf("user-edited prompt was overwritten: %q", set.EditSection)
	}
}

func TestRenderContextPreamble(t *testing.T) {
	t.Parallel()
	set, err := loadFrom(t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	got := set.RenderContextPreamble("FOCUS-ON-METHODOLOGY")
	if !strings.Contains(got, "FOCUS-ON-METHODOLOGY") || strings.Contains(got, "{{CONTEXT}}") {
		t.Fatalf("context preamble not rendered: %q", got)
	}
}
