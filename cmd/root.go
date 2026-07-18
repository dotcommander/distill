package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
)

// Deps is the explicit dependency seam shared by both command trees.
type Deps struct{}

type commandOutput struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	flags          map[string]bool
}

type runContext struct {
	ctx         context.Context
	in          io.Reader
	out, errOut io.Writer
	flags       map[string]bool
}

func (c *runContext) Context() context.Context     { return c.ctx }
func (c *runContext) OutOrStdout() io.Writer       { return c.out }
func (c *runContext) ErrOrStderr() io.Writer       { return c.errOut }
func (c *runContext) FlagChanged(name string) bool { return c.flags[name] }

type distillCLI struct {
	Count       countCommand       `cmd:"" help:"Count tokens, characters, and lines"`
	Chunk       chunkCommand       `cmd:"" help:"Split a document into chunks"`
	Digest      digestCommand      `cmd:"" help:"Distill a document into a fact-preserving rewrite"`
	DigestScore digestScoreCommand `cmd:"" name:"digest-score" hidden:""`
	DigestGrade movedCommand       `cmd:"" name:"digest-grade" hidden:"" passthrough:""`
	Eval        movedCommand       `cmd:"" hidden:"" passthrough:""`
	Models      movedCommand       `cmd:"" hidden:"" passthrough:""`
}

type countCommand struct {
	Format string `default:"json" help:"Output format: json or plain"`
	File   string `arg:"" optional:"" name:"file"`
}

func (c *countCommand) Run(ctx context.Context, out *commandOutput) error {
	return runCount(newRunContext(ctx, out), optionalArg(c.File), c.Format)
}

type chunkCommand struct {
	Mode            string  `default:"headings" help:"Chunking mode: headings, semantic, cramit"`
	MaxTokens       int     `name:"max-tokens" default:"4000" help:"Target max tokens per chunk"`
	Overlap         int     `default:"200" help:"Tokens of overlap between chunks"`
	OutDir          string  `name:"out-dir" help:"Output directory (default: temp dir)"`
	Format          string  `default:"json" help:"Output format for manifest"`
	Threshold       float64 `default:"0.3" help:"Similarity threshold for semantic mode"`
	Provider        string  `help:"Embedding provider for semantic mode (openrouter, openai, gemini)"`
	EmbeddingModel  string  `name:"embedding-model" help:"Embedding model name for semantic mode (e.g. text-embedding-3-small). Falls back to $DISTILL_EMBEDDING_MODEL when unset."`
	Local           bool    `help:"Use the local embedding profile (local_embedding_model/local_embedding_endpoint) instead of the remote OpenRouter default."`
	SmoothingWindow int     `name:"smoothing-window" help:"Semantic mode: moving-average window over similarity scores (0 = engine default)"`
	CoherenceWindow int     `name:"coherence-window" help:"Semantic mode: two-sided coherence window for break validation (0 = engine default)"`
	MinChunkChars   int     `name:"min-chunk-chars" help:"Semantic mode: merge chunk groups smaller than this many chars (0 = engine default)"`
	MaxChunkChars   int     `name:"max-chunk-chars" help:"Semantic mode: split chunk groups larger than this many chars (0 = engine default)"`
	NoClean         bool    `name:"no-clean" help:"Do not auto-clean detected transcript (VTT/SRT) input"`
	Clean           bool    `help:"Force transcript cleaning even when format detection is unsure"`
	File            string  `arg:"" required:""`
}

func (c *chunkCommand) Run(ctx context.Context, out *commandOutput) error {
	return runChunk(newRunContext(ctx, out), []string{c.File}, &chunkFlags{mode: c.Mode, maxTokens: c.MaxTokens, overlap: c.Overlap, outDir: c.OutDir, format: c.Format, threshold: c.Threshold, provider: c.Provider, embeddingModel: c.EmbeddingModel, local: c.Local, smoothingWindow: c.SmoothingWindow, coherenceWindow: c.CoherenceWindow, minChunkChars: c.MinChunkChars, maxChunkChars: c.MaxChunkChars, noClean: c.NoClean, forceClean: c.Clean})
}

