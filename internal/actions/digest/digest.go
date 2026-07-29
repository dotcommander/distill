// Package digest implements the fact-preserving distillation pipeline: chunk a
// document, extract atomic facts per chunk via an LLM, compile them, then rewrite
// the compiled facts into a cohesive document. No single LLM call ever holds the
// whole source.
package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/fsutil"
	"github.com/dotcommander/distill/internal/prompts"
	"github.com/dotcommander/distill/internal/structured"
	"github.com/dotcommander/distill/internal/tokenizer"

	"github.com/dotcommander/reliquary/chunking"
)

// Completer runs a single LLM completion. *ai.Client satisfies it.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

const (
	stageResearch    = "research"
	stageFuse        = "fuse"
	stageOutline     = "outline"
	stageSection     = "section"
	stageEdit        = "edit"
	stageQualityGate = "quality-gate"
	ledgerActionCall = "call"
)

// CacheMeta is the deterministic self-check metrics stored alongside a cached
// article, so gates (--min-coverage/--min-words) evaluate identically on cache
// hits and fresh runs.
type CacheMeta struct {
	Coverage  extractscore.SpecificsResult `json:"coverage"`
	Words     int                          `json:"words"`
	Citations *CitationResult              `json:"citations,omitempty"`
	Precision *PrecisionResult             `json:"precision,omitempty"`
}

// ArticleCache stores completed digest articles (plus their coverage metrics)
// by caller-provided key.
type ArticleCache interface {
	Load(key string) (article string, meta CacheMeta, ok bool)
	Store(key, article string, meta CacheMeta)
}

// ResearchCache stores per-chunk research responses. It is intentionally scoped
// to extraction reuse and does not affect the article cache key.
type ResearchCache interface {
	Load(chunkText string) (response string, ok bool)
	Store(chunkText, response string)
}

// Options configures one digest run. ChunkSize is a character budget. MaxTokens,
// when >0, is a cl100k_base preflight budget used to split dense character
// chunks; it is an estimate, not a provider context guarantee (0 disables it).
// Concurrency bounds parallel per-chunk extraction (<1
// means serial). Timeout, when >0, caps each individual LLM call.
type Options struct {
	Style                string
	OutPath              string
	FactsPath            string
	ArtifactDir          string
	ChunkSize            int
	MaxTokens            int
	Concurrency          int
	Timeout              time.Duration
	Retries              int
	RetryDelay           time.Duration
	ReuseFacts           bool
	Resume               bool
	Fuse                 bool
	Edit                 bool
	Appendix             bool // when true, append the verbatim compiled research facts as a lossless appendix
	Repair               bool // when true, run one verify→repair reinsert pass for specifics dropped from the article (best-of by coverage)
	DocContext           bool // when true, generate a document-level research header prepended to every extraction prompt
	Cite                 bool // when true, require fact-id markers during generation and strip them from final output after verification
	Cascade              bool // when true, weak fresh research chunks get one optional escalation pass
	CascadeThreshold     float64
	MergeFacts           bool // when true, similar extracted facts are clustered and merged before planning
	MergeThreshold       float64
	OutlineFromClusters  bool // when true, synthesize outline sections from merge clusters
	TargetFacts          int
	MaxSections          int
	MinSectionFacts      int
	ClusterBalanceFactor float64
	CheckPrecision       bool // when true, run sentence-level hallucination checking after the article is fixed
	RequirePrecision     bool // when true, precision judge errors fail the run instead of warning
	PrecisionBatchSize   int
	LedgerPath           string
	Context              string // optional user steering text; prepended to outline/section/edit prompts when non-empty
	Cache                ArticleCache
	CacheKey             string
	CacheRead            bool
	ResearchCache        ResearchCache
	Embedder             BatchEmbedder
	// StoreOK, when non-nil, is consulted after a failure-free verified run;
	// the article is stored in the cache only if it returns true. The digest
	// command wires the deterministic quality gate here so a below-threshold
	// article can never be served from the cache.
	StoreOK func(*Result) bool
	// FinalGate, when non-nil, runs after all result metrics are populated but
	// before either final output is published. A rejection is a terminal quality
	// stage failure and is reflected in run-summary.json.
	FinalGate func(*Result) error
	// Usage, when set, returns the provider's cumulative token counts (prompt,
	// cached, output) at call time. The ledger snapshots it around each serial
	// stage to record per-stage token deltas; the concurrent research stage is
	// accounted as one phase-level event (per-call deltas would double-count
	// across overlapping calls). Nil disables token accounting (deltas stay 0).
	Usage func() (prompt, cached, output int64)
	// PackedPlan is normally prepared once by the CLI and shared by dry-run and
	// execution. Run fills it for legacy single-source callers.
	PackedPlan *PackedPlan
	// CallPlan is the authoritative mandatory-stage plan prepared before any
	// artifact or provider side effect. It is updated after a valid outline.
	CallPlan *CallPlan
	// Dispatcher owns explicit retry/fallback routing for digest roles.
	Dispatcher *Dispatcher
	// ProvenanceParameters contains the exact request policy shared by every
	// reusable response. A non-nil map enables strict schema-v2 sidecars;
	// missing or corrupt metadata is then never reused.
	ProvenanceParameters map[string]string
	writeFile            func(path string, data []byte, perm fs.FileMode) error
	renameFile           func(oldpath, newpath string) error
}

func writeCheckpoint(opts Options, stage, path string, data []byte) error {
	if err := opts.writeFile(path, data, 0o644); err != nil {
		return fmt.Errorf("digest: write %s checkpoint %q: %w", stage, path, err)
	}
	return nil
}

func writeJSONCheckpoint(opts Options, stage, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("digest: encode %s checkpoint %q: %w", stage, path, err)
	}
	return writeCheckpoint(opts, stage, path, data)
}

// Result reports where outputs landed and which chunks failed extraction.
type Result struct {
	OutPath         string
	FactsPath       string
	LedgerPath      string
	ChunkCount      int
	FailedChunks    []string
	FailedSections  []string
	FailedEdits     []string
	ReusedChunks    int
	ReusedSections  int
	ReusedEdits     int
	ReusedFacts     bool
	UnverifiedFacts bool // facts reused from a checkpoint not verified against the current source; the article is not cached
	ReusedOutline   bool
	CacheHit        bool
	Coverage        extractscore.SpecificsResult
	Citations       *CitationResult
	Precision       *PrecisionResult
	Contradictions  int
	SelectedFacts   int
	DeselectedFacts int
	Words           int // word count of the final article (pre-appendix); fed to the deterministic gate
}

// extraction is the compiled output of the per-chunk fact-extraction pass.
type extraction struct {
	facts  string
	count  int
	failed []string
	reused int
}

type resolveFactsInput struct {
	source      string
	opts        Options
	result      *Result
	ledger      *runLedger
	verifiedDir bool
}

const factSeparator = "\n\n---\n\n"
const nearDuplicateFactFlag = "<!-- near-duplicate fact: retained for review; compare with an earlier extracted fact before merging. -->"

