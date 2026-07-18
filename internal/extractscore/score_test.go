package extractscore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	expectedDir = "../../testdata/extraction/expected"
	baselineDir = "../../testdata/extraction/baseline-35b/responses"
)

func TestScoreChunk(t *testing.T) {
	t.Parallel()
	facts := []Fact{
		{ID: "a", Anchors: []string{"hailo-8", "26 tops"}},
		{ID: "b", Any: [][]string{{"12 mp"}, {"12-megapixel"}}},
		{ID: "c", Anchors: []string{"ip66"}},
	}
	ext := "The Hailo-8 is rated at 26 TOPS. Sensor is 12-megapixel. Enclosure IP66-rated."
	if got := ScoreChunk(ext, facts); got.Covered != 3 || got.Total != 3 {
		t.Fatalf("want 3/3, got %d/%d (missing %v)", got.Covered, got.Total, got.Missing)
	}
	// anchor miss: one of two required anchors absent -> not covered
	if got := ScoreChunk("only 26 tops here", []Fact{{ID: "a", Anchors: []string{"hailo-8", "26 tops"}}}); got.Covered != 0 {
		t.Fatalf("anchor miss: want 0 covered, got %d", got.Covered)
	}
	// any-group: no group satisfied -> not covered
	if got := ScoreChunk("no sensor info", []Fact{{ID: "b", Any: [][]string{{"12 mp"}, {"12-megapixel"}}}}); got.Covered != 0 {
		t.Fatalf("unmatched any: want 0 covered, got %d", got.Covered)
	}
	// normalization: case + collapsed whitespace
	if got := ScoreChunk("HAILO-8\n\n  26   TOPS", []Fact{{ID: "a", Anchors: []string{"hailo-8", "26 tops"}}}); got.Covered != 1 {
		t.Fatalf("normalization: want 1 covered, got %d", got.Covered)
	}
}

func TestContainsTokenBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		norm, anchor string
		want         bool
	}{
		// False positives the boundary matcher must REJECT.
		{"25_not_in_2025", "ships in 2025", "25", false},
		{"5.4_not_in_15.4", "draws 15.4 watts", "5.4", false},
		{"55_not_in_550", "rated 550 units", "55", false},
		{"349_not_in_3490", "costs 3490", "349", false},
		{"memo_not_in_memory", "shared memory pool", "memo", false},
		{"pro_not_in_product", "a product brief", "pro", false},
		{"5.4_not_in_5.45", "draws 5.45 watts", "5.4", false},
		// A real decimal point still extends-left: "55" must not match "0.55".
		{"55_not_in_0.55_decimal", "ratio 0.55 today", "55", false},
		// True matches the boundary matcher must PRESERVE.
		{"349_with_dollar", "retails for $349", "349", true},
		{"349_sentence_period", "retails for $349.", "349", true},
		{"5.4_with_unit", "draws 5.4w idle", "5.4", true},
		{"55_with_degree", "up to 55°c", "55", true},
		{"cb52_sentence_period", "beats the cb52.", "cb52", true},
		{"pro_standalone", "the pro tier", "pro", true},
		// An abbreviation dot (circa) is NOT a decimal: "1720" matches "c.1720".
		{"1720_circa_abbrev_dot", "born c.1720 in town", "1720", true},
		{"phrase_anchor", "is a security camera", "security camera", true},
		{"normalized_match", "26   tops", "26 tops", true},
		// Thousands-separator commas are stripped before matching.
		{"comma_thousands_match", "rated for 3,000 cycles", "3000", true},
		{"comma_million_match", "starts at $12,000", "12000", true},
		{"comma_strip_no_false_pos", "rated for 1,250 cycles", "125", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsToken(normalize(tc.norm), normalize(tc.anchor)); got != tc.want {
				t.Fatalf("containsToken(%q, %q) = %v, want %v", tc.norm, tc.anchor, got, tc.want)
			}
		})
	}
}

func TestBaseline35BPerChunk(t *testing.T) {
	t.Parallel()
	thresholds := map[string]float64{"chunk-001": 0.5}
	const defaultThreshold = 0.8

	run, err := ScoreRun(expectedDir, baselineDir)
	if err != nil {
		t.Fatalf("ScoreRun: %v", err)
	}
	if len(run.Chunks) == 0 {
		t.Fatal("no chunks scored — check fixture glob / testdata paths")
	}
	for _, cr := range run.Chunks {
		t.Run(cr.Chunk, func(t *testing.T) {
			t.Parallel()
			th := defaultThreshold
			if v, ok := thresholds[cr.Chunk]; ok {
				th = v
			}
			t.Logf("%s recall=%.2f (%d/%d) missing=%v", cr.Chunk, cr.Recall(), cr.Covered, cr.Total, cr.Missing)
			if cr.Recall() < th {
				t.Errorf("%s recall %.2f below threshold %.2f (missing %v)", cr.Chunk, cr.Recall(), th, cr.Missing)
			}
		})
	}
	if run.Recall() < 0.85 {
		t.Errorf("overall micro-avg recall %.2f below 0.85", run.Recall())
	}
	t.Logf("baseline-35b overall recall=%.3f (%d/%d)", run.Recall(), run.Covered, run.Total)
}

func TestRenderINDEXTieBreakByName(t *testing.T) {
	t.Parallel()
	tie := RunResult{Covered: 1, Total: 2} // recall 0.5 for all
	cands := []Candidate{{Name: "zeta", Run: tie}, {Name: "alpha", Run: tie}, {Name: "mid", Run: tie}}
	out := RenderINDEX(cands)
	ai, mi, zi := strings.Index(out, "alpha"), strings.Index(out, "mid"), strings.Index(out, "zeta")
	if ai >= mi || mi >= zi {
		t.Fatalf("tie rows not name-ordered: alpha@%d mid@%d zeta@%d\n%s", ai, mi, zi, out)
	}
}

func TestScoreRunZeroResponsesErrors(t *testing.T) {
	t.Parallel()
	if _, err := ScoreRun(expectedDir, t.TempDir()); err == nil {
		t.Fatal("expected error when no response files match any fixture")
	}
}

func TestLoadFixtureValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty chunk":   `{"chunk":"","facts":[{"id":"a"}]}`,
		"empty fact id": `{"chunk":"c1","facts":[{"id":""}]}`,
		"dup fact id":   `{"chunk":"c1","facts":[{"id":"a"},{"id":"a"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "chunk-001.json")
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFixture(p); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}
