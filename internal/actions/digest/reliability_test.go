package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotcommander/distill/internal/ai"
)

func TestPlanPackedSourcesLosslessSourceIsolatedUnicode(t *testing.T) {
	parts := make([]SourcePart, 6)
	for i := range parts {
		body := "# First\n\n" + strings.Repeat("alpha é漢字 ", 360) +
			"\n\n# Second\n\n" + strings.Repeat("beta Unicode ", 300)
		if i == 5 {
			body = "# Last\n\nshort unicode é漢字"
		}
		parts[i] = SourcePart{Ordinal: i + 1, Text: body}
	}
	plan, err := PlanPackedSources(parts, 6000, 4000)
	if err != nil {
		t.Fatalf("PlanPackedSources: %v", err)
	}
	if len(plan.Chunks) != 11 {
		t.Fatalf("chunks = %d, want 11", len(plan.Chunks))
	}
	got := make(map[int]string)
	for _, chunk := range plan.Chunks {
		if chunk.Characters > 6000 {
			t.Fatalf("%s has %d runes, exceeds cap", chunk.ID, chunk.Characters)
		}
		if chunk.Tokens > 4000 {
			t.Fatalf("%s has %d tokens, exceeds cap", chunk.ID, chunk.Tokens)
		}
		got[chunk.SourceOrdinal] += chunk.Text
	}
	for _, part := range parts {
		if got[part.Ordinal] != part.Text {
			t.Fatalf("%s not lossless", SourceLabel(part.Ordinal))
		}
	}
	if SourceLabel(5) != "Source 05" || strings.Contains(SourceLabel(5), "/") {
		t.Fatalf("unsafe source label %q", SourceLabel(5))
	}
	if prompt := SourceLabel(5) + "\n\n" + plan.Chunks[8].Text; strings.Contains(prompt, "/Users/") {
		t.Fatalf("provider prompt leaked a local path: %q", prompt)
	}
}

func TestDefaultCallPlanElevenChunksIsThirtyFour(t *testing.T) {
	packed := PackedPlan{Chunks: make([]PackedChunk, 11)}
	plan, err := NewCallPlan(packed, 0, true, 36, 3)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SectionCap != 11 || plan.MandatoryCalls != 34 || plan.RecoveryHeadroom != 2 {
		t.Fatalf("plan = %+v, want cap=11 mandatory=34 recovery=2", plan)
	}
	if err := plan.Preflight(); err != nil {
		t.Fatalf("preflight: %v", err)
	}
}

type scriptedRouteCompleter struct {
	budget *ai.RequestBudget
	steps  []error
	calls  int
}

func (s *scriptedRouteCompleter) Complete(ctx context.Context, _ string) (string, error) {
	if err := s.budget.AcquireContext(ctx, "text"); err != nil {
		return "", err
	}
	s.calls++
	if len(s.steps) > 0 {
		err := s.steps[0]
		s.steps = s.steps[1:]
		if err != nil {
			return "", err
		}
	}
	return "ok", nil
}

func TestDispatcherReservesRetryAndFallbackAtThirtySix(t *testing.T) {
	budget, err := ai.NewRequestBudget(36)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewCallPlan(PackedPlan{Chunks: make([]PackedChunk, 11)}, 0, true, 36, 2)
	if err != nil {
		t.Fatal(err)
	}
	primary := &scriptedRouteCompleter{budget: budget, steps: []error{errors.New("temporary"), errors.New("temporary")}}
	fallback := &scriptedRouteCompleter{budget: budget}
	d := NewDispatcher(RoleRoutes{"research": {Primary: Route{Completer: primary, Available: true, Kind: RoutePrimary}, Fallback: Route{Completer: fallback, Available: true, Kind: RouteFallback}}}, budget, &plan, 2, time.Nanosecond)
	out, route, _, err := d.Complete(context.Background(), "research", "prompt", 0)
	if err != nil || out != "ok" || route.Kind != RouteFallback {
		t.Fatalf("dispatcher = out=%q route=%+v err=%v", out, route, err)
	}
	if primary.calls != 2 || fallback.calls != 1 || budget.Used() != 3 {
		t.Fatalf("calls primary=%d fallback=%d used=%d", primary.calls, fallback.calls, budget.Used())
	}
}

type orderedCompleter struct {
	budget *ai.RequestBudget
	calls  atomic.Int32
	call   func(context.Context, int) (string, error)
}

func (c *orderedCompleter) Complete(ctx context.Context, _ string) (string, error) {
	return c.call(ctx, int(c.calls.Add(1)))
}