type ledgerEvent struct {
	Version      int       `json:"version"`
	Time         time.Time `json:"time"`
	Stage        string    `json:"stage"`
	Unit         string    `json:"unit,omitempty"`
	Action       string    `json:"action"`
	Route        string    `json:"route,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Model        string    `json:"model,omitempty"`
	Attempt      int       `json:"attempt,omitempty"`
	Sent         bool      `json:"sent"`
	Duration     string    `json:"duration,omitempty"`
	ErrorClass   string    `json:"error_class,omitempty"`
	PromptTokens int64     `json:"prompt_tokens,omitempty"`
	CachedTokens int64     `json:"cached_tokens,omitempty"`
	OutputTokens int64     `json:"output_tokens,omitempty"`
}

func (l *runLedger) RecordAttempt(event AttemptEvent) {
	if l == nil {
		return
	}
	l.writeEvent(ledgerEvent{
		Version:    2,
		Time:       time.Now().UTC(),
		Stage:      event.Role,
		Action:     "attempt",
		Route:      string(event.Route.Kind),
		Provider:   event.Route.Provider,
		Model:      event.Route.Model,
		Attempt:    event.Attempt,
		Sent:       event.Sent,
		Duration:   event.Duration.Round(time.Millisecond).String(),
		ErrorClass: ai.ErrorClass(event.Err),
	})
}

// usageSnap is a point-in-time copy of the provider's cumulative token counts.
type usageSnap struct {
	prompt int64
	cached int64
	output int64
}

type runLedger struct {
	mu    sync.Mutex
	path  string
	usage func() (prompt, cached, output int64)
	err   error
}

func newRunLedger(path string, usage func() (prompt, cached, output int64)) (*runLedger, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("digest: create ledger directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("digest: initialize ledger: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("digest: sync initialized ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("digest: close initialized ledger: %w", err)
	}
	return &runLedger{path: path, usage: usage}, nil
}

// usageNow returns the provider's current cumulative token counts, or a zero
// snapshot when no usage func is wired. Nil-safe.
func (l *runLedger) usageNow() usageSnap {
	if l == nil || l.usage == nil {
		return usageSnap{}
	}
	p, c, o := l.usage()
	return usageSnap{prompt: p, cached: c, output: o}
}

// Record appends one ledger event whose token deltas are usageNow()-before,
// where before was snapshotted just before the event's LLM work began. Only
// meaningful for serial stages (fuse/outline/section/edit/repair): under
// concurrency the overlapping windows double-count, so concurrent stages use
// RecordZeroDelta per call plus one phase-level Record for the stage total.
func (l *runLedger) Record(stage, name, action string, started time.Time, err error, before usageSnap) {
	if l == nil {
		return
	}
	ev := l.event(stage, name, action, started, err)
	now := l.usageNow()
	ev.PromptTokens = now.prompt - before.prompt
	ev.CachedTokens = now.cached - before.cached
	ev.OutputTokens = now.output - before.output
	l.writeEvent(ev)
}

// RecordZeroDelta appends an event carrying duration/error but no token
// deltas — for reuse events and concurrent per-call events whose tokens are
// accounted at phase scope.
func (l *runLedger) RecordZeroDelta(stage, name, action string, started time.Time, err error) {
	if l == nil {
		return
	}
	l.writeEvent(l.event(stage, name, action, started, err))
}

func (l *runLedger) event(stage, name, action string, started time.Time, err error) ledgerEvent {
	ev := ledgerEvent{Version: 2, Time: time.Now().UTC(), Stage: stage, Unit: name, Action: action, Sent: action == ledgerActionCall || action == "phase"}
	if !started.IsZero() {
		ev.Duration = time.Since(started).Round(time.Millisecond).String()
	}
	if err != nil {
		ev.ErrorClass = ai.ErrorClass(err)
	}
	return ev
}

func (l *runLedger) writeEvent(ev ledgerEvent) {
	if l == nil {
		return
	}
	data, jerr := json.Marshal(ev)
	if jerr != nil {
		l.mu.Lock()
		if l.err == nil {
			l.err = fmt.Errorf("digest: encode ledger event: %w", jerr)
		}
		l.mu.Unlock()
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ferr := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if ferr != nil {
		if l.err == nil {
			l.err = fmt.Errorf("digest: open ledger: %w", ferr)
		}
		return
	}
	if _, ferr = f.Write(append(data, '\n')); ferr == nil {
		ferr = f.Sync()
	}
	if closeErr := f.Close(); ferr == nil {
		ferr = closeErr
	}
	if ferr != nil && l.err == nil {
		l.err = fmt.Errorf("digest: append ledger: %w", ferr)
	}
}

func (l *runLedger) Err() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// complete runs a single LLM call, capping it with a per-call timeout when one is
// configured. A hard error (or timeout) propagates to the caller.
func complete(ctx context.Context, llm Completer, prompt string, timeout time.Duration) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return llm.Complete(ctx, prompt)
}

// retryBackoff returns the wait before the next attempt: base, 2×base, 4×base …
// capped at 8s.
func retryBackoff(attempt int, base time.Duration) time.Duration {
	d := (time.Duration(1) << uint(attempt-1)) * base
	if max := 8 * time.Second; d > max {
		d = max
	}
	return d
}

// retryComplete runs an LLM call, retrying transient (non-systemic) failures and
// empty responses up to attempts times with exponential backoff. Systemic errors
// (auth/quota/network) and context cancellation abort immediately — retrying
// would fail identically. Cross-model fallback is handled inside the Completer
// (OpenRouter routes server-side); this adds resilience to our-side transient
// blips and empty responses.
func retryComplete(ctx context.Context, stage string, llm Completer, prompt string, timeout time.Duration, attempts int, base time.Duration) (string, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for a := 1; a <= attempts; a++ {
		out, err := complete(ctx, llm, prompt, timeout)
		if err == nil && strings.TrimSpace(out) != "" {
			return strings.TrimSpace(out), nil
		}
		if err == nil {
			err = ai.ErrEmptyResponse
		}
		lastErr = err
		if ai.IsSystemic(err) || ctx.Err() != nil {
			return "", err
		}
		if a < attempts {
			slog.WarnContext(ctx, "digest: retrying LLM call", "stage", stage, "attempt", a, "err", err)
			if err := waitRetry(ctx, retryBackoff(a, base)); err != nil {
				return "", err
			}
		}
	}
	return "", lastErr
}

// retryRoleComplete is the sole normal digest completion dispatch point. When
// a Dispatcher is configured it owns retries and explicit cross-provider
// fallback; legacy direct callers retain retryComplete behavior.
//
//nolint:revive // The compatibility adapter carries the complete direct-call contract.
func retryRoleComplete(ctx context.Context, opts Options, role, stage string, llm Completer, prompt string, timeout time.Duration, attempts int, base time.Duration) (string, error) {
	out, _, err := retryRoleCompleteRoute(ctx, opts, role, stage, llm, prompt, timeout, attempts, base)
	return out, err
}

//nolint:revive // Routing and legacy retry use the same complete call contract.
func retryRoleCompleteRoute(ctx context.Context, opts Options, role, stage string, llm Completer, prompt string, timeout time.Duration, attempts int, base time.Duration) (string, Route, error) {
	if opts.Dispatcher != nil {
		out, route, _, err := opts.Dispatcher.Complete(ctx, role, prompt, timeout)
		if err != nil {
			return "", route, err
		}
		if strings.TrimSpace(out) == "" {
			return "", route, ai.ErrEmptyResponse
		}
		return strings.TrimSpace(out), route, nil
	}
	out, err := retryComplete(ctx, stage, llm, prompt, timeout, attempts, base)
	return out, Route{}, err
}

// retryOutline treats malformed and over-cap structures exactly like an empty
// provider response: they are retryable invalid responses, never a reason to
// allocate unplanned section calls.
//
//nolint:revive // Outline validation adds the cap to the common retry contract.
func retryOutline(ctx context.Context, llm Completer, prompt string, timeout time.Duration, attempts int, base time.Duration, cap int) (string, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		out, err := complete(ctx, llm, prompt, timeout)
		if err == nil {
			out = strings.TrimSpace(out)
			if validateOutline(out, cap) == nil {
				return out, nil
			}
			err = fmt.Errorf("%w: %w", ai.ErrEmptyResponse, validateOutline(out, cap))
		}
		lastErr = err
		if !ai.IsRetryable(err) || ctx.Err() != nil || attempt == attempts {
			return "", err
		}
		if err := waitRetry(ctx, retryBackoff(attempt, base)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validateOutline(out string, cap int) error {
	title, sections := parseOutline(out)
	if strings.TrimSpace(title) == "" || len(sections) == 0 {
		return errors.New("outline must contain a title and at least one section")
	}
	if cap > 0 && len(sections) > cap {
		return fmt.Errorf("outline has %d sections, cap is %d", len(sections), cap)
	}
	return nil
}

// RoleCompleters holds the per-stage model clients for the digest pipeline.
// Each stage may use a different model; callers that want one model for all
// stages set every field to the same Completer. Outline plans the article's
// sections; Section writes one section at a time; Edit rewrites one section at a
// time with the full draft as context.
type RoleCompleters struct {
	Research           Completer
	ResearchEscalation Completer
	Fuse               Completer
	Outline            Completer
	Section            Completer
	Edit               Completer
	Judge              Completer
}

// Run executes the pipeline against source. Empty and failed mandatory
// responses are terminal after dispatcher recovery is exhausted; durable
// checkpoints and partial Result metadata remain available for resume.
func Run(ctx context.Context, rc RoleCompleters, p *prompts.Set, source string, opts Options) (*Result, error) {
	started := time.Now()
	if err := clearFinalPublication(opts); err != nil {
		return finishRun(opts, started, nil, err)
	}
	plan, err := PlanPackedSources([]SourcePart{{Ordinal: 1, Text: source}}, opts.ChunkSize, opts.MaxTokens)
	if err != nil {
		return finishRun(opts, started, nil, err)
	}
	opts.PackedPlan = &plan
	result, err := run(ctx, rc, p, source, opts)
	return finishRun(opts, started, result, err)
}

// RunSources executes digest against ordered structured sources. Provider
// prompts receive only Source NN labels and source content, never paths.
func RunSources(ctx context.Context, rc RoleCompleters, p *prompts.Set, parts []SourcePart, opts Options) (*Result, error) {
	started := time.Now()
	if err := clearFinalPublication(opts); err != nil {
		return finishRun(opts, started, nil, err)
	}
	plan := opts.PackedPlan
	if plan == nil {
		computed, err := PlanPackedSources(parts, opts.ChunkSize, opts.MaxTokens)
		if err != nil {
			return finishRun(opts, started, nil, err)
		}
		plan = &computed
	} else if !samePackedSources(plan.Sources, parts) {
		return finishRun(opts, started, nil, errors.New("digest: supplied packed plan does not match ordered source parts"))
	}
	opts.PackedPlan = plan
	result, err := run(ctx, rc, p, joinSourceParts(plan.Sources), opts)
	return finishRun(opts, started, result, err)
}

func samePackedSources(a, b []SourcePart) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Ordinal != b[i].Ordinal || a[i].Hash != digestHash(b[i].Text) {
			return false
		}
	}
	return true
}

func joinSourceParts(parts []SourcePart) string {
	if len(parts) == 1 {
		return parts[0].Text
	}
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "# %s\n\n", SourceLabel(part.Ordinal))
		b.WriteString(part.Text)
	}
	return b.String()
}

//nolint:funlen,gocognit,gocyclo,revive // This is the explicit durable stage machine for the full digest.
func run(ctx context.Context, rc RoleCompleters, p *prompts.Set, source string, opts Options) (*Result, error) {
	if opts.writeFile == nil {
		opts.writeFile = fsutil.WriteFile
	}
	if opts.renameFile == nil {
		opts.renameFile = os.Rename
	}
	// Direct callers must receive the same fail-closed artifact protection as
	// the CLI. The CLI prepares this before constructing clients; repeating it
	// here protects callers that invoke Run without the CLI boundary.
	if opts.PackedPlan == nil {
		return nil, errors.New("digest: missing packed source plan")
	}
	if opts.CallPlan != nil {
		if err := opts.CallPlan.Preflight(); err != nil {
			return nil, err
		}
	}
	reuseOK, err := PrepareArtifactBindingPlan(opts.ArtifactDir, *opts.PackedPlan)
	if err != nil {
		return nil, err
	}
	if opts.CacheRead && opts.Cache != nil && opts.CacheKey != "" {
		if article, meta, ok := opts.Cache.Load(opts.CacheKey); ok {
			ledgerPath := opts.LedgerPath
			if ledgerPath == "" && opts.ArtifactDir != "" {
				ledgerPath = filepath.Join(opts.ArtifactDir, "run-ledger.jsonl")
			}
			res := &Result{OutPath: opts.OutPath, FactsPath: opts.FactsPath, LedgerPath: ledgerPath, CacheHit: true, Coverage: meta.Coverage, Citations: meta.Citations, Precision: meta.Precision, Words: meta.Words}
			if opts.FinalGate != nil {
				if gateErr := opts.FinalGate(res); gateErr != nil {
					return res, &StageError{Stage: stageQualityGate, Err: gateErr}
				}
			}
			if publishErr := publishFinal(opts, []byte(article)); publishErr != nil {
				return res, publishErr
			}
			slog.InfoContext(ctx, "digest cache hit (skipped research/fuse/outline/section/edit stages)", "out", opts.OutPath)
			return res, nil
		}
	}
	if opts.LedgerPath == "" && opts.ArtifactDir != "" {
		opts.LedgerPath = filepath.Join(opts.ArtifactDir, "run-ledger.jsonl")
	}
	ledger, err := newRunLedger(opts.LedgerPath, opts.Usage)
	if err != nil {
		return nil, err
	}
	if opts.Dispatcher != nil {
		opts.Dispatcher.SetObserver(ledger.RecordAttempt)
	}
	// A newly prepared marker has no reusable artifacts. opts is a value copy,
	// so clearing Resume gates every reuse site — facts, research responses,
	// outline, sections, and edits.
	if !reuseOK {
		if opts.Resume {
			ledger.RecordZeroDelta("artifacts", "", "new", time.Time{}, nil)
		}
		opts.Resume = false
	}
	res := &Result{OutPath: opts.OutPath, FactsPath: opts.FactsPath, LedgerPath: opts.LedgerPath}
	retries := opts.Retries
	if retries < 1 {
		retries = 1
	}
	backoff := opts.RetryDelay
	if backoff <= 0 {
		backoff = time.Second
	}

	// Optional user steering: prepend to the writing-stage prompts only (outline,
	// section, edit) — never research, so fact extraction/recall is unaffected.
	// Empty Context means the prompts are byte-identical to before (cache-safe).
	ctxBlock := ""
	if opts.Context != "" {
		ctxBlock = p.RenderContextPreamble(opts.Context)
	}
	citeSectionBlock := ""
	citeEditBlock := ""
	if opts.Cite {
		citeSectionBlock = p.CiteSection
		citeEditBlock = p.CiteEdit
	}

	facts, err := resolveFacts(ctx, rc.Research, rc.ResearchEscalation, p, resolveFactsInput{source: source, opts: opts, result: res, ledger: ledger, verifiedDir: reuseOK})
	if err != nil {
		return res, &StageError{Stage: stageResearch, Err: err}
	}
	factsAppendix := facts // pre-fuse snapshot for the lossless appendix
	coverageBase := facts

	// fuse: merge the per-chunk research notes into one organized set (opt-in).
	if opts.Fuse {
		slog.InfoContext(ctx, "digest fuse start")
		started := time.Now()
		beforeUsage := ledger.usageNow()
		fused, ferr := retryRoleComplete(ctx, opts, "fuse", "fuse", rc.Fuse, p.RenderFuse(facts), opts.Timeout, retries, backoff)
		ledger.Record("fuse", "", "call", started, ferr, beforeUsage)
		if ferr != nil {
			return res, &StageError{Stage: "fuse", Err: ferr}
		}
		if strings.TrimSpace(fused) == "" {
			return nil, errors.New("digest: fuse returned empty output")
		}
		facts = strings.TrimSpace(fused)
		if fusedWriteErr := writeCheckpoint(opts, "fused facts", filepath.Join(opts.ArtifactDir, "facts.fused.md"), []byte(facts)); fusedWriteErr != nil {
			return nil, fusedWriteErr
		}
		slog.InfoContext(ctx, "digest fuse done")
	}

	var mergedClusters []factCluster
	if opts.MergeFacts {
		if opts.Embedder == nil {
			return nil, errors.New("digest: --merge-facts requires an embedder")
		}
		slog.InfoContext(ctx, "digest merge start")
		merged, merr := mergeFacts(ctx, rc.Fuse, opts.Embedder, p, facts, opts, ledger)
		if merr != nil {
			return nil, merr
		}
		facts = merged.Facts
		mergedClusters = merged.Clusters
		res.Contradictions = len(merged.Contradictions)
		if werr := writeMergeArtifacts(opts, merged); werr != nil {
			return nil, werr
		}
		slog.InfoContext(ctx, "digest merge done", "contradictions", res.Contradictions)
	}

	if opts.TargetFacts > 0 {
		if opts.Embedder == nil {
			return nil, errors.New("digest: --target-facts requires an embedder")
		}
		selected, kept, total, serr := selectTargetFacts(ctx, opts.Embedder, facts, opts.TargetFacts, opts, ledger)
		if serr != nil {
			return nil, serr
		}
		facts = selected
		coverageBase = selected
		res.SelectedFacts = kept
		res.DeselectedFacts = total - kept
		slog.InfoContext(ctx, "digest target-facts applied", "kept", kept, "total", total)
	}

	// Number the facts so the outline can route each one to a section: numbered is
	// what the planner sees; units is the per-ID lookup used to build per-section facts.
	units, numbered := numberFacts(facts)

	// outline: plan the article's sections from the facts. One small call whose
	// output is just the structure, not prose.
	slog.InfoContext(ctx, "digest outline start")
	outlinePath := filepath.Join(opts.ArtifactDir, "responses", "outline.md")
	outlineText := ""
	outlineCap := max(3, len(opts.PackedPlan.Chunks))
	if opts.CallPlan != nil {
		outlineCap = opts.CallPlan.SectionCap
	}
	outlinePrompt := ctxBlock + p.RenderOutlineCapped(opts.Style, numbered, outlineCap)
	if opts.Resume {
		if data, ok := readProvenancedResponse(opts, outlinePath, "outline", outlinePrompt, numbered+"\x00"+ctxBlock+"\x00"+opts.Style); ok {
			outlineText = data
			res.ReusedOutline = true
			opts.Dispatcher.ReleaseMandatory(1)
			ledger.RecordZeroDelta("outline", "", "reuse", time.Time{}, nil)
		}
	}
	if outlineText == "" {
		if opts.OutlineFromClusters {
			outlineText, err = synthesizeOutlineFromClusters(ctx, rc.Fuse, p, units, mergedClusters, opts, ledger)
			if err != nil {
				return nil, err
			}
		}
		if strings.TrimSpace(outlineText) == "" {
			started := time.Now()
			beforeUsage := ledger.usageNow()
			var outlineRoute Route
			if opts.Dispatcher != nil {
				outlineText, outlineRoute, _, err = opts.Dispatcher.CompleteValidated(ctx, "outline", outlinePrompt, opts.Timeout, func(out string) error { return validateOutline(strings.TrimSpace(out), outlineCap) })
			} else {
				outlineText, err = retryOutline(ctx, rc.Outline, outlinePrompt, opts.Timeout, retries, backoff, outlineCap)
			}
			ledger.Record("outline", "", "call", started, err, beforeUsage)
			if err != nil {
				return res, &StageError{Stage: stageOutline, Err: err}
			}
			if err := writeProvenancedResponse(opts, "outline", outlinePath, "outline", outlinePrompt, numbered+"\x00"+ctxBlock+"\x00"+opts.Style, outlineRoute, []byte(strings.TrimSpace(outlineText))); err != nil {
				return nil, err
			}
		} else if err := writeCheckpoint(opts, "outline", outlinePath, []byte(strings.TrimSpace(outlineText))); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(outlineText) == "" {
		return nil, errors.New("digest: outline returned empty output")
	}
	outlineText = strings.TrimSpace(outlineText)
	title, secs := parseOutline(outlineText)
	if len(secs) == 0 {
		return nil, errors.New("digest: outline produced no sections")
	}
	slog.InfoContext(ctx, "digest outline done", "sections", len(secs))

	// Merge sections with duplicate normalized headings before routing.
	secs = mergeDuplicateSections(ctx, secs)

	// Route facts to sections. If the outline assigned any fact IDs, give each
	// section only its facts (kills the recall gap and the per-call quadratic cost)
	// and append a catch-all so orphaned facts are never silently dropped. If the
	// outline assigned nothing (old prompt / non-compliance), routing stays off and
	// every section gets all facts, exactly as before.
	routing := false
	for _, s := range secs {
		if len(s.factIDs) > 0 {
			routing = true
			break
		}
	}
	sectionFacts := make([]string, len(secs))
	if routing {
		assigned := make(map[int]bool)
		for i := range secs {
			ids, dropped := routeSectionFacts(assigned, secs[i].factIDs)
			if dropped > 0 {
				slog.DebugContext(ctx, "digest outline dropped duplicate fact IDs from section", "section", secs[i].title, "dropped", dropped)
			}
			if opts.Cite {
				sectionFacts[i] = selectFactsTagged(units, ids)
			} else {
				sectionFacts[i] = selectFacts(units, ids)
			}
		}
		var orphans []int
		for _, u := range units {
			if !assigned[u.id] {
				orphans = append(orphans, u.id)
			}
		}
		if len(orphans) > 0 {
			if len(secs) >= outlineCap {
				last := len(secs) - 1
				secs[last].factIDs = append(secs[last].factIDs, orphans...)
				if opts.Cite {
					sectionFacts[last] += "\n\n" + selectFactsTagged(units, orphans)
				} else {
					sectionFacts[last] += "\n\n" + selectFacts(units, orphans)
				}
			} else {
				secs = append(secs, section{title: "Additional details", intent: "Remaining facts not otherwise covered above.", factIDs: orphans})
				if opts.Cite {
					sectionFacts = append(sectionFacts, selectFactsTagged(units, orphans))
				} else {
					sectionFacts = append(sectionFacts, selectFacts(units, orphans))
				}
			}
			slog.WarnContext(ctx, "digest outline left facts unassigned; routed to catch-all section", "orphans", len(orphans))
		}
	} else {
		for i := range secs {
			if opts.Cite {
				sectionFacts[i] = numbered
			} else {
				sectionFacts[i] = facts
			}
		}
	}
	if len(secs) > outlineCap {
		return res, &StageError{Stage: stageOutline, Err: fmt.Errorf("final routed outline has %d sections, cap is %d", len(secs), outlineCap)}
	}
	if opts.CallPlan != nil {
		beforeMandatory := opts.CallPlan.MandatoryCalls
		if err := opts.CallPlan.ReleaseUnusedSections(len(secs), retries); err != nil {
			return res, &StageError{Stage: stageOutline, Err: err}
		}
		opts.Dispatcher.ReleaseMandatory(beforeMandatory - opts.CallPlan.MandatoryCalls)
	}

	// expand: write each section in order. Each call sees its section's facts, the
	// full outline, and the sections already written, but emits only one section — so
	// no single call must output the whole article (which causes verbatim echo).
	bodies := make([]string, len(secs))
	for i, s := range secs {
		sectionPath := filepath.Join(opts.ArtifactDir, "responses", fmt.Sprintf("section-%03d.md", i+1))
		prior := assembleArticle(title, secs[:i], bodies[:i])
		sectionPrompt := ctxBlock + citeSectionBlock + p.RenderSection(opts.Style, outlineText, sectionFacts[i], prior, s.title, s.intent)
		sectionUpstream := outlineText + "\x00" + sectionFacts[i] + "\x00" + prior + "\x00" + opts.Style + "\x00" + ctxBlock + "\x00" + citeSectionBlock
		if opts.Resume {
			if data, ok := readProvenancedResponse(opts, sectionPath, "section", sectionPrompt, sectionUpstream); ok {
				bodies[i] = data
				res.ReusedSections++
				opts.Dispatcher.ReleaseMandatory(1)
				ledger.RecordZeroDelta("section", s.title, "reuse", time.Time{}, nil)
				slog.InfoContext(ctx, "digest section done", "section", s.title, "n", i+1, "total", len(secs))
				continue
			}
		}
		stage := fmt.Sprintf("section %d/%d: %s", i+1, len(secs), s.title)
		slog.InfoContext(ctx, "digest section start", "section", s.title, "n", i+1, "total", len(secs))
		started := time.Now()
		beforeUsage := ledger.usageNow()
		body, route, serr := retryRoleCompleteRoute(ctx, opts, "section", stage, rc.Section, sectionPrompt, opts.Timeout, retries, backoff)
		ledger.Record("section", s.title, "call", started, serr, beforeUsage)
		if serr != nil {
			res.FailedSections = append(res.FailedSections, s.title)
			return res, &StageError{Stage: stageSection, Unit: s.title, Err: serr}
		}
		bodies[i] = body
		if err := writeProvenancedResponse(opts, "section draft", sectionPath, "section", sectionPrompt, sectionUpstream, route, []byte(bodies[i])); err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "digest section done", "section", s.title, "n", i+1, "total", len(secs))
	}
	draft := assembleArticle(title, secs, bodies)
	if err := writeCheckpoint(opts, "draft", filepath.Join(opts.ArtifactDir, "responses", "draft.md"), []byte(draft)); err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "digest draft done", "sections", len(secs))

	// edit (opt-in): rewrite one section at a time against the STABLE full draft as
	// read-only context — it is identical across every edit call, so the provider
	// prompt cache covers it. Full global awareness, bounded output — the editor
	// cannot echo the whole article because it may emit only one section.
	final := draft
	if opts.Edit {
		for i, s := range secs {
			editPath := filepath.Join(opts.ArtifactDir, "responses", fmt.Sprintf("section-%03d.edited.md", i+1))
			editFacts := facts
			if opts.Cite {
				editFacts = numbered
			}
			priorEdited := assembleArticle(title, secs[:i], bodies[:i])
			editPrompt := ctxBlock + citeEditBlock + p.RenderEditSection(opts.Style, draft, priorEdited, editFacts, s.title)
			editUpstream := draft + "\x00" + priorEdited + "\x00" + editFacts + "\x00" + opts.Style + "\x00" + ctxBlock + "\x00" + citeEditBlock
			if opts.Resume {
				if data, ok := readProvenancedResponse(opts, editPath, "edit", editPrompt, editUpstream); ok {
					bodies[i] = data
					res.ReusedEdits++
					opts.Dispatcher.ReleaseMandatory(1)
					ledger.RecordZeroDelta("edit", s.title, "reuse", time.Time{}, nil)
					slog.InfoContext(ctx, "digest edit done", "section", s.title, "n", i+1, "total", len(secs))
					continue
				}
			}
			stage := fmt.Sprintf("edit section %d/%d: %s", i+1, len(secs), s.title)
			slog.InfoContext(ctx, "digest edit start", "section", s.title, "n", i+1, "total", len(secs))
			started := time.Now()
			beforeUsage := ledger.usageNow()
			edited, route, eerr := retryRoleCompleteRoute(ctx, opts, "edit", stage, rc.Edit, editPrompt, opts.Timeout, retries, backoff)
			ledger.Record("edit", s.title, "call", started, eerr, beforeUsage)
			if eerr != nil {
				res.FailedEdits = append(res.FailedEdits, s.title)
				return res, &StageError{Stage: "edit", Unit: s.title, Err: eerr}
			}
			bodies[i] = edited
			if err := writeProvenancedResponse(opts, "section edit", editPath, "edit", editPrompt, editUpstream, route, []byte(bodies[i])); err != nil {
				return nil, err
			}
			slog.InfoContext(ctx, "digest edit done", "section", s.title, "n", i+1, "total", len(secs))
		}
		final = assembleArticle(title, secs, bodies)
	}

	citedForPrecision := ""
	if opts.Cite {
		cited := final
		if err := writeCheckpoint(opts, "cited rewrite", filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"), []byte(cited)); err != nil {
			return nil, err
		}
		citations := computeCitations(units, cited)
		res.Citations = &citations
		if err := writeJSONCheckpoint(opts, "citations", filepath.Join(opts.ArtifactDir, "responses", "citations.json"), citations); err != nil {
			return nil, err
		}
		if citations.Total > 0 && len(citeGroupRe.FindAllString(cited, -1)) == 0 {
			slog.WarnContext(ctx, "digest citation check found no markers; model ignored citation instructions", "total", citations.Total)
		} else if opts.Repair && len(citations.MissingIDs) > 0 {
			var rerr error
			cited, citations, rerr = repairMissingCited(repairInput{
				ctx: ctx, llm: rc.Edit, prompts: p, ledger: ledger,
				timeout: opts.Timeout, attempts: retries, backoff: backoff,
				dispatch: opts.Dispatcher,
			}, units, cited, citations)
			if rerr != nil {
				return nil, fmt.Errorf("digest: citation repair: %w", rerr)
			}
			res.Citations = &citations
			if err := writeCheckpoint(opts, "cited rewrite", filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"), []byte(cited)); err != nil {
				return nil, err
			}
			if err := writeJSONCheckpoint(opts, "citations", filepath.Join(opts.ArtifactDir, "responses", "citations.json"), citations); err != nil {
				return nil, err
			}
		}
		citedForPrecision = cited
		final = stripCiteMarkers(cited)
	}

	// Deterministic, offline fact-coverage self-check: how many specifics
	// (numbers, dates, names) from the research facts survive into the article.
	// Computed on the pre-appendix article so the appendix cannot inflate it.
	res.Coverage = extractscore.SpecificsCoverage(coverageBase, final)
	if err := writeJSONCheckpoint(opts, "coverage", filepath.Join(opts.ArtifactDir, "responses", "coverage.json"), res.Coverage); err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "digest fact-coverage", "covered", res.Coverage.Covered, "total", res.Coverage.Total, "dropped", len(res.Coverage.Missing))

	// verify→repair: if the user opted in and any specific was dropped, run one
	// bounded reinsert pass against the edit-role model, then recompute coverage.
	// Best-of by Covered — repair can only raise coverage, never lower it.
	if opts.Repair && !opts.Cite && len(res.Coverage.Missing) > 0 {
		var rerr error
		final, res.Coverage, rerr = repairMissing(repairInput{
			ctx: ctx, llm: rc.Edit, prompts: p, ledger: ledger,
			timeout: opts.Timeout, attempts: retries, backoff: backoff,
			dispatch: opts.Dispatcher,
		}, coverageBase, final, res.Coverage)
		if rerr != nil {
			return nil, fmt.Errorf("digest: coverage repair: %w", rerr)
		}
		if err := writeJSONCheckpoint(opts, "coverage", filepath.Join(opts.ArtifactDir, "responses", "coverage.json"), res.Coverage); err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "digest fact-coverage after repair", "covered", res.Coverage.Covered, "total", res.Coverage.Total, "dropped", len(res.Coverage.Missing))
	}

	if opts.CheckPrecision {
		if rc.Judge == nil {
			err := errors.New("digest: precision check requires a judge completer")
			if opts.RequirePrecision {
				return nil, err
			}
			slog.WarnContext(ctx, "digest precision skipped", "err", err)
		} else {
			sentences := extractscore.SplitSentences(final)
			if opts.Cite {
				sentences = citedSentencesForPrecision(citedForPrecision)
			}
			precision, perr := checkPrecisionDispatched(ctx, rc.Judge, p, facts, sentences, opts.PrecisionBatchSize, ledger, opts.Dispatcher, opts.Timeout, retries, backoff)
			if perr != nil {
				if ai.IsSystemic(perr) {
					return nil, fmt.Errorf("digest: precision: %w", perr)
				}
				if opts.RequirePrecision {
					return nil, perr
				}
				slog.WarnContext(ctx, "digest precision failed, continuing without precision gate", "err", perr)
			} else {
				preUnsupported := len(precision.Unsupported)
				if opts.Repair && preUnsupported > 0 {
					repairArticle := final
					if opts.Cite {
						repairArticle = citedForPrecision
					}
					repaired, repairedCov, rerr := repairPrecisionDispatched(ctx, rc.Judge, p, facts, precision.Unsupported, repairArticle, coverageBase, res.Coverage, ledger, opts.Dispatcher, opts.Timeout, retries, backoff)
					if rerr != nil && ai.IsSystemic(rerr) {
						return nil, fmt.Errorf("digest: precision repair: %w", rerr)
					}
					if rerr == nil {
						repairSentences := extractscore.SplitSentences(repaired)
						if opts.Cite {
							repairSentences = citedSentencesForPrecision(repaired)
						}
						repairedPrecision, rperr := checkPrecisionDispatched(ctx, rc.Judge, p, facts, repairSentences, opts.PrecisionBatchSize, ledger, opts.Dispatcher, opts.Timeout, retries, backoff)
						if rperr != nil {
							if ai.IsSystemic(rperr) {
								return nil, fmt.Errorf("digest: precision re-check: %w", rperr)
							}
							if opts.RequirePrecision {
								return nil, rperr
							}
							slog.WarnContext(ctx, "digest precision re-check failed, continuing with pre-repair precision", "err", rperr)
						} else {
							final = repaired
							res.Coverage = repairedCov
							if err := writeJSONCheckpoint(opts, "coverage", filepath.Join(opts.ArtifactDir, "responses", "coverage.json"), repairedCov); err != nil {
								return nil, err
							}
							if opts.Cite {
								repairedCitations := computeCitations(units, repaired)
								res.Citations = &repairedCitations
								citedForPrecision = repaired
								final = stripCiteMarkers(repaired)
								if err := writeCheckpoint(opts, "cited rewrite", filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"), []byte(citedForPrecision)); err != nil {
									return nil, err
								}
								if err := writeJSONCheckpoint(opts, "citations", filepath.Join(opts.ArtifactDir, "responses", "citations.json"), repairedCitations); err != nil {
									return nil, err
								}
							}
							precision = repairedPrecision
							slog.InfoContext(ctx, "digest precision repaired", "unsupported_pre", preUnsupported, "unsupported_post", len(precision.Unsupported))
						}
					}
				}
				res.Precision = &precision
				if err := writeJSONCheckpoint(opts, "precision", filepath.Join(opts.ArtifactDir, "responses", "precision.json"), precision); err != nil {
					return nil, err
				}
				slog.InfoContext(ctx, "digest precision", "supported", precision.Supported, "total", precision.Total, "unsupported", len(precision.Unsupported))
			}
		}
	}
	res.Words = len(strings.Fields(final))

	if opts.Appendix {
		var b strings.Builder
		b.WriteString("\n\n---\n\n# Appendix: Extracted Facts\n\n")
		b.WriteString("Verbatim structured data and atomic facts, preserved in full. " +
			"The article above is a synthesis; this appendix is the lossless record.\n\n")
		var tables []structured.Block
		for _, blk := range structured.Extract(source) {
			if blk.Confidence >= 0.6 {
				tables = append(tables, blk)
			}
		}
		if len(tables) > 0 {
			b.WriteString(structured.Render(tables))
			b.WriteString("\n## Research Notes\n\n")
		}
		b.WriteString(factsAppendix + "\n")
		final += b.String()
	}

	if opts.FinalGate != nil {
		if err := opts.FinalGate(res); err != nil {
			return res, &StageError{Stage: stageQualityGate, Err: err}
		}
	}
	if err := ledger.Err(); err != nil {
		return nil, err
	}
	if err := publishFinal(opts, []byte(final)); err != nil {
		return res, err
	}
	if shouldStoreArticle(opts, res) {
		opts.Cache.Store(opts.CacheKey, final, CacheMeta{Coverage: res.Coverage, Citations: res.Citations, Precision: res.Precision, Words: res.Words})
	}
	return res, nil
}

// shouldStoreArticle reports whether a finished run's article may enter the
// output cache: never on partial failures, never when facts came from an
// unverified checkpoint (the key would not describe the actual inputs), never
// after a precision pass that can rewrite the article, and only if the caller's
// StoreOK gate (when set) passes.
func shouldStoreArticle(opts Options, res *Result) bool {
	if opts.Cache == nil || opts.CacheKey == "" {
		return false
	}
	if opts.CheckPrecision {
		return false
	}
	if len(res.FailedChunks)+len(res.FailedSections)+len(res.FailedEdits) > 0 {
		return false
	}
	if res.UnverifiedFacts {
		return false
	}
	return opts.StoreOK == nil || opts.StoreOK(res)
}

func cancellationErr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return nil
}

func readNonEmptyFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "", false
	}
	return string(data), true
}

func readReusableArtifact(path, stage string) (string, bool) {
	data, ok := readNonEmptyFile(path)
	if !ok || !artifactReusable(stage, data) {
		return "", false
	}
	return data, true
}

func artifactReusable(stage, data string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	norm := strings.ToLower(trimmed)
	switch {
	case strings.Contains(norm, "this section could not be generated"):
		return false
	case strings.Contains(norm, "could not be generated"):
		return false
	case strings.Contains(norm, "empty response"):
		return false
	}
	if stage == stageOutline {
		_, secs := parseOutline(trimmed)
		return len(secs) > 0
	}
	return true
}

// ArtifactReusableForResume reports whether a persisted artifact is safe enough
// for resume planning to count and reuse. It intentionally rejects empty files,
// known placeholders, and outlines that cannot be parsed into sections.
func ArtifactReusableForResume(stage, data string) bool {
	return artifactReusable(stage, data)
}

// resolveFacts returns the compiled facts: read from the checkpoint when
// ReuseFacts is set, otherwise produced by extracting from source (updating res
// with chunk counts and writing the checkpoint).
func resolveFacts(ctx context.Context, llm, escalation Completer, p *prompts.Set, in resolveFactsInput) (string, error) {
	within := pathWithin(in.opts.ArtifactDir, in.opts.FactsPath)
	if in.opts.ReuseFacts || (in.opts.Resume && within && in.opts.ProvenanceParameters == nil) { //nolint:nestif // Reuse needs all three independent safety conditions.
		data, ok := readReusableArtifact(in.opts.FactsPath, "facts")
		if ok {
			in.result.ReusedFacts = true
			if !in.verifiedDir || !within {
				in.result.UnverifiedFacts = true
				slog.WarnContext(ctx, "digest: reusing facts not verified against current source; result will not be cached", "facts", in.opts.FactsPath)
			}
			if in.ledger != nil {
				in.ledger.RecordZeroDelta("facts", filepath.Base(in.opts.FactsPath), "reuse", time.Time{}, nil)
			}
			return data, nil
		}
		if in.opts.ReuseFacts {
			return "", fmt.Errorf("digest: reusing facts: %s is missing, empty, or not reusable", in.opts.FactsPath)
		}
	}

	ex, err := researchAndCompile(ctx, llm, escalation, p, in.source, in.opts, in.ledger)
	if err != nil {
		return "", err
	}
	in.result.ChunkCount = ex.count
	in.result.FailedChunks = ex.failed
	in.result.ReusedChunks = ex.reused
	if strings.TrimSpace(ex.facts) == "" {
		return "", fmt.Errorf("digest: no facts extracted (all %d chunks returned empty)", ex.count)
	}
	if err := writeCheckpoint(in.opts, "compiled facts", in.opts.FactsPath, []byte(ex.facts)); err != nil {
		return "", err
	}
	return ex.facts, nil
}

// researchAndCompile extracts packed chunks with bounded parallelism and
// assembles facts in source order. Any exhausted mandatory response cancels
// admission and aborts; source chunks are checkpointed before provider work.
func researchAndCompile(ctx context.Context, llm, escalation Completer, p *prompts.Set, source string, opts Options, ledger *runLedger) (extraction, error) {
	if opts.PackedPlan == nil {
		return extraction{}, errors.New("digest: missing packed source plan")
	}
	chunks := opts.PackedPlan.Chunks
	total := len(chunks)
	if total == 0 {
		return extraction{}, errors.New("digest: source produced no research chunks")
	}
	slog.InfoContext(ctx, "digest chunking done", "chunks", total)

	for _, chunk := range chunks {
		id := chunk.ID
		if werr := writeCheckpoint(opts, "source chunk", filepath.Join(opts.ArtifactDir, "chunks", id+".md"), []byte(chunk.Text)); werr != nil {
			return extraction{}, werr
		}
	}

	retries := opts.Retries
	if retries < 1 {
		retries = 1
	}
	backoff := opts.RetryDelay
	if backoff <= 0 {
		backoff = time.Second
	}
	headerBlock := ""
	if opts.DocContext {
		header, herr := resolveDocHeader(ctx, llm, p, source, chunks[0].Text, opts, ledger, retries, backoff)
		if herr != nil {
			return extraction{}, herr
		}
		headerBlock = p.RenderDocHeaderPreamble(header)
	}

	limit := opts.Concurrency
	if limit < 1 {
		limit = 1
	}
	outs := make([]string, total)
	failedFlag := make([]bool, total)
	reusedFlag := make([]bool, total)

	gctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	var workers sync.WaitGroup
	var firstErr error
	var firstErrMu sync.Mutex
	setErr := func(err error) {
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		firstErrMu.Unlock()
	}
	phaseStart := time.Now()
	phaseBefore := ledger.usageNow()
	worker := func() {
		defer workers.Done()
		for i := range jobs {
			if gctx.Err() != nil {
				return
			}
			chunk := chunks[i]
			id := chunk.ID
			responsePath := filepath.Join(opts.ArtifactDir, "responses", id+".md")
			prompt := headerBlock + p.RenderResearch(id, SourceLabel(chunk.SourceOrdinal)+"\n\n"+chunk.Text)
			upstream := headerBlock + "\x00" + chunk.Hash
			if opts.Resume {
				if data, ok := readProvenancedResponse(opts, responsePath, "research", prompt, upstream); ok {
					outs[i] = strings.TrimSpace(data)
					if werr := writeCheckpoint(opts, "resumed research response", responsePath, []byte(outs[i])); werr != nil {
						setErr(werr)
						return
					}
					reusedFlag[i] = true
					opts.Dispatcher.ReleaseMandatory(1)
					ledger.RecordZeroDelta("research", id, "reuse", time.Time{}, nil)
					slog.InfoContext(gctx, "digest research done", "chunk", id, "n", i+1, "total", total)
					continue
				}
			}
			if opts.ResearchCache != nil {
				cacheText := researchCacheText(headerBlock, chunk.Text)
				if data, ok := opts.ResearchCache.Load(cacheText); ok {
					outs[i] = strings.TrimSpace(data)
					reusedFlag[i] = true
					opts.Dispatcher.ReleaseMandatory(1)
					if werr := writeCheckpoint(opts, "cached research response", responsePath, []byte(outs[i])); werr != nil {
						setErr(werr)
						return
					}
					ledger.RecordZeroDelta("research", id, "cache", time.Time{}, nil)
					slog.InfoContext(gctx, "digest research cache hit", "chunk", id, "n", i+1, "total", total)
					continue
				}
			}
			slog.InfoContext(gctx, "digest research", "chunk", id, "n", i+1, "total", total)
			started := time.Now()
			out, route, cerr := retryRoleCompleteRoute(gctx, opts, "research", "research", llm, prompt, opts.Timeout, retries, backoff)
			// Tokens for concurrent research calls are accounted once at phase
			// scope (overlapping per-call windows would double-count).
			ledger.RecordZeroDelta("research", id, "call", started, cerr)
			if cerr != nil {
				setErr(fmt.Errorf("digest: research %s: %w", id, cerr))
				return
			}
			out, cerr = maybeEscalateResearch(researchEscalationInput{
				ctx: gctx, llm: escalation, prompts: p, headerBlock: headerBlock,
				id: id, chunkText: chunk.Text, baseOut: out, options: opts, ledger: ledger,
			})
			if cerr != nil {
				setErr(fmt.Errorf("digest: research escalation %s: %w", id, cerr))
				return
			}
			if werr := writeProvenancedResponse(opts, "research response", responsePath, "research", prompt, upstream, route, []byte(out)); werr != nil {
				setErr(werr)
				return
			}
			if opts.ResearchCache != nil {
				opts.ResearchCache.Store(researchCacheText(headerBlock, chunk.Text), out)
			}
			outs[i] = strings.TrimSpace(out)
			slog.InfoContext(gctx, "digest research done", "chunk", id, "n", i+1, "total", total)
		}
	}
	workers.Add(limit)
	for range limit {
		go worker()
	}
dispatchJobs:
	for i := range chunks {
		select {
		case <-gctx.Done():
			break dispatchJobs
		case jobs <- i:
		}
		if gctx.Err() != nil {
			break dispatchJobs
		}
	}
	close(jobs)
	workers.Wait()
	firstErrMu.Lock()
	werr := firstErr
	firstErrMu.Unlock()
	ledger.Record("research", "", "phase", phaseStart, werr, phaseBefore)
	if werr != nil {
		return extraction{}, werr
	}

	facts, failed, reused := compileFacts(outs, failedFlag, reusedFlag)
	slog.InfoContext(ctx, "digest research complete", "chunks", total, "failed", len(failed))
	return extraction{facts: facts, count: total, failed: failed, reused: reused}, nil
}

type researchEscalationInput struct {
	ctx         context.Context
	llm         Completer
	prompts     *prompts.Set
	headerBlock string
	id          string
	chunkText   string
	baseOut     string
	options     Options
	ledger      *runLedger
}

func maybeEscalateResearch(in researchEscalationInput) (string, error) {
	ctx, llm, p := in.ctx, in.llm, in.prompts
	headerBlock, id, chunkText, baseOut := in.headerBlock, in.id, in.chunkText, in.baseOut
	opts, ledger := in.options, in.ledger
	baseOut = strings.TrimSpace(baseOut)
	if !opts.Cascade || opts.CascadeThreshold <= 0 || llm == nil {
		return baseOut, nil
	}
	cov := extractscore.SpecificsCoverage(chunkText, baseOut)
	if cov.Total < 5 || float64(cov.Covered)/float64(cov.Total) >= opts.CascadeThreshold {
		return baseOut, nil
	}
	started := time.Now()
	out, err := retryRoleComplete(ctx, opts, "research-escalation", "research escalation", llm, headerBlock+p.RenderResearch(id, chunkText), opts.Timeout, opts.Retries, opts.RetryDelay)
	if ledger != nil {
		ledger.RecordZeroDelta("research", id, "escalate", started, err)
	}
	if err != nil {
		if ai.IsSystemic(err) {
			return baseOut, err
		}
		slog.WarnContext(ctx, "digest research escalation skipped", "chunk", id, "err", err)
		return baseOut, nil
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return baseOut, nil
	}
	return baseOut + "\n" + out, nil
}

func resolveDocHeader(ctx context.Context, llm Completer, p *prompts.Set, source, excerpt string, opts Options, ledger *runLedger, retries int, backoff time.Duration) (string, error) {
	headerPath := filepath.Join(opts.ArtifactDir, "responses", "doc-context.md")
	skeleton := headingSkeleton(source, 100)
	prompt := p.RenderDocContext(excerpt, skeleton)
	upstream := excerpt + "\x00" + skeleton
	if opts.Resume {
		if data, ok := readProvenancedResponse(opts, headerPath, "doc-context", prompt, upstream); ok {
			opts.Dispatcher.ReleaseMandatory(1)
			ledger.RecordZeroDelta("doc-context", "", "reuse", time.Time{}, nil)
			return strings.TrimSpace(data), nil
		}
	}
	started := time.Now()
	beforeUsage := ledger.usageNow()
	header, route, err := retryRoleCompleteRoute(ctx, opts, "doc-context", "doc-context", llm, prompt, opts.Timeout, retries, backoff)
	ledger.Record("doc-context", "", "call", started, err, beforeUsage)
	if err != nil {
		return "", fmt.Errorf("digest: doc-context: %w", err)
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return "", errors.New("digest: doc-context returned empty output")
	}
	if err := writeProvenancedResponse(opts, "document context", headerPath, "doc-context", prompt, upstream, route, []byte(header)); err != nil {
		return "", err
	}
	return header, nil
}

func researchCacheText(headerBlock, chunkText string) string {
	if headerBlock == "" {
		return chunkText
	}
	return headerBlock + "\n" + chunkText
}

func headingSkeleton(source string, maxLines int) string {
	if maxLines < 1 {
		return ""
	}
	var out []string
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if !headingLineRe.MatchString(trimmed) {
			continue
		}
		out = append(out, trimmed)
		if len(out) >= maxLines {
			break
		}
	}
	return strings.Join(out, "\n")
}

func compileFacts(outs []string, failedFlag, reusedFlag []bool) (string, []string, int) {
	var b strings.Builder
	var failed []string
	reused := 0
	seen := make(map[string]struct{})
	for i := range outs {
		id := fmt.Sprintf("chunk-%03d", i+1)
		if failedFlag[i] {
			failed = append(failed, id)
			continue
		}
		if reusedFlag[i] {
			reused++
		}
		if b.Len() > 0 {
			b.WriteString(factSeparator)
		}
		fmt.Fprintf(&b, "## %s\n\n%s", id, dedupeFacts(outs[i], seen))
	}
	return b.String(), failed, reused
}

func dedupeFacts(facts string, seen map[string]struct{}) string {
	lines := strings.Split(facts, "\n")
	var kept []string
	var unit []string
	unitKey := ""
	inUnit := false

	flush := func() {
		if !inUnit {
			return
		}
		if _, dup := seen[unitKey]; !dup {
			if nearDuplicateFact(unitKey, seen) {
				kept = append(kept, nearDuplicateFactFlag)
			}
			seen[unitKey] = struct{}{}
			kept = append(kept, unit...)
		}
		unit = nil
		inUnit = false
	}

	for _, line := range lines {
		if bulletRe.MatchString(line) {
			flush()
			unit = []string{line}
			unitKey = normalizeFactKey(line)
			inUnit = true
			continue
		}
		if inUnit && strings.TrimSpace(line) != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			unit = append(unit, line)
			continue
		}
		flush()
		key := normalizeFactKey(line)
		if key == "" {
			kept = append(kept, line)
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		if nearDuplicateFact(key, seen) {
			kept = append(kept, nearDuplicateFactFlag)
		}
		seen[key] = struct{}{}
		kept = append(kept, line)
	}
	flush()
	return strings.Join(kept, "\n")
}

func normalizeFactKey(fact string) string {
	collapsed := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(fact))), " ")
	return strings.TrimSpace(strings.TrimFunc(collapsed, unicode.IsPunct))
}

func nearDuplicateFact(key string, seen map[string]struct{}) bool {
	tokens := factTokens(key)
	if len(tokens) < 5 {
		return false
	}
	fingerprint := simHash(tokens)
	for prior := range seen {
		priorTokens := factTokens(prior)
		if len(priorTokens) < 5 {
			continue
		}
		if tokenOverlap(tokens, priorTokens) < 0.72 {
			continue
		}
		if bits.OnesCount64(fingerprint^simHash(priorTokens)) <= 10 {
			return true
		}
	}
	return false
}

func factTokens(key string) []string {
	fields := strings.Fields(key)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.TrimFunc(field, unicode.IsPunct)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func simHash(tokens []string) uint64 {
	var weights [64]int
	for _, token := range tokens {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		sum := h.Sum64()
		for i := range weights {
			if sum&(uint64(1)<<i) != 0 {
				weights[i]++
			} else {
				weights[i]--
			}
		}
	}
	var out uint64
	for i, weight := range weights {
		if weight > 0 {
			out |= uint64(1) << i
		}
	}
	return out
}

func tokenOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(a))
	for _, token := range a {
		seen[token] = struct{}{}
	}
	matches := 0
	for _, token := range b {
		if _, ok := seen[token]; ok {
			matches++
		}
	}
	smaller := len(a)
	if len(b) < smaller {
		smaller = len(b)
	}
	return float64(matches) / float64(smaller)
}

// ChunkSource returns the digest research chunks for source without making any
// LLM calls. It is used by both the real pipeline and CLI dry-run planning.
func ChunkSource(source string, chunkSize, maxTokens int) ([]chunking.Chunk, error) {
	c, err := chunking.NewChunker(chunking.HeadingAware)
	if err != nil {
		return nil, fmt.Errorf("digest: creating chunker: %w", err)
	}
	// Apply the cl100k_base preflight budget to dense chunks (CJK, code, base64).
	// It is not the provider's tokenizer or a context-overflow guarantee. With no
	// estimate configured, fall back to the plain character chunker.
	if maxTokens > 0 {
		tc, terr := tokenizer.NewTokenCounter(maxTokens)
		if terr != nil {
			return nil, fmt.Errorf("digest: creating token counter: %w", terr)
		}
		return chunking.ChunkWithTokenLimit(c, source, chunkSize, 0, tc), nil
	}
	return c.Chunk(source, chunkSize, 0), nil
}

// section is one planned unit of the article: a heading title and a one-line
// statement of what that section should cover.
type section struct {
	title   string
	intent  string
	factIDs []int
}

// parseOutline extracts the article title (first "# " line) and the ordered
// sections from the outline model's output: each "## " heading starts a section
// whose intent is the text up to the next heading. Preamble before the first
// "## " heading and stray code-fence lines are ignored; sections with an empty
// title are dropped.
func parseOutline(s string) (title string, secs []section) {
	var cur *section
	flush := func() {
		if cur != nil {
			cur.intent = strings.TrimSpace(cur.intent)
			secs = append(secs, *cur)
			cur = nil
		}
	}
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "## "):
			flush()
			cur = &section{title: strings.TrimSpace(t[3:])}
		case strings.HasPrefix(t, "# "):
			if title == "" {
				title = strings.TrimSpace(t[2:])
			}
		case cur != nil && factsLineRe.MatchString(t):
			for _, m := range factIDRe.FindAllStringSubmatch(t, -1) {
				if n, err := strconv.Atoi(m[1]); err == nil {
					cur.factIDs = append(cur.factIDs, n)
				}
			}
		case cur != nil && t != "":
			cur.intent += " " + t
		}
	}
	flush()
	out := secs[:0]
	for _, sec := range secs {
		if sec.title != "" {
			out = append(out, sec)
		}
	}
	return title, out
}

// mergeDuplicateSections merges sections whose titles are identical after
// normalization (lowercase, trim space, strip non-alphanumerics, collapse
// whitespace). The first occurrence keeps its position and original title;
// later duplicates' fact IDs are appended in order and the duplicates are
// removed from the slice. Order of surviving sections is otherwise preserved.
func mergeDuplicateSections(ctx context.Context, secs []section) []section {
	seen := make(map[string]int)   // normalized -> index in out
	counts := make(map[string]int) // normalized -> merge count
	out := make([]section, 0, len(secs))
	for _, s := range secs {
		norm := normalizeTitle(s.title)
		if idx, ok := seen[norm]; ok {
			out[idx].factIDs = append(out[idx].factIDs, s.factIDs...)
			counts[norm]++
		} else {
			seen[norm] = len(out)
			out = append(out, s)
		}
	}
	for norm, n := range counts {
		slog.DebugContext(ctx, "digest merged duplicate outline section", "section", out[seen[norm]].title, "merged", n)
	}
	return out
}

// normalizeTitle returns a canonical form of the title for duplicate detection:
// lowercase, trim space, strip all non-alphanumeric characters, collapse
// whitespace to single spaces.
func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// factUnit is one atomic fact from the compiled research notes, with a stable
// 1-based ID used to route facts to outline sections.
type factUnit struct {
	id   int
	line string // the full bullet (plus any indented continuation lines)
}

var (
	bulletRe      = regexp.MustCompile(`^\s*[-*]\s`)
	factsLineRe   = regexp.MustCompile(`(?i)^facts?:`)
	factIDRe      = regexp.MustCompile(`[Ff](\d+)`)
	headingLineRe = regexp.MustCompile(`^#{1,6}\s+\S`)
)

