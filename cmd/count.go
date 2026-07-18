package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/tokenizer"
)

// countMaxInputBytes bounds the input the `count` command will load into
// memory before tokenization. 100 MiB is far larger than any realistic
// document a human pastes or pipes to the CLI, but small enough to fail
// predictably instead of OOM'ing when stdin is wedged open by a runaway
// producer or a user accidentally pipes a multi-GB binary. ReadAll on
// unbounded input is the workspace anti-pattern this cap addresses.
const countMaxInputBytes = 100 * 1024 * 1024

// errCountInputTooLarge is returned when the input exceeds
// countMaxInputBytes. Surfaced as a typed sentinel so tests can match on it
// without scraping the error message.
var errCountInputTooLarge = errors.New("input exceeds maximum size")

type countResult struct {
	Tokens int `json:"tokens"`
	Chars  int `json:"chars"`
	Lines  int `json:"lines"`
}

func runCount(cmd *runContext, args []string, format string) error {
	if format != "json" && format != "plain" {
		return fmt.Errorf("unknown format: %s", format)
	}
	start := time.Now()
	var src io.Reader
	var file *os.File
	srcName := "stdin"
	if len(args) == 0 || args[0] == "-" {
		src = cmd.in
	} else {
		var err error
		file, err = openFileInput(args[0])
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		src = file
		srcName = args[0]
	}
	slog.InfoContext(cmd.Context(), "count start", "file", srcName)

	data, err := readCappedInput(src, countMaxInputBytes)
	var closeErr error
	if file != nil {
		closeErr = file.Close()
	}
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing input: %w", closeErr)
	}

	text := string(data)

	tok, err := tokenizer.New()
	if err != nil {
		return fmt.Errorf("creating tokenizer: %w", err)
	}

	tokenCount, err := tok.Count(text)
	if err != nil {
		return fmt.Errorf("counting tokens: %w", err)
	}
	result := countResult{
		Tokens: tokenCount,
		Chars:  len(text),
		Lines:  tokenizer.CountLines(text),
	}

	switch format {
	case "json":
		out, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
	case "plain":
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tokens: %d\nchars: %d\nlines: %d\n", result.Tokens, result.Chars, result.Lines)
	}

	slog.InfoContext(cmd.Context(), "count done",
		"file", srcName,
		"bytes", len(data),
		"tokens", result.Tokens,
		"lines", result.Lines,
		"duration", time.Since(start),
	)
	return nil
}

// readCappedInput reads up to maxBytes from r. If the input is strictly
// larger than maxBytes it returns errCountInputTooLarge so callers can fail
// predictably instead of streaming an unbounded payload into memory. The
// +1-byte trick lets us distinguish "exactly at cap" from "over".
func readCappedInput(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes", errCountInputTooLarge, maxBytes)
	}
	return data, nil
}

// openFileInput opens path for reading, rejecting directories with a clear
// error instead of letting a 0-byte read masquerade as empty input.
func openFileInput(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%s is a directory, expected a file", path)
	}
	return f, nil
}

// normalizeInput strips a leading UTF-8 BOM and converts CRLF/CR line endings to
// LF so heading detection, transcript signatures (WEBVTT on line 1), and chunk
// boundaries are platform-independent. NOT applied by count, which reports the
// raw byte/char/line totals of the file as-is.
func normalizeInput(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}
