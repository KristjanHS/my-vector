# "Hey Vector" asks while active listening is on

Status: staged 2026-11-08 from the `2026-11-08-mic-stream.md` (docs/plans/) 1.2 listening run; H1–H4 ruled 2026-11-08 (qimpag), executing.

## Rulings

| id | ruling | edit site |
|---|---|---|
| H1 | Observed 20:48:17: the stream's copy of "Hey Vector, kui palju kell on?" → listen `SPEAK` 1 ms before the wake-word turn's STT → clock intent lost. Both gates: stamp the wake turn at its audio START (preqs `forkMicCapture`, fork file) and drop a SPEAK if a turn started since the utterance's `Start`; also skip stream text starting with "hey/hei vector". | `listen.Hearer`; `chipper/pkg/wirepod/preqs/fork_miccap.go` |
| H2 | "mis mu nimi on?" → the LLM answers from about-me: drop `intent_names_ask` from `forkAllowedIntents`. | `chipper/pkg/vars/fork_intents.go` |
| H3 | Observed 20:48:59: listen `SPEAK` on a 1-word utterance (unprompted). Measure 1-word cases on `listenprobe` (~20 calls); add a min-word gate only if it misfires. | `listen.Hearer` / `listenprobe` |
| H4 | Observed 20:32–20:36: et-EE STT on/off misses ("activate active listering", "deaktiveeriv aktiivne kuulamine") fall to the LLM, which says "Activating active listening right now." and does nothing. Fuzzy match: small per-word slips, only in utterances ≤5 words holding the listen word + an on/off cue; LLM prompt: never claim a switch. | `listen.Match`; LLM prompt |
| H5 | On/off confirmation (`listen.Confirm`, already Estonian) is spoken by the robot's built-in voice: `forkSpeak` sends KNOWLEDGE_GRAPH `SpokenText`. Speak it in the PC voice. | `forkSpeak` → `forkSayText` |

## Stages

| id | step | proof |
|---|---|---|
| 1.1 | H1: turn-start stamp + text gate; drop SPEAK on overlap. | `TestHeardWakeTurn` (overlap → no speak; wake text → no speak); `make check` |
| 1.2 | H2 per the ruling. | `go run ./pkg/myvector/routeprobe -lang et-EE "mis mu nimi on"` (from chipper/) |
| 1.3 | H3: measure 1-word SPEAK rate on `listenprobe`; cut only if it misfires. | `listenprobe` output |
| 1.4 | H4: fuzzy short `listen.Match`; LLM prompt line: never claim listening was switched. | `go test ./pkg/myvector/listen -run TestMatch` with both strings + a 10-word non-match (from chipper/) |
| 1.5 | H5: confirmation through the PC voice. | `! grep -q 'SpokenText: text' chipper/pkg/wirepod/ttr/fork_listen.go`; `make check` |
| 2.1 | USER live (volume 1): on/off by voice in both languages (confirmation in the PC voice), 3 "Hey Vector" asks (clock, name, one LLM ask) → each answered once, no listen SPEAK on them. | `journalctl -u wire-pod \| grep -E 'SPEAK\|Intent Sent'` |
