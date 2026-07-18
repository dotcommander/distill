package extractscore

import "testing"

func TestSpecificsCoverage(t *testing.T) {
	t.Parallel()
	facts := "- The Voyager 1 probe launched in 1977 from Cape Canaveral.\n" +
		"- Budget was $250M; NASA led it for 5 nations.\n" +
		"- Coverage reached 92% in 2025."
	article := "Voyager launched in 1977 from Cape Canaveral. NASA ran it. Coverage reached 92%."
	got := SpecificsCoverage(facts, article)

	// specifics: 1977, $250M, 92%, 2025, NASA, Cape Canaveral (6).
	// dropped at extraction: bare "1" and "5"; "The Voyager" -> one real word.
	if got.Total != 6 {
		t.Fatalf("Total = %d, want 6 (missing=%v)", got.Total, got.Missing)
	}
	// article keeps 1977, 92%, NASA, Cape Canaveral (4); drops $250M, 2025.
	if got.Covered != 4 {
		t.Fatalf("Covered = %d, want 4 (missing=%v)", got.Covered, got.Missing)
	}
	for _, want := range []string{"$250M", "2025"} {
		found := false
		for _, m := range got.Missing {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in Missing, got %v", want, got.Missing)
		}
	}
}

func TestSpecificsCoverageBoundary(t *testing.T) {
	t.Parallel()
	// "25" must NOT count as covered by "2025" (boundary-checked match).
	got := SpecificsCoverage("- Exactly 25 units shipped.", "We shipped in 2025, many units.")
	if got.Total != 1 {
		t.Fatalf("Total = %d, want 1 (missing=%v)", got.Total, got.Missing)
	}
	if got.Covered != 0 || len(got.Missing) != 1 || got.Missing[0] != "25" {
		t.Fatalf("expected only 25 missing, got covered=%d missing=%v", got.Covered, got.Missing)
	}
}

func TestSpecificsCoverageIgnoresChunkHeadings(t *testing.T) {
	t.Parallel()
	// "001" from a "## chunk-001" scaffolding heading must NOT count as a specific.
	got := SpecificsCoverage("## chunk-001\n\n- The year was 1977.", "It happened in 1977.")
	if got.Total != 1 {
		t.Fatalf("Total = %d, want 1 (chunk heading must be ignored); missing=%v", got.Total, got.Missing)
	}
	if got.Covered != 1 || len(got.Missing) != 0 {
		t.Fatalf("expected 1977 covered with nothing missing; got covered=%d missing=%v", got.Covered, got.Missing)
	}
}

func TestSpecificsCoverageStripsRecordIDs(t *testing.T) {
	t.Parallel()
	// FamilySearch-style IDs ("LYQM-85P") and their fragments ("85", "LYQM")
	// are not content specifics. Only "Abiah Blankenship" and "1785" survive.
	got := SpecificsCoverage("- Abiah Blankenship (LYQM-85P) born 1785.", "Abiah Blankenship was born in 1785.")
	if got.Total != 2 {
		t.Fatalf("Total = %d, want 2 (Abiah Blankenship + 1785; record ID stripped); missing=%v", got.Total, got.Missing)
	}
	if got.Covered != 2 || len(got.Missing) != 0 {
		t.Fatalf("expected both covered, nothing missing; got covered=%d missing=%v", got.Covered, got.Missing)
	}
}

func TestWordBandOK(t *testing.T) {
	t.Parallel()
	cases := []struct {
		words, lo, hi int
		ok            bool
	}{
		{500, 0, 0, true},     // both bounds off
		{500, 600, 0, false},  // below floor
		{500, 400, 0, true},   // above floor, no ceiling
		{500, 0, 400, false},  // above ceiling
		{500, 0, 600, true},   // within ceiling
		{500, 400, 600, true}, // within full band
	}
	for _, c := range cases {
		if got := WordBandOK(c.words, c.lo, c.hi); got != c.ok {
			t.Fatalf("WordBandOK(%d,%d,%d)=%v want %v", c.words, c.lo, c.hi, got, c.ok)
		}
	}
}
