# Backlog

## OWED[USER]

- Autostart: run the `Register-ScheduledTask` line (§Start with Windows in run-without-sudo-password.md, docs/); reboot + log in → `systemctl is-active wire-pod` = `active`.
- Stay-docked: if he leaves the dock and dies, read `/data/log/keep/messages*` on the robot (`robot/logkeep/apply.sh` keeps them);
  also try one voice command that moves him (Q6) and confirm "Hey Vector" answers. FAIL → `git checkout 18541df^ -- chipper/pkg/myvector chipper/pkg/wirepod/sdkapp/fork_autohold.go chipper/pkg/wirepod/sdkapp/server.go scripts/upstream-drift.sh`, `export AUTO_HOLD=override` in `chipper/source.sh`, restart chipper (A1–A5 at `18541df^`); then Rollback in 2026-11-03-vector-unlock-wireos.md (docs/plans/).
