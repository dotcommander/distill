package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/actions/eval"
	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/prompts"
)

// evalFlags holds the resolved --eval flag values for one invocation,
// closure-scoped in newEvalCmd (no package globals).
type evalFlags struct {
	chunks       string
	reference    string
	candidates   string
	judgeModel   string
	judgeCmd     string
	baseURL      string
	out          string
	judgeTimeout int
	local        bool
	deepseek     bool
}

func runEval(cmd *runContext, f *evalFlags) error {
	start := time.Now()
	if f.chunks == "" || f.reference == "" || f.candidates == "" {
		return errors.New("eval requires --chunks, --reference, and --candidates")
	}
	candidateDirs := splitCandidates(f.candidates)
	if len(candidateDirs) == 0 {
		return errors.New("eval: --candidates lists no directories")
	}

	judge, err := buildJudge(cmd.Context(), f)
	if err != nil {
		return err
	}
	p, err := prompts.Load()
	if err != nil {
		return err
	}
	outDir, err := resolveArtifactDir(f.out)
	if err != nil {
		return err
	}

	results, err := eval.Run(cmd.Context(), judge, p, eval.Options{
		ChunksDir:     f.chunks,
		ReferenceDir:  f.reference,
		CandidateDirs: candidateDirs,
		OutDir:        outDir,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Evaluations written to: %s\n", outDir)
	for i, r := range results {
		fmt.Fprintf(cmd.ErrOrStderr(), "  %d. %-28s P=%.3f R=%.3f F1=%.3f\n",
			i+1, r.Name, r.Metrics.Precision, r.Metrics.Recall, r.Metrics.F1)
	}
	slog.InfoContext(cmd.Context(), "eval done", "candidates", len(results), "duration", time.Since(start))
	return nil
}

// splitCandidates parses the comma-separated --candidates value into dirs.
func splitCandidates(csv string) []string {
	var dirs []string
	for _, d := range strings.Split(csv, ",") {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// buildJudge selects the judge backend: an explicit external command
// (--judge-cmd) when set, otherwise the Wormhole-backed provider client.
func buildJudge(ctx context.Context, f *evalFlags) (eval.Completer, error) {
	if f.judgeCmd != "" {
		fields := strings.Fields(f.judgeCmd)
		if len(fields) == 0 {
			return nil, errors.New("--judge-cmd is empty")
		}
		slog.InfoContext(ctx, "eval judge", "judge_cmd", f.judgeCmd, "timeout_sec", f.judgeTimeout)
		return cmdCompleter{name: fields[0], args: fields[1:], timeout: time.Duration(f.judgeTimeout) * time.Second}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	profile, err := profileFromFlags(f.local, f.deepseek)
	if err != nil {
		return nil, err
	}
	judgeModel := firstNonEmpty(f.judgeModel, os.Getenv("DISTILL_MODEL"), cfg.EffectiveEvalJudgeProfile(profile))
	if judgeModel == "" {
		return nil, errors.New("eval requires a judge model (--judge-model, $DISTILL_MODEL, config) or --judge-cmd")
	}
	resolved, err := endpointForTextModel(cfg, profile, judgeModel, f.baseURL)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "eval judge", "judge_model", resolved.model, "provider", resolved.provider, "base_url", resolved.baseURL)
	client, err := ai.New(ai.Config{
		Provider:  resolved.provider,
		BaseURL:   resolved.baseURL,
		APIKey:    ai.APIKeyForProvider(resolved.provider),
		TextModel: resolved.model,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ai client: %w", err)
	}
	return client, nil
}

// cmdCompleter judges by running an external command (e.g. "codex exec -") with
// the rendered prompt on stdin and capturing stdout. The JSON object is salvaged
// downstream, so surrounding CLI chatter (headers, token footers) is tolerated.
type cmdCompleter struct {
	name    string
	args    []string
	timeout time.Duration
}

func (c cmdCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.name, c.args...) //nolint:gosec // judge command is operator-supplied via --judge-cmd
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("judge command %q failed: %w; stderr: %s", c.name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
