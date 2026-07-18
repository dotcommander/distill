package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

type digestProgress struct {
	out         io.Writer
	interactive bool
	color       bool

	mu            sync.Mutex
	done          chan struct{}
	stopOnce      sync.Once
	startedAt     time.Time
	source        string
	model         string
	steps         []digestStep
	warnings      []string
	activeDetail  string
	renderedLines int
}

type digestStep struct {
	key    string
	label  string
	state  stepState
	detail string
	done   int
	total  int
}

type stepState int

const (
	stepPending stepState = iota
	stepActive
	stepDone
	stepWarn
	stepSkipped
)

func newDigestProgress(out io.Writer) *digestProgress {
	interactive := isInteractiveTerminal(out)
	return &digestProgress{
		out:         out,
		interactive: interactive,
		color:       interactive && os.Getenv("NO_COLOR") == "",
		done:        make(chan struct{}),
		startedAt:   time.Now(),
		steps: []digestStep{
			{key: "prepare", label: "Prepare input", state: stepActive},
			{key: "chunk", label: "Chunk source"},
			{key: "research", label: "Extract facts"},
			{key: "outline", label: "Outline article"},
			{key: "draft", label: "Draft sections"},
			{key: "edit", label: "Edit sections"},
		},
	}
}

func (p *digestProgress) Start() {
	if !p.interactive {
		return
	}
	// Disable terminal autowrap so a long line stays on one physical row; the
	// redraw moves the cursor up by logical-line count, and a wrapped line would
	// otherwise undershoot and flood the terminal. Restored in Stop.
	_, _ = fmt.Fprint(p.out, "\x1b[?7l")
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-p.done:
				return
			case <-ticker.C:
				p.mu.Lock()
				p.renderLocked(frames[i%len(frames)])
				p.mu.Unlock()
				i++
			}
		}
	}()
}

func (p *digestProgress) Stop() {
	p.stopOnce.Do(func() {
		close(p.done)
		if p.interactive {
			p.mu.Lock()
			p.renderLocked("")
			_, _ = fmt.Fprint(p.out, "\x1b[?7h")
			_, _ = fmt.Fprintln(p.out)
			p.mu.Unlock()
		}
	})
}

func (p *digestProgress) Enabled(context.Context, slog.Level) bool {
	return true
}

func (p *digestProgress) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	if r.Level >= slog.LevelWarn {
		warning := formatDigestWarning(r.Message, attrs)
		p.recordWarningLocked(warning)
		if !p.interactive {
			fmt.Fprintf(p.out, "! %s\n", warning)
		}
	}

	switch r.Message {
	case "digest start":
		p.source = stringAttr(attrs, "file")
		p.model = stringAttr(attrs, "model")
		p.setActiveLocked("prepare", "reading and normalizing")
		p.printNonInteractiveHeaderLocked()
	case "transcript cleaned":
		p.finishLocked("prepare", "transcript cleaned")
	case "stripped embedded binary blobs":
		p.finishLocked("prepare", "binary blobs stripped")
	case "input is unusually dense; chunk count may be inflated (embedded binary, base64, or large tables?)":
		p.recordWarningLocked("input is unusually dense; chunk count may be inflated")
	case "digest chunking done":
		chunks := intAttr(attrs, "chunks")
		p.finishLocked("prepare", "input ready")
		p.finishCountLocked("chunk", chunks, chunks, countLabel(chunks, "chunk"))
		p.setCountLocked("research", stepActive, 0, chunks, "waiting for model responses")
	case "digest research":
		p.setCountLocked("research", stepActive, p.stepByKey("research").done, intAttr(attrs, "total"), "running concurrently")
	case "digest research done":
		s := p.stepByKey("research")
		total := maxPositive(s.total, intAttr(attrs, "total"))
		p.setCountLocked("research", stepActive, s.done+1, total, percentDetail(s.done+1, total, "chunks"))
	case "digest research complete":
		s := p.stepByKey("research")
		failed := intAttr(attrs, "failed")
		if failed > 0 {
			p.finishCountLocked("research", s.done, s.total, fmt.Sprintf("%d failed", failed))
			p.setWarnLocked("research")
		} else {
			p.finishCountLocked("research", s.done, s.total, countLabel(s.total, "chunk"))
		}
	case "digest fuse start":
		p.setActiveLocked("outline", "fusing research notes")
	case "digest fuse done":
		p.setActiveLocked("outline", "research notes fused")
	case "digest outline start":
		p.setActiveLocked("outline", "planning structure")
	case "digest outline done":
		sections := intAttr(attrs, "sections")
		p.finishCountLocked("outline", sections, sections, countLabel(sections, "section"))
		p.setCountLocked("draft", stepActive, 0, sections, "ready")
	case "digest section start":
		total := intAttr(attrs, "total")
		section := stringAttr(attrs, "section")
		s := p.stepByKey("draft")
		p.setCountLocked("draft", stepActive, s.done, total, fmt.Sprintf("%s - %s", percentDetail(s.done, total, "sections"), section))
	case "digest section done":
		s := p.stepByKey("draft")
		total := maxPositive(s.total, intAttr(attrs, "total"))
		done := s.done + 1
		state := stepActive
		if total > 0 && done >= total {
			state = stepDone
		}
		p.setCountLocked("draft", state, done, total, percentDetail(done, total, "sections"))
	case "digest draft done":
		sections := intAttr(attrs, "sections")
		p.finishCountLocked("draft", sections, sections, countLabel(sections, "section"))
		p.setCountLocked("edit", stepActive, 0, sections, "ready")
	case "digest edit start":
		total := intAttr(attrs, "total")
		section := stringAttr(attrs, "section")
		s := p.stepByKey("edit")
		p.setCountLocked("edit", stepActive, s.done, total, fmt.Sprintf("%s - %s", percentDetail(s.done, total, "sections"), section))
	case "digest edit done":
		s := p.stepByKey("edit")
		total := maxPositive(s.total, intAttr(attrs, "total"))
		done := s.done + 1
		state := stepActive
		if total > 0 && done >= total {
			state = stepDone
		}
		p.setCountLocked("edit", state, done, total, percentDetail(done, total, "sections"))
	case "digest done":
		p.finishAllLocked()
		p.activeDetail = fmt.Sprintf("complete in %s", roundDuration(time.Since(p.startedAt)))
	default:
		if r.Level >= slog.LevelWarn {
			return nil
		}
	}

	if !p.interactive {
		p.printMilestoneLocked(r.Message)
	}
	return nil
}

