# Lift recalibration loop after wake

**Goal**: an awake Vector calibrates the lift once, not every second.
**Status**: ✅ SHIPPED to the patch series 2026-11-04 — fix in `0002-lift-lock.patch`; live confirm owed after `make ship`.

| id | ruling | edit site |
|---|---|---|
| L0 | Symptom: `BehaviorReactToUncalibratedHeadAndLift` logs `LiftController.Calibrated … error 0.000 deg` ~1/s; a vic-robot restart or robot reboot on the charger does not stop it | `robot/work/calib-loop/messages-2026-11-04.txt` |
| L1 | Root cause: syscon `Encoders::lift_invalid` clears only while `Motors::lift_driven`; lift-lock never drives, so `LiftController::Update` re-latches `ENCODER_LIFT_INVALID` after every calibration | `robot/syscon/src/encoders.cpp`, `liftController.cpp` |
| L2 | Fix (ruled): `LiftController::Update` no longer latches `HAL::IsLiftEncoderInvalid()`; folded into 0002, src marker `lift_invalid only while the lift is driven` | `0002-lift-lock.patch`, `markers.tsv` |
| L3 | Falsifier: `make robot-test` — P1 loop bound + L3 `status & 0x400000 == 0` at A4 start/end | `robot/firmware/smoke.sh` |
