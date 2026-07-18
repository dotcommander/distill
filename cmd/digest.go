package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/actions/digest"
	"github.com/dotcommander/distill/internal/ai"
	"github.com/dotcommander/distill/internal/config"
	"github.com/dotcommander/distill/internal/digestcache"
	"github.com/dotcommander/distill/internal/extractscore"
	"github.com/dotcommander/distill/internal/prompts"
	"github.com/dotcommander/distill/internal/researchcache"
)

// digestFlags holds the resolved --digest flag values for one invocation,
// closure-scoped in newDigestCmd (no package globals).
type digestFlags struct {
	style               string
	out                 string
	facts               string
	artifacts           string
	model               string
	baseURL             string
	chunkSize           int
	maxTokens           int
	concurrency         int
	timeout             int
	retries             int
	maxCalls            int
	reuseFacts          bool
	noClean             bool
	forceClean          bool
	fuse                bool
	noEdit              bool
	appendix            bool
	resume              bool
	dryRun              bool
	local               bool
	deepseek            bool
	noCache             bool
	researchCache       bool
	context             string
	contextFile         string
	repair              bool
	docContext          bool
	cite                bool
	cascade             bool
	mergeFacts          bool
	outlineFromClusters bool
	targetFacts         int
	checkPrecision      bool
	minCoverage         float64
	minCited            float64
	minPrecision        float64
	cascadeThreshold    float64
	mergeThreshold      float64
	minWords            int
	maxWords            int
	maxSections         int
}

