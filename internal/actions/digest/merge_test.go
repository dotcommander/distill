package digest

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeEmbedder struct {
	vecs [][]float32
}

func (f fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	copy(out, f.vecs[:len(texts)])
	return out, nil
}

type mergeAndLabelLLM struct {
	merge  string
	labels string
	calls  []string
}

func (m *mergeAndLabelLLM) Complete(_ context.Context, prompt string) (string, error) {
	m.calls = append(m.calls, prompt)
	if strings.HasPrefix(prompt, "CLUSTER_LABELS") {
		return m.labels, nil
	}
	return m.merge, nil
}

func TestCosineAndClusterByCosineDeterministic(t *testing.T) {
	t.Parallel()
	vecs := [][]float32{
		{1, 0},
		{0.95, 0.05},
		{0, 1},
		{0, 0.9},
	}
	if got := cosine(vecs[0], vecs[1]); got < 0.99 {
		t.Fatalf("cosine close vectors = %.3f, want >= .99", got)
	}
	clusters := clusterByCosine(vecs, 0.90)
	want := [][]int{{0, 1}, {2, 3}}
	if !reflect.DeepEqual(clusters, want) {
		t.Fatalf("clusters = %#v, want %#v", clusters, want)
	}
}

func TestParseMergeOutputKeepsOriginalOnGarbage(t *testing.T) {
	t.Parallel()
	parsed := parseMergeOutput([]string{"- Alpha paid $100.", "- Alpha paid 100 dollars."}, "not valid output")
	if len(parsed.Facts) != 0 {
		t.Fatalf("parsed facts = %#v, want none so caller keeps originals", parsed.Facts)
	}
}

func TestMergePreservesSourceIDsForNumberedFacts(t *testing.T) {
	t.Parallel()
	original := []string{"- [F1] Sony IMX678 appears twice.", "- [F2] Sony IMX678 is repeated."}
	if mergePreservesSourceIDs(original, []string{"- [F1, F2] Sony IMX678 is repeated once."}) != true {
		t.Fatal("expected merged fact with both source IDs to pass")
	}
	if mergePreservesSourceIDs(original, []string{"- [F1] Sony IMX678 is repeated once."}) != false {
		t.Fatal("expected merged fact missing one source ID to fail")
	}
}

func TestMergeFactsPipelineWritesMergedFactsAndContradictions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# Payments\n\nAlpha and Beta payments."
	research := &fakeLLM{research: "- Alpha paid $100.\n- Alpha paid 100 dollars.\n- Beta paid $200.", section: "draft body"}
	mergeLLM := &fakeLLM{research: "MERGED: Alpha paid $100.\nCONTRADICTION: Alpha payment was duplicated with different wording."}
	writer := &fakeLLM{section: "draft body"}
	opts := Options{
		Style:          "brief",
		OutPath:        filepath.Join(dir, "out.md"),
		FactsPath:      filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:    filepath.Join(dir, "artifacts"),
		ChunkSize:      6000,
		Concurrency:    1,
		Edit:           false,
		MergeFacts:     true,
		MergeThreshold: 0.90,
		Embedder: fakeEmbedder{vecs: [][]float32{
			{1, 0},
			{0.95, 0.05},
			{0, 1},
		}},
	}
	res, err := Run(context.Background(), RoleCompleters{
		Research: research,
		Fuse:     mergeLLM,
		Outline:  writer,
		Section:  writer,
		Edit:     writer,
	}, testPrompts(), source, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Contradictions != 1 {
		t.Fatalf("Contradictions = %d, want 1", res.Contradictions)
	}
	merged, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "facts.merged.md"))
	if err != nil {
		t.Fatalf("read merged facts: %v", err)
	}
	if strings.Count(string(merged), "Alpha paid") != 1 || !strings.Contains(string(merged), "Beta paid $200") {
		t.Fatalf("unexpected merged facts:\n%s", merged)
	}
	contradictions, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "contradictions.md"))
	if err != nil {
		t.Fatalf("read contradictions: %v", err)
	}
	if !strings.Contains(string(contradictions), "different wording") {
		t.Fatalf("unexpected contradictions:\n%s", contradictions)
	}
}

