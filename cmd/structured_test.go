package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredCommandWritesReports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	schema := filepath.Join(root, "schema.json")
	gold := filepath.Join(root, "gold.json")
	good := filepath.Join(root, "good.json")
	bad := filepath.Join(root, "bad.json")
	out := filepath.Join(root, "out")
	writeStructuredFile(t, schema, `{
  "type": "object",
  "additionalProperties": false,
  "required": ["id", "amount"],
  "properties": {
    "id": {"type": "string", "evaluation_config": "string_exact"},
    "amount": {"type": "number", "evaluation_config": {"metrics": [{"metric_id": "number_tolerance", "params": {"tolerance": 0.01}}]}}
  }
}`)
	writeStructuredFile(t, gold, `{"id":"A-1","amount":10.0}`)
	writeStructuredFile(t, good, `{"id":"A-1","amount":10.005}`)
	writeStructuredFile(t, bad, `{"id":"A-2","amount":"10.0","extra":true}`)

	var stdout bytes.Buffer
	err := executeEval(context.Background(), []string{
		"eval", "structured",
		"--schema", schema,
		"--gold", gold,
		"--candidates", good + "," + bad,
		"--out", out,
	}, strings.NewReader(""), &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "| 1 | good | true | 1.000 | 1.000 | 1.000 | 2/2 |") {
		t.Fatalf("stdout missing good rank:\n%s", got)
	}
	if !strings.Contains(got, "| 2 | bad | false | 0.000 | 0.000 | 0.000 |") {
		t.Fatalf("stdout missing bad rank:\n%s", got)
	}
	for _, rel := range []string{"INDEX.md", "good.summary.md", "good.report.json", "bad.summary.md", "bad.report.json"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
}

func writeStructuredFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
