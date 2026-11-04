# Vector speech volume: loud answers, quiet idle

**Goal**: "Hey Vector, what time is it?" clearly audible; the rare idle sounds on the dock stay quiet.
**Status**: open — follows S8 of 2026-11-04-vector-quiet-idle.md (docs/plans/); interim `master_volume=2`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| V1 | Interim `master_volume=2` (MEDIUM_LOW): volume 1 was too quiet for answers (listen check, 2026-11-04) | `/api-sdk/volume` — done |
| V2 | Target: master back to 4; idle quietness comes from a per-bus RTPC (`Robot_Vic_Volume_Animation` / `_Behavior` / `_Procedural`), never from master | `audioParameterTypes.clad` (no setter exists yet) |
| V3 | Probe with no build: `:8889/consolefunccall?func=SetAudioParameter&args=<rtpc>+0.3`, all three RTPCs first, then one at a time; keep the RTPC that softens idle gestures but leaves the answer and `Play__Robot_Vic_Sfx__Wake_Word_On` loud | `cozmoAudioController.cpp` |
| V5 | Probe sounds are fired from the console (engine `:8888` `SayText` / `PlayAnimationByName`, anim `PostAudioEvent`); one real "what time is it?" confirms the winner | — |
| V4 | No RTPC passes V3 → close at master 2 and archive | — |

## Checklist

### Stage 1: probe (no build)
- [x] vic-anim web console live on the robot (port 8889), `SetAudioParameter` registered.
- [ ] Master 4; RTPCs per V3/V5. `[USER]` listens: answer/chime loud, gesture sound soft?

### Stage 2: firmware (only if a V3 RTPC passes)
- [ ] Post it at engine start as a commit on the clone's `my-vector` after `quiet-idle`, exported by `robot/firmware/apply.sh --export`; `./build/build-v.sh` green.
- [ ] User: `make syncwt`, `make ship` from main; `master_volume=4`; listen check `[USER]`; `quiet-count.sh` → PASS.

## Rollback

`/api-sdk/volume` back to 2; redeploy without `speech-volume.patch`.
