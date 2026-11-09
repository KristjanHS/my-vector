# make ship — speed audit (Kaizen VSM + Muda), 2026-11-08

Goal: cut the wall-clock time of every next `make ship` as far as possible. Audit only; nothing implemented.
Method: Value Stream Mapping of `robot/firmware/ship.sh` (steps, waits, handoffs), then Muda (waste) per step.
Evidence: the 21 kept ship logs, a live timestamped tail of ship `28a8cd3` (14:44:41 → 14:48:05), a
`bash -x` timestamp trace of `scripts/check.sh --head`, the wire-pod journal, `.ninja_log`, and SSH timings.

## 1. Current state (measured)

Typical full ship: **2m20s – 3m50s** (median ≈ 2m45s over 13 green runs). Run `28a8cd3` (server + robot-script change, no firmware change), ≈ 3m25s:

| # | Step | Time | Value-add for this commit? | Notes |
|---|------|-----:|---|---|
| 1 | pod-trust interlock | <1 s | yes (cheap) | one TLS probe |
| 2 | check HEAD (`check.sh --head`) | **≈ 65–70 s** | mostly no — `make check` already passed on this exact tree before commit | fully serial; Go tests in a fresh `mktemp` dir → no Go cache hits |
| 3 | apply patches | ≈ 2 s | no (series unchanged) | |
| 4 | robot keys (hostkey + sshkey apply) | ≈ 3–5 s | no (re-done in step 11) | ~10 SSH handshakes |
| 5 | compat + stt freshness | ≈ 1 s | no (pin unchanged) | |
| 6 | patched JSON parses | <1 s | no | |
| 7 | firmware build (`build-v.sh`) | **≈ 30 s** | **no** — ninja itself ran **1.4 s** | forced cmake reconfigure + metabuild + asset encode every run |
| 8 | check-static `--built` | ≈ 2 s | no | |
| 9 | server restart | ≈ 6 s | **yes** | Go build cached (5 s), :443 up |
| 10 | deploy (`deploy-v.sh`) | **≈ 30 s** | **no** — rsync sent only `lib/libc++.so.1` | stops + restarts `anki-robot.target` anyway → robot reboots its stack |
| 11 | robot settings | ≈ 5 s | yes (settings.sh changed) | re-runs hostkey + sshkey apply |
| 12 | smoke test | **≈ 58 s** | partly (wire-pod link + speech) | 15 s fixed stability sleep, 10 s speech, 9 s log intent, 8 s end sleep |
| 13 | push | ≈ 2 s | yes | |

Value-add time ≈ 6 (server) + 5 (settings) + ~10 (server-side smoke) + 2 (push) ≈ **25 s of ~205 s → ~12 % flow efficiency.**

### What the ships actually changed (last 18 ship-to-ship diffs)

| Lane | Ships touching it |
|---|---:|
| server (`chipper/`, firewall, launchers) | 17 / 18 |
| robot-side scripts (settings/sshkey/hostkey/epod/diskguard/lib) | 6 / 18 |
| **firmware** (`*.patch`, `series`, `apply.sh` pin) | **4 / 18** |
| docs/other only | 1 / 18 |

**78 % of ships rebuilt, redeployed and rebooted the robot stack for a firmware that had not changed.** That is the single biggest waste.

### check.sh breakdown (trace, ≈ 70 s, serial)

| Test | s |
|---|---:|
| `robot/firmware/tests/mic-stream.sh` (g++ jsoncpp + test) | 14.1 |
| `robot/hostkey/tests/apply.sh` (ssh-keygen RSA/ECDSA fixtures every run) | 12.5 |
| `go test ./pkg/myvector/...` | 9.0 |
| tts pytest | 5.9 |
| `robot/firmware/tests/pc-voice.sh` (g++ jsoncpp again + test) | 5.4 |
| stt pytest | 4.0 |
| `go test -run Fork ./pkg/wirepod/ttr/` | 3.7 |
| `robot/sshkey/tests/apply.sh` | 3.6 |
| `robot/lib/tests/etc.sh` | 2.6 |
| `go test ... ./pkg/wirepod/preqs/` | 2.6 |
| everything else (≈ 15 items) | < 2 each, ≈ 7 total |