func TestCoalesceClustersCap(t *testing.T) {
	t.Parallel()
	// 8 groups: 6 multi-fact clusters + 2 singletons.
	clusters := []factCluster{
		{Index: 1, Facts: []string{"- A1", "- A2"}, Centroid: []float32{1.0, 0.0}, Size: 2},
		{Index: 2, Facts: []string{"- B1"}, Centroid: []float32{0.98, 0.02}, Size: 1},
		{Index: 3, Facts: []string{"- C1", "- C2"}, Centroid: []float32{0.0, 1.0}, Size: 2},
		{Index: 4, Facts: []string{"- D1"}, Centroid: []float32{0.0, 0.98}, Size: 1},
		{Index: 5, Facts: []string{"- E1", "- E2"}, Centroid: []float32{0.7, 0.7}, Size: 2},
		{Index: 6, Facts: []string{"- F1", "- F2"}, Centroid: []float32{0.72, 0.68}, Size: 2},
		{Index: 7, Facts: []string{"- G1"}, Centroid: []float32{-1.0, 0.0}, Size: 1},
		{Index: 8, Facts: []string{"- H1"}, Centroid: []float32{-0.98, 0.0}, Size: 1},
	}
	got := coalesceClusters(clusters, 3)
	if len(got) != 3 {
		t.Fatalf("got %d groups, want 3", len(got))
	}
	// Every fact must appear exactly once.
	seen := map[string]int{}
	expectedFacts := []string{"- A1", "- A2", "- B1", "- C1", "- C2", "- D1", "- E1", "- E2", "- F1", "- F2", "- G1", "- H1"}
	for _, c := range got {
		for _, f := range c.Facts {
			seen[f]++
		}
	}
	for _, f := range expectedFacts {
		if seen[f] != 1 {
			t.Errorf("fact %q appears %d times, want 1", f, seen[f])
		}
	}
	if len(seen) != len(expectedFacts) {
		t.Errorf("total unique facts = %d, want %d", len(seen), len(expectedFacts))
	}
}

func TestCoalesceSmallClustersMergesNearestAndPrefersPreviousOnTie(t *testing.T) {
	t.Parallel()
	clusters := []factCluster{
		{Index: 1, Facts: []string{"- A1", "- A2", "- A3"}, Centroid: []float32{1, 0}, Size: 3},
		{Index: 2, Facts: []string{"- B1"}, Centroid: []float32{1, 0}, Size: 1},
		{Index: 3, Facts: []string{"- C1", "- C2", "- C3"}, Centroid: []float32{1, 0}, Size: 3},
	}
	got := coalesceSmallClusters(clusters, 3)
	if len(got) != 2 {
		t.Fatalf("got %d groups, want 2", len(got))
	}
	if !reflect.DeepEqual(got[0].Facts, []string{"- A1", "- A2", "- A3", "- B1"}) {
		t.Fatalf("stub did not merge into previous tied neighbor: %+v", got[0].Facts)
	}
	if got[0].Size != 4 {
		t.Fatalf("merged size = %d, want 4", got[0].Size)
	}
}

func TestSplitOversizedClustersUsesContiguousFactOrder(t *testing.T) {
	t.Parallel()
	clusters := []factCluster{
		{
			Index:    1,
			Facts:    []string{"- A1", "- A2", "- A3", "- A4", "- A5", "- A6", "- A7"},
			Centroid: []float32{1, 0},
			Size:     7,
		},
		{Index: 2, Facts: []string{"- B1", "- B2"}, Centroid: []float32{0, 1}, Size: 2},
	}
	got := splitOversizedClusters(clusters, 3, 1.5)
	if len(got) != 3 {
		t.Fatalf("got %d groups, want 3", len(got))
	}
	want := [][]string{
		{"- A1", "- A2", "- A3", "- A4", "- A5"},
		{"- A6", "- A7"},
		{"- B1", "- B2"},
	}
	for i := range want {
		if !reflect.DeepEqual(got[i].Facts, want[i]) {
			t.Fatalf("cluster %d facts = %#v, want %#v", i, got[i].Facts, want[i])
		}
	}
}

func TestCoalesceClustersUncapped(t *testing.T) {
	t.Parallel()
	clusters := []factCluster{
		{Index: 1, Facts: []string{"- A"}, Centroid: []float32{1.0, 0.0}, Size: 1},
		{Index: 2, Facts: []string{"- B"}, Centroid: []float32{0.0, 1.0}, Size: 1},
		{Index: 3, Facts: []string{"- C"}, Centroid: []float32{0.5, 0.5}, Size: 1},
	}
	got := coalesceClusters(clusters, 0)
	if len(got) != 3 {
		t.Fatalf("uncapped (0) changed group count: got %d, want 3", len(got))
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], clusters[i]) {
			t.Fatalf("uncapped (0) altered cluster %d: got %+v, want %+v", i, got[i], clusters[i])
		}
	}
}

