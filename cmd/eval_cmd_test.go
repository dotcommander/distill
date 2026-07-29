package cmd

import (
	"context"
	"os"
	"path/filepath"
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

func TestRunEvalInvalidCorpusDoesNotCreateOutputDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	outDir := filepath.Join(root, "out")

	err := runEval(&runContext{ctx: context.Background()}, &evalFlags{
		chunks:       filepath.Join(root, "missing-chunks"),
		reference:    filepath.Join(root, "missing-reference"),
		candidates:   filepath.Join(root, "missing-candidate"),
		judgeCmd:     "cat",
		out:          outDir,
		judgeTimeout: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "reading source directory") {
		t.Fatalf("runEval error = %v, want source preflight failure", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("invalid corpus created output directory: %v", statErr)
	}
}