### SSH cost

One robot SSH handshake = **0.65–0.72 s** (ARM CPU, RSA host keys). With `ControlMaster` reuse: **0.03–0.04 s**. A ship makes roughly 50–70 robot SSH connections (keys ×2, compat, settings, smoke, plus ~15 `robot_sh` calls inside `deploy.sh`) → **≈ 30–45 s of pure handshakes**.

## 2. Waste found (Muda)

- **Overproduction** — firmware build + deploy + robot stack restart + full robot smoke on 78 % of ships that changed no firmware (≈ 2 min each).
- **Over-processing** — `check.sh --head` re-runs the full check on a tree `make check` already passed; Go tests recompile every run (new `mktemp` path defeats the build/test cache, the same trap `run-server.sh` already documents and avoids); jsoncpp compiled twice per check; RSA fixture keys generated every check; hostkey + sshkey apply run twice per ship; `victor_build_release.sh` passes `-D…` flags, and in `build-victor.sh` any `-D` forces `CONFIGURE=1` → `rm -rf generated/cmake`, metabuild, full cmake configure (~10 s+) on every build.
- **Waiting** — fixed sleeps in smoke (15 s stability, 8 s end window, 5 s / 2 s poll intervals); every step strictly serial although check (local CPU), build (docker CPU) and robot pre-checks (network) are independent.
- **Transportation** — ~60 fresh SSH handshakes at ~0.7 s each; `deploy-v.sh` runs inside docker with its own ssh-agent.
- **Motion** — the robot stack restart forces smoke's 18–19 s "units up + stable" phase on every ship.
- **Defects** — 8 of 20 finished kept runs failed (3 at smoke after deploy, 2 at check, 1 build, 1 interlock, 1 uncommitted-server guard); a check failure costs up to ~70 s before it reports, and a smoke failure costs the whole ship.

## 3. Proposals — no refactor needed (scripts/config only)

IDs are for picking; savings are per ship, estimated from the measurements above. "Server-only ship" = the 78 % case.

### A. Skip what didn't change (largest wins)

- **A1 Change-scoped ship lanes.** Diff the shipped sha against the sha the robot/server last received (record it: robot `/data/myvector/shipped-sha` + local `robot/work/shipped-firmware-tip`) and run only the lanes touched:
  `firmware` (patches/series/pin) → build + deploy + full smoke; `robot-scripts` → settings.sh only; `server` → restart + server-side smoke; `docs` → check stamp + push.
  Save **≈ 120 s on a server-only ship**; docs-only ship → seconds. Risk: a robot left on a different firmware (reflash, manual deploy) — mitigate by reading the robot's `/anki/etc/revision` (or the marker) instead of trusting a local record; `SHIP_FULL=1` override.
