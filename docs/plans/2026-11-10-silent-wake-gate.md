# Silent wake gate — a false wake on silence ends quietly, not with an answer to a made-up word

Goal: an unarmed turn whose clip STT scores as silence (`no_speech` ≥ `WakeNoSpeech`) is dropped like a blank follow-up is today.

| id | ruling | edit site |
|---|---|---|
| S1 | Wake sensitivity 0.80 → **0.75** — USER sets it in the wire-pod UI (ruling 11-10); verify `persistent/picovoice/sensitivity2` = 0.75 | `docs/backlog.md` §OWED[USER] |
| S2 | Silent wakes reach `StreamingKGSim` with `isKG=false` (intent-graph LLM path, logs 10:40 / 19:05). `!isKG` + `p ≥ WakeNoSpeech` → `forkChain.Reset`, log `myvector: stt no-speech <p>, silent wake, dropped`, `forkNoAudio(req)` (`intent_system_noaudio`, as an empty-transcript wake), skip | `fork_followup.go` `forkBegin` gains `req`; `kgsim.go` call passes it |
| S3 | `WakeNoSpeech = 0.8` (silent wakes 0.897/0.926; real speech ≤ 0.543) | `followup` const block beside `NoSpeech` |
| S4 | Intent-matched wakes stay ungated (never reach `StreamingKGSim`) | none |
| S5 | Unarmed KG turns ("I have a question") gate at `WakeNoSpeech` too — reverses follow-up-listening 7.5 (ruling 11-10): default case `armed && p ≥ NoSpeech \|\| !armed && p ≥ WakeNoSpeech` → same log, `forkKGSilence` | `fork_followup.go` default case |

Falsifiers:
- `go test ./pkg/wirepod/ttr -run 'ForkBegin'` (from chipper/): `!isKG` 0.9 → skip + one noaudio; `!isKG` 0.7 → answered; unarmed KG 0.94 → skip, 0.7 → answered; armed KG 0.7 → skip. Each fails on the pre-change code.
- `make check` green.
- Live (USER, after `make ship`): next false wake → `journalctl -u wire-pod | grep 'silent wake'` hits, robot silent; a real request still answered.
