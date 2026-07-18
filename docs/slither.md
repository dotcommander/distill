# Slither Report

> Slither creeps like a snake through this repository, tasting each path for cheap-model scent before striking only where the signal is strongest.

- Generated: `2026-06-27 02:23:05 EDT`
- Days: `90`
- Patterns source: `embedded:triage_patterns.json`
- Files seen: `211`
- Slither build: `v0.1.1-0.20260626194356-6917b9ba2369+dirty (6917b9ba2369, modified, go1.26.4)`
- Discovery: source `git`, candidates `211`, git tracked `210`, git untracked `1`, filesystem files `0`
- Files reported: `210`
- Scoring: deterministic fallback

- Skipped signals: `git_ls_files:included_untracked:1`, `model_scoring:not_configured`

## Executive Triage

- Start with: `internal/actions/digest/digest.go` (score `5`, reliability boundary, resource lifecycle, rate-limit boundary)
- Ranked production files: `52`; separated documentation rows: `23`; separated test/fixture rows: `83`; generated/support rows: `19`; detail-only weak rows: `33`; total reported rows: `210`
- Confidence: high `21`, medium `55`, low `134`; test-gap rows: `1`
- History-backed rows: `207`; import-graph-backed rows: `2`; deterministic-only rows: `1`
- Dominant discriminating evidence layers: `churn:207`, `bugfix-history:86`, `content-risk:84`, `cochange:46`, `ownership:46`
- Actionability: `dependency_review:1`, `inspect:56`, `hotspot:4`, `verify_first:149`
- Review lanes: `cli-ux`, `api-contracts`, `data-integrity`, `error-handling`, `dependency-policy`, `lifecycle-concurrency`, `performance`, `test-risk`, `coupling`, `architecture`

## Cheap-Model Cull Ledger

- Stop reason: `deterministic cull complete; premium review should start with kept_for_premium`
- Rows considered: `210`

### `kept_for_premium`

- Count: `24`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `internal/actions/digest/digest.go` | 5 | high | inspect | go test ./internal/actions/digest/... | content-risk + cochange + ownership | strong multi-layer seed |
| `internal/extractscore/score.go` | 5 | high | inspect | go test ./internal/extractscore/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `cmd/digest.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `internal/extractscore/structured.go` | 5 | high | inspect | go test ./internal/extractscore/... | content-risk + architecture-smell + unknowns | strong multi-layer seed |
| `internal/prompts/prompts.go` | 5 | high | inspect | go test ./internal/prompts/... | content-risk + unknowns + env-contract | strong multi-layer seed |
| `cmd/eval.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `cmd/label.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `cmd/comedy.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `internal/actions/eval/metrics.go` | 5 | high | inspect | go test ./internal/actions/eval/... | content-risk + architecture-smell + cochange | strong multi-layer seed |
| `cmd/rankings.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + env-contract | strong multi-layer seed |
| `internal/actions/eval/eval.go` | 5 | high | inspect | go test ./internal/actions/eval/... | content-risk + unknowns + hotspot | strong multi-layer seed |
| `internal/labelscore/score.go` | 5 | high | inspect | go test ./internal/labelscore/... | content-risk + unknowns + hotspot | strong multi-layer seed |
| `internal/codeeval/run.go` | 5 | high | inspect | go test ./internal/codeeval/... | content-risk + unknowns + hotspot | strong multi-layer seed |
| `internal/structured/series.go` | 5 | high | inspect | go test ./internal/structured/... | content-risk + unknowns + hotspot | strong multi-layer seed |
| `internal/extractscore/digest.go` | 5 | high | inspect | go test ./internal/extractscore/... | content-risk + unknowns + centrality | strong multi-layer seed |
| `cmd/digestscore.go` | 5 | high | inspect | go test ./cmd/... | content-risk + cochange + ownership | strong multi-layer seed |
| `cmd/digestgrade.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `internal/extractscore/report.go` | 5 | high | inspect | go test ./internal/extractscore/... | content-risk + churn + bugfix-history | strong multi-layer seed |
| `cmd/trace.go` | 5 | high | inspect | go test ./cmd/... | content-risk + unknowns + churn | strong multi-layer seed |
| `internal/labelscore/report.go` | 5 | high | inspect | go test ./internal/labelscore/... | content-risk + unknowns + cochange | strong multi-layer seed |
| `cmd/evalroot.go` | 5 | high | inspect | go test ./cmd/... | content-risk + ownership + churn | strong multi-layer seed |
| `internal/ai/client.go` | 5 | medium | inspect | go test ./internal/ai/... | path-risk + content-risk + sdk-dx | strong multi-layer seed |
| `internal/config/config.go` | 5 | medium | inspect | go test ./internal/config/... | path-risk + content-risk + unknowns | strong multi-layer seed |
| `internal/extractscore/digest_report.go` | 5 | medium | inspect | go test ./internal/extractscore/... | content-risk + cochange + churn | strong multi-layer seed |

### `alternates`

- Count: `28`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `cmd/root.go` | 3 | medium | inspect | go test ./cmd/... | ownership + churn + bugfix-history | plausible next premium target if budget remains |
| `internal/fsutil/write.go` | 3 | medium | inspect | go test ./internal/fsutil/... | content-risk + centrality + churn | plausible next premium target if budget remains |
| `cmd/distill-eval/main.go` | 3 | medium | inspect | go test ./cmd/distill-eval/... | path-risk + content-risk + churn | plausible next premium target if budget remains |

### `culled_generated_or_report`

- Count: `19`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `docs/remote-eval-report.html` | 5 | low | verify_first | - | content-risk + size + churn | generated, report, minified, or derived artifact |
| `docs/digest-review.html` | 5 | low | verify_first | - | content-risk + cochange + ownership | generated, report, minified, or derived artifact |
| `docs/label-classification-routing.html` | 5 | low | verify_first | - | content-risk + churn | generated, report, minified, or derived artifact |

### `culled_documentation`

- Count: `23`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `README.md` | 4 | medium | verify_first | go test ./... | path-risk + sdk-dx + cochange | documentation or guide separated from the production premium queue |
| `CLAUDE.md` | 4 | medium | verify_first | - | cochange + ownership + churn | documentation or guide separated from the production premium queue |
| `docs/handoff-merit-eval.md` | 4 | medium | verify_first | go test ./... | cochange + ownership + churn | documentation or guide separated from the production premium queue |

### `culled_test_only`

