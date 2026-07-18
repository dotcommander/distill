# Pipeline Q&A Log

A running record of questions asked about distill's digest/eval pipeline and their answers, captured so the reasoning isn't lost between sessions. Newest entries at the top. Each answer cites the source code or run it came from.

---

## Q: ling has high precision but low recall — should we stop using it, and can we rank models on recall alone?

**Date:** 2026-06-24

**Frame correction first:** precision and recall are **not roles you assign a model to** — they're two scores on the *same* extraction job. Every extraction yields both at once. ling's profile is just: faithful (P=0.98) but incomplete (R=0.79 relative to the per-role reference). You can't "use ling for precision"; you can only place it where its profile fits.

**Recall is front-loaded; precision is everywhere.** The digest pipeline is `research (extract per chunk) → fuse → write → edit`, and **only research ever sees the source**. So recall is binding at exactly one stage:

| Stage | Recall matters? | Precision matters? | Why |
|-------|-----------------|--------------------|-----|
| Research | **Critically** | Yes (floor) | A missed fact is **gone forever** — no later stage can recover it |
| Fuse | Don't-drop | Yes | Merge faithfully, don't invent |
| Write | n/a (no new facts to find) | **Critically** | Render compiled facts faithfully, add nothing |
| Edit | n/a | **Critically** | Polish without distorting |

**Implications:**

1. **ling isn't unusable — it's mis-placed.** Wrong for research (the one recall-binding stage; it loses ~21% of facts). Potentially *ideal* for write/edit, which have no recall demand — they need a fast, faithful renderer, which is exactly ling's profile (cheap + high precision). Worth testing.
2. **"Rank on recall only?"** — Rank extraction candidates on recall, but keep precision as a **gate, not skipped**: precision comes *free* in the same judge call (no saving from skipping it), and without a precision floor a model that fabricates to pad its fact count would *win* a recall-only ranking. This is exactly the existing `recall − copy_penalty×overlap` composite — the penalty is the anti-gaming guard.
3. **"Precision only matters for eval, not the digest run?"** — Inverted: precision is *more* load-bearing in the real run, because faithfulness (no hallucination) **is** the product promise of a "fact-preserving rewrite." Eval just measures it. Only the F1 *number* is eval-only.

**Caveat that bounds all of this:** the 0.79 recall is **relative to the per-role (v2) extraction, not a human gold set** — it means "thinner than the per-role stack," NOT "missed 21% of the document." Ranking extractor models on recall honestly requires a **gold fact set** for a doc; otherwise every comparison just measures models against each other.

**Path this points to:** (1) build a gold fact set to measure *absolute* extractor recall; (2) test a cheap high-precision model (ling) at write/edit vs the current glm-5.2, same fused facts, compare fidelity; (3) keep precision as a gate, rank research on recall.

---

## Q: Why did `digest` run `inclusionai/ling-2.6-flash` for every role instead of the per-role picks?

**Date:** 2026-06-24

**Answer — stale live config; committed defaults never merged.**

`distill`'s `internal/config/defaults/config.yaml` is written to `~/.config/distill/config.yaml` **only on first run**. It does **not** merge newly-added keys into a config that already exists. When the per-role keys (`research_model`/`fuse_model`/`write_model`/`edit_model`/`judge_model`) were added to the committed defaults, any pre-existing user config kept its old single `model:` and silently fell through.

Resolution chain (`internal/config/config.go:67-97`): `EffectiveRole`/`EffectiveJudge` look up the per-role override; if it's empty they `return base`, where `base` is the top-level `model`. So an absent per-role key falls straight back to `model:`.

The live `~/.config/distill/config.yaml` still held eval-day local pins — `model: inclusionai/ling-2.6-flash` (no per-role keys) plus `embedding_model: Qwen3-Embedding-4B-mxfp8 @ 127.0.0.1:8000`. So the digest used ling-flash for all four roles and hit the local embedding box instead of OpenRouter.

**Fix applied:** rewrote the live config to the eval-day picks — `model: z-ai/glm-5.2`, `research_model: google/gemini-2.5-flash-lite-preview-09-2025`, `fuse_model`/`judge_model: deepseek/deepseek-v4-pro`, `write_model`/`edit_model: z-ai/glm-5.2`, `embedding_model: qwen/qwen3-embedding-8b @ https://openrouter.ai/api/v1`.

**Takeaway:** after changing committed `defaults/config.yaml`, reconcile the live `~/.config/distill/config.yaml` manually (or via `distill models rankings apply`). When a digest's logged `model=` doesn't match expectation, check the **live** config first, not the committed defaults.

---

## Q: How is Precision calculated vs Recall in `eval judge`?

**Date:** 2026-06-24

**Answer — different yardsticks for each metric** (verified in `internal/actions/eval/metrics.go`).

```
Precision = Supported / (Supported + Contradicted + NotInSource)   # metrics.go:65-67
Recall    = Supported / (Supported + Missed)                       # metrics.go:69-70
F1        = 2·P·R / (P + R)                                        # metrics.go:73 (harmonic mean)
```

The key asymmetry — each metric is measured against a **different reference**:

| Metric | Judged against | Question it answers |
|--------|----------------|---------------------|
| Precision | the **source chunk** (ground truth) | "Of the facts the candidate extracted, how many are actually in the source?" |
| Recall | the **reference extraction** (used as a coverage checklist) | "Of the facts the reference found, how many did the candidate also find?" |

Judge prompt (`internal/prompts/defaults/judge.md:3-4`): *"Judge the candidate extraction against the source chunk... Use the reference extraction as a coverage checklist, not as infallible truth."*

**Category definitions** (each candidate fact gets one verdict; `metrics.go:49-54`, unknown verdicts default to NotInSource per `metrics.go:41`):

- **Supported** — directly backed by the *source chunk* (numerator of both metrics).
- **Contradicted** — conflicts with the source → lowers precision.
- **NotInSource** — plausible but not recoverable from the source (mild hallucination) → lowers precision.
- **Missed** — a *reference* fact the candidate omitted → lowers recall. This is the **only** count sourced from the reference responses dir (`eval.go:35,69`), not the source chunks.

**Why it matters when reading results:** precision has a hard ground truth (the document), so it's an absolute quality signal. Recall is only *relative to the reference extraction* — a low recall means "thinner than the reference," NOT "missed X% of the document." If the reference over-extracts, the candidate's true recall against a human gold set would be higher.

---
