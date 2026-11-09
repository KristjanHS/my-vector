# Robot crash debugging — runbook

Order: catalogue → plain restarts → no-gdb deploys → gdb only once a crash reproduces without it.

| step | command | gives |
|---|---|---|
| 1 catalogue | `robot/crash-review/crashes.sh [-p] ['2026-11-07 12:00']` | real dumps + journal crash lines since then; `-p` copies dumps |
| 2 restarts | `robot/crash-review/restarts.sh [N]` | per-start crash count, no gdb (stop, 5 s, start; 60 s watch) |
| 3 deploys | `robot/crash-review/restarts.sh -d [N]` | per-deploy crash count, no gdb (deploy-v.sh + settings.sh) |
| 4 gdb catch | `robot/seassert/catch.sh [-d] [N]` | registers at vic-anim SeAssert `nrgainv.c:191`; stops at first hit |
| dump reader | `robot/seassert/md.py <file.dmp> [addr:len …]` | exception registers, module bases, memory |

## Rules

- Never count a crash from a gdb round: attach/SIGSTOP + fast restarts caused vic-engine GPU-init SIGSEGV (fault 914) and vic-anim start timeouts (fault 800).
- `.dmp` + `.dmp.log` = real crash; `.dmp~` groups = stop-cascade snapshots; "Displaying a fault code in 5.0 seconds" = normal stop notice.
- Two fault restarts within 97 s escalate the fault handler — the scripts wait 100 s after one.
- Every round plays the power-on sound. Claude runs deploy rounds only after the user OKs them, and `make ship` only when the user asks for it.
- `-d` modes deploy the clone's build and refuse an off-tip, dirty or stale one: `robot/firmware/apply.sh`, then `./build/build-v.sh` in `robot/work/wire-os-victor`.
- Plain `catch.sh` needs the robot already on that build (vic-anim md5 = the clone's).

## Setup

Host: `gdb-multiarch` (apt). Robot: `gdbserver` ships in the WireOS image. Outputs: gitignored `robot/work/crash-review/`, `robot/work/seassert/`.
Last hunt: `2026-11-07-vector-crash-review.md (docs/plans/)`; SeAssert root cause + fix (patch 0012): `2026-11-07-vic-anim-seassert.md (docs/plans/archive/)`.
