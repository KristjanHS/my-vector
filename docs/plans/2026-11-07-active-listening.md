# Active listening

Status: ruled 2026-11-07, no code; next Stage 1.1. Stage 3 reuses `robot/openmic` and waits on `2026-11-07-shut-up-sleep.md (docs/plans/)` §Stage 1 verdict.

Ask: Vector listens to all talk, speaks only when he has something relevant. Ollama only, never Claude. On: "hey vector, activate active listening" / "hey vector, aktiveeri aktiivne kuulamine". Off: "deactivate active listening" / "deaktiveeri aktiivne kuulamine".

## Rulings

(P) = proposed, not yet ruled by the user.

| id | ruling | edit site |
|---|---|---|
| A1 (P) | Whole-phrase match on `quiet.Match`'s tokenizer + `quiet.Fillers`. On: `activate active listening`, `aktiveeri aktiivne kuulamine`; off: `deactivate active listening`, `deaktiveeri aktiivne kuulamine`. STT variants added only from the Stage 1.1 round trip. | new `pkg/myvector/listen` `On` `Off` `Match` |
| A2 (P) | `forkListen` in `ProcessTextAll` right after `forkQuiet`: match → state → spoken confirmation via `IntentPass` → return true. Off phrase also matched on overheard text (Stage 4). | `ttr/fork_listen.go`; `matchIntentSend.go` drift comment |
| A3 | State in RAM per ESN `{on, since, lastHeard, lastSpoke}`; off on: wire-pod restart, a quiet-lock match, `listen.IdleOff` = 30 min with no utterance, entering the night window (`listen.NightFrom`/`NightTo` mirror patch 0011's `IsNightTime`: stock evening hour from the clone, morning hour 9). Auto-off logs one line, no sound. | `listen.State` |
| A4 | Brain while on: every LLM call (overheard and wake-word asks) uses the language's `llm.Effective` Ollama profile; a Claude profile is replaced by `llm.DefaultENModel` / `Config.etModel()`. Only while on: the saved profile is never written, Claude returns at off. | `ttr/fork_llm.go` at the `llm.Resolve` call |
| A5 (P) | One Ollama call per utterance: rolling transcript (~2 min) + new utterance → exactly `SKIP` or a 1–2 sentence reply. Under the Stage 2.1 bar → two calls (follow-up YES/NO decider, then answer). | `listen.Prompt` `listen.Parse` |
| A6 (P) | Echo gate: drop overheard text from clips that overlap Vector's own speech (+1 s). | `listen.Heard` |
| A7 (P) | Replies go out through `forkSay`, so the quiet lock's 423 silences them too. | `ttr/fork_listen.go` |
| A8 (P) | Overheard text stays in RAM; journal gets `myvector listen: <n> words → SKIP\|SPEAK <ms>`, never the words. | `listen.Heard` |
| C1 | Capture = `robot/openmic` (ruled 2026-11-07): `RecordMicDataClip` via `:8889/consolefunccall`, scp from `debugCapture`, 2 s windows / 1 s hop → myvector-stt. Refactor-first: move those `probe.sh` pieces into `robot/openmic/lib.sh`; `probe.sh` and the new loop both source it. | `robot/openmic/{lib.sh,probe.sh,listen.sh}` |
| C2 (P) | `listen.sh` (silent=1, records only) loops clips back to back, keeps the previous clip's last 2 s ahead of the next so a cut sentence is whole, joins windows into utterances, and POSTs each to `/myvector/listen/heard`. Server starts it on activate, kills it on deactivate (miccap repo-path precedent). | `robot/openmic/listen.sh`; `listen.Runner` |
| A9 | Chattiness normal: `listen.Cooldown` = 45 s between replies; SPEAK bar = a question in the air, a factual slip, his name, a clear fun opening (the Stage 2.1 SPEAK labels). | `listen.Cooldown`; `listen.Prompt` |
| P1 | (pending Stage 2.1) | `listenprobe` |

## Stages

| slice | work | proof | verify |
|---|---|---|---|
| 1.1 | `listen.Match` (A1), `listen.State` (A3). Phrase round trip: `robot/voice-samples/gen.sh` (TTS only, no robot sound) per phrase × af_bella + mari → myvector-stt → `Match`; add each variant STT produces. | `TestMatchTable`: phrases with `hey vector`, `palun`, punctuation hit; `activate the lights`, `listening to music`, `ära aktiveeri aktiivne kuulamine` miss. Red: contains-match → misses fail. | `cd chipper && go test ./pkg/myvector/listen/` |
| 1.2 | `forkListen` (A2) + A4 override. Confirmation en "Active listening on/off", et "Aktiivne kuulamine sees/väljas". | `TestForkListenShortCircuits`; `TestResolveWhileOn`: Claude profile + on → Ollama default target, off → Claude. Red: drop the override → fails. | `make check`; `go run ./pkg/myvector/routeprobe "hey vector, activate active listening"` → confirmation |
| 2.1 | `listenprobe` (followupprobe pattern): ≥ 2 prompts × 20 labeled snippets (10 en + 10 et; per language 4 SPEAK: question in the air, factual slip, Vector named, fun opening; 6 SKIP: small talk, phone-call half, TV, private talk, half sentence, already answered) × each language's Ollama default. Rule: ≥ 17/20, ≤ 1 false SPEAK per language, median ≤ 2 s wins; none → §OWED[USER] line, stop. Result → row P1. | probe table | `go run ./pkg/myvector/listenprobe` |
| 3.1 | C1 refactor-first: `lib.sh` extraction, `probe.sh` behaviour unchanged. | `probe.sh -r <saved run> et-EE` re-scores a saved `robot/work/openmic/` run to the same table before and after | that command, diffed |
| 3.2 | C2 `listen.sh` + `/myvector/listen/heard` + `listen.Runner`. Live check (USER, silent, robot records only): does a wake word still fire mid-clip? | fixture: two saved clips with a sentence across the cut → one utterance POSTed; `TestRunnerStopsOnOff` | `make check` |
| 4.1 | `listen.Heard`: off/quiet phrases first → A5 → SPEAK → `forkSay`; A6 echo gate; A9 cooldown; A3 idle + night auto-off. | `TestHeard` with httptest Ollama: SKIP → no say; SPEAK → one say; SPEAK inside `Cooldown` → none; `IdleOff` and night entry → state off; off phrase → state off, no LLM call; text overlapping own speech → no LLM call. | `make check` |
| 5.1 | `make check`; `code-reviewer` subagent over the diff. | — | — |
| 5.2 | USER (ask first, volume 1): activate en + et, talk near him 5 min with 2 relevant openings, deactivate. | `journalctl -u wire-pod \| grep 'myvector listen'` shows SKIP/SPEAK and no Claude pick | `docs/backlog.md` §OWED[USER] line at archive |
