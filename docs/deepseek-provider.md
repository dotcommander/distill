# DeepSeek Provider Notes

Run direct DeepSeek digests with the DeepSeek key, not an OpenRouter key:

```bash
export DEEPSEEK_API_KEY=sk-...
distill digest document.md --deepseek --concurrency 2

# Equivalent when selecting a DeepSeek model directly.
distill digest document.md --model deepseek/deepseek-v4-pro --concurrency 2
```

`distill` routes DeepSeek text models to the built-in Wormhole `deepseek`
provider using `deepseek_base_url` (`https://api.deepseek.com`). Remote custom
endpoint overrides are disabled; `--base-url` and `$DISTILL_BASE_URL` are
local-only. OpenRouter-style slugs such as `deepseek/deepseek-v4-pro` are
normalized to DeepSeek model IDs such as `deepseek-v4-pro` before the request.

## Request Shape

Direct DeepSeek chat completions default to thinking mode. `distill` sends
`thinking: {"type":"disabled"}` for direct DeepSeek calls unless the caller
explicitly sets a `thinking` provider option. Digest runs also send a stable,
hashed `user_id` derived from the source path. That keeps the identifier stable
for provider-side cache locality without exposing the local path.

Existing user configs must still inherit new default keys such as
`deepseek_base_url`. Keep the embedded-default merge in `internal/config` intact:
load embedded defaults first, then merge user config on top.

## Batching And Caching

DeepSeek's public OpenAI-compatible chat surface does not provide a separate
batch-completions endpoint for this workload. Use digest's existing client-side
parallelism instead:

```bash
distill digest document.md --deepseek --concurrency 2
```

Prompt caching is provider-side and automatic. A live probe on a 6 KB slice of
`document.md` with the same `user_id`
returned:

| request | prompt cache hit tokens | prompt cache miss tokens |
|---|---:|---:|
| first identical call | 0 | 1347 |
| second identical call | 1280 | 67 |

This means repeated request prefixes can become cheaper/faster when the request
shape and stable user identifier are preserved.

## QA Evidence

The direct DeepSeek path was tested against:

```bash
go run . digest document.md \
  --model deepseek/deepseek-v4-pro \
  --chunk-size 12000 \
  --concurrency 2 \
  --timeout 180 \
  --retries 1 \
  --no-edit \
  --out /tmp/distill-j2-deepseek.md \
  --artifacts /tmp/distill-j2-deepseek-artifacts
```

Result: completed in 4m7s, processed 28 chunks, drafted 15 outline sections,
wrote `/tmp/distill-j2-deepseek.md`, and recorded no chunk failures.

`digest score` can verify parseability of a generated digest, but the default
86-fact golden set is the Halcyon fixture. Scores from unrelated sources such
as `j2/raw.md` are not recall-quality evidence.

## Maintenance Checks

Use these targeted checks after changing DeepSeek routing, provider options, or
config default merging:

```bash
go test ./internal/config ./cmd ./internal/ai
go test ./...
go build ./...
git diff --check
```
