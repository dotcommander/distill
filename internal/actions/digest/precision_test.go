package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/distill/internal/prompts"
)

func TestPrecisionBatchesKeepFactsPrefix(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		precisionRecords: []string{
			`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":false,"reason":"unsupported"}]}`,
			`{"verdicts":[{"i":3,"supported":true,"reason":""}]}`,
		},
	}
	p := testPrompts()
	res, err := checkPrecision(context.Background(), llm, p, "- fact one\n- fact two", []string{"One.", "Two.", "Three."}, 2, nil, 0, 1, 0)
	if err != nil {
		t.Fatalf("checkPrecision: %v", err)
	}
	if res.Supported != 2 || res.Total != 3 || len(res.Unsupported) != 1 {
		t.Fatalf("unexpected precision result: %+v", res)
	}
	if len(llm.prompts) != 2 {
		t.Fatalf("expected two precision batches, got %d", len(llm.prompts))
	}
	const prefix = "PRECISION FACTS:\n- fact one\n- fact two\nSENTENCES:\n"
	for _, prompt := range llm.prompts {
		if !strings.HasPrefix(prompt, prefix) {
			t.Fatalf("precision prompt lost stable facts prefix:\n%s", prompt)
		}
	}
}

func TestPrecisionMalformedJSONFailsClosed(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{precision: "not json"}
	res, err := checkPrecision(context.Background(), llm, testPrompts(), "- fact", []string{"One.", "Two."}, 80, nil, 0, 1, 0)
	if err != nil {
		t.Fatalf("checkPrecision: %v", err)
	}
	if res.Supported != 0 || res.Total != 2 || len(res.Unsupported) != 2 {
		t.Fatalf("malformed JSON should mark all unsupported, got %+v", res)
	}
	for _, unsupported := range res.Unsupported {
		if unsupported.Reason != "precision judge returned malformed JSON" {
			t.Fatalf("malformed JSON reason = %q", unsupported.Reason)
		}
	}
}

func TestValidatePrecisionVerdictIndexes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		verdicts []precisionVerdict
		first    int
		last     int
		wantErr  bool
	}{
		{name: "ordered", verdicts: []precisionVerdict{{Index: 4}, {Index: 5}, {Index: 6}}, first: 4, last: 6},
		{name: "shuffled", verdicts: []precisionVerdict{{Index: 6}, {Index: 4}, {Index: 5}}, first: 4, last: 6},
		{name: "duplicate", verdicts: []precisionVerdict{{Index: 4}, {Index: 4}, {Index: 5}}, first: 4, last: 6, wantErr: true},
		{name: "zero", verdicts: []precisionVerdict{{Index: 0}, {Index: 4}, {Index: 5}}, first: 4, last: 6, wantErr: true},
		{name: "negative", verdicts: []precisionVerdict{{Index: -1}, {Index: 4}, {Index: 5}}, first: 4, last: 6, wantErr: true},
		{name: "missing", verdicts: []precisionVerdict{{Index: 4}, {Index: 6}}, first: 4, last: 6, wantErr: true},
		{name: "prior batch", verdicts: []precisionVerdict{{Index: 3}, {Index: 4}, {Index: 5}}, first: 4, last: 6, wantErr: true},
		{name: "future batch", verdicts: []precisionVerdict{{Index: 4}, {Index: 5}, {Index: 7}}, first: 4, last: 6, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePrecisionVerdictIndexes(tt.verdicts, tt.first, tt.last)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePrecisionVerdictIndexes() error = %v, want error %t", err, tt.wantErr)
			}
		})
	}
}

func TestPrecisionInvalidIndexSetFailsWholeBatchClosed(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{precision: `{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":1,"supported":true,"reason":""}]}`}
	res, err := checkPrecision(context.Background(), llm, testPrompts(), "- fact", []string{"One.", "Two."}, 80, nil, 0, 1, 0)
	if err != nil {
		t.Fatalf("checkPrecision: %v", err)
	}
	if res.Supported != 0 || len(res.Unsupported) != 2 {
		t.Fatalf("invalid set should mark the entire batch unsupported, got %+v", res)
	}
	for _, unsupported := range res.Unsupported {
		if unsupported.Reason != "precision judge returned invalid verdict index set" {
			t.Fatalf("unsupported reason = %q", unsupported.Reason)
		}
	}
}

