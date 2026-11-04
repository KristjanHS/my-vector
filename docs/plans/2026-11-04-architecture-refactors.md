# Architecture refactors: recommended, not started

**Goal**: pay down the fork's structural debt before the next feature lands on top of it. **Scope**: fork-owned code
only (`chipper/pkg/myvector/`, `fork_*.go`, the allowed upstream edits, `scripts/`, `robot/`, `Makefile`); upstream
wire-pod internals stay as they are. **Status**: audit 2026-11-04 at `709ba96`; nothing implemented. Ranked by
risk removed per hour of work.

## Recommendations

| id | ruling | edit site | falsifier |
|---|---|---|---|
| R1 | `make check` never compiles the fork glue: `go test ./pkg/myvector/...` skips `pkg/wirepod/sdkapp`, so an upstream merge that renames `getRobot`, `robots` or `ConnTimer` breaks `fork_autohold.go` and still passes. Add `go vet ./pkg/wirepod/sdkapp/` (runs clean today) and `-race` to the myvector tests | `scripts/check.sh` | rename `ConnTimer` in `resetConnTimer` → `make check` exits non-zero |
| R2 | The patch list lives twice: the `for patch in …` loop in `apply.sh` and `patches=(…)` in `ship.sh`. Put one name per line in a `series` file that both read | `robot/stay-on-charger/{series,apply.sh,ship.sh}` | `grep -c 'quiet-idle.patch' robot/stay-on-charger/*.sh` → 0 |
| R3 | Keep the patches as commits, not loose diffs: `apply.sh` = `git checkout -B my-vector <pin>` + `git am` the series; refresh = `git format-patch <pin>..my-vector`; `tree_is_patches` = `git diff --quiet my-vector` + no untracked files. Unblocks two patches editing the same file (none overlap today) and replaces hand-exporting one patch out of a tree with all of them applied | `robot/stay-on-charger/{apply.sh,ship.sh,*.patch}` | `apply.sh` twice in a row on a clean clone → second run exits 0 with no change; `make ship NO_DEPLOY=1` green |
| R4 | Two layers own "stay docked": firmware `stay-on-charger.patch` and server-side `autohold` (`AUTO_HOLD`). Rule the owner once the soak verdict lands: PASS → delete `chipper/pkg/myvector/autohold/`, `fork_autohold.go` and the `InitAutoHold()` line in `server.go` (keep the `bcassume.go` busy-spin fix); FAIL → autohold stays as the documented fallback | `chipper/pkg/myvector/autohold/`, `chipper/pkg/wirepod/sdkapp/{fork_autohold.go,server.go}`, `scripts/upstream-drift.sh` | after the delete: `grep -rn AUTO_HOLD chipper/` → empty, `scripts/upstream-drift.sh` green |
| R5 | Run the backlog's auto-hold live check only after the soak is judged: `AUTO_HOLD=override` stops him from driving off, so a soak running under it PASSes no matter what the firmware does | `docs/backlog.md` §OWED[USER] (order of the two lines) | — (ordering only) |
| R6 | Robot connection facts sit in four places: IP and SSH key inside the third-party clone (`robot/work/wire-os-victor/robot_{ip.txt,sshkey}`, which `deploy-v.sh` needs), ESN from `robot/backup/robot-id-*.txt`, server URL hardcoded in `soak.sh` and `power-stress.sh`. Move them to one gitignored `robot/robot.env` (ESN, IP, key path, SERVER) that the scripts source and `apply.sh` copies into the clone. Until then a re-clone on a pin bump deletes the only plain copy of the SSH key found outside the backup tarballs | `robot/robot.env` (new, gitignored), `robot/*/*.sh`, `robot/.gitignore` | `git check-ignore robot/robot.env` → ignored; `grep -rn '8080\|robot_ip.txt' robot/*/*.sh` hits only the env loader |
| R7 | The victor pin and the robot's compat version are tied only by a comment in `apply.sh`; `deploy-v.sh` refuses a mismatch only after the full build. Store the expected compat (209) next to `pin` and have `ship.sh` compare it to the robot's `/etc/victor-compat-version` over SSH before the build step | `robot/stay-on-charger/{apply.sh,ship.sh}` | set the expected compat to 999 → `make ship` dies before `build-v.sh` starts |
| R8 | `robot/stay-on-charger/` now holds the patch set for three features (stay-docked, lift-lock, quiet-idle) plus ship and soak. Rename it to `robot/firmware/` in the same commit as R2/R3 so the paths change once. Archived plans keep the old path (read-only) | `robot/stay-on-charger/` → `robot/firmware/`, `Makefile` `ship`, `scripts/check.sh` glob, `docs/backlog.md`, live plans | `make check && make ship NO_DEPLOY=1` green; `grep -rn stay-on-charger/ Makefile scripts docs/backlog.md docs/plans/*.md` → empty |

## Order

R1 → R5 (minutes each, no robot needed) → R4 after the soak verdict → R2 + R3 + R8 as one slice → R6 → R7.
