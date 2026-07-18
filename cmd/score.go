package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/fsutil"
)

// scoreFlags holds the resolved --score flag values for one invocation,
// closure-scoped in newScoreCmd (no package globals).
type scoreFlags struct {
	expected   string
	candidates string
	out        string
}

func runScore(cmd *runContext, f *scoreFlags) error {
	dirs := splitCandidates(f.candidates)
	if len(dirs) == 0 {
		return errors.New("score requires --candidates")
	}

	var cands []extractscore.Candidate
	for _, dir := range dirs {
		run, err := extractscore.ScoreRun(f.expected, dir)
		if err != nil {
			return fmt.Errorf("scoring %s: %w", dir, err)
		}
		cands = append(cands, extractscore.Candidate{Name: scoreCandidateName(dir), Run: run})
	}

	index := extractscore.RenderINDEX(cands)
	_, _ = fmt.Fprint(cmd.OutOrStdout(), index)

	if f.out != "" {
		if err := os.MkdirAll(f.out, 0o750); err != nil {
			return err
		}
		if err := fsutil.WriteFile(filepath.Join(f.out, "INDEX.md"), []byte(index), 0o644); err != nil {
			return err
		}
		for _, c := range cands {
			path := filepath.Join(f.out, c.Name+".summary.md")
			if err := fsutil.WriteFile(path, []byte(extractscore.RenderSummary(c)), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// scoreCandidateName derives a label from a responses dir: when the dir's base
// is literally "responses", use the parent dir's base; otherwise use the base.
func scoreCandidateName(dir string) string {
	base := filepath.Base(strings.TrimRight(dir, string(filepath.Separator)))
	if base == "responses" {
		return filepath.Base(filepath.Dir(dir))
	}
	return base
}
