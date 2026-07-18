package cmd

import "context"

type evalCommand struct {
	Chunks       string `help:"Source chunks directory (chunk-NNN.md) [required]"`
	Reference    string `help:"Reference extraction responses directory [required]"`
	Candidates   string `help:"Comma-separated candidate response directories [required]"`
	JudgeModel   string `name:"judge-model" help:"Judge model for the built-in endpoint (falls back to $DISTILL_MODEL or config)"`
	JudgeCmd     string `name:"judge-cmd" help:"External judge command run with the prompt on stdin (e.g. \"codex exec -\"). Overrides --judge-model/--base-url."`
	BaseURL      string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local        bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek     bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Out          string `help:"Output directory for evaluations (default: temp dir)"`
	JudgeTimeout int    `name:"judge-timeout" default:"300" help:"Per-judge-call timeout in seconds for --judge-cmd (0 = no timeout)"`
}

func (c *evalCommand) Run(ctx context.Context, o *commandOutput) error {
	return runEval(newRunContext(ctx, o), &evalFlags{chunks: c.Chunks, reference: c.Reference, candidates: c.Candidates, judgeModel: c.JudgeModel, judgeCmd: c.JudgeCmd, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, out: c.Out, judgeTimeout: c.JudgeTimeout})
}

type scoreCommand struct {
	Expected   string `default:"testdata/extraction/expected" help:"Directory of chunk-*.json golden-fact fixtures"`
	Candidates string `help:"Comma-separated per-chunk responses directories to score [required]"`
	Out        string `help:"Output directory for INDEX.md and <name>.summary.md (default: stdout only)"`
}

func (c *scoreCommand) Run(ctx context.Context, o *commandOutput) error {
	return runScore(newRunContext(ctx, o), &scoreFlags{expected: c.Expected, candidates: c.Candidates, out: c.Out})
}

type structuredCommand struct {
	Schema     string `help:"JSON Schema file with optional evaluation_config fields [required]"`
	Gold       string `help:"Gold JSON file [required]"`
	Candidates string `help:"Comma-separated candidate JSON files to score [required]"`
	Out        string `help:"Output directory for INDEX.md, summaries, and JSON reports (default: stdout only)"`
}

func (c *structuredCommand) Run(ctx context.Context, o *commandOutput) error {
	return runStructured(newRunContext(ctx, o), &structuredFlags{schema: c.Schema, gold: c.Gold, candidates: c.Candidates, out: c.Out})
}

type optimizeCommand struct {
	Chunks       string `default:"testdata/extraction/chunks" help:"Directory of chunk-*.md source chunks"`
	Expected     string `default:"testdata/extraction/expected" help:"Directory of chunk-*.json golden-fact fixtures"`
	Seed         string `help:"Seed prompt path (default: configured research prompt)"`
	Out          string `help:"Output directory for score-log.jsonl, candidates, and best-prompt.md"`
	Iterations   int    `default:"5" help:"Mutation iterations to attempt"`
	BudgetCalls  int    `name:"budget-calls" help:"Maximum model calls across mutation and evaluation (0 disables)"`
	Concurrency  int    `default:"4" help:"Maximum parallel candidate extraction calls"`
	Model        string `help:"Extractor model override"`
	MutatorModel string `name:"mutator-model" help:"Mutator model override (default: --model/config model)"`
	Holdout      string `help:"Optional holdout expected-dir scored but not used for acceptance"`
}

func (c *optimizeCommand) Run(ctx context.Context, o *commandOutput) error {
	return runOptimize(ctx, &Deps{}, optimizeFlags{chunks: c.Chunks, expected: c.Expected, seed: c.Seed, out: c.Out, iterations: c.Iterations, budgetCalls: c.BudgetCalls, concurrency: c.Concurrency, model: c.Model, mutatorModel: c.MutatorModel, holdout: c.Holdout})
}

