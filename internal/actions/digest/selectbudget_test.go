package digest

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFacilityLocationSelectDeterministic(t *testing.T) {
	t.Parallel()
	vecs := [][]float32{
		{1, 0},
		{0.9, 0.1},
		{0, 1},
		{0, 0.9},
	}
	got := facilityLocationSelect(vecs, 2)
	want := []int{1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected = %#v, want %#v", got, want)
	}
}

func TestTargetFactsCoverageBaseAndAppendixLossless(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# Facts\n\nAlpha, Beta, and Gamma."
	research := &fakeLLM{research: "- Alpha score 101.\n- Beta score 202.\n- Gamma score 303."}
	writer := &fakeLLM{section: "Alpha score 101."}
	opts := Options{
		Style:       "brief",
		OutPath:     filepath.Join(dir, "out.md"),
		FactsPath:   filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir: filepath.Join(dir, "artifacts"),
		ChunkSize:   6000,
		Concurrency: 1,
		Edit:        false,
		Appendix:    true,
		TargetFacts: 1,
		Embedder: fakeEmbedder{vecs: [][]float32{
			{1, 0},
			{0, 1},
			{-1, 0},
		}},
	}
	res, err := Run(context.Background(), RoleCompleters{
		Research: research,
		Fuse:     writer,
		Outline:  writer,
		Section:  writer,
		Edit:     writer,
	}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SelectedFacts != 1 || res.DeselectedFacts != 2 {
		t.Fatalf("selection = %d selected/%d deselected, want 1/2", res.SelectedFacts, res.DeselectedFacts)
	}
	if res.Coverage.Total == 0 || len(res.Coverage.Missing) != 0 {
		t.Fatalf("coverage should be over selected facts only, got %#v", res.Coverage)
	}
	selected, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "facts.selected.md"))
	if err != nil {
		t.Fatalf("read selected facts: %v", err)
	}
	if strings.Contains(string(selected), "Beta score 202") {
		t.Fatalf("selected facts unexpectedly kept deselected fact:\n%s", selected)
	}
	out, err := os.ReadFile(opts.OutPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(out), "Appendix: Extracted Facts") || !strings.Contains(string(out), "Beta score 202") {
		t.Fatalf("appendix should remain lossless, got:\n%s", out)
	}
}