func runDigest(cmd *runContext, args []string, f *digestFlags) error {
	start := time.Now()

	if f.chunkSize < 1000 {
		return errors.New("--chunk-size must be >= 1000")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	profile, err := profileFromFlags(f.local, f.deepseek)
	if err != nil {
		return err
	}
	preflightMaxTokens := effectivePreflightMaxTokens(f.local, f.maxTokens)
	effModel, _ := cfg.EffectiveProfile(profile)
	model := firstNonEmpty(f.model, os.Getenv("DISTILL_MODEL"), effModel)
	if model == "" {
		return errors.New("digest requires a model (--model, $DISTILL_MODEL, or config)")
	}
	timeoutSec := firstPositive(f.timeout, cfg.RequestTimeoutSeconds, 300)
	concurrency := firstPositive(f.concurrency, cfg.ExtractConcurrency, 4)
	retries := firstPositive(f.retries, cfg.RequestRetries, 3)
	checkPrecision := f.checkPrecision || f.minPrecision > 0
	if f.outlineFromClusters && !f.mergeFacts {
		return errors.New("--outline-from-clusters requires --merge-facts")
	}
	if f.targetFacts < 0 {
		return errors.New("--target-facts must be >= 0")
	}
	cascadeThreshold := f.cascadeThreshold
	if cascadeThreshold == 0 {
		cascadeThreshold = cfg.CascadeMinCapture
	}
	f.cascadeThreshold = cascadeThreshold
	cascadeEnabled := f.cascade && cascadeThreshold > 0
	mergeThreshold := f.mergeThreshold
	if mergeThreshold == 0 {
		mergeThreshold = cfg.MergeFactsThreshold
	}
	f.mergeThreshold = mergeThreshold
	maxSections := f.maxSections
	if maxSections < 0 {
		maxSections = cfg.MaxSections
	}
	if maxSections < 0 {
		maxSections = 0
	}

	input, err := readDigestInput(cmd.in, args)
	if err != nil {
		return err
	}
	filePath := input.Source
	text := maybeCleanTranscript(cmd.Context(), maybeStripBinary(cmd.Context(), normalizeInput(input.Text)), f.forceClean, f.noClean)
	steerContext, err := resolveDigestContext(f.context, f.contextFile)
	if err != nil {
		return err
	}
	// Stable provider-cache key for piped input: hash the content (the literal
	// path "-" is meaningless), else keep the file path behavior unchanged.
	sourceID := filePath
	if input.Stdin {
		sum := sha256.Sum256([]byte(text))
		sourceID = "stdin:" + hex.EncodeToString(sum[:12])
	} else if input.Multi {
		sum := sha256.Sum256([]byte(text))
		sourceID = "pathspec:" + hex.EncodeToString(sum[:12])
	}

	artifactDir, err := resolveArtifactDir(f.artifacts)
	if err != nil {
		return err
	}
	factsPath := f.facts
	if factsPath == "" {
		factsPath = filepath.Join(artifactDir, "facts.compiled.md")
	}
	outPath := f.out
	if outPath == "" {
		if input.Stdin {
			outPath = "stdin.distilled.md"
		} else if input.Multi {
			outPath = "combined.distilled.md"
		} else {
			base := filepath.Base(filePath)
			outPath = strings.TrimSuffix(base, filepath.Ext(base)) + ".distilled.md"
		}
	}

	style := f.style
	if style == "" {
		style = cfg.Style
	}
	if style == "" {
		style = "flowing, well-connected prose that reads like a thoughtful written explainer"
	}
	if expanded, ok := cfg.Styles[style]; ok {
		style = expanded
	}
	embeddingModel := ""
	if f.mergeFacts || f.targetFacts > 0 {
		effEmbeddingModel, _ := cfg.EffectiveEmbedding(f.local)
		embeddingModel = firstNonEmpty(os.Getenv("DISTILL_EMBEDDING_MODEL"), effEmbeddingModel)
	}

	// Per-role models: an explicit --model / $DISTILL_MODEL overrides every stage;
	// otherwise each stage resolves via config (EffectiveRole), falling back to model.
	explicit := firstNonEmpty(f.model, os.Getenv("DISTILL_MODEL"))
	roleModel := func(role string) string {
		if explicit != "" {
			return explicit
		}
		return cfg.EffectiveRoleProfile(role, profile)
	}
	researchEscalationModel := ""
	if cascadeEnabled {
		researchEscalationModel = explicit
		if researchEscalationModel == "" {
			researchEscalationModel = cfg.ResearchEscalationModel
		}
		if researchEscalationModel == "" {
			return errors.New("--cascade requires research_escalation_model in config.yaml, or an explicit --model / $DISTILL_MODEL")
		}
	}
	if f.dryRun {
		return printDigestDryRun(cmd.ErrOrStderr(), cfg, profile, f, filePath, outPath, factsPath, artifactDir, style, text, roleModel, researchEscalationModel)
	}
	if f.maxCalls > 0 {
		chunks, cerr := digest.ChunkSource(text, f.chunkSize, preflightMaxTokens)
		if cerr != nil {
			return cerr
		}
		plannedCalls := plannedDigestCalls(len(chunks), f, factsPath, artifactDir, digest.ArtifactsMatchSource(artifactDir, text, f.chunkSize, preflightMaxTokens))
		if plannedCalls > f.maxCalls {
			return fmt.Errorf("digest planned %d provider calls, exceeds --max-calls %d (use --dry-run, --resume, --reuse-facts, or a higher ceiling)", plannedCalls, f.maxCalls)
		}
	}

	progress := newDigestProgress(cmd.ErrOrStderr())
	previousLogger := slog.Default()
	progressActive := true
	slog.SetDefault(slog.New(progress))
	progress.Start()
	defer func() {
		if progressActive {
			progress.Stop()
			slog.SetDefault(previousLogger)
		}
	}()

	slog.InfoContext(cmd.Context(), "digest start",
		"file", filePath,
		"model", model,
		"chunk_size", f.chunkSize,
		"concurrency", concurrency,
		"timeout_sec", timeoutSec,
	)
	// Secondary model for OpenRouter-native fallback. Skipped when --model /
	// $DISTILL_MODEL pins one model (honor the pin) or when it would equal the
	// primary (a no-op array).
	fallback := ""
	if explicit == "" {
		fallback = cfg.EffectiveFallbackProfile(profile)
	}
	endpointCache := map[string]*ai.Endpoint{}
	clientCache := map[string]*ai.Client{}
	getClient := func(m string) (*ai.Client, error) {
		resolved, rerr := endpointForTextModel(cfg, profile, m, f.baseURL)
		if rerr != nil {
			return nil, rerr
		}
		textModel, provider, baseURL := resolved.model, resolved.provider, resolved.baseURL
		cacheKey := provider + "\x00" + baseURL + "\x00" + textModel
		if c, ok := clientCache[cacheKey]; ok {
			return c, nil
		}
		apiKey := ai.APIKeyForProvider(provider)
		endpointKey := provider + "\x00" + baseURL
		endpoint, ok := endpointCache[endpointKey]
		if !ok {
			var eerr error
			endpoint, eerr = ai.NewEndpoint(ai.Config{
				Provider: provider,
				BaseURL:  baseURL,
				APIKey:   apiKey,
			})
			if eerr != nil {
				return nil, fmt.Errorf("creating ai endpoint: %w", eerr)
			}
			endpointCache[endpointKey] = endpoint
		}
		fb := fallback
		if provider != "openrouter" || fb == m {
			fb = ""
		}
		c := endpoint.Client(ai.Config{
			Provider:        provider,
			BaseURL:         baseURL,
			APIKey:          apiKey,
			TextModel:       textModel,
			FallbackModel:   fb,
			ProviderOptions: providerOptionsForDigest(provider, sourceID),
		})
		clientCache[cacheKey] = c
		return c, nil
	}
	researchClient, err := getClient(roleModel("research"))
	if err != nil {
		return fmt.Errorf("creating ai client: %w", err)
	}
	fuseClient, err := getClient(roleModel("fuse"))
	if err != nil {
		return fmt.Errorf("creating ai client: %w", err)
	}
	writeClient, err := getClient(roleModel("write"))
	if err != nil {
		return fmt.Errorf("creating ai client: %w", err)
	}
	editClient, err := getClient(roleModel("edit"))
	if err != nil {
		return fmt.Errorf("creating ai client: %w", err)
	}
	outlineClient, err := getClient(roleModel("outline"))
	if err != nil {
		return fmt.Errorf("creating ai client: %w", err)
	}
	var judgeClient *ai.Client
	if checkPrecision {
		judgeModel := cfg.EffectiveEvalJudgeProfile(profile)
		if explicit != "" {
			judgeModel = explicit
		}
		judgeClient, err = getClient(judgeModel)
		if err != nil {
			return fmt.Errorf("creating precision judge client: %w", err)
		}
	}
	var researchEscalationClient *ai.Client
	if cascadeEnabled {
		researchEscalationClient, err = getClient(researchEscalationModel)
		if err != nil {
			return fmt.Errorf("creating research escalation client: %w", err)
		}
	}
	rc := digest.RoleCompleters{
		Research:           researchClient,
		ResearchEscalation: researchEscalationClient,
		Fuse:               fuseClient,
		Outline:            outlineClient,
		Section:            writeClient,
		Edit:               editClient,
		Judge:              judgeClient,
	}

	p, err := prompts.Load()
	if err != nil {
		return err
	}

	var researchCache digest.ResearchCache
	if f.researchCache && !f.noCache {
		resolved, rerr := endpointForTextModel(cfg, profile, roleModel("research"), f.baseURL)
		if rerr != nil {
			return rerr
		}
		c, cerr := researchcache.New(resolved.provider, resolved.baseURL, resolved.model, p.Research)
		if cerr != nil {
			slog.WarnContext(cmd.Context(), "digest research cache disabled", "err", cerr)
		} else {
			researchCache = c
		}
	}

	var embedder digest.BatchEmbedder
	if f.mergeFacts || f.targetFacts > 0 {
		var berr error
		embedder, embeddingModel, berr = buildCachedEmbedder(cmd.Context(), cfg, f.local, "", "")
		if berr != nil {
			return fmt.Errorf("creating digest embedder: %w", berr)
		}
	}

	var artCache digest.ArticleCache
	cacheKey := ""
	cacheRead := false
	if !f.noCache {
		c, cerr2 := digestcache.New()
		if cerr2 != nil {
			slog.WarnContext(cmd.Context(), "digest output cache disabled", "err", cerr2)
		} else {
			artCache = c
			cacheKey = digestcache.Key(digestcache.KeyInputs{
				Source:                  text,
				Profile:                 string(profile),
				BaseURL:                 f.baseURL,
				ResearchModel:           roleModel("research"),
				OutlineModel:            roleModel("outline"),
				WriteModel:              roleModel("write"),
				FuseModel:               roleModel("fuse"),
				EditModel:               roleModel("edit"),
				Style:                   style,
				ChunkSize:               f.chunkSize,
				MaxTokens:               preflightMaxTokens,
				Fuse:                    f.fuse,
				Edit:                    !f.noEdit,
				Appendix:                f.appendix,
				Repair:                  f.repair,
				DocContext:              f.docContext,
				Cite:                    f.cite,
				Cascade:                 cascadeEnabled,
				CascadeThreshold:        cascadeThreshold,
				ResearchEscalationModel: researchEscalationModel,
				MergeFacts:              f.mergeFacts,
				MergeThreshold:          mergeThreshold,
				OutlineFromClusters:     f.outlineFromClusters,
				TargetFacts:             f.targetFacts,
				EmbeddingModel:          embeddingModel,
				Context:                 steerContext,
				ResearchPrompt:          p.Research,
				OutlinePrompt:           p.Outline,
				SectionPrompt:           p.Section,
				FusePrompt:              p.Fuse,
				EditPrompt:              p.EditSection,
				RepairPrompt:            p.Repair,
				DocContextPrompt:        p.DocContext,
				DocHeaderPreamblePrompt: p.DocHeaderPreamble,
				CiteSectionPrompt:       p.CiteSection,
				CiteEditPrompt:          p.CiteEdit,
				CiteRepairPrompt:        p.CiteRepair,
				ContextPrompt:           p.ContextPreamble,
			})
			cacheRead = !f.reuseFacts && !checkPrecision
		}
	}

	usageFn := func() (prompt, cached, output int64) {
		for _, cl := range clientCache {
			pt, ct, ot := cl.Usage()
			prompt += pt
			cached += ct
			output += ot
		}
		return prompt, cached, output
	}
	res, err := digest.Run(cmd.Context(), rc, p, text, digest.Options{
		Style:                style,
		OutPath:              outPath,
		FactsPath:            factsPath,
		ArtifactDir:          artifactDir,
		ChunkSize:            f.chunkSize,
		MaxTokens:            preflightMaxTokens,
		Concurrency:          concurrency,
		Retries:              retries,
		Timeout:              time.Duration(timeoutSec) * time.Second,
		ReuseFacts:           f.reuseFacts,
		Resume:               f.resume,
		Fuse:                 f.fuse,
		Edit:                 !f.noEdit,
		Appendix:             f.appendix,
		Repair:               f.repair,
		DocContext:           f.docContext,
		Cite:                 f.cite,
		Cascade:              cascadeEnabled,
		CascadeThreshold:     cascadeThreshold,
		MergeFacts:           f.mergeFacts,
		MergeThreshold:       mergeThreshold,
		OutlineFromClusters:  f.outlineFromClusters,
		TargetFacts:          f.targetFacts,
		MaxSections:          maxSections,
		MinSectionFacts:      cfg.MinSectionFacts,
		ClusterBalanceFactor: cfg.ClusterBalanceFactor,
		CheckPrecision:       checkPrecision,
		RequirePrecision:     f.minPrecision > 0,
		PrecisionBatchSize:   firstPositive(cfg.PrecisionBatchSize, 80),
		Context:              steerContext,
		Cache:                artCache,
		CacheKey:             cacheKey,
		CacheRead:            cacheRead,
		ResearchCache:        researchCache,
		Embedder:             embedder,
		StoreOK:              func(r *digest.Result) bool { return checkDigestGate(r, f) == nil },
		Usage:                usageFn,
	})
	if err != nil {
		return err
	}

	slog.InfoContext(cmd.Context(), "digest done",
		"file", filePath,
		"chunk_count", res.ChunkCount,
		"failed", len(res.FailedChunks),
		"duration", time.Since(start),
	)
	progress.Stop()
	slog.SetDefault(previousLogger)
	progressActive = false

	printDigestSummary(cmd.ErrOrStderr(), res, artifactDir, clientCache)
	return checkDigestGate(res, f)
}

// checkDigestGate applies the deterministic, offline quality gate after a digest
// run: it returns a non-nil error (→ non-zero exit) when fact-coverage is below
// --min-coverage or the article word count falls outside --min-words/--max-words.
// Each check is opt-in via its flag (0 disables). No LLM call.
func checkDigestGate(res *digest.Result, f *digestFlags) error {
	if f.minCoverage > 0 && res.Coverage.Total > 0 {
		ratio := float64(res.Coverage.Covered) / float64(res.Coverage.Total)
		if ratio < f.minCoverage {
			return fmt.Errorf("digest gate failed: fact-coverage %.2f below --min-coverage %.2f (%d/%d specifics survived)",
				ratio, f.minCoverage, res.Coverage.Covered, res.Coverage.Total)
		}
	}
	if f.minCited > 0 {
		if res.Citations == nil || res.Citations.Total == 0 {
			return errors.New("digest gate failed: --min-cited requires --cite with extracted facts")
		}
		ratio := res.Citations.Ratio()
		if ratio < f.minCited {
			return fmt.Errorf("digest gate failed: cited fact coverage %.2f below --min-cited %.2f (%d/%d facts cited)",
				ratio, f.minCited, res.Citations.Covered, res.Citations.Total)
		}
	}
	if f.minPrecision > 0 {
		if res.Precision == nil || res.Precision.Total == 0 {
			return errors.New("digest gate failed: --min-precision requires --check-precision with judge results")
		}
		if res.Precision.Precision < f.minPrecision {
			return fmt.Errorf("digest gate failed: sentence precision %.2f below --min-precision %.2f (%d/%d sentences supported)",
				res.Precision.Precision, f.minPrecision, res.Precision.Supported, res.Precision.Total)
		}
	}
	if (f.minWords > 0 || f.maxWords > 0) && !extractscore.WordBandOK(res.Words, f.minWords, f.maxWords) {
		return fmt.Errorf("digest gate failed: article word count %d outside band [min=%d, max=%d] (--min-words/--max-words)",
			res.Words, f.minWords, f.maxWords)
	}
	return nil
}

// printDigestSummary writes the post-run output paths, token usage, reuse
// counts, fact-coverage, and any failure warnings for a completed digest run.
func printDigestSummary(w io.Writer, res *digest.Result, artifactDir string, clientCache map[string]*ai.Client) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Output")
	if res.CacheHit {
		_, _ = fmt.Fprintln(w, "  (served from digest output cache; no provider calls)")
	}
	_, _ = fmt.Fprintf(w, "  rewrite: %s\n", res.OutPath)
	_, _ = fmt.Fprintf(w, "  facts:   %s\n", res.FactsPath)
	_, _ = fmt.Fprintf(w, "  run:     %s\n", artifactDir)
	_, _ = fmt.Fprintf(w, "  ledger:  %s\n", res.LedgerPath)
	var promptToks, cachedToks, outputToks int64
	for _, cl := range clientCache {
		p, c, o := cl.Usage()
		promptToks += p
		cachedToks += c
		outputToks += o
	}
	if promptToks > 0 {
		pct := 100 * cachedToks / promptToks
		_, _ = fmt.Fprintf(w, "  tokens: %d prompt (%d cached, %d%%) + %d output\n", promptToks, cachedToks, pct, outputToks)
	}
	if res.ReusedFacts || res.ReusedOutline || res.ReusedChunks > 0 || res.ReusedSections > 0 || res.ReusedEdits > 0 {
		_, _ = fmt.Fprintf(w, "  reused:  facts=%t outline=%t chunks=%d sections=%d edits=%d\n",
			res.ReusedFacts, res.ReusedOutline, res.ReusedChunks, res.ReusedSections, res.ReusedEdits)
	}
	if res.UnverifiedFacts {
		_, _ = fmt.Fprintln(w, "  warning: facts reused from a checkpoint not verified against this source; article not cached")
	}
	if res.Coverage.Total > 0 {
		pct := 100 * res.Coverage.Covered / res.Coverage.Total
		_, _ = fmt.Fprintf(w, "  fact-coverage: %d%% (%d/%d specifics survived)\n", pct, res.Coverage.Covered, res.Coverage.Total)
		if res.SelectedFacts > 0 && res.DeselectedFacts > 0 {
			_, _ = fmt.Fprintf(w, "  selection: coverage over %d selected facts (%d deselected)\n", res.SelectedFacts, res.DeselectedFacts)
		}
		if len(res.Coverage.Missing) > 0 {
			shown := res.Coverage.Missing
			suffix := ""
			if len(shown) > 10 {
				suffix = fmt.Sprintf(" (+%d more)", len(shown)-10)
				shown = shown[:10]
			}
			_, _ = fmt.Fprintf(w, "  warning: %d specific(s) dropped: %s%s\n", len(res.Coverage.Missing), strings.Join(shown, ", "), suffix)
		}
	}
	if res.Citations != nil && res.Citations.Total > 0 {
		pct := 100 * res.Citations.Covered / res.Citations.Total
		_, _ = fmt.Fprintf(w, "  citations: %d%% (%d/%d facts cited)\n", pct, res.Citations.Covered, res.Citations.Total)
		if len(res.Citations.MissingIDs) > 0 {
			shown := res.Citations.MissingIDs
			suffix := ""
			if len(shown) > 10 {
				suffix = fmt.Sprintf(" (+%d more)", len(shown)-10)
				shown = shown[:10]
			}
			parts := make([]string, len(shown))
			for i, id := range shown {
				parts[i] = fmt.Sprintf("F%d", id)
			}
			_, _ = fmt.Fprintf(w, "  warning: %d fact citation(s) missing: %s%s\n", len(res.Citations.MissingIDs), strings.Join(parts, ", "), suffix)
		}
	}
	if res.Precision != nil && res.Precision.Total > 0 {
		pct := int(res.Precision.Precision * 100)
		fmt.Fprintf(w, "  precision: %d%% (%d/%d sentences supported)\n", pct, res.Precision.Supported, res.Precision.Total)
		if len(res.Precision.Unsupported) > 0 {
			shown := res.Precision.Unsupported
			suffix := ""
			if len(shown) > 5 {
				suffix = fmt.Sprintf(" (+%d more)", len(shown)-5)
				shown = shown[:5]
			}
			parts := make([]string, len(shown))
			for i, u := range shown {
				parts[i] = strconv.Itoa(u.Index)
			}
			fmt.Fprintf(w, "  warning: %d unsupported sentence(s): %s%s\n", len(res.Precision.Unsupported), strings.Join(parts, ", "), suffix)
		}
	}
	if res.Contradictions > 0 {
		fmt.Fprintf(w, "  contradictions: %d reported\n", res.Contradictions)
	}
	if len(res.FailedChunks) > 0 {
		fmt.Fprintf(w, "  warning: %d chunk(s) failed extraction: %s\n",
			len(res.FailedChunks), strings.Join(res.FailedChunks, ", "))
	}
	if len(res.FailedSections) > 0 {
		fmt.Fprintf(w, "  warning: %d section(s) failed after retries (stubbed): %s\n",
			len(res.FailedSections), strings.Join(res.FailedSections, "; "))
	}
	if len(res.FailedEdits) > 0 {
		fmt.Fprintf(w, "  warning: %d section edit(s) failed after retries (kept draft): %s\n",
			len(res.FailedEdits), strings.Join(res.FailedEdits, "; "))
	}
}

