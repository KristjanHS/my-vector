# Silent wake gate — a false wake on silence ends quietly, not with an answer to a made-up word

Goal: a KG wake turn whose clip STT scores as silence (`no_speech`) is dropped like a blank follow-up is today.

| id | ruling | edit site |
|---|---|---|
| S1 | Wake sensitivity 0.80 → **0.75**: `curl "http://<robot>:8080/api/mods/SensitivityPV/set?value=0.75"`, then POST `/api/extra/restartvic` (plays the wake-up sound — ask first; never during a `robot/tailscale/soak.sh` run). Verify `persistent/picovoice/sensitivity2` = 0.75 | robot only |
| S2 | Unarmed KG turn with `no_speech` ≥ `WakeNoSpeech` → `forkChain.Reset`, log `myvector: stt no-speech <p>, silent wake, dropped`, `forkKGSilence`, skip (no LLM, no follow-up arm) | `fork_followup.go` default case: one `forkChain.Begin`, gate `ok && p >= NoSpeech \|\| !ok && p >= WakeNoSpeech` |
| S3 | `WakeNoSpeech = 0.8` (silent wakes 0.897/0.926; real speech ≤ 0.543) | `followup` const block beside `NoSpeech` |
| S4 | Intent-matched wake turns (`!isKG`) stay ungated | none |

Falsifiers:
- `go test ./pkg/wirepod/ttr -run 'ForkFollowup'` (from chipper/): unarmed + 0.9 → skipped, zero LLM calls; unarmed + 0.7 → answered; armed + 0.7 → skipped. Each fails on the pre-change code.
- `make check` green.
- Live (USER, after `make ship`): next false wake → `journalctl -u wire-pod | grep 'silent wake'` hits, robot silent; a real request still answered.