func (p *digestProgress) WithAttrs([]slog.Attr) slog.Handler {
	return p
}

func (p *digestProgress) WithGroup(string) slog.Handler {
	return p
}

func (p *digestProgress) printNonInteractiveHeaderLocked() {
	if p.interactive || p.source == "" {
		return
	}
	fmt.Fprintf(p.out, "distill digest %s\n", p.source)
	if p.model != "" {
		fmt.Fprintf(p.out, "  model: %s\n", p.model)
	}
}

func (p *digestProgress) printMilestoneLocked(msg string) {
	switch msg {
	case "digest chunking done":
		fmt.Fprintf(p.out, "✓ chunked source: %s\n", p.stepByKey("chunk").detail)
	case "digest research complete":
		fmt.Fprintf(p.out, "✓ extracted facts: %s\n", p.stepByKey("research").detail)
	case "digest outline done":
		fmt.Fprintf(p.out, "✓ outlined article: %s\n", p.stepByKey("outline").detail)
	case "digest draft done":
		fmt.Fprintf(p.out, "✓ drafted sections: %s\n", p.stepByKey("draft").detail)
	case "digest edit done":
		s := p.stepByKey("edit")
		if s.total > 0 && s.done >= s.total {
			fmt.Fprintf(p.out, "✓ edited sections: %s\n", s.detail)
		}
	case "digest done":
		fmt.Fprintf(p.out, "✓ digest complete: %s\n", p.activeDetail)
	}
}

func (p *digestProgress) renderLocked(frame string) {
	if !p.interactive {
		return
	}
	if p.renderedLines > 0 {
		fmt.Fprintf(p.out, "\x1b[%dA", p.renderedLines)
	}
	lines := p.viewLocked(frame)
	for _, line := range lines {
		fmt.Fprintf(p.out, "\r\x1b[2K%s\n", line)
	}
	p.renderedLines = len(lines)
}

func (p *digestProgress) viewLocked(frame string) []string {
	elapsed := roundDuration(time.Since(p.startedAt))
	title := p.style("distill digest", "title")
	if p.source != "" {
		title += "  " + p.style(p.source, "accent")
	}
	meta := []string{"elapsed " + elapsed}
	if p.model != "" {
		meta = append([]string{"model " + p.model}, meta...)
	}
	lines := []string{
		title,
		"  " + p.style(strings.Join(meta, "  ·  "), "muted"),
		"",
	}
	for _, step := range p.steps {
		lines = append(lines, p.stepLine(step, frame))
	}
	if p.activeDetail != "" {
		lines = append(lines, "", "  "+p.style(p.activeDetail, "muted"))
	}
	if len(p.warnings) > 0 {
		lines = append(lines, "", p.style("Warnings", "warn"))
		start := 0
		if len(p.warnings) > 2 {
			start = len(p.warnings) - 2
		}
		for _, warning := range p.warnings[start:] {
			lines = append(lines, "  "+p.style("! ", "warn")+warning)
		}
	}
	return lines
}

