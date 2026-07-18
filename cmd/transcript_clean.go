package cmd

import (
	"context"
	"log/slog"
	"strings"

	"github.com/dotcommander/distill/internal/mdclean"
	"github.com/dotcommander/distill/internal/transcript"
)

// maybeCleanTranscript applies transcript cleaning to data per the command's
// flags and returns the (possibly cleaned) text plus whether cleaning ran.
//
//	disable=true  -> never clean (return raw); precedence over force.
//	force=true    -> clean even if Detect is unsure (Clean is a no-op on prose).
//	default       -> clean only when Detect recognizes a transcript.
//
// It emits a single slog line with before/after char counts when it cleans,
// consistent with the "digest start"/"chunk done" logging convention.
func maybeCleanTranscript(ctx context.Context, data string, force, disable bool) string {
	if disable {
		return data
	}
	if !force && !transcript.IsTranscript(data) {
		return data
	}
	cleaned, err := transcript.Clean(data)
	if err != nil {
		slog.WarnContext(ctx, "transcript clean failed; using raw input", "error", err)
		return data
	}
	if cleaned == data {
		return data
	}
	slog.InfoContext(ctx, "transcript cleaned",
		"chars_before", len(data),
		"chars_after", len(cleaned),
	)
	return cleaned
}

// maybeStripBinary removes embedded base64 / data-URI blobs (e.g. images from a
// PDF-to-markdown conversion) from data before chunking. Such blobs are never
// digestible content and otherwise explode the chunk count. Always on; emits a
// single slog line when it strips anything.
func maybeStripBinary(ctx context.Context, data string) string {
	cleaned, removed := mdclean.StripBinaryBlobs(data)
	if removed > 0 {
		slog.InfoContext(ctx, "stripped embedded binary blobs",
			"bytes_removed", removed,
			"bytes_before", len(data),
			"bytes_after", len(cleaned),
		)
	}
	// Pre-flight density guard: normal prose runs ~5-7 bytes/word. A much higher
	// ratio after stripping means leftover binary, wrapped base64, or large tables
	// that will inflate the chunk count — warn rather than silently over-chunk.
	if w := len(strings.Fields(cleaned)); w > 0 && len(cleaned)/w > maxBytesPerWord {
		slog.WarnContext(ctx, "input is unusually dense; chunk count may be inflated (embedded binary, base64, or large tables?)",
			"bytes_per_word", len(cleaned)/w,
			"bytes", len(cleaned),
			"words", w,
		)
	}
	return cleaned
}

// maxBytesPerWord bounds normal prose density (~5-7 bytes/word); above it the
// input likely carries non-prose bulk that inflates chunking.
const maxBytesPerWord = 40
