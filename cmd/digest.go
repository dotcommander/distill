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
	"github.com/dotcommander/distill/internal/manifest"
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
	budget, err := ai.NewRequestBudget(f.maxCalls)
	if err != nil {
		return fmt.Errorf("--max-calls: %w", err)
	}
	if budget.Limit() > 0 {
		defer func() {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "request budget: %d/%d\n", budget.Used(), budget.Limit())
		}()
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
	parts := make([]digest.SourcePart, len(input.Parts))
	for i, part := range input.Parts {
		cleaned := maybeCleanTranscript(cmd.Context(), maybeStripBinary(cmd.Context(), normalizeInput(part.Text)), f.forceClean, f.noClean)
		parts[i] = digest.SourcePart{Ordinal: i + 1, Text: cleaned}
	}
	text := joinDigestSourceParts(parts)
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

	artifactDir := resolveDigestArtifactDir(f.artifacts, f.dryRun)
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
	packedPlan, err := digest.PlanPackedSources(parts, f.chunkSize, preflightMaxTokens)
	if err != nil {
		return err
	}
	if f.maxCalls > 0 && (f.mergeFacts || f.targetFacts > 0 || checkPrecision) {
		return errors.New("digest: finite --max-calls cannot authoritatively plan dynamic embedding, merge-cluster, or precision-batch calls; omit --max-calls or disable --merge-facts, --target-facts, and --check-precision")
	}
	sectionCap := max(minSectionEstimate, len(packedPlan.Chunks))
	if f.outlineFromClusters && maxSections > 0 {
		sectionCap = maxSections
	}
	callPlan, err := digest.NewCallPlan(packedPlan, sectionCap, !f.noEdit, f.maxCalls, retries)
	if err != nil {
		return err
	}
	stageCalls := map[string]int{}
	if f.docContext {
		stageCalls["doc-context"] = 1
	}
	if cascadeEnabled {
		stageCalls["research-escalation"] = len(packedPlan.Chunks)
	}
	if f.fuse {
		stageCalls["fuse"] = 1
	}
	if f.mergeFacts {
		stageCalls["merge"] = len(packedPlan.Chunks)
	}
	if f.repair {
		stageCalls["repair"] = 1
	}
	if f.cite {
		stageCalls["cite-repair"] = 1
	}
	if checkPrecision {
		stageCalls["precision"] = 1
	}
	if f.repair && checkPrecision {
		stageCalls["precision-repair"] = 2
	}
	callPlan, err = callPlan.WithStageCalls(stageCalls, retries)
	if err != nil {
		return err
	}
	if _, bindingErr := digest.ValidateArtifactBindingPlan(artifactDir, packedPlan); bindingErr != nil {
		return bindingErr
	}
	// Schema-v2 response reuse is released by the runtime only after exact
	// prompt/upstream/route sidecar validation. Preflight therefore stays
	// conservative instead of trusting raw checkpoint presence.
	if preflightErr := callPlan.Preflight(); preflightErr != nil {
		return preflightErr
	}
	if f.dryRun {
		return printDigestDryRun(cmd.ErrOrStderr(), cfg, profile, f, filePath, outPath, factsPath, artifactDir, style, packedPlan, callPlan, roleModel, researchEscalationModel)
	}
	if _, prepareErr := digest.PrepareArtifactBindingPlan(artifactDir, packedPlan); prepareErr != nil {
		return prepareErr
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
	// Secondary models are dispatched explicitly by the digest pipeline so every
	// outbound attempt is budgeted and recorded. Never configure Wormhole's
	// invisible OpenRouter-native models-array fallback here.
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
				Provider:      provider,
				BaseURL:       baseURL,
				APIKey:        apiKey,
				RequestBudget: budget,
				NoRetries:     true,
			})
			if eerr != nil {
				return nil, fmt.Errorf("creating ai endpoint: %w", eerr)
			}
			endpointCache[endpointKey] = endpoint
		}
		c := endpoint.Client(ai.Config{
			Provider:        provider,
			BaseURL:         baseURL,
			APIKey:          apiKey,
			TextModel:       textModel,
			ProviderOptions: providerOptionsForDigest(provider, sourceID),
			RequestBudget:   budget,
			NoRetries:       true,
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
	routeFor := func(model string, client digest.Completer, kind digest.RouteKind) (digest.Route, error) {
		if client == nil || model == "" {
			return digest.Route{Kind: kind}, nil
		}
		resolved, rerr := endpointForTextModel(cfg, profile, model, f.baseURL)
		if rerr != nil {
			return digest.Route{}, rerr
		}
		return digest.Route{
			Completer: client,
			Provider:  resolved.provider,
			Model:     resolved.model,
			Kind:      kind,
			Available: true,
		}, nil
	}
	fallbackRoute := digest.Route{Kind: digest.RouteFallback}
	fallbackStatus := "disabled by explicit model pin"
	//nolint:nestif // Credential discovery and route construction are one fallback decision.
	if fallback != "" {
		resolvedFallback, rerr := endpointForTextModel(cfg, profile, fallback, f.baseURL)
		if rerr != nil {
			return rerr
		}
		if ai.APIKeyForProvider(resolvedFallback.provider) == "" && resolvedFallback.provider != digestProviderLocal {
			fallbackStatus = fmt.Sprintf("unavailable: missing %s credentials", resolvedFallback.provider)
		} else {
			fallbackClient, ferr := getClient(fallback)
			if ferr != nil {
				return fmt.Errorf("creating fallback ai client: %w", ferr)
			}
			fallbackRoute, err = routeFor(fallback, fallbackClient, digest.RouteFallback)
			if err != nil {
				return err
			}
			fallbackStatus = fmt.Sprintf("available: provider=%s model=%s", fallbackRoute.Provider, fallbackRoute.Model)
		}
	}
	roleRoutes := make(digest.RoleRoutes)
	addRoleRoute := func(role, model string, client digest.Completer) error {
		primary, rerr := routeFor(model, client, digest.RoutePrimary)
		if rerr != nil {
			return rerr
		}
		roleRoutes[role] = digest.RoutePair{Primary: primary, Fallback: fallbackRoute}
		return nil
	}
	for _, role := range []struct {
		name   string
		model  string
		client digest.Completer
	}{
		{name: digestRoleResearch, model: roleModel(digestRoleResearch), client: researchClient},
		{name: digestRoleDocContext, model: roleModel(digestRoleResearch), client: researchClient},
		{name: digestRoleFuse, model: roleModel(digestRoleFuse), client: fuseClient},
		{name: digestRoleMerge, model: roleModel(digestRoleFuse), client: fuseClient},
		{name: digestRoleClusterLabels, model: roleModel(digestRoleFuse), client: fuseClient},
		{name: digestRoleOutline, model: roleModel(digestRoleOutline), client: outlineClient},
		{name: "section", model: roleModel("write"), client: writeClient},
		{name: digestRoleEdit, model: roleModel(digestRoleEdit), client: editClient},
		{name: digestRoleRepair, model: roleModel(digestRoleEdit), client: editClient},
		{name: digestRoleCiteRepair, model: roleModel(digestRoleEdit), client: editClient},
	} {
		if routeErr := addRoleRoute(role.name, role.model, role.client); routeErr != nil {
			return routeErr
		}
	}
	//nolint:nestif // The judge routes are installed together when that optional client exists.
	if judgeClient != nil {
		judgeModel := cfg.EffectiveEvalJudgeProfile(profile)
		if explicit != "" {
			judgeModel = explicit
		}
		if routeErr := addRoleRoute(digestRoleJudge, judgeModel, judgeClient); routeErr != nil {
			return routeErr
		}
		if routeErr := addRoleRoute(digestRolePrecision, judgeModel, judgeClient); routeErr != nil {
			return routeErr
		}
		if routeErr := addRoleRoute(digestRolePrecisionRepair, judgeModel, judgeClient); routeErr != nil {
			return routeErr
		}
	}
	if researchEscalationClient != nil {
		if routeErr := addRoleRoute("research-escalation", researchEscalationModel, researchEscalationClient); routeErr != nil {
			return routeErr
		}
	}
	//nolint:sloglint // The CLI intentionally uses the process-wide configured logger.
	slog.InfoContext(cmd.Context(), "digest fallback routing", "status", fallbackStatus)
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
		embedder, embeddingModel, berr = buildCachedEmbedder(cachedEmbedderOptions{
			ctx:    cmd.Context(),
			cfg:    cfg,
			local:  f.local,
			budget: budget,
		})
		if berr != nil {
			return fmt.Errorf("creating digest embedder: %w", berr)
		}
	}

	digestOpts := digest.Options{
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
		ResearchCache:        researchCache,
		Embedder:             embedder,
		PackedPlan:           &packedPlan,
		CallPlan:             &callPlan,
		Dispatcher:           digest.NewDispatcher(roleRoutes, budget, &callPlan, retries, time.Second),
		ProvenanceParameters: digest.BuildProvenanceParameters(roleRoutes, retries, time.Duration(timeoutSec)*time.Second),
	}

	var artCache digest.ArticleCache
	cacheKey := ""
	cacheRead := false
	if !f.noCache {
		c, cerr2 := digestcache.New()
		if cerr2 != nil {
			slog.WarnContext(cmd.Context(), "digest output cache disabled", "err", cerr2) //nolint:sloglint // existing command-wide progress logger routing
		} else {
			artCache = c
			cacheInputs := digestcache.KeyInputs{
				Source:                  text,
				Profile:                 string(profile),
				BaseURL:                 f.baseURL,
				ResearchModel:           roleModel("research"),
				OutlineModel:            roleModel("outline"),
				WriteModel:              roleModel("write"),
				FuseModel:               roleModel("fuse"),
				EditModel:               roleModel("edit"),
				Style:                   digestOpts.Style,
				ChunkSize:               digestOpts.ChunkSize,
				MaxTokens:               digestOpts.MaxTokens,
				Fuse:                    digestOpts.Fuse,
				Edit:                    digestOpts.Edit,
				Appendix:                digestOpts.Appendix,
				Repair:                  digestOpts.Repair,
				DocContext:              digestOpts.DocContext,
				Cite:                    digestOpts.Cite,
				Cascade:                 digestOpts.Cascade,
				CascadeThreshold:        digestOpts.CascadeThreshold,
				ResearchEscalationModel: researchEscalationModel,
				TargetFacts:             digestOpts.TargetFacts,
				EmbeddingModel:          embeddingModel,
				Context:                 digestOpts.Context,
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
			}
			populateMergeDigestCacheInputs(&cacheInputs, digestOpts, embeddingModel, p)
			cacheKey = digestcache.Key(cacheInputs)
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
	digestOpts.Cache = artCache
	digestOpts.CacheKey = cacheKey
	digestOpts.CacheRead = cacheRead
	digestOpts.StoreOK = func(r *digest.Result) bool { return checkDigestGate(r, f) == nil }
	digestOpts.FinalGate = func(r *digest.Result) error { return checkDigestGate(r, f) }
	digestOpts.Usage = usageFn
	res, err := digest.RunSources(cmd.Context(), rc, p, parts, digestOpts)
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

	printDigestSummary(cmd.ErrOrStderr(), res, artifactDir, clientCache, budget)
	return checkDigestGate(res, f)
}

func populateMergeDigestCacheInputs(in *digestcache.KeyInputs, opts digest.Options, embeddingModel string, p *prompts.Set) {
	in.MergeFacts = opts.MergeFacts
	in.MergeThreshold = opts.MergeThreshold
	in.OutlineFromClusters = opts.OutlineFromClusters
	in.MaxSections = opts.MaxSections
	in.MinSectionFacts = opts.MinSectionFacts
	in.ClusterBalanceFactor = opts.ClusterBalanceFactor
	in.EmbeddingModel = embeddingModel
	in.MergeFactsPrompt = p.MergeFacts
	in.ClusterLabelsPrompt = p.ClusterLabels
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
func printDigestSummary(w io.Writer, res *digest.Result, artifactDir string, clientCache map[string]*ai.Client, budget *ai.RequestBudget) {
	writeDigestOutputSummary(w, res, artifactDir, budget)
	writeDigestUsageSummary(w, clientCache)
	writeDigestReuseSummary(w, res)
	writeDigestCoverageSummary(w, res)
	writeDigestCitationSummary(w, res)
	writeDigestPrecisionSummary(w, res)
	writeDigestFailureSummary(w, res)
}

func writeDigestOutputSummary(w io.Writer, res *digest.Result, artifactDir string, budget *ai.RequestBudget) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Output")
	if res.CacheHit && budget.Used() == 0 {
		_, _ = fmt.Fprintln(w, "  (served from digest output cache; no provider calls)")
	}
	_, _ = fmt.Fprintf(w, "  rewrite: %s\n", res.OutPath)
	_, _ = fmt.Fprintf(w, "  facts:   %s\n", res.FactsPath)
	_, _ = fmt.Fprintf(w, "  run:     %s\n", artifactDir)
	_, _ = fmt.Fprintf(w, "  ledger:  %s\n", res.LedgerPath)
}

func writeDigestUsageSummary(w io.Writer, clientCache map[string]*ai.Client) {
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
}

func writeDigestReuseSummary(w io.Writer, res *digest.Result) {
	if res.ReusedFacts || res.ReusedOutline || res.ReusedChunks > 0 || res.ReusedSections > 0 || res.ReusedEdits > 0 {
		_, _ = fmt.Fprintf(w, "  reused:  facts=%t outline=%t chunks=%d sections=%d edits=%d\n",
			res.ReusedFacts, res.ReusedOutline, res.ReusedChunks, res.ReusedSections, res.ReusedEdits)
	}
	if res.UnverifiedFacts {
		_, _ = fmt.Fprintln(w, "  warning: facts reused from a checkpoint not verified against this source; article not cached")
	}
}

func writeDigestCoverageSummary(w io.Writer, res *digest.Result) {
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
}

func writeDigestCitationSummary(w io.Writer, res *digest.Result) {
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
}

func writeDigestPrecisionSummary(w io.Writer, res *digest.Result) {
	if res.Precision != nil && res.Precision.Total > 0 {
		pct := int(res.Precision.Precision * 100)
		_, _ = fmt.Fprintf(w, "  precision: %d%% (%d/%d sentences supported)\n", pct, res.Precision.Supported, res.Precision.Total)
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
			_, _ = fmt.Fprintf(w, "  warning: %d unsupported sentence(s): %s%s\n", len(res.Precision.Unsupported), strings.Join(parts, ", "), suffix)
		}
	}
}

func writeDigestFailureSummary(w io.Writer, res *digest.Result) {
	if res.Contradictions > 0 {
		_, _ = fmt.Fprintf(w, "  contradictions: %d reported\n", res.Contradictions)
	}
	if len(res.FailedChunks) > 0 {
		_, _ = fmt.Fprintf(w, "  warning: %d chunk(s) failed extraction: %s\n",
			len(res.FailedChunks), strings.Join(res.FailedChunks, ", "))
	}
	if len(res.FailedSections) > 0 {
		_, _ = fmt.Fprintf(w, "  warning: %d section(s) failed after retries (stubbed): %s\n",
			len(res.FailedSections), strings.Join(res.FailedSections, "; "))
	}
	if len(res.FailedEdits) > 0 {
		_, _ = fmt.Fprintf(w, "  warning: %d section edit(s) failed after retries (kept draft): %s\n",
			len(res.FailedEdits), strings.Join(res.FailedEdits, "; "))
	}
}

type digestInput struct {
	Source string
	Text   string
	Parts  []digestInputPart
	Stdin  bool
	Multi  bool
}

type digestInputPart struct {
	Path string
	Text string
}

func readDigestInput(stdin io.Reader, args []string) (digestInput, error) {
	if len(args) == 1 && args[0] == "-" {
		data, err := readCappedInput(stdin, countMaxInputBytes)
		if err != nil {
			return digestInput{}, fmt.Errorf("reading stdin: %w", err)
		}
		text := string(data)
		return digestInput{Source: "-", Text: text, Parts: []digestInputPart{{Path: "-", Text: text}}, Stdin: true}, nil
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
	for _, input := range paths {
		var file *os.File
		if input.fromManifest {
			file, err = manifest.OpenChunk(filepath.Dir(input.path), filepath.Base(input.path))
		} else {
			file, err = openFileInput(input.path)
		}
		if err != nil {
			return digestInput{}, fmt.Errorf("reading file %s: %w", input.path, err)
		}
		data, rerr := readCappedInput(file, countMaxInputBytes)
		cerr := file.Close()
		if rerr != nil {
			return digestInput{}, fmt.Errorf("reading file %s: %w", input.path, rerr)
		}
		if cerr != nil {
			return digestInput{}, fmt.Errorf("closing file %s: %w", input.path, cerr)
		}
		total += int64(len(data))
		if total > countMaxInputBytes {
			return digestInput{}, fmt.Errorf("reading pathspecs: %w: %d bytes", errCountInputTooLarge, countMaxInputBytes)
		}
		files = append(files, fileInput{Path: input.path, Data: data})
	}
	if len(files) == 1 {
		text := string(files[0].Data)
		return digestInput{
			Source: files[0].Path,
			Text:   text,
			Parts:  []digestInputPart{{Path: files[0].Path, Text: text}},
		}, nil
	}
	var b strings.Builder
	parts := make([]digestInputPart, 0, len(files))
	for i, file := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "# Source %02d\n\n", i+1)
		b.Write(file.Data)
		parts = append(parts, digestInputPart{Path: file.Path, Text: string(file.Data)})
	}
	return digestInput{
		Source: fmt.Sprintf("%d files", len(files)),
		Text:   b.String(),
		Parts:  parts,
		Multi:  true,
	}, nil
}

