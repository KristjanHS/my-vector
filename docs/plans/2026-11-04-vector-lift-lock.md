# Vector lift lock: the lift never moves, the eyes still animate

**Goal**: no lift motor movement ever (a lift move pried him off the contacts on 2026-11-04 and he lost power);
face/eye animation, head, voice and the stay-docked patch keep working.
**Status**: Stage 1 built 2026-11-04; Stage 2 (deploy + live check) owed. Same checkout, build and deploy as slice 3b of
2026-11-03-vector-unlock-wireos.md (docs/plans/archive/); the robot step is `[USER]`.

## Rulings

| id | ruling | edit site |
|---|---|---|
| L1 | Clamp in `LiftController::SetPower`: always sends `HAL::MotorSetPower(MotorID::MOTOR_LIFT, 0)`; head, wheels and `testModeController` direct writes untouched | `robot/supervisor/src/liftController.cpp` |
| L2 | Calibration runs unpowered (completes at rest after `LIFT_STOP_TIME_MS`); `CalibrateMotorAction` drops `LIFT_TRACK` from its tracks so L3 can't fail it into a `ReactToUncalibratedHeadAndLift` loop | `engine/actions/basicActions.cpp` |
| L3 | Hold `LIFT_TRACK` locked for good from `MovementComponent::InitDependent`, re-taken in `UnlockAllTracks` (SDK behavior); eyes = face track, unaffected | `engine/components/movementComponent.{h,cpp}`, `kLiftLockOwner` |
| L4 | Accepted breakage: lift moves in charger mount (raises lift to 45 mm), pick-up, fist bump, cliff recovery, self-righting from back/side, CubeDrive fail with TRACKS_LOCKED | — |
| L6 | Animation actions drop `LIFT_TRACK` from their IAction lock set; `SetDesiredAngle_internal` returns early (keeps `inPosition_`); lock state re-sent once the robot connects | `animActions.cpp`, `liftController.cpp`, `movementComponent.cpp` |
| L5 | Ship as `robot/stay-on-charger/lift-lock.patch`; `apply.sh` applies both patches | `robot/stay-on-charger/apply.sh` |

## Checklist

### Stage 1: patch (PC side)
- [x] L1 + L2 + L3 in `robot/work/wire-os-victor`, export `lift-lock.patch`, extend `apply.sh` (L5); `./build/build-v.sh` is green.

### Stage 2: deploy + live check `[USER]`
- [ ] `./build/deploy-v.sh`; check `md5sum /anki/bin/vic-robot /anki/lib/libcozmo_engine.so` on the robot matches `_build/vicos/Release/{bin,dist/lib}/`.
- [ ] "Hey Vector, fist bump" → the lift doesn't move, the eyes animate, he stays docked; "Hey Vector, what time is it?" is answered.
- [ ] No calibration loop: `logcat -d | grep -ci calibrat` is not rising after 10 min on the dock.
- [ ] No lock-failure loop: `logcat -d | grep -c IActionRunner.Update.TracksLocked` is not rising after 10 min on the dock.

## Rollback

Restore `robot/backup/anki-2026-11-03.tgz` over SSH after `mount -o rw,remount /`, or redeploy without `lift-lock.patch`.
