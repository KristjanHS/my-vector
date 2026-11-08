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

_pending_

## Stage 2 — local smoke (top 5 + gemma4 reference)

_pending_

## Recommendation

_pending_
