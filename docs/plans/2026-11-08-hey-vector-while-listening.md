# "Hey Vector" asks while active listening is on

Status: staged 2026-11-08 from the `2026-11-08-mic-stream.md` (docs/plans/) 1.2 listening run; H2 USER DECISION owed first.

## Rulings

| id | ruling | edit site |
|---|---|---|
| H1 | Observed 20:48:17: the stream's copy of "Hey Vector, kui palju kell on?" → listen `SPEAK` 1 ms before the wake-word turn's STT → clock intent lost, listen reply spoken instead. A stream utterance overlapping a wake-word turn must not SPEAK. | `listen.Hearer` decision vs `State.Turn` (`forkListen`) |
| H2 | [USER DECISION] "mis mu nimi on?" hits built-in `intent_names_ask` (face recognition → "I don't know", PC voice); the LLM never runs, so the about-me name is unused. Options: drop it from `forkAllowedIntents` (LLM answers from about-me) · keep it (enroll the face). | `chipper/pkg/vars/fork_intents.go` |
| H3 | Observed 20:48:59: listen `SPEAK` on a 1-word utterance (unprompted). Check whether a minimum word count belongs in the decider. | `listen.Hearer` / `followupprobe` |

## Stages

| id | step | proof |
|---|---|---|
| 1.1 | H1: hold a SPEAK decision until no wake-word turn started within the utterance window; drop it if one did. | `TestHeardWakeTurn` (overlap → no speak); `make check` |
| 1.2 | H2 per the ruling. | `go run ./pkg/myvector/routeprobe -lang et-EE "mis mu nimi on"` (from chipper/) |
| 1.3 | H3: measure 1-word SPEAK rate on `followupprobe`; cut only if it misfires. | `followupprobe` output |
| 2.1 | USER live (volume 1): listening on, 3 "Hey Vector" asks (clock, name, one LLM ask) → each answered once, no listen SPEAK on them. | `journalctl -u wire-pod \| grep -E 'SPEAK\|Intent Sent'` |
