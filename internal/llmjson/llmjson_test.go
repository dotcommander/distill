package llmjson

import "testing"

func TestExtractObjectReturnsLastBalancedObject(t *testing.T) {
	t.Parallel()
	in := "prefix {\"a\":\"ignored\"}\n```json\n{\"b\":\"brace { inside string }\"}\n```\nfooter"
	want := "{\"b\":\"brace { inside string }\"}"
	if got := ExtractObject(in); got != want {
		t.Fatalf("ExtractObject = %q, want %q", got, want)
	}
}

func TestExtractObjectMiss(t *testing.T) {
	t.Parallel()
	if got := ExtractObject("no balanced { object"); got != "" {
		t.Fatalf("ExtractObject miss = %q, want empty", got)
	}
}
