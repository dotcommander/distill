# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is distill?

A CLI tool for splitting documents into chunks, counting tokens, and distilling documents into fact-preserving rewrites for LLM processing. Built with Go 1.26 and Kong.

## Commands

```bash
# Build
go build -o distill .
go build -o distill-eval ./cmd/distill-eval

# Run tests
go test ./...

# Run a single test
go test -run TestCountStdin .

# Run internal package tests
go test ./internal/...

# Install
go build -o distill . && ln -sf "$(pwd)/distill" ~/go/bin/distill
go build -o distill-eval ./cmd/distill-eval && ln -sf "$(pwd)/distill-eval" ~/go/bin/distill-eval
```

## Architecture

**CLI layer** (`cmd/`) — two Kong binaries share the same `cmd` package and internal packages: `distill` is wired via `Execute`, and `distill-eval` via `ExecuteEval` (no globals/`init()`):

`distill`:
- `count` — Counts estimated tokens (`cl100k_base`), characters, and lines from file or stdin. Outputs JSON or plain text.
- `chunk` — Splits documents into chunks. Three modes: `headings`, `semantic`, `cramit`. Writes numbered `.md` files + `manifest.json` to an output directory.
- `digest` — Distills a document into a fact-preserving article via a four-role pipeline: research (extract atomic facts per chunk) → fuse (merge notes) → write (narrative draft) → edit (polish); also owns the deterministic offline `digest score` self-check. See `internal/actions/digest`.

`distill-eval`:
- `eval` — Parent for `eval judge` (LLM-judged precision/recall/F1, ranked INDEX), `eval facts` (deterministic golden scorer), `eval structured` (schema-backed JSON scoring), and `eval optimize` (tune the research prompt against golden fixtures). See `internal/actions/eval`.
- `grade` — Merit and pairwise tournament grading commands; was `digest grade`.
- `models` — Model-benchmark harnesses for `models rankings`, `models code`, `models trace-go`, `models comedy`, and `models label`.

**Chunking engine** — provided by the external `github.com/dotcommander/reliquary/pipeline/chunking` package (NOT in-repo). `cmd/chunk.go` maps `--mode` to a reliquary `Strategy` (`headings`→`HeadingAware`, `cramit`→`SentenceBoundary`, `semantic`→`SemanticChunker`), then applies an optional `cl100k_base` preflight budget for remote profiles. Local Qwen flows use character budgets because Distill does not own the model-distributed tokenizer.

**AI layer** (`internal/ai/`) — wraps `github.com/garyblankenship/wormhole` for both text completion (`Complete`, used by `digest`) and batch embeddings (`EmbedBatch`, used by semantic chunking). Provider/API-shape fixes belong upstream in Wormhole; distill should select built-in Wormhole providers and must not add custom provider protocol code.

> **Default models, `--local`, and direct-provider routing:** the committed config default (`internal/config/defaults/config.yaml`) uses OpenRouter for non-prefixed remote text models and semantic embeddings. Text model IDs that identify DeepSeek (`deepseek/...` or `deepseek-*`), Z.AI/GLM (`z-ai/...`, `zai/...`, `glm-*`), or Gemini (`google/gemini-*`, `gemini-*`) route to direct built-in Wormhole providers using their provider API keys. Remote custom API endpoints are forbidden; `--base-url`, `$DISTILL_BASE_URL`, and `$DISTILL_EMBEDDING_ENDPOINT` are local-only escape hatches with `--local`. Direct DeepSeek completions send `thinking: disabled` by default for efficiency and temperature control; digest also sends a stable hashed `user_id` so DeepSeek can isolate/cache related requests without local path disclosure. **Local models are strictly opt-in: pass `--local`** to switch AI text commands plus `chunk --mode semantic` to the on-box Qwen profile (`Qwen3.6-35B-A3B-oQ4-fp16-mtp` / `Qwen3-Embedding-4B-mxfp8` at `http://127.0.0.1:8000/v1`). Pass `--deepseek` only when the whole text-generation/judging command should use the direct DeepSeek profile. If an eval needs a second extraction set, vary the prompt/chunking/temperature, not the model.

> **DeepSeek QA note:** direct DeepSeek has no separate batch-completions endpoint for this workload; use digest's `--concurrency` for client-side parallelism. A live cache probe on a 6 KB input document showed the second identical request hitting 1280 prompt-cache tokens after a cold first call. Keep existing-user config compatibility by preserving the embedded-default merge before user config overlay. Details and commands: `docs/deepseek-provider.md`.

