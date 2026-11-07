# Vector crash review — every change we make, eliminated one by one

Status: **plan, not started.** Method = `systematic-debugging`: reproduce → one variable per test → pre-registered pass/fail.
Authored 2026-11-07; execution is a fresh session (user: "write .md, don't launch debugging in this session").

## Symptoms (S)

| id | symptom | seen | evidence |
|---|---|---|---|
| S1 | vic-anim `SeAssert se_lib/nrgainv.c:191` → SIGILL | 11-05 09:22, 11-07 11:54 | both ship deploys, ~13 s after start, first speaker audio; `2026-11-07-vic-anim-seassert.md (docs/plans/)` |
| S2 | vic-engine SIGSEGV → fault 914 | 11-07 12:31, 12:37 | pc=0 from `libOpenCL.so+0xa480`, stack `tflite::gpu::cl::CreateDefaultGPUDevice`; 3–7 s after a stop→start |
| S3 | vic-anim.service start timeout → fault 800 | 11-07 12:33 | during my catch loop (rapid restarts); no gdb on that pid |
| S4 | vic-engine dump, night idle | 11-06 05:18 | dump all zeros; /data was full (`No space left on device` 11-05 21:08) |
| S5 | ship smoke FAIL | 4 logs: 1105-204236, 1106-071657, 1107-114118, 1107-115312 | `robot/work/ship-logs/`; which S each was = E0 |

Not causes: vic-switchboard SIGQUIT / vic-cloud SIGTERM / 4-process `.dmp~` groups = fault-handler or stop cascades.
Dumps + `.dmp.log` copies: `robot/work/crash-review/`, `robot/work/seassert/`; reader `robot/seassert/md.py`.

## Critical points before blaming any change

- **Exposure, not just code:** S1–S3 are all *startup* crashes. Vanilla days had ~no restarts; we did 1 / 13 / 6 ships on
  11-05 / 11-06 / 11-07 plus ~12 catch-loop restarts today. A constant per-start rate looks like "new" crashes. E0 measures it.
- **Observer effect:** 12:22–12:39 my loop restarted every ~45 s, ptrace-attached and SIGSTOP'd vic-anim. S2/S3 today may be induced.
- **Vanilla baseline is unrecorded:** robot journal starts at the WireOS image (10-25 clock), crashDumps start 11-03. No vanilla data.
- **Biggest single change is the OS, not our code:** WireOS 3.0.2.36d (`/etc/os-version`) replaced stock; S2's code path
  (TFLite GPU delegate) does not exist in stock vision.

## Change inventory (C) and suspicion

