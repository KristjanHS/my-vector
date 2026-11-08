# "Hey Vector" asks while active listening is on

Status: staged 2026-11-08 from the `2026-11-08-mic-stream.md` (docs/plans/) 1.2 listening run; H2 USER DECISION owed first.

## Rulings

| id | ruling | edit site |
|---|---|---|
| H1 | Observed 20:48:17: the stream's copy of "Hey Vector, kui palju kell on?" → listen `SPEAK` 1 ms before the wake-word turn's STT → clock intent lost, listen reply spoken instead. A stream utterance overlapping a wake-word turn must not SPEAK. | `listen.Hearer` decision vs `State.Turn` (`forkListen`) |
| H2 | [USER DECISION] "mis mu nimi on?" hits built-in `intent_names_ask` → robot `WhatsMyName` behavior → `sayname.dont_know`, spoken via patch 0010 PC voice (robot `/var/log/messages`); the LLM never runs, so the about-me name is unused. Options: drop it from `forkAllowedIntents` (LLM answers from about-me) · keep it (enroll the face). | `chipper/pkg/vars/fork_intents.go` |
| H3 | Observed 20:48:59: listen `SPEAK` on a 1-word utterance (unprompted). Check whether a minimum word count belongs in the decider. | `listen.Hearer` / `followupprobe` |
| H4 | Observed 20:32–20:36: et-EE STT on/off misses ("activate active listering", "deaktiveeriv aktiivne kuulamine") fall to the LLM, which says "Activating active listening right now." and does nothing. Near-miss match, and the LLM must not claim a switch. | `listen.Match`; LLM prompt |
| H5 | On/off confirmation (`listen.Confirm`, already Estonian) is spoken by the robot's built-in voice: `forkSpeak` sends KNOWLEDGE_GRAPH `SpokenText`. Speak it in the PC voice. | `forkSpeak` → `forkSayText` |

## Stages

| id | step | proof |
|---|---|---|
| 1.1 | H1: hold a SPEAK decision until no wake-word turn started within the utterance window; drop it if one did. | `TestHeardWakeTurn` (overlap → no speak); `make check` |
| 1.2 | H2 per the ruling. | `go run ./pkg/myvector/routeprobe -lang et-EE "mis mu nimi on"` (from chipper/) |
| 1.3 | H3: measure 1-word SPEAK rate on `followupprobe`; cut only if it misfires. | `followupprobe` output |
| 1.4 | H4: near-miss `listen.Match`; LLM prompt line: never claim listening was switched. | `go test ./pkg/myvector/listen -run TestMatch` with both strings as cases (from chipper/) |
| 1.5 | H5: confirmation through the PC voice. | `! grep -q 'SpokenText: text' chipper/pkg/wirepod/ttr/fork_listen.go`; `make check` |
| 2.1 | USER live (volume 1): on/off by voice in both languages (confirmation in the PC voice), 3 "Hey Vector" asks (clock, name, one LLM ask) → each answered once, no listen SPEAK on them. | `journalctl -u wire-pod \| grep -E 'SPEAK\|Intent Sent'` |
