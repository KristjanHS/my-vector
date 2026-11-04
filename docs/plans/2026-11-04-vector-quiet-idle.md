# Vector quiet idle: very few, quiet sounds while he sits on the charger

**Goal**: powered on and idle on the dock, he makes only rare, quiet sounds; "Hey Vector", the wake-word chime and
spoken answers stay audible. **Status**: Stage 1 measured 2026-11-04; Stage 2–3 in progress. Same checkout, build and
deploy as 2026-11-04-vector-lift-lock.md (docs/plans/); robot steps are `[USER]`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| S1 | Baseline = boot→sleep 07:13–07:36 on 2026-11-04: `ListeningForBeats` 5, `RTS_OnCharger_*` 3, eye contact/motion 0 (wake-word turns excluded) | robot `/var/log/messages`, Stage 1 |
| S2 | Lower volume with no code: `/api-sdk/volume` `master_volume=1` (= LOW; MUTE 0 … HIGH 5, default 4) | `chipper/pkg/wirepod/sdkapp/server.go` (`/api-sdk/volume`) |
| S3 | Drop `ReactToSoundAwake` from `StayOnChargerDelegate` so noises in the room don't trigger reactions | `victorBehaviorTree/stayOnChargerUntilCharged.json` |
| S4 | Stretch idle gestures: every `small_motion_period_s` node floors at 300 s (stock ramps 0.5 → 30 s) | `observingOnChargerIdle.json` |
| S5 | Eye-contact and motion reactions stay: 0 hits in S1 | — |
| S6 | Ship S3, S4, S7 as `robot/stay-on-charger/quiet-idle.patch`, applied by `apply.sh` after `lift-lock.patch` | `robot/stay-on-charger/apply.sh` |
| S7 | `DanceToTheBeatCoordinator` wakes only off the charger: add `not OnChargerPlatform` to its `wantsToBeActivatedCondition`; voice "dance" untouched | `danceToTheBeat/danceToTheBeatCoordinator.json` |

## Open questions

- `[AUDIT]` TTS on the same bus as SFX? Stage 2's "what time is it" check decides by ear; if answers are too quiet, go back to 2.

## Checklist

### Stage 1: measure (no change) — done, S1
- [x] Count since boot: `grep -E 'vic-(engine|anim)' /var/log/messages` over SSH, split fields on `\x1f`, count `@behavior.feature.start` and `@action.play_animation`.

### Stage 2: volume (no code)
- [ ] `curl "http://localhost:8080/api-sdk/volume?serial=0090927c&volume=1"`; "Hey Vector, what time is it?" is still clearly audible.

### Stage 3: quiet-idle patch (PC side)
- [x] S3 + S4 + S7 in `robot/work/wire-os-victor`, export `quiet-idle.patch`, extend `apply.sh`; `./build/build-v.sh` is green.

### Stage 4: deploy + listen `[USER]` — today, with lift-lock; restarts the 24 h soak
- [ ] User: `make syncwt`, then `make ship` in the main checkout; rerun the Stage 1 count over awake + a sleep window: `RTS_OnCharger_*` and `ListeningForBeats` = 0, chime and answer still play.
- [ ] Restart `robot/stay-on-charger/soak.sh` and repoint the soak line in `docs/backlog.md` §OWED[USER].

## Rollback

`/api-sdk/volume` back to 4; redeploy without `quiet-idle.patch`, or restore `robot/backup/anki-2026-11-03.tgz`.