func joinDigestSourceParts(parts []digest.SourcePart) string {
	if len(parts) == 1 {
		return parts[0].Text
	}
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "# %s\n\n", digest.SourceLabel(part.Ordinal))
		b.WriteString(part.Text)
	}
	return b.String()
}

type digestPath struct {
	path         string
	fromManifest bool
}

func expandDigestPathspecs(args []string) ([]digestPath, error) {
	var paths []digestPath
	seen := map[string]int{}
	add := func(input digestPath) {
		input.path = filepath.Clean(input.path)
		if index, ok := seen[input.path]; ok {
			paths[index].fromManifest = paths[index].fromManifest || input.fromManifest
			return
		}
		seen[input.path] = len(paths)
		paths = append(paths, input)
	}
	for _, arg := range args {
		matches, err := expandDigestInputPathspec(arg)
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

func expandDigestInputPathspec(spec string) ([]digestPath, error) {
	if paths, ok, err := manifestDigestPaths(spec); err != nil {
		return nil, err
	} else if ok {
		return paths, nil
	}
	paths, err := expandDigestPathspec(spec)
	if err != nil {
		return nil, err
	}
	inputs := make([]digestPath, 0, len(paths))
	for _, path := range paths {
		inputs = append(inputs, digestPath{path: path})
	}
	return inputs, nil
}

func manifestDigestPaths(spec string) ([]digestPath, bool, error) {
	if strings.ContainsAny(spec, "*?[") {
		return nil, false, nil
	}
	info, err := os.Stat(spec)
	if err != nil {
		return nil, false, fmt.Errorf("reading pathspec %q: %w", spec, err)
	}
	if !info.IsDir() {
		return nil, false, nil
	}
	manifestPath := filepath.Join(spec, "manifest.json")
	if _, manifestErr := os.Lstat(manifestPath); errors.Is(manifestErr, os.ErrNotExist) {
		return nil, false, nil
	} else if manifestErr != nil {
		return nil, false, fmt.Errorf("inspecting directory manifest %q: %w", manifestPath, manifestErr)
	}
	m, err := manifest.ReadManifest(spec)
	if err != nil {
		return nil, false, fmt.Errorf("reading directory manifest %q: %w", manifestPath, err)
	}
	paths := make([]digestPath, 0, len(m.Chunks))
	for _, chunk := range m.Chunks {
		paths = append(paths, digestPath{
			path:         filepath.Join(spec, chunk.File),
			fromManifest: true,
		})
	}
	return paths, true, nil
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

//nolint:funlen,gocognit,gocyclo,nestif,revive // Dry-run renders the complete, side-effect-free call plan.
func printDigestDryRun(out io.Writer, cfg *config.Config, profile config.Profile, f *digestFlags, filePath, outPath, factsPath, artifactDir, style string, packedPlan digest.PackedPlan, callPlan digest.CallPlan, roleModel func(string) string, researchEscalationModel string) error {
	var report strings.Builder
	chunks := packedPlan.Chunks
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

	reusedFacts := callPlan.Reused.Research == len(chunks)
	reusedOutline := callPlan.Reused.Outline == 1
	reusedResearch := callPlan.Reused.Research
	reusedSections := callPlan.Reused.Sections
	reusedEdits := callPlan.Reused.Edits

	_, _ = fmt.Fprintln(&report, "Digest dry run")
	_, _ = fmt.Fprintf(&report, "  source:      %s\n", filePath)
	_, _ = fmt.Fprintf(&report, "  rewrite:     %s\n", outPath)
	_, _ = fmt.Fprintf(&report, "  facts:       %s\n", factsPath)
	_, _ = fmt.Fprintf(&report, "  artifacts:   %s\n", artifactDir)
	_, _ = fmt.Fprintf(&report, "  ledger:      %s\n", filepath.Join(artifactDir, "run-ledger.jsonl"))
	_, _ = fmt.Fprintf(&report, "  chunks:      %d\n", len(chunks))
	_, _ = fmt.Fprintf(&report, "  calls:       mandatory=%d configured_worst_case=%d section_cap=%d recovery_headroom=%d",
		callPlan.MandatoryCalls, callPlan.ConfiguredWorstCase, callPlan.SectionCap, callPlan.RecoveryHeadroom)
	if callPlan.HardLimit > 0 {
		_, _ = fmt.Fprintf(&report, " hard_limit=%d", callPlan.HardLimit)
	}
	if f.resume || f.reuseFacts {
		_, _ = fmt.Fprintf(&report, " (reused facts=%t outline=%t chunks=%d sections=%d edits=%d)", reusedFacts, reusedOutline, reusedResearch, reusedSections, reusedEdits)
	}
	_, _ = fmt.Fprintln(&report)
	_, _ = fmt.Fprintf(&report, "  style:       %s\n", style)
	_, _ = fmt.Fprintln(&report, "  roles:")
	for _, role := range roles {
		_, _ = fmt.Fprintf(&report, "    %-8s model=%s request_model=%s provider=%s base_url=%s nominal_calls=%d\n",
			role.name, role.model, role.textID, role.provider, role.baseURL, role.callCost)
	}
	fallbackStatus := "disabled by explicit model pin"
	if explicit := firstNonEmpty(f.model, os.Getenv("DISTILL_MODEL")); explicit == "" {
		fallbackModel := cfg.EffectiveFallbackProfile(profile)
		fallbackStatus = "not configured"
		if fallbackModel != "" {
			resolved, rerr := endpointForTextModel(cfg, profile, fallbackModel, f.baseURL)
			if rerr != nil {
				return rerr
			}
			if ai.APIKeyForProvider(resolved.provider) == "" && resolved.provider != digestProviderLocal {
				fallbackStatus = fmt.Sprintf("unavailable: missing %s credentials (model=%s)", resolved.provider, resolved.model)
			} else {
				fallbackStatus = fmt.Sprintf("available: provider=%s model=%s", resolved.provider, resolved.model)
			}
		}
	}
	_, _ = fmt.Fprintf(&report, "  fallback:    %s\n", fallbackStatus)
	_, _ = fmt.Fprintln(&report, "  no provider calls were made")
	_, err := io.WriteString(out, report.String())
	return err
}

// minSectionEstimate floors the section-count estimate used for planning API
// calls: the outline stage can split a document into more sections than there
// are chunks, so basing the estimate solely on chunk count under-predicts calls
// for small inputs.
const minSectionEstimate = 3

const (
	digestProviderLocal       = "local"
	digestRoleResearch        = "research"
	digestRoleDocContext      = "doc-context"
	digestRoleFuse            = "fuse"
	digestRoleMerge           = "merge"
	digestRoleClusterLabels   = "cluster-labels"
	digestRoleOutline         = "outline"
	digestRoleEdit            = "edit"
	digestRoleRepair          = "repair"
	digestRoleCiteRepair      = "cite-repair"
	digestRoleJudge           = "judge"
	digestRolePrecision       = "precision"
	digestRolePrecisionRepair = "precision-repair"
)

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

// resolveDigestArtifactDir returns a path without creating it. Preparation is
// deliberately deferred until after authoritative call-plan preflight, so a
// rejected finite run and every dry-run leave no artifact directory behind.
func resolveDigestArtifactDir(dir string, _ bool) string {
	if dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("distill-digest-%d-%d", os.Getpid(), time.Now().UnixNano()))
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
