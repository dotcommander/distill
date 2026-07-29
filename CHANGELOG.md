# Changelog

## v0.3.0 (2026-07-29)

### Features

- Pack multiple digest inputs independently and losslessly, preserving source
  boundaries while exposing only safe source ordinals to providers.
- Make finite `--max-calls` planning authoritative across dry-run and execution,
  reserving mandatory work before bounded retries and cross-provider fallback.
- Add schema-v2 artifact binding, response provenance sidecars, a versioned run
  ledger, and atomic success or failure summaries for safe checkpoint reuse.
- Publish the final article only after every mandatory research, outline, write,
  edit, and configured quality gate succeeds.

### Fixes

- Count only provider attempts that reach transport and classify retryable
  failures without retrying authentication, quota, configuration, cancellation,
  or request-budget failures.
- Preserve incompatible or mismatched artifact directories without mutation and
  refuse legacy schema-v1 resume attempts with a fresh-path recovery hint.

## v0.2.1 (2026-07-29)

### Fixes

- Refuse unsafe non-empty digest artifact directories instead of rebinding them.
- Enforce `--max-calls` as a shared text-and-embedding request budget and report
  finite actual usage.
- Preflight every evaluator chunk corpus and reject malformed precision verdict
  index sets before trusting results.
- Preserve identity-bearing extraction values and document the explicit cascade
  escalation and all-role model-pinning contracts.

## v0.2.0 (2026-07-29)

### Features

- Publish chunk generations atomically and require explicit output paths to be
  absent.
- Read digest directory inputs from validated manifests in declared chunk order.

### Fixes

- Isolate direct-provider API keys from the OpenAI fallback.
- Include all article-shaping inputs in digest cache keys and invalidate the
  earlier key format.
- Harden judge JSON extraction and trace output parsing.

### Other

- Document and isolate the repository's scrubbed public-history boundary.
- Remove obsolete indirect module checksums.
