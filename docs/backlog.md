# Backlog

## OWED[USER]

- Autostart: re-run the sudoers install, stop the foreground wire-pod, `make daemon-enable`, run the `schtasks` line (§Start with Windows in run-without-sudo-password.md, docs/); reboot + log in → `systemctl is-active wire-pod` = `active`.
- Stay-docked: if he leaves the dock and dies, read `/data/log/keep/messages*` on the robot (`robot/logkeep/apply.sh` keeps them);
  also try one voice command that moves him (Q6) and confirm "Hey Vector" answers. FAIL → `git checkout 18541df^ -- chipper/pkg/myvector chipper/pkg/wirepod/sdkapp/fork_autohold.go chipper/pkg/wirepod/sdkapp/server.go scripts/upstream-drift.sh`, `export AUTO_HOLD=override` in `chipper/source.sh`, restart chipper (A1–A5 at `18541df^`); then Rollback in 2026-11-03-vector-unlock-wireos.md (docs/plans/).
- Restart chipper (`make start`) to drop auto-hold: log has no `Auto-hold` line; "Hey Vector" answers; web-UI Assume/Release still work.
- Quiet idle: after he has slept once, `robot/firmware/quiet-count.sh` → `PASS`. FAIL → plan a ruling for the behavior its `hit:` lines name.
- Next `make ship`: step 7/9 `settings.sh` → only `ok:` lines; step 8/9 `smoke.sh` → PASS before the push (FAIL = robot runs the new sha, not pushed); during its A2 step Vector shows a solid face. From 2026-11-04-firmware-test-suite.md (docs/plans/archive/).
- Speech volume (0008): after he has slept, `quiet-count.sh` → PASS.
  FAIL → `volume=2` in `robot/firmware/settings.sh` (else the next ship resets 4); Rollback in 2026-11-04-vector-speech-volume.md (docs/plans/).
