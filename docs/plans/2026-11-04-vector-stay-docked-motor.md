# Vector stay docked: wheels and lift can't move him off the charger

**Goal**: no voice intent, behavior or SDK call moves Vector off the charger (wheels locked on contacts; lift already 0 via `lift-lock`); "Hey Vector, fist bump" leaves him docked.
**Status**: open — Stage 4 (user ship + live check).

## Rulings

Stage 2 ruled 2026-11-04: M1 + M3; lift stays on `lift-lock` (always 0, boot calibration verified); no M5, no software escape.

| id | ruling | edit site |
|---|---|---|
| M1 | Wheel power forced to 0 while `HAL::BatteryIsOnCharger()`; released 1000 ms after contacts lost (fails open); head and lift untouched | top of `HAL::MotorSetPower` (`hal_motors.cpp`), predicate `HAL::MotorLockActive()` in `hal.h` |
| M1-int | While `HAL::MotorLockActive()`, `WheelController::Run` calls `ResetIntegralGainSums()` so release can't lurch | `wheelController.cpp` |
| M1-log | `AnkiInfo("my-vector.MotorLock", …)` once per lock episode, on the first non-zero wheel request zeroed | `hal_motors.cpp` |
| M3 | On `IsOnChargerContacts()`, `DriveStraightAction::Init` ignores `SetCanMoveOnCharger(true)` → `SHOULDNT_DRIVE_ON_CHARGER`, log `my-vector.DriveGuard`; ⛔ a blanket `BODY_TRACK` guard in `IActionRunner::Update` — `TrackFaceAction` (HeadAndBody) locks `BODY_TRACK` | `basicActions.cpp` |
| E1 | Escape: lift him off by hand (contacts lost → wheels free after the latch) | — |
| F0 | Falsifier: `robot/firmware/soak.sh 1 30` while the user says "fist bump" ×3; judge `grep -c '^off-dock'` = 0, not the verdict (worn battery may fail the volts-rise check) | `soak.sh` |

## Checklist

### Stage 1: recon only (file/symbol evidence per answer, no build, read-only SSH)
- [x] Q0: is the shipped `series` on the robot? `/anki/etc/version` reads `3.0.2.0`; grep a `0003-quiet-idle` value in `/anki/data/assets/cozmo_resources/config/`.
- [x] Q1: does `BackupOntoChargerAction` / `MountChargerAction` still drive after first contact (M1 would strand him half on)?
- [x] Q2: contact flicker while docked — on-charger changes tracked in `cozmoBot.cpp` (`lastOnChargerChangedTime_ms_`) → M1 latch length.
- [x] Q3: engine reaction to stalled wheels under M1 — does `DriveOffChargerFace` retry, loop or trigger a stuck/pickup reaction?
- [x] Q4: any runtime toggle into the robot process (no `CONSOLE_VAR` in `robot/supervisor/src`) — reuse an engine→robot msg or a file flag for E1?
- [x] Q5: M2 — does the anim process skip body keyframes on a `BODY_TRACK` lock like it does for `LIFT_TRACK`? Which on-charger anims turn?
- [x] Q6: does SDK direct drive (`MovementComponent::DriveWheels`, `kDrivingWheelsStr`) honor track locks, or reach the robot process anyway?
- [x] Q7: syscon (body MCU) source is present in `robot/syscon/` (Keil `syscon.uvprojx`, `publickeygen.sh`) — is reflashing feasible and signed? Out of scope unless M1 fails.
- [x] Q8: E1 `CONSOLE_VAR` reachable at vic-engine `:8888/consolevars` (port live, netstat 2026-11-04) — name and default.
- [x] Q9: lift boot calibration (`HAL_LIFT_MOTOR_CALIB_POWER`) while docked under M1 — does it stall, and how does the engine react; exempt it?
- [x] F0 can fail: `soak.sh --summary` on a synthetic log with one `off` line → exit 1.

### Stage 2: pick a lever
- [x] `[USER DECISION]` ruled M1 + M3, no M5 (§Rulings).

### Stage 3: implement
- [x] Commit `motor-lock` (M1, M1-int, M1-log) then `drive-guard` (M3) on the clone's `my-vector` after `quiet-winsock`.
- [x] `robot/firmware/apply.sh --export` → `0005-motor-lock.patch` + `0006-drive-guard.patch` in `series`; `./build/build-v.sh` green.

### Stage 4: ship + live check (user)
- [ ] User: `make syncwt`, `make ship` from main — never Claude.
- [ ] User: "Hey Vector, fist bump" ×3 during F0; F0 off-dock count 0; `grep -c my-vector.MotorLock /var/log/messages` ≥1 on the robot (no logcat; exit 1 at 0).
- [ ] 24 h `robot/firmware/soak.sh` → PASS; `robot/firmware/quiet-count.sh` → PASS (no regression from stalled-drive retries).

## Rollback

Drop the `drive-guard` and/or `motor-lock` commit from `my-vector`, `apply.sh --export`, user redeploys via `make ship`.