// numberFacts splits the compiled facts into atomic units (one per bullet) and
// returns both the units and a copy of the text with each bullet prefixed by its
// "[F<id>] " tag. Chunk headings, separators, and blank lines are untouched;
// indented continuation lines attach to the preceding bullet's unit.
func numberFacts(compiled string) (units []factUnit, numbered string) {
	lines := strings.Split(compiled, "\n")
	out := make([]string, len(lines))
	id := 0
	cur := -1
	for i, ln := range lines {
		if loc := bulletRe.FindStringIndex(ln); loc != nil {
			id++
			units = append(units, factUnit{id: id, line: ln})
			cur = len(units) - 1
			out[i] = ln[:loc[1]] + fmt.Sprintf("[F%d] ", id) + ln[loc[1]:]
			continue
		}
		out[i] = ln
		if cur >= 0 && strings.TrimSpace(ln) != "" && (strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t")) {
			units[cur].line += "\n" + ln
			continue
		}
		cur = -1
	}
	return units, strings.Join(out, "\n")
}

// routeSectionFacts filters ids to exclude any already in assigned (first-home-wins),
// marking kept IDs as assigned. Returns the filtered list and the count of dropped
// duplicates.
func routeSectionFacts(assigned map[int]bool, ids []int) ([]int, int) {
	filtered := make([]int, 0, len(ids))
	dropped := 0
	for _, id := range ids {
		if assigned[id] {
			dropped++
			continue
		}
		assigned[id] = true
		filtered = append(filtered, id)
	}
	return filtered, dropped
}

