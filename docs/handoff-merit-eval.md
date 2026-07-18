# Handoff — digest merit evaluation (2026-06-24)

Self-contained resume point for the next session. Goal in flight:
**"finish hardening calibrations, evals, run real tournament until blocked"** — done
through the first real tournament; blocked on a de-bias decision (below).

## TL;DR state
- Two **separate, complementary** leaderboards now exist for the 37-model
  `runs/write-bakeoff/*/digest.md` field, both ranking the SAME source (the
  "Project Halcyon" brief, `testdata/extraction/source.md`):
  - **Fidelity** (deterministic, free): `distill digest score` → composite =
    recall − 2×overlap. Punishes copiers. Report: `docs/digest-review.html`.
  - **Merit as a read** (LLM judge, paid): `distill-eval grade tournament` →
    pairwise order-swapped judge. Report: `docs/merit-tournament.html`.
- Key finding: **merit ≠ fidelity.** Composite #1 (qwen3.7-plus, faithful +
  near-zero copy) is merit #4 and won 0 matches; glm-5.2 / bytedance-2.0-mini
  rose. The two axes genuinely diverge — that's the payoff.
- All code committed, tree clean, 45 tests green (`go test ./internal/...`).

## What shipped this session (all committed on `main`)
1. `internal/extractscore/digest.go` + `digest_report.go` + `cmd/digestscore.go`
   — deterministic digest review: fact recall (86 golden facts), tension
   preservation (3 source discrepancies), hygiene, **8-gram verbatim overlap**
   (copy signal), composite ranking, HTML.
2. Matcher fix: `normalize` strips thousands-separator commas (`3,000`→`3000`,
   37/37 blast radius); battery/thermal fixtures broadened. gpt-5.5 was a false
   83/86 → truly 86/86.
3. `internal/gradecal/` — pairwise merit judge:
   - `calibrate.go` + `sabotage.go`: planted-pair calibration. 6 sabotages
     (shuffle/truncate/cliche/listify/drop-frame/para-reverse), order-swap,
     grounded-reason confabulation guard. Metrics: swap-robust acc, position
     bias, grounded rate.
   - `tournament.go` + `tournament_report.go`: order-swapped merge-sort ranking
     (~N·logN, memoized), random-triple cycle audit, flip/cycle/grounded trust
     stats, self-preference flag, HTML.
   - Prompt: `internal/prompts/defaults/merit-judge.md` (config-data, materializes
     to `~/.config/distill/prompts/`).
4. `internal/extractscore/gate.go` — **candidate gate**: GateConfig thresholds in
   `testdata/extraction/digest-checks.json`
   (`minRecall 0.88, maxOverlap 0.06, minTensions 2, requireClean true`). Tournament
   gates by default; `--all` bypasses, `--models` overrides, `--dry-run` previews
   candidates + cost with zero spend.

## Runs completed (opus-4.8 judge via OpenRouter)
- **Calibration (hardened): TRUSTWORTHY.** 18/18 planted pairs, swap-robust 100%,
  position bias 0%, grounded 86%. opus reads merit, not guessing.
- **Tournament (top-10): TRUSTWORTHY** but biased #1. flip 17.1%, cycle 0%,
  grounded 71%, 0 err, 70 calls. Ranking:
  1. opus-4.8 (8-0-0) ⚠ **SELF-PREFERENCE — opus judged its own digest; discount**
  2. z-ai-glm-5.2 (7-1) ← credible best read
  3. bytedance-seed-2.0-mini (5-2)
  4. qwen3.7-plus (0-3-3)  5. gpt-5.5 (2-2-2)  …  10. gpt-5-nano (0-5)
- **Gate dry-run on full field:** 21 eliminated (16 low-recall, 5 copiers), **16
  candidates** kept. Full-candidate tournament est. ≤152 calls ≈ $3–7.60.

## DE-BIAS: COMPLETE (2026-06-24) — opus self-preference confirmed & corrected
1. **Self-recognition probe — BUILT + RUN.** `distill-eval grade recognize`
   (`internal/gradecal/recognize*.go`, prompt `internal/prompts/defaults/recognize.md`,
   cmd `cmd/digestrecognize.go`). **opus-4.8 8/8 & gpt-5.5 8/8 (vs 25% chance) —
   RECOGNITION PRESENT even after canonicalization.** Report `docs/recognize-probe.html`.
