# distill

Document chunking, estimated token counting, and fact-preserving distillation CLI for LLM processing. Uses `cl100k_base` for remote-profile preflight estimates and Wormhole provider integrations for embeddings and distillation.

## Install

distill uses the pinned remote `reliquary` chunking engine, so no sibling
checkout is required. Build both binaries directly:

```bash
# Build both binaries
go build -o distill .
go build -o distill-eval ./cmd/distill-eval
ln -sf "$(pwd)/distill" ~/go/bin/distill
ln -sf "$(pwd)/distill-eval" ~/go/bin/distill-eval

# …or just: build + symlink both
just install
```

Wormhole, the LLM SDK, is also a pinned remote dependency and needs no local
checkout.

## Commands

distill ships as **two binaries** that share the same `~/.config/distill/config.yaml` (models, base URL, per-role picks):

**`distill`** — the document pipeline:

- `count` — Count tokens, characters, and lines.
- `chunk` — Split documents into chunks.
- `digest` — Distill documents into a fact-preserving article; includes the deterministic `digest score` self-check.

**`distill-eval`** — evaluation & model benchmarking:

- `eval` — Score extractions: `eval judge` (LLM precision/recall/F1), `eval facts` (deterministic golden-fact scoring, no LLM), and `eval structured` (schema-backed JSON scoring, no LLM).
- `grade` — Grade digests on merit via the pairwise judge (`calibrate`, `tournament`, `recognize`, `panel`). *(Was `distill digest grade`.)*
- `models` — Rank models on a task: `models rankings` (public leaderboards), `models code`, `models trace-go`, `models comedy`, `models label`.

Old `distill eval` / `distill models` / `distill digest grade` invocations still resolve, but print a one-line pointer to the new `distill-eval` path.

### count

Count tokens, characters, and lines in a file or stdin.

```bash
# From file
distill count document.md

# From stdin
cat document.md | distill count

# Plain text output
distill count --format plain document.md
```

Output (JSON by default):

```json
{"tokens":1234,"chars":5678,"lines":42}
```

### chunk

Split a document into chunks using one of three strategies.

```bash
# Default: split at markdown headings
distill chunk document.md

# Dense sentence packing
distill chunk --mode cramit --max-tokens 2000 document.md

# Semantic splitting through a Wormhole provider
export OPENROUTER_API_KEY=sk-or-...
distill chunk --mode semantic --embedding-model qwen/qwen3-embedding-8b --threshold 0.3 document.md

# Custom output directory
distill chunk --out-dir ./chunks document.md
```

Writes numbered `.md` files and a `manifest.json` to the output directory.

#### Modes

| Mode | Strategy | Requires |
|------|----------|----------|
| `headings` | Splits at markdown headings (H1-H6), recurses into sub-headings when a section exceeds max tokens | Nothing |
| `semantic` | Embeds paragraphs via a built-in Wormhole provider, splits at low cosine-similarity boundaries. Embeddings are cached on disk (keyed by model) so repeated runs skip re-embedding. | provider API key, `--embedding-model` |
| `cramit` | Greedy sentence packing to fill chunks to max tokens | Nothing |

Modes fall back to dense boundary packing when content can't be split further by their native strategy.

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `headings` | Chunking strategy |
| `--max-tokens` | `4000` | `cl100k_base` preflight budget per chunk; local Qwen mode uses only the derived character budget |
| `--overlap` | `200` | Tokens of overlap between chunks |
| `--out-dir` | temp dir | Output directory |
| `--threshold` | `0.3` | Similarity threshold (semantic mode) |
| `--embedding-model` | `$DISTILL_EMBEDDING_MODEL` | Embedding model for semantic mode (default `qwen/qwen3-embedding-8b` on OpenRouter) |
| `--local` | — | Use the on-box Qwen embedding profile (`local_*` config keys) instead of the OpenRouter default |
| `--min-chunk-chars` / `--max-chunk-chars` | engine default | Semantic chunk size bounds |
| `--smoothing-window` / `--coherence-window` | engine default | Semantic boundary-detection tuning |
| `--format` | `json` | Manifest output format |

#### Manifest

The `manifest.json` tracks chunk metadata:

```json
{
  "source": "document.md",
  "mode": "headings",
  "total_chunks": 3,
  "total_tokens": 8500,
  "chunks": [
    {"file": "001.md", "tokens": 3200, "start_line": 1, "end_line": 45},
    {"file": "002.md", "tokens": 2800, "start_line": 44, "end_line": 89},
    {"file": "003.md", "tokens": 2500, "start_line": 88, "end_line": 120}
  ]
}
```

Lines overlap between chunks when `--overlap` is set.

