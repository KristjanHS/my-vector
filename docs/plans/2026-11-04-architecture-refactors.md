# Architecture refactors: recommended, not started

**Goal**: pay down the fork's structural debt before the next feature lands on top of it. **Scope**: fork-owned code
only (`chipper/pkg/myvector/`, `fork_*.go`, the allowed upstream edits, `scripts/`, `robot/`, `Makefile`); upstream
wire-pod internals stay as they are. **Status**: audit 2026-11-04 at `709ba96`; nothing implemented. Ranked by
risk removed per hour of work.

## Recommendations

| id | ruling | edit site | falsifier |
|---|---|---|---|
| R2 | The patch list lives twice: the `for patch in …` loop in `apply.sh` and `patches=(…)` in `ship.sh`. Put one name per line in a `series` file that both read | `robot/stay-on-charger/{series,apply.sh,ship.sh}` | `grep -c 'quiet-idle.patch' robot/stay-on-charger/*.sh` → 0 |
| R3 | Keep the patches as commits, not loose diffs: `apply.sh` = `git checkout -B my-vector <pin>` + `git am` the series; refresh = `git format-patch <pin>..my-vector`; `tree_is_patches` = `git diff --quiet my-vector` + no untracked files. Unblocks two patches editing the same file (none overlap today) and replaces hand-exporting one patch out of a tree with all of them applied. Skip the reset when `my-vector` already equals pin + series, or every ship rewrites the patched files and recompiles them | `robot/stay-on-charger/{apply.sh,ship.sh,*.patch}` | `apply.sh` twice in a row on a clean clone → second run exits 0 with no change; `make ship NO_DEPLOY=1` green |
| R6 | Robot connection facts sit in four places: IP and SSH key inside the third-party clone (`robot/work/wire-os-victor/robot_{ip.txt,sshkey}`, which `deploy-v.sh` needs), ESN from `robot/backup/robot-id-*.txt`, server URL hardcoded in `soak.sh` and `power-stress.sh`. Move them to one gitignored `robot/robot.env` (ESN, IP, key path, SERVER) that the scripts source and `apply.sh` copies into the clone. Until then a re-clone on a pin bump deletes the only plain copy of the SSH key found outside the backup tarballs | `robot/robot.env` (new, gitignored), `robot/*/*.sh`, `robot/.gitignore` | `git check-ignore robot/robot.env` → ignored; `grep -rn '8080\|robot_ip.txt' robot/*/*.sh` hits only the env loader |
| R7 | The victor pin and the robot's compat version are tied only by a comment in `apply.sh`; `deploy-v.sh` refuses a mismatch only after the full build. Store the expected compat (209) next to `pin` and have `ship.sh` compare it to the robot's `/etc/victor-compat-version` over SSH before the build step | `robot/stay-on-charger/{apply.sh,ship.sh}` | set the expected compat to 999 → `make ship` dies before `build-v.sh` starts |
| R8 | `robot/stay-on-charger/` now holds the patch set for three features (stay-docked, lift-lock, quiet-idle) plus ship and soak. Rename it to `robot/firmware/` in the same commit as R2/R3 so the paths change once. Archived plans keep the old path (read-only) | `robot/stay-on-charger/` → `robot/firmware/`, `Makefile` `ship`, `scripts/check.sh` glob, `docs/backlog.md`, live plans | `make check && make ship NO_DEPLOY=1` green; `grep -rn stay-on-charger/ Makefile scripts docs/backlog.md docs/plans/*.md` → empty |

## Value stream: patch → soak verdict (kaizen VSM, 2026-11-04)

| step | owner | time | source |
|---|---|---|---|
| edit patch in the clone, export, commit, review | assistant | ~5–30 min | commit times `256710d`→`5db5e9c`, `717da29`→`8363175` |
| `make syncwt` + `make ship` | USER (hand-off) | waits for the user | `robot/stay-on-charger/ship.sh` |
| build | ship | cold 21.5 min / incremental 1.5 min | `robot/work/build-v.log`, `build-lift-lock.log` |
| deploy, services up, push | ship | ≤3 min wait | `ship.sh` `all_up` |
| restart soak + repoint `docs/backlog.md` line | USER (hand-off) | manual | ship's last `echo` |
| soak | waiting | 24 h | `soak.sh` default `hours` |
| `soak.sh --summary` + voice/logcat live checks | USER (hand-off) | manual | `docs/backlog.md` §OWED[USER] |

Value-add ≈ 10–55 min of a ≥ 25 h lead time (≤ 4 %). The 24 h soak is the bottleneck, and three features (stay-docked,
lift-lock, quiet-idle) are queued behind one robot and one soak.

| id | ruling | edit site | falsifier |
|---|---|---|---|
| K1 | A PC-side poller can lose a whole soak: `soak-2026-11-04-072053.log` has no lines from 07:25:52 to 07:55:49 while the poller stayed alive (cause unverified; host or WSL sleep would fit), so it was restarted at 08:12. Move the logger onto the robot so a PC sleep can't cost a day | `robot/stay-on-charger/soak.sh`; Windows power plan | `soak.sh --summary <log>` prints max gap ≤ 900 s |
| K2 | A soak log doesn't record which firmware it judges, and each ship restarts the 24 h clock by hand. `ship.sh` writes the shipped full SHA + series to `robot/work/shipped` and restarts the soak itself (pidfile); `soak.sh` stamps `# sha=` in the header and ends INCOMPLETE if `shipped` changes mid-run. That removes the "repoint the soak line" step, since `--summary` already defaults to the newest log | `robot/stay-on-charger/{ship.sh,soak.sh}`, `docs/backlog.md` soak line | ship twice during one soak → the first log's summary is INCOMPLETE and names both SHAs |
| K3 | The live checks are hand-typed: logcat greps in `docs/backlog.md`, and Stage 1's count is prose in the quiet-idle plan (`\x1f` field split). Script them as `robot/stay-on-charger/verify.sh` (SSH counts of `TracksLocked`, `calibrat`, `RTS_OnCharger_*`, `ListeningForBeats` over a window) and run it from `ship.sh` after `all_up`; only the by-ear checks stay with the user | `robot/stay-on-charger/verify.sh` (new), `ship.sh`, `docs/backlog.md` §OWED[USER] | inject a `RTS_OnCharger` hit (or run against the unpatched backup) → `verify.sh` exits non-zero |
| K4 | Runtime robot settings live only on the robot: volume LOW=1 (quiet-idle S2, set by a one-off `curl`), the no-auto-update setting, SSH setup. A re-onboard or factory reset loses them silently. Add an idempotent `robot/stay-on-charger/settings.sh` that `ship.sh` runs after deploy | `robot/stay-on-charger/settings.sh` (new), `ship.sh` | set the volume to 4 by hand → the next `make ship` puts it back to 1 and logs it |

## Order

R2 + R3 + R8 as one slice → K2 + K3 on the renamed folder → K4 → R6 → R7.
