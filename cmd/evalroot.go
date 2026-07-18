package cmd

import (
	"context"
	"io"
	"slices"
)

const evalDescription = "Evaluation and model-benchmark companion for distill"

type evalCLI struct {
	Eval   evalGroup   `cmd:"" help:"Score extractions against a reference"`
	Grade  gradeGroup  `cmd:"" help:"Grade digests on merit as a read (currently: calibrate the pairwise judge)"`
	Models modelsGroup `cmd:"" help:"Rank models on a task"`
}
type evalGroup struct {
	Judge      evalCommand       `cmd:"" help:"Score candidate extractions against a reference with an LLM judge"`
	Facts      scoreCommand      `cmd:"" help:"Score per-chunk extractions against golden-fact fixtures (deterministic, no LLM)"`
	Structured structuredCommand `cmd:"" help:"Score JSON extractions against a schema-backed gold file (deterministic, no LLM)"`
	Optimize   optimizeCommand   `cmd:"" help:"Optimize the digest research prompt against golden extraction fixtures"`
}
type gradeGroup struct {
	Calibrate  calibrateCommand  `cmd:"" help:"Test whether the pairwise merit judge discriminates or just guesses"`
	Tournament tournamentCommand `cmd:"" help:"Rank digests by merit via an order-swapped pairwise judge (merge sort + cycle audit)"`
	Recognize  recognizeCommand  `cmd:"" help:"self-recognition probe: can each judge spot its own digest?"`
	Panel      panelCommand      `cmd:"" help:"de-biased leave-one-out panel tournament (no model judges its own digest)"`
}
type modelsGroup struct {
	Code     codeCommand   `cmd:"" help:"Rank models by unit-test pass-rate on coding problems (compiles + runs model code)"`
	Comedy   comedyCommand `cmd:"" help:"Rank models by how funny their short comedy bits are (de-biased leave-one-out panel)"`
	Label    labelCommand  `cmd:"" help:"Evaluate models on a single-label task (classification/sentiment) by exact gold match"`
	Rankings rankingsGroup `cmd:"" help:"Inspect and update remote model rankings → per-role config"`
	Trace    traceCommand  `cmd:"" name:"trace-go" help:"Rank models by exact stdout prediction on deterministic Go programs"`
}
type rankingsGroup struct {
	Fetch rankingsFetchCommand `cmd:"" help:"Fetch board rankings via the defuddle CLI and update rankings.yaml"`
	Apply rankingsApplyCommand `cmd:"" help:"Re-derive per-role model picks from rankings and write them into config.yaml"`
	Show  rankingsShowCommand  `cmd:"" help:"Print the current per-role model picks from config"`
}

func ExecuteEval(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return executeEval(ctx, args, stdin, stdout, stderr)
}
func executeEval(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"--help"}
	}
	for _, group := range [][]string{{"eval"}, {"grade"}, {"models"}, {"models", "rankings"}} {
		if slices.Equal(args, group) {
			args = append(args, "--help")
			break
		}
	}
	var tree evalCLI
	return parseAndRun(ctx, "distill-eval", evalDescription, &tree, args, stdin, stdout, stderr)
}