- Count: `83`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `internal/actions/digest/digest_test.go` | 5 | low | verify_first | go test ./internal/actions/digest/... | content-risk + architecture-smell + cochange | test or fixture separated from the production premium queue |
| `internal/extractscore/score_test.go` | 5 | low | verify_first | go test ./internal/extractscore/... | content-risk + cochange + ownership | test or fixture separated from the production premium queue |
| `internal/actions/eval/metrics_test.go` | 5 | low | verify_first | go test ./internal/actions/eval/... | content-risk + cochange + ownership | test or fixture separated from the production premium queue |

### `culled_low_signal`

- Count: `25`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `internal/rankings/derive.go` | 2 | medium | verify_first | go test ./internal/rankings/... | content-risk + churn | low score or weak evidence intersection |
| `cmd/score.go` | 2 | medium | verify_first | go test ./cmd/... | content-risk + churn + bugfix-history | low score or weak evidence intersection |
| `cmd/structured.go` | 2 | medium | verify_first | go test ./cmd/... | content-risk + churn | low score or weak evidence intersection |

### `culled_duplicate_surface`

- Count: `8`

| file | score | confidence | actionability | verify | strongest_evidence_intersection | reason |
| --- | ---: | --- | --- | --- | --- | --- |
| `internal/gradecal/tournament.go` | 3 | medium | inspect | go test ./internal/gradecal/... | content-risk + unknowns + ownership | same evidence surface represented by stronger row internal/gradecal/calibrate.go |
| `internal/extractscore/gate.go` | 3 | medium | inspect | go test ./internal/extractscore/... | content-risk + unknowns + cochange | same evidence surface represented by stronger row internal/extractscore/digest.go |
| `internal/extractscore/structured_report.go` | 3 | medium | inspect | go test ./internal/extractscore/... | content-risk + churn | same evidence surface represented by stronger row internal/extractscore/report.go |

### `needs_more_evidence`

- Count: `0`

## Ranked Files

Generated/support, documentation, test/fixture, duplicate-surface, needs-more-evidence, low-signal, and weak-score rows are omitted here; separated rows appear below, and `--json` retains all reported evidence rows.

