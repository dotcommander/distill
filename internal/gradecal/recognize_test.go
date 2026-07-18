package gradecal

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// fakeRecognizeRenderer renders indexed candidates in a parseable offline format.
type fakeRecognizeRenderer struct{}

func (fakeRecognizeRenderer) RenderRecognize(digests []string) string {
	var b strings.Builder
	for i, d := range digests {
		fmt.Fprintf(&b, "[%d]=%s\n", i, d)
	}
	return b.String()
}

// recognizeJudgeFunc adapts prompt-driven fakes to the Completer interface.
type recognizeJudgeFunc func(prompt string) (string, error)

func (f recognizeJudgeFunc) Complete(_ context.Context, prompt string) (string, error) {
	return f(prompt)
}

func recognitionField() []Digest {
	return []Digest{
		{Name: "amber", Text: "Amber title\n\namber"},
		{Name: "blue", Text: "Blue title\n\nblue"},
		{Name: "crimson", Text: "Crimson title\n\ncrimson"},
		{Name: "dove", Text: "Dove title\n\ndove"},
	}
}

func selfRecognizeJudge(ownText string) recognizeJudgeFunc {
	return func(prompt string) (string, error) {
		for i, text := range parseRecognizePrompt(prompt) {
			if text == ownText {
				return strconv.Itoa(i), nil
			}
		}
		return "none", nil
	}
}

func parseRecognizePrompt(prompt string) []string {
	lines := strings.Split(strings.TrimSpace(prompt), "\n")
	out := make([]string, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, "[") {
			continue
		}
		end := strings.Index(line, "]=")
		if end < 0 {
			continue
		}
		i, err := strconv.Atoi(line[1:end])
		if err != nil || i < 0 || i >= len(out) {
			continue
		}
		out[i] = line[end+2:]
	}
	return out
}

func TestRecognition_PerfectSelfRecognizer(t *testing.T) {
	t.Parallel()
	field := recognitionField()
	judges := map[string]Completer{
		"crimson": selfRecognizeJudge("crimson"),
	}

	got := RunRecognition(context.Background(), judges, fakeRecognizeRenderer{}, field, []string{"crimson"}, 8, 4, 13)
	if len(got) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(got))
	}
	if got[0].Accuracy() != 1.0 {
		t.Fatalf("Accuracy = %v, want 1.0", got[0].Accuracy())
	}
	if !got[0].AboveChance() {
		t.Fatal("perfect self-recognizer should be above chance")
	}
	if !strings.Contains(got[0].Verdict(), "RECOGNITION PRESENT") {
		t.Fatalf("Verdict = %q, want recognition present", got[0].Verdict())
	}
}

func TestRecognition_RandomGuesser(t *testing.T) {
	t.Parallel()
	judges := map[string]Completer{
		"crimson": recognizeJudgeFunc(func(string) (string, error) { return "0", nil }),
	}

	got := RunRecognition(context.Background(), judges, fakeRecognizeRenderer{}, recognitionField(), []string{"crimson"}, 8, 4, 12)
	if len(got) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(got))
	}
	if got[0].AboveChance() {
		t.Fatalf("AboveChance = true, want false; hits=%d trials=%d chance=%v", got[0].Hits, got[0].Trials, got[0].Chance())
	}
}

func TestCanonicalize_StripsTitleTells(t *testing.T) {
	t.Parallel()
	got := Canonicalize("# Title\n\n**bold** text\n- bullet one\n_em_ word")
	for _, marker := range []string{"#", "**", "- "} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			if strings.Contains(got, marker) {
				t.Fatalf("Canonicalize output contains marker %q: %q", marker, got)
			}
		})
	}
	for _, word := range []string{"bold", "text", "bullet", "word"} {
		t.Run(word, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(got, word) {
				t.Fatalf("Canonicalize output missing word %q: %q", word, got)
			}
		})
	}
}

func TestParseIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		setSize int
		want    int
		wantErr bool
	}{
		{name: "bare", raw: "2", setSize: 4, want: 2},
		{name: "sentence", raw: "I pick 3.", setSize: 4, want: 3},
		{name: "out_of_range", raw: "9", setSize: 4, wantErr: true},
		{name: "no_integer", raw: "none", setSize: 4, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseIndex(tc.raw, tc.setSize)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseIndex(%q, %d) error = nil, want error", tc.raw, tc.setSize)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseIndex(%q, %d) error = %v", tc.raw, tc.setSize, err)
			}
			if got != tc.want {
				t.Fatalf("parseIndex(%q, %d) = %d, want %d", tc.raw, tc.setSize, got, tc.want)
			}
		})
	}
}

func TestRunRecognition_Deterministic(t *testing.T) {
	t.Parallel()
	field := recognitionField()
	judges := map[string]Completer{
		"blue": recognizeJudgeFunc(func(string) (string, error) { return "0", nil }),
	}

	first := RunRecognition(context.Background(), judges, fakeRecognizeRenderer{}, field, []string{"blue"}, 8, 4, 42)
	second := RunRecognition(context.Background(), judges, fakeRecognizeRenderer{}, field, []string{"blue"}, 8, 4, 42)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("result lengths = %d/%d, want 1/1", len(first), len(second))
	}
	if first[0].Hits != second[0].Hits {
		t.Fatalf("Hits = %d/%d, want identical", first[0].Hits, second[0].Hits)
	}
}
