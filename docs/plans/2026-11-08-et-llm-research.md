# Estonian small-LLM research (Nov 2026)

**Status**: in progress — research only, no code/config changes (user, 2026-11-08).

**Ask**: `gemma4:e4b-it-qat` and `qwen3.5:4b` neither understand nor speak proper Estonian (user, by ear).
Find which small LLMs are best at Estonian comprehension + generation as of November 2026, smoke-test the top 5
locally, recommend one pick for the et-EE chat model.

## Contract (questions round 2026-11-08)

| # | Rule |
|---|---|
| C1 | Output = this doc: ranked shortlist with evidence + local smoke replies + one recommended pick. No code, config or default-model change. |
| C2 | Size: up to ~9B params at Q4 (QAT / Q4_K_M / IQ4); slightly over 8 GB VRAM (beside STT+TTS) is OK only if Estonian quality is clearly better (user note). gemma4:e4b-it-qat = 6.1 GB on disk; EstLLM-8B Q4_K_M ran at stack peak 7753/8192 MiB. |
| C3 | Smoke the **top 5** (user note, not 2–3), plus `gemma4:e4b-it-qat` as the known-bad reference. |
| C4 | Smoke harness (from `chipper/`): `go run ./pkg/myvector/routeprobe -llm -lang et-EE -brain ollama:<tag> -set dev -judge none` (+ per-ask replies saved); Ollama 0.40.14 on Windows at `localhost:11434`, pull via `curl localhost:11434/api/pull`. |
| C5 | Out of scope: cloud APIs, fine-tuning, switching the shipped et model (a later plan does that from §Recommendation). |

Prior art: `archive/2026-11-05-estonian-language-mode.md` (EstLLM-8B pick, E11), `archive/2026-11-07-et-listen-model.md`
(no et listen winner; EuroLLM-9B, EstLLM-8B, gemma3/4, qwen3.5 tried), `docs/backlog.md` (EstLLM deleted 2026-11-08).

## Stage 1 — web research → ranked shortlist

Keys: **TN** = TartuNLP generative evals (Apertus-EstLLM-0326 card, 2026-04) — Grammar-et / IFEval-et / wmt24pp en→et BLEU;
**EE** = EuroEval Estonian generative (data to 2026-09-23, fetched 2026-11-08) — rank score (lower = better) / grammar_et; **Arena** = EstLLM paper Elo (2026-03);
**EKI-NB** = Estonian Native LLM Benchmark mean (TalTech, 2026-02). EKI moodupuu has no open model <27B.