type digestCommand struct {
	Style               string   `help:"Target style for the rewrite (default from config)"`
	Out                 string   `help:"Output path for the rewrite (default: <source>.distilled.md)"`
	Facts               string   `help:"Path for the compiled facts checkpoint (default: <artifacts>/facts.compiled.md)"`
	Artifacts           string   `help:"Artifacts directory (default: temp dir)"`
	Model               string   `help:"Text model for extraction and rewrite. Falls back to $DISTILL_MODEL."`
	BaseURL             string   `name:"base-url" help:"Local OpenAI-compatible base URL. Remote custom API endpoints are disabled; use built-in Wormhole providers."`
	Local               bool     `help:"Use the local model profile (local_model/local_base_url) instead of the remote OpenRouter default. The local endpoint gets $DISTILL_LOCAL_API_KEY (never $OPENAI_API_KEY)."`
	Deepseek            bool     `help:"Use the direct DeepSeek profile (deepseek_model/deepseek_base_url) with $DEEPSEEK_API_KEY."`
	ChunkSize           int      `name:"chunk-size" default:"6000" help:"Character budget per chunk (min 1000)"`
	MaxTokens           int      `name:"max-tokens" default:"4000" help:"Hard Claude token ceiling per chunk; oversize character chunks are split (0 disables)"`
	Concurrency         int      `help:"Max parallel chunk extractions (default from config, else 4)"`
	Timeout             int      `help:"Per-LLM-call timeout in seconds (default from config, else 300)"`
	Retries             int      `help:"Per-call retry attempts for outline/section/edit on transient errors (default from config, else 3)"`
	MaxCalls            int      `name:"max-calls" help:"Abort before provider calls if the planned paid-call count exceeds this ceiling (0 disables)"`
	ReuseFacts          bool     `name:"reuse-facts" help:"Reuse an existing compiled-facts checkpoint (skip extraction)"`
	NoClean             bool     `name:"no-clean" help:"Do not auto-clean detected transcript (VTT/SRT) input"`
	Clean               bool     `help:"Force transcript cleaning even when format detection is unsure"`
	Fuse                bool     `help:"Run the fuse stage that merges per-chunk notes before writing (off by default; can time out on large inputs)"`
	NoEdit              bool     `name:"no-edit" help:"Skip the editor stage that polishes the writer draft"`
	Appendix            bool     `help:"Append the verbatim extracted facts as a lossless appendix (recovers tables/ranked lists the prose stage samples away)"`
	Resume              bool     `default:"true" help:"Reuse complete artifacts from a previous run in --artifacts to avoid repeated paid calls (use --resume=false to force regeneration)"`
	DryRun              bool     `name:"dry-run" help:"Plan chunks, role models, endpoints, and artifact paths without making provider calls"`
	NoCache             bool     `name:"no-cache" help:"Skip digest caches: neither read nor store output articles or research responses"`
	ResearchCache       bool     `name:"research-cache" help:"Reuse per-chunk research responses across runs (disabled by --no-cache)"`
	Context             string   `help:"Free-text guidance to steer the rewrite's emphasis/framing (injected into outline/section/edit prompts, never extraction)"`
	ContextFile         string   `name:"context-file" help:"Read steering context from a file (mutually exclusive with --context)"`
	Repair              bool     `help:"After writing, run one targeted reinsert pass for extracted specifics that did not survive into the article (best-of by coverage; never lowers it)"`
	DocContext          bool     `name:"doc-context" help:"Generate a compact document-context header for research prompts (opt-in; extraction remains chunk-grounded)"`
	Cite                bool     `help:"Generate with temporary [F#] fact citations, verify coverage, then strip markers from the final article"`
	Cascade             bool     `help:"Run one extra extraction pass for fresh chunks whose source-specific capture is below the configured threshold"`
	MergeFacts          bool     `name:"merge-facts" help:"Cluster and merge near-duplicate extracted facts before outlining"`
	OutlineFromClusters bool     `name:"outline-from-clusters" help:"Use merged fact clusters to synthesize the outline (requires --merge-facts)"`
	TargetFacts         int      `name:"target-facts" help:"Keep the most representative N extracted facts before writing (0 disables)"`
	CheckPrecision      bool     `name:"check-precision" help:"Judge final article sentences against extracted facts and write responses/precision.json"`
	MinCoverage         float64  `name:"min-coverage" help:"Fail (non-zero exit) if the fraction of extracted specifics surviving in the article is below this ratio (0 disables)"`
	MinCited            float64  `name:"min-cited" help:"Fail if cited fact coverage is below this ratio (0 disables; use with --cite)"`
	MinPrecision        float64  `name:"min-precision" help:"Fail if sentence support precision is below this ratio (implies --check-precision)"`
	CascadeThreshold    float64  `name:"cascade-threshold" help:"Source-specific capture ratio below which --cascade fires (0 uses config cascade_min_capture; 0 after config disables)"`
	MergeThreshold      float64  `name:"merge-threshold" help:"Cosine similarity threshold for --merge-facts clustering (0 uses config merge_facts_threshold)"`
	MaxSections         int      `name:"max-sections" default:"-1" help:"max outline sections in cluster-outline mode (0 = uncapped; unset = config digest.max_sections)"`
	MinWords            int      `name:"min-words" help:"Fail if the final article has fewer than this many words (0 disables)"`
	MaxWords            int      `name:"max-words" help:"Fail if the final article has more than this many words (0 disables)"`
	Paths               []string `arg:"" optional:"" name:"pathspec"`
}

