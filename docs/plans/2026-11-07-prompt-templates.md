# Prompt templates (slice 2)

Status: Stages 1–3 committed; Stage 4 open: Claude story asks reply empty (`-lang en-US -brain claude:claude-haiku-5-5 -llm "Tell me a story about a dragon in exactly six sentences."` → bare `llm:`), fakebot stream, review. Parent: `2026-11-07-system-prompt-redesign.md (docs/plans/)` build order 2; its rulings P, U, SP, E, P-guard, P-budget, N bind.

## Rulings

| id | ruling | edit site |
|---|---|---|
| RF | Refactor-first: none. `forkAIReq` replaces `Messages[0].Content` wholesale with the rendered template and sets the reply cap; `defaultPrompt` and `CreatePrompt` text stay untouched but unused — zero new upstream lines. | `forkAIReq` |
| T1 | Templates per brain only: `prompt.Claude`, `prompt.Ollama`; brain = `llm.Resolve` target `t.Ollama()`, else `llm.IsOllama(kg)`. Resolve runs before the render. | `forkAIReq` |
| T2 | Tags `{now}` `{lang}` `{user}` `{facts}` `{diary}` `{search}` `{speech}` `{commands}`; one-pass `strings.NewReplacer`, block text never rescanned (the `{{cmd\|\|param}}` grammar survives); 3+ newlines squeeze to 2. | `prompt.Render` |
| T3 | Default order: persona, `{user}`, `{facts}`, `{diary}`, `{search}`, `{speech}`, `{commands}`, `{now}`, `{lang}` (N: now before lang). Same order both brains. | `prompt.Default` |
| PE | Persona (English): warm, playful companion robot called Vector; 2–4 short sentences unless asked for more; sometimes a question back; vary openings; no stock catchphrases. | `prompt.Default` |
| LA | `{lang}` = "Answer in Estonian. Keep names, brands and titles as written." for et-EE only; en-US renders empty. `forkETLine` is deleted. | `prompt.LangLine` |
| SP | `{speech}`: input is speech-to-text and may be misheard; times and dates as digits, every other number as words; normal punctuation; no markdown, emoji, lists. Replaces the upstream STT note. | `prompt.SpeechBlock` |
| CM | `{commands}` renders only when `vars.APIConfig.Knowledge.CommandsEnable`: one grammar line, one example, `playAnimationWI` + `playAnimation` (stops speech) with emotions from `ValidLLMCommands` `playAnimationWI` `ParamChoices`, `getImage` param `front` only, `newVoiceRequest` only when `SaveChat` and isKG (`forkAIReq(r, esn, isKG)`). Calm voice. ≤170 tokens. | `prompt.Commands`; caller `forkCommands` |
| OP | `openai_prompt` is ignored (live empty); the `/myvector/prompt` page shows one warning line when it is non-empty. | `prompt.PageHandler` |
| DBG | `DEBUG_PRINT_PROMPT=true` logs the rendered prompt (upstream logs the discarded one). | `forkAIReq` |
| UA | `{user}` = "About the person you are talking with:" + newline + the about-me text; empty about renders empty. | `prompt.UserBlock`; `forkAIReq` |
| VS | Versions = last 5 saves per brain, newest first (= live); Restore(i) re-saves version i; a save equal to the newest adds none. Reset drops the saved template (built-in default goes live), history kept. | `prompt.Save` `Restore` `Reset` |
| LF | Missing file = defaults, silent; unparsable file, or a brain's template / about failing checks → that part falls back to default + one log line. | `prompt.Load` `Store.Live` `Store.User` |

## Constants (source is the home; defaults below)