| id | change | process | S1 | S2 | S3 | elimination test |
|---|---|---|---|---|---|---|
| C1 | WireOS 3.0.2 replaces stock image (11-03) | all | M | **H** | M | official WireOS release, no patches (E6) |
| C2 | self-built binaries from `robot/work/wire-os-victor` (vic-anim built 11-06 21:03); deploy re-copies `lib/libc++.so.1` (same md5) | all | M | L | L | E6 (official binaries) |
| C3 | TFLite GPU delegate `coretech/vision/engine/tfliteModel.cpp` (`CreateGpuDelegate`, WireOS 2026-09), via `faceNet.cpp` `useGpu` | vic-engine | – | **H** | – | E1 |
| P1 | 0001 stay-on-charger (behavior) | engine | L | L | L | revert in E2 arm |
| P2 | 0002 lift-lock (actions, liftController) | engine, robot | L | L | L | ″ |
| P3 | 0003 quiet-idle (behavior JSON) | engine | L | L | L | ″ |
| P4 | 0004 quiet-winsock (CMake, civetweb) | build | L | L | L | ″ |
| P5 | 0005 motor-lock (hal_motors, wheelController) | vic-robot | L | L | L | ″ |
| P6 | 0006 drive-guard (basicActions) | engine | L | L | L | ″ |
| P7 | 0007 log-level-intent (shared `logging.cpp`, `iLoggerProvider.cpp`) | **every process** | M | M | L | ″ |
| P8 | 0008 speech-volume (`audioLayerManager`, `cozmoAudioController`) | audio | **H** | L | L | E2 audio arm |
| P9 | 0009 eye-chimes-off (`cozmoAudioController`) | audio | **H** | L | L | E2 audio arm |
| P10 | 0010 pc-voice (TTS behaviors, `messageEngineToRobot` CLAD) | engine↔anim | **H** | M | L | E2 audio arm |
| P11 | 0011 night-window (`sleepTracker`, `sleepingTypes.clad`) | engine | L | L | L | revert in E2 arm |
| C4 | `settings.sh`: master volume 1, tz, no-auto-update, T2 voice url | anim audio | M | L | L | E3 (default volume) |
| C5 | diskguard timer (only non-stock unit besides WireOS's) | – | L | L | L | `systemctl stop myvector-diskguard.timer` |
| C6 | server fork: pc-voice TTS push, jdocs/settings writes, intents | anim audio, gateway | M | L | L | E5 (vanilla wire-pod) |
| C7 | my instrumentation (gdb attach, SIGSTOP, 45 s restarts) | anim | – | M | **H** | E0 (no gdb) |
| C8 | /data full on 11-05 (log capture runaway) | all | – | – | – | fixed by diskguard 11-06; `df -h /data` = 5 % now |

## Elimination stages (in order; stop when a stage explains a symptom)

**E0 — measure exposure, no instrumentation (S2, S3, S5).** Read the 4 smoke-FAIL logs → map each to S1/S2/S3.
Then 5 plain restarts (user ruling) via `robot/crash-review/restarts.sh` (stop, 5 s, start; no gdb; 60 s watch), log per round: S1/S2/S3 + new crashDumps.
**Result 13:52:** S1 0/5 S2 0/5 S3 0/5 (`robot/work/crash-review/restarts-20261107-1346*`); every S hit = a deploy or gdb attached.
Smoke FAILs: 1105-204236 + 1106-071657 = lift calibration, 1107-114118 = gateway refused pre-deploy, 1107-115312 = S1.

**E1 — GPU delegate (S2).** First: `ls -l /lib/libOpenCL.so` (what `ShouldUseGpu` checks) and WireOS upstream log of `tfliteModel.cpp` for a fix.
User ruling: keep the GPU (Adreno 304 + Qualcomm OpenCL is real; GPU init ok on 15 of 17 starts) — no `useGpu=false` patch.
Re-open only on S2 without gdb/fast restarts: then look for an upstream WireOS fix of `tfliteModel.cpp` first.

**E2 — SeAssert (S1), only with a hit or a repro.** Rate ~2 per 20 deploys, so bisecting blind costs ~20 deploys per arm.
Step 1 (user OK'd 2026-11-07): `robot/seassert/catch.sh -d` 5 deploy rounds (stops at first hit) — the reading picks corruption vs register (seassert plan §Plan 4).
A gdb-round S2/S3 counts as observer-induced (E0), never as a deploy finding.
Step 2 if corruption: build with P8+P9+P10 reverted (audio arm) → 20 deploys; S1 0/20 ⇒ the writer is in those patches → bisect the 3.

**E3 — volume (S1).** Same 20-deploy count at default master volume (settings.sh volume step skipped). Only if E2 points at SE inputs, not corruption.

**E4 — exposure check.** Per day: starts vs crashes (ship logs + catch logs + crashDumps). Constant per-start rate ⇒ the "new errors" are frequency of restarts, not new bugs.

**E5 — server (C6).** One day on upstream wire-pod (`git stash`-free: build `upstream/main` in `robot/work/`). Only if S1 persists after E2 and correlates with voice/TTS use.

**E6 — official WireOS release, no patches (C1, C2).** Control arm for everything; costs a reflash and losing patches for the test. Only if E1/E2 inconclusive.

## Stopping rules

- Each stage pre-registers N and its pass rule above; stop at the first stage that explains a symptom, fix, then re-run E0's count.
- Robot audio: every restart plays the power-on sound — accepted by the user for C-hunts; ask before any other audio.
- The user runs `make ship`; Claude may run `deploy-v.sh` only in E2 rounds the user OK'd.