type digestScoreCommand struct {
	Expected    string  `default:"testdata/extraction/expected" help:"Directory of chunk-*.json golden-fact fixtures"`
	Checks      string  `default:"testdata/extraction/digest-checks.json" help:"Doc-specific tension/hygiene checks JSON"`
	Source      string  `default:"testdata/extraction/source.md" help:"Source document, for the verbatim-overlap (copying) signal"`
	Digests     string  `default:"runs/write-bakeoff" help:"Directory whose */digest.md files are reviewed"`
	Out         string  `help:"Write a self-contained HTML report to this path"`
	CopyPenalty float64 `name:"copy-penalty" default:"2" help:"Composite score = recall - copyPenalty*overlap (higher punishes copiers harder)"`
}

func (c *digestScoreCommand) Run(ctx context.Context, o *commandOutput) error {
	return runDigestScore(newRunContext(ctx, o), &digestScoreFlags{expected: c.Expected, checks: c.Checks, source: c.Source, digests: c.Digests, out: c.Out, copyPenalty: c.CopyPenalty})
}

type calibrateCommand struct {
	Digests       string `default:"runs/write-bakeoff" help:"Directory whose */digest.md files are calibrated against"`
	Source        string `default:"testdata/extraction/source.md" help:"Source document (for fidelity grounding + the vs-source pair)"`
	Limit         int    `default:"5" help:"Calibrate on the first N digests (alphabetical); 0 = all"`
	Models        string `help:"Comma-separated digest dir names to calibrate (overrides --limit)"`
	JudgeModel    string `name:"judge-model" help:"Judge model (falls back to $DISTILL_MODEL or config)"`
	BaseURL       string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local         bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek      bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	IncludeSource bool   `name:"include-source" help:"Also pit each digest against the raw source brief"`
}

func (c *calibrateCommand) Run(ctx context.Context, o *commandOutput) error {
	return runCalibrate(newRunContext(ctx, o), &digestGradeFlags{digests: c.Digests, source: c.Source, limit: c.Limit, models: c.Models, judgeModel: c.JudgeModel, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, includeSource: c.IncludeSource})
}

type tournamentCommand struct {
	Digests    string `default:"runs/write-bakeoff" help:"Directory whose */digest.md files are ranked"`
	Source     string `default:"testdata/extraction/source.md" help:"Source document (for fidelity grounding + overlap)"`
	Expected   string `default:"testdata/extraction/expected" help:"Golden-fact fixtures dir (for the candidate gate)"`
	Checks     string `default:"testdata/extraction/digest-checks.json" help:"Doc-specific checks + gate thresholds JSON"`
	Top        int    `help:"With --models/--all: cap to first N; default gate selects candidates automatically"`
	Models     string `help:"Explicit comma-separated digest dir names IN SEED ORDER (bypasses the gate)"`
	All        bool   `help:"Rank the WHOLE field, skipping the candidate gate (expensive)"`
	JudgeModel string `name:"judge-model" help:"Judge model (falls back to $DISTILL_MODEL or config)"`
	BaseURL    string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local      bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek   bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Audit      int    `default:"12" help:"Number of random triples to audit for transitivity (cycle rate)"`
	Out        string `help:"Write a self-contained HTML report to this path"`
	DryRun     bool   `name:"dry-run" help:"Print the gated candidates + estimated judge-call count and exit (no judging, no spend)"`
}

func (c *tournamentCommand) Run(ctx context.Context, o *commandOutput) error {
	return runTournament(newRunContext(ctx, o), &digestTourFlags{digests: c.Digests, source: c.Source, expected: c.Expected, checks: c.Checks, top: c.Top, models: c.Models, all: c.All, judgeModel: c.JudgeModel, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, audit: c.Audit, out: c.Out, dryRun: c.DryRun})
}

type recognizeCommand struct {
	Digests  string `default:"runs/write-bakeoff" help:"source dir of <model>/digest.md"`
	Judges   string `default:"${recognize_judges}" help:"comma-separated judge MODEL IDS"`
	Trials   int    `default:"8" help:"Number of recognition trials per judge"`
	SetSize  int    `name:"set-size" default:"4" help:"Number of anonymized digests shown per trial"`
	Seed     int64  `default:"1" help:"Random seed"`
	BaseURL  string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Deepseek bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Out      string `help:"optional HTML report path"`
	DryRun   bool   `name:"dry-run" help:"Print estimated judge-call count and exit (no judging, no spend)"`
}

