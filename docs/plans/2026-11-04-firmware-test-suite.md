# Firmware test suite: static checks in `make check`, on-robot smoke after deploy

**Goal**: a firmware change that breaks a patch or a main function fails a command — static in seconds (`make check`), live in ~30 s (`make robot-test`, and `make ship` before its push).
**Status**: staged 2026-11-04 — 3.2 next (gate ruled). Scope: no host unit tests, no Anki gtests; nothing moves the wheels.

## Refactor first

| id | ruling | edit site |
|---|---|---|
| RF1 | Extract the series→commit loop into `build_tip()`; new `--check` = `build_tip` + format-patch `$pin..$tip` to a temp dir + `diff -r` against `*.patch`/`series`; no branch move, checkout or submodule update | `robot/firmware/apply.sh` |
| RF2 | ship.sh's JSON `cmp` loop moves into `check-static.sh --built` (one home for built = patched); `tree_is_patches` stays in ship.sh | `robot/firmware/{ship.sh,check-static.sh}` |
| RF3 | ship.sh's "units active, still active 15 s later" step is superseded by `smoke.sh` (same 180 s wait); delete it with the ship gate (Stage 3.3), not before | `robot/firmware/ship.sh` |
| RF4 | Skip: connection facts stay where `quiet-count.sh` (IP, key via git-common-dir) and `soak.sh` (ESN, `SERVER`) resolve them; R6 in `2026-11-04-architecture-refactors.md (docs/plans/)` owns the move, and its grep falsifier catches `smoke.sh` | — |

## Rulings

