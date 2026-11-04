# Architecture refactors

**Goal**: pay down the fork's structural debt before the next feature lands on top of it. **Scope**: fork-owned code
only (`chipper/pkg/myvector/`, `fork_*.go`, the allowed upstream edits, `scripts/`, `robot/`, `Makefile`); upstream
wire-pod internals stay as they are. **Status**: audit 2026-11-04 at `709ba96`; R2 + R3 + R8 + K4 + R6 shipped (patch `series`, `my-vector` commits, `robot/firmware/`, `settings.sh`, `robot/env.sh`). Ranked by
risk removed per hour of work.

## Recommendations

| id | ruling | edit site | falsifier |
|---|---|---|---|
| R6 | ESN, IP, SERVER and key path live in gitignored `robot/robot.env` (template `robot.env.example`), read by `robot/env.sh`, which every robot script sources; a missing fact → SETUP exit 2; `ESN`/`SERVER` env vars override. The key stays the clone's tracked `robot_sshkey` (upstream `971632fc`); `ship.sh` writes the clone's `robot_ip.txt` from env.sh before deploy | `robot/{env.sh,robot.env.example,.gitignore}`, `robot/*/*.sh`, `scripts/check.sh` | `git check-ignore robot/robot.env` → ignored; `grep -rn '8080\|robot_ip.txt\|robot-id-\|robot_sshkey' robot/*/*.sh` → no hits |
| R7 | The victor pin and the robot's compat version are tied only by a comment in `apply.sh`; `deploy-v.sh` refuses a mismatch only after the full build. Store the expected compat (209) next to `pin` and have `ship.sh` compare it to the robot's `/etc/victor-compat-version` over SSH before the build step | `robot/firmware/{apply.sh,ship.sh}` | set the expected compat to 999 → `make ship` dies before `build-v.sh` starts |

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

## Order

R7.
