# Vector lift lock: the lift never moves, the eyes still animate

**Goal**: no lift motor movement ever (a lift move pried him off the contacts on 2026-11-04 and he lost power);
face/eye animation, head, voice and the stay-docked patch keep working.
**Status**: planned 2026-11-04; nothing built. Same checkout, build and deploy as slice 3b of
2026-11-03-vector-unlock-wireos.md (docs/plans/archive/); the robot step is `[USER]`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| L1 | Clamp at the one lift output: `LiftController::SetPower` always sends `HAL::MotorSetPower(MotorID::MOTOR_LIFT, 0)`; head, wheels untouched | `robot/supervisor/src/liftController.cpp` |
| L2 | Lift calibration never runs: report the lift calibrated at boot so `behaviorReactToUncalibratedHeadAndLift` can't loop | `liftController.cpp` `StartCalibrationRoutine` / `isCalibrated_` |
| L3 | Hold `LIFT_TRACK` locked for good from engine start so animation lift keyframes are dropped (eyes = face track, unaffected) | `MovementComponent::LockTracks`, owner `"my-vector-lift-lock"` |
| L4 | Accepted breakage: charger mount (raises lift to 45 mm), pick-up, fist bump, cliff recovery lift moves fail or time out | — |
| L5 | Ship as `robot/stay-on-charger/lift-lock.patch`; `apply.sh` applies both patches | `robot/stay-on-charger/apply.sh` |

## Open questions

- `[AUDIT]` With 0 lift power, does `CalibrationUpdate` (`LCS_LOWER_LIFT`) hang with no timeout? L2 removes the path; confirm by reading it.
- `[AUDIT]` Do `MoveLiftToHeightAction`/`MoveLiftToAngleAction` check `LIFT_TRACK` locks? If not, L1 alone stops them; check that they fail and don't retry forever.

## Checklist

### Stage 1: patch (PC side)
- [ ] L1 + L2 + L3 in `robot/work/wire-os-victor`, export `lift-lock.patch`, extend `apply.sh` (L5); `./build/build-v.sh` is green.

### Stage 2: deploy + live check `[USER]`
- [ ] `./build/deploy-v.sh`; check `md5sum /anki/bin/vic-robot /anki/lib/libcozmo_engine.so` on the robot matches `_build/vicos/Release/{bin,dist/lib}/`.
- [ ] "Hey Vector, fist bump" → the lift doesn't move, the eyes animate, he stays docked; "Hey Vector, what time is it?" is answered.
- [ ] No calibration loop: `logcat -d | grep -ci calibrat` is not rising after 10 min on the dock.

## Rollback

Restore `robot/backup/anki-2026-11-03.tgz` over SSH after `mount -o rw,remount /`, or redeploy without `lift-lock.patch`.
