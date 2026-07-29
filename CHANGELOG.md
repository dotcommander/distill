# Changelog

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
