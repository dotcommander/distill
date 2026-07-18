# Providers to follow — ranked by extraction-test performance

Provider = OpenRouter org prefix (model creator). Recall vs the hardened 86-fact golden set, across all swept models.
PASS = a model at 86/86. Aggregated from runs/INDEX-hardened.

| rank | provider | models | passed (86/86) | pass % | avg recall | best/cheapest 86/86 |
|---|---|---|---|---|---|---|
| 1 | qwen | 22 | 11 | 50% | 0.966 | qwen/qwen3-30b-a3b-instruct-2507 (0.084) |
| 2 | deepseek | 9 | 6 | 67% | 0.995 | deepseek/deepseek-v4-flash (0.113) |
| 3 | bytedance-seed | 2 | 2 | 100% | 1.000 | bytedance-seed/seed-1.6-flash (0.131) |
| 4 | google | 12 | 2 | 17% | 0.968 | google/gemini-2.5-flash-lite-preview-09-2025 (0.175) |
| 5 | nex-agi | 1 | 1 | 100% | 1.000 | nex-agi/nex-n2-pro (0.438) |
| 6 | perplexity | 1 | 1 | 100% | 1.000 | perplexity/sonar (1.000) |
| 7 | poolside | 2 | 1 | 50% | 0.994 | poolside/laguna-xs.2 (0.125) |
| 8 | tencent | 2 | 1 | 50% | 0.994 | tencent/hunyuan-a13b-instruct (0.248) |
| 9 | xiaomi | 2 | 1 | 50% | 0.994 | xiaomi/mimo-v2.5 (0.175) |
| 10 | z-ai | 3 | 1 | 33% | 0.992 | z-ai/glm-4.7-flash (0.145) |
| 11 | amazon | 2 | 1 | 50% | 0.988 | amazon/nova-lite-v1 (0.105) |
| 12 | inclusionai | 6 | 1 | 17% | 0.975 | inclusionai/ling-2.6-1t (0.212) |
| 13 | arcee-ai | 2 | 1 | 50% | 0.971 | arcee-ai/trinity-large-thinking (0.387) |
| 14 | openai | 7 | 1 | 14% | 0.897 | openai/gpt-5-nano (0.137) |
| 15 | verify-default | 1 | 0 | 0% | 0.988 | — |
| 16 | nousresearch | 2 | 0 | 0% | 0.977 | — |
| 17 | stepfun | 1 | 0 | 0% | 0.977 | — |
| 18 | verify-wormhole | 1 | 0 | 0% | 0.977 | — |
| 19 | microsoft | 2 | 0 | 0% | 0.953 | — |
| 20 | smoke | 1 | 0 | 0% | 0.953 | — |
| 21 | cohere | 2 | 0 | 0% | 0.948 | — |
| 22 | ibm-granite | 5 | 0 | 0% | 0.947 | — |
| 23 | minimax | 4 | 0 | 0% | 0.945 | — |
| 24 | meta-llama | 8 | 0 | 0% | 0.887 | — |
| 25 | mistralai | 10 | 0 | 0% | 0.872 | — |
| 26 | nvidia | 3 | 0 | 0% | 0.872 | — |
| 27 | liquid | 1 | 0 | 0% | 0.814 | — |
| 28 | inception | 1 | 0 | 0% | 0.721 | — |
| 29 | upstage | 1 | 0 | 0% | 0.709 | — |
| 30 | rekaai | 1 | 0 | 0% | 0.023 | — |

## Follow these (≥1 model at 86/86, ranked by passers then avg recall)

1. **qwen** — 11/22 models at 86/86 (avg recall 0.966)
2. **deepseek** — 6/9 models at 86/86 (avg recall 0.995)
3. **bytedance-seed** — 2/2 models at 86/86 (avg recall 1.000)
4. **google** — 2/12 models at 86/86 (avg recall 0.968)
5. **nex-agi** — 1/1 models at 86/86 (avg recall 1.000)
6. **perplexity** — 1/1 models at 86/86 (avg recall 1.000)
7. **poolside** — 1/2 models at 86/86 (avg recall 0.994)
8. **tencent** — 1/2 models at 86/86 (avg recall 0.994)
9. **xiaomi** — 1/2 models at 86/86 (avg recall 0.994)
10. **z-ai** — 1/3 models at 86/86 (avg recall 0.992)
11. **amazon** — 1/2 models at 86/86 (avg recall 0.988)
12. **inclusionai** — 1/6 models at 86/86 (avg recall 0.975)
13. **arcee-ai** — 1/2 models at 86/86 (avg recall 0.971)
14. **openai** — 1/7 models at 86/86 (avg recall 0.897)
