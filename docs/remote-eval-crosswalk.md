# Remote Eval Crosswalk — model selection for distill

**Public benchmark leaderboards are the PRIMARY signal for choosing models per role.**
distill's own local evals (the 86-fact extraction harness, merit/panel tournaments,
label/code/comedy harnesses) are now a **SECONDARY / fallback** signal — used only
when the public boards don't cover a roster model for a given role, or when they
disagree and a tiebreak is needed.

This doc is the process of record. It is refreshed on demand (the leaderboards move),
not scraped automatically — see **Refreshing** below.

## How selection works

1. The candidate pool is the cost-capped OpenRouter roster in [`runs/roster.sh`](../runs/roster.sh)
   (upper cap: $8.00 / 1M output tokens). Frontier leaders (opus-4.8, gpt-5.5,
   gemini-3-pro) are *off-roster* by cost — selection means "best-ranked **among the
   cheap candidates**."
2. For each role, read the role's PRIMARY public benchmark, find the roster models on it,
   and pick the top-ranked one.
3. If a roster model is absent or the board is inconclusive for that role, fall back to
   distill's local harness for that role (the FALLBACK rule below).
4. Encode the result in [`internal/config/defaults/config.yaml`](../internal/config/defaults/config.yaml)
   via the per-role keys (`research_model`, `fuse_model`, `write_model`, `edit_model`,
   `judge_model`); each falls back to `model` when unset. `--model` / `$DISTILL_MODEL`
   override every role at runtime.

## Role → benchmark → pick (current, 2026-06-24)

| Role | Primary benchmark | Roster pick | Why |
|------|-------------------|-------------|-----|
| **research** (extract atomic facts, must not fabricate) | Vectara HHEM | `google/gemini-2.5-flash-lite` | least-hallucinating roster model (~3.3%); cheap + fast for N-parallel chunk calls. Frontier models hallucinate *more* on extraction. |
| **fuse** (merge notes, faithful synthesis) | llm-stats Open LLM (GPQA / SWE) | `deepseek/deepseek-v4-pro` | strong synthesis reasoning (GPQA 90.1), low fabrication (HHEM 8.6) |
| **write** (narrative prose) | EQ-Bench Creative Writing v3 | `z-ai/glm-5.2` | #1 prose among roster (board #8 overall); corroborates distill's local merit tournament |
| **edit** (polish prose, keep facts) | EQ-Bench Creative Writing v3 | `z-ai/glm-5.2` | prose polish is the job |
| **judge** (pairwise merit/publish) | EQ-Bench Judgemark + Panickssery 2404.13076 | `deepseek/deepseek-v4-pro` | strong judge, *different family from the writer* → avoids self-preference bias |
| default / fallback (`model`) | llm-stats Open LLM composite | `z-ai/glm-5.2` | best all-round roster model |
| *code (bucket)* | Aider Polyglot (Go-inclusive) / llm-stats SWE-Bench | `deepseek/deepseek-v4-pro` | SWE-Bench 80.6%; Aider is the only public Go-language anchor |
| *label / sentiment (bucket)* | none (category saturated/dead) | `google/gemini-2.5-flash-lite` | route on cost + latency only |
| *comedy (bucket)* | EQ-Bench Creative Writing v3 | `z-ai/glm-5.2` | top roster creative model |
| *embeddings* | MTEB | `qwen/qwen3-embedding-8b` (keep) | MTEB-tracked; not covered by the text boards above |

## The sources (PRIMARY)

| # | Benchmark | What it measures | URL |
|---|-----------|------------------|-----|
| 1 | **llm-stats Open LLM Leaderboard** | Open-weight composite: coding arena + GPQA Diamond + SWE-Bench + throughput + latency + **price**. Closest to a drop-in roster ranker (roster is mostly open-weight). | https://llm-stats.com/leaderboards/open-llm-leaderboard |
| 2 | **EQ-Bench Creative Writing v3** | Long-form prose quality via order-swapped pairwise Elo + rubric; tracks "slop". Direct analogue of distill's merit/publish/comedy panels. | https://eqbench.com/creative_writing.html |
| 3 | **Vectara Hallucination Leaderboard (HHEM)** | Summarization faithfulness (added-content hallucination rate) via a deterministic classifier. Validates the research/extraction fabrication signal. | https://github.com/vectara/hallucination-leaderboard |
| 4 | **Aider Polyglot** | Unit-test pass-rate across 6 languages **including Go** (2 attempts). Only public board that anchors distill's Go code eval. | https://aider.chat/docs/leaderboards/ |
| 5 | **EQ-Bench Judgemark** | How good a model is *as a judge* (discriminative power). Use to validate the judge-model pick. | https://eqbench.com/judgemark.html |

### Research / methodology citations

- **Panickssery et al., "LLM Evaluators Recognize and Favor Their Own Generations"** (NeurIPS 2024) — https://arxiv.org/abs/2404.13076 — the basis for keeping the judge in a different model family than the writer, and for distill's self-recognition probe.
- **FactScore** (https://arxiv.org/abs/2305.14251) / **VeriScore** (https://arxiv.org/pdf/2406.19276) — the methodological twins of distill's atomic-fact recall/precision harness (no maintained public leaderboard, so they inform method, not selection).

## Refreshing (on demand)

Leaderboards move; refresh before a selection decision. Fetch each through the fetch
pipeline (never raw curl/WebFetch — a fetch-guard hook blocks inline URLs):

```
Agent(dc:fetch-agent, "<url from the table above>")
```

Then re-derive the role table and update `config.yaml`. Suggested cadence: whenever a
new roster model lands or a major model release shifts the boards.

## FALLBACK rule (when local evals apply)

Use distill's LOCAL harness for a role only when the PRIMARY board can't decide it:

- **Roster model absent from the board** — e.g. a newly added OpenRouter model not yet
  ranked on EQ-Bench/HHEM → score it locally (`distill-eval eval facts` for extraction recall,
  `distill-eval grade tournament` / `panel` for prose merit).
- **Board inconclusive / within noise** — two roster models within the board's
  confidence interval → break the tie with the local harness most aligned to the role
  (extraction → 86-fact recall; write/edit/comedy → merit/comedy panel;
  research fabrication → `digest-score` fabrication signal).
- **Role has no public board** — label/sentiment (category is dead): route on cost +
  latency, local macro-F1 only as a floor check.

Local-eval reports live in `docs/` (extraction-model-benchmark.md, merit/panel/comedy
tournaments, code/label routing) and remain the deeper, distill-specific evidence — they
are the fallback, not the headline.
