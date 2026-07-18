# Extraction-model benchmark — hardened 86-fact test

Test: `distill-eval eval facts` recall vs the **86-fact** golden set (`testdata/extraction/expected`, 8 chunks). PASS = 86/86.
Provider: OpenRouter ($/M tokens; blend = 0.75·prompt + 0.25·completion). Local baseline = Qwen-35B.
Single run/model @ temp 0.2 (non-deterministic — confirm a borderline 85↔86 with repeats). Generated from runs/INDEX-hardened.

| rank | model | recall | covered | PASS | blend $/M | $/Mout | ctx | provider |
|---|---|---|---|---|---|---|---|---|
| 1 | (local) Qwen-35B | 1.000 | 86/86 | ✅ | 0.000 | 0.000 | - | local |
| 2 | qwen/qwen3-30b-a3b-instruct-2507 | 1.000 | 86/86 | ✅ | 0.084 | 0.193 | 128K | qwen |
| 3 | qwen/qwen3-235b-a22b-2507 | 1.000 | 86/86 | ✅ | 0.092 | 0.100 | 256K | qwen |
| 4 | qwen/qwen3-235b-a22b-thinking-2507 | 1.000 | 86/86 | ✅ | 0.100 | 0.100 | 256K | qwen |
| 5 | amazon/nova-lite-v1 | 1.000 | 86/86 | ✅ | 0.105 | 0.240 | 292K | amazon |
| 6 | qwen/qwen3.5-9b | 1.000 | 86/86 | ✅ | 0.112 | 0.150 | 256K | qwen |
| 7 | deepseek/deepseek-v4-flash | 1.000 | 86/86 | ✅ | 0.113 | 0.180 | 1024K | deepseek |
| 8 | qwen/qwen3.5-flash-02-23 | 1.000 | 86/86 | ✅ | 0.114 | 0.260 | 976K | qwen |
| 9 | poolside/laguna-xs.2 | 1.000 | 86/86 | ✅ | 0.125 | 0.200 | 256K | poolside |
| 10 | bytedance-seed/seed-1.6-flash | 1.000 | 86/86 | ✅ | 0.131 | 0.300 | 256K | bytedance-seed |
| 11 | openai/gpt-5-nano | 1.000 | 86/86 | ✅ | 0.137 | 0.400 | 390K | openai |
| 12 | z-ai/glm-4.7-flash | 1.000 | 86/86 | ✅ | 0.145 | 0.400 | 198K | z-ai |
| 13 | qwen/qwen3-30b-a3b-thinking-2507 | 1.000 | 86/86 | ✅ | 0.160 | 0.400 | 128K | qwen |
| 14 | bytedance-seed/seed-2.0-mini | 1.000 | 86/86 | ✅ | 0.175 | 0.400 | 256K | bytedance-seed |
| 15 | google/gemini-2.5-flash-lite-preview-09-2025 | 1.000 | 86/86 | ✅ | 0.175 | 0.400 | 1024K | google |
| 16 | xiaomi/mimo-v2.5 | 1.000 | 86/86 | ✅ | 0.175 | 0.280 | 1024K | xiaomi |
| 17 | google/gemma-4-31b-it | 1.000 | 86/86 | ✅ | 0.177 | 0.350 | 256K | google |
| 18 | inclusionai/ling-2.6-1t | 1.000 | 86/86 | ✅ | 0.212 | 0.625 | 256K | inclusionai |
| 19 | qwen/qwen3-30b-a3b | 1.000 | 86/86 | ✅ | 0.215 | 0.500 | 128K | qwen |
| 20 | tencent/hunyuan-a13b-instruct | 1.000 | 86/86 | ✅ | 0.248 | 0.570 | 128K | tencent |
| 21 | deepseek/deepseek-v3.2 | 1.000 | 86/86 | ✅ | 0.257 | 0.343 | 128K | deepseek |
| 22 | deepseek/deepseek-v3.2-exp | 1.000 | 86/86 | ✅ | 0.305 | 0.410 | 160K | deepseek |
| 23 | deepseek/deepseek-chat-v3.1 | 1.000 | 86/86 | ✅ | 0.355 | 0.790 | 160K | deepseek |
| 24 | qwen/qwen3.6-35b-a3b | 1.000 | 86/86 | ✅ | 0.355 | 1.000 | 256K | qwen |
| 25 | qwen/qwen-2.5-72b-instruct | 1.000 | 86/86 | ✅ | 0.370 | 0.400 | 128K | qwen |
| 26 | arcee-ai/trinity-large-thinking | 1.000 | 86/86 | ✅ | 0.387 | 0.800 | 256K | arcee-ai |
| 27 | qwen/qwen-plus-2025-07-28 | 1.000 | 86/86 | ✅ | 0.390 | 0.780 | 976K | qwen |
| 28 | qwen/qwen-plus-2025-07-28:thinking | 1.000 | 86/86 | ✅ | 0.390 | 0.780 | 976K | qwen |
| 29 | nex-agi/nex-n2-pro | 1.000 | 86/86 | ✅ | 0.438 | 1.000 | 256K | nex-agi |
| 30 | deepseek/deepseek-v3.1-terminus | 1.000 | 86/86 | ✅ | 0.440 | 0.950 | 160K | deepseek |
| 31 | deepseek/deepseek-v4-pro | 1.000 | 86/86 | ✅ | 0.544 | 0.870 | 1024K | deepseek |
| 32 | perplexity/sonar | 1.000 | 86/86 | ✅ | 1.000 | 1.000 | 124K | perplexity |
| 33 | inclusionai/ling-2.6-flash | 0.988 | 85/86 | — | 0.015 | 0.030 | 256K | inclusionai |
| 34 | google/gemma-3-27b-it | 0.988 | 85/86 | — | 0.100 | 0.160 | 128K | google |
| 35 | tencent/hy3-preview | 0.988 | 85/86 | — | 0.100 | 0.210 | 256K | tencent |
| 36 | qwen/qwen3-coder-30b-a3b-instruct | 0.988 | 85/86 | — | 0.120 | 0.270 | 156K | qwen |
| 37 | qwen/qwen3-32b | 0.988 | 85/86 | — | 0.130 | 0.280 | 128K | qwen |
| 38 | meta-llama/llama-3-8b-instruct | 0.988 | 85/86 | — | 0.140 | 0.140 | 8K | meta-llama |
| 39 | nousresearch/hermes-4-70b | 0.988 | 85/86 | — | 0.198 | 0.400 | 128K | nousresearch |
| 40 | poolside/laguna-m.1 | 0.988 | 85/86 | — | 0.250 | 0.400 | 256K | poolside |
| 41 | openai/gpt-4o-mini | 0.988 | 85/86 | — | 0.262 | 0.600 | 125K | openai |
| 42 | openai/gpt-4o-mini-2024-07-18 | 0.988 | 85/86 | — | 0.262 | 0.600 | 125K | openai |
| 43 | qwen/qwen3-next-80b-a3b-thinking | 0.988 | 85/86 | — | 0.268 | 0.780 | 256K | qwen |
| 44 | qwen/qwen3-coder-next | 0.988 | 85/86 | — | 0.282 | 0.800 | 256K | qwen |
| 45 | z-ai/glm-4.5-air | 0.988 | 85/86 | — | 0.310 | 0.850 | 128K | z-ai |
| 46 | minimax/minimax-m2.5 | 0.988 | 85/86 | — | 0.337 | 0.900 | 200K | minimax |
| 47 | deepseek/deepseek-chat | 0.988 | 85/86 | — | 0.350 | 0.800 | 128K | deepseek |
| 48 | qwen/qwen3.5-35b-a3b | 0.988 | 85/86 | — | 0.355 | 1.000 | 256K | qwen |
| 49 | qwen/qwen-plus | 0.988 | 85/86 | — | 0.390 | 0.780 | 976K | qwen |
| 50 | z-ai/glm-4.6v | 0.988 | 85/86 | — | 0.450 | 0.900 | 128K | z-ai |
| 51 | minimax/minimax-m2.1 | 0.988 | 85/86 | — | 0.455 | 0.950 | 200K | minimax |
| 52 | xiaomi/mimo-v2.5-pro | 0.988 | 85/86 | — | 0.544 | 0.870 | 1024K | xiaomi |
| 53 | deepseek/deepseek-r1-distill-llama-70b | 0.988 | 85/86 | — | 0.800 | 0.800 | 125K | deepseek |
| 54 | inclusionai/ling-2.6-flash_run1 | 0.988 | 85/86 | — | 99 | 99 | 0K | inclusionai |
| 55 | inclusionai/ling-2.6-flash_run2 | 0.988 | 85/86 | — | 99 | 99 | 0K | inclusionai |
| 56 | inclusionai/ling-2.6-flash_run3 | 0.988 | 85/86 | — | 99 | 99 | 0K | inclusionai |
| 57 | verify-default | 0.988 | 85/86 | — | 99 | 99 | 0K | verify-default |
| 58 | amazon/nova-micro-v1 | 0.977 | 84/86 | — | 0.061 | 0.140 | 125K | amazon |
| 59 | google/gemma-3-12b-it | 0.977 | 84/86 | — | 0.075 | 0.150 | 128K | google |
| 60 | mistralai/mistral-small-3.2-24b-instruct | 0.977 | 84/86 | — | 0.106 | 0.200 | 125K | mistralai |
| 61 | stepfun/step-3.5-flash | 0.977 | 84/86 | — | 0.143 | 0.300 | 256K | stepfun |
| 62 | google/gemini-2.5-flash-lite | 0.977 | 84/86 | — | 0.175 | 0.400 | 1024K | google |
| 63 | meta-llama/llama-4-maverick | 0.977 | 84/86 | — | 0.262 | 0.600 | 1024K | meta-llama |
| 64 | mistralai/mistral-small-2603 | 0.977 | 84/86 | — | 0.262 | 0.600 | 256K | mistralai |
| 65 | deepseek/deepseek-chat-v3-0324 | 0.977 | 84/86 | — | 0.343 | 0.770 | 160K | deepseek |
| 66 | nvidia/llama-3.3-nemotron-super-49b-v1.5 | 0.977 | 84/86 | — | 0.400 | 0.400 | 128K | nvidia |
| 67 | verify-wormhole | 0.977 | 84/86 | — | 99 | 99 | 0K | verify-wormhole |
| 68 | ibm-granite/granite-4.0-h-micro | 0.965 | 83/86 | — | 0.041 | 0.112 | 127K | ibm-granite |
| 69 | mistralai/mistral-small-24b-instruct-2501 | 0.965 | 83/86 | — | 0.057 | 0.080 | 32K | mistralai |
| 70 | cohere/command-r7b-12-2024 | 0.965 | 83/86 | — | 0.066 | 0.150 | 125K | cohere |
| 71 | google/gemma-3n-e4b-it | 0.965 | 83/86 | — | 0.075 | 0.120 | 32K | google |
| 72 | microsoft/phi-4 | 0.965 | 83/86 | — | 0.088 | 0.140 | 16K | microsoft |
| 73 | google/gemma-4-26b-a4b-it | 0.965 | 83/86 | — | 0.128 | 0.330 | 256K | google |
| 74 | qwen/qwen3-8b | 0.965 | 83/86 | — | 0.137 | 0.400 | 128K | qwen |
| 75 | mistralai/ministral-8b-2512 | 0.965 | 83/86 | — | 0.150 | 0.150 | 256K | mistralai |
| 76 | openai/gpt-4.1-nano | 0.965 | 83/86 | — | 0.175 | 0.400 | 1023K | openai |
| 77 | mistralai/mistral-saba | 0.965 | 83/86 | — | 0.300 | 0.600 | 32K | mistralai |
| 78 | google/gemma-2-27b-it | 0.965 | 83/86 | — | 0.650 | 0.650 | 8K | google |
| 79 | nousresearch/hermes-3-llama-3.1-405b | 0.965 | 83/86 | — | 1.000 | 1.000 | 128K | nousresearch |
| 80 | qwen/qwen3-14b | 0.953 | 82/86 | — | 0.135 | 0.240 | 128K | qwen |
| 81 | meta-llama/llama-4-scout | 0.953 | 82/86 | — | 0.150 | 0.300 | 9765K | meta-llama |
| 82 | meta-llama/llama-3.3-70b-instruct | 0.953 | 82/86 | — | 0.155 | 0.320 | 128K | meta-llama |
| 83 | qwen/qwen3-coder-flash | 0.953 | 82/86 | — | 0.390 | 0.975 | 976K | qwen |
| 84 | meta-llama/llama-3.1-70b-instruct | 0.953 | 82/86 | — | 0.400 | 0.400 | 128K | meta-llama |
| 85 | google/gemma-3-4b-it_run1 | 0.953 | 82/86 | — | 99 | 99 | 0K | google |
| 86 | smoke | 0.953 | 82/86 | — | 99 | 99 | 0K | smoke |
| 87 | meta-llama/llama-3.1-8b-instruct | 0.942 | 81/86 | — | 0.022 | 0.030 | 128K | meta-llama |
| 88 | google/gemma-3-4b-it | 0.942 | 81/86 | — | 0.062 | 0.100 | 128K | google |
| 89 | ibm-granite/granite-4.1-8b | 0.942 | 81/86 | — | 0.062 | 0.100 | 128K | ibm-granite |
| 90 | arcee-ai/trinity-mini | 0.942 | 81/86 | — | 0.071 | 0.150 | 128K | arcee-ai |
| 91 | microsoft/phi-4-mini-instruct | 0.942 | 81/86 | — | 0.147 | 0.350 | 128K | microsoft |
| 92 | mistralai/ministral-14b-2512 | 0.942 | 81/86 | — | 0.200 | 0.200 | 256K | mistralai |
| 93 | mistralai/codestral-2508 | 0.942 | 81/86 | — | 0.450 | 0.900 | 250K | mistralai |
| 94 | google/gemma-3-4b-it_run2 | 0.942 | 81/86 | — | 99 | 99 | 0K | google |
| 95 | google/gemma-3-4b-it_run3 | 0.942 | 81/86 | — | 99 | 99 | 0K | google |
| 96 | ibm-granite/granite-4.1-8b_run1 | 0.942 | 81/86 | — | 99 | 99 | 0K | ibm-granite |
| 97 | ibm-granite/granite-4.1-8b_run2 | 0.942 | 81/86 | — | 99 | 99 | 0K | ibm-granite |
| 98 | ibm-granite/granite-4.1-8b_run3 | 0.942 | 81/86 | — | 99 | 99 | 0K | ibm-granite |
| 99 | mistralai/ministral-3b-2512 | 0.930 | 80/86 | — | 0.100 | 0.100 | 128K | mistralai |
| 100 | cohere/command-r-08-2024 | 0.930 | 80/86 | — | 0.262 | 0.600 | 125K | cohere |
| 101 | minimax/minimax-m2 | 0.930 | 80/86 | — | 0.441 | 1.000 | 200K | minimax |
| 102 | mistralai/mistral-nemo | 0.907 | 78/86 | — | 0.022 | 0.030 | 128K | mistralai |
| 103 | inclusionai/ring-2.6-1t | 0.895 | 77/86 | — | 0.212 | 0.625 | 256K | inclusionai |
| 104 | qwen/qwen-2.5-7b-instruct | 0.884 | 76/86 | — | 0.055 | 0.100 | 128K | qwen |
| 105 | openai/gpt-4o-mini-search-preview | 0.872 | 75/86 | — | 0.262 | 0.600 | 125K | openai |
| 106 | minimax/minimax-m2.7 | 0.872 | 75/86 | — | 0.420 | 0.960 | 200K | minimax |
| 107 | nvidia/nemotron-3-super-120b-a12b | 0.860 | 74/86 | — | 0.180 | 0.450 | 976K | nvidia |
| 108 | liquid/lfm-2-24b-a2b | 0.814 | 70/86 | — | 0.052 | 0.120 | 125K | liquid |
| 109 | meta-llama/llama-3.2-1b-instruct | 0.814 | 70/86 | — | 0.071 | 0.201 | 128K | meta-llama |
| 110 | nvidia/nemotron-3-nano-30b-a3b | 0.779 | 67/86 | — | 0.087 | 0.200 | 256K | nvidia |
| 111 | openai/gpt-oss-120b | 0.744 | 64/86 | — | 0.074 | 0.180 | 128K | openai |
| 112 | openai/gpt-oss-20b | 0.721 | 62/86 | — | 0.057 | 0.140 | 128K | openai |
| 113 | inception/mercury-2 | 0.721 | 62/86 | — | 0.375 | 0.750 | 125K | inception |
| 114 | upstage/solar-pro-3 | 0.709 | 61/86 | — | 0.262 | 0.600 | 125K | upstage |
| 115 | qwen/qwen-2.5-coder-32b-instruct | 0.558 | 48/86 | — | 0.745 | 1.000 | 125K | qwen |
| 116 | meta-llama/llama-3.2-3b-instruct | 0.512 | 44/86 | — | 0.122 | 0.335 | 128K | meta-llama |
| 117 | mistralai/mistral-small-3.1-24b-instruct | 0.151 | 13/86 | — | 0.402 | 0.555 | 125K | mistralai |
| 118 | rekaai/reka-flash-3 | 0.023 | 2/86 | — | 0.125 | 0.200 | 64K | rekaai |

**Cheapest comprehensive (86/86) models** — the real winners after hardening:
- **qwen/qwen3-30b-a3b-instruct-2507** — 0.084 blend $/M, 0.193 out, 128K ctx (qwen)
- **qwen/qwen3-235b-a22b-2507** — 0.092 blend $/M, 0.100 out, 256K ctx (qwen)
- **qwen/qwen3-235b-a22b-thinking-2507** — 0.100 blend $/M, 0.100 out, 256K ctx (qwen)
- **amazon/nova-lite-v1** — 0.105 blend $/M, 0.240 out, 292K ctx (amazon)
- **qwen/qwen3.5-9b** — 0.112 blend $/M, 0.150 out, 256K ctx (qwen)
- **deepseek/deepseek-v4-flash** — 0.113 blend $/M, 0.180 out, 1024K ctx (deepseek)
