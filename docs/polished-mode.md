# Polished Digest Mode

Polished mode is the current high-quality `distill digest` recipe. It is not a separate command; it is a set of existing flags that make the pipeline extract facts, consolidate duplicates, plan from fact clusters, write with citations, repair dropped facts, check sentence precision, and gate the result.

## Cascade Prerequisite

The committed default leaves `research_escalation_model` blank so cascade is an
explicit spend choice. Before using `--cascade`, choose one of these paths:

```yaml
# ~/.config/distill/config.yaml
research_escalation_model: <provider>/<model>
```

Or pass `--model <provider>/<model>` (or set `$DISTILL_MODEL`) for the command.
An explicit model pin applies to every text role, including the precision judge
and cascade escalation; per-role config does not override it.

## Recipe: configured escalation

```bash
distill digest SOURCE.md \
  --cascade \
  --merge-facts \
  --outline-from-clusters \
  --cite \
  --repair \
  --check-precision \
  --min-cited 0.90 \
  --min-precision 0.95 \
  --max-sections 8 \
  --out SOURCE.polished.md \
  --artifacts artifacts/SOURCE-polished
```

Use `--dry-run` first when spending against remote providers. This command
needs the explicit `research_escalation_model` configuration shown above:

```bash
distill digest SOURCE.md --dry-run --cascade --merge-facts --outline-from-clusters --cite --repair --check-precision --min-cited 0.90 --min-precision 0.95 --max-sections 8 --out SOURCE.polished.md --artifacts artifacts/SOURCE-polished
```

## Recipe: all-role model pin

Use a whole-command pin instead of changing per-role configuration:

```bash
distill digest SOURCE.md \
  --model <provider>/<model> \
  --dry-run \
  --cascade \
  --merge-facts \
  --outline-from-clusters \
  --cite \
  --repair \
  --check-precision \
  --min-cited 0.90 \
  --min-precision 0.95 \
  --max-sections 8 \
  --out SOURCE.polished.md \
  --artifacts artifacts/SOURCE-polished
```

Dry-run performs artifact-binding validation but makes no provider requests and
does not create an absent artifact directory.

## What The Flags Do

- `--cascade` retries weak fresh extraction chunks with the configured escalation model when their deterministic capture ratio is below `cascade_min_capture`.
- `--merge-facts` embeds extracted fact bullets, clusters near duplicates, and asks the fuse model to merge each duplicate cluster. Exact duplicates may drop. Near-duplicates with explicit `[F#]` markers only merge when the merged output still represents every source fact ID.
- `--outline-from-clusters` turns merged fact clusters into outline sections without a separate free-form outline plan. Stub clusters below `min_section_facts` merge into the nearest semantic neighbor before section-count coalescing.
- `--max-sections` is a cap for cluster-outline mode, not a promise that every section will have equal size. After stub coalescing, oversized separable clusters split by original fact order using `cluster_balance_factor`.
- `--cite` writes with temporary `[F#]` markers, verifies cited fact coverage, writes `responses/citations.json`, and strips markers from the final article.
- `--repair` runs citation repair when cited facts are missing, and precision repair when `--check-precision` finds unsupported sentences. Repairs are accepted only when the deterministic coverage check does not regress.
- `--check-precision` judges final article sentences against extracted facts and writes `responses/precision.json`.
- `--min-cited`, `--min-precision`, and `--min-coverage` turn those checks into non-zero exit gates.

## Artifacts To Inspect

`--artifacts` accepts only an absent/new directory, an empty directory, or a
directory whose `source.json` exactly matches the cleaned source and chunking
settings. A non-empty directory with a missing, unreadable, corrupt, or
mismatched marker is refused without mutation.

`--max-calls` is a hard aggregate ceiling across text and embedding provider
requests (`0` is unlimited). The dry-run call plan is advisory because runtime
inputs, cache hits, embedding batches, and retries change actual usage. Finite
runs report `request budget: <used>/<limit>`.

- `facts.compiled.md` is the raw extracted-facts checkpoint.
- `responses/facts.merged.md` is the post-merge fact set used for planning.
- `responses/outline.md` is the cluster-derived outline.
- `responses/draft.md` is the pre-edit article.
- `responses/rewrite.cited.md` preserves temporary citations before stripping.
- `responses/citations.json`, `responses/coverage.json`, and `responses/precision.json` are the verification records.
- `run-ledger.jsonl` records provider calls, reuse events, repairs, and token deltas when available.

## Prompt Refresh

Default prompts are materialized under `~/.config/distill/prompts/`. Distill tracks hashes in `~/.config/distill/prompts/.embedded-hashes.json`; when an embedded default changes, a materialized prompt refreshes only if it still matches a previously materialized default. User-edited prompt files are preserved.

The section editor prompt keeps the stable full draft first for provider cache locality, then appends already accepted prior edited sections so later edits can avoid reintroducing repetition.
