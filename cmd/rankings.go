package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/fsutil"
	"github.com/dotcommander/distill/internal/rankings"

	"gopkg.in/yaml.v3"
)

func runRankingsFetch(cmd *runContext, render bool, timeout int, onlyBoard string) error {
	defuddleBin, err := exec.LookPath("defuddle")
	if err != nil {
		fallback := filepath.Join(os.Getenv("HOME"), "go/bin/defuddle")
		if _, statErr := os.Stat(fallback); statErr != nil { //nolint:gosec // path from trusted CLI flags; eval harness launches model commands by design
			return errors.New("defuddle CLI not found on PATH or ~/go/bin — install it to use 'rankings fetch'")
		}
		defuddleBin = fallback
	}

	r, err := rankings.Load()
	if err != nil {
		return err
	}
	path, err := rankings.Path()
	if err != nil {
		return err
	}
	dir := filepath.Join(filepath.Dir(path), "rankings.fetched")
	if mkerr := os.MkdirAll(dir, 0o750); mkerr != nil {
		return fmt.Errorf("create fetched rankings dir: %w", mkerr)
	}

	keys := make([]string, 0, len(r.Boards))
	if onlyBoard != "" {
		if _, ok := r.Boards[onlyBoard]; !ok {
			return fmt.Errorf("unknown rankings board %q", onlyBoard)
		}
		keys = append(keys, onlyBoard)
	} else {
		for key := range r.Boards {
			keys = append(keys, key)
		}
		slices.Sort(keys)
	}

	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	for _, key := range keys {
		board := r.Boards[key]
		if board.SourceURL == "" {
			fmt.Fprintf(errOut, "fetch %s: skipped empty source_url\n", key)
			continue
		}

		args := []string{"parse", board.SourceURL, "--markdown"}
		if board.Render && render {
			args = append(args, "--render")
		}
		bctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		fetchCmd := exec.CommandContext(bctx, defuddleBin, args...) //nolint:gosec // args from trusted CLI flags; eval harness launches model commands by design
		pipe, cerr := fetchCmd.StdoutPipe()
		if cerr != nil {
			cancel()
			fmt.Fprintf(errOut, "fetch %s: %v\n", key, cerr)
			continue
		}
		if cerr := fetchCmd.Start(); cerr != nil {
			cancel()
			fmt.Fprintf(errOut, "fetch %s: %v\n", key, cerr)
			continue
		}
		var buf bytes.Buffer
		_, copyErr := io.Copy(&buf, io.LimitReader(pipe, 10<<20))
		waitErr := fetchCmd.Wait()
		cancel()
		if copyErr != nil {
			fmt.Fprintf(errOut, "fetch %s: %v\n", key, copyErr)
			continue
		}
		if waitErr != nil {
			fmt.Fprintf(errOut, "fetch %s: %v\n", key, waitErr)
			continue
		}

		if werr := fsutil.WriteFile(filepath.Join(dir, key+".md"), buf.Bytes(), 0o644); werr != nil {
			fmt.Fprintf(errOut, "fetch %s: %v\n", key, werr)
			continue
		}
		rows := rankings.ParseMarkdownTableRows(buf.String())
		scores, matched, unmatched := rankings.MatchScores(board, rows, r.Roster)
		if len(scores) > 0 {
			board.Scores = scores
			r.Boards[key] = board
		} else {
			fmt.Fprintf(errOut, "fetch %s: 0 roster models matched (kept seed); unmatched sample: %v\n", key, sampleStrings(unmatched, 5))
		}

		pairs := make([]string, 0, len(scores))
		for slug, score := range scores {
			pairs = append(pairs, fmt.Sprintf("%s=%g", slug, score))
		}
		slices.Sort(pairs)
		fmt.Fprintf(out, "%s: matched %d/%d (unmatched rows %d)", key, len(matched), len(r.Roster), len(unmatched))
		for _, pair := range pairs {
			fmt.Fprintf(out, " %s", pair)
		}
		_, _ = fmt.Fprintln(out)
	}

	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal rankings: %w", err)
	}
	if err := fsutil.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write rankings %s: %w", path, err)
	}
	fmt.Fprintf(out, "wrote %s\n", path)
	_, _ = fmt.Fprintln(out, "run 'distill models rankings apply' to update config")
	return nil
}

func sampleStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func runRankingsApply(cmd *runContext, dryRun bool) error {
	r, err := rankings.Load()
	if err != nil {
		return err
	}
	picks, err := rankings.Derive(r)
	if err != nil {
		return err
	}

	path, err := config.Path()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config file %s does not exist; run any distill command once to materialize config.yaml", path)
		}
		return fmt.Errorf("read config %s: %w", path, err)
	}

	original := string(data)
	newText, changes := rankings.ApplyToConfig(original, picks)
	out := cmd.OutOrStdout()
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CONFIG_KEY\tOLD\tNEW\tSOURCE")
	updated := 0
	for _, change := range changes {
		newValue := change.New
		if change.Skipped {
			newValue = "(absent — kept)"
		} else {
			updated++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", change.ConfigKey, change.Old, newValue, change.Note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if dryRun {
		_, _ = fmt.Fprintln(out, "dry run — config.yaml not modified")
		return nil
	}
	if newText == original {
		_, _ = fmt.Fprintln(out, "no changes")
		return nil
	}
	if err := fsutil.WriteFile(path, []byte(newText), 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	fmt.Fprintf(out, "wrote %s (%d keys updated)\n", path, updated)
	return nil
}

func runRankingsShow(cmd *runContext, local bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	model, baseURL := cfg.Effective(local)
	embeddingModel, embeddingEndpoint := cfg.EffectiveEmbedding(local)
	out := cmd.OutOrStdout()
	profile := "remote"
	if local {
		profile = "local"
	}
	fmt.Fprintf(out, "profile: %s\n", profile)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ROLE\tMODEL")
	fmt.Fprintf(tw, "model (shared)\t%s\n", model)
	fmt.Fprintf(tw, "research\t%s\n", cfg.EffectiveRole("research", local))
	fmt.Fprintf(tw, "fuse\t%s\n", cfg.EffectiveRole("fuse", local))
	fmt.Fprintf(tw, "outline\t%s\n", cfg.EffectiveRole("outline", local))
	fmt.Fprintf(tw, "write\t%s\n", cfg.EffectiveRole("write", local))
	fmt.Fprintf(tw, "edit\t%s\n", cfg.EffectiveRole("edit", local))
	fmt.Fprintf(tw, "eval-judge\t%s\n", cfg.EffectiveEvalJudge(local))
	fmt.Fprintf(tw, "merit-judge\t%s\n", cfg.EffectiveMeritJudge(local))
	fmt.Fprintf(tw, "embedding\t%s\n", embeddingModel)
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "base_url: %s\nembedding_endpoint: %s\n", baseURL, embeddingEndpoint)
	return nil
}
