# Changelog

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