> **Model selection is remote-eval-primary.** The per-role model choices (`research_model`/`fuse_model`/`write_model`/`edit_model`/`judge_model` in `config.yaml`) are derived from public benchmark leaderboards (EQ-Bench, Vectara HHEM, llm-stats Open LLM, Aider Polyglot) cross-referenced against the cost-capped OpenRouter roster (`runs/roster.sh`). distill's own local evals (`eval facts`, `grade` merit/panel tournaments, `models comedy`, `models label`, and `models code`) are now a **SECONDARY / fallback** signal — used only when the public boards don't cover a roster model or can't decide a role. Process of record: `docs/remote-eval-crosswalk.md`.

**Embedding cache** (`internal/embedcache/`) — disk cache wrapping the embedder, keyed by `sha256(model‖text)` under `os.UserCacheDir()/distill/embeddings/`, so repeated semantic runs skip re-embedding.

**Prompts** (`internal/prompts/`) — research/fuse/write/editor/judge templates, embedded as defaults and materialized to `~/.config/distill/prompts/` on first run (config data, not Go literals).

**Manifest** (`internal/manifest/`) — JSON manifest (snake_case keys) written alongside chunk files, tracking source, mode, chunk metadata, and totals. Remote preflight manifests identify `cl100k_base`; local-model manifests set `token_counts_available: false` and omit unavailable token totals.

**Tokenizer** (`internal/tokenizer/`) — code-owned tokenizer interface backed by the canonical `pkoukk/tiktoken-go` `cl100k_base` estimator for `count`, chunk preflight, and manifest totals. It estimates Claude usage; provider-returned usage is authoritative when available.

**FS helper** (`internal/fsutil/`) — `WriteFile` for atomic temp+fsync+rename durable writes.

**Transcript cleaning** (`internal/transcript/`) — deterministic, offline detection and cleaning of YouTube auto-caption transcripts (VTT/SRT). `Detect` classifies input; `Clean` drops the WEBVTT header / cue numbers / timing lines, strips inline timestamps, `<c>` tags, and `[bracket]` labels, deduplicates the auto-caption triplication / rolling overlap, then reflows fragments into paragraphs (timing-gap or fixed-line-window boundaries). Wired into `digest` and `chunk` via `cmd/maybeCleanTranscript` (auto-clean on detect; `--no-clean`/`--clean` override). `count` stays raw. No LLM, no network, stdlib only.

**Polished digest recipe** — `docs/polished-mode.md` is the current high-quality path for long documents: `--cascade --merge-facts --outline-from-clusters --cite --repair --check-precision` plus gates and `--max-sections`. Treat `--max-sections` as a cap for cluster-outline mode, not an equal-size section guarantee; stub clusters merge before cap coalescing and separable oversized clusters split by original fact order.

## Key Dependencies

- `github.com/dotcommander/reliquary` — Chunking engine (`pipeline/chunking`). Pinned remote dependency (currently `v0.3.1`).
- `github.com/garyblankenship/wormhole` — Provider-agnostic LLM SDK (text + embeddings). Pinned remote dep (currently `v1.24.0`); no local checkout required.
- `github.com/pkoukk/tiktoken-go` — `cl100k_base` preflight token estimates (`count` command, chunking, manifest totals).
- `github.com/alecthomas/kong` — CLI framework.

## Notable Patterns

- The `digest` pipeline never asks one model call to hold the whole source: it researches atomic facts per chunk, fuses the notes, writes a narrative draft, then edits it — recording per-chunk failures instead of aborting the run. Per-chunk research runs with bounded parallelism (`--concurrency`, default 4) and a per-call timeout (`--timeout`, default 300s); a hard LLM error fails the run fast, while an empty response is a recorded soft skip. The editor stage runs by default (skip with `--no-edit`); the fuse stage is off by default (enable with `--fuse` — it can time out on large inputs).
- Semantic embeddings are cached on disk (keyed by model) so re-running `chunk --mode semantic` (e.g. tuning `--threshold`) skips re-embedding.
- Integration tests in `cmd_test.go` build the binary and run it as a subprocess; package-level tests cover the digest pipeline, embedding cache, and prompts offline (fakes, no network).
- Reliquary and Wormhole are pinned remote dependencies; neither requires a sibling checkout for `go build`.
