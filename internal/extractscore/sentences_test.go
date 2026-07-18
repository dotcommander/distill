package extractscore

import (
	"reflect"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	t.Parallel()
	input := "# Heading\n\nDr. Rivera measured 3.14 units. The result held.\n- Bullet fact. With detail.\n\n```go\nfmt.Println(\"ignore. This.\")\n```\nA final line without punctuation"
	want := []string{
		"Dr. Rivera measured 3.14 units.",
		"The result held.",
		"- Bullet fact. With detail.",
		"A final line without punctuation",
	}
	if got := SplitSentences(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitSentences mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSplitSentencesConservativeLowercaseContinuation(t *testing.T) {
	t.Parallel()
	input := "The device shipped. but this lowercase continuation stays attached. Next sentence starts."
	want := []string{
		"The device shipped. but this lowercase continuation stays attached.",
		"Next sentence starts.",
	}
	if got := SplitSentences(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitSentences mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