func TestPrecisionRejectsCrossBatchIndexes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		index int
	}{
		{name: "prior", index: 1},
		{name: "future", index: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm := &fakeLLM{precisionRecords: []string{
				`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":true,"reason":""}]}`,
				fmt.Sprintf(`{"verdicts":[{"i":%d,"supported":true,"reason":""},{"i":3,"supported":true,"reason":""}]}`, tt.index),
			}}
			res, err := checkPrecision(context.Background(), llm, testPrompts(), "- fact", []string{"One.", "Two.", "Three."}, 2, nil, 0, 1, 0)
			if err != nil {
				t.Fatalf("checkPrecision: %v", err)
			}
			if res.Supported != 2 || len(res.Unsupported) != 1 {
				t.Fatalf("cross-batch index should invalidate only batch two, got %+v", res)
			}
			if got := res.Unsupported[0]; got.Index != 3 || got.Reason != "precision judge returned invalid verdict index set" {
				t.Fatalf("unexpected unsupported sentence: %+v", got)
			}
		})
	}
}

func TestDigestPrecisionWritesArtifact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research:  "- Alpha fact.",
		outline:   "# Draft Title\n\n## Summary\nFacts: F1\ncover alpha",
		section:   "Alpha fact. Unsupported invention.",
		precision: `{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":false,"reason":"not in facts"}]}`,
	}
	opts := Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:        filepath.Join(dir, "artifacts"),
		ChunkSize:          6000,
		Edit:               false,
		CheckPrecision:     true,
		PrecisionBatchSize: 80,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Precision == nil || res.Precision.Supported != 1 || res.Precision.Total != 2 {
		t.Fatalf("unexpected precision: %+v", res.Precision)
	}
	data, err := os.ReadFile(filepath.Join(opts.ArtifactDir, "responses", "precision.json"))
	if err != nil {
		t.Fatalf("read precision artifact: %v", err)
	}
	var got PrecisionResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal precision artifact: %v", err)
	}
	if got.Supported != 1 || got.Total != 2 || len(got.Unsupported) != 1 {
		t.Fatalf("unexpected precision artifact: %+v", got)
	}
}

func TestCitePrecisionJudgesAllSentencesMarkersStripped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research:  "- Alpha fact.\n- Beta fact.",
		outline:   "# Draft Title\n\n## Summary\nFacts: F1, F2\ncover both",
		section:   "Alpha fact. [F1]\n\nUnsupported bridge sentence.\n\nBeta fact. [F2]",
		precision: `{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":false,"reason":"unsupported bridge"},{"i":3,"supported":true,"reason":""}]}`,
	}
	opts := Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:        filepath.Join(dir, "artifacts"),
		ChunkSize:          6000,
		Edit:               false,
		Cite:               true,
		CheckPrecision:     true,
		PrecisionBatchSize: 80,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// All three sentences (with markers stripped) should be judged, not just the unmarked one.
	if res.Precision == nil || res.Precision.Total != 3 || len(res.Precision.Unsupported) != 1 {
		t.Fatalf("cite precision should judge all 3 sentences (markers stripped), got %+v", res.Precision)
	}
	var precisionPrompt string
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "PRECISION") {
			precisionPrompt = prompt
			break
		}
	}
	if precisionPrompt == "" {
		t.Fatal("expected precision prompt")
	}
	_, sentenceBlock, _ := strings.Cut(precisionPrompt, "SENTENCES:\n")
	// All three sentences should appear in the prompt, stripped of markers.
	if !strings.Contains(sentenceBlock, "Alpha fact.") {
		t.Fatalf("Alpha fact. sentence missing from precision prompt sentences:\n%s", sentenceBlock)
	}
	if !strings.Contains(sentenceBlock, "Unsupported bridge sentence") {
		t.Fatalf("Unsupported bridge sentence missing from precision prompt sentences:\n%s", sentenceBlock)
	}
	if !strings.Contains(sentenceBlock, "Beta fact.") {
		t.Fatalf("Beta fact. sentence missing from precision prompt sentences:\n%s", sentenceBlock)
	}
	// Markers must be stripped.
	if strings.Contains(sentenceBlock, "[F1]") || strings.Contains(sentenceBlock, "[F2]") {
		t.Fatalf("markers should be stripped from precision prompt sentences:\n%s", sentenceBlock)
	}
}

