# Reply cap by kind of ask

Status: ✅ SHIPPED to main (Stages 1.1–1.2 + review); 2.1 fakebot/journalctl + 2.2 audible owed → `docs/backlog.md` §OWED[USER]. Parent: `2026-11-07-system-prompt-redesign.md (docs/plans/)` ruling P-budget.

## Rulings

| id | ruling | edit site |
|---|---|---|
| A1 | Cap follows the kind of ask: long asks (story, detail, "N sentences") get the long cap, others keep today's; no flat high cap. An LLM classify call is allowed if it is fast (D1-d). | `forkReplyCap` |
| A2 | The decision comes from wire-pod's own routing (knowledge graph or intent logic), not a word list bolted onto the request. | per D1 |
| A3 | short = `prompt.ReplyCap` today (Ollama 300 / Claude 1024); long = Ollama 1000 / Claude 2048 (pick at plan time; a live story ask validates). | `prompt.ReplyCap` |
| D1 | long = fork long-answer phrase hit (en + et, whole-word `ForkContainsPhrase`) OR `isKG`; no LLM classify call. | phrase data + `forkAIReq` / `forkReplyCap` (Stage 1.2) |

## Stages

Refactor-first: skipped — the one prep (`checkCap` picks max_tokens vs max_completion_tokens by `want == prompt.ReplyCap(prompt.Ollama)`, so Ollama's long 1000 would read as Claude) is a test-helper fix riding Stage 1.2.

| id | ruling | edit site |
|---|---|---|
| 1.1 | `forkLongAskPhrases` (one flat en+et list): `story`, `stories`, `fairy tale`, `in detail`, `detailed`, `step by step`, `sentences`, `paragraphs`, `lugu`, `lugusid`, `jutusta*`, `muinasjut*`, `pikemalt`, `üksikasjalik*`, `põhjalikult`, `samm-sammult`, `lauset`, `lõiku`; `ForkLongAsk(text)` = any `ForkContainsPhrase` hit. Proof `TestForkLongAsk`: en hits (lang.English) + et hits (lang.Estonian); non-hits `storage`, `history`, `storyteller`, `ajalugu`, `kahe lausega`, `what is the capital of france`. | new `pkg/vars/fork_longask.go` + `fork_longask_test.go` |
| 1.2 | `prompt.LongReplyCap(brain)` = Ollama 1000 / Claude 2048. `forkReplyCap(r, ollama, long)`; `forkAIReq` passes `isKG \|\| vars.ForkLongAsk(<last message: CreateAIReq's user turn>)` and logs `myvector: reply cap <n> (long ask)`; `forkCameraReq` passes false. No `kgsim.go` / `preqs` edit: both `isKG=false` fallbacks reach `CreateAIReq` with the text as the last message; `upstream-drift.sh` unchanged. | `prompt/defaults.go`, `ttr/fork_prompt.go` |
| 1.2t | `TestReplyCap` asserts `LongReplyCap` per brain. `TestForkPromptReplyCapLongAsk` × both brains: story ask (en), `räägi mulle muinasjutt` (et), `hi` with isKG → long; `what is the capital of france` isKG=false → short. Existing cap tests switch `CreateAIReq("hi", …)` to isKG=false (isKG now means long); `checkCap` takes an `ollama bool`. | `prompt/build_test.go`, `ttr/fork_prompt_test.go` |
| 2.1 | `make check` green. Live, from `chipper/`: `go run ./pkg/myvector/routeprobe -llm -brain ollama:qwen3.5:4b "tell me a story about a robot in ten sentences"` + same with `-brain` Claude + `-lang et-EE "räägi mulle muinasjutt robotist"` — answer not cap-cut, longer than the short-ask answer. After the user restarts wire-pod: `robot/work/live-check/fakebot -lang <saved> -say "tell me a story about a robot"` + `journalctl -u wire-pod \| grep 'reply cap'` shows the long cap. A story ending `stop` well under the cap → backlog (prompt brevity), not this plan. | — |
| 2.2 | Audible story on Vector = USER (volume stays 1; ask before any robot audio test). Code review: `code-reviewer` subagent on the 1.1–1.2 diff. | — |