// selectFacts returns the bullet lines for the given fact IDs in ID order,
// joined as a clean list (the "[F#] " routing tags are not re-emitted).
func selectFacts(units []factUnit, ids []int) string {
	byID := make(map[int]string, len(units))
	for _, u := range units {
		byID[u.id] = u.line
	}
	var b strings.Builder
	for _, id := range ids {
		if line, ok := byID[id]; ok {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(line)
		}
	}
	return b.String()
}

// selectFactsTagged returns the selected fact bullets with their stable [F#]
// prefixes preserved for citation-grounded generation.
func selectFactsTagged(units []factUnit, ids []int) string {
	byID := make(map[int]factUnit, len(units))
	for _, u := range units {
		byID[u.id] = u
	}
	var b strings.Builder
	for _, id := range ids {
		if u, ok := byID[id]; ok {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(tagFactLine(u.line, u.id))
		}
	}
	return b.String()
}

func tagFactLine(line string, id int) string {
	if loc := bulletRe.FindStringIndex(line); loc != nil {
		return line[:loc[1]] + fmt.Sprintf("[F%d] ", id) + line[loc[1]:]
	}
	return fmt.Sprintf("[F%d] %s", id, line)
}

// assembleArticle joins the title and sections into one Markdown document: an
// optional "# title" line, then each section as "## <title>" followed by its body.
func assembleArticle(title string, secs []section, bodies []string) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	for i, sec := range secs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## %s\n\n%s", sec.title, strings.TrimSpace(bodies[i]))
	}
	return b.String()
}
