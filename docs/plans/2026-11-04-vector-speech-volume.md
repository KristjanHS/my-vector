# Vector speech volume: loud answers, quiet idle

**Goal**: "Hey Vector, what time is it?" clearly audible; the rare idle sounds on the dock stay quiet.
**Status**: open — follows S8 of 2026-11-04-vector-quiet-idle.md (docs/plans/); interim `master_volume=2`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| V1 | Interim `master_volume=2` (MEDIUM_LOW): volume 1 was too quiet for answers (listen check, 2026-11-04) | `/api-sdk/volume` — done |
| V2 | Target: master back to 4; idle quietness comes from a per-bus RTPC (`Robot_Vic_Volume_Animation` / `_Behavior` / `_Procedural`), never from master | `audioParameterTypes.clad` (no setter exists yet) |
| V3 | Probe with no build: `:8889/consolefunccall?func=SetAudioParameter&args=<rtpc>+<lvl>`, ladder 1.0/0.5/0.25/0.1 per RTPC at master 4; keep the RTPC that softens idle gestures but leaves the answer and `Play__Robot_Vic_Sfx__Wake_Word_On` loud | `cozmoAudioController.cpp` |
| V5 | Fire sounds from the console (`PostAudioEvent`; voice via SDK `say_text`), measure dB via `SetWriteAudioOutputCapture` + user ear per play; capture fails → ear only | — |
| V4 | No RTPC passes V3 → close at master 2 and archive | — |
| V6 | Round B pick: `Robot_Vic_Volume_Animation=0.1` (Behavior/Procedural leave idle near-unchanged); chime + `say_text` voice unchanged | Stage 2 |
| V7 | Eye-move ticks muted: `kEnableKeepAliveEye{Blink,Dart,Squint}AudioEvents` default false (live: `:8889/consolevarset?key=EnableKeepAliveEyeBlinkAudioEvents&value=false`) | `audioLayerManager.cpp` |

## Checklist

### Stage 1: probe (no build)
- [x] vic-anim web console live on the robot (port 8889), `SetAudioParameter` registered.
- [x] Round A (master 4, all RTPCs 1.0): "what time is it?" answered at full volume.
- [x] Round B: per RTPC × ladder, measured dB + `[USER]` ear; real "what time is it?" at Animation 0.1 → chime + answer loud (V6).

### Stage 2: firmware (only if a V3 RTPC passes)
- [x] V6 in `CozmoAudioController` init (after the `SetSwitchState` block), V7 in `audioLayerManager.cpp` (also mutes engine-commanded squints) as `0008-speech-volume` on the clone's `my-vector`, exported by `robot/firmware/apply.sh --export`; `./build/build-v.sh` green.
- [ ] User: `make syncwt`, `make ship` from main; `master_volume=4`; listen check `[USER]`; `quiet-count.sh` → PASS.

## Rollback

`/api-sdk/volume` back to 2; redeploy without `0008-speech-volume.patch`.