| id | ruling | edit site |
|---|---|---|
| M0 | Manifest, TAB-separated `patch  kind  path  marker`, kind `src` (path in the victor tree) or `bin` (path under `_build/vicos/Release/`); every series patch has ≥1 `src` row; 0004 has no `bin` row | `robot/firmware/markers.tsv` (new) |
| M0-rows | 0001 src `behaviorStayOnChargerUntilCharged.cpp` + bin `lib/libcozmo_engine.so`: `StayOnChargerUntilCharged.BehaviorUpdate.OffCharger` · 0002 src `liftController.cpp`: `power_ = 0.f`, bin engine .so: `my-vector-lift-lock` · 0003 src `danceToTheBeatCoordinator.json`: `"not": { "conditionType": "OnChargerPlatform" }`, bin `data/assets/cozmo_resources/config/…/globalInterruptions.json`: `outranks ReactToSoundAwake` · 0004 src civetweb `CMakeLists.txt`: `if (WIN32)` · 0005 src `hal_motors.cpp` + bin `bin/vic-robot`: `my-vector.MotorLock` · 0006 src `basicActions.cpp` + bin engine .so: `my-vector.DriveGuard` | `markers.tsv` |
| S1 | Series ↔ files ↔ manifest: every series line is an existing `*.patch`, every `*.patch` is in series once, every series patch is in the manifest, no manifest row names a patch outside series | `robot/firmware/check-static.sh` (new) |
| S2 | `apply.sh --check` (RF1) exits 0 = re-export byte-identical | `check-static.sh` |
| S3 | Each `src` marker: `git show $tip:$path \| grep -qF` hits AND `git show $pin:$path` misses it (patch-specific) | `check-static.sh` |
| S4 | `--built`: each `bin` marker in `strings $build/$path` + RF2's JSON cmp | `check-static.sh`, ship.sh's built-assets step |
| S5 | Safety: `grep -nE 'move_wheels\|move_lift\|DriveWheels\|DriveStraight\|MoveLift'` over `smoke.sh` + `fork_smoke.go` → any hit fails | `check-static.sh` |
| S-src | Victor clone = `VICTOR_SRC`, else `<main checkout>/robot/work/wire-os-victor` via git-common-dir; no clone → S2/S3 print `SKIP: no victor clone` and S1/S5 still run; never clone from check | `check-static.sh` |
| W1 | `check.sh` runs `$dir/robot/firmware/check-static.sh` after the shell-syntax loop (so `--head` checks the shipped HEAD's manifest) | `scripts/check.sh` |
| F1 | Fork endpoints registered in `init()` (exact patterns win over `/api-sdk/`; no upstream edit), robot via `getRobot(serial)`: `fork_robot_state` = first `EventStream` robot_state as JSON (head_angle_rad, pose, status); `fork_face` = `DisplayFaceImageRGB` solid 184×96 for 2000 ms → `ok` or the error; `fork_say` = `SayText` → its final state name | `chipper/pkg/wirepod/sdkapp/fork_smoke.go` (new) |
| P1 | Passive: `conn_test` answers within 20 s (exit 2 if not); `vic-engine vic-anim vic-robot` active within 180 s, PIDs unchanged 15 s later AND at the end of the run; `/var/log/messages` since the current vic-robot PID's first line has `LiftController.Calibrated` and 0 lines matching `Fatal\|SIGSEGV\|SIGABRT\|terminate called\|Backtrace`; `curl -s -m 5 $ip:8888/consolevars` answers 200 | `robot/firmware/smoke.sh` (new) |
| P2 | Docked: `get_battery` has `"is_on_charger_platform":true` and `"is_charging":true` (false booleans are omitted) before and after; `is_charging` absent at rest (Q1) → WARN, never FAIL | `smoke.sh` |
| A1 | Under `assume_behavior_control` (default priority, settle 2 s, `release` in an EXIT trap): `move_head speed=1` 2 s, `speed=0`, read ≥ `MAX_HEAD_ANGLE` − 0.1 rad; `speed=-1` 2 s, `speed=0`, read ≤ `MIN_HEAD_ANGLE` + 0.1 rad | `smoke.sh` |
| A2 | `fork_face` → `ok`; `fork_say text=smoke test` → `FINISHED` (`SayTextResponse_FINISHED`) within 20 s | `smoke.sh` |
| A4 | Wheels put: `fork_robot_state` pose Δx, Δy ≤ 5 mm start → end (pick a default; Stage 3.2's rest sample validates) and `RobotStatus_ROBOT_STATUS_ARE_WHEELS_MOVING` clear in both samples | `smoke.sh` |
| X | Exit 0 PASS, 1 FAIL (first failing check named), 2 setup (no ESN/key/wire-pod), like `quiet-count.sh`; `set -euo pipefail`, `die()`, curl `-m 20` | `smoke.sh` |
| W2 | `make robot-test` → `smoke.sh`; ship: after deploy, `smoke.sh` replaces the units step (RF3) → non-zero = `die` before push, message names the sha the robot now runs | `Makefile`, `ship.sh` |

## Checklist

Every check gets a red demo: mutate → command exits non-zero → restore by retyping (never `git checkout`).

### Stage 1: live probes (read-only: GETs, SSH `cat`/`grep`/`systemctl`; no SDK control, no motion)
- [x] Q1: at rest 3.57 V → `is_on_charger_platform:true`, `is_charging` absent.
- [x] Q2: no clean motion-free intent signal (LOG_DEBUG compiled out) → A3 dropped at the 3.2 gate.
- [x] Q3: 0 Fatal-pattern hits at rest; `LiftController.Calibrated … reason Startup` once under the vic-robot PID.
- [x] Q4: `:8888/consolevars` → 200; `conn_test` body = `success`.

### Stage 2: static (`make check`)
- [x] 2.1 `markers.tsv` + `check-static.sh` S1, S5, S-src; W1; red demo S1 (extra series line) + S5 (`move_wheels` in a smoke comment).
- [x] 2.2 RF1 `apply.sh --check` + S2, S3; `make check` stays under 10 s; red demos: a patch's `index` line hash edited (S2), a marker typo (S3 hit), a marker present at pin e.g. `#include` (S3 miss).
- [x] 2.3 S4 `--built` + RF2 in ship's built-assets step; red demo: a `bin` marker typo against the current `_build`.

### Stage 3: smoke (`make robot-test`)
- [x] 3.1 `smoke.sh` P1, P2, X + `make robot-test`; red demos: `vic-bogus` in units, Fatal pattern `LiftController`, P2 expects `false`.
- [x] `[USER DECISION]` ruled 2026-11-04: A3 dropped · `fork_smoke.go` OK · `is_charging` absent → WARN (built in 3.1).
- [ ] 3.2 F1 `fork_smoke.go` + A1, A2, A4; `go vet` sdkapp green; two `fork_robot_state` reads 10 s apart at rest before A4's bound lands; red demos: head bound 2.0 rad, say expects `XFINISHED`, pose bound −1 mm.
- [ ] 3.3 W2 ship gate + RF3 delete; `total` step count updated; `NO_DEPLOY=1 make ship` still stops after the build.

### Stage 4: full verify
- [ ] `make check` green; code review in a fresh sub-agent.
- [ ] User (needs wire-pod + robot): `make robot-test` → PASS in ≤ 45 s; eyeball the solid face during `fork_face`; robot still on the contacts.
- [ ] User: next `make ship` runs the smoke gate before the push.