func (c *recognizeCommand) Run(ctx context.Context, o *commandOutput) error {
	return runRecognize(newRunContext(ctx, o), &digestRecognizeFlags{digests: c.Digests, judges: c.Judges, trials: c.Trials, setSize: c.SetSize, seed: c.Seed, baseURL: c.BaseURL, deepseek: c.Deepseek, out: c.Out, dryRun: c.DryRun})
}

type panelCommand struct {
	Digests   string `default:"runs/write-bakeoff" help:"Directory whose */digest.md files are ranked"`
	Source    string `default:"testdata/extraction/source.md" help:"Source document (for fidelity grounding + overlap)"`
	Expected  string `default:"testdata/extraction/expected" help:"Golden-fact fixtures dir (for the candidate gate)"`
	Checks    string `default:"testdata/extraction/digest-checks.json" help:"Doc-specific checks + gate thresholds JSON"`
	Top       int    `help:"With --models/--all: cap to first N; default gate selects candidates automatically"`
	Models    string `help:"Explicit comma-separated digest dir names IN SEED ORDER (bypasses the gate)"`
	Criterion string `default:"merit" help:"Judge criterion: merit | comedy | publish"`
	All       bool   `help:"Rank the WHOLE field, skipping the candidate gate (expensive)"`
	BaseURL   string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local     bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek  bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Audit     int    `default:"12" help:"Number of random triples to audit for transitivity (cycle rate)"`
	Out       string `help:"Write a self-contained HTML report to this path"`
	DryRun    bool   `name:"dry-run" help:"Print the gated candidates + estimated judge-call count and exit (no judging, no spend)"`
	Seed      int64  `default:"1" help:"Random seed"`
	Judges    string `default:"${panel_judges}" help:"comma-separated judge MODEL IDS"`
}

func (c *panelCommand) Run(ctx context.Context, o *commandOutput) error {
	return runPanel(&Deps{}, newRunContext(ctx, o), &digestPanelFlags{digests: c.Digests, source: c.Source, expected: c.Expected, checks: c.Checks, top: c.Top, models: c.Models, criterion: c.Criterion, all: c.All, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, audit: c.Audit, out: c.Out, dryRun: c.DryRun, seed: c.Seed, judges: c.Judges})
}

type codeCommand struct {
	Problems    string `default:"testdata/code/problems.json" help:"Coding-problems fixture ({problems})"`
	Models      string `help:"Comma-separated model roster [required]"`
	BaseURL     string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local       bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek    bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Out         string `help:"Write a self-contained HTML report to this path"`
	Concurrency int    `default:"4" help:"Max models evaluated concurrently"`
	Timeout     int    `default:"60" help:"Per-problem timeout in seconds"`
	Seed        int64  `default:"1" help:"Random seed (advisory)"`
	AllowExec   bool   `name:"i-understand-code-execution" help:"REQUIRED: acknowledge this compiles+runs model-generated code on your machine"`
}

func (c *codeCommand) Run(ctx context.Context, o *commandOutput) error {
	return runCode(newRunContext(ctx, o), &codeFlags{problems: c.Problems, models: c.Models, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, out: c.Out, concurrency: c.Concurrency, timeout: c.Timeout, seed: c.Seed, allowExec: c.AllowExec})
}

type comedyCommand struct {
	Topics      string `default:"testdata/comedy/topics.json" help:"Comedy topics fixture ({style, topics})"`
	Models      string `help:"Comma-separated model roster that writes the bits [required]"`
	Judges      string `default:"${panel_judges}" help:"comma-separated judge MODEL IDS (leave-one-out panel)"`
	BaseURL     string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local       bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek    bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Out         string `help:"Write a self-contained HTML report to this path"`
	Concurrency int    `default:"4" help:"Max models generating bits concurrently"`
	Timeout     int    `default:"300" help:"Per-model generation timeout in seconds (0 = no timeout)"`
	Audit       int    `default:"12" help:"Number of random triples to audit for transitivity (cycle rate)"`
	Seed        int64  `default:"1" help:"Random seed (advisory)"`
}

