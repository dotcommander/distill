package labelscore

import (
	"os"
	"testing"
)

func TestParseLabel(t *testing.T) {
	t.Parallel()
	allowed := []string{"positive", "negative", "neutral"}
	cases := []struct {
		raw     string
		wantLbl string
		wantOK  bool
	}{
		{"Positive.", "positive", true},
		{"  NEGATIVE  ", "negative", true},
		{"The sentiment is neutral here.", "neutral", true},
		{"", "", false},                     // unparseable
		{"banana", "", false},               // out-of-taxonomy
		{"positive or negative", "", false}, // ambiguous (2 hits)
		{"This is nonnegative.", "", false}, // embedded substring, not a label token
	}
	for _, c := range cases {
		got, ok := ParseLabel(c.raw, allowed)
		if got != c.wantLbl || ok != c.wantOK {
			t.Errorf("ParseLabel(%q) = (%q,%v), want (%q,%v)", c.raw, got, ok, c.wantLbl, c.wantOK)
		}
	}
}

func TestParseLabel_MultiWordLabelsUsePhraseBoundaries(t *testing.T) {
	t.Parallel()
	allowed := []string{"very positive", "negative"}
	cases := []struct {
		raw     string
		wantLbl string
		wantOK  bool
	}{
		{"The answer is very positive.", "very positive", true},
		{"This is very positively framed.", "", false},
	}
	for _, c := range cases {
		got, ok := ParseLabel(c.raw, allowed)
		if got != c.wantLbl || ok != c.wantOK {
			t.Errorf("ParseLabel(%q) = (%q,%v), want (%q,%v)", c.raw, got, ok, c.wantLbl, c.wantOK)
		}
	}
}

func TestScoreMetrics(t *testing.T) {
	t.Parallel()
	allowed := []string{"positive", "negative"}
	preds := []Prediction{
		{ID: "1", Gold: "positive", Pred: "positive", InVocab: true},
		{ID: "2", Gold: "positive", Pred: "negative", InVocab: true},
		{ID: "3", Gold: "negative", Pred: "negative", InVocab: true},
		{ID: "4", Gold: "negative", Pred: "", InVocab: false},      // unparseable
		{ID: "5", Gold: "positive", Pred: "maybe", InVocab: false}, // out-of-vocab
	}
	ms := Score("m", preds, allowed)
	if ms.N != 5 {
		t.Fatalf("N = %d, want 5", ms.N)
	}
	if ms.Unparseable != 1 {
		t.Errorf("Unparseable = %d, want 1", ms.Unparseable)
	}
	if ms.OutOfVocab != 1 {
		t.Errorf("OutOfVocab = %d, want 1", ms.OutOfVocab)
	}
	if ms.Accuracy < 0.39 || ms.Accuracy > 0.41 {
		t.Errorf("Accuracy = %.3f, want ~0.400", ms.Accuracy)
	}
	if len(ms.PerClass) != 2 {
		t.Fatalf("PerClass len = %d, want 2", len(ms.PerClass))
	}
}

func TestLoadFixtureRejectsGoldNotInAllowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/bad.json"
	if err := os.WriteFile(path, []byte(`{"task":"sentiment","allowed_labels":["positive","negative"],"items":[{"id":"1","text":"x","gold_label":"happy"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFixture(path); err == nil {
		t.Fatal("expected error for gold_label not in allowed_labels, got nil")
	}
}
