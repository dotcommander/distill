package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/dotcommander/distill/internal/manifest"
	"github.com/dotcommander/distill/internal/tokenizer"

	"github.com/dotcommander/reliquary/chunking"
)

// chunkFlags holds the resolved --chunk flag values for one invocation. It is
// closure-scoped in newChunkCmd (no package globals) and threaded through the
// chunk pipeline explicitly.
type chunkFlags struct {
	mode            string
	maxTokens       int
	overlap         int
	outDir          string
	format          string
	threshold       float64
	provider        string
	embeddingModel  string
	smoothingWindow int
	coherenceWindow int
	minChunkChars   int
	maxChunkChars   int
	noClean         bool
	forceClean      bool
	local           bool
}

func runChunk(cmd *runContext, args []string, f *chunkFlags) error {
	filePath := args[0]
	sourceName := filePath
	start := time.Now()
	slog.InfoContext(cmd.Context(), "chunk start",
		"file", filePath,
		"mode", f.mode,
		"max_tokens", f.maxTokens,
	)

	// Reuse the count cap to keep both commands' input budgets coupled at a
	// single source of truth (~100 MiB). chunk has the same OOM risk on a
	// runaway file and the same predictability requirement on oversize input.
	var data []byte
	var err error
	if filePath == "-" {
		sourceName = "stdin"
		data, err = readCappedInput(cmd.in, countMaxInputBytes)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	} else {
		file, ferr := openFileInput(filePath)
		if ferr != nil {
			return fmt.Errorf("reading file: %w", ferr)
		}
		data, err = readCappedInput(file, countMaxInputBytes)
		cerr := file.Close()
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		if cerr != nil {
			return fmt.Errorf("closing file: %w", cerr)
		}
	}
	text := maybeCleanTranscript(cmd.Context(), maybeStripBinary(cmd.Context(), normalizeInput(string(data))), f.forceClean, f.noClean)

	var tc chunking.TokenCounter
	if !f.local {
		tc, err = tokenizer.NewTokenCounter(f.maxTokens)
		if err != nil {
			return fmt.Errorf("creating token counter: %w", err)
		}
	}

	// Base boundary chunkers take CHARACTER budgets; distill's flags are in
	// tokens, approximated at 4 chars/token. Remote-profile runs additionally
	// apply a cl100k_base preflight budget. Local runs cannot honestly use that
	// vocabulary for Qwen, so they retain only the character budget.
	charSize := f.maxTokens * 4
	charOverlap := f.overlap * 4

	chunks, err := chunkText(cmd.Context(), f, text, charSize, charOverlap, tc)
	if err != nil {
		return fmt.Errorf("chunking: %w", err)
	}
	if len(chunks) == 0 {
		return errors.New("chunking produced no chunks (input is empty or whitespace-only after cleaning)")
	}

	output, err := prepareChunkOutput(f.outDir)
	if err != nil {
		return err
	}
	defer output.abort()

	m, err := writeChunks(output.workDir, sourceName, text, chunks, tc, f.mode)
	if err != nil {
		return err
	}

	if err := manifest.WriteManifest(m, output.workDir); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := output.publish(); err != nil {
		return err
	}

	if f.format == "json" {
		jsonData, err := manifest.ToJSON(m)
		if err != nil {
			return fmt.Errorf("marshaling manifest: %w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(jsonData))
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Chunks written to: %s\n", output.targetDir)
	slog.InfoContext(cmd.Context(), "chunk done",
		"file", filePath,
		"mode", f.mode,
		"max_tokens", f.maxTokens,
		"chunk_count", len(chunks),
		"duration", time.Since(start),
	)
	return nil
}

// modeStrategy maps a distill --mode to a base chunking.Strategy. semantic is
// handled separately (it is not a plain Strategy).
var modeStrategy = map[string]chunking.Strategy{
	"headings": chunking.HeadingAware,
	"cramit":   chunking.SentenceBoundary,
}

// chunkText dispatches to the chunking package. headings/cramit run a base
// boundary strategy then apply the optional cl100k_base preflight budget;
// semantic builds an embedder and applies the same optional estimate.
func chunkText(ctx context.Context, f *chunkFlags, text string, size, overlap int, tc chunking.TokenCounter) ([]chunking.Chunk, error) {
	if f.mode == "semantic" {
		return chunkSemantic(ctx, f, text, size, overlap, tc)
	}
	strat, ok := modeStrategy[f.mode]
	if !ok {
		return nil, fmt.Errorf("unknown mode %q; allowed: headings, semantic, cramit", f.mode)
	}
	c, err := chunking.NewChunker(strat)
	if err != nil {
		return nil, fmt.Errorf("creating chunker: %w", err)
	}
	if tc == nil {
		return c.Chunk(text, size, overlap), nil
	}
	return chunking.ChunkWithTokenLimit(c, text, size, overlap, tc), nil
}

type chunkOutput struct {
	targetDir    string
	workDir      string
	explicit     bool
	published    bool
	beforeRename func()
}

// prepareChunkOutput creates a private generation directory. Explicit targets
// are published only after every chunk and the manifest have been written.
func prepareChunkOutput(dir string) (*chunkOutput, error) {
	if dir == "" {
		d, err := os.MkdirTemp("", "distill-chunks-*")
		if err != nil {
			return nil, fmt.Errorf("creating temp output dir: %w", err)
		}
		return &chunkOutput{targetDir: d, workDir: d}, nil
	}

	dir = filepath.Clean(dir)
	if err := requireChunkOutputTargetAbsent(dir); err != nil {
		return nil, err
	}
	parent := filepath.Dir(dir)
	if mkdirErr := os.MkdirAll(parent, 0o750); mkdirErr != nil {
		return nil, fmt.Errorf("creating output parent directory: %w", mkdirErr)
	}
	workDir, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".distill-staging-*")
	if err != nil {
		return nil, fmt.Errorf("creating staging directory: %w", err)
	}
	if err := os.Chmod(workDir, 0o750); err != nil { // #nosec G302 -- directory mode matches normal explicit output directories
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("setting staging directory permissions: %w", err)
	}
	return &chunkOutput{
		targetDir: dir,
		workDir:   workDir,
		explicit:  true,
	}, nil
}

func requireChunkOutputTargetAbsent(dir string) error {
	_, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspecting output path: %w", err)
	}
	return fmt.Errorf("output path %q already exists; choose a new --out-dir", dir)
}

func (o *chunkOutput) publish() error {
	if !o.explicit {
		o.published = true
		return nil
	}
	if err := requireChunkOutputTargetAbsent(o.targetDir); err != nil {
		return err
	}
	if o.beforeRename != nil {
		o.beforeRename()
	}
	if err := renameNoReplace(o.workDir, o.targetDir); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output path %q already exists; choose a new --out-dir", o.targetDir)
		}
		return fmt.Errorf("publishing chunk generation: %w", err)
	}
	o.published = true
	return nil
}