func TestCoalesceClustersDeterminism(t *testing.T) {
	t.Parallel()
	clusters := []factCluster{
		{Index: 1, Facts: []string{"- A"}, Centroid: []float32{1.0, 0.0}, Size: 1},
		{Index: 2, Facts: []string{"- B"}, Centroid: []float32{0.98, 0.02}, Size: 1},
		{Index: 3, Facts: []string{"- C"}, Centroid: []float32{0.0, 1.0}, Size: 1},
		{Index: 4, Facts: []string{"- D"}, Centroid: []float32{0.0, 0.98}, Size: 1},
		{Index: 5, Facts: []string{"- E"}, Centroid: []float32{-1.0, 0.0}, Size: 1},
	}
	run1 := coalesceClusters(clusters, 2)
	run2 := coalesceClusters(clusters, 2)
	if len(run1) != len(run2) {
		t.Fatalf("non-deterministic: len(run1)=%d, len(run2)=%d", len(run1), len(run2))
	}
	for i := range run1 {
		if !reflect.DeepEqual(run1[i], run2[i]) {
			t.Fatalf("non-deterministic at index %d: %+v vs %+v", i, run1[i], run2[i])
		}
	}
}

func TestCoalesceClustersAlreadyUnderCap(t *testing.T) {
	t.Parallel()
	clusters := []factCluster{
		{Index: 1, Facts: []string{"- A"}, Centroid: []float32{1.0, 0.0}, Size: 1},
		{Index: 2, Facts: []string{"- B"}, Centroid: []float32{0.0, 1.0}, Size: 1},
	}
	got := coalesceClusters(clusters, 5)
	if len(got) != 2 {
		t.Fatalf("already-under-cap changed group count: got %d, want 2", len(got))
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], clusters[i]) {
			t.Fatalf("already-under-cap altered cluster %d", i)
		}
	}
}

func TestOutlineFromClustersSkipsOutlineModel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := "# Payments\n\nAlpha payments."
	research := &fakeLLM{research: "- Alpha paid $100.\n- Alpha paid 100 dollars.", section: "draft body"}
	mergeLLM := &mergeAndLabelLLM{
		merge:  "MERGED: Alpha paid $100.",
		labels: "C1: Alpha payments",
	}
	outline := &fakeLLM{outline: "# Should Not Be Used\n\n## Wrong\nFacts: F1"}
	writer := &fakeLLM{section: "draft body"}
	opts := Options{
		Style:               "brief",
		OutPath:             filepath.Join(dir, "out.md"),
		FactsPath:           filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:         filepath.Join(dir, "artifacts"),
		ChunkSize:           6000,
		Concurrency:         1,
		Edit:                false,
		MergeFacts:          true,
		MergeThreshold:      0.90,
		OutlineFromClusters: true,
		Embedder: fakeEmbedder{vecs: [][]float32{
			{1, 0},
			{0.95, 0.05},
		}},
	}
	if _, err := Run(context.Background(), RoleCompleters{
		Research: research,
		Fuse:     mergeLLM,
		Outline:  outline,
		Section:  writer,
		Edit:     writer,
	}, testPrompts(), source, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outline.calls != 0 {
		t.Fatalf("outline model calls = %d, want 0", outline.calls)
	}
	outlineArtifact, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "outline.md"))
	if err != nil {
		t.Fatalf("read outline artifact: %v", err)
	}
	if !strings.Contains(string(outlineArtifact), "## Alpha payments") || strings.Contains(string(outlineArtifact), "Should Not Be Used") {
		t.Fatalf("unexpected outline artifact:\n%s", outlineArtifact)
	}
}

func TestMergeFactsSingletonClustersGatedByMaxSections(t *testing.T) {
	t.Parallel()
	// Vectors: A and B are similar (cluster), C is far (singleton)
	embedder := fakeEmbedder{vecs: [][]float32{
		{1, 0},       // A
		{0.95, 0.05}, // B — cosine ~0.999 with A
		{0, 1},       // C — far from A/B, isolated singleton
	}}
	mergeLLM := &fakeLLM{research: "KEEP: A"}
	facts := "- A\n- B\n- C"
	// Uncapped (MaxSections == 0): singleton clusters must be excluded.
	res0, err := mergeFacts(context.Background(), mergeLLM, embedder, testPrompts(), facts, Options{MergeThreshold: 0.90, MaxSections: 0}, nil)
	if err != nil {
		t.Fatalf("MaxSections=0: %v", err)
	}
	for _, c := range res0.Clusters {
		if c.Size == 1 {
			t.Fatalf("MaxSections=0 produced singleton cluster: %+v", c)
		}
	}
	// Capped (MaxSections > 0): singleton clusters are included for coalescing.
	resN, err := mergeFacts(context.Background(), mergeLLM, embedder, testPrompts(), facts, Options{MergeThreshold: 0.90, MaxSections: 3}, nil)
	if err != nil {
		t.Fatalf("MaxSections=3: %v", err)
	}
	hasSingleton := false
	for _, c := range resN.Clusters {
		if c.Size == 1 {
			hasSingleton = true
			break
		}
	}
	if !hasSingleton {
		t.Fatal("MaxSections=3 had no singleton cluster when one was expected")
	}
}
