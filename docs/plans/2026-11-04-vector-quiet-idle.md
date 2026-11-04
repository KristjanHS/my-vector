# Vector quiet idle: very few, quiet sounds while he sits on the charger

**Goal**: powered on and idle on the dock, he makes only rare, quiet sounds; "Hey Vector", the wake-word chime and
spoken answers stay audible. **Status**: planned 2026-11-04; nothing changed. Same checkout, build and deploy as
2026-11-04-vector-lift-lock.md (docs/plans/); robot steps are `[USER]`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| S1 | Measure first: count which behaviors/anims fire in 30 min idle and rank them before cutting | robot `logcat`, Stage 1 |
| S2 | Lower volume with no code: fork `/api-sdk/volume` sets `master_volume` (default 4) | `chipper/pkg/wirepod/sdkapp/server.go` (`/api-sdk/volume`) |
| S3 | Drop `ReactToSoundAwake` from `StayOnChargerDelegate` so noises in the room don't trigger reactions | `victorBehaviorTree/stayOnChargerUntilCharged.json` |
| S4 | Stretch idle gestures: `small_motion_period_s` nodes floor at 300 s (stock ramps 0.5 → 30 s) | `observingOnChargerIdle.json` |
| S5 | Cut the eye-contact and motion reactions on the charger (`ObservingOnChargerEyeContact`, `ReactToMotionOnCharger`) only if S1 ranks them high | `observingOnCharger.json` |
| S6 | Ship S3–S5 as `robot/stay-on-charger/quiet-idle.patch`, applied by `apply.sh` next to `lift-lock.patch` | `robot/stay-on-charger/apply.sh` |

## Open questions

- `[AUDIT]` `master_volume` value → level mapping (`Volume` MUTE..HIGH in `engineRobotAudioClient.cpp`); pick the value that gives LOW.
- `[AUDIT]` Is text-to-speech on the same Wwise bus as the effects (`Victor_Robot_SFX_Character_Buses`)? If yes, S2 also quiets answers; prefer S3–S5.
- `[AUDIT]` The wake-word chime must not depend on `ReactToSoundAwake` (S3); check `behaviorReactToSound.cpp` vs the voice-command path.

## Checklist

### Stage 1: measure (no change) `[USER]` for the quiet half hour
- [ ] 30 min docked and idle, then `logcat -d | grep -oE 'BehaviorSystem.*Activated[^ ]*|PlayAnimation[^ ]*' | sort | uniq -c | sort -rn | head -20` over SSH; record the top sources here.

### Stage 2: volume (no code)
- [ ] `curl "http://localhost:8080/api-sdk/volume?serial=0090927c&volume=<LOW>"`; "Hey Vector, what time is it?" is still clearly audible.

### Stage 3: quiet-idle patch (PC side)
- [ ] S3 + S4 (+ S5 if Stage 1 says so) in `robot/work/wire-os-victor`, export `quiet-idle.patch`, extend `apply.sh`; `./build/build-v.sh` is green.

### Stage 4: deploy + listen `[USER]`
- [ ] `./build/deploy-v.sh`; rerun the Stage 1 count: the top idle sources drop at least 5×, and the wake-word chime and answer still play.

## Rollback

`/api-sdk/volume` back to 4; redeploy without `quiet-idle.patch`, or restore `robot/backup/anki-2026-11-03.tgz`.
