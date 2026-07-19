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
	"golang.org/x/sync/errgroup"
)

// Completer runs a single LLM completion. *ai.Client satisfies it.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

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
	// Usage, when set, returns the provider's cumulative token counts (prompt,
	// cached, output) at call time. The ledger snapshots it around each serial
	// stage to record per-stage token deltas; the concurrent research stage is
	// accounted as one phase-level event (per-call deltas would double-count
	// across overlapping calls). Nil disables token accounting (deltas stay 0).
	Usage func() (prompt, cached, output int64)
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
	Time         time.Time `json:"time"`
	Stage        string    `json:"stage"`
	Name         string    `json:"name,omitempty"`
	Action       string    `json:"action"`
	Duration     string    `json:"duration,omitempty"`
	Error        string    `json:"error,omitempty"`
	PromptTokens int64     `json:"prompt_tokens,omitempty"`
	CachedTokens int64     `json:"cached_tokens,omitempty"`
	OutputTokens int64     `json:"output_tokens,omitempty"`
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
}

func newRunLedger(path string, usage func() (prompt, cached, output int64)) (*runLedger, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("digest: creating ledger dir: %w", err)
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
	ev := ledgerEvent{Time: time.Now().UTC(), Stage: stage, Name: name, Action: action}
	if !started.IsZero() {
		ev.Duration = time.Since(started).Round(time.Millisecond).String()
	}
	if err != nil {
		ev.Error = err.Error()
	}
	return ev
}

func (l *runLedger) writeEvent(ev ledgerEvent) {
	data, jerr := json.Marshal(ev)
	if jerr != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ferr := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if ferr != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()
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
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryBackoff(a, base)):
			}
		}
	}
	return "", lastErr
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