- **A2 Skip the deploy when nothing differs.** Even within the firmware lane: `rsync --dry-run --itemize-changes` (or a manifest hash of `_build/.../dist` vs a hash stored on the robot) → if empty, don't stop/start `anki-robot.target`. Also find why `lib/libc++.so.1` is re-sent on unchanged firmware (seen in `28a8cd3` and `0580cf3`'s logs) (install step rewrites it → content or mtime churn) and stop it. Save ≈ 30 s deploy + ≈ 19 s smoke boot wait.
- **A3 Green-check stamp.** `make check` (and syncwt's gate) writes `robot/work/check-pass/<tree-sha>` on success; `ship.sh` skips step 2 when HEAD's tree id (`git rev-parse <sha>^{tree}`) has a stamp. Same tree = same inputs, so it is exact, not a heuristic (venvs/toolchain are outside the tree — key the stamp on their versions too, or expire stamps after N hours). Save **≈ 65 s** whenever check already ran (the normal flow, per the run-make-check-before-finishing habit).
- **A4 Skip the firmware build when the tip is already built.** Stamp `robot/work/firmware-built/<tip>` after a green `check-static --built`; if `tip` matches and `_build` is untouched, skip step 7–8. Save ≈ 32 s on firmware-lane-but-same-tip reruns (e.g. re-ship after a smoke flake).

### B. Run independent work in parallel

- **B1 Parallel check.sh.** The ~25 checks are independent; run them as background jobs (or `xargs -P`) with per-job logs, fail on any. Critical path becomes mic-stream (14 s) → **≈ 15–18 s** instead of 70 s. Combine with E1–E3 to get under 10 s.
- **B2 Overlap check with build + robot pre-checks.** Check (host CPU), apply + build (docker), keys + compat (network) don't depend on each other; the gate that matters is "nothing touches the robot's firmware before check is green". Start build and keys/compat while check runs; join before step 9. Save ≈ 30–40 s on firmware ships.
- **B3 Pre-build the server binary during check.** `run-server.sh`'s build logic as a standalone `server-build <sha>` called in the background at ship start; the restart then finds `server-build/<sha>/chipper` ready. Save ≈ 4–5 s and shortens the window the robot has no server.
- **B4 Robot settings in parallel with smoke's boot wait.** settings.sh (≈ 5 s, network) can run while the robot stack is coming up after deploy, as long as epod's trust rewrite (which restarts the target) is sequenced before smoke's PID baseline. Save ≈ 5 s.

### C. Cut waits and round trips

- **C1 SSH connection reuse.** Add `-o ControlMaster=auto -o ControlPath=$XDG_RUNTIME_DIR/mv-%C -o ControlPersist=120` to `ssh_base` in `robot/env.sh` (path must stay < 108 bytes — the scratchpad path was too long in the test); open the master once at ship start, `-O exit` in the EXIT trap. Measured 0.68 → 0.04 s per call → **≈ 30–40 s per firmware ship**, ≈ 10–15 s per server-only ship. For `deploy.sh`'s calls inside docker either mount the control socket or use the clone's existing `nodocker=1` branch of `deploy-v.sh` (rsync + ssh on the host).
- **C2 Drop smoke's fixed 15 s stability sleep.** The end-of-run `same_pids "during the run"` already proves the PIDs stayed stable across the whole ~40 s run; a crash loop also shows as `NRestarts`/`ActiveEnterTimestamp` changes. Keep a short (3–5 s) gap or replace with a `systemctl show -p NRestarts` compare. Save ≈ 10–15 s.
- **C3 Overlap smoke's 8 s end window.** Instead of `sleep 8` after release, release behavior control *before* A3 (the log-intent step already takes ~9 s and doesn't need control) and run the end log check afterwards. Save ≈ 8 s.
- **C4 Faster polls.** units-up poll 5 s → 1 s, `seen()` 2 s → 0.5 s, `conn_test`/`get_battery` 2 s → 0.5 s, `restart-service.sh` 1 s → 0.2 s. Save ≈ 3–6 s.
- **C5 Shorter speech phrase on ship.** The ear-test phrase takes ~10 s; keep it in `make robot-test`, use a 2–3 word phrase in ship (still proves T1 voice + FINISHED). Save ≈ 6–8 s.
- **C6 Don't apply hostkey/sshkey twice.** Step 4 already applied them; settings.sh re-runs both. Pass `SKIP_KEYS=1` from ship. Save ≈ 2–4 s (≈ 0.5 s with C1).
- **C7 Batch settings.sh reads.** Read the whole settings jdoc + flag + voice file in one SSH call, compare locally, only then set. Save ≈ 2–3 s (less with C1).

### D. Firmware build