### Transcript cleaning

`digest` and `chunk` auto-detect YouTube/auto-caption transcript input (WebVTT
or SubRip) and clean it before processing: the `WEBVTT` header, cue numbers,
timing lines, inline `<HH:MM:SS.mmm>` timestamps, `<c>` styling tags, and
`[Music]`/`[Applause]` bracket labels are stripped; the auto-caption
triplication / rolling-overlap duplication is deduplicated; and the ~5-word
fragments are reflowed into paragraphs (split on timing gaps, else a fixed
line window). Cleaning is deterministic and offline — no LLM, no network.

Detection never false-positives on ordinary markdown. Override with
`--no-clean` (process raw) or `--clean` (force). `count` always counts the
raw source and is never cleaned.

### digest

Distill a long document into a fact-preserving article. Rather than asking one
model call to summarize the whole source, `digest` runs a four-role pipeline —
research (extract atomic facts from each chunk), fuse (merge the notes), write
(compose a narrative draft in a target style), then edit (polish it) — so no
single call ever holds the entire document.

```bash
export OPENROUTER_API_KEY=sk-or-...   # or OPENAI_API_KEY; omit only with --local against a no-auth endpoint
distill digest document.md \
  --style "concise technical brief" \
  --out document.distilled.md

# Reuse the compiled-facts checkpoint for a free style-only re-run
distill digest document.md --reuse-facts --facts artifacts/facts.compiled.md

# Run against the on-box Qwen profile instead of the OpenRouter default
distill digest document.md --local

# Force the whole run directly against DeepSeek
export DEEPSEEK_API_KEY=sk-...
distill digest document.md --deepseek
```

Uses Wormhole provider integrations and a text model (`--model` /
`$DISTILL_MODEL`, default `z-ai/glm-5.2`, with per-role `*_model` config keys
overriding each stage). Non-prefixed remote models use OpenRouter; DeepSeek,
Z.AI/GLM, and Gemini model IDs route directly to their built-in Wormhole
providers. Remote custom API endpoints are disabled. Pass `--local` to switch to
the on-box Qwen profile (`http://127.0.0.1:8000/v1`), where `--base-url` /
`$DISTILL_BASE_URL` may point at a local OpenAI-compatible server, or `--deepseek`
to force the whole run to the direct DeepSeek profile (`deepseek_model` /
`deepseek_base_url`). Direct DeepSeek calls normalize OpenRouter-style slugs such as
`deepseek/deepseek-v4-pro` to `deepseek-v4-pro`, send `thinking: disabled` by
default for normal completions, and include a stable hashed `user_id` for digest
runs so DeepSeek can isolate/cache related requests without exposing local paths.
See `docs/deepseek-provider.md` for the request shape, cache probe, and
maintenance checks.

For the current high-quality long-document recipe, see
`docs/polished-mode.md`. It combines cascade extraction, fact merging,
cluster-derived outlines, temporary citations, repair, precision checks, and
quality gates using existing `digest` flags.