func (c *digestCommand) Run(ctx context.Context, out *commandOutput) error {
	if len(c.Paths) == 0 {
		return errors.New("expected at least one pathspec")
	}
	f := &digestFlags{style: c.Style, out: c.Out, facts: c.Facts, artifacts: c.Artifacts, model: c.Model, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, chunkSize: c.ChunkSize, maxTokens: c.MaxTokens, concurrency: c.Concurrency, timeout: c.Timeout, retries: c.Retries, maxCalls: c.MaxCalls, reuseFacts: c.ReuseFacts, noClean: c.NoClean, forceClean: c.Clean, fuse: c.Fuse, noEdit: c.NoEdit, appendix: c.Appendix, resume: c.Resume, dryRun: c.DryRun, noCache: c.NoCache, researchCache: c.ResearchCache, context: c.Context, contextFile: c.ContextFile, repair: c.Repair, docContext: c.DocContext, cite: c.Cite, cascade: c.Cascade, mergeFacts: c.MergeFacts, outlineFromClusters: c.OutlineFromClusters, targetFacts: c.TargetFacts, checkPrecision: c.CheckPrecision, minCoverage: c.MinCoverage, minCited: c.MinCited, minPrecision: c.MinPrecision, cascadeThreshold: c.CascadeThreshold, mergeThreshold: c.MergeThreshold, maxSections: c.MaxSections, minWords: c.MinWords, maxWords: c.MaxWords}
	return runDigest(newRunContext(ctx, out), c.Paths, f)
}

type movedCommand struct {
	Args []string `arg:"" optional:"" passthrough:""`
}

func (c *movedCommand) Run(kctx *kong.Context) error {
	command := strings.TrimSpace(kctx.Command())
	switch {
	case strings.HasPrefix(command, "eval"):
		return movedError("distill-eval eval")
	case strings.HasPrefix(command, "models"):
		return movedError("distill-eval models")
	case strings.HasPrefix(command, "digest-grade"):
		return movedError("distill-eval grade")
	}
	return errors.New("command moved to the distill-eval binary")
}
func movedError(target string) error {
	return fmt.Errorf("this command moved to the distill-eval binary — run: %s", target)
}