func (p *digestProgress) stepLine(step digestStep, frame string) string {
	marker := "·"
	switch step.state {
	case stepPending:
		// not started yet
	case stepActive:
		if frame == "" {
			marker = "•"
		} else {
			marker = frame
		}
	case stepDone:
		marker = "✓"
	case stepWarn:
		marker = "!"
	case stepSkipped:
		marker = "-"
	default:
		marker = "?"
	}
	label := step.label
	if step.state == stepActive {
		label = p.style(label, "active")
	}
	detail := step.detail
	if detail == "" && step.total > 0 {
		detail = percentDetail(step.done, step.total, "items")
	}
	if detail != "" {
		detail = p.style(detail, "muted")
	}
	return fmt.Sprintf("  %s %-16s %s", p.style(marker, styleForState(step.state)), label, detail)
}

// setStepLocked sets a step's state + detail and marks it the active detail.
// setActiveLocked/finishLocked are the intent-named wrappers; the count
// variants differ (they also track done/total) and stay separate.
func (p *digestProgress) setStepLocked(key string, state stepState, detail string) {
	p.updateStepLocked(key, func(s *digestStep) {
		s.state = state
		s.detail = detail
	})
	p.activeDetail = detail
}

func (p *digestProgress) setActiveLocked(key, detail string) {
	p.setStepLocked(key, stepActive, detail)
}

func (p *digestProgress) setCountLocked(key string, state stepState, done, total int, detail string) {
	p.updateStepLocked(key, func(s *digestStep) {
		s.state = state
		s.done = maxPositive(done, 0)
		s.total = maxPositive(total, s.total)
		s.detail = detail
	})
	p.activeDetail = detail
}

func (p *digestProgress) finishLocked(key, detail string) {
	p.setStepLocked(key, stepDone, detail)
}

func (p *digestProgress) finishCountLocked(key string, done, total int, detail string) {
	p.updateStepLocked(key, func(s *digestStep) {
		s.state = stepDone
		s.done = done
		s.total = maxPositive(total, s.total)
		s.detail = detail
	})
	p.activeDetail = detail
}

func (p *digestProgress) setWarnLocked(key string) {
	p.updateStepLocked(key, func(s *digestStep) {
		s.state = stepWarn
	})
}

func (p *digestProgress) finishAllLocked() {
	for i := range p.steps {
		switch p.steps[i].state {
		case stepActive:
			p.steps[i].state = stepDone
		case stepPending:
			p.steps[i].state = stepSkipped
			p.steps[i].detail = "skipped"
		case stepDone, stepWarn, stepSkipped:
			// already terminal
		}
	}
}

func (p *digestProgress) updateStepLocked(key string, fn func(*digestStep)) {
	for i := range p.steps {
		if p.steps[i].key == key {
			fn(&p.steps[i])
			return
		}
	}
}

func (p *digestProgress) stepByKey(key string) digestStep {
	for _, step := range p.steps {
		if step.key == key {
			return step
		}
	}
	return digestStep{}
}

func (p *digestProgress) recordWarningLocked(warning string) {
	if warning == "" {
		return
	}
	p.warnings = append(p.warnings, warning)
	p.activeDetail = warning
}

func (p *digestProgress) style(s, kind string) string {
	if !p.color {
		return s
	}
	code := ""
	switch kind {
	case "title":
		code = "1;36"
	case "accent", "active":
		code = "36"
	case "done":
		code = "32"
	case "warn":
		code = "33"
	case "muted", "pending":
		code = "2"
	}
	if code == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func styleForState(state stepState) string {
	switch state {
	case stepPending:
		return "pending"
	case stepActive:
		return "active"
	case stepDone:
		return "done"
	case stepWarn:
		return "warn"
	case stepSkipped:
		return "pending"
	}
	return ""
}

func isInteractiveTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func formatDigestWarning(msg string, attrs map[string]any) string {
	suffix := ""
	if stage := stringAttr(attrs, "stage"); stage != "" {
		suffix = fmt.Sprintf(" (%s)", stage)
	} else if section := stringAttr(attrs, "section"); section != "" {
		suffix = fmt.Sprintf(" (%s)", section)
	}
	if errText := fmt.Sprint(attrs["err"]); errText != "" && errText != "<nil>" {
		return fmt.Sprintf("%s%s: %s", msg, suffix, errText)
	}
	return msg + suffix
}

func intAttr(attrs map[string]any, key string) int {
	switch v := attrs[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v) //nolint:gosec // line count from trusted input; cannot plausibly exceed MaxInt32
	default:
		return 0
	}
}

func stringAttr(attrs map[string]any, key string) string {
	if v, ok := attrs[key].(string); ok {
		return v
	}
	return ""
}

func maxPositive(a, b int) int {
	if b > a {
		return b
	}
	return a
}

func roundDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(10 * time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

func countLabel(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

func percentDetail(done, total int, unit string) string {
	if total <= 0 {
		return fmt.Sprintf("%d %s", done, unit)
	}
	pct := int(float64(done) / float64(total) * 100)
	return fmt.Sprintf("%d/%d %s  %d%%", done, total, unit, pct)
}
