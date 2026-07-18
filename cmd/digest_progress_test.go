package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestDigestProgressNonInteractiveOutput(t *testing.T) {
	var buf bytes.Buffer
	progress := newDigestProgress(&buf)
	logger := slog.New(progress)
	ctx := context.Background()

	logger.InfoContext(ctx, "digest start", "file", "raw.md", "model", "z-ai/glm-5.2")
	logger.InfoContext(ctx, "digest chunking done", "chunks", 2)
	logger.InfoContext(ctx, "digest research done", "total", 2)
	logger.InfoContext(ctx, "digest research done", "total", 2)
	logger.InfoContext(ctx, "digest research complete", "failed", 0)
	logger.InfoContext(ctx, "digest outline start")
	logger.WarnContext(ctx, "digest: retrying LLM call", "stage", "outline", "attempt", 1, "err", "empty response")
	logger.InfoContext(ctx, "digest outline done", "sections", 1)
	logger.InfoContext(ctx, "digest draft done", "sections", 1)
	logger.InfoContext(ctx, "digest done")

	out := buf.String()
	for _, want := range []string{
		"distill digest raw.md",
		"  model: z-ai/glm-5.2",
		"✓ chunked source: 2 chunks",
		"✓ extracted facts: 2 chunks",
		"! digest: retrying LLM call (outline): empty response",
		"✓ outlined article: 1 section",
		"✓ drafted sections: 1 section",
		"✓ digest complete",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in progress output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "level=") || strings.Contains(out, "time=") {
		t.Fatalf("progress output should not contain raw slog fields:\n%s", out)
	}
}

func TestDigestProgressDashboardView(t *testing.T) {
	var buf bytes.Buffer
	progress := newDigestProgress(&buf)
	progress.interactive = true
	progress.color = false
	logger := slog.New(progress)
	ctx := context.Background()

	logger.InfoContext(ctx, "digest start", "file", "raw.md", "model", "z-ai/glm-5.2")
	logger.InfoContext(ctx, "digest chunking done", "chunks", 2)
	logger.InfoContext(ctx, "digest research done", "total", 2)
	logger.WarnContext(ctx, "digest: retrying LLM call", "stage", "outline", "attempt", 1, "err", "empty response")

	progress.mu.Lock()
	view := strings.Join(progress.viewLocked("⠋"), "\n")
	progress.mu.Unlock()

	for _, want := range []string{
		"distill digest  raw.md",
		"model z-ai/glm-5.2",
		"✓ Chunk source     2 chunks",
		"⠋ Extract facts    1/2 chunks  50%",
		"· Outline article",
		"Warnings",
		"! digest: retrying LLM call (outline): empty response",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in dashboard view:\n%s", want, view)
		}
	}
}
