# make syncwt — speed plan, 2026-11-09

Goal: cut the wall-clock time of `make syncwt` (rebase the worktree onto `main` → gate → ff-land).
Builds on [2026-11-08-ship-speed.md](2026-11-08-ship-speed.md): its B1, E2–E4, R6 and R7 are the same
levers seen from the syncwt side; its Rung 1 A3 (check pass stamp) is already in `scripts/check.sh`.
Plan only; nothing implemented.

## 1. Where the time goes (measured 2026-11-09, worktree `sec`, 10 cores, 49 GB)

`~/bin/syncwt` (dotfiles) does recon + pre-flight (<1 s), rebase (~1 s), **the gate `make check`**, then
an ff-only merge in the primary (<1 s). If `main` moved during the gate it re-rebases and **runs the
gate again** (up to 2 more times). So syncwt time ≈ gate × (1 + re-runs).

Gate = `scripts/check.sh` in working-tree mode: **76 s, fully serial**. Working-tree mode runs in the
worktree itself, so Go already hits its build/test cache (all Go ≈ 4.5 s, `(cached)`): ship-speed E1
does not apply here.

| Test | s | What it spends the time on |
|---|---:|---|
| `robot/firmware/tests/mic-stream.sh` | 14.4 | **11 s is the test binary waiting on real backoff timers** (1 s → 2 s → 4 s redial + 1.3 s `kMyVectorMicHealthyS` sleep); 2 s jsoncpp compile, 1 s test compile |
| `robot/hostkey/tests/apply.sh` | 13.0 | `sleep 4` (waits out the 3 s confirm windows), RSA/ECDSA/ed25519 `ssh-keygen` fixtures, ~1 s per fake-robot apply run |
| `robot/firmware/check-static.sh` | 1.8–8.4 | cold vs warm page cache on the victor clone (`git show`/`git grep`) |
| `robot/quiet/tests/guard.sh` | 7.5 | **6.6 s is one case**: `volume-probe/sweep.sh` under a fake curl still runs its real `sleep 2.5` per event |
| `robot/firmware/tests/pc-voice.sh` | 5.9 | 2 s jsoncpp compile (again), 1.1 s test compile, 1.5 s test run |
| tts pytest | 5.8 | |
| stt pytest | 4.0 | |
| `robot/sshkey/tests/apply.sh` | 3.8 | |
| `robot/lib/tests/etc.sh` | 2.5 | |
| `robot/netguard/tests/apply.sh` + `hosts.sh` | 3.4 | |
| all Go (vet + 6 `go test`) | 4.5 | cached |
| ~12 others (drift, syntax, nft, nap, silent-start, scan, listen, secrets…) | < 1 each, ≈ 4 | |

Reproduce: `PS4='+${BASH_SOURCE##*/}:$EPOCHREALTIME ' bash -x scripts/check.sh 2>trace`, then diff
consecutive timestamps of the `+check.sh:` lines.

Two facts that shape the plan:
- **Every test is independent** (each makes its own `mktemp` dir under `robot/work/`, none touches the
  robot or shared state) → they can all run at once. Critical path = the slowest test (mic-stream, 14 s).
