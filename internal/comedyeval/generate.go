package comedyeval

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Completer is the generation backend (satisfied by *ai.Client).
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// WriterPrompt builds the instruction for a model to write ONE comedy bit for a
// topic in the fixture's shared style. Kept here (assembly logic) while the style
// and briefs are config data from the fixture.
func WriterPrompt(style, brief string) string {
	var b strings.Builder
	b.WriteString("You are a stand-up comedian. Write ONE short comedy bit (1-4 lines, no title, no preamble) in this style:\n")
	b.WriteString(style)
	b.WriteString("\n\nTopic: ")
	b.WriteString(brief)
	b.WriteString("\n\nOutput ONLY the bit — no setup labels, no explanation, no quotation marks around it.")
	return b.String()
}

// GenerateBits has one model write a bit for every topic CONCURRENTLY, each with
// its own perTopic timeout so one slow topic can't starve the others (a plain
// WaitGroup is used instead of errgroup precisely so a single topic failure does
// NOT cancel its siblings). Results are indexed by position to preserve fixture
// order. It tolerates up to one failed topic; it returns an error only if more
// than one topic produced no bit (a model that can barely write is not a fair
// candidate). Mirrors the digest pipeline's record-per-item-failure philosophy.
func GenerateBits(ctx context.Context, c Completer, ts TopicSet, perTopic time.Duration) (string, error) {
	bits := make([]string, len(ts.Topics))
	var wg sync.WaitGroup
	for i, t := range ts.Topics {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tctx := ctx
			if perTopic > 0 {
				var cancel context.CancelFunc
				tctx, cancel = context.WithTimeout(ctx, perTopic)
				defer cancel()
			}
			bit, err := c.Complete(tctx, WriterPrompt(ts.Style, t.Brief))
			if err != nil {
				return // leave bits[i] empty — tolerated, not fatal
			}
			bits[i] = fmt.Sprintf("[%s]\n%s", t.ID, strings.TrimSpace(bit))
		}()
	}
	wg.Wait()

	got := make([]string, 0, len(bits))
	for _, b := range bits {
		if b != "" {
			got = append(got, b)
		}
	}
	minBits := len(ts.Topics) - 1 // tolerate a single transient topic failure
	if minBits < 2 {
		minBits = 2
	}
	if len(got) < minBits {
		return "", fmt.Errorf("only %d/%d topics produced a bit (need >=%d)", len(got), len(ts.Topics), minBits)
	}
	return strings.Join(got, "\n\n"), nil
}