func TestDispatcherKeepsMandatoryReservationsAheadOfConcurrentRecovery(t *testing.T) {
	budget, err := ai.NewRequestBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	firstEntered := make(chan struct{})
	allowFirst := make(chan struct{})
	primary := &orderedCompleter{budget: budget}
	primary.call = func(ctx context.Context, call int) (string, error) {
		switch call {
		case 1:
			close(firstEntered)
			<-allowFirst
			if err := budget.AcquireContext(ctx, "text"); err != nil {
				return "", err
			}
			return "first mandatory", nil
		case 2:
			if err := budget.AcquireContext(ctx, "text"); err != nil {
				return "", err
			}
			return "", ai.ErrEmptyResponse
		default:
			return "", errors.New("unexpected primary retry")
		}
	}
	fallback := &orderedCompleter{budget: budget, call: func(context.Context, int) (string, error) {
		return "", errors.New("fallback must not be admitted")
	}}
	d := NewDispatcher(RoleRoutes{"research": {
		Primary:  Route{Completer: primary, Available: true, Kind: RoutePrimary},
		Fallback: Route{Completer: fallback, Available: true, Kind: RouteFallback},
	}}, budget, &CallPlan{MandatoryCalls: 2}, 2, time.Nanosecond)

	var wg sync.WaitGroup
	wg.Go(func() {
		out, _, _, gotErr := d.Complete(context.Background(), "research", "first", 0)
		if gotErr != nil || out != "first mandatory" {
			t.Errorf("first mandatory = out=%q err=%v", out, gotErr)
		}
	})
	<-firstEntered // Its mandatory permit is held, but not yet acquired.
	secondDone := make(chan error, 1)
	wg.Go(func() {
		_, _, _, gotErr := d.Complete(context.Background(), "research", "second", 0)
		secondDone <- gotErr
	})
	if gotErr := <-secondDone; !errors.Is(gotErr, ai.ErrEmptyResponse) {
		t.Fatalf("second mandatory error = %v, want empty response", gotErr)
	}

	// The second mandatory call may not reserve retry or fallback capacity while
	// the first mandatory call still owns its unacquired reservation.
	if got := budget.Used(); got != 1 {
		t.Fatalf("used before first acquire = %d, want 1", got)
	}
	if got := budget.Reserved(); got != 1 {
		t.Fatalf("reserved before first acquire = %d, want 1 mandatory permit", got)
	}
	if got := fallback.calls.Load(); got != 0 {
		t.Fatalf("fallback calls = %d, want 0", got)
	}
	close(allowFirst)
	wg.Wait()
	if got := budget.Used(); got != 2 {
		t.Fatalf("used after both mandatory calls = %d, want 2", got)
	}
}

func TestDispatcherHoldsRetryAndFallbackReservationsUntilTheirAttemptsAcquire(t *testing.T) {
	budget, err := ai.NewRequestBudget(3)
	if err != nil {
		t.Fatal(err)
	}
	retryEntered := make(chan struct{})
	allowRetry := make(chan struct{})
	fallbackEntered := make(chan struct{})
	allowFallback := make(chan struct{})
	primary := &orderedCompleter{budget: budget}
	primary.call = func(ctx context.Context, call int) (string, error) {
		if call == 1 {
			if err := budget.AcquireContext(ctx, "text"); err != nil {
				return "", err
			}
			return "", ai.ErrEmptyResponse
		}
		close(retryEntered)
		<-allowRetry
		if err := budget.AcquireContext(ctx, "text"); err != nil {
			return "", err
		}
		return "", ai.ErrEmptyResponse
	}
	fallback := &orderedCompleter{budget: budget, call: func(ctx context.Context, _ int) (string, error) {
		close(fallbackEntered)
		<-allowFallback
		if err := budget.AcquireContext(ctx, "text"); err != nil {
			return "", err
		}
		return "fallback", nil
	}}
	d := NewDispatcher(RoleRoutes{"research": {
		Primary:  Route{Completer: primary, Available: true, Kind: RoutePrimary},
		Fallback: Route{Completer: fallback, Available: true, Kind: RouteFallback},
	}}, budget, &CallPlan{MandatoryCalls: 1}, 2, time.Nanosecond)

	done := make(chan error, 1)
	go func() {
		out, route, _, gotErr := d.Complete(context.Background(), "research", "prompt", 0)
		if gotErr != nil || out != "fallback" || route.Kind != RouteFallback {
			done <- fmt.Errorf("dispatcher = out=%q route=%+v: %w", out, route, gotErr)
			return
		}
		done <- nil
	}()
	<-retryEntered
	if got := budget.Reserved(); got != 2 {
		t.Fatalf("reserved before retry acquire = %d, want retry plus fallback", got)
	}
	if err := budget.Acquire("text"); !errors.Is(err, ai.ErrRequestBudgetExhausted) {
		t.Fatalf("unreserved call while retry held = %v, want budget exhaustion", err)
	}
	close(allowRetry)
	<-fallbackEntered
	if got := budget.Reserved(); got != 1 {
		t.Fatalf("reserved before fallback acquire = %d, want fallback", got)
	}
	if err := budget.Acquire("text"); !errors.Is(err, ai.ErrRequestBudgetExhausted) {
		t.Fatalf("unreserved call while fallback held = %v, want budget exhaustion", err)
	}
	close(allowFallback)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := budget.Used(); got != 3 {
		t.Fatalf("used = %d, want 3 sent attempts", got)
	}
}