| rank | file | score | confidence | actionability | evidence | review command | top signals | note |
| ---: | --- | ---: | --- | --- | --- | --- | --- | --- |
| 1 | `internal/actions/digest/digest.go` | 5 | high | inspect | content-risk, cochange, ownership, hotspot, size, +2 more | go test ./internal/actions/digest/... | content:reliability_policy_boundary:5, content:resource_factory:1, content:rate_limit_boundary:1 | review content-risk + cochange + ownership |
| 2 | `internal/extractscore/score.go` | 5 | high | inspect | content-risk, unknowns, cochange, ownership, hotspot, +2 more | go test ./internal/extractscore/... | content:audit_correctness_drift_metric:3, content:error_context_dropped:1, content:resource_factory:2 | review content-risk + unknowns + cochange |
| 3 | `cmd/digest.go` | 5 | high | inspect | content-risk, unknowns, cochange, ownership, hotspot, +2 more | go test ./cmd/... | content:reliability_policy_boundary:5, unknowns:env_assumptions:4, cochange:partners:6 | review content-risk + unknowns + cochange |
| 4 | `internal/extractscore/structured.go` | 5 | high | inspect | content-risk, architecture-smell, unknowns, hotspot, size, +1 more | go test ./internal/extractscore/... | content:audit_correctness_drift_metric:5, content:error_context_dropped:1, content:resource_factory:3 | review content-risk + architecture-smell + unknowns |
| 5 | `internal/prompts/prompts.go` | 5 | high | inspect | content-risk, unknowns, env-contract, centrality, cochange, +2 more | go test ./internal/prompts/... | content:drift_hazard_comment:2, content:error_context_dropped:1, content:resource_factory:1 | review content-risk + unknowns + env-contract |
| 6 | `cmd/eval.go` | 5 | high | inspect | content-risk, unknowns, cochange, ownership, churn, +1 more | go test ./cmd/... | content:shell_boundary:1, content:reliability_policy_boundary:5, content:audit_correctness_drift_metric:5 | review content-risk + unknowns + cochange |
| 7 | `cmd/label.go` | 5 | high | inspect | content-risk, unknowns, cochange, ownership, hotspot, +2 more | go test ./cmd/... | content:reliability_policy_boundary:5, content:audit_correctness_drift_metric:5, content:drift_hazard_comment:1 | review content-risk + unknowns + cochange |
| 8 | `cmd/comedy.go` | 5 | high | inspect | content-risk, unknowns, cochange, ownership, hotspot, +2 more | go test ./cmd/... | content:reliability_policy_boundary:5, content:rate_limit_boundary:1, unknowns:env_assumptions:3 | review content-risk + unknowns + cochange |
| 9 | `internal/actions/eval/metrics.go` | 5 | high | inspect | content-risk, architecture-smell, cochange, ownership, churn, +1 more | go test ./internal/actions/eval/... | content:audit_correctness_drift_metric:5, smell:case_cascade:9, cochange:partners:1 | review content-risk + architecture-smell + cochange |
| 10 | `cmd/rankings.go` | 5 | high | inspect | content-risk, unknowns, env-contract, ownership, churn | go test ./cmd/... | content:shell_boundary:1, content:looped_io_or_query:1, content:reliability_policy_boundary:5 | review content-risk + unknowns + env-contract |
| 11 | `internal/actions/eval/eval.go` | 5 | high | inspect | content-risk, unknowns, hotspot, churn, bugfix-history | go test ./internal/actions/eval/... | content:audit_correctness_drift_metric:5, content:error_context_dropped:2, content:resource_factory:1 | review content-risk + unknowns + hotspot |
| 12 | `internal/labelscore/score.go` | 5 | high | inspect | content-risk, unknowns, hotspot, churn, bugfix-history | go test ./internal/labelscore/... | content:audit_correctness_drift_metric:5, unknowns:resource_factory:2, hotspot:bugfix_complexity | review content-risk + unknowns + hotspot |
| 13 | `internal/codeeval/run.go` | 5 | high | inspect | content-risk, unknowns, hotspot, churn | go test ./internal/codeeval/... | content:shell_boundary:1, content:async_or_concurrent_boundary:1, content:reliability_policy_boundary:5 | review content-risk + unknowns + hotspot |
| 14 | `internal/structured/series.go` | 5 | high | inspect | content-risk, unknowns, hotspot, churn | go test ./internal/structured/... | content:custom_security_or_compliance_surface:5, unknowns:nested_loop_scale:1, unknowns:resource_factory:2 | review content-risk + unknowns + hotspot |
| 15 | `internal/extractscore/digest.go` | 5 | high | inspect | content-risk, unknowns, centrality, cochange, ownership, +1 more | go test ./internal/extractscore/... | content:audit_correctness_drift_metric:2, content:error_context_dropped:1, content:resource_factory:1 | review content-risk + unknowns + centrality |
| 16 | `cmd/digestscore.go` | 5 | high | inspect | content-risk, cochange, ownership, churn | go test ./cmd/... | content:audit_correctness_drift_metric:4, content:resource_factory:2, cochange:partners:2 | review content-risk + cochange + ownership |
| 17 | `cmd/digestgrade.go` | 5 | high | inspect | content-risk, unknowns, cochange, ownership, churn | go test ./cmd/... | content:read_all_or_global_growth:1, content:resource_factory:2, unknowns:env_assumptions:4 | review content-risk + unknowns + cochange |
| 18 | `internal/extractscore/report.go` | 5 | high | inspect | content-risk, churn, bugfix-history | go test ./internal/extractscore/... | content:audit_correctness_drift_metric:5, churn:55, bugfix_touches:2 | review content-risk + churn + bugfix-history |
| 19 | `cmd/trace.go` | 5 | high | inspect | content-risk, unknowns, churn | go test ./cmd/... | content:reliability_policy_boundary:5, content:rate_limit_boundary:2, unknowns:env_assumptions:2 | review content-risk + unknowns + churn |
| 20 | `internal/labelscore/report.go` | 5 | high | inspect | content-risk, unknowns, cochange, churn | go test ./internal/labelscore/... | content:audit_correctness_drift_metric:5, unknowns:nested_loop_scale:1, cochange:partners:1 | review content-risk + unknowns + cochange |
| 21 | `cmd/evalroot.go` | 5 | high | inspect | content-risk, ownership, churn | go test ./cmd/... | content:audit_correctness_drift_metric:5, ownership:risky_single_author, churn:96 | review content-risk + ownership + churn |
| 22 | `internal/ai/client.go` | 5 | medium | inspect | path-risk, content-risk, sdk-dx, centrality, ownership, +3 more | go test ./internal/ai/... | path:client, content:reliability_policy_boundary:1, content:blocking_inline_worker:1 | review path-risk + content-risk + sdk-dx |
| 23 | `internal/config/config.go` | 5 | medium | inspect | path-risk, content-risk, unknowns, env-contract, centrality, +3 more | go test ./internal/config/... | path:config, content:reliability_policy_boundary:1, content:drift_hazard_comment:1 | review path-risk + content-risk + unknowns |
| 24 | `internal/extractscore/digest_report.go` | 5 | medium | inspect | content-risk, cochange, churn | go test ./internal/extractscore/... | content:audit_correctness_drift_metric:4, cochange:partners:2, cochange:max_jaccard:0.67 | review content-risk + cochange + churn |
| 25 | `cmd/code.go` | 5 | medium | inspect | content-risk, unknowns, churn | go test ./cmd/... | content:reliability_policy_boundary:5, content:rate_limit_boundary:1, unknowns:env_assumptions:2 | review content-risk + unknowns + churn |
| 26 | `internal/traceeval/gold.go` | 5 | medium | inspect | content-risk, unknowns, churn | go test ./internal/traceeval/... | content:shell_boundary:1, content:reliability_policy_boundary:3, content:error_context_dropped:1 | review content-risk + unknowns + churn |
| 27 | `cmd/chunk.go` | 4 | medium | inspect | content-risk, centrality, cochange, ownership, hotspot, +2 more | go test ./cmd/... | content:workflow_mode_validation_gate:1, centrality:incoming_refs:2, cochange:partners:2 | review content-risk + centrality + cochange |
| 28 | `cmd/digesttournament.go` | 4 | medium | inspect | content-risk, unknowns, ownership, hotspot, churn | go test ./cmd/... | content:audit_correctness_drift_metric:1, content:resource_factory:3, unknowns:env_assumptions:2 | review content-risk + unknowns + ownership |
| 29 | `cmd/count.go` | 4 | medium | inspect | content-risk, cochange, ownership, churn, bugfix-history | go test ./cmd/... | content:resource_lifecycle:1, content:read_all_or_global_growth:1, cochange:partners:2 | review content-risk + cochange + ownership |
| 30 | `internal/transcript/transcript.go` | 4 | medium | inspect | content-risk, unknowns, cochange, ownership, churn, +1 more | go test ./internal/transcript/... | content:drift_hazard_comment:1, content:custom_security_or_compliance_surface:2, unknowns:recursive_control_flow:1 | review content-risk + unknowns + cochange |
| 31 | `go.mod` | 4 | medium | dependency_review | path-risk, content-risk, dependency-health, cochange, ownership, +2 more | go list -m all | path:go.mod, content:go_module_replace:2, dependency_health:go_module_replace | review path-risk + content-risk + dependency-health |
| 32 | `internal/comedyeval/generate.go` | 4 | medium | inspect | content-risk, cochange, ownership, churn, bugfix-history | go test ./internal/comedyeval/... | content:async_or_concurrent_boundary:1, content:reliability_policy_boundary:1, content:drift_hazard_comment:1 | review content-risk + cochange + ownership |
| 33 | `internal/gradecal/calibrate.go` | 4 | medium | inspect | content-risk, unknowns, centrality, cochange, ownership, +1 more | go test ./internal/gradecal/... | content:custom_security_or_compliance_surface:1, unknowns:nested_loop_scale:1, unknowns:resource_factory:3 | review content-risk + unknowns + centrality |
| 34 | `runs/run-label.sh` | 4 | medium | inspect | content-risk, cochange, ownership, churn, bugfix-history | - | content:dynamic_eval:1, content:reliability_policy_boundary:2, cochange:partners:4 | review content-risk + cochange + ownership |
| 35 | `internal/embedcache/embedcache.go` | 4 | medium | inspect | content-risk, ownership, churn, bugfix-history | go test ./internal/embedcache/... | content:vector_embedding_surface:5, content:resource_factory:1, ownership:risky_single_author | review content-risk + ownership + churn |
| 36 | `internal/rankings/rankings.go` | 4 | medium | inspect | content-risk, unknowns, env-contract, cochange, churn | go test ./internal/rankings/... | content:error_context_dropped:2, content:resource_factory:1, unknowns:env_assumptions:1 | review content-risk + unknowns + env-contract |
| 37 | `internal/transcript/ytdlp.go` | 4 | medium | inspect | unknowns, cochange, hotspot, churn, bugfix-history | go test ./internal/transcript/... | unknowns:resource_factory:6, cochange:partners:1, cochange:max_jaccard:0.67 | review unknowns + cochange + hotspot |
| 38 | `internal/config/defaults/config.yaml` | 4 | medium | inspect | path-risk, cochange, ownership, churn, bugfix-history | - | path:config, cochange:partners:5, cochange:max_jaccard:0.78 | review path-risk + cochange + ownership |
| 39 | `internal/gradecal/sabotage.go` | 4 | medium | inspect | unknowns, cochange, churn | go test ./internal/gradecal/... | unknowns:nested_loop_scale:1, unknowns:resource_factory:1, cochange:partners:3 | review unknowns + cochange + churn |
| 40 | `internal/gradecal/recognize.go` | 4 | medium | inspect | unknowns, churn | go test ./internal/gradecal/... | unknowns:nested_loop_scale:1, unknowns:resource_factory:4, churn:170 | review unknowns + churn |
| 41 | `internal/structured/markdown.go` | 4 | medium | inspect | unknowns, churn | go test ./internal/structured/... | unknowns:nested_loop_scale:1, unknowns:resource_factory:2, churn:91 | review unknowns + churn |
| 42 | `cmd/root.go` | 3 | medium | inspect | ownership, churn, bugfix-history | go test ./cmd/... | ownership:risky_single_author, ownership:concentrated_touches:15, churn:136 | review ownership + churn + bugfix-history |
| 43 | `internal/fsutil/write.go` | 3 | medium | inspect | content-risk, centrality, churn, bugfix-history | go test ./internal/fsutil/... | content:resource_lifecycle:1, content:custom_infra_reinvention:1, centrality:incoming_refs:11 | review content-risk + centrality + churn |
| 44 | `cmd/distill-eval/main.go` | 3 | medium | inspect | path-risk, content-risk, churn | go test ./cmd/distill-eval/... | path:main.go, content:process_exit:1, content:background_context:1 | review path-risk + content-risk + churn |
| 45 | `cmd/chunk_semantic.go` | 3 | medium | inspect | unknowns, env-contract, ownership, churn, bugfix-history | go test ./cmd/... | unknowns:env_assumptions:3, env_contract:undocumented:1, env_contract:vars:DISTILL_EMBEDDING_ENDPOINT | review unknowns + env-contract + ownership |
| 46 | `main.go` | 3 | medium | inspect | path-risk, content-risk, churn | go test ./... | path:main.go, content:process_exit:1, content:background_context:1 | review path-risk + content-risk + churn |
| 47 | `internal/rankings/defaults/rankings.yaml` | 3 | medium | inspect | cochange, ownership, churn | - | cochange:partners:1, cochange:max_jaccard:1.00, cochange:churn_overlap | review cochange + ownership + churn |
| 48 | `internal/ai/embedder.go` | 3 | medium | inspect | content-risk, unknowns, churn | go test ./internal/ai/... | content:vector_embedding_surface:4, unknowns:nested_loop_scale:1, churn:45 | review content-risk + unknowns + churn |
| 49 | `runs/run-code.sh` | 3 | medium | inspect | content-risk, churn | - | content:reliability_policy_boundary:2, churn:28 | review content-risk + churn |
| 50 | `internal/mdclean/mdclean.go` | 3 | medium | inspect | unknowns, churn, bugfix-history | go test ./internal/mdclean/... | unknowns:resource_factory:3, churn:29, bugfix_touches:1 | review unknowns + churn + bugfix-history |
| 51 | `internal/transcript/parse.go` | 3 | medium | inspect | unknowns, churn | go test ./internal/transcript/... | unknowns:resource_factory:5, churn:100 | review unknowns + churn |
| 52 | `internal/rankings/apply.go` | 3 | medium | inspect | unknowns, churn | go test ./internal/rankings/... | unknowns:resource_factory:2, churn:62 | review unknowns + churn |

