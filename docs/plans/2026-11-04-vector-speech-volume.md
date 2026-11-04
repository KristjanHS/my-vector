# Vector speech volume: loud answers, quiet idle

**Goal**: "Hey Vector, what time is it?" clearly audible; the rare idle sounds on the dock stay quiet.
**Status**: open — follows S8 of 2026-11-04-vector-quiet-idle.md (docs/plans/); interim `master_volume=2`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| V1 | Interim `master_volume=2` (MEDIUM_LOW): volume 1 was too quiet for answers (listen check, 2026-11-04) | `/api-sdk/volume` — done |
| V2 | Target: master back to 4; idle quietness comes from a per-bus RTPC (`Robot_Vic_Volume_Animation` / `_Behavior` / `_Procedural`), never from master | `audioParameterTypes.clad` (no setter exists yet) |
| V3 | Probe with no build via the vic-anim console func `SetAudioParameter`; keep the RTPC that quiets idle gestures but leaves the answer and `Play__Robot_Vic_Sfx__Wake_Word_On` loud | `cozmoAudioController.cpp` |
| V4 | No RTPC passes V3 → close at master 2 and archive | — |

## Checklist

### Stage 1: probe (no build)
- [ ] Find the vic-anim web console on the robot (port 8889); not live on WireOS → probe with a startup `PostParameter` build instead.
- [ ] Master 4; set each RTPC low in turn. `[USER]`: "what time is it?" + "fist bump" → answer/chime loud, gesture sound quiet?

### Stage 2: firmware (only if a V3 RTPC passes)
- [ ] Post it at engine start as `robot/stay-on-charger/speech-volume.patch`, applied by `apply.sh` after `quiet-idle.patch`; `./build/build-v.sh` green.
- [ ] User: `make syncwt`, `make ship` from main; `master_volume=4`; listen check `[USER]`; `quiet-count.sh` → PASS.

## Rollback

`/api-sdk/volume` back to 2; redeploy without `speech-volume.patch`.