- **D1 Stop the forced reconfigure.** `victor_build_release.sh` passes `-D…` → `build-victor.sh` sets `CONFIGURE=1` → `rm -rf generated/cmake` + metabuild + cmake configure every build. Call `build-victor.sh` without `-D` when `CMakeCache.txt` already holds those values (or carry a small series patch that only reconfigures when the defines differ from the cache). Save ≈ 10–15 s per build.
- **D2 Skip asset re-encode.** `fetch-build-deps.sh` re-encodes animation JSON → FBS every run; stamp on the asset dir's content hash. Save ≈ 3–5 s.
- **D3 Keep a warm builder container.** `docker run --rm` per build; a long-lived `vic-standalone-builder-8` container + `docker exec` avoids container start + toolchain probes (glibc/cmake/go/upx/protoc lookups). Save ≈ 2–4 s.
- **D4 ccache is already mounted** (`build/cache/ccache`) — confirm it is actually used by the clang toolchain (`CCACHE_DIR` inside the container, `ccache -s` hit rate); a real patch change rebuilds libcozmo_engine.so (17 MB), where ccache matters most.

### E. check.sh contents

- **E1 Fixed check dir for Go.** Archive HEAD into a fixed `robot/work/check-head/.src` (as `run-server.sh` does for its build) instead of `mktemp -d`, so Go's build *and test-result* cache hit (`(cached)`). Save ≈ 15 s of the ≈ 20 s Go share when Go code didn't change. (Parallel sessions: lock the dir with `flock`.)
- **E2 Cache jsoncpp.o.** mic-stream.sh and pc-voice.sh each compile `jsoncpp.cpp` at `-O1`; build it once into `robot/work/hostcache/jsoncpp-<hash>.o`. Save ≈ 8–12 s.
- **E3 Pre-generated test fixture keys.** hostkey/tests/apply.sh runs `ssh-keygen` for RSA/ECDSA/ed25519 every run; generate once into a gitignored cache (or commit throwaway *test-only* keys under `tests/fixtures/` — but `origin` is public and `secrets.sh` guards key-shaped files, so a cache is the safer choice). Save ≈ 10 s.
- **E4 Per-test input stamps.** Generalise A3 to each check: inputs = a list of paths, key = `git rev-parse <sha>:<path>` tree ids; skip the test when its key has a pass stamp. Turns most checks into no-ops on a server-only commit.

### F. Fail fast

- **F1 Order checks by failure likelihood × cheapness** (drift, syntax, static first; slow host tests last) — with B1 this matters less.
- **F2 Pre-flight before the build:** robot reachable (one SSH), wire-pod enabled, sudoers rule present — today a robot-off or unit problem surfaces only after check + build.

## 4. Proposals that need refactoring (application / server / firmware)

