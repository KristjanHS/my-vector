# Built-in intent allowlist — most sentences go to the LLM

**Status**: OPEN — rulings made 2026-11-05 (llm-trials run); build in a fresh session.

Why: wire-pod substring-matches 55 built-in intents before the LLM ("tonight" → goodnight, "start" → explore);
with a worn battery Vector stays docked, so movement/cube/game intents only steal questions.

| id | ruling | edit site |
|---|---|---|
| R1 | Allowlist, unconditional (LLM on or off): keep only `intent_clock_time`, `intent_clock_settimer_extend`, `intent_clock_checktimer`, `intent_weather_extend`, `intent_photo_take_extend`, `intent_names_username_extend`, `intent_names_ask`, `intent_knowledge_promptquestion` | new `chipper/pkg/vars/fork_intents.go`: `forkAllowedIntents` + `forkFilterIntents([]JsonIntent) []JsonIntent` |
| R2 | Kept intents keep upstream partial matching (no exact-only) | — |
| R3 | `intent_knowledge_promptquestion` stays: "I have a question" is the only route with follow-up listening | R1 list |
| R4 | Apply in `LoadIntents` (all 3 callers reload through it) | `chipper/pkg/vars/vars.go` `LoadIntents`: one call before return; add `vars.go` to `allowed_edits` in `scripts/upstream-drift.sh` |
| R5 | Custom intents + plugins still run first (untouched) | — |

## Stage 1 — build + verify

- [ ] R1/R4 + `fork_intents_test.go`: filtered names == allowlist; an unknown name in the list fails the test.
- [ ] `make check`; `go -C chipper test ./pkg/vars/`; red demo on the filter.
- [ ] `make start`; falsifier: "Hey Vector, what should I cook tonight?" → log shows `Making LLM request`, not `intent_greeting_goodnight`.
- [ ] Code review; update `llm-trials.md` (docs/) step 2 note.