type digestInput struct {
	Source string
	Text   string
	Stdin  bool
	Multi  bool
}

func readDigestInput(stdin io.Reader, args []string) (digestInput, error) {
	if len(args) == 1 && args[0] == "-" {
		data, err := readCappedInput(stdin, countMaxInputBytes)
		if err != nil {
			return digestInput{}, fmt.Errorf("reading stdin: %w", err)
		}
		return digestInput{Source: "-", Text: string(data), Stdin: true}, nil
	}
	for _, arg := range args {
		if arg == "-" {
			return digestInput{}, errors.New("digest cannot combine stdin with file pathspecs")
		}
	}
	paths, err := expandDigestPathspecs(args)
	if err != nil {
		return digestInput{}, err
	}
	type fileInput struct {
		Path string
		Data []byte
	}
	files := make([]fileInput, 0, len(paths))
	total := int64(0)
	for _, path := range paths {
		file, err := openFileInput(path)
		if err != nil {
			return digestInput{}, fmt.Errorf("reading file %s: %w", path, err)
		}
		data, rerr := readCappedInput(file, countMaxInputBytes)
		cerr := file.Close()
		if rerr != nil {
			return digestInput{}, fmt.Errorf("reading file %s: %w", path, rerr)
		}
		if cerr != nil {
			return digestInput{}, fmt.Errorf("closing file %s: %w", path, cerr)
		}
		total += int64(len(data))
		if total > countMaxInputBytes {
			return digestInput{}, fmt.Errorf("reading pathspecs: %w: %d bytes", errCountInputTooLarge, countMaxInputBytes)
		}
		files = append(files, fileInput{Path: path, Data: data})
	}
	if len(files) == 1 {
		return digestInput{Source: files[0].Path, Text: string(files[0].Data)}, nil
	}
	var b strings.Builder
	for i, file := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "# Source: %s\n\n", file.Path)
		b.Write(file.Data)
	}
	return digestInput{
		Source: fmt.Sprintf("%d files", len(files)),
		Text:   b.String(),
		Multi:  true,
	}, nil
}