## Documentation Rows

Documentation and guide files are separated from the production-ranked code queue.

| rank | file | score | confidence | actionability | evidence | review command | top signals |
| ---: | --- | ---: | --- | --- | --- | --- | --- |
| 1 | `README.md` | 4 | medium | verify_first | path-risk, sdk-dx, cochange, ownership, churn | go test ./... | path:readme, sdk_dx:surface_path, cochange:partners:3 |
| 2 | `CLAUDE.md` | 4 | medium | verify_first | cochange, ownership, churn, bugfix-history | - | cochange:partners:1, cochange:max_jaccard:0.62, cochange:bugfix_overlap |
| 3 | `docs/handoff-merit-eval.md` | 4 | medium | verify_first | cochange, ownership, churn | go test ./... | cochange:partners:2, cochange:max_jaccard:1.00, cochange:churn_overlap |
| 4 | `docs/handoff.md` | 4 | medium | verify_first | cochange, ownership, churn, bugfix-history | go test ./... | cochange:partners:2, cochange:max_jaccard:1.00, cochange:bugfix_overlap |
| 5 | `internal/prompts/defaults/section.md` | 3 | medium | verify_first | cochange, churn, bugfix-history | - | cochange:partners:1, cochange:max_jaccard:1.00, cochange:bugfix_overlap |
| 6 | `internal/prompts/defaults/edit-section.md` | 3 | medium | verify_first | cochange, churn, bugfix-history | - | cochange:partners:1, cochange:max_jaccard:1.00, cochange:bugfix_overlap |
| 7 | `internal/prompts/defaults/research.md` | 1 | medium | verify_first | churn, bugfix-history | - | churn:2, bugfix_touches:1 |
| 8 | `docs/extraction-providers.md` | 1 | low | verify_first | churn | go test ./... | churn:106 |
| 9 | `docs/remote-eval-crosswalk.md` | 1 | low | verify_first | churn | go test ./... | churn:87 |
| 10 | `docs/pipeline-qa.md` | 1 | low | verify_first | churn | go test ./... | churn:82 |
| 11 | `AGENTS.md` | 1 | low | verify_first | churn | - | churn:76 |
| 12 | `docs/slither.md` | 1 | low | verify_first | size | go test ./... | size:316 lines |
| 13 | `internal/prompts/defaults/merit-judge.md` | 1 | low | verify_first | churn | - | churn:25 |
| 14 | `internal/prompts/defaults/judge.md` | 1 | low | verify_first | churn | - | churn:25 |
| 15 | `internal/prompts/defaults/comedy-judge.md` | 1 | low | verify_first | churn | - | churn:24 |
| 16 | `internal/prompts/defaults/publish-judge.md` | 1 | low | verify_first | churn | - | churn:21 |
| 17 | `internal/prompts/defaults/trace-go.md` | 1 | low | verify_first | churn | - | churn:14 |
| 18 | `internal/prompts/defaults/fuse.md` | 1 | low | verify_first | churn | - | churn:13 |
| 19 | `internal/prompts/defaults/label-classification.md` | 1 | low | verify_first | churn | - | churn:13 |
| 20 | `internal/prompts/defaults/label-sentiment.md` | 1 | low | verify_first | churn | - | churn:13 |
| 21 | `internal/prompts/defaults/code.md` | 1 | low | verify_first | churn | - | churn:12 |
| 22 | `internal/prompts/defaults/outline.md` | 1 | low | verify_first | churn | - | churn:12 |
| 23 | `internal/prompts/defaults/recognize.md` | 1 | low | verify_first | churn | - | churn:8 |

