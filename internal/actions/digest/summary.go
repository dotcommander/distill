package digest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/fsutil"
)

// StageError identifies the mandatory stage/unit that stopped a digest. It is
// intentionally small enough for callers to surface partial checkpoints
// without leaking provider response bodies.
type StageError struct {
	Stage string
	Unit  string
	Err   error
}

func (e *StageError) Error() string {
	if e.Unit == "" {
		return fmt.Sprintf("digest: %s: %v", e.Stage, e.Err)
	}
	if e.Stage == stageEdit {
		return fmt.Sprintf("digest: edit section %q: %v", e.Unit, e.Err)
	}
	if e.Stage == stageSection {
		return fmt.Sprintf("digest: section %q: %v", e.Unit, e.Err)
	}
	return fmt.Sprintf("digest: %s %s: %v", e.Stage, e.Unit, e.Err)
}
func (e *StageError) Unwrap() error { return e.Err }

type runSummary struct {
	Version       int            `json:"version"`
	Status        string         `json:"status"`
	FailingStage  string         `json:"failing_stage,omitempty"`
	FailingUnit   string         `json:"failing_unit,omitempty"`
	ErrorClass    string         `json:"error_class,omitempty"`
	CallPlan      *CallPlan      `json:"call_plan,omitempty"`
	CallsUsed     int            `json:"calls_used"`
	CallsLimit    int            `json:"calls_limit"`
	Reuse         CallReuse      `json:"reuse"`
	ProviderUsage map[string]int `json:"provider_usage,omitempty"`
	PromptTokens  int64          `json:"prompt_tokens,omitempty"`
	CachedTokens  int64          `json:"cached_tokens,omitempty"`
	OutputTokens  int64          `json:"output_tokens,omitempty"`
	ChunkCount    int            `json:"chunk_count"`
	Elapsed       string         `json:"elapsed"`
	OutputSHA256  string         `json:"output_sha256,omitempty"`
	ResumeHint    string         `json:"resume_hint,omitempty"`
	CompletedAt   time.Time      `json:"completed_at"`
}

func finishRun(opts Options, started time.Time, result *Result, runErr error) (*Result, error) {
	runErr = cleanupFailedFinalPublication(opts, result, runErr)
	if !artifactDirectoryExists(opts.ArtifactDir) {
		return result, runErr
	}
	return writeRunSummary(opts, started, result, runErr)
}

func cleanupFailedFinalPublication(opts Options, result *Result, runErr error) error {
	if runErr == nil {
		return nil
	}
	if opts.OutPath == "" && result != nil {
		opts.OutPath = result.OutPath
	}
	if cleanupErr := clearFinalPublication(opts); cleanupErr != nil {
		return errors.Join(runErr, cleanupErr)
	}
	return runErr
}

func artifactDirectoryExists(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	return true
}

func writeRunSummary(opts Options, started time.Time, result *Result, runErr error) (*Result, error) {
	summary := runSummary{Version: 1, Status: runStatusSuccess, CallPlan: opts.CallPlan, Elapsed: time.Since(started).Round(time.Millisecond).String(), CompletedAt: time.Now().UTC()}
	if opts.Dispatcher != nil && opts.Dispatcher.Budget != nil {
		summary.CallsUsed = opts.Dispatcher.Budget.Used()
		summary.CallsLimit = opts.Dispatcher.Budget.Limit()
		summary.ProviderUsage = opts.Dispatcher.ProviderUsage()
	}
	if opts.Usage != nil {
		summary.PromptTokens, summary.CachedTokens, summary.OutputTokens = opts.Usage()
	}
	if result != nil {
		summary.ChunkCount = result.ChunkCount
		summary.Reuse = CallReuse{Research: result.ReusedChunks, Outline: boolCount(result.ReusedOutline), Sections: result.ReusedSections, Edits: result.ReusedEdits}
		summary.OutputSHA256 = summaryOutputHash(result.OutPath, runErr)
	}
	if runErr != nil {
		summary.Status = runStatusFailed
		summary.ErrorClass = ai.ErrorClass(runErr)
		summary.FailingStage, summary.FailingUnit = summaryStage(runErr)
		summary.ResumeHint = "rerun with --resume using this artifact directory; completed response checkpoints remain available"
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		if runErr != nil {
			return result, errors.Join(runErr, fmt.Errorf("digest: encode run summary: %w", err))
		}
		return result, fmt.Errorf("digest: encode run summary: %w", err)
	}
	if err := fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "run-summary.json"), data, 0o644); err != nil {
		if runErr != nil {
			return result, errors.Join(runErr, fmt.Errorf("digest: write run summary: %w", err))
		}
		return result, fmt.Errorf("digest: write run summary: %w", err)
	}
	return result, runErr
}

const (
	runStatusSuccess = "success"
	runStatusFailed  = "failed"
)

func summaryOutputHash(path string, runErr error) string {
	if runErr != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return digestHash(string(data))
}

func summaryStage(err error) (stageName, unit string) {
	var stage *StageError
	if errors.As(err, &stage) {
		return stage.Stage, stage.Unit
	}
	text := strings.TrimPrefix(err.Error(), "digest: ")
	stageName, _, _ = strings.Cut(text, ":")
	return stageName, ""
}