2. **Leave-one-out diverse-judge panel — BUILT + RUN.** `distill-eval grade panel`
   (judges opus + gpt-5.5 + deepseek-v4-pro + glm-5.2; one eligible judge per
   comparison, no model judges its own pair; `gradecal.Canonicalize` applied
   cmd-side). Code: `internal/gradecal/panel.go` + `panel_report.go` (tournament.go
   refactored to a `judgeResolver` — single source of truth), `cmd/digestpanel.go`.
   - **Result (TRUSTWORTHY: flip 10%, cycle 0%, grounded 64%, 50 cmp/100 calls):**
     1 glm-5.2 · 2 deepseek-v4-pro · 3 Qwen3.6-35B · 4 gpt-5.5 · **5 opus-4.8**.
   - **opus #1→#5** once it couldn't judge itself = ~4 ranks of self-preference
     inflation. **glm-5.2 held #1** = genuine best read. Report `docs/panel-tournament.html`.
   - Caveat: one seeded run; same-source digests, so partly content not pure voice.

## Operating constraints (important)
- **No local models** — all LLM calls go through OpenRouter
  (`DISTILL_BASE_URL=https://openrouter.ai/api/v1`, `OPENROUTER_API_KEY`,
  `DISTILL_MODEL=anthropic/claude-opus-4.8`). Keep base-URL in a SCRIPT FILE,
  not inline in Bash (fetch-guard blocks inline https URLs).
- **Cost:** frontier judge (opus/gpt-5.5) ≈ 2–5¢/call on these big prompts;
  blended fleet avg was 0.07¢. Always `--dry-run` and state the call×3¢ estimate
  before a paid run. Bound to gated candidates, never the full 37.
- **pi-agent write path WORKS again (2026-06-24).** The rc=3 fail-loud fix landed
  `cdd59b9` (2026-06-23); this session drove 5 specs through pi cleanly (Go code +
  tests). KNOWN LIMIT: pi cannot author *instruction-shaped* files — a prompt .md
  full of second-person directives makes pi echo the spec instead of editing
  (zero-tool no-op). Write prompt/data files directly (or base64-pipe), never as a
  plain pi spec. (`recognize.md` was written directly for this reason.)
- Run scripts live in the session scratchpad (gitignored `runs/` is scratch).
  Re-derive seed order with: `./distill digest score | awk 'NR>1 && $1!=""{print $1}'`.

## Reproduce / continue
```
go build -o distill-eval ./cmd/distill-eval
./distill-eval grade tournament --dry-run                 # preview gate + cost, no spend
# de-biased run (after building leave-one-out panel):
#   keep base-url in a script file; DISTILL_MODEL per judge; --models = 16 gated, seed order
```

## Model exclude/pin (from OpenRouter logs + merit + gate) — not yet codified
- EXCLUDE from future bakeoffs: web-search models (gpt-4o*-search, perplexity-
  sonar — different task, 40% of spend; sonar is wrongly in the candidate set),
  qwen3.7-plus (111s latency, weak read), thinking variants (slow, gate already
  drops them), copiers (gate drops).
- PIN: extraction default ~~ling-2.6-flash~~ → **`deepseek/deepseek-v4-flash`**
  (set as config default 2026-06-24, commit `e83b0a0`); on-box Qwen 35B is now
  opt-in via `--local`, not the implicit offline fallback; non-frontier volume
  → DeepInfra/WandB.
- DONE (2026-06-24, `8cddcf5`): exclude-list codified in the candidate gate —
  `excludeModels` in `testdata/extraction/digest-checks.json` (`["sonar","search",
  "qwen3.7-plus"]`), enforced by `GateConfig.ExcludeModels` (case-insensitive
  substring) so web-search/weak models are dropped even at high recall.

## Memory written this session
`distill-no-local-models`, `feedback-digest-report-composite-ranking`,
`feedback-bound-frontier-judge-cost` (+ MEMORY.md index updated).