## Detailed Signals

Showing the first `80` of `210` rows; full per-risk fields remain available in `--json` output.

| rank | file | seed_score | class | actionability | churn | fix_touches | lines | key risk fields | test_gap | reasons |
| ---: | --- | ---: | --- | --- | ---: | ---: | ---: | --- | --- | --- |
| 1 | `internal/actions/digest/digest.go` | 2.72 | git_history | inspect | 680 | 2 | 445 | content_risk=15, hotspot_risk=9, cochange_risk=10, ownership_risk=4 | false | content:reliability_policy_boundary:5, content:resource_factory:1, content:rate_limit_boundary:1 |
| 2 | `internal/extractscore/score.go` | 2.38 | git_history | inspect | 264 | 4 | 249 | content_risk=16, hotspot_risk=6, unknowns_risk=8, cochange_risk=7, +1 more | false | content:audit_correctness_drift_metric:3, content:error_context_dropped:1, content:resource_factory:2 |
| 3 | `cmd/digest.go` | 2.32 | git_history | inspect | 376 | 4 | 279 | content_risk=10, hotspot_risk=6, unknowns_risk=6, cochange_risk=10, +1 more | false | content:reliability_policy_boundary:5, unknowns:env_assumptions:4, cochange:partners:6 |
| 4 | `internal/extractscore/structured.go` | 2.19 | git_history | inspect | 511 | 0 | 512 | content_risk=24, smell_risk=4, hotspot_risk=7, unknowns_risk=5 | false | content:audit_correctness_drift_metric:5, content:error_context_dropped:1, content:resource_factory:3 |
| 5 | `internal/prompts/prompts.go` | 2.00 | git_history | inspect | 406 | 0 | 269 | content_risk=20, unknowns_risk=2, env_contract_risk=5, centrality_risk=6, +2 more | false | content:drift_hazard_comment:2, content:error_context_dropped:1, content:resource_factory:1 |
| 6 | `cmd/eval.go` | 1.89 | git_history | inspect | 337 | 1 | 178 | content_risk=31, unknowns_risk=6, cochange_risk=6, ownership_risk=3 | false | content:shell_boundary:1, content:reliability_policy_boundary:5, content:audit_correctness_drift_metric:5 |
| 7 | `cmd/label.go` | 1.82 | git_history | inspect | 177 | 2 | 156 | content_risk=34, hotspot_risk=2, unknowns_risk=4, cochange_risk=6, +1 more | false | content:reliability_policy_boundary:5, content:audit_correctness_drift_metric:5, content:drift_hazard_comment:1 |
| 8 | `cmd/comedy.go` | 1.73 | git_history | inspect | 265 | 1 | 204 | content_risk=13, hotspot_risk=2, unknowns_risk=6, cochange_risk=7, +1 more | false | content:reliability_policy_boundary:5, content:rate_limit_boundary:1, unknowns:env_assumptions:3 |
| 9 | `internal/actions/eval/metrics.go` | 1.72 | git_history | inspect | 185 | 1 | 138 | content_risk=15, smell_risk=4, cochange_risk=7, ownership_risk=2 | false | content:audit_correctness_drift_metric:5, smell:case_cascade:9, cochange:partners:1 |
| 10 | `cmd/rankings.go` | 1.63 | git_history | inspect | 276 | 0 | 277 | content_risk=26, unknowns_risk=4, env_contract_risk=5, ownership_risk=2 | false | content:shell_boundary:1, content:looped_io_or_query:1, content:reliability_policy_boundary:5 |
| 11 | `internal/actions/eval/eval.go` | 1.59 | git_history | inspect | 217 | 1 | 206 | content_risk=23, hotspot_risk=2, unknowns_risk=2 | false | content:audit_correctness_drift_metric:5, content:error_context_dropped:2, content:resource_factory:1 |
| 12 | `internal/labelscore/score.go` | 1.54 | git_history | inspect | 171 | 1 | 190 | content_risk=15, hotspot_risk=2, unknowns_risk=6 | false | content:audit_correctness_drift_metric:5, unknowns:resource_factory:2, hotspot:bugfix_complexity |
| 13 | `internal/codeeval/run.go` | 1.52 | git_history | inspect | 229 | 0 | 234 | content_risk=20, hotspot_risk=4, unknowns_risk=8 | false | content:shell_boundary:1, content:async_or_concurrent_boundary:1, content:reliability_policy_boundary:5 |
| 14 | `internal/structured/series.go` | 1.47 | git_history | inspect | 204 | 0 | 205 | content_risk=15, hotspot_risk=4, unknowns_risk=8 | false | content:custom_security_or_compliance_surface:5, unknowns:nested_loop_scale:1, unknowns:resource_factory:2 |
| 15 | `internal/extractscore/digest.go` | 1.43 | git_history | inspect | 218 | 0 | 205 | content_risk=11, unknowns_risk=5, centrality_risk=5, cochange_risk=8, +1 more | false | content:audit_correctness_drift_metric:2, content:error_context_dropped:1, content:resource_factory:1 |
| 16 | `cmd/digestscore.go` | 1.38 | git_history | inspect | 145 | 0 | 120 | content_risk=16, cochange_risk=7, ownership_risk=2 | false | content:audit_correctness_drift_metric:4, content:resource_factory:2, cochange:partners:2 |
| 17 | `cmd/digestgrade.go` | 1.37 | git_history | inspect | 230 | 0 | 207 | content_risk=11, unknowns_risk=6, cochange_risk=7, ownership_risk=3 | false | content:read_all_or_global_growth:1, content:resource_factory:2, unknowns:env_assumptions:4 |
| 18 | `internal/extractscore/report.go` | 1.36 | git_history | inspect | 55 | 2 | 52 | content_risk=15 | false | content:audit_correctness_drift_metric:5, churn:55, bugfix_touches:2 |
| 19 | `cmd/trace.go` | 1.33 | git_history | inspect | 185 | 0 | 186 | content_risk=16, unknowns_risk=4 | false | content:reliability_policy_boundary:5, content:rate_limit_boundary:2, unknowns:env_assumptions:2 |
| 20 | `internal/labelscore/report.go` | 1.24 | git_history | inspect | 96 | 0 | 93 | content_risk=15, unknowns_risk=2, cochange_risk=4 | false | content:audit_correctness_drift_metric:5, unknowns:nested_loop_scale:1, cochange:partners:1 |
| 21 | `cmd/evalroot.go` | 1.16 | git_history | inspect | 96 | 0 | 81 | content_risk=15, ownership_risk=2 | false | content:audit_correctness_drift_metric:5, ownership:risky_single_author, churn:96 |
| 22 | `internal/ai/client.go` | 1.66 | git_history | inspect | 151 | 2 | 126 | path_risk=2, content_risk=12, sdk_dx_risk=1, centrality_risk=6, +1 more | true | path:client, content:reliability_policy_boundary:1, content:blocking_inline_worker:1 |
| 23 | `internal/config/config.go` | 1.66 | git_history | inspect | 209 | 0 | 166 | path_risk=3, content_risk=11, unknowns_risk=2, env_contract_risk=5, +3 more | false | path:config, content:reliability_policy_boundary:1, content:drift_hazard_comment:1 |
| 24 | `internal/extractscore/digest_report.go` | 1.19 | git_history | inspect | 105 | 0 | 98 | content_risk=14, cochange_risk=6 | false | content:audit_correctness_drift_metric:4, cochange:partners:2, cochange:max_jaccard:0.67 |
| 25 | `cmd/code.go` | 1.16 | git_history | inspect | 162 | 0 | 163 | content_risk=13, unknowns_risk=4 | false | content:reliability_policy_boundary:5, content:rate_limit_boundary:1, unknowns:env_assumptions:2 |
| 26 | `internal/traceeval/gold.go` | 0.86 | git_history | inspect | 71 | 0 | 72 | content_risk=11, unknowns_risk=2 | false | content:shell_boundary:1, content:reliability_policy_boundary:3, content:error_context_dropped:1 |
| 27 | `internal/actions/digest/digest_test.go` | 1.65 | git_history | verify_first | 466 | 1 | 387 | content_risk=24, smell_risk=4, hotspot_risk=6, cochange_risk=10, +1 more | false | content:background_context:4, content:reliability_policy_boundary:3, content:resource_factory:5 |
| 28 | `internal/extractscore/score_test.go` | 1.30 | git_history | verify_first | 151 | 4 | 150 | content_risk=15, hotspot_risk=2, cochange_risk=7, ownership_risk=2 | false | content:audit_correctness_drift_metric:5, cochange:partners:1, cochange:max_jaccard:1.00 |
| 29 | `docs/remote-eval-report.html` | 0.81 | git_history | verify_first | 2651 | 0 | 2650 | content_risk=10 | false | content:async_or_concurrent_boundary:1, content:agent_dispatch_boundary:1, content:plugin_agent_boundary:1 |
| 30 | `internal/actions/eval/metrics_test.go` | 0.69 | git_history | verify_first | 91 | 1 | 80 | content_risk=15, cochange_risk=6, ownership_risk=2 | false | content:audit_correctness_drift_metric:5, cochange:partners:1, cochange:max_jaccard:1.00 |
| 31 | `docs/digest-review.html` | 0.67 | git_history | verify_first | 494 | 0 | 235 | content_risk=62, cochange_risk=5, ownership_risk=3 | false | content:shell_boundary:1, content:browser_injection:2, content:async_or_concurrent_boundary:1 |
| 32 | `internal/codeeval/codeeval_test.go` | 0.49 | git_history | verify_first | 162 | 0 | 181 | content_risk=29 | false | content:shell_boundary:2, content:background_context:4, content:reliability_policy_boundary:1 |
| 33 | `internal/codeeval/fixture.go` | 0.48 | git_history | verify_first | 79 | 1 | 80 | content_risk=14, unknowns_risk=2 | false | content:error_context_dropped:1, content:resource_factory:1, content:custom_security_or_compliance_surface:3 |
| 34 | `internal/traceeval/fixture.go` | 0.42 | git_history | verify_first | 123 | 0 | 124 | content_risk=21 | false | content:reliability_policy_boundary:4, content:drift_hazard_comment:1, content:error_context_dropped:2 |
| 35 | `cmd_test.go` | 0.35 | git_history | verify_first | 108 | 1 | 147 | content_risk=10, unknowns_risk=2, ownership_risk=2 | false | content:shell_boundary:4, content:resource_factory:1, unknowns:env_assumptions:1 |
| 36 | `internal/actions/eval/eval_test.go` | 0.32 | git_history | verify_first | 69 | 0 | 70 | content_risk=15 | false | content:background_context:1, content:audit_correctness_drift_metric:3, content:single_item_slice_to_aggregate:1 |
| 37 | `internal/comedyeval/fixture.go` | 0.23 | git_history | verify_first | 57 | 0 | 58 | content_risk=14 | false | content:async_messaging_boundary:3, content:error_context_dropped:1, content:resource_factory:1 |
| 38 | `docs/label-classification-routing.html` | 0.00 | git_history | verify_first | 1 | 0 | 1 | content_risk=15 | false | content:audit_correctness_drift_metric:5, churn:1 |
| 39 | `docs/label-sentiment-routing.html` | 0.00 | git_history | verify_first | 1 | 0 | 1 | content_risk=15 | false | content:audit_correctness_drift_metric:5, churn:1 |
| 40 | `cmd/chunk.go` | 1.98 | git_history | inspect | 635 | 3 | 242 | content_risk=3, hotspot_risk=2, centrality_risk=3, cochange_risk=8, +1 more | false | content:workflow_mode_validation_gate:1, centrality:incoming_refs:2, cochange:partners:2 |
| 41 | `cmd/digesttournament.go` | 1.29 | git_history | inspect | 290 | 0 | 277 | content_risk=9, hotspot_risk=4, unknowns_risk=4, ownership_risk=2 | false | content:audit_correctness_drift_metric:1, content:resource_factory:3, unknowns:env_assumptions:2 |
| 42 | `cmd/count.go` | 1.26 | git_history | inspect | 122 | 3 | 154 | content_risk=5, cochange_risk=9, ownership_risk=2 | false | content:resource_lifecycle:1, content:read_all_or_global_growth:1, cochange:partners:2 |
| 43 | `internal/transcript/transcript.go` | 1.26 | git_history | inspect | 92 | 2 | 91 | content_risk=9, unknowns_risk=3, cochange_risk=6, ownership_risk=2 | false | content:drift_hazard_comment:1, content:custom_security_or_compliance_surface:2, unknowns:recursive_control_flow:1 |
| 44 | `go.mod` | 1.23 | git_history | dependency_review | 106 | 1 | 43 | path_risk=4, content_risk=6, dependency_health_risk=4, cochange_risk=7, +1 more | false | path:go.mod, content:go_module_replace:2, dependency_health:go_module_replace |
| 45 | `internal/comedyeval/generate.go` | 1.19 | git_history | inspect | 132 | 2 | 73 | content_risk=7, cochange_risk=7, ownership_risk=2 | false | content:async_or_concurrent_boundary:1, content:reliability_policy_boundary:1, content:drift_hazard_comment:1 |
| 46 | `internal/gradecal/calibrate.go` | 1.15 | git_history | inspect | 260 | 0 | 233 | content_risk=5, unknowns_risk=8, centrality_risk=4, cochange_risk=8, +1 more | false | content:custom_security_or_compliance_surface:1, unknowns:nested_loop_scale:1, unknowns:resource_factory:3 |
| 47 | `runs/run-label.sh` | 1.06 | git_history | inspect | 39 | 2 | 24 | content_risk=7, cochange_risk=8, ownership_risk=3 | false | content:dynamic_eval:1, content:reliability_policy_boundary:2, cochange:partners:4 |
| 48 | `internal/embedcache/embedcache.go` | 0.94 | git_history | inspect | 148 | 1 | 121 | content_risk=7, ownership_risk=2 | false | content:vector_embedding_surface:5, content:resource_factory:1, ownership:risky_single_author |
| 49 | `internal/rankings/rankings.go` | 0.86 | git_history | inspect | 79 | 0 | 80 | content_risk=8, unknowns_risk=2, env_contract_risk=4, cochange_risk=4 | false | content:error_context_dropped:2, content:resource_factory:1, unknowns:env_assumptions:1 |
| 50 | `README.md` | 0.85 | git_history | verify_first | 242 | 0 | 264 | path_risk=2, sdk_dx_risk=1, cochange_risk=7, ownership_risk=4 | false | path:readme, sdk_dx:surface_path, cochange:partners:3 |
| 51 | `internal/transcript/ytdlp.go` | 0.83 | git_history | inspect | 131 | 2 | 126 | hotspot_risk=2, unknowns_risk=6, cochange_risk=7 | false | unknowns:resource_factory:6, cochange:partners:1, cochange:max_jaccard:0.67 |
| 52 | `internal/config/defaults/config.yaml` | 0.80 | git_history | inspect | 96 | 1 | 65 | path_risk=3, cochange_risk=9, ownership_risk=4 | false | path:config, cochange:partners:5, cochange:max_jaccard:0.78 |
| 53 | `CLAUDE.md` | 0.66 | git_history | verify_first | 157 | 1 | 77 | cochange_risk=7, ownership_risk=4 | false | cochange:partners:1, cochange:max_jaccard:0.62, cochange:bugfix_overlap |
| 54 | `docs/handoff-merit-eval.md` | 0.54 | git_history | verify_first | 189 | 0 | 114 | cochange_risk=7, ownership_risk=3 | false | cochange:partners:2, cochange:max_jaccard:1.00, cochange:churn_overlap |
| 55 | `docs/handoff.md` | 0.51 | git_history | verify_first | 68 | 1 | 65 | cochange_risk=8, ownership_risk=2 | false | cochange:partners:2, cochange:max_jaccard:1.00, cochange:bugfix_overlap |
| 56 | `internal/gradecal/sabotage.go` | 0.49 | git_history | inspect | 156 | 0 | 149 | unknowns_risk=5, cochange_risk=7 | false | unknowns:nested_loop_scale:1, unknowns:resource_factory:1, cochange:partners:3 |
| 57 | `internal/gradecal/recognize.go` | 0.41 | git_history | inspect | 170 | 0 | 171 | unknowns_risk=8 | false | unknowns:nested_loop_scale:1, unknowns:resource_factory:4, churn:170 |
| 58 | `internal/structured/markdown.go` | 0.26 | git_history | inspect | 91 | 0 | 92 | unknowns_risk=8 | false | unknowns:nested_loop_scale:1, unknowns:resource_factory:2, churn:91 |
| 59 | `internal/transcript/transcript_test.go` | 0.41 | git_history | verify_first | 246 | 3 | 247 | cochange_risk=9, ownership_risk=2 | false | cochange:partners:2, cochange:max_jaccard:0.67, cochange:bugfix_overlap |
| 60 | `internal/traceeval/traceeval_test.go` | 0.26 | git_history | verify_first | 220 | 0 | 221 | content_risk=8, hotspot_risk=4 | false | content:background_context:4, hotspot:complexity_with_pressure:41, churn:220 |
| 61 | `internal/gradecal/recognize_test.go` | 0.10 | git_history | verify_first | 179 | 0 | 180 | content_risk=8 | false | content:background_context:4, churn:179 |
| 62 | `cmd/path_confine_test.go` | 0.08 | git_history | verify_first | 81 | 2 | 44 | content_risk=3, cochange_risk=8 | false | content:drift_hazard_comment:1, cochange:partners:2, cochange:max_jaccard:1.00 |
| 63 | `internal/labelscore/fixture.go` | 0.07 | git_history | verify_first | 68 | 1 | 69 | content_risk=8 | false | content:audit_correctness_drift_metric:1, content:error_context_dropped:1, content:resource_factory:1 |
| 64 | `internal/gradecal/calibrate_test.go` | 0.03 | git_history | verify_first | 162 | 0 | 159 | content_risk=4, cochange_risk=8, ownership_risk=2 | false | content:background_context:2, cochange:partners:4, cochange:max_jaccard:1.00 |
| 65 | `cmd/count_test.go` | 0.00 | git_history | verify_first | 103 | 2 | 52 | cochange_risk=8 | false | cochange:partners:2, cochange:max_jaccard:1.00, cochange:bugfix_overlap |
| 66 | `internal/gradecal/panel_test.go` | 0.00 | git_history | verify_first | 114 | 0 | 115 | content_risk=8 | false | content:background_context:4, churn:114 |
| 67 | `internal/extractscore/gate_test.go` | 0.00 | git_history | verify_first | 39 | 0 | 40 | content_risk=9 | false | content:audit_correctness_drift_metric:3, churn:39 |
| 68 | `internal/gradecal/tournament.go` | 1.07 | git_history | inspect | 340 | 0 | 293 | content_risk=4, hotspot_risk=4, unknowns_risk=3, ownership_risk=3 | false | content:async_or_concurrent_boundary:2, unknowns:recursive_control_flow:1, ownership:risky_single_author |
| 69 | `internal/extractscore/gate.go` | 0.67 | git_history | inspect | 66 | 0 | 65 | content_risk=6, unknowns_risk=2, cochange_risk=6 | false | content:audit_correctness_drift_metric:2, unknowns:nested_loop_scale:1, cochange:partners:2 |
| 70 | `cmd/root.go` | 0.64 | git_history | inspect | 136 | 2 | 59 | ownership_risk=4 | false | ownership:risky_single_author, ownership:concentrated_touches:15, churn:136 |
| 71 | `internal/fsutil/write.go` | 0.60 | git_history | inspect | 18 | 1 | 55 | content_risk=4, centrality_risk=5 | false | content:resource_lifecycle:1, content:custom_infra_reinvention:1, centrality:incoming_refs:11 |
| 72 | `cmd/distill-eval/main.go` | 0.59 | git_history | inspect | 26 | 0 | 27 | path_risk=4, content_risk=4 | false | path:main.go, content:process_exit:1, content:background_context:1 |
| 73 | `cmd/chunk_semantic.go` | 0.58 | git_history | inspect | 110 | 1 | 83 | unknowns_risk=6, env_contract_risk=5, ownership_risk=2 | false | unknowns:env_assumptions:3, env_contract:undocumented:1, env_contract:vars:DISTILL_EMBEDDING_ENDPOINT |
| 74 | `main.go` | 0.58 | git_history | inspect | 23 | 0 | 27 | path_risk=4, content_risk=4 | false | path:main.go, content:process_exit:1, content:background_context:1 |
| 75 | `internal/extractscore/structured_report.go` | 0.52 | git_history | inspect | 64 | 0 | 65 | content_risk=6 | false | content:audit_correctness_drift_metric:2, churn:64 |
| 76 | `internal/rankings/fetch.go` | 0.48 | git_history | inspect | 143 | 0 | 144 | content_risk=2, unknowns_risk=5 | false | unknowns:nested_loop_scale:1, unknowns:resource_factory:1, churn:143 |
| 77 | `internal/rankings/defaults/rankings.yaml` | 0.40 | git_history | inspect | 123 | 0 | 124 | cochange_risk=5, ownership_risk=2 | false | cochange:partners:1, cochange:max_jaccard:1.00, cochange:churn_overlap |
| 78 | `internal/ai/embedder.go` | 0.40 | git_history | inspect | 45 | 0 | 46 | content_risk=4, unknowns_risk=2 | false | content:vector_embedding_surface:4, unknowns:nested_loop_scale:1, churn:45 |
| 79 | `internal/prompts/defaults/section.md` | 0.37 | git_history | verify_first | 38 | 1 | 23 | cochange_risk=6 | false | cochange:partners:1, cochange:max_jaccard:1.00, cochange:bugfix_overlap |
| 80 | `internal/prompts/defaults/edit-section.md` | 0.37 | git_history | verify_first | 35 | 1 | 18 | cochange_risk=6 | false | cochange:partners:1, cochange:max_jaccard:1.00, cochange:bugfix_overlap |