//nolint:gocyclo // Explicit channel sequencing keeps this concurrency regression deterministic.
func TestDispatcherAttributesSentPerAttemptUnderConcurrency(t *testing.T) {
	budget, err := ai.NewRequestBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	canceledEntered := make(chan struct{})
	allowCanceledReturn := make(chan struct{})
	secondAcquired := make(chan struct{})
	primary := &orderedCompleter{budget: budget}
	primary.call = func(ctx context.Context, call int) (string, error) {
		if call == 1 {
			close(canceledEntered)
			<-allowCanceledReturn
			return "", ctx.Err()
		}
		if err := budget.AcquireContext(ctx, "text"); err != nil {
			return "", err
		}
		close(secondAcquired)
		return "second", nil
	}
	d := NewDispatcher(RoleRoutes{"research": {Primary: Route{Completer: primary, Available: true, Kind: RoutePrimary}}}, budget, &CallPlan{MandatoryCalls: 2}, 1, time.Nanosecond)
	var eventsMu sync.Mutex
	var events []AttemptEvent
	d.SetObserver(func(event AttemptEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})

	canceledCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone := make(chan error, 1)
	go func() {
		_, _, _, gotErr := d.Complete(canceledCtx, "research", "canceled", 0)
		firstDone <- gotErr
	}()
	<-canceledEntered
	secondDone := make(chan error, 1)
	go func() {
		out, _, _, gotErr := d.Complete(context.Background(), "research", "sent", 0)
		if gotErr == nil && out == "second" {
			secondDone <- nil
			return
		}
		secondDone <- fmt.Errorf("second = out=%q: %w", out, gotErr)
	}()
	<-secondAcquired
	cancel()
	close(allowCanceledReturn)
	if gotErr := <-firstDone; !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("canceled attempt error = %v", gotErr)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	sent := 0
	canceledUnsent := 0
	for _, event := range events {
		if event.Sent {
			sent++
		}
		if errors.Is(event.Err, context.Canceled) && !event.Sent {
			canceledUnsent++
		}
	}
	if sent != 1 || canceledUnsent != 1 {
		t.Fatalf("events = %+v, want one sent and one canceled-unsent", events)
	}
	if got := d.ProviderUsage()["//primary"]; got != 1 {
		t.Fatalf("provider usage = %d, want exactly one sent attempt", got)
	}
}

func TestValidateOutlineRejectsOverCap(t *testing.T) {
	err := validateOutline("# T\n\n## One\na\n\n## Two\nb", 1)
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("validateOutline = %v, want cap error", err)
	}
}

func TestResponseProvenanceRequiresExactSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chunk-001.md")
	data := []byte("atomic fact")
	params := map[string]string{"route_policy_sha256": "policy", "primary_attempts": "2"}
	route := Route{Provider: "primary.example", Model: "model-a", Kind: RoutePrimary, Available: true}
	expected := requestProvenance("research", "rendered prompt", "upstream", Route{}, params)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadResponseProvenance(path, expected); ok {
		t.Fatal("response without sidecar was reusable")
	}
	meta := requestProvenance("research", "rendered prompt", "upstream", route, params)
	meta.ContentHash = digestHash(string(data))
	if err := WriteResponseProvenance(path, meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadResponseProvenance(path, expected); !ok {
		t.Fatal("exact response sidecar was not reusable")
	}
	bad := expected
	bad.PromptHash = digestHash("changed prompt")
	if _, ok := ReadResponseProvenance(path, bad); ok {
		t.Fatal("prompt change did not invalidate response")
	}
	if err := os.WriteFile(responseSidecarPath(path), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadResponseProvenance(path, expected); ok {
		t.Fatal("corrupt response sidecar was reusable")
	}
}

func TestFinishRunWritesParseableFailureSummary(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.md")
	if err := os.WriteFile(outPath, []byte("checkpoint"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget, err := ai.NewRequestBudget(36)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(nil, budget, nil, 2, time.Nanosecond)
	_, gotErr := finishRun(Options{ArtifactDir: dir, OutPath: outPath, Dispatcher: dispatcher}, time.Now(), &Result{OutPath: outPath, ChunkCount: 11}, &StageError{Stage: "section", Unit: "Two", Err: ai.ErrEmptyResponse})
	if gotErr == nil {
		t.Fatal("finishRun swallowed run failure")
	}
	data, err := os.ReadFile(filepath.Join(dir, "run-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary runSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("parse run summary: %v", err)
	}
	if summary.Status != "failed" || summary.FailingStage != "section" || summary.FailingUnit != "Two" || summary.ErrorClass != "empty_response" {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.OutputSHA256 != "" {
		t.Fatalf("failed summary identifies removed output: %+v", summary)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("failed run left final output: %v", err)
	}
}

func TestRunSummaryOmitsSourceText(t *testing.T) {
	const sentinel = "PRIVATE SOURCE TEXT MUST NOT BE SERIALIZED"
	packed, err := PlanPackedSources([]SourcePart{{Ordinal: 1, Text: sentinel}}, 6000, 0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewCallPlan(packed, 1, true, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(runSummary{CallPlan: &plan})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sentinel) {
		t.Fatalf("run summary leaked source text: %s", data)
	}
	if !strings.Contains(string(data), `"ordinal":1`) || !strings.Contains(string(data), `"sha256":"`) {
		t.Fatalf("run summary lost safe source identity: %s", data)
	}
}
