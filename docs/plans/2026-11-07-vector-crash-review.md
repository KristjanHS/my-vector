# Vector crash review — every change we make, eliminated one by one

Status: **stub** — evidence so far; review + elimination order not yet written (user: doc only, don't run it).

## Crashes 2026-11-05..07 (robot crashDumps + journal)

| time | process | signature | context |
|---|---|---|---|
| 11-05 09:22 | vic-anim | SeAssert nrgainv.c:191, SIGILL | ship deploy |
| 11-07 11:54 | vic-anim | same | ship deploy b70e53a (smoke step) |
| 11-07 12:31:28 | vic-engine | SIGSEGV | catch.sh restart round 5 (observer?) |
| 11-07 12:33:13 | vic-anim | start timeout → fault 800 | catch.sh round 6, no gdb on that pid |
| 11-07 12:37:02 | vic-engine | SIGSEGV → fault 914 | catch.sh -d deploy round 1 |

Complete crash dumps (`crashDumps/*.dmp` + `.log`) since unlock — the organic set:

| dump time | process | context |
|---|---|---|
| 11-04 12:39:51 | vic-engine | ? |
| 11-05 09:22:33 | vic-anim | SeAssert, ship deploy |
| 11-06 05:18:19 | vic-engine | **night idle — no deploy** |
| 11-06 19:15:36 | vic-switchboard | ? |
| 11-07 11:54:29 | vic-anim | SeAssert, ship deploy |
| 11-07 12:31:26 / 12:36:59 | vic-engine | catch loop / deploy round |
| 11-07 12:33:14 | vic-switchboard | fault-800 cascade (SIGQUIT) |

`.dmp~` (incomplete) dumps come in 4-process groups (vic-robot/anim/engine/switchboard within 2 s): 11-03 21:21, 21:30;
11-04 07:13, 08:49, 19:39; 11-05 10:43, 18:27; 11-06 19:22; 11-07 10:09, 11:54:58, 12:37:17 = whole-stack kills (fault handler or deploy stop?).
vic-switchboard SIGQUIT / vic-cloud SIGTERM = fault-handler cascade, not causes.

Dump `.log` reads (robot `crashDumps/*.dmp.log`, tail of syslog at dump time):
- 11-07 12:31:28 + 12:37:02 vic-engine SIGSEGV, both minidumps identical (`md.py`, copies in `robot/work/crash-review/`):
  pc = 0 (NULL call), lr = `libOpenCL.so+0xa480`, stack → `tflite::gpu::cl::CreateDefaultGPUDevice+52` (libcozmo_engine.so+0x7747b4).
  ⇒ TFLite GPU-delegate init calls an unresolved OpenCL entry in stock `/usr/lib/libOpenCL.so` (78 KB, 2011). Not our patch code.
  Source: upstream WireOS `coretech/vision/engine/tfliteModel.cpp` (`CreateGpuDelegate`, added 2026-09 by WireOS author), gated by
  `TfliteConfig.useGpu` set in `faceNet.cpp`. Elimination test: force `useGpu=false` (patch) → vic-engine SIGSEGV gone across N restarts.
- 11-06 05:18 vic-engine dump is all zeros (written while /data was full) — unreadable.
- Both came ~3–7 s after a stop→start (12:31:19 stop; 12:36:46 deploy stop): crash in the new instance's startup or the old one's shutdown — unresolved.
- 11-04 12:39 vic-engine and 11-06 19:15 vic-switchboard logs show only systemd SIGTERM/SIGKILL shutdown lines: likely stop-time dumps, not organic.
- 11-06 05:18 vic-engine (night idle): its log ends with syslog-ng `No space left on device (28)` from 11-05 21:08 — /data full
  is a suspect for that crash (diskguard came after; check `git log -- robot/diskguard`).
Observer effect: 12:22–12:39 the catch loop restarted every ~45 s, ptrace-attached and SIGSTOP'd vic-anim.

## Change surface to review

- Self-built WireOS (`robot/work/wire-os-victor`, vic-anim built 11-06 21:03); every deploy re-copies `lib/libc++.so.1`.
- Firmware patches 0001–0011 (`robot/firmware/series`); `settings.sh` (volume, tz, no-auto-update, T2 voice, diskguard).
- Robot tools `robot/*/` (diskguard, logkeep, openmic, wifi, et-stt, voice push); server-side fork (`chipper/pkg/myvector/`).
- Baseline "vanilla had no crashes": no crash-dump evidence from the vanilla period checked yet.

## Next

1. Catalogue all crashDumps since 11-03 with context tags (ship / idle / observer); symbolize vic-engine dumps (`robot/seassert/md.py`).
2. Per change: mechanism → which crash it could cause → cheapest elimination test; order by cost.
3. Candidate first test: official WireOS release without our patches/self-built bins, idle + deploys.

Tooling: `robot/seassert/catch.sh [-d] N` (gdb catcher, works; history in commits 5b31b99, 5859d4a).
