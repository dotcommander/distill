package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestCoreCommandHelpPreservesCobraProse(t *testing.T) {
	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{
			"distill is a command-line tool for splitting documents into chunks,",
			"digest score   Deterministic review of a digest draft (no LLM)",
		}},
		{[]string{"count", "--help"}, []string{
			"Count tokens (Claude tokenizer), characters, and lines in a file or stdin.",
			"Output format: json or plain",
		}},
		{[]string{"chunk", "--help"}, []string{
			"Split a document into chunks using various strategies.",
			"Semantic mode: two-sided coherence window for break validation (0 = engine default)",
			"Falls back to $DISTILL_EMBEDDING_MODEL when unset.",
		}},
		{[]string{"digest", "--help"}, []string{
			"Distill a long document into a cohesive rewrite without any single model",
			"Hard Claude token ceiling per chunk; oversize character chunks are split (0 disables)",
			"Reuse complete artifacts from a previous run in --artifacts to avoid repeated paid calls",
			"score    Deterministic review of digest drafts (no LLM)",
		}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := execute(context.Background(), tt.args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			normalizedHelp := strings.Join(strings.Fields(stdout.String()), " ")
			for _, want := range tt.want {
				if !strings.Contains(normalizedHelp, strings.Join(strings.Fields(want), " ")) {
					t.Fatalf("help for %v missing %q:\n%s", tt.args, want, stdout.String())
				}
			}
		})
	}
}

func TestCoreCommandFlagsCarryHelpTags(t *testing.T) {
	for _, command := range []any{countCommand{}, chunkCommand{}, digestCommand{}} {
		typ := reflect.TypeOf(command)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if _, positional := field.Tag.Lookup("arg"); positional {
				continue
			}
			if field.Tag.Get("help") == "" {
				t.Errorf("%s.%s has no help tag", typ.Name(), field.Name)
			}
		}
	}
}

func TestCountRejectsInvalidFormatBeforeStartLog(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var stdout, stderr bytes.Buffer
	err := execute(context.Background(), []string{"count", "--format", "bogus"}, strings.NewReader("ignored"), &stdout, &stderr)
	if err == nil || err.Error() != "unknown format: bogus" {
		t.Fatalf("got %v, want unknown format error", err)
	}
	if strings.Contains(logs.String(), "count start") {
		t.Fatalf("invalid format emitted count start log: %s", logs.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("invalid format wrote stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
