# Log-level intent: motion-free app intent that turns on vic-engine debug logging

**Goal**: `cloud_intent intent=my_vector_log_debug` → vic-engine DEBUG lines (all channels) in `/var/log/messages` for 10 min, no behavior runs; `my_vector_log_info` reverts early. Its marker line is A3 for `2026-11-04-firmware-test-suite.md (docs/plans/)`.
**Status**: staged 2026-11-04 — Stage 1 next; Stage 3 runs before Stage 2 (A3 static + red demos only until 0007 is live). Scope: vic-engine only; all-channels DEBUG on/off, no per-channel choice.

## Refactor first

Skip: one new patch on the existing series; the intercept is one call site, the gate one util function.

## Rulings

VT = `robot/work/wire-os-victor`.

| id | ruling | edit site |
|---|---|---|
| L1 | Two intent names, no param (wire-pod `cloud_intent` forwards none → no Go change): `my_vector_log_debug`, `my_vector_log_info`. Intercept at the top of `OnAppIntent`, before `GetUserIntentFromAppIntent`; `return` without `DevSetUserIntentPending` → no behavior, no motion | `VT/engine/aiComponent/behaviorComponent/userIntentComponent.cpp` |
| L2 | Marker `LOG_WARNING("my-vector.LogLevel", …)`: `debug on for %d s` / `info (app)` / `info (timeout)` — WARNING skips the channel filter | same |
| L3 | Gate `std::atomic<bool> gMyVectorDebugLog{false}` (decl `logging.h`, def `logging.cpp`); `sChanneledDebugF`/`sChanneledDebugV`/`sChanneledDebug` drop their `#if ALLOW_DEBUG_LOGGING` and return first when the gate is false, before `vsnprintf`; every `#if ALLOW_DEBUG_LOGGING` macro branch also wraps its `sChanneledDebug*` call in `if (gMyVectorDebugLog)` → args not evaluated while off | `VT/lib/util/source/anki/util/logging/logging.{h,cpp}` |
| L4 | `ALLOW_DEBUG_LOGGING=1` in `target_compile_definitions(cozmo_engine PRIVATE …)` only — not util, not coretech (root CMake drops non-`ANKI_` `-D` args) | `VT/engine/CMakeLists.txt` |
| L5 | All channels while on: `ILoggerProvider::PrintChanneledLogD` skips the filter when `gMyVectorDebugLog` (engine can't reach its `ChannelFilter`); INFO stays filtered, nothing to restore | `VT/lib/util/source/anki/util/logging/iLoggerProvider.cpp` |
| L6 | Revert deadline now + 600 s, checked in `UserIntentComponent::UpdateDependent` via `BaseStationTimer`; boot = gate off, channels as configured | `userIntentComponent.cpp` |
| L7 | `0007-log-level-intent.patch`: authored in the clone, `apply.sh --export`; body names `vic-engine` + `libankiutil`, ends `Logs my-vector.LogLevel.`, no `---` line | `robot/firmware/{series,0007-log-level-intent.patch}` |
| L8 | Rows: src `userIntentComponent.cpp` `my_vector_log_debug` · src `logging.cpp` `gMyVectorDebugLog` · bin `lib/libcozmo_engine.so` `my-vector.LogLevel` · bin `lib/libankiutil.so` `gMyVectorDebugLog` (drop if `strings` misses it) | `robot/firmware/markers.tsv` |
| L9 | A3: `cloud_intent my_vector_log_debug` → `my-vector.LogLevel debug on` within 10 s AND ≥1 new vic-engine `BehaviorSystem`-channel line; then `my_vector_log_info` → `info (app)`; EXIT trap also sends `my_vector_log_info`; A4's pose span covers A3 | `robot/firmware/smoke.sh` |

## Checklist

Every check gets a red demo: mutate → command exits non-zero → restore by retyping (never `git checkout`).

### Stage 1: patch 0007 in the clone
- [x] 1.1 L5 audit → answer written into L5.
- [x] 1.2 L1–L6 in the clone on `my-vector`, commit, `apply.sh --export` → 0007 + series line; L8 rows; `make check` green; red demo: an L8 src marker typo.
- [ ] 1.3 `build/build-v.sh` (background); `-Werror` clean; `check-static.sh --built` green; red demo: a bin marker typo.

### Stage 2: live (USER: `make syncwt` + `make ship`)
- [ ] 2.1 At rest after ship: 0 `my-vector.LogLevel` lines; `quiet-count.sh` rate ≈ pre-ship.
- [ ] 2.2 `my_vector_log_debug` → marker + `BehaviorSystem` lines, pose Δ ≤ 5 mm; `my_vector_log_info` → marker, rate back to rest; one run left to `info (timeout)` at 10 min.

### Stage 3: A3 in the smoke test
- [x] 3.1 L9 in `smoke.sh` (A3 FAILs without sending the intent until 0007 is on the robot); S5 green; offline matcher red demo done.
- [ ] 3.1b After ship, live red demos: wrong intent name, marker expects `debug off`.
- [x] 3.2 Test-suite plan Q2 line → A3 owned here.

### Stage 4: full verify
- [ ] `make check` green; code review in a fresh sub-agent over 0007 + the smoke diff.
- [ ] User: `make robot-test` PASS with A3; robot still on the contacts.