Outputs: the final article (`--out`, default `<source>.distilled.md`), the
`facts.compiled.md` checkpoint, the fused notes (`facts.fused.md`, only when
`--fuse` is enabled), the stage-level `run-ledger.jsonl` call/reuse ledger, and
per-chunk artifacts (`chunks/`, `responses/`, including the writer's `draft.md`)
under the artifacts directory. The research, fuse, write, and editor prompts live in
`~/.config/distill/prompts/` and are created on first run — edit them to tune behavior.

#### digest flags

| Flag | Default | Description |
|------|---------|-------------|
| `--model` | `$DISTILL_MODEL` | Top-level text model fallback; per-role `*_model` config keys override per stage (default `z-ai/glm-5.2`) |
| `--base-url` | local profile only | Local OpenAI-compatible endpoint when `--local` is set; remote custom endpoints are disabled |
| `--local` | — | Use the on-box Qwen profile (`local_*` config keys) instead of the OpenRouter default |
| `--deepseek` | — | Force the direct DeepSeek profile (`deepseek_*` config keys) with `$DEEPSEEK_API_KEY` |
| `--style` | narrative prose | Writer style; a preset name (narrative/brief/faq/reference) or free text |
| `--context` | — | Free-text guidance to steer the rewrite's emphasis/framing (injected into outline/section/edit prompts, never extraction) |
| `--context-file` | — | Read steering context from a file (mutually exclusive with `--context`) |
| `--out` | `<source>.distilled.md` | Final article output path |
| `--facts` | `<artifacts>/facts.compiled.md` | Compiled-facts checkpoint path |
| `--artifacts` | temp dir | Artifacts directory |
| `--chunk-size` | `6000` | Character budget per chunk (min 1000) |
| `--max-tokens` | `4000` | `cl100k_base` preflight budget per chunk for remote profiles; local Qwen uses the character budget (`0` disables) |
| `--concurrency` | `4` | Max parallel chunk extractions |
| `--no-clean` / `--clean` | auto | Skip / force transcript (VTT/SRT) cleaning before distillation |
| `--timeout` | `300` | Per-LLM-call timeout in seconds |
| `--retries` | config, else `3` | Per-call retry attempts for outline/section/edit on transient errors |
| `--dry-run` | `false` | Plan chunks, role models, endpoints, artifact paths, ledger path, and provider-call count without making provider calls |
| `--max-calls` | `0` | Abort before provider calls if the planned paid-call count exceeds this ceiling |
| `--reuse-facts` | `false` | Reuse the facts checkpoint (skip research) |
| `--resume` | `true` | Reuse complete artifacts from `--artifacts` to avoid repeated paid calls after an interrupted run; pass `--resume=false` to force regeneration |
| `--fuse` | `false` | Run the fuse stage that merges per-chunk notes before writing (off by default; can time out on large inputs) |
| `--merge-facts` | `false` | Cluster and merge similar extracted fact bullets before planning; near-duplicates with explicit fact markers merge only when source fact IDs remain represented |
| `--outline-from-clusters` | `false` | Build the outline from merged fact clusters instead of a free-form outline call (requires `--merge-facts`) |
| `--max-sections` | config `max_sections` | Section cap for cluster-outline mode; stub clusters are merged first and oversized separable clusters may split, so section sizes are balanced but not guaranteed equal |
| `--no-edit` | `false` | Skip the editor stage (output the writer's draft) |
| `--appendix` | `false` | Append the verbatim extracted facts as a lossless appendix (recovers tables/ranked lists the prose stage samples away) |

### eval judge

Score one or more candidate extractions against a reference, chunk by chunk,
using an LLM judge (the source chunk is ground truth; the reference is a coverage
checklist). Useful for comparing how well different models extract facts in the
digest pipeline.

```bash
# Compare two models' extractions against a trusted reference
distill-eval eval judge \
  --chunks      runA/chunks \
  --reference   runA/responses \
  --candidates  runB/responses,runC/responses \
  --judge-model gpt-4o \
  --out         evals/
```

Inputs are the artifact directories `distill digest` writes (`chunks/`,
`responses/`). Outputs under `<out>/evaluations/`:
- `<candidate>/judgments.jsonl` — per-chunk verdicts (SUPPORTED / CONTRADICTED / NOT_IN_SOURCE) plus missed reference facts
- `<candidate>/summary.md` — per-chunk and aggregate precision/recall/F1 (micro-average)
- `INDEX.md` — all candidates ranked by F1

The judge model resolves from `--judge-model`, then `$DISTILL_MODEL`, then config. DeepSeek, Z.AI/GLM, and Gemini judge model IDs use their direct Wormhole providers; `--base-url` is local-only.

### eval structured

Score one or more JSON extraction candidates against a JSON Schema and gold JSON
file, deterministically and without an LLM. Structural compliance is a hard
gate: invalid JSON, type mismatches, missing required fields, and disallowed
extra fields zero the aggregate scores while still writing field-level outcomes
when possible.

```bash
distill-eval eval structured \
  --schema     schema.json \
  --gold       gold.json \
  --candidates model-a.json,model-b.json \
  --out        structured-evals/
```

Supported schema evaluation metrics include exact strings, case-insensitive
strings, fuzzy strings, URL-normalized strings, exact/tolerant numbers, exact
integers, booleans, and exact array item matching with matched/missed/spurious
counts. Outputs include `INDEX.md`, `<candidate>.summary.md`, and
`<candidate>.report.json`.

### models trace-go

Rank models by exact stdout prediction on deterministic, trusted single-file Go
programs. The fixture programs are executed locally to verify or generate gold;
model output is never executed. Model calls use greedy decoding (`temperature=0`)
and the scorer compares the last non-empty model output line to byte-exact stdout
gold. Console progress prints each task as it completes; HTML reports include
per-task predictions and errors.

```bash
distill-eval models trace-go \
  --models local \
  --base-url http://127.0.0.1:8000/v1 \
  --only int_div_floor,map_stable \
  --skip map_stable \
  --task-timeout 30 \
  --out docs/go-trace-routing.html
```

Use only trusted fixtures. The default fixture is
`testdata/trace-go/tasks.json`; override it with `--fixtures`. Use `--only`
and `--skip` with comma-separated task IDs to isolate slow or suspicious cases.