func (c *comedyCommand) Run(ctx context.Context, o *commandOutput) error {
	return runComedy(newRunContext(ctx, o), &comedyFlags{topics: c.Topics, models: c.Models, judges: c.Judges, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, out: c.Out, concurrency: c.Concurrency, timeout: c.Timeout, audit: c.Audit, seed: c.Seed})
}

type labelCommand struct {
	Task        string `help:"Task: classification or sentiment [required]"`
	Fixtures    string `help:"Label fixture JSON ({task, allowed_labels, items}) [required]"`
	Models      string `help:"Comma-separated model roster to evaluate [required]"`
	BaseURL     string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local       bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek    bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Seed        int    `help:"Seed passed through for reproducibility (advisory; SDK applies where supported)"`
	Out         string `help:"Write a self-contained HTML routing report to this path"`
	Concurrency int    `default:"4" help:"Max concurrent per-item model calls"`
	Timeout     int    `default:"300" help:"Per-run timeout in seconds (0 = no timeout)"`
}

func (c *labelCommand) Run(ctx context.Context, o *commandOutput) error {
	return runLabel(newRunContext(ctx, o), &labelFlags{task: c.Task, fixtures: c.Fixtures, models: c.Models, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, seed: c.Seed, out: c.Out, concurrency: c.Concurrency, timeout: c.Timeout})
}

type traceCommand struct {
	Fixtures      string `default:"testdata/trace-go/tasks.json" help:"Trace fixture JSON ({tasks})"`
	Models        string `help:"Comma-separated model roster [required]"`
	BaseURL       string `name:"base-url" help:"Local OpenAI-compatible base URL; remote custom endpoints are disabled"`
	Local         bool   `help:"Use the local model profile instead of the remote OpenRouter default."`
	Deepseek      bool   `help:"Use the direct DeepSeek profile with $DEEPSEEK_API_KEY."`
	Only          string `help:"Comma-separated task IDs to run (default: all tasks)"`
	Skip          string `help:"Comma-separated task IDs to skip after --only is applied"`
	Out           string `help:"Write a self-contained HTML routing report to this path"`
	Concurrency   int    `default:"4" help:"Max concurrent per-task model calls"`
	Timeout       int    `default:"300" help:"Per-model timeout in seconds (0 = no timeout)"`
	TaskTimeout   int    `name:"task-timeout" default:"60" help:"Per-task model-call timeout in seconds (0 = no timeout)"`
	VerifyTimeout int    `name:"verify-timeout" default:"10" help:"Per-fixture go-run timeout in seconds (0 = no timeout)"`
}

func (c *traceCommand) Run(ctx context.Context, o *commandOutput) error {
	return runTrace(newRunContext(ctx, o), &traceFlags{fixtures: c.Fixtures, models: c.Models, baseURL: c.BaseURL, local: c.Local, deepseek: c.Deepseek, only: c.Only, skip: c.Skip, out: c.Out, concurrency: c.Concurrency, timeout: c.Timeout, taskTimeout: c.TaskTimeout, verifyTimeout: c.VerifyTimeout})
}

type rankingsFetchCommand struct {
	Render  bool   `default:"true" help:"Pass --render to defuddle for boards configured with render: true."`
	Timeout int    `default:"90" help:"Per-board timeout in seconds."`
	Board   string `help:"Fetch only this board key."`
}

func (c *rankingsFetchCommand) Run(ctx context.Context, o *commandOutput) error {
	return runRankingsFetch(newRunContext(ctx, o), c.Render, c.Timeout, c.Board)
}

type rankingsApplyCommand struct {
	DryRun bool `name:"dry-run" help:"Show changes without writing config.yaml."`
}

func (c *rankingsApplyCommand) Run(ctx context.Context, o *commandOutput) error {
	return runRankingsApply(newRunContext(ctx, o), c.DryRun)
}

type rankingsShowCommand struct {
	Local bool `help:"Use the local model profile instead of the remote OpenRouter default."`
}

func (c *rankingsShowCommand) Run(ctx context.Context, o *commandOutput) error {
	return runRankingsShow(newRunContext(ctx, o), c.Local)
}