| rank | model | params / quant | GGUF | Ollama tag (verified) | Estonian evidence | notes |
|---|---|---|---|---|---|---|
| 1 | Llama-3.1-EstLLM-8B-Instruct-1125 | 8B / i1-Q4_K_M | 4.92 GB | `hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M` | TN 0.831 / 0.614 / 0.264 (best ≤9B grammar+IFEval); EE grammar 35.0 (best ≤10B; unranked, no err_news); Arena 1384 = #1 open ≤12B | = paper's "CV" pick; previous ship (7753 MiB peak); IQ4_XS 4.45 GB to save VRAM; Llama-3 template |
| 2 | Apertus-EstLLM-8B-Instruct-0326 | 8B / i1-Q4_K_M | 5.06 GB | `hf.co/mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF:i1-Q4_K_M` | TN 0.713 / 0.561 / **0.268** (best en→et) | newest EstLLM (2026-03); Apertus arch (xIELU) — Ollama load unverified; Apache-2.0 |
| 3 | EuroLLM-9B-Instruct-2512 | 9.2B / Q4_K_M | 5.58 GB | `hf.co/mradermacher/EuroLLM-9B-Instruct-2512-GGUF:Q4_K_M` | EE 2.34 / 24.0; TN 0.644 / 0.557 / 0.257 | newer EuroLLM (2026-01); ChatML; card wants a system prompt; 4k ctx |
| 4 | EuroLLM-9B-Instruct (2412) | 9.2B / Q4_K_M | 5.58 GB | `hf.co/bartowski/EuroLLM-9B-Instruct-GGUF:Q4_K_M` | TN 0.764 / 0.540 / 0.260; EE grammar 22.4; EKI-NB 0.409 (#2 after EstLLM 0.454); Arena 1328 | previously tried; beats 2512 on grammar MC |
| 5 | gemma-3-12b-it | 12B / QAT q4_0 | 8.9 GB (vision incl.) | `gemma3:12b-it-qat` | TN 0.789 / **0.756** / –; EE **2.10** / 39.1; Arena 1317; EKI-NB 0.354 | **over-budget**: text-only `hf.co/bartowski/google_gemma-3-12b-it-GGUF:IQ4_XS` 6.55 GB |
| 6 | Bielik-Minitron-7B-v3.0-Instruct | 7.5B / Q4_K_M | 4.50 GB | `hf.co/speakleash/Bielik-Minitron-7B-v3.0-Instruct-GGUF:Q4_K_M` | EE **2.22** / 20.3 (best-ranked ≤10B) | Polish-focused wildcard; no Estonian-specific bench; base repo gated, GGUF not |
| 7 | gemma-4-12B-it | 12B / QAT q4_0 | 7.2 GB | `gemma4:12b-it-qat` | EE 2.21 / 29.2, trivia_et 0.5 | **over-budget**; thinking model — set `think:false` |
| 8 | gemma-3n-E4B-it | 7.8B (E4B) / Q4_K_M | 4.54 GB | `hf.co/unsloth/gemma-3n-E4B-it-GGUF:Q4_K_M` | EE 2.43 / 22.2 | official `gemma3n:e4b` is now 9.8 GB — use unsloth text GGUF |
| 9 | Qwen3.5-9B | 9.7B / Q4_K_M | 6.6 GB | `qwen3.5:9b` | EE 2.51 / 28.4 (but NER 21.7, MWQA 43.7) | thinking on by default — `think:false`; 4B sibling EE 3.03 |
| 10 | Ministral-3-8B-Instruct-2512 | 8.9B / Q4_K_M | 6.0 GB | `ministral-3:8b` | EE 2.54 / 23.2; TN 0.562 / 0.489 / – | weak Estonian grammar; strong English |
| 11 | tiny-aya-global | 3.35B / Q4_K_M | 2.14 GB | `hf.co/CohereLabs/tiny-aya-global-GGUF:Q4_K_M` | TN 0.563 / 0.669 / –; EE grammar_et 0.0 | tiny + fast; grammar weak |
| ref | gemma4:e4b-it-qat · gemma3:4b-it-qat · qwen3.5:4b | 4–8B | 6.1 / 4.0 / 3.3–4.0 GB | (as named) | EE 2.64 / 14.5 · 2.79 / 18.2 · 3.03 / 19.1 | confirms user's by-ear verdict |

Smoke top 5: `hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M`, `hf.co/mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF:i1-Q4_K_M`, `hf.co/mradermacher/EuroLLM-9B-Instruct-2512-GGUF:Q4_K_M`, `hf.co/bartowski/EuroLLM-9B-Instruct-GGUF:Q4_K_M`, `gemma3:12b-it-qat` (over-budget)

Sources:
- https://huggingface.co/tartuNLP/Apertus-EstLLM-8B-Instruct-0326 (TN tables)
- https://huggingface.co/tartuNLP/Llama-3.1-EstLLM-8B-Instruct-1125
- https://arxiv.org/abs/2603.02041 (EstLLM paper, arena)
- https://arxiv.org/abs/2510.21193 (Estonian Native LLM Benchmark)
- https://euroeval.com/leaderboards/Monolingual/estonian/
- https://moodupuu.eki.ee/ · https://github.com/keeleinstituut/leaderboard-data-ui
- https://huggingface.co/utter-project/EuroLLM-9B-Instruct-2512 · https://huggingface.co/utter-project/EuroLLM1.5-9B-SFT
- https://huggingface.co/speakleash/Bielik-Minitron-7B-v3.0-Instruct-GGUF · https://huggingface.co/CohereLabs/tiny-aya-global-GGUF
- https://ollama.com/library/gemma3/tags · https://ollama.com/library/gemma4/tags · https://ollama.com/library/gemma3n/tags · https://ollama.com/library/qwen3.5/tags · https://ollama.com/library/ministral-3/tags

## Stage 2 — local smoke (top 5 + gemma4 reference)

_pending_

## Recommendation

_pending_
