# Lift recalibration loop after wake

**Goal**: an awake Vector calibrates the lift once, not every second.
**Status**: open 2026-11-04 — root cause not yet found; run `systematic-debugging` first.

| id | ruling | edit site |
|---|---|---|
| L0 | Symptom: once woken (SDK control at 12:37), `BehaviorReactToUncalibratedHeadAndLift` logs `LiftController.Calibrated … error 0.000 deg` ~1/s, head held up, last face frozen; `systemctl restart vic-engine` stops it | robot `/var/log/messages`; local copy `robot/work/calib-loop/messages-2026-11-04.txt` |
| L1 | Hypothesis (untested): 0002 lift-lock's `power_ = 0.f` leaves the engine's lift-uncalibrated flag set, so the behavior re-fires; falsify by waking Vector with 0002 off | `0002-lift-lock.patch`, `0005-motor-lock.patch` |
| L2 | Falsifier: `make robot-test` — its end-of-run `logcheck end` fails on 3 lift calibrations within 60 s (red on the saved log) | `robot/firmware/smoke.sh` |
