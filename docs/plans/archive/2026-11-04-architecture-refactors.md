# Architecture refactors

**Goal**: pay down the fork's structural debt before the next feature lands on top of it. **Scope**: fork-owned code
only (`chipper/pkg/myvector/`, `fork_*.go`, the allowed upstream edits, `scripts/`, `robot/`, `Makefile`); upstream
wire-pod internals stay as they are. **Status**: ✅ SHIPPED 2026-11-04 — R2 + R3 + R8 + K4 + R6 + R7 (patch `series`, `my-vector` commits, `robot/firmware/`, `settings.sh`, `robot/env.sh`, `compat.sh`). Ranked by
risk removed per hour of work.

## Recommendations

| id | ruling | edit site | falsifier |
|---|---|---|---|
| R6 | ESN, IP, SERVER and key path live in gitignored `robot/robot.env` (template `robot.env.example`), read by `robot/env.sh`, which every robot script sources; a missing fact → SETUP exit 2; `ESN`/`SERVER` env vars override. The key stays the clone's tracked `robot_sshkey` (upstream `971632fc`); `ship.sh` writes the clone's `robot_ip.txt` from env.sh before deploy | `robot/{env.sh,robot.env.example,.gitignore}`, `robot/*/*.sh`, `scripts/check.sh` | `git check-ignore robot/robot.env` → ignored; `grep -rn '8080\|robot_ip.txt\|robot-id-\|robot_sshkey' robot/*/*.sh` → no hits |
| R7 | `compat.sh <src> <rev>` compares the clone's `VICTOR_COMPAT_VERSION` at `<rev>` (the file cmake reads; no stored copy) to the robot's `/etc/victor-compat-version`; `ship.sh` runs it on the tip before the build when deploying: mismatch → FAIL, SSH down → WARN and build | `robot/firmware/{compat.sh,ship.sh,apply.sh}` | `want=999` in compat.sh → exit 1 naming both values |

## Value stream: patch → live verdict (kaizen VSM, 2026-11-04)

| step | owner | time | source |
|---|---|---|---|
| edit patch in the clone, export, commit, review | assistant | ~5–30 min | commit times `256710d`→`5db5e9c`, `717da29`→`8363175` |
| `make syncwt` + `make ship` | USER (hand-off) | waits for the user | `robot/firmware/ship.sh` |
| build | ship | cold 21.5 min / incremental 1.5 min | `robot/work/build-v.log`, `build-lift-lock.log` |
| deploy, services up, push | ship | ≤3 min wait | `ship.sh` `all_up` |
| voice/logcat live checks | USER (hand-off) | manual | `docs/backlog.md` §OWED[USER] |

Value-add ≈ 10–55 min; the rest waits on user hand-offs (ship, live checks). No soak: a drive-off death is seen at once and
read from `/data/log/keep/messages*` (`robot/logkeep/apply.sh`).
