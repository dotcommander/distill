package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestCmdCompleterPipesPromptAndCapturesStdout(t *testing.T) {
	t.Parallel()
	// `cat` echoes stdin to stdout, so Complete returns the prompt verbatim.
	c := cmdCompleter{name: "cat"}
	got, err := c.Complete(context.Background(), `judge {"x":1}`)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(got, `{"x":1}`) {
		t.Fatalf("expected the prompt echoed back, got %q", got)
	}
}

func TestCmdCompleterReportsFailure(t *testing.T) {
	t.Parallel()
	c := cmdCompleter{name: "false"} // exits non-zero
	if _, err := c.Complete(context.Background(), "x"); err == nil {
		t.Fatal("expected an error from a failing judge command")
	}
}