func TestFullCitePrecisionRepairRebuildsCitations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research: "- Revenue was $1,200 in 2021 at Acme Corp.\n- Profit was $300 in 2021 at Acme Corp.",
		outline:  "# Draft Title\n\n## Summary\nFacts: F1, F2\ncover both",
		section:  "Revenue was $1,200 in 2021 at Acme Corp. [F1]\n\nProfit was $300 in 2021 at Acme Corp. [F2]\n\nThe moon landing failed.",
		precisionRecords: []string{
			`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":true,"reason":""},{"i":3,"supported":false,"reason":"unsupported bridge"}]}`,
			`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":true,"reason":""},{"i":3,"supported":true,"reason":""}]}`,
		},
		precisionRepair: "Revenue was $1,200 in 2021 at Acme Corp. [F1]\n\nProfit was $300 in 2021 at Acme Corp. [F2]\n\nThe moon landing was not in the source.",
	}
	opts := Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:        filepath.Join(dir, "artifacts"),
		ChunkSize:          6000,
		Edit:               false,
		Cite:               true,
		CheckPrecision:     true,
		Repair:             true,
		PrecisionBatchSize: 80,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Citations should be recomputed after repair: both facts cited in repaired article.
	if res.Citations == nil || res.Citations.Covered != 2 || res.Citations.Total != 2 {
		t.Fatalf("citations should reflect repaired article (2/2), got %+v", res.Citations)
	}
	// Precision should pass after repair.
	if res.Precision == nil || len(res.Precision.Unsupported) != 0 {
		t.Fatalf("precision repair should clear unsupported, got %+v", res.Precision)
	}
	// Final output should be marker-free.
	out, _ := os.ReadFile(opts.OutPath)
	if strings.Contains(string(out), "[F1]") || strings.Contains(string(out), "[F2]") {
		t.Fatalf("final output should have stripped markers: %s", out)
	}
	if !strings.Contains(string(out), "Revenue was $1,200") {
		t.Fatalf("expected fact 1 in output: %s", out)
	}
	if !strings.Contains(string(out), "Profit was $300") {
		t.Fatalf("expected fact 2 in output: %s", out)
	}
	// Verify precision checks were called (before + after repair).
	precisionChecks := 0
	precisionRepairs := 0
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "PRECISION_REPAIR") {
			precisionRepairs++
		}
		if strings.HasPrefix(prompt, "PRECISION FACTS") {
			precisionChecks++
		}
	}
	if precisionChecks != 2 {
		t.Fatalf("expected 2 precision checks (initial + repaired), got %d", precisionChecks)
	}
	if precisionRepairs != 1 {
		t.Fatalf("expected 1 precision repair call, got %d", precisionRepairs)
	}
}

func TestParsePrecisionVerdicts(t *testing.T) {
	t.Parallel()
	got, ok := parsePrecisionVerdicts("prefix {\"verdicts\":[{\"i\":7,\"supported\":true,\"reason\":\"ok\"}]} suffix")
	if !ok || len(got) != 1 || got[0].Index != 7 || !got[0].Supported {
		t.Fatalf("parsePrecisionVerdicts = %+v, %v", got, ok)
	}
}

func TestRenderPrecisionUsesFactsFirst(t *testing.T) {
	t.Parallel()
	p := &prompts.Set{Precision: "FACTS={{FACTS}}\nSENTENCES={{SENTENCES}}"}
	got := p.RenderPrecision("facts", "1. sentence")
	if got != "FACTS=facts\nSENTENCES=1. sentence" {
		t.Fatalf("RenderPrecision = %q", got)
	}
}