## Review Plan

| lane | files | top files | omitted | gates | verify | why |
| --- | ---: | --- | ---: | --- | --- | --- |
| `cli-ux` | 12 | cmd/digest.go, cmd/eval.go, cmd/label.go, cmd/comedy.go | 8 | flag parsing, help-text accuracy, exit codes, +1 more | go build ./..., go test ./cmd/..., +3 more | entrypoints, docs, SDK, or output-facing files need command and report checks |
| `api-contracts` | 1 | internal/ai/client.go | 0 | JSON schema stability, encoder/decoder round-trip, backward compatibility | go test ./..., go test ./internal/ai/... | API, OpenAPI, CORS, cookie, or serialization signals need contract checks |
| `data-integrity` | 6 | internal/config/config.go, internal/prompts/prompts.go, cmd/rankings.go, cmd/chunk_semantic.go | 2 | write atomicity, read-after-write, rollback on error | go test ./..., go test ./internal/config/..., +4 more | migration, store, or stateful artifact signals need integrity checks |
| `error-handling` | 12 | cmd/eval.go, cmd/rankings.go, internal/traceeval/gold.go, internal/codeeval/run.go | 8 | subprocess timeout, stderr capture, exit-code propagation, +1 more | go vet ./..., go test ./..., +8 more | subprocess, shell, or process-exit signals need error and timeout checks |
| `dependency-policy` | 1 | go.mod | 0 | dependency necessity, version pinning, replacement justification | go list -m all | dependency manifests and replacement policy need review |
| `lifecycle-concurrency` | 2 | cmd/distill-eval/main.go, main.go | 0 | context propagation, goroutine ownership, shutdown cleanup | go test -race ./..., go test ./cmd/distill-eval/..., +1 more | concurrency, context, or flaky-test signals need lifecycle checks |
| `performance` | 12 | internal/actions/digest/digest.go, internal/extractscore/structured.go, internal/extractscore/score.go, cmd/digest.go | 8 | bounded reads, resource limits, hot path evidence | go test ./..., go test ./internal/actions/digest/..., +6 more | large, hot, or unbounded-resource signals need bounds checks |
| `test-risk` | 1 | internal/ai/client.go | 0 | nearby coverage, assertion strength, flake controls | go test ./..., go test ./internal/ai/... | test gaps and weak test oracles need coverage checks |
| `coupling` | 12 | internal/config/config.go, internal/extractscore/digest.go, internal/prompts/prompts.go, cmd/chunk.go | 8 | fan-in blast radius, cochange partners, ownership concentration | go test ./internal/config/..., go test ./internal/extractscore/..., +6 more | centrality, cochange, or ownership concentration need blast-radius checks |
| `architecture` | 12 | internal/actions/digest/digest.go, internal/extractscore/score.go, cmd/digest.go, internal/extractscore/structured.go | 8 | central dependency blast radius, layer boundary, change sequencing | go build ./..., go test ./internal/actions/digest/..., +5 more | high-ranked multi-layer files need architecture review |
