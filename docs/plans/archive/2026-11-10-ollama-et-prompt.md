# Ollama et/en prompt split + et sing line

Status: ✅ SHIPPED to main (b262b6d..26af1b4); `make ship` + live check owed (`docs/backlog.md` §OWED[USER]).

Evidence: `2026-11-07-follow-up-listening.md` (docs/plans/) rows P11–P14; implement in a fresh session (qimpag).

## Rulings (user, 11-10)

| # | Ruling |
|---|---|
| R1 | et brain stays apertus-estllm:8b at 0.4 / 0.9 (P11, P12). No EstLLM swap. |
| R2 | Family characters in stories: keep as is (P13). |
| R3 | Local-model prompt changes are **Ollama-only**, never the Claude template or the shared `persona`. |
| R4 | **Split the Ollama prompt into separate et and en templates**; the sing sentence goes only into Ollama-et. |
| R5 | Adopt the Estonian sing sentence **now, as probed** (no confirm probe). |
| R6 | Invented weather in small talk: accept. Real weather only when asked (existing web-search path). |
| R7 | Opinion dodge ("See on väga isiklik küsimus!"): accepted, closed — no prompt line or sampling change. |
| R8 | Add an Estonian "about me" (`AboutEt`) with an Estonian heading, used **only by the ollama-et brain**; Claude and Ollama-en keep `About`. |
| R9 | Empty `AboutEt` → ollama-et falls back to `About` with today's English heading. |

## Steps

| # | Step | Edit site |
|---|---|---|
| 1 | Split brains: keep `"ollama"` = en, add an et Ollama brain (e.g. `"ollama-et"`), default = today's `order`; a store without it seeds it from the saved `ollama` template + versions. `forkBrain` picks by provider and reply language; every `== prompt.Ollama` check accepts both. Page: tabs Claude / Ollama en / Ollama et. | `prompt` pkg (`Budget`, `SearchBudget`, `MemoryBudget`, `ReplyCap`, `LongReplyCap`, `Validate`, `Save`, `prompt.html`), `ttr.forkBrain`, `memory.Block`, `search` block, chainprobe, routeprobe, visionbench |
| 2 | Add to the Ollama-et default and the live saved et template, right after "Now and then ask a question back.": `Sa räägid valjult oma robotikehaga: kui palutakse, laulad laulu, öeldes selle sõnad.` Proof: a prompt test that the et Ollama render has it and the en Ollama and Claude renders don't. | `prompt/defaults.go`, saved store |
| 2b | `Store.AboutEt` (JSON `about_et`, `AboutCap`), heading `Inimene, kellega räägid:`; `User(brain)` picks per R8/R9; page: "About me (eesti)" box shown on the Ollama et tab. Proof: ollama-et render has the et about, Claude/en don't; empty → English fallback. | `prompt` store/http/`prompt.html`, `ttr.forkAIReq`, routeprobe |
| 3 | chainprobe review fixes: record arm L's `-line` text per JSONL row; post-save check `lineQ+add`; die on `-line` without arm L; `-line` help quotes the lineQ sentence. | `chainprobe` |
| 4 | `make check`, then one code review (store migration = high-risk). `make ship` only on the user's ask. | — |

## Moves

- Write et-brain lines in Estonian — English lines didn't move apertus (P14).
- Run probes from the main checkout's `chipper/` (the worktree lacks `secrets/server/` config); binaries in `robot/work/followup-debug/bin/`.
