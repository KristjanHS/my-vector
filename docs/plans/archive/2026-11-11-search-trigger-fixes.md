# Search trigger fixes — "search for X" reaches the web; no robot timeout, no parroted replies

**Goal**: an ask to search, in Estonian or English and in either language mode, runs the web search, and the wake-word
turn never hits vic-engine's 10 s `cloud_response_failed` timeout. Evidence (incident 2026-11-11 08:27): this doc's commit.
**Status**: ✅ SHIPPED (committed on main) 2026-11-11 (F1–F5, R5 = B); `make ship` + live check → `docs/backlog.md` §OWED[USER].

## Fixes

| id | cause → fix | edit site | falsifier |
|---|---|---|---|
| F1 | `Route` has topic cues only → add a `command` table that always routes to `Live`. et: `otsi`, `otsige`, `guugelda`, `googelda`, `vaata * järele`, `uuri * välja`, `otsi * internetist`, `leia * netist`. en: `search`, `google`, `search for`, `look up` (not `look up at`), `find out` | `search.Route`, `tables` | `go test ./pkg/myvector/search/ -run TestRoute`: "otsi viimase pakkumise pealt, number kolm." → Live; "otsin prille", "look up at me" → None |
| F2 | an et-EE turn checks only et cues, but STT writes English speech as English → `Route(s, et)` falls back to the en tables when et gives None | `search.Route` | same test: "search for present news about semiconductors related financial." in et → Live; an et corpus in `search/testdata/` (route_test et cases, probe sets, searchbench; no journal transcripts — origin is public) gives no en-only hits |
| F3 | be8e4d1 already sends the wake-word intent at wait start (before the LLM call) → log `forkWaitIntent`'s error and the turn's TTFT; pin intent-before-first-token | `forkWaitStart` + the stream's first chunk via a `fork_*.go` hook (+ `scripts/upstream-drift.sh` if `kgsim.go` changes) | `go test ./pkg/wirepod/ttr/ -run 'TestForkWaitHook\|TestForkFirstToken'` (source order: `defer forkWaitStart` before `CreateAIReq`) |
| F4 | the Ollama brain offers to search ("Ma võin otsida…") and copies its last reply → `prompt.NoSearch` fills an empty `{search}` for Ollama brains (Claude: none) | `prompt.NoSearch` + `forkSystem` | `go test ./pkg/wirepod/ttr/ -run TestForkAIReqSearch`; replay: 0/3 offers, 0/3 lists |
| F5 | a command cue with a short or deictic rest ("…, number kolm") searches the literal sentence → prefix the previous search ask's keywords (R6–R8) from `GetChat` | `search.Fill` / `forkSearching` | `go test ./pkg/myvector/search/ -run 'TestFollowQuery\|TestFillFollowUp'` + `./pkg/wirepod/ttr/ -run TestForkPrevAsks`; replay of both incident turns (R5) |
| F6 | upstream `CreateAIReq` logs `Using <Knowledge.Model>` before `forkAIReq` swaps the brain → leave it: `myvector llm:` logs the real brain | none | none |

## Rulings

| id | ruling | edit site |
|---|---|---|
| R1 | command cues skip the self guard and have no object block ("otsi kuubik" → Live) | `search.Route` |
| R2 | the et→en fallback covers every class | `search.Route` |
| R3 | no watchdog; F3 = error + TTFT logging and the pin test | `forkWaitStart` |
| R4 | F5 this run, after F4 | `search.Fill` / `forkSearch` |
| R5 | B: `NoSearch` without its "don't repeat" clause (replay: copy 1/3 vs A 2/3, offers 0/3) | `prompt.NoSearch` |
| R6 | carry when <2 content words survive (counted before `Keywords`' whole-sentence fallback) or the ask has a pointer word (et `seda`, `sellest`, `selle`, `viimase`, `eelmise`, `number N`; en `it`, `that`, `this`, `the last`, `more`, `number N`) | `search` |
| R7 | topic = `Keywords` of the last user ask that routes to search, at most 2 user turns back; topic words first, cap `maxKeywords`; cache key = the combined query | `search.Fill`, `forkSearching` |
| R8 | no "number N" → reply-item resolution; history stays untimed | none |

## Order

F1+F2 (one router slice) → F3 → F4 → F5; `make check` per slice; ship on the user's ask.