func (o *chunkOutput) abort() {
	if o == nil || o.published {
		return
	}
	_ = os.RemoveAll(o.workDir)
}

// writeChunks writes each chunk as a numbered .md file under outDir and
// returns the populated manifest.
func writeChunks(outDir, filePath, source string, chunks []chunking.Chunk, tc chunking.TokenCounter, mode string) (*manifest.Manifest, error) {
	m := &manifest.Manifest{
		Source: filepath.Base(filePath),
		Mode:   mode,
	}
	if tc != nil {
		m.Tokenizer = "cl100k_base"
		m.TokenCountsAvailable = true
	}
	totalTokens := 0
	cursor := 0
	for i, chunk := range chunks {
		fileName := fmt.Sprintf("%03d.md", i+1)
		chunkPath := filepath.Join(outDir, fileName)
		// Confine the write to outDir. Today the filename is a pure integer
		// derivation so escape is impossible by construction; this assertion
		// is a tripwire so any future change that funnels user-derived
		// content (chunk titles, source headings) into the filename surfaces
		// as a hard failure instead of a silent path-traversal write.
		if err := assertUnderDir(outDir, chunkPath); err != nil {
			return nil, fmt.Errorf("chunk path escape: %w", err)
		}
		if err := os.WriteFile(chunkPath, []byte(chunk.Text), 0o600); err != nil {
			return nil, fmt.Errorf("writing chunk file: %w", err)
		}
		// Token count from Distill's tokenizer (NOT chunk.TokenCount, which a
		// base chunker may leave 0, and NOT FillTokenCounts, which is tiktoken).
		var tokens *int
		if tc != nil {
			count := tc.CountTokens(chunk.Text)
			tokens = &count
		}
		// Map byte-offset span -> line range against the original source. Spans
		// can be cleared (0/0) after a token-limit split; ResolveChunkSpan
		// relocates by forward search from cursor, else we fall back to 0/0.
		startLine, endLine := 0, 0
		if span, ok := chunking.ResolveChunkSpan(source, chunk, cursor); ok {
			startLine, endLine = chunking.LineRangeForSpan(source, span)
			cursor = chunking.NextChunkCursor(span)
		}
		m.Chunks = append(m.Chunks, manifest.ChunkInfo{
			File:      fileName,
			Tokens:    tokens,
			StartLine: startLine,
			EndLine:   endLine,
		})
		if tokens != nil {
			totalTokens += *tokens
		}
	}
	if m.TokenCountsAvailable {
		m.TotalTokens = &totalTokens
	}
	m.TotalChunks = len(chunks)
	return m, nil
}