- **`make check` never uses the pass stamp.** Only `check.sh --head` (ship's mode) skips on a stamped
  tree. syncwt calls plain `make check`, so a tree the session already checked before committing
  (the run-make-check-before-finishing habit) is re-checked in full whenever no rebase was needed.

## 2. Proposals

IDs for picking. Savings per syncwt run. "Rebased" = `main` moved since the branch forked, so the
tree after rebase is new and no stamp can match it.

### S1 — Stamp hit in working-tree mode too (project, `check.sh`)
When the working tree is clean and its tree id has a pass stamp, plain `check.sh` does what `--head`
does on a hit: run drift + secrets (the commit-range checks), print `check skipped`, exit 0.
Clean tree = `wt_tree == HEAD^{tree}`; dirty trees keep running in full (so `make check` mid-edit is
unchanged). syncwt already refuses a dirty tree, so its gate always qualifies.
- Save: **76 s → ~2 s** when no rebase was needed (branch already on top of `main`).
- Risk: a flaky or time-dependent test that passed once stays "passed" for that tree. Same exposure
  `make ship` already accepts. `CHECK_FULL=1` forces a full run.
- Falsifier: commit, `make check`, `make check` again → second prints `check skipped`; touch a tracked
  file → full run.

### S2 — Run the checks in parallel (project, `check.sh`) — ship-speed B1
Run every test as a background job with its own log under one `mktemp` dir; wait for all; print each
job's verdict line in a fixed order; on any failure print that job's full log and exit 1. Write the
pass stamp only after all are green (unchanged). Bash 5.1 is installed, so `wait -n -p` gives the
finished job's pid without polling.
- Save: **76 s → ~15–17 s** (mic-stream's 14.4 s + startup). 10 cores and the biggest jobs are
  single-threaded, so CPU contention is small; measure, don't assume.
- Keep cheap gates first and serial: drift + shell syntax (< 1.5 s) fail before anything starts.
- Fail fast: on the first failure, kill the rest (`kill` the recorded pids) — a red gate then reports
  in seconds instead of after the slowest job.
- Output: today's output is a stream; parallel output must not interleave. Per-job logs + an
  ordered summary (`ok   mic-stream   14.4 s`) also give per-test timings for free on every run.
- Ship benefits too: a stamp miss in `ship.sh`'s `check.sh --head` drops from ~70 s the same way.

### S3 — Make the slow tests stop waiting on wall-clock (project, tests + 1 patch)
With S2 the gate is only as fast as its slowest test; these cut the top of the list.
- **S3a mic-stream (14.4 → ~3 s).** The test sleeps through real backoffs. Give patch 0016's timing
  constants a test seam (e.g. `#ifndef MYVECTOR_MIC_BACKOFF_SCALE` → test compiles with 1/10 scale)
  and scale the test's waits to match. Touches a firmware patch → the next ship takes the firmware
  lane once. Check the seam compiles out (check-static or `nm` on the built lib).
- **S3b hostkey (13 → ~4 s).** Make the 3 s confirm window an env knob the test sets to 0.3 s
  (`sleep 4` → `sleep 0.5`), and cache the RSA/ECDSA/ed25519 fixture keys in a gitignored
  `robot/work/hostcache/` (ship-speed E3; `origin` is public, so no committed keys).
- **S3c guard.sh sweep case (7.5 → ~1 s).** `sweep.sh`'s `sleep 2.5` per event becomes `${EV_GAP:-2.5}`;
  the guard test passes `EV_GAP=0`.
- **S3d jsoncpp once (−2 s each in mic-stream and pc-voice).** Compile `jsoncpp.cpp` once into
  `robot/work/hostcache/jsoncpp-<clone HEAD>.o` under `flock` (ship-speed E2). With S2 both run at
  once, so the cache matters mostly for CPU, less for wall time.
- After S3a–c the critical path is pytest (~6 s) → gate ≈ **7–9 s**.

### S4 — Per-test input stamps (project, `check.sh`) — ship-speed E4 / R6
Needed for the rebased case, where S1 never hits. Each test declares its input paths; its stamp key =
`git rev-parse HEAD:<path>` tree ids of those paths + the toolchain part of `stamp_key`. A test whose
key has a pass stamp is skipped. Rebasing onto a `main` that only moved `chipper/` leaves every
`robot/` test a hit.
- Save: rebased runs → **~2–6 s** when `main`'s new commits don't touch the branch's test inputs
  (most sibling-session landings touch one lane).
- Risk: a wrong input list skips a test that should have run. Mitigation: the input list is the
  test's directory + the code it sources (`robot/lib`, `robot/env.sh`); a missing input only costs
  a stale skip until the full-tree stamp misses on ship. Go is excluded (its own cache already does this).
- Best done as ship-speed R6: one table `name | inputs | command` driving S2 and S4, instead of the
  hand-written `if [[ -f … ]]` lines.

### S5 — Gate already done before syncwt runs (project, opt-in) — ship-speed R7
After a commit in a worktree, start `make check` in the background (a `make prebuild` the session
runs, or a post-commit hook). By the time the user types `make syncwt`, S1's stamp exists and a
no-rebase sync is ~2 s. Parallel sessions run checks at once: S2 jobs use unique temp dirs already;
the S3d cache needs `flock`.
- Open question: hook (automatic, every commit, burns CPU on WIP commits) vs explicit target.

### S6 — syncwt itself (dotfiles `~/bin/syncwt`, cross-project)
Small, but a different repo: do these from a dotfiles session (CLAUDE.md cross-project rule).
- **S6a Time the phases.** Print `gate passed (NN s)` and a total in the report. Needed to measure
  every rung above; costs nothing.
- **S6b Re-gate is the multiplier.** When `main` moves mid-gate, syncwt re-runs the full gate (up to
  3× total). Nothing to change in syncwt: S2 makes each pass ~5× shorter (fewer moves land inside the
  window) and S4 makes the re-pass mostly hits. Do not skip the re-gate — the rebased tree is new code.

## 3. Projected

| syncwt case | Today | S1 | + S2 | + S3 | + S4 |
|---|---:|---:|---:|---:|---:|
| no rebase, tree checked before commit | 76 s | **~2 s** | ~2 s | ~2 s | ~2 s |
| no rebase, tree never checked | 76 s | 76 s | **~16 s** | ~8 s | ~8 s |
| rebased onto a moved `main` | 76 s | 76 s | ~16 s | ~8 s | **~3–6 s** |
| `main` moved during the gate (re-gate) | 152–228 s | same | ~32–48 s | ~16–24 s | ~8–12 s |

`make ship` gains the same on a stamp miss (its `check.sh --head` shares the code).

## 4. First rung (thinnest visible slice)

**S1 + S2 + S6a.** S1 and S2 are both in `scripts/check.sh` and independent of each other; S6a is
three lines in dotfiles. The very next `make syncwt` shows either `check skipped` (~2 s) or a
parallel gate (~16 s) with a per-test timing table. S3a–d, S4 and S5 stay an unordered backlog,
re-picked after the rung is measured. S3c and S3b are the cheapest of them (env knobs, no patch).

| id | ruling (qimpag 2026-11-09) | edit site |
|---|---|---|
| R1 | S2's runner is a sourceable lib with its own fake-job test (pass/fail/slow → rc, log, kill, order) | `scripts/lib/check-runner.sh`, `scripts/tests/check-runner.sh` |
| R2 | S6a lands from this session, dotfiles commit pathspec'd to the one file | dotfiles `bin/bin/syncwt` `run_gate` + report |

### Pre-registered check
- S1: the falsifier in §S1 (second `make check` on a clean committed tree prints `check skipped`; a
  touched file runs in full).
- S2: `time make check` on a clean tree with `CHECK_FULL=1`, 3 runs: median ≤ 20 s passes, > 30 s
  means contention or a serial dependency — stop and find it before S3. Also break one test on
  purpose (e.g. `exit 1` in a copy) and see the gate fail with that test's log printed — the
  parallel runner must be able to fail.
- Stop rule: once a no-rebase syncwt is ≤ 3 s and a rebased one ≤ 20 s, S3/S4 are picked only if
  re-gates show up in practice.