func optionalArg(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
func newRunContext(ctx context.Context, out *commandOutput) *runContext {
	return &runContext{ctx: ctx, in: out.stdin, out: out.stdout, errOut: out.stderr, flags: out.flags}
}

func Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return execute(ctx, args, stdin, stdout, stderr)
}
func execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"--help"}
	}
	var tree distillCLI
	return parseAndRun(ctx, "distill", "Document chunking and token counting CLI", &tree, args, stdin, stdout, stderr)
}

func parseAndRun(ctx context.Context, name, description string, tree any, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if name == "distill" && len(args) >= 2 && args[0] == "digest" {
		switch args[1] {
		case "score":
			args = append([]string{"digest-score"}, args[2:]...)
		case "grade":
			args = append([]string{"digest-grade"}, args[2:]...)
		}
	}
	out := &commandOutput{stdin: stdin, stdout: stdout, stderr: stderr, flags: presentFlags(args)}
	parser, err := kong.New(tree, kong.Name(name), kong.Description(description), kong.Vars{"panel_judges": defaultPanelJudges, "recognize_judges": defaultRecognizeJudges}, kong.Writers(stdout, stderr), kong.BindTo(ctx, (*context.Context)(nil)), kong.Bind(out), kong.ConfigureHelp(kong.HelpOptions{NoExpandSubcommands: true}), kong.Help(distillHelp))
	if err != nil {
		return err
	}
	exited := false
	parser.Exit = func(int) { exited = true }
	parsed, err := parser.Parse(args)
	if exited {
		return nil
	}
	if err != nil {
		return err
	}
	err = parsed.Run()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func distillHelp(options kong.HelpOptions, ctx *kong.Context) error {
	var rendered bytes.Buffer
	stdout := ctx.Stdout
	ctx.Stdout = &rendered
	err := kong.DefaultHelpPrinter(options, ctx)
	ctx.Stdout = stdout
	if err != nil {
		return err
	}
	help := strings.ReplaceAll(rendered.String(), "distill digest-score", "distill digest score")
	help = restoreCommandHelp(help, ctx.Command())
	if ctx.Command() == "digest" {
		help += "\nCommands:\n  score    Deterministic review of digest drafts (no LLM)\n"
	}
	_, err = fmt.Fprint(stdout, help)
	return err
}

func restoreCommandHelp(rendered, command string) string {
	prefix := "distill:"
	if strings.HasPrefix(rendered, "Usage: distill-eval") {
		prefix = "eval:"
	}
	command = strings.TrimSpace(command)
	key := prefix + command
	meta, ok := commandHelpOverrides[key]
	if !ok {
		meta, ok = commandHelpCatalog[key]
	}
	if !ok {
		return rendered
	}
	help := rendered
	if meta.Long != "" {
		if start := strings.Index(help, "\n\n"); start >= 0 {
			content := start + 2
			if end := strings.Index(help[content:], "\n\n"); end >= 0 {
				help = help[:start] + "\n\n" + help[content+end+2:]
			}
		}
	}
	if meta.Long == "" && meta.Short != "" && !strings.Contains(help, meta.Short) {
		help += "\nSummary:\n" + meta.Short + "\n"
	}
	if meta.Long != "" && !strings.Contains(help, meta.Long) {
		help += "\nDescription:\n" + meta.Long + "\n"
	}
	if meta.Example != "" {
		help += "\nExamples:\n" + meta.Example + "\n"
	}
	return help
}

// commandHelpOverrides contains deliberate corrections to the generated
// pre-Kong catalog. All other command prose comes from commandHelpCatalog.
var commandHelpOverrides = map[string]commandHelp{
	"distill:count": {
		Short: "Count tokens, characters, and lines",
		Long:  "Count tokens (Claude tokenizer), characters, and lines in a file or stdin.",
	},
}

func presentFlags(args []string) map[string]bool {
	flags := map[string]bool{}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			name := strings.TrimPrefix(arg, "--")
			if i := strings.IndexByte(name, '='); i >= 0 {
				name = name[:i]
			}
			flags[name] = true
		}
	}
	return flags
}
