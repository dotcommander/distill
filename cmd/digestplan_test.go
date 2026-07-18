package cmd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dotcommander/distill/internal/actions/digest"
	"github.com/dotcommander/distill/internal/prompts"

	"os"
	"path/filepath"
	"testing"
)

func TestPlannedDigestCallsIgnoresUnverifiedArtifacts(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	responses := filepath.Join(artifactDir, "responses")
	if err := os.MkdirAll(responses, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{
		filepath.Join(responses, "chunk-001.md"):   "- fact",
		filepath.Join(responses, "outline.md"):     "# T\n\n## S\nintent",
		filepath.Join(responses, "section-001.md"): "body",
	}
	for path, content := range seed {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")
	f := &digestFlags{resume: true, noEdit: true}

	verified := plannedDigestCalls(1, f, factsPath, artifactDir, true)
	unverified := plannedDigestCalls(1, f, factsPath, artifactDir, false)
	if verified >= unverified {
		t.Fatalf("verified plan (%d) should reuse artifacts and cost fewer calls than unverified (%d)", verified, unverified)
	}
	// Unverified: 1 research + 1 outline + 3 sections (floored at minSectionEstimate), nothing counted as reused.
	if unverified != 5 {
		t.Fatalf("unverified plan = %d, want 5", unverified)
	}
	// Verified: research and outline reused; only 1 of the floored 3 sections was seeded, leaving 2 section calls.
	if verified != 2 {
		t.Fatalf("verified plan = %d, want 2", verified)
	}
}

type ledgerAuditLLM struct {
	precision []string
}

func (l *ledgerAuditLLM) Complete(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "CITE_SECTION"):
		return "Revenue was $1,200 in 2021 at Acme Corp. [F1]", nil
	case strings.HasPrefix(prompt, "CITE_REPAIR"):
		return "Revenue was $1,200 in 2021 at Acme Corp. [F1]\n\nProfit was $300 in 2021 at Acme Corp. [F2]", nil
	case strings.HasPrefix(prompt, "PRECISION_REPAIR"):
		return "Revenue was $1,200 in 2021 at Acme Corp. [F1]\n\nProfit was $300 in 2021 at Acme Corp. [F2]", nil
	case strings.HasPrefix(prompt, "PRECISION"):
		if len(l.precision) == 0 {
			return `{"verdicts":[{"i":1,"supported":true,"reason":""}]}`, nil
		}
		out := l.precision[0]
		l.precision = l.precision[1:]
		return out, nil
	case strings.HasPrefix(prompt, "OUTLINE"):
		return "# Audit\n\n## Money\nFacts: F1, F2", nil
	case strings.HasPrefix(prompt, "SECTION"):
		return "Revenue was $1,200 in 2021 at Acme Corp.", nil
	default:
		return "- Revenue was $1,200 in 2021 at Acme Corp.\n- Profit was $300 in 2021 at Acme Corp.", nil
	}
}