- **R1 Move tunables out of the firmware.** Patches 0008 (speech volume), 0009 (eye chimes), 0011 (night window), 0003/0004 (quiet idle/winsock), 0010 (PC voice URL) carry values that change more often than code. Have the firmware read them from a file on `/data/myvector/` (or a wire-pod endpoint/jdoc) at start or on SIGHUP; settings.sh pushes the file. Turns future "tweak" ships from firmware ships (~3 min) into robot-script ships (~10 s). Costs one-time patch work and a fallback-defaults path in C++.
- **R2 Per-unit deploy + restart.** Map built files → owning unit (vic-engine ← libcozmo_engine.so, vic-anim ← vic-anim, vic-cloud ← vic-cloud …); restart only units whose files changed instead of `anki-robot.target`. Smoke's boot wait then only waits on the restarted unit; a vic-cloud-only change skips the engine's ~15 s startup. Needs a `deploy` wrapper around upstream `deploy.sh` (fork-side, no upstream edit).
- **R3 Zero-downtime wire-pod restart.** Today a server ship drops :443 for ~5 s and the robot's vic-cloud reconnects (smoke's `get_battery` retry exists for this). Options: systemd socket activation for :443/:8084/:80 (listeners survive the restart), or start the new binary on the inherited fds then stop the old (graceful handoff). Makes server ships invisible to the robot and lets smoke skip the reconnect wait.
- **R4 Server-side smoke endpoint.** Fold the server-lane checks (conn_test, get_battery, fork_robot_state, fork_say, log-level intent) into one `fork_smoke` endpoint in `sdkapp/fork_smoke.go` that runs them concurrently server-side and returns a JSON verdict; smoke.sh makes one HTTP call. Cuts curl/jq round trips and lets the server lane's smoke take ~3–5 s. Must keep check-static S5's motion-free allowlist intact.
- **R5 One-shot robot agent.** A single robot-side script (shipped via `robot/lib`) that does keys check, compat read, settings diff, diskguard/epod state and returns one report — one SSH round trip instead of ~25. Bigger rewrite of hostkey/sshkey/settings into "plan locally, apply remotely in one batch".
- **R6 Declarative check runner.** Rewrite `check.sh` as a table of `name | input paths | command` driving B1 + E4 (parallel, input-hash cached, per-test logs, fail-fast summary). One mechanism instead of hand-added `if [[ -f … ]]` lines; new tests get caching for free.
- **R7 Background pre-ship on commit.** A post-commit hook (or `make prebuild` the session runs after committing) that runs check, firmware build and server build for HEAD in the background and writes the stamps from A3/A4/B3. `make ship` then only does deploy + smoke + push. Must lock the shared victor clone (`flock`) since parallel sessions commit too; ship still verifies stamps belong to its pinned sha.
- **R8 Native (non-docker) firmware build.** The vicos SDK, cmake, go, upx and protoc already live under `anki-deps` / `~/.anki`; building on the host removes docker startup and the uid/volume mapping, and makes ccache/Go caches plain host dirs. Medium effort, upstream build scripts already have a non-docker (macOS) path to imitate.
- **R9 Split `make ship` into lanes as targets** (`ship-server`, `ship-robot`, `ship-firmware`) sharing one library; `make ship` = auto-detected lanes (A1). Makes each lane testable alone and keeps ship.sh from growing.

## 5. Future state (projected)

| Ship kind (share of history) | Today | Quick wins (A1 A3 C1 C2 C5 E1–E3 B1) | + refactors (R1 R3 R4 R7) |
|---|---:|---:|---:|
| docs only (≈ 5 %) | ~2m45s | **≈ 5 s** | ≈ 5 s |
| server only (≈ 60 %) | ~2m45s | **≈ 25–35 s** | ≈ 10–15 s |
| server + robot scripts (≈ 15 %) | ~3m00s | ≈ 30–40 s | ≈ 15–20 s |
| firmware (≈ 20 %) | ~3m30s | ≈ 1m30s | ≈ 50–60 s (tunable tweaks → robot-script lane) |

Floor for a firmware ship ≈ real compile + rsync of changed files + robot stack boot (~18 s) + minimal smoke.

## 6. Suggested first rung (thinnest visible slice)

