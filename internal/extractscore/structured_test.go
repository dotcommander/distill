package extractscore

import (
	"testing"
)

const structuredSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["company", "price", "active", "website", "items"],
  "properties": {
    "company": {"type": "string", "evaluation_config": "string_case_insensitive"},
    "price": {
      "type": "number",
      "evaluation_config": {"metrics": [{"metric_id": "number_tolerance", "params": {"tolerance": 0.01}}]}
    },
    "active": {"type": "boolean"},
    "website": {"type": "string", "format": "uri", "evaluation_config": "string_exact"},
    "items": {"type": "array", "items": {"type": "string"}}
  }
}`

const structuredGold = `{
  "company": "Acme Inc",
  "price": 12.34,
  "active": true,
  "website": "https://www.example.com/",
  "items": ["alpha", "beta", "gamma"]
}`

func TestScoreStructuredPassesWithFieldMetrics(t *testing.T) {
	t.Parallel()
	pred := `{
  "company": "acme inc",
  "price": 12.345,
  "active": true,
  "website": "example.com",
  "items": ["gamma", "alpha", "beta"]
}`
	res, err := ScoreStructured("good", []byte(structuredSchema), []byte(structuredGold), []byte(pred))
	if err != nil {
		t.Fatalf("ScoreStructured: %v", err)
	}
	if !res.StructuralPass {
		t.Fatalf("structural pass = false: %v", res.StructuralErrors)
	}
	if res.Total != 5 || res.Passed != 5 {
		t.Fatalf("passed/total = %d/%d, want 5/5", res.Passed, res.Total)
	}
	if res.FieldScore != 1 || res.ItemScore != 1 || res.PassRate != 1 {
		t.Fatalf("scores = field %.3f item %.3f pass %.3f", res.FieldScore, res.ItemScore, res.PassRate)
	}
	items := fieldByPath(t, res, "$/items")
	if items.Matched != 3 || items.Missed != 0 || items.Spurious != 0 || items.F1 != 1 {
		t.Fatalf("array outcome = %+v", items)
	}
	website := fieldByPath(t, res, "$/website")
	if website.Metric != "string_url" || !website.Passed {
		t.Fatalf("website outcome = %+v", website)
	}
}

func TestScoreStructuredStructuralFailureZerosAggregate(t *testing.T) {
	t.Parallel()
	pred := `{
  "company": "Acme Inc",
  "price": "12.34",
  "active": true,
  "website": "https://example.com",
  "items": ["alpha", "delta"],
  "extra": "not allowed"
}`
	res, err := ScoreStructured("bad", []byte(structuredSchema), []byte(structuredGold), []byte(pred))
	if err != nil {
		t.Fatalf("ScoreStructured: %v", err)
	}
	if res.StructuralPass {
		t.Fatal("expected structural failure")
	}
	if len(res.StructuralErrors) != 2 {
		t.Fatalf("structural errors = %v, want 2", res.StructuralErrors)
	}
	if res.FieldScore != 0 || res.ItemScore != 0 || res.PassRate != 0 {
		t.Fatalf("structural failure must zero aggregates: %+v", res)
	}
	items := fieldByPath(t, res, "$/items")
	if items.Matched != 1 || items.Missed != 2 || items.Spurious != 1 {
		t.Fatalf("array outcome = %+v", items)
	}
}

func TestScoreStructuredInvalidPredictionJSON(t *testing.T) {
	t.Parallel()
	res, err := ScoreStructured("invalid", []byte(structuredSchema), []byte(structuredGold), []byte(`{"company":`))
	if err != nil {
		t.Fatalf("ScoreStructured: %v", err)
	}
	if res.StructuralPass {
		t.Fatal("expected structural failure")
	}
	if len(res.StructuralErrors) == 0 {
		t.Fatal("expected structural error")
	}
}

func fieldByPath(t *testing.T, res StructuredResult, p string) FieldOutcome {
	t.Helper()
	for _, f := range res.Fields {
		if f.Path == p {
			return f
		}
	}
	t.Fatalf("missing field %s in %+v", p, res.Fields)
	return FieldOutcome{}
}
