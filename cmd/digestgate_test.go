package cmd

import (
	"testing"

	"github.com/dotcommander/distill/internal/actions/digest"
	"github.com/dotcommander/distill/internal/extractscore"
)

func TestCheckDigestGate(t *testing.T) {
	t.Parallel()
	res := &digest.Result{
		Coverage: extractscore.SpecificsResult{Covered: 6, Total: 10},
		Words:    500,
	}
	// min-coverage above the achieved 0.60 ratio -> non-zero (error).
	if err := checkDigestGate(res, &digestFlags{minCoverage: 0.8}); err == nil {
		t.Fatal("expected coverage gate to fail when ratio below --min-coverage")
	}
	// min-coverage at/below the ratio -> pass.
	if err := checkDigestGate(res, &digestFlags{minCoverage: 0.6}); err != nil {
		t.Fatalf("expected pass when ratio meets --min-coverage, got %v", err)
	}
	// Word band: 500 words above max 400 -> fail.
	if err := checkDigestGate(res, &digestFlags{maxWords: 400}); err == nil {
		t.Fatal("expected word-band gate to fail when words exceed --max-words")
	}
	// Word band: 500 words below min 600 -> fail.
	if err := checkDigestGate(res, &digestFlags{minWords: 600}); err == nil {
		t.Fatal("expected word-band gate to fail when words below --min-words")
	}
	// All gates off (zero flags) -> pass.
	if err := checkDigestGate(res, &digestFlags{}); err != nil {
		t.Fatalf("expected pass with all gates disabled, got %v", err)
	}
}