func TestRunPrecisionRepairAdoptedWithCoverageGuard(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research: "- Revenue was $1,200 in 2021 at Acme Corp.",
		outline:  "# Draft Title\n\n## Summary\nFacts: F1\ncover summary",
		section:  "Revenue was $1,200 in 2021 at Acme Corp. The moon landing failed.",
		precisionRecords: []string{
			`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":false,"reason":"unsupported bridge"}]}`,
			`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":true,"reason":""}]}`,
		},
		precisionRepair: "Revenue was $1,200 in 2021 at Acme Corp. The moon landing was not in the source.",
	}
	opts := Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:        filepath.Join(dir, "artifacts"),
		ChunkSize:          6000,
		CheckPrecision:     true,
		Repair:             true,
		PrecisionBatchSize: 80,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	precisionChecks := 0
	precisionRepairs := 0
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "PRECISION_REPAIR") {
			precisionRepairs++
		}
		if strings.HasPrefix(prompt, "PRECISION FACTS") {
			precisionChecks++
		}
	}
	if precisionChecks != 2 {
		t.Fatalf("expected 2 precision checks (initial + repaired), got %d", precisionChecks)
	}
	if precisionRepairs != 1 {
		t.Fatalf("expected 1 precision repair call, got %d", precisionRepairs)
	}
	if res.Precision == nil || len(res.Precision.Unsupported) != 0 {
		t.Fatalf("precision repair should improve unsupported count, got %+v", res.Precision)
	}
	out, _ := os.ReadFile(opts.OutPath)
	if !strings.Contains(string(out), "The moon landing was not in the source") {
		t.Fatalf("expected repaired text in output: %q", out)
	}
	if strings.Contains(string(out), "The moon landing failed") {
		t.Fatalf("expected unsupported sentence to be repaired out: %q", out)
	}
}

func TestRunPrecisionRepairRevertedWhenCoverageDrops(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research: "- Revenue was $1,200 in 2021 at Acme Corp.",
		outline:  "# Draft Title\n\n## Summary\nFacts: F1\ncover summary",
		section:  "Revenue was $1,200 in 2021 at Acme Corp. The moon landing failed.",
		precisionRecords: []string{
			`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":false,"reason":"unsupported bridge"}]}`,
		},
		precisionRepair: "The report mentions no concrete figures.",
	}
	opts := Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:        filepath.Join(dir, "artifacts"),
		ChunkSize:          6000,
		CheckPrecision:     true,
		Repair:             true,
		PrecisionBatchSize: 80,
	}
	res, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}, testPrompts(), "# Title\n\nbody", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Precision == nil || len(res.Precision.Unsupported) != 1 {
		t.Fatalf("precision should remain unsupported when repaired output drops coverage, got %+v", res.Precision)
	}
	precisionChecks := 0
	precisionRepairs := 0
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "PRECISION_REPAIR") {
			precisionRepairs++
		}
		if strings.HasPrefix(prompt, "PRECISION FACTS") {
			precisionChecks++
		}
	}
	if precisionChecks != 1 {
		t.Fatalf("expected initial precision check only, got %d", precisionChecks)
	}
	if precisionRepairs != 1 {
		t.Fatalf("expected precision repair attempt, got %d", precisionRepairs)
	}
	out, _ := os.ReadFile(opts.OutPath)
	if !strings.Contains(string(out), "The moon landing failed") {
		t.Fatalf("expected pre-repair output to remain, got %q", out)
	}
}

func TestRunPrecisionRepairSkipsWhenNoUnsupportedSentences(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{
		research:  "- Revenue was $1,200 in 2021 at Acme Corp.",
		outline:   "# Draft Title\n\n## Summary\nFacts: F1\ncover summary",
		section:   "Revenue was $1,200 in 2021 at Acme Corp.",
		precision: `{"verdicts":[{"i":1,"supported":true,"reason":""}]}`,
	}
	opts := Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          filepath.Join(dir, "facts.compiled.md"),
		ArtifactDir:        filepath.Join(dir, "artifacts"),
		ChunkSize:          6000,
		CheckPrecision:     true,
		Repair:             true,
		PrecisionBatchSize: 80,
	}
	if _, err := Run(context.Background(), RoleCompleters{Research: llm, Fuse: llm, Outline: llm, Section: llm, Edit: llm, Judge: llm}, testPrompts(), "# Title\n\nbody", opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	precisionChecks := 0
	precisionRepairs := 0
	for _, prompt := range llm.prompts {
		if strings.HasPrefix(prompt, "PRECISION_REPAIR") {
			precisionRepairs++
		}
		if strings.HasPrefix(prompt, "PRECISION FACTS") {
			precisionChecks++
		}
	}
	if precisionChecks != 1 {
		t.Fatalf("expected only one precision check, got %d", precisionChecks)
	}
	if precisionRepairs != 0 {
		t.Fatalf("expected no precision repair when there are no unsupported sentences, got %d", precisionRepairs)
	}
}
