# Vector stay docked: wheels and lift can't move him off the charger

**Goal**: no voice intent, behavior or SDK call moves Vector off the charger; "Hey Vector, fist bump" leaves him docked.
**Status**: open — Stage 1 recon. Behavior patches (`stay-on-charger`, `lift-lock`) left holes: `FistBumpVoiceCommand` runs `DriveOffChargerFace`.

## Rulings

Levers ranked lowest-level / hardest to bypass first; Stage 2 picks one or a pair.

| id | ruling | edit site |
|---|---|---|
| M1 | Robot process: wheel + lift power forced to 0 (head stock) while `HAL::BatteryIsOnCharger()` (`POWER_ON_CHARGER`, contact voltage); release only after contacts lost for a latch time | `HAL::MotorSetPower` for `MOTOR_LEFT_WHEEL`/`MOTOR_RIGHT_WHEEL`/`MOTOR_LIFT` (`hal_motors.cpp`) or `WheelController::Run` (`wheelController.cpp`) |
| M1-safe | Fails open: contacts lost (pushed/lifted off) → stock wheels, so he can still drive home; stuck-true can only keep him docked | — |
| M2 | Engine: `BODY_TRACK` locked while `BatteryComponent::IsOnChargerContacts()`, unlocked off it — `lift-lock` template (`kLiftLockOwner` analogue, resend in `NotifyOfRobotState`, relock in `UnlockAllTracks`) | `MovementComponent` (`movementComponent.cpp/.h`) |
| M2-cost | Drive/turn actions fail `TRACKS_LOCKED`; anim actions must strip `BODY_TRACK` as `lift-lock` strips `LIFT_TRACK`, so body turns in anims vanish; head, eyes, face stay | `animActions.cpp` |
| M3 | Action guard: fail any action whose `GetTracksToLock()` has `BODY_TRACK` while on contacts; anims untouched | `IActionRunner::Update` (`actionInterface.cpp`) |
| M4 | Contrast only — today's approach: strip drive-off behaviors (`DriveOffChargerFace` in `fistBumpVoiceCommand.json`) one by one; whack-a-mole | behavior JSON |
| M5 | Interim until Stage 4: restore the server-side hold removed by `18541df` via the path-scoped `git checkout 18541df^ --` in `docs/backlog.md` §OWED[USER] `[USER DECISION]` | `chipper/pkg/myvector/autohold/`, `sdkapp/fork_autohold.go` |
| E1 | Escape for M1: lift him off by hand (contacts lost → wheels free); M2/M3 escape: an engine `CONSOLE_VAR` that skips the lock | `movementComponent.cpp` / `actionInterface.cpp` |
| F0 | Falsifier for all levers: `robot/firmware/soak.sh 1 30` while the user says "fist bump" ×3 → exit 1 on any off-dock reading | `soak.sh` |

## Checklist

### Stage 1: recon only (file/symbol evidence per answer, no build, read-only SSH)
- [ ] Q0: is the shipped `series` on the robot? `/anki/etc/version` reads `3.0.2.0`; grep a `0003-quiet-idle` value in `/anki/data/assets/cozmo_resources/config/`.
- [ ] Q1: does `BackupOntoChargerAction` / `MountChargerAction` still drive after first contact (M1 would strand him half on)?
- [ ] Q2: contact flicker while docked — on-charger changes tracked in `cozmoBot.cpp` (`lastOnChargerChangedTime_ms_`) → M1 latch length.
- [ ] Q3: engine reaction to stalled wheels under M1 — does `DriveOffChargerFace` retry, loop or trigger a stuck/pickup reaction?
- [ ] Q4: any runtime toggle into the robot process (no `CONSOLE_VAR` in `robot/supervisor/src`) — reuse an engine→robot msg or a file flag for E1?
- [ ] Q5: M2 — does the anim process skip body keyframes on a `BODY_TRACK` lock like it does for `LIFT_TRACK`? Which on-charger anims turn?
- [ ] Q6: does SDK direct drive (`MovementComponent::DriveWheels`, `kDrivingWheelsStr`) honor track locks, or reach the robot process anyway?
- [ ] Q7: syscon (body MCU) source is present in `robot/syscon/` (Keil `syscon.uvprojx`, `publickeygen.sh`) — is reflashing feasible and signed? Out of scope unless M1 fails.
- [ ] Q8: E1 `CONSOLE_VAR` reachable at vic-engine `:8888/consolevars` (port live, netstat 2026-11-04) — name and default.
- [ ] Q9: lift boot calibration (`HAL_LIFT_MOTOR_CALIB_POWER`) while docked under M1 — does it stall, and how does the engine react; exempt it?
- [ ] F0 can fail: `soak.sh --summary` on a synthetic log with one `off` line → exit 1.

### Stage 2: pick a lever
- [ ] `[USER DECISION]` M1 (wheels + lift) alone, M1+M3 (M3 stops the engine fighting stalled wheels), or M2; and M5 yes/no as the interim.

### Stage 3: implement
- [ ] Commit `motor-lock` on the clone's `my-vector` after `quiet-winsock`, log tag `my-vector.MotorLock` on each zeroed request.
- [ ] `robot/firmware/apply.sh --export` → `0005-motor-lock.patch` in `series`; `./build/build-v.sh` green.

### Stage 4: ship + live check (user)
- [ ] User: `make syncwt`, `make ship` from main — never Claude.
- [ ] User: "Hey Vector, fist bump" ×3 during F0; F0 exit 0; `my-vector.MotorLock` logcat count ≥1 (`grep -c`, exit 1 at 0).
- [ ] 24 h `robot/firmware/soak.sh` → PASS; `robot/firmware/quiet-count.sh` → PASS (no regression from stalled-drive retries).

## Rollback

Drop the `motor-lock` commit from `my-vector`, `apply.sh --export`, user redeploys via `make ship`; M5 as the stopgap.