| symbol | value | validated by |
|---|---|---|
| `prompt.Estimate` | ceil(runes / 3) tokens | `TestEstimate` |
| `ReplyCap` claude / ollama | 400 / 300 → Claude `max_completion_tokens`, Ollama `max_tokens` (its /v1 ignores the other), also on the camera follow-up | default; `TestForkPromptReplyCap` |
| `Budget` claude / ollama | 2000 / 600 tokens, rendered with `prompt.Sample` and empty facts/diary/search | default; `TestValidateBudget` |
| `AboutCap` | 450 runes (≈150 tokens), shared by both brains | `TestAboutCap` |
| `RequiredTags` (both brains) | `{speech}` `{commands}` `{now}` `{lang}` (appended in this order); `{user}` `{facts}` `{diary}` `{search}` optional | `TestRenderAppendsMissing` |
| `Versions` | last 5 saved per brain | `TestVersionsKeepFive` |
| `prompt.Sample` | 2026-11-07 09:43 Europe/Tallinn, commands on, saved about-me | `TestRenderGolden` |

## Stages

| stage | focus files | edit sites | falsifiers | gate |
|---|---|---|---|---|
| Stage 1 — builtin templates on the wire | `pkg/myvector/prompt/{build,defaults}.go` + `_test.go` + `testdata/*.golden`; `ttr/fork_prompt.go` + test | `prompt.Render` `Default` `LangLine` `SpeechBlock` `Commands` `Estimate` `ReplyCap`; `forkAIReq` `forkCameraReq` `forkCommands`; delete `forkETLine`; `{user}` empty | `TestRenderGolden` (et/en × claude/ollama: et has LA line, en has no "Answer in"), `TestRenderDropsEmptyBlocks`, `TestCommandsBlockBudget`, `TestForkPromptReplyCap`, `TestForkPromptEstonian` (rewritten to LA) | `make check` |
| Stage 2 — store + P-guard | `prompt/store.go` + test; `.gitignore`; `routeprobe` `isolate` | `prompt.Path` (beside `vars.ApiConfigPath`, overridable), `Load` `Save` `SaveAbout` `Reset` `Restore` `Validate` `RequiredTags` `KnownTags`; write via `conf.Update`; `forkAIReq` reads the store; `isolate` copies `myvectorPrompt.json` (must=false) | `TestValidateUnknownTag`, `TestRenderAppendsMissing` (appended + flagged), `TestValidateBudget`, `TestAboutCap`, `TestLoadFallback` (bad file → default + log line), `TestVersionsKeepFive`, `TestReset` | `make check`; `git check-ignore -v chipper/myvectorPrompt.json` |
| Stage 3 — `/myvector/prompt` page | `prompt/{http.go,prompt.html}` + `http_test.go`; `sdkapp/fork_voice.go` `init`; `voice/settings.html` link (no `webroot/index.html` edit) | `PageHandler`, `APIHandler` (GET; POST save/reset/about/restore via `decode`-style valid), `PreviewHandler` → {text, tokens, budget, appended}; page: brain picker, textarea, tag chips, about-me box + counter, preview, budget meter, save/reset, versions | `TestPromptAPIRejectsUnknownTag` (400), `TestPromptAPISaveRoundTrip`, `TestPreviewReportsAppended`, `TestPromptPageServed` | `make check` |
| Stage 4 — e2e + review | — | fix-ups only | `scripts/live-check.sh route`; from `chipper/`: `go run ./pkg/myvector/routeprobe -lang et-EE -llm "mis nädalapäev täna on?"` → today's Estonian weekday; `-llm "kui palju on kaks tuhat viissada jagatud viiega?"` → number words, no digits, reply ends in `.`/`!`/`?`; both again with `-brain ollama:<ET model>`; a long-answer `-llm` on Ollama stops by 300 tokens (`max_completion_tokens` honoured); `-lang en-US` → no Estonian; fakebot stream; `code-reviewer` dispatch | `make check` |

## Owed after Stage 4

| id | ruling | edit site |
|---|---|---|
| OW | Add one `docs/backlog.md` §OWED[USER] line: voice confirm of the new persona/speech on the robot (weekday question + a number-heavy question, et-EE), and the `/myvector/prompt` page by eye. | `docs/backlog.md` §OWED[USER] |
