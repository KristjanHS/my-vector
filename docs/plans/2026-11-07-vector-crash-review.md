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

vic-switchboard SIGQUIT / vic-cloud SIGTERM = fault-handler cascade, not causes.
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
