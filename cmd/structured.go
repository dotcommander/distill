package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/fsutil"
)

type structuredFlags struct {
	schema     string
	gold       string
	candidates string
	out        string
}

func runStructured(cmd *runContext, f *structuredFlags) error {
	if f.schema == "" || f.gold == "" || f.candidates == "" {
		return errors.New("structured requires --schema, --gold, and --candidates")
	}
	paths := splitCandidates(f.candidates)
	if len(paths) == 0 {
		return errors.New("structured: --candidates lists no files")
	}

	var results []extractscore.StructuredResult
	for _, p := range paths {
		res, err := extractscore.ScoreStructuredFiles(structuredCandidateName(p), f.schema, f.gold, p)
		if err != nil {
			return fmt.Errorf("scoring %s: %w", p, err)
		}
		results = append(results, res)
	}

	index := extractscore.RenderStructuredINDEX(results)
	_, _ = fmt.Fprint(cmd.OutOrStdout(), index)

	if f.out != "" {
		if err := os.MkdirAll(f.out, 0o750); err != nil {
			return err
		}
		if err := fsutil.WriteFile(filepath.Join(f.out, "INDEX.md"), []byte(index), 0o644); err != nil {
			return err
		}
		for _, r := range results {
			summaryPath := filepath.Join(f.out, r.Name+".summary.md")
			if err := fsutil.WriteFile(summaryPath, []byte(extractscore.RenderStructuredSummary(r)), 0o644); err != nil {
				return err
			}
			data, err := json.MarshalIndent(r, "", "  ")
			if err != nil {
				return err
			}
			data = append(data, '\n')
			if err := fsutil.WriteFile(filepath.Join(f.out, r.Name+".report.json"), data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func structuredCandidateName(p string) string {
	base := filepath.Base(strings.TrimRight(p, string(filepath.Separator)))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