func TestPlannedDigestCallsCoversLedgerRepairEvents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")
	llm := &ledgerAuditLLM{precision: []string{
		`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":false,"reason":"unsupported"}]}`,
		`{"verdicts":[{"i":1,"supported":true,"reason":""},{"i":2,"supported":true,"reason":""}]}`,
	}}
	p := &prompts.Set{
		Research:        "RESEARCH {{CHUNK_ID}} {{CHUNK}}",
		Outline:         "OUTLINE {{FACTS}}",
		Section:         "SECTION {{HEADING}} {{FACTS}}",
		CiteSection:     "CITE_SECTION\n",
		CiteRepair:      "CITE_REPAIR\n",
		Repair:          "REPAIR {{ARTICLE}} {{MISSING}}",
		Precision:       "PRECISION {{FACTS}} {{SENTENCES}}",
		PrecisionRepair: "PRECISION_REPAIR {{FACTS}} {{FLAGGED}} {{ARTICLE}}",
	}
	_, err := digest.Run(context.Background(), digest.RoleCompleters{
		Research: llm,
		Fuse:     llm,
		Outline:  llm,
		Section:  llm,
		Edit:     llm,
		Judge:    llm,
	}, p, "# Source\n\nRevenue and profit.", digest.Options{
		Style:              "brief",
		OutPath:            filepath.Join(dir, "out.md"),
		FactsPath:          factsPath,
		ArtifactDir:        artifactDir,
		ChunkSize:          6000,
		Concurrency:        1,
		Edit:               false,
		Cite:               true,
		Repair:             true,
		CheckPrecision:     true,
		PrecisionBatchSize: 80,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(artifactDir, "run-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	callEvents := 0
	stages := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var ev struct {
			Stage  string `json:"stage"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("parse ledger line %q: %v", line, err)
		}
		if ev.Action == "call" {
			callEvents++
			stages[ev.Stage] = true
		}
	}
	for _, stage := range []string{"cite-repair", "precision-repair", "precision"} {
		if !stages[stage] {
			t.Fatalf("ledger missing %s call event; stages=%v\n%s", stage, stages, data)
		}
	}
	flags := &digestFlags{noEdit: true, cite: true, repair: true, checkPrecision: true}
	planned := plannedDigestCalls(1, flags, factsPath, artifactDir, false)
	if planned < callEvents {
		t.Fatalf("planned calls undercounted ledger: planned=%d actual_call_events=%d\n%s", planned, callEvents, data)
	}
}

func TestPlannedDigestCallsFloorsSectionEstimateForSmallChunkCounts(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")
	f := &digestFlags{noEdit: true}

	planned := plannedDigestCalls(1, f, factsPath, artifactDir, false)
	// 1 research + 1 outline + minSectionEstimate(3) sections, nothing reused.
	want := 1 + 1 + minSectionEstimate
	if planned != want {
		t.Fatalf("planned = %d, want %d (chunks=1 should floor section estimate at %d)", planned, want, minSectionEstimate)
	}
}

func TestPlannedDigestCallsCountsRepairPass(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withRepair := &digestFlags{noEdit: true, repair: true}

	base := plannedDigestCalls(1, without, factsPath, artifactDir, false)
	repaired := plannedDigestCalls(1, withRepair, factsPath, artifactDir, false)
	if repaired != base+1 {
		t.Fatalf("repair plan = %d, want base+1 = %d (base %d)", repaired, base+1, base)
	}
}

func TestPlannedDigestCallsCountsDocContextWhenFresh(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withDocContext := &digestFlags{noEdit: true, docContext: true}

	base := plannedDigestCalls(2, without, factsPath, artifactDir, false)
	planned := plannedDigestCalls(2, withDocContext, factsPath, artifactDir, false)
	if planned != base+1 {
		t.Fatalf("doc-context plan = %d, want base+1 = %d (base %d)", planned, base+1, base)
	}

	responses := filepath.Join(artifactDir, "responses")
	if err := os.MkdirAll(responses, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(responses, "doc-context.md"), []byte("TITLE: Reused"), 0o644); err != nil {
		t.Fatal(err)
	}
	reused := plannedDigestCalls(2, withDocContext, factsPath, artifactDir, true)
	withoutReusedHeader := plannedDigestCalls(2, without, factsPath, artifactDir, true)
	if reused != withoutReusedHeader {
		t.Fatalf("reusable doc-context should not add a call, got %d want %d", reused, withoutReusedHeader)
	}
}

func TestCheckDigestGateMinCited(t *testing.T) {
	t.Parallel()
	err := checkDigestGate(&digest.Result{
		Citations: &digest.CitationResult{Covered: 1, Total: 2},
	}, &digestFlags{minCited: 0.75})
	if err == nil {
		t.Fatal("expected --min-cited gate failure")
	}
	if !strings.Contains(err.Error(), "cited fact coverage") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := checkDigestGate(&digest.Result{
		Citations: &digest.CitationResult{Covered: 2, Total: 2},
	}, &digestFlags{minCited: 0.75}); err != nil {
		t.Fatalf("expected --min-cited gate pass, got %v", err)
	}
}

func TestPlannedDigestCallsCountsPrecisionJudge(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withPrecision := &digestFlags{noEdit: true, checkPrecision: true}

	base := plannedDigestCalls(1, without, factsPath, artifactDir, false)
	planned := plannedDigestCalls(1, withPrecision, factsPath, artifactDir, false)
	if planned != base+1 {
		t.Fatalf("precision plan = %d, want base+1 = %d (base %d)", planned, base+1, base)
	}
}

func TestPlannedDigestCallsCountsCascadePerFreshChunk(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withCascade := &digestFlags{noEdit: true, cascade: true, cascadeThreshold: 0.55}

	base := plannedDigestCalls(2, without, factsPath, artifactDir, false)
	planned := plannedDigestCalls(2, withCascade, factsPath, artifactDir, false)
	if planned != base+2 {
		t.Fatalf("cascade plan = %d, want base+2 = %d (base %d)", planned, base+2, base)
	}
}

func TestPlannedDigestCallsSkipsCascadeWhenThresholdZero(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withCascadeDisabled := &digestFlags{noEdit: true, cascade: true, cascadeThreshold: 0}

	base := plannedDigestCalls(2, without, factsPath, artifactDir, false)
	planned := plannedDigestCalls(2, withCascadeDisabled, factsPath, artifactDir, false)
	if planned != base {
		t.Fatalf("disabled cascade plan = %d, want base = %d", planned, base)
	}
}

func TestPlannedDigestCallsCountsMergeFactsEstimate(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withMerge := &digestFlags{noEdit: true, mergeFacts: true}

	base := plannedDigestCalls(2, without, factsPath, artifactDir, false)
	planned := plannedDigestCalls(2, withMerge, factsPath, artifactDir, false)
	if planned != base+2 {
		t.Fatalf("merge plan = %d, want base+2 = %d (base %d)", planned, base+2, base)
	}
}

func TestPlannedDigestCallsCountsCiteRepair(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	without := &digestFlags{noEdit: true}
	withCite := &digestFlags{noEdit: true, cite: true}

	base := plannedDigestCalls(1, without, factsPath, artifactDir, false)
	planned := plannedDigestCalls(1, withCite, factsPath, artifactDir, false)
	if planned != base+1 {
		t.Fatalf("cite plan = %d, want base+1 = %d (base %d)", planned, base+1, base)
	}
}

func TestPlannedDigestCallsCountsPrecisionRepairPass(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()
	factsPath := filepath.Join(artifactDir, "facts.compiled.md")

	baseFlags := &digestFlags{noEdit: true}
	repairOnly := &digestFlags{noEdit: true, repair: true}
	precisionOnly := &digestFlags{noEdit: true, checkPrecision: true}
	both := &digestFlags{noEdit: true, repair: true, checkPrecision: true}

	base := plannedDigestCalls(1, baseFlags, factsPath, artifactDir, false)
	repairDelta := plannedDigestCalls(1, repairOnly, factsPath, artifactDir, false) - base
	precisionDelta := plannedDigestCalls(1, precisionOnly, factsPath, artifactDir, false) - base
	bothDelta := plannedDigestCalls(1, both, factsPath, artifactDir, false) - base

	// Precision repair adds exactly 2 calls: one repair call + one re-check.
	precisionRepairDelta := bothDelta - repairDelta - precisionDelta
	if precisionRepairDelta != 2 {
		t.Fatalf("precision repair delta = %d, want 2 (repairDelta=%d, precisionDelta=%d, bothDelta=%d, base=%d)",
			precisionRepairDelta, repairDelta, precisionDelta, bothDelta, base)
	}
}

func TestCheckDigestGateMinPrecision(t *testing.T) {
	t.Parallel()
	err := checkDigestGate(&digest.Result{
		Precision: &digest.PrecisionResult{Supported: 1, Total: 2, Precision: 0.5},
	}, &digestFlags{minPrecision: 0.75})
	if err == nil {
		t.Fatal("expected --min-precision gate failure")
	}
	if !strings.Contains(err.Error(), "sentence precision") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := checkDigestGate(&digest.Result{
		Precision: &digest.PrecisionResult{Supported: 2, Total: 2, Precision: 1},
	}, &digestFlags{minPrecision: 0.75}); err != nil {
		t.Fatalf("expected --min-precision gate pass, got %v", err)
	}
}
