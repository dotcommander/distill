package digest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComputeCitations(t *testing.T) {
	t.Parallel()
	units := []factUnit{{id: 1, line: "- alpha"}, {id: 2, line: "- beta"}, {id: 3, line: "- gamma"}}
	cases := []struct {
		name        string
		article     string
		wantCovered int
		wantMissing []int
	}{
		{"grouped", "Alpha and gamma. [F1, F3]", 2, []int{2}},
		{"lowercase and bare second id", "Alpha and beta. [f1, 2]", 2, []int{3}},
		{"unknown ignored", "Unknown. [F99]", 0, []int{1, 2, 3}},
		{"absent", "No markers here.", 0, []int{1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := computeCitations(units, tc.article)
			if got.Covered != tc.wantCovered {
				t.Fatalf("Covered = %d, want %d", got.Covered, tc.wantCovered)
			}
			if !reflect.DeepEqual(got.MissingIDs, tc.wantMissing) {
				t.Fatalf("MissingIDs = %v, want %v", got.MissingIDs, tc.wantMissing)
			}
		})
	}
}

func TestStripCiteMarkers(t *testing.T) {
	t.Parallel()
	in := "Alpha happened [F1, F3].\n\nBeta happened [f2] , too."
	want := "Alpha happened.\n\nBeta happened, too."
	if got := stripCiteMarkers(in); got != want {
		t.Fatalf("stripCiteMarkers mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestSelectFactsTagged(t *testing.T) {
	t.Parallel()
	units := []factUnit{{id: 1, line: "- alpha"}, {id: 2, line: "* beta\n  continued"}, {id: 3, line: "- gamma"}}
	got := selectFactsTagged(units, []int{3, 1})
	want := "- [F3] gamma\n- [F1] alpha"
	if got != want {
		t.Fatalf("selectFactsTagged mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestCitePipelineWritesCitedArtifactAndStripsFinal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research: "- Alpha fact 42.\n- Beta fact 99.",
		outline:  "# Draft Title\n\n## Summary\nFacts: F1, F2\ncover both facts",
		section:  "Alpha fact 42 is central. [F1]\n\nBeta fact 99 follows. [F2]",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        false,
		Cite:        true,
	}

	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Citations == nil || res.Citations.Covered != 2 || res.Citations.Total != 2 {
		t.Fatalf("unexpected citations: %+v", res.Citations)
	}
	final, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if strings.Contains(string(final), "[F1]") || strings.Contains(string(final), "[F2]") {
		t.Fatalf("final output should have stripped markers: %s", final)
	}
	cited, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"))
	if err != nil {
		t.Fatalf("read cited artifact: %v", err)
	}
	if !strings.Contains(string(cited), "[F1]") || !strings.Contains(string(cited), "[F2]") {
		t.Fatalf("cited artifact should preserve markers: %s", cited)
	}
	data, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "citations.json"))
	if err != nil {
		t.Fatalf("read citations: %v", err)
	}
	var got CitationResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal citations: %v", err)
	}
	if got.Covered != 2 || got.Total != 2 || len(got.MissingIDs) != 0 {
		t.Fatalf("unexpected citations artifact: %+v", got)
	}
}

func TestCiteRepairPromptContainsOnlyMissingFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research: "- Alpha fact 42.\n- Beta fact 99.",
		outline:  "# Draft Title\n\n## Summary\nFacts: F1, F2\ncover both facts",
		section:  "Alpha fact 42 is central. [F1]",
		repair:   "Alpha fact 42 is central. [F1]\n\nBeta fact 99 follows. [F2]",
	}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Edit:        false,
		Repair:      true,
		Cite:        true,
	}

	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Citations == nil || res.Citations.Covered != 2 {
		t.Fatalf("expected cited repair to cover both facts, got %+v", res.Citations)
	}
	var repairPrompt string
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "CITE_REPAIR") {
			repairPrompt = prompt
			break
		}
	}
	if repairPrompt == "" {
		t.Fatal("expected cited repair prompt")
	}
	if !strings.Contains(repairPrompt, "[F2] Beta fact 99.") {
		t.Fatalf("repair prompt missing F2 fact: %s", repairPrompt)
	}
	if strings.Contains(repairPrompt, "[F1] Alpha fact 42.") {
		t.Fatalf("repair prompt should not include already cited F1 fact: %s", repairPrompt)
	}
}