// Run executes the pipeline against source. Per-chunk extraction that returns an
// empty response is recorded in Result.FailedChunks rather than aborting; a hard
// LLM error fails the whole run fast. The run also fails if no facts could be
// extracted at all.
func Run(ctx context.Context, rc RoleCompleters, p *prompts.Set, source string, opts Options) (*Result, error) {
	if opts.CacheRead && opts.Cache != nil && opts.CacheKey != "" {
		if article, meta, ok := opts.Cache.Load(opts.CacheKey); ok {
			if err := fsutil.WriteFile(opts.OutPath, []byte(article), 0o644); err != nil {
				return nil, fmt.Errorf("digest: writing cached output: %w", err)
			}
			ledgerPath := opts.LedgerPath
			if ledgerPath == "" && opts.ArtifactDir != "" {
				ledgerPath = filepath.Join(opts.ArtifactDir, "run-ledger.jsonl")
			}
			slog.InfoContext(ctx, "digest cache hit (skipped research/fuse/outline/section/edit stages)", "out", opts.OutPath)
			return &Result{OutPath: opts.OutPath, FactsPath: opts.FactsPath, LedgerPath: ledgerPath, CacheHit: true, Coverage: meta.Coverage, Citations: meta.Citations, Precision: meta.Precision, Words: meta.Words}, nil
		}
	}
	if opts.LedgerPath == "" && opts.ArtifactDir != "" {
		opts.LedgerPath = filepath.Join(opts.ArtifactDir, "run-ledger.jsonl")
	}
	ledger, err := newRunLedger(opts.LedgerPath, opts.Usage)
	if err != nil {
		return nil, err
	}
	// Bind artifact reuse to the current source: on a missing or mismatched
	// marker, stale artifacts are invalidated and all resume reuse is disabled
	// for this run (opts is a value copy, so clearing Resume gates every reuse
	// site — facts, research responses, outline, sections, edits).
	reuseOK, err := ensureArtifactBinding(ctx, opts, source)
	if err != nil {
		return nil, err
	}
	if !reuseOK {
		if opts.Resume {
			ledger.RecordZeroDelta("artifacts", "", "invalidate", time.Time{}, nil)
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
		return nil, err
	}
	factsAppendix := facts // pre-fuse snapshot for the lossless appendix
	coverageBase := facts

	// fuse: merge the per-chunk research notes into one organized set (opt-in).
	if opts.Fuse {
		slog.InfoContext(ctx, "digest fuse start")
		started := time.Now()
		beforeUsage := ledger.usageNow()
		fused, ferr := complete(ctx, rc.Fuse, p.RenderFuse(facts), opts.Timeout)
		ledger.Record("fuse", "", "call", started, ferr, beforeUsage)
		if ferr != nil {
			return nil, fmt.Errorf("digest: fuse: %w", ferr)
		}
		if strings.TrimSpace(fused) == "" {
			return nil, errors.New("digest: fuse returned empty output")
		}
		facts = strings.TrimSpace(fused)
		_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "facts.fused.md"), []byte(facts), 0o644)
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
	if opts.Resume {
		if data, ok := readReusableArtifact(outlinePath, "outline"); ok {
			outlineText = data
			res.ReusedOutline = true
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
			outlineText, err = retryComplete(ctx, "outline", rc.Outline, ctxBlock+p.RenderOutline(opts.Style, numbered), opts.Timeout, retries, backoff)
			ledger.Record("outline", "", "call", started, err, beforeUsage)
			if err != nil {
				return nil, fmt.Errorf("digest: outline: %w", err)
			}
		}
		_ = fsutil.WriteFile(outlinePath, []byte(strings.TrimSpace(outlineText)), 0o644)
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
			secs = append(secs, section{title: "Additional details", intent: "Remaining facts not otherwise covered above.", factIDs: orphans})
			if opts.Cite {
				sectionFacts = append(sectionFacts, selectFactsTagged(units, orphans))
			} else {
				sectionFacts = append(sectionFacts, selectFacts(units, orphans))
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

	// expand: write each section in order. Each call sees its section's facts, the
	// full outline, and the sections already written, but emits only one section — so
	// no single call must output the whole article (which causes verbatim echo).
	bodies := make([]string, len(secs))
	for i, s := range secs {
		sectionPath := filepath.Join(opts.ArtifactDir, "responses", fmt.Sprintf("section-%03d.md", i+1))
		if opts.Resume {
			if data, ok := readReusableArtifact(sectionPath, "section"); ok {
				bodies[i] = data
				res.ReusedSections++
				ledger.RecordZeroDelta("section", s.title, "reuse", time.Time{}, nil)
				slog.InfoContext(ctx, "digest section done", "section", s.title, "n", i+1, "total", len(secs))
				continue
			}
		}
		prior := assembleArticle(title, secs[:i], bodies[:i])
		stage := fmt.Sprintf("section %d/%d: %s", i+1, len(secs), s.title)
		slog.InfoContext(ctx, "digest section start", "section", s.title, "n", i+1, "total", len(secs))
		started := time.Now()
		beforeUsage := ledger.usageNow()
		body, serr := retryComplete(ctx, stage, rc.Section, ctxBlock+citeSectionBlock+p.RenderSection(opts.Style, outlineText, sectionFacts[i], prior, s.title, s.intent), opts.Timeout, retries, backoff)
		ledger.Record("section", s.title, "call", started, serr, beforeUsage)
		if serr != nil {
			if cerr := cancellationErr(ctx, serr); cerr != nil {
				return nil, fmt.Errorf("digest: section %q: %w", s.title, cerr)
			}
			if ai.IsSystemic(serr) {
				return nil, fmt.Errorf("digest: section %q: %w", s.title, serr)
			}
			// Graceful degradation: a non-systemic failure after retries leaves a
			// placeholder so the rest of the article still assembles; the gap is
			// recorded for the caller to surface.
			slog.WarnContext(ctx, "digest: section failed after retries, degrading", "section", s.title, "err", serr)
			res.FailedSections = append(res.FailedSections, s.title)
			bodies[i] = "_(this section could not be generated)_"
			continue
		}
		bodies[i] = body
		_ = fsutil.WriteFile(sectionPath, []byte(bodies[i]), 0o644)
		slog.InfoContext(ctx, "digest section done", "section", s.title, "n", i+1, "total", len(secs))
	}
	draft := assembleArticle(title, secs, bodies)
	_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "draft.md"), []byte(draft), 0o644)
	slog.InfoContext(ctx, "digest draft done", "sections", len(secs))

	// edit (opt-in): rewrite one section at a time against the STABLE full draft as
	// read-only context — it is identical across every edit call, so the provider
	// prompt cache covers it. Full global awareness, bounded output — the editor
	// cannot echo the whole article because it may emit only one section.
	final := draft
	if opts.Edit {
		for i, s := range secs {
			editPath := filepath.Join(opts.ArtifactDir, "responses", fmt.Sprintf("section-%03d.edited.md", i+1))
			if opts.Resume {
				if data, ok := readReusableArtifact(editPath, "edit"); ok {
					bodies[i] = data
					res.ReusedEdits++
					ledger.RecordZeroDelta("edit", s.title, "reuse", time.Time{}, nil)
					slog.InfoContext(ctx, "digest edit done", "section", s.title, "n", i+1, "total", len(secs))
					continue
				}
			}
			stage := fmt.Sprintf("edit section %d/%d: %s", i+1, len(secs), s.title)
			slog.InfoContext(ctx, "digest edit start", "section", s.title, "n", i+1, "total", len(secs))
			started := time.Now()
			beforeUsage := ledger.usageNow()
			editFacts := facts
			if opts.Cite {
				editFacts = numbered
			}
			priorEdited := assembleArticle(title, secs[:i], bodies[:i])
			edited, eerr := retryComplete(ctx, stage, rc.Edit, ctxBlock+citeEditBlock+p.RenderEditSection(opts.Style, draft, priorEdited, editFacts, s.title), opts.Timeout, retries, backoff)
			ledger.Record("edit", s.title, "call", started, eerr, beforeUsage)
			if eerr != nil {
				if cerr := cancellationErr(ctx, eerr); cerr != nil {
					return nil, fmt.Errorf("digest: edit section %q: %w", s.title, cerr)
				}
				if ai.IsSystemic(eerr) {
					return nil, fmt.Errorf("digest: edit section %q: %w", s.title, eerr)
				}
				// Recovery: keep the un-edited draft body for this section (already
				// in bodies[i]); a failed polish must not discard a written section.
				slog.WarnContext(ctx, "digest: edit failed after retries, keeping draft", "section", s.title, "err", eerr)
				res.FailedEdits = append(res.FailedEdits, s.title)
				continue
			}
			bodies[i] = edited
			_ = fsutil.WriteFile(editPath, []byte(bodies[i]), 0o644)
			slog.InfoContext(ctx, "digest edit done", "section", s.title, "n", i+1, "total", len(secs))
		}
		final = assembleArticle(title, secs, bodies)
	}

	citedForPrecision := ""
	if opts.Cite {
		cited := final
		_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"), []byte(cited), 0o644)
		citations := computeCitations(units, cited)
		res.Citations = &citations
		if data, jerr := json.MarshalIndent(citations, "", "  "); jerr == nil {
			_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "citations.json"), data, 0o644)
		}
		if citations.Total > 0 && len(citeGroupRe.FindAllString(cited, -1)) == 0 {
			slog.WarnContext(ctx, "digest citation check found no markers; model ignored citation instructions", "total", citations.Total)
		} else if opts.Repair && len(citations.MissingIDs) > 0 {
			cited, citations = repairMissingCited(ctx, rc.Edit, p, units, cited, citations, ledger, opts.Timeout, retries, backoff)
			res.Citations = &citations
			_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"), []byte(cited), 0o644)
			if data, jerr := json.MarshalIndent(citations, "", "  "); jerr == nil {
				_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "citations.json"), data, 0o644)
			}
		}
		citedForPrecision = cited
		final = stripCiteMarkers(cited)
	}

	// Deterministic, offline fact-coverage self-check: how many specifics
	// (numbers, dates, names) from the research facts survive into the article.
	// Computed on the pre-appendix article so the appendix cannot inflate it.
	res.Coverage = extractscore.SpecificsCoverage(coverageBase, final)
	if data, jerr := json.MarshalIndent(res.Coverage, "", "  "); jerr == nil {
		_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "coverage.json"), data, 0o644)
	}
	slog.InfoContext(ctx, "digest fact-coverage", "covered", res.Coverage.Covered, "total", res.Coverage.Total, "dropped", len(res.Coverage.Missing))

	// verify→repair: if the user opted in and any specific was dropped, run one
	// bounded reinsert pass against the edit-role model, then recompute coverage.
	// Best-of by Covered — repair can only raise coverage, never lower it.
	if opts.Repair && !opts.Cite && len(res.Coverage.Missing) > 0 {
		final, res.Coverage = repairMissing(ctx, rc.Edit, p, coverageBase, final, res.Coverage, ledger, opts.Timeout, retries, backoff)
		if data, jerr := json.MarshalIndent(res.Coverage, "", "  "); jerr == nil {
			_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "coverage.json"), data, 0o644)
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
			precision, perr := checkPrecision(ctx, rc.Judge, p, facts, sentences, opts.PrecisionBatchSize, ledger, opts.Timeout, retries, backoff)
			if perr != nil {
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
					repaired, repairedCov, rerr := repairPrecision(ctx, rc.Judge, p, facts, precision.Unsupported, repairArticle, coverageBase, res.Coverage, ledger, opts.Timeout, retries, backoff)
					if rerr == nil {
						repairSentences := extractscore.SplitSentences(repaired)
						if opts.Cite {
							repairSentences = citedSentencesForPrecision(repaired)
						}
						repairedPrecision, rperr := checkPrecision(ctx, rc.Judge, p, facts, repairSentences, opts.PrecisionBatchSize, ledger, opts.Timeout, retries, backoff)
						if rperr != nil {
							if opts.RequirePrecision {
								return nil, rperr
							}
							slog.WarnContext(ctx, "digest precision re-check failed, continuing with pre-repair precision", "err", rperr)
						} else {
							final = repaired
							res.Coverage = repairedCov
							if data, jerr := json.MarshalIndent(repairedCov, "", "  "); jerr == nil {
								_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "coverage.json"), data, 0o644)
							}
							if opts.Cite {
								repairedCitations := computeCitations(units, repaired)
								res.Citations = &repairedCitations
								citedForPrecision = repaired
								final = stripCiteMarkers(repaired)
								_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "rewrite.cited.md"), []byte(citedForPrecision), 0o644)
								if data, jerr := json.MarshalIndent(repairedCitations, "", "  "); jerr == nil {
									_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "citations.json"), data, 0o644)
								}
							}
							precision = repairedPrecision
							slog.InfoContext(ctx, "digest precision repaired", "unsupported_pre", preUnsupported, "unsupported_post", len(precision.Unsupported))
						}
					}
				}
				res.Precision = &precision
				if data, jerr := json.MarshalIndent(precision, "", "  "); jerr == nil {
					_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "precision.json"), data, 0o644)
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

	if err := fsutil.WriteFile(opts.OutPath, []byte(final), 0o644); err != nil {
		return nil, fmt.Errorf("digest: writing output: %w", err)
	}
	_ = fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "rewrite.md"), []byte(final), 0o644)
	if shouldStoreArticle(opts, res) {
		opts.Cache.Store(opts.CacheKey, final, CacheMeta{Coverage: res.Coverage, Citations: res.Citations, Precision: res.Precision, Words: res.Words})
	}
	return res, nil
}

