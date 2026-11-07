# Estonian active-listening model

Status: CLOSED WITHOUT SHIPPING (E6: no et model reached E4; 1.4–2.2 not run). Parent: `2026-11-07-active-listening.md (docs/plans/)` A4/P1.

Ask: pick the best Ollama model for Estonian active listening; `gemma4:e4b-it-qat` stays the en-US listen model only.

## Rulings

| id | ruling | edit site |
|---|---|---|
| E1 | en-US listen model = `gemma4:e4b-it-qat` (P1); et-EE = this plan's winner, `gemma4:e4b-it-qat` until then. | `listen.Models` |
| E2 | Candidates (8 GB VRAM, one loaded at a time): `gemma4:e4b-it-qat` (baseline et 15/20), `hf.co/bartowski/EuroLLM-9B-Instruct-GGUF:Q4_K_M` (pull), `llm.DefaultETModel` EstLLM-8B, `gemma3:4b-it-qat`, `qwen3.5:4b`. | `listenprobe -models` |
| E3 | Arms: prompt 2 (English system prompt) + `prompt 2 et` (same rules in Estonian, reported only). Two-call arm dropped (12/20 in Stage 2.1). | `listenprobe` `arms` |
| E7 | English `listen.Prompt` always ships; the model verdict is the `prompt 2` row. | `listen.Prompt` |
| E8 | [USER] checks the Estonian (`confirm` lines + `prompt 2 et`) at 1.4, with the replies. | 1.4 |
| E4 | Selection: the 20 et snippets (dev + holdout). Bar ≥ 17/20, ≤ 1 false SPEAK, median ≤ 2 s; ties → fewer false SPEAK, lower median, smaller model. | `listenprobe -lang et-EE` |
| E5 | Confirm: winner only, on a new `confirm` set (10 SPEAK + 10 SKIP, et), committed before any Stage 1.3 run; same bar. | `listenprobe` `confirm` |
| E6 | No winner or confirm fails → et stays `gemma4:e4b-it-qat`, backlog §OWED[USER] line, stop. | `docs/backlog.md` |
| W1 | No winner (E6): et stays `gemma4:e4b-it-qat` via `listen.Model`; best et 15/20 fs 3 (`qwen3.5:4b`, `prompt 2 et`). | `listen.Model` |

## Stages

| slice | work | proof | verify |
|---|---|---|---|
| 1.1 | `listenprobe`: `-lang` filter, `-models a,b` (verdict list), `-show` (print every reply), arm `prompt 2 et`, set `confirm` (E5). | `-set confirm -lang et-EE -models gemma4:e4b-it-qat -arm "prompt 2" -show` runs 20 and prints 20 replies | `cd chipper && go vet ./pkg/myvector/listenprobe/` |
| 1.2 | Pull EuroLLM: `curl localhost:11434/api/pull -d '{"model":"hf.co/bartowski/EuroLLM-9B-Instruct-GGUF:Q4_K_M"}'`. | `/api/tags` lists it; one warm-up answer | `listenprobe -models <it> -lang et-EE -arm "prompt 2"` |
| 1.3 | Run E2 × E3 on the dev + holdout et snippets → winner (E4) → row W1. | probe table | `listenprobe -set dev,holdout -lang et-EE -models a,b,…` |
| 1.4 | Winner on `confirm` (E5) with `-show` → row W1. [USER] reads the printed replies: natural Estonian? | probe table + replies | user ruling |
| 2.1 | `listen.Model` → `listen.Models` per language (E1); `forkResolve` passes `listen.Models[l]`. | `TestResolveWhileOn`: on → en `gemma4:e4b-it-qat`, et winner. Red: et → en model fails. | `make check` |
| 2.2 | `make check`; `code-reviewer` over this plan's commits; live et check rides on the parent's 5.2. | — | — |