func expandDigestPathspecs(args []string) ([]string, error) {
	var paths []string
	seen := map[string]bool{}
	add := func(path string) {
		clean := filepath.Clean(path)
		if !seen[clean] {
			seen[clean] = true
			paths = append(paths, clean)
		}
	}
	for _, arg := range args {
		matches, err := expandDigestPathspec(arg)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			add(match)
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("digest pathspec matched no files")
	}
	return paths, nil
}

func expandDigestPathspec(spec string) ([]string, error) {
	if strings.Contains(spec, "**") {
		return expandRecursiveDigestGlob(spec)
	}
	if strings.ContainsAny(spec, "*?[") {
		matches, err := filepath.Glob(spec)
		if err != nil {
			return nil, fmt.Errorf("bad pathspec %q: %w", spec, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("pathspec %q matched no files", spec)
		}
		return filterDigestFiles(spec, matches)
	}
	info, err := os.Stat(spec)
	if err != nil {
		return nil, fmt.Errorf("reading pathspec %q: %w", spec, err)
	}
	if !info.IsDir() {
		return []string{spec}, nil
	}
	matches, err := filepath.Glob(filepath.Join(spec, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("reading directory pathspec %q: %w", spec, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("directory pathspec %q contains no .md files", spec)
	}
	return filterDigestFiles(spec, matches)
}

func expandRecursiveDigestGlob(spec string) ([]string, error) {
	before, after, _ := strings.Cut(spec, "**")
	root := strings.TrimRight(before, string(filepath.Separator))
	if root == "" {
		root = "."
	}
	pattern := strings.TrimLeft(after, string(filepath.Separator))
	if pattern == "" {
		pattern = "*"
	}
	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		ok, err := recursiveGlobMatch(pattern, rel)
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading recursive pathspec %q: %w", spec, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("pathspec %q matched no files", spec)
	}
	return filterDigestFiles(spec, matches)
}

func recursiveGlobMatch(pattern, rel string) (bool, error) {
	if !strings.Contains(pattern, string(filepath.Separator)) {
		return filepath.Match(pattern, filepath.Base(rel))
	}
	return filepath.Match(pattern, rel)
}

func filterDigestFiles(spec string, paths []string) ([]string, error) {
	files := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("reading pathspec %q match %q: %w", spec, path, err)
		}
		if info.IsDir() {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("pathspec %q matched no files", spec)
	}
	return files, nil
}

// resolveDigestContext returns the steering context from --context (inline) or
// --context-file (file, capped), erroring if both are set. Empty when neither.
func resolveDigestContext(inline, file string) (string, error) {
	if inline != "" && file != "" {
		return "", errors.New("--context and --context-file are mutually exclusive")
	}
	if file != "" {
		f, err := openFileInput(file)
		if err != nil {
			return "", fmt.Errorf("reading --context-file: %w", err)
		}
		b, rerr := readCappedInput(f, countMaxInputBytes)
		cerr := f.Close()
		if rerr != nil {
			return "", fmt.Errorf("reading --context-file: %w", rerr)
		}
		if cerr != nil {
			return "", fmt.Errorf("closing --context-file: %w", cerr)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(inline), nil
}

func providerOptionsForDigest(provider, sourcePath string) map[string]any {
	switch provider {
	case "deepseek":
		return map[string]any{"user_id": digestSessionID(sourcePath)}
	case "openrouter":
		return map[string]any{"session_id": digestSessionID(sourcePath)}
	default:
		return nil
	}
}

func printDigestDryRun(out io.Writer, cfg *config.Config, profile config.Profile, f *digestFlags, filePath, outPath, factsPath, artifactDir, style, text string, roleModel func(string) string, researchEscalationModel string) error {
	preflightMaxTokens := effectivePreflightMaxTokens(profile == config.ProfileLocal, f.maxTokens)
	chunks, err := digest.ChunkSource(text, f.chunkSize, preflightMaxTokens)
	if err != nil {
		return err
	}
	type rolePlan struct {
		name     string
		model    string
		textID   string
		provider string
		baseURL  string
		callCost int
	}
	outlineRole := "outline"
	if f.outlineFromClusters {
		outlineRole = "cluster-labels"
	}
	roles := []rolePlan{
		{name: "research", callCost: len(chunks)},
		{name: outlineRole, callCost: 1},
		{name: "write", callCost: 1},
	}
	if f.docContext {
		roles = append([]rolePlan{{name: "doc-context", callCost: 1}}, roles...)
	}
	if f.cascade && f.cascadeThreshold > 0 {
		roles = append(roles, rolePlan{name: "escalate", model: researchEscalationModel, callCost: len(chunks)})
	}
	if f.mergeFacts {
		roles = append(roles, rolePlan{name: "merge", callCost: len(chunks)})
	}
	if f.fuse {
		roles = append(roles, rolePlan{name: "fuse", callCost: 1})
	}
	if f.checkPrecision || f.minPrecision > 0 {
		roles = append(roles, rolePlan{name: "judge", callCost: 1})
	}
	if !f.noEdit {
		roles = append(roles, rolePlan{name: "edit", callCost: len(chunks)})
	}
	sectionEstimate := 1
	if len(chunks) > sectionEstimate {
		sectionEstimate = len(chunks)
	}
	for i := range roles {
		if roles[i].name == "write" {
			roles[i].callCost = sectionEstimate
		}
		modelRole := roles[i].name
		switch roles[i].name {
		case "doc-context":
			modelRole = "research"
		case "escalate":
			modelRole = ""
		case "merge":
			modelRole = "fuse"
		case "cluster-labels":
			modelRole = "fuse"
		case "judge":
			roles[i].model = cfg.EffectiveEvalJudgeProfile(profile)
			if explicit := firstNonEmpty(f.model, os.Getenv("DISTILL_MODEL")); explicit != "" {
				roles[i].model = explicit
			}
			modelRole = ""
		case "write":
			modelRole = "write"
		}
		if modelRole != "" {
			roles[i].model = roleModel(modelRole)
		}
		resolved, rerr := endpointForTextModel(cfg, profile, roles[i].model, f.baseURL)
		if rerr != nil {
			return rerr
		}
		roles[i].textID = resolved.model
		roles[i].provider = resolved.provider
		roles[i].baseURL = resolved.baseURL
	}

	verified := digest.ArtifactsMatchSource(artifactDir, text, f.chunkSize, preflightMaxTokens)
	resume := f.resume && verified
	reusedFacts := (f.reuseFacts || resume) && fileReusable(factsPath, "facts")
	reusedOutline := resume && fileReusable(filepath.Join(artifactDir, "responses", "outline.md"), "outline")
	reusedResearch := countExistingArtifacts(resume, len(chunks), artifactDir, "chunk-%03d.md", "research")
	reusedSections := 0
	reusedEdits := 0
	for i := 0; i < sectionEstimate; i++ {
		if resume && fileReusable(filepath.Join(artifactDir, "responses", fmt.Sprintf("section-%03d.md", i+1)), "section") {
			reusedSections++
		}
		if resume && fileReusable(filepath.Join(artifactDir, "responses", fmt.Sprintf("section-%03d.edited.md", i+1)), "edit") {
			reusedEdits++
		}
	}
	plannedCalls := plannedDigestCalls(len(chunks), f, factsPath, artifactDir, verified)

	_, _ = fmt.Fprintln(out, "Digest dry run")
	fmt.Fprintf(out, "  source:      %s\n", filePath)
	fmt.Fprintf(out, "  rewrite:     %s\n", outPath)
	fmt.Fprintf(out, "  facts:       %s\n", factsPath)
	fmt.Fprintf(out, "  artifacts:   %s\n", artifactDir)
	fmt.Fprintf(out, "  ledger:      %s\n", filepath.Join(artifactDir, "run-ledger.jsonl"))
	fmt.Fprintf(out, "  chunks:      %d\n", len(chunks))
	fmt.Fprintf(out, "  calls:       %d planned", plannedCalls)
	if f.resume || f.reuseFacts {
		fmt.Fprintf(out, " (reused facts=%t outline=%t chunks=%d sections=%d edits=%d)", reusedFacts, reusedOutline, reusedResearch, reusedSections, reusedEdits)
	}
	_, _ = fmt.Fprintln(out)
	fmt.Fprintf(out, "  style:       %s\n", style)
	_, _ = fmt.Fprintln(out, "  roles:")
	for _, role := range roles {
		fmt.Fprintf(out, "    %-8s model=%s request_model=%s provider=%s base_url=%s nominal_calls=%d\n",
			role.name, role.model, role.textID, role.provider, role.baseURL, role.callCost)
	}
	_, _ = fmt.Fprintln(out, "  no provider calls were made")
	return nil
}

// minSectionEstimate floors the section-count estimate used for planning API
// calls: the outline stage can split a document into more sections than there
// are chunks, so basing the estimate solely on chunk count under-predicts calls
// for small inputs.
const minSectionEstimate = 3

// verified reports whether the artifact dir's source-binding marker matches the
// current run; unverified artifacts are never counted as reusable.
func plannedDigestCalls(chunks int, f *digestFlags, factsPath, artifactDir string, verified bool) int {
	sectionEstimate := max(minSectionEstimate, chunks)
	resume := f.resume && verified
	reusedResearch := countExistingArtifacts(resume, chunks, artifactDir, "chunk-%03d.md", "research")
	reusedFacts := (f.reuseFacts || resume) && fileReusable(factsPath, "facts")
	reusedOutline := resume && fileReusable(filepath.Join(artifactDir, "responses", "outline.md"), "outline")
	reusedSections := countExistingArtifacts(resume, sectionEstimate, artifactDir, "section-%03d.md", "section")
	reusedEdits := countExistingArtifacts(resume, sectionEstimate, artifactDir, "section-%03d.edited.md", "edit")
	planned := 0
	if !reusedFacts {
		planned += chunks - reusedResearch
		if f.cascade && f.cascadeThreshold > 0 {
			planned += chunks - reusedResearch
		}
		if f.docContext && !fileReusable(filepath.Join(artifactDir, "responses", "doc-context.md"), "doc-context") {
			planned++
		}
	}
	if f.mergeFacts {
		planned += chunks
	}
	if f.fuse {
		planned++
	}
	if !reusedOutline {
		planned++
	}
	planned += sectionEstimate - reusedSections
	if !f.noEdit {
		planned += sectionEstimate - reusedEdits
	}
	if f.repair {
		planned++
	}
	if f.cite {
		planned++
	}
	if f.checkPrecision || f.minPrecision > 0 {
		planned++
	}
	if f.repair && (f.checkPrecision || f.minPrecision > 0) {
		planned += 2
	}
	return planned
}

func countExistingArtifacts(enabled bool, count int, artifactDir, pattern, stage string) int {
	if !enabled {
		return 0
	}
	reused := 0
	for i := 1; i <= count; i++ {
		if fileReusable(filepath.Join(artifactDir, "responses", fmt.Sprintf(pattern, i)), stage) {
			reused++
		}
	}
	return reused
}

func fileReusable(path, stage string) bool {
	data, err := os.ReadFile(path)
	return err == nil && digest.ArtifactReusableForResume(stage, string(data))
}

func digestSessionID(sourcePath string) string {
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		abs = sourcePath
	}
	sum := sha256.Sum256([]byte(abs))
	return "distill-digest-" + hex.EncodeToString(sum[:12])
}

// resolveArtifactDir returns dir as-is (creating it if needed) or a fresh temp
// dir when dir is empty.
func resolveArtifactDir(dir string) (string, error) {
	if dir == "" {
		d, err := os.MkdirTemp("", "distill-digest-*")
		if err != nil {
			return "", fmt.Errorf("creating temp dir: %w", err)
		}
		return d, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating artifact dir: %w", err)
	}
	return dir, nil
}

// firstPositive returns the first value in vals that is greater than zero, or 0
// if none are. Used to resolve flag → config → built-in defaults.
func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