// shouldStoreArticle reports whether a finished run's article may enter the
// output cache: never on partial failures, never when facts came from an
// unverified checkpoint (the key would not describe the actual inputs), and
// only if the caller's StoreOK gate (when set) passes.
func shouldStoreArticle(opts Options, res *Result) bool {
	if opts.Cache == nil || opts.CacheKey == "" {
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
	if stage == "outline" {
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
	if in.opts.ReuseFacts || (in.opts.Resume && within) {
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
	if err := fsutil.WriteFile(in.opts.FactsPath, []byte(ex.facts), 0o644); err != nil {
		return "", fmt.Errorf("digest: writing compiled facts: %w", err)
	}
	return ex.facts, nil
}

// extractAndCompile chunks source, extracts facts per chunk with bounded
// parallelism, and assembles the compiled-facts document in chunk order. A hard
// LLM error on any chunk cancels the group and aborts; an empty response is a soft
// skip recorded by id. Chunk artifacts are written sequentially up front so an IO
// error surfaces before any LLM work.
func researchAndCompile(ctx context.Context, llm, escalation Completer, p *prompts.Set, source string, opts Options, ledger *runLedger) (extraction, error) {
	chunks, err := ChunkSource(source, opts.ChunkSize, opts.MaxTokens)
	if err != nil {
		return extraction{}, err
	}
	total := len(chunks)
	slog.InfoContext(ctx, "digest chunking done", "chunks", total)

	for i, chunk := range chunks {
		id := fmt.Sprintf("chunk-%03d", i+1)
		if werr := fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "chunks", id+".md"), []byte(chunk.Text), 0o644); werr != nil {
			return extraction{}, fmt.Errorf("digest: writing chunk %s: %w", id, werr)
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

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	phaseStart := time.Now()
	phaseBefore := ledger.usageNow()
	for i, chunk := range chunks {
		id := fmt.Sprintf("chunk-%03d", i+1)
		g.Go(func() error {
			responsePath := filepath.Join(opts.ArtifactDir, "responses", id+".md")
			if opts.Resume {
				if data, ok := readReusableArtifact(responsePath, "research"); ok {
					outs[i] = strings.TrimSpace(data)
					reusedFlag[i] = true
					ledger.RecordZeroDelta("research", id, "reuse", time.Time{}, nil)
					slog.InfoContext(gctx, "digest research done", "chunk", id, "n", i+1, "total", total)
					return nil
				}
			}
			if opts.ResearchCache != nil {
				cacheText := researchCacheText(headerBlock, chunk.Text)
				if data, ok := opts.ResearchCache.Load(cacheText); ok {
					outs[i] = strings.TrimSpace(data)
					reusedFlag[i] = true
					_ = fsutil.WriteFile(responsePath, []byte(outs[i]), 0o644)
					ledger.RecordZeroDelta("research", id, "cache", time.Time{}, nil)
					slog.InfoContext(gctx, "digest research cache hit", "chunk", id, "n", i+1, "total", total)
					return nil
				}
			}
			slog.InfoContext(gctx, "digest research", "chunk", id, "n", i+1, "total", total)
			started := time.Now()
			out, cerr := complete(gctx, llm, headerBlock+p.RenderResearch(id, chunk.Text), opts.Timeout)
			// Tokens for concurrent research calls are accounted once at phase
			// scope (overlapping per-call windows would double-count).
			ledger.RecordZeroDelta("research", id, "call", started, cerr)
			if cerr != nil {
				// An empty model response is a soft skip (recorded in failed),
				// not a run-ending error: one chunk the model declines to
				// answer must not abort the whole digest. Any other error
				// (timeout, transport, cancellation) still fails fast.
				if errors.Is(cerr, ai.ErrEmptyResponse) {
					failedFlag[i] = true
					return nil
				}
				return fmt.Errorf("digest: research %s: %w", id, cerr)
			}
			if strings.TrimSpace(out) == "" {
				failedFlag[i] = true
				return nil
			}
			out = maybeEscalateResearch(gctx, escalation, p, headerBlock, id, chunk.Text, out, opts, ledger)
			_ = fsutil.WriteFile(responsePath, []byte(out), 0o644)
			if opts.ResearchCache != nil {
				opts.ResearchCache.Store(researchCacheText(headerBlock, chunk.Text), out)
			}
			outs[i] = strings.TrimSpace(out)
			slog.InfoContext(gctx, "digest research done", "chunk", id, "n", i+1, "total", total)
			return nil
		})
	}
	werr := g.Wait()
	ledger.Record("research", "", "phase", phaseStart, werr, phaseBefore)
	if werr != nil {
		return extraction{}, werr
	}

	facts, failed, reused := compileFacts(outs, failedFlag, reusedFlag)
	slog.InfoContext(ctx, "digest research complete", "chunks", total, "failed", len(failed))
	return extraction{facts: facts, count: total, failed: failed, reused: reused}, nil
}

func maybeEscalateResearch(ctx context.Context, llm Completer, p *prompts.Set, headerBlock, id, chunkText, baseOut string, opts Options, ledger *runLedger) string {
	baseOut = strings.TrimSpace(baseOut)
	if !opts.Cascade || opts.CascadeThreshold <= 0 || llm == nil {
		return baseOut
	}
	cov := extractscore.SpecificsCoverage(chunkText, baseOut)
	if cov.Total < 5 || float64(cov.Covered)/float64(cov.Total) >= opts.CascadeThreshold {
		return baseOut
	}
	started := time.Now()
	out, err := complete(ctx, llm, headerBlock+p.RenderResearch(id, chunkText), opts.Timeout)
	if ledger != nil {
		ledger.RecordZeroDelta("research", id, "escalate", started, err)
	}
	if err != nil {
		slog.WarnContext(ctx, "digest research escalation skipped", "chunk", id, "err", err)
		return baseOut
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return baseOut
	}
	return baseOut + "\n" + out
}

func resolveDocHeader(ctx context.Context, llm Completer, p *prompts.Set, source, excerpt string, opts Options, ledger *runLedger, retries int, backoff time.Duration) (string, error) {
	headerPath := filepath.Join(opts.ArtifactDir, "responses", "doc-context.md")
	if opts.Resume {
		if data, ok := readReusableArtifact(headerPath, "doc-context"); ok {
			ledger.RecordZeroDelta("doc-context", "", "reuse", time.Time{}, nil)
			return strings.TrimSpace(data), nil
		}
	}
	started := time.Now()
	beforeUsage := ledger.usageNow()
	header, err := retryComplete(ctx, "doc-context", llm, p.RenderDocContext(excerpt, headingSkeleton(source, 100)), opts.Timeout, retries, backoff)
	ledger.Record("doc-context", "", "call", started, err, beforeUsage)
	if err != nil {
		return "", fmt.Errorf("digest: doc-context: %w", err)
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return "", errors.New("digest: doc-context returned empty output")
	}
	_ = fsutil.WriteFile(headerPath, []byte(header), 0o644)
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
