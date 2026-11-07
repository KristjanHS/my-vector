# Active listening

Status: Stage 1 + 2.1 committed; Stage 3 waits on the verdict below, so 4.1 runs first. Code review of `c0b0efb..b6a71d9` deferred to 5.1. Owed live: G5 curl 423 (1.3), en-US phrase STT round trip (1.1, needs saved lang en-US). Stage 3 reuses `robot/openmic` and waits on `2026-11-07-shut-up-sleep.md (docs/plans/)` §Stage 1 verdict.

Ask: Vector listens to all talk, speaks only when he has something relevant. Ollama only, never Claude. On: "hey vector, activate active listening" / "hey vector, aktiveeri aktiivne kuulamine". Off: "deactivate active listening" / "deaktiveeri aktiivne kuulamine". Short forms "active listening on/off", "aktiivne kuulamine sees/välja" too.

## Rulings

| id | ruling | edit site |
|---|---|---|
| A1 | Whole-phrase match on `quiet.Match`'s tokenizer + `quiet.Fillers`. On: `activate active listening`, `active listening on`, `aktiveeri aktiivne kuulamine`, `aktiivne kuulamine sees`; off: `deactivate active listening`, `active listening off`, `deaktiveeri aktiivne kuulamine`, `aktiivne kuulamine välja`. STT variants added only from the Stage 1.1 round trip. | new `pkg/myvector/listen` `On` `Off` `Match` |
| A2 | `forkListen` in `ProcessTextAll` right after `forkQuiet`: match → state → spoken confirmation via `IntentPass` → return true. Off phrase also matched on overheard text (Stage 4). | `ttr/fork_listen.go`; `matchIntentSend.go` drift comment |
| A3 | State in RAM per ESN `{on, since, lastHeard, lastSpoke}`; off on: wire-pod restart, a quiet-lock match, `listen.IdleOff` = 30 min with no utterance, entering the night window (`listen.NightFrom`/`NightTo` mirror patch 0011's `IsNightTime`: `NightFrom` = 21, `NightTo` = 9). Auto-off logs one line, no sound. | `listen.State` |
| A4 | Brain while on: every LLM call (overheard and wake-word asks, both languages) uses `listen.Models` (en `gemma4:e4b-it-qat`, P1; et → plan: `2026-11-07-et-listen-model.md (docs/plans/)`) on the saved Ollama URL. Only while on: the saved profile is never written, Claude returns at off. | `ttr/fork_listen.go` helper wrapping both `llm.Resolve` calls in `ttr/fork_prompt.go` |
| A5 | One `listen.Model` call per utterance with `listenprobe` prompt 2 (system) + its `user()` shape (rolling ~2 min transcript, newest line) → `SKIP` prefix (any case) or a 1–2 sentence reply. | `listen.Prompt` `listen.Parse` |
| A6 | Echo gate (ruled 2026-11-07): drop an utterance whose clip window overlaps a listen reply or a wake-word turn (ask → `listen.TurnTail` = 10 s, plus every `forkSayText` answer), each +`listen.EchoPad` = 1 s. | `listen.Heard`; `ttr/fork_listen.go` `forkListen`; `ttr/fork_say.go` |
| A7 | G5 pulled in from `2026-11-07-shut-up-sleep.md (docs/plans/)` (Stage 1.3); listen replies pass the same lock check before speaking. | `sdkapp/fork_quiet.go`; `ttr/fork_listen.go` |
| A8 | Overheard text stays in RAM; journal gets `myvector listen: <n> words → SKIP\|SPEAK <ms>`, never the words. `MYVECTOR_LISTEN_LOG=<dir>` (unset = off) appends `[SKIP\|SPEAK] <text>` to `<dir>/<date>.txt`; use `robot/work/listen/`. | `listen.Heard` |
| C1 | Capture = `robot/openmic` (ruled 2026-11-07): `RecordMicDataClip` via `:8889/consolefunccall`, scp from `debugCapture`, 2 s windows / 1 s hop → myvector-stt. Refactor-first: move those `probe.sh` pieces into `robot/openmic/lib.sh`; `probe.sh` and the new loop both source it. | `robot/openmic/{lib.sh,probe.sh,listen.sh}` |
| C2 | `listen.sh` (silent=1, records only) loops clips back to back, keeps the previous clip's last 2 s ahead of the next so a cut sentence is whole, joins windows into utterances, and POSTs each to `/myvector/listen/heard`. Server starts it on activate, kills it on deactivate (miccap repo-path precedent). | `robot/openmic/listen.sh`; `listen.Runner` |
| A9 | Chattiness normal: `listen.Cooldown` = 45 s between listen replies (wake-word answers don't start it, ruled 2026-11-07); SPEAK bar = a question in the air, a factual slip, his name, a clear fun opening (the Stage 2.1 SPEAK labels). | `listen.Cooldown`; `listen.Prompt` |
| P1 | Ruled 2026-11-07: `listen.Model` = `gemma4:e4b-it-qat` + prompt 2 (held-out 16/20 accepted: 0 false SPEAK, misses silent); saved profiles fail (et SPEAK 0/4). | `listenprobe` |

## Stages

| slice | work | proof | verify |
|---|---|---|---|
| 1.1 | `listen.Match` (A1), `listen.State` (A3). Phrase round trip: `robot/voice-samples/gen.sh` (TTS only, no robot sound) per phrase × af_bella + mari → myvector-stt → `Match`; add each variant STT produces. | `TestMatchTable`: phrases with `hey vector`, `palun`, punctuation, short forms hit; `activate the lights`, `the active listening off switch`, `listening to music`, `ära aktiveeri aktiivne kuulamine` miss. Red: contains-match → misses fail. | `cd chipper && go test ./pkg/myvector/listen/` |
| 1.2 | `forkListen` (A2) + A4 override. Confirmation en "Active listening on/off", et "Aktiivne kuulamine sees/väljas". | `TestForkListenShortCircuits`; `TestResolveWhileOn`: Claude profile + on → Ollama default target, off → Claude. Red: drop the override → fails. | `make check`; `go run ./pkg/myvector/routeprobe "hey vector, activate active listening"` → confirmation |
| 1.3 | G5 = `2026-11-07-shut-up-sleep.md (docs/plans/)` Stage 3.3 verbatim (its proof + verify). | `TestForkQuietBlocksSound`, `TestForkQuietLeavesSilent` | `make check` |
| 2.1 | `listenprobe` (followupprobe pattern): 2 single-call prompts + the A5 two-call arm in one run; verdict on the saved Ollama profiles, `gemma4:e4b-it-qat` + `llm.DefaultETModel` reported as alternates; × 20 labeled STT-style snippets (10 en + 10 et; per language 4 SPEAK: question in the air, factual slip, Vector named, fun opening; 6 SKIP: small talk, phone-call half, TV, private talk, half sentence, already answered) × each language's Ollama default. Rule: ≥ 17/20, ≤ 1 false SPEAK per language, median ≤ 2 s wins; none → §OWED[USER] line, stop. Result → row P1. | probe table | `go run ./pkg/myvector/listenprobe` |
| 3.1 | C1 refactor-first: `lib.sh` extraction, `probe.sh` behaviour unchanged. | `probe.sh -r <saved run> et-EE` re-scores a saved `robot/work/openmic/` run to the same table before and after | that command, diffed |
| 3.2 | C2 `listen.sh` + `/myvector/listen/heard` + `listen.Runner`. Live check (USER, silent, robot records only): does a wake word still fire mid-clip? | fixture: two saved clips with a sentence across the cut → one utterance POSTed; `TestRunnerStopsOnOff` | `make check` |
| 4.1 | `listen.Heard`: off/quiet phrases first → A5 → SPEAK → `forkSay`; A6 echo gate; A9 cooldown; A3 idle + night auto-off. | `TestHeard` with httptest Ollama: SKIP → no say; SPEAK → one say; SPEAK inside `Cooldown` → none; `IdleOff` and night entry → state off; off phrase → state off, no LLM call; text overlapping own speech → no LLM call. | `make check` |
| 5.1 | `make check`; `code-reviewer` subagent over the diff. | — | — |
| 5.2 | USER (ask first, volume 1): activate en + et, talk near him 5 min with 2 relevant openings, deactivate. | `journalctl -u wire-pod \| grep 'myvector listen'` shows SKIP/SPEAK and no Claude pick | `docs/backlog.md` §OWED[USER] line at archive |
