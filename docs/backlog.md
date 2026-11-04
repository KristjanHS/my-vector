# Backlog

- ⚠ Server restart drops "Hey Vector" until someone runs `sudo ./chipper/start.sh` · seam: WSL + `setup.sh daemon-enable` at Windows logon (Task Scheduler running `wsl`) · falsifier: reboot Windows → `pgrep -a chipper` is non-empty.

## OWED[USER]

- Judge the stay-docked soak 24 h after the last `make ship` (ship restarts it; the 08:12 soak spans the 13:46 ship → after `make syncwt` run `robot/firmware/soak.sh --restart`): `robot/firmware/soak.sh --summary` → `PASS`; keep the PC awake.
  also try one voice command that moves him (Q6) and confirm "Hey Vector" answers. FAIL → `git checkout 18541df^ -- chipper/pkg/myvector chipper/pkg/wirepod/sdkapp/fork_autohold.go chipper/pkg/wirepod/sdkapp/server.go scripts/upstream-drift.sh`, `export AUTO_HOLD=override` in `chipper/source.sh`, restart chipper (A1–A5 at `18541df^`); then Rollback in 2026-11-03-vector-unlock-wireos.md (docs/plans/).
- Restart chipper (`make start`) to drop auto-hold: log has no `Auto-hold` line; "Hey Vector" answers; web-UI Assume/Release still work.
- Volume 2 (interim): "Hey Vector, what time is it?" → answer clearly audible; then Stage 1 of 2026-11-04-vector-speech-volume.md (docs/plans/).
- Quiet idle: at the soak verdict `robot/firmware/quiet-count.sh` → `PASS`. FAIL → plan a ruling for the behavior its `hit:` lines name.
- Next `make ship`: step 7/8 runs `smoke.sh` → PASS before the push (FAIL = robot runs the new sha, not pushed); during its A2 step Vector shows a solid face. From 2026-11-04-firmware-test-suite.md (docs/plans/archive/).