**A1 (lane detection, firmware lane = today's full path) + A3 (check stamp).** Two small, separable changes in `ship.sh`/`check.sh`; together they turn the common server-only ship from ~2m45s into ≈ 40 s, visible on the very next ship. Everything else stays an unordered backlog to re-pick after each rung; C1 (SSH reuse) and C2 (stability sleep) are the next cheapest large wins.

### Pre-registered check for each rung

Compare `robot/work/ship-logs/` wall time (log name → mtime) for the next 3 ships of the same lane against the table in §1; add per-step `+N s` stamps to ship.sh's `step()` (smoke.sh already has them) as part of rung 1 so later rungs are measured, not estimated.

## Rung 1 — rulings (2026-11-08) and stages

| id | ruling | edit site |
|---|---|---|
| scope | rung 1 = A1 + A3 + per-step `+N s` + C1 + C2; rest stays backlog | — |
| A1 | firmware lane iff robot `/data/myvector/firmware-tree` ≠ "built tip's `^{tree}` + current `/anki/{bin,lib}` stat fingerprint"; `make ship FIRMWARE=1` forces | `ship.sh`, `Makefile` |
| A1 marker | `rm` before deploy (fail = die, robot untouched); write after green full smoke; write fail = WARN | `ship.sh` |
| A1/C5 smoke | no-firmware ship → `smoke.sh --server`: units baseline (no wait), link, battery, docked + log, face + short phrase, A3 intent, end PIDs | `smoke.sh` |
| A3 | stamp key = sha1(content tree + go/g++/venv versions + clone HEAD); `check.sh` writes on green; `--head` on a hit runs only drift + secrets | `check.sh` |
| C1 | `ControlPath` in `ssh_opts` only (hostkey/sshkey pass `ControlPath=none`); ship opens one master after the keys step, closes on EXIT | `robot/env.sh`, `ship.sh` |
| C2 | full smoke's stability sleep 15 s → 3 s; end-of-run `same_pids` stays | `smoke.sh` |

- **Stage 1.1** A3 check stamp — `check.sh`; falsifier: `scripts/check.sh --head` on a tree `make check` passed prints `check skipped`.
- **Stage 1.2** C1 + C2 + `step()` `+N s` — `robot/env.sh`, `smoke.sh`, `ship.sh`.
- **Stage 1.3** A1 lanes + `smoke.sh --server` — `ship.sh`, `smoke.sh`, `Makefile`.
- **Stage 1.4** done: 9 server-lane ships reached push at 38–53 s (median ≈ 47 s) vs the 20–25 s target — missed; firmware lane ≈ 140 s.

## Rung 2 — rulings (2026-11-09) and stages

| id | ruling | edit site |
|---|---|---|
| scope | rung 2 = server lane only: C6 + L1 + C4 (`seen()` only) + T2 stamps; ship step 4 keys untouched (ControlPath=none stays) | — |
| C6 | ship step 8 runs `SKIP_KEYS=1 settings.sh`; set → settings.sh skips hostkey + sshkey apply.sh; standalone default unchanged | `settings.sh`, `ship.sh` |
| L1 | logkeep's `logger "$tag"; sleep 2` → grep both files at once, then ≤ 3 × `sleep 1`, then the existing FAIL | `robot/logkeep/apply.sh` |
| C4 | `seen()` poll `sleep 2` → `sleep 0.5`; 10 s cap unchanged | `smoke.sh` |
| T2 | settings.sh `ok`/`warn` and smoke.sh `ok` end with `(+$SECONDS s)` | `settings.sh`, `smoke.sh` |
| R7 | open: background build after each commit — not asked yet | — |

Refactor-first: skipped — each pick is a local edit; R5 would subsume C6/C7 and stays backlog.

- **Stage 2.1** C6 — `settings.sh`, `ship.sh`, `robot/firmware/tests/pc-voice.sh`; falsifier: `bash robot/firmware/tests/pc-voice.sh` (new `SKIP_KEYS=1` case fails on a `hostkey stub`/`sshkey stub` line).
- **Stage 2.2** L1 + C4 + T2 — `robot/logkeep/apply.sh`, `robot/logkeep/tests/apply.sh`, `smoke.sh`, `settings.sh`; falsifier: `bash robot/logkeep/tests/apply.sh` (new no-op-logger case must print `FAIL: … not in`).
- **Stage 2.3** `make check` + code review; live: user runs 3 server-lane `make ship`; settings gap per log ≤ 6 s: `sed 's/\x1b\[[0-9;]*m//g' <log> | awk -F'[+ ]' '/^==> .*robot settings:/{a=$(NF-1)} /^==> .*robot smoke test:/{exit $(NF-1)-a > 6}'`.
- **Target** median push stamp of the next 3 server-lane ships ≤ 33 s: `for f in $(grep -l 'smoke.sh --server' $(ls -t robot/work/ship-logs/*.log) | head -3); do sed 's/\x1b\[[0-9;]*m//g' "$f" | awk -F'[+ ]' '/^==> \[[0-9]+\] push:/{print $(NF-1)}'; done | sort -n | sed -n 2p | awk '{exit $1 > 33}'`
