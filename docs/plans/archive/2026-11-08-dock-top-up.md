# Dock top-up — Vector restarts his own charge

Status: approach ruled 2026-11-08 (USER: "he tops himself up"); next Stage 0.1 recon. Nothing built.

## Rulings

| id | ruling | edit site |
|---|---|---|
| T1 | A top-up = off the contacts longer than syscon `ON_CHARGER_RESET` (> 60 s; use 75 s), then back on; syscon then starts a fresh charge. No head-side command can do it. | `robot/topup/run.sh`, scheduler |
| T2 | Each departure's true voltage = `@battery.on_charger_changed` with i1 = 0, i3 = raw mV in `/var/log/messages`; record it per top-up. | `robot/topup/run.sh`, Stage 2.2 |
| T3 | Motion is noisy: every top-up runs behind the quiet guard (`robot/env.sh` `quiet_guard`, not `silent=1`). | `robot/topup/run.sh`, scheduler |

## Open (rule before the stage that needs it)

| id | question | default proposal |
|---|---|---|
| O1 | Cadence after charge cut-off | 48 h; retune from T2 points |
| O2 | When allowed | outside the 0011 night window, quiet lock free, no voice session |
| O3 | Movement | nudge forward to break contact, 75 s, reverse same distance; go-home only as fallback |
| O4 | Failed re-dock | one go-home retry, then web-page alert + log; never loop |
| O5 | Firmware exception | console var `MyVectorTopUp` lifting 0001 / 0005 / 0006 only while set; dead-man auto-clear ≤ 5 min |
| O6 | Low-battery abort: a death off the dock strands him | T2 raw mV < 3.75 V at departure → reverse at once, web-page alert "lift me"; no 75 s wait |
| O7 | No-motion reset: can the head restart syscon via its bootloader path (`POWER_ERASE` → `enterBootloader`) without flashing, zeroing the charge timers? | recon in 0.1; if yes it replaces O3–O6 |
| O8 | Off-dock death = syscon cold start (power latch `POWER_EN` released) → charges on return regardless of `ON_CHARGER_RESET` | falsifier: next off-dock death's boot logs `@battery.is_charging_changed` i1 = 1 |

## Stages

| id | work | proof |
|---|---|---|
| 0.1 | Read-only recon: O7 first (head → syscon bootloader path, rampost `/etc/syscon.dfu`); then how 0001 / 0005 / 0006 block motion, SDK drive / go-home endpoints, :8889 console vars. Rule O7, then O3 + O5. | O-rows moved into Rulings |
| 1.1 | O5 patch in `robot/firmware/` (+ `series`, `markers.tsv`); default off = today. | `robot/firmware/check-static.sh`, `compat.sh`, `make check` |
| 1.2 | `robot/topup/run.sh` per T1–T3 + O3/O4: set flag → nudge off → confirm off → 75 s → reverse → confirm on + `is_charging` true → clear flag; print T2 mV. | `bash -n`, fake-SDK test; USER-watched live run (ask first; `robot/quiet/scan.sh` after) |
| 2.1 | Wire-pod scheduler per O1/O2 calling the same steps; status + last T2 mV on the web page; O4 alert. | `TestFork*` httptest fakes; `make check` |
| 2.2 | Drain trend on the page: each T2 mV + hours since cut-off → retune O1. | page lists the 11-08 points |
