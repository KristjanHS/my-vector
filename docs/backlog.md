# Backlog

- ⚠ Server restart drops "Hey Vector" until someone runs `sudo ./chipper/start.sh` · seam: WSL + `setup.sh daemon-enable` at Windows logon (Task Scheduler running `wsl`) · falsifier: reboot Windows → `pgrep -a chipper` is non-empty.

## OWED[USER]

- Judge the stay-docked soak (all three patches, auto-hold removed) after 2026-11-05 08:12: `robot/firmware/soak.sh --summary` → `PASS`; keep the PC awake until then.
  also try one voice command that moves him (Q6) and confirm "Hey Vector" answers. FAIL → `git checkout 18541df^ -- chipper/pkg/myvector chipper/pkg/wirepod/sdkapp/fork_autohold.go chipper/pkg/wirepod/sdkapp/server.go scripts/upstream-drift.sh`, `export AUTO_HOLD=override` in `chipper/source.sh`, restart chipper (A1–A5 at `18541df^`); then Rollback in 2026-11-03-vector-unlock-wireos.md (docs/plans/).
- Restart chipper (`make start`) to drop auto-hold: log has no `Auto-hold` line; "Hey Vector" answers; web-UI Assume/Release still work.
- Volume 2 (interim): "Hey Vector, what time is it?" → answer clearly audible; then Stage 1 of 2026-11-04-vector-speech-volume.md (docs/plans/).
- Quiet idle: at the soak verdict `robot/firmware/quiet-count.sh` → `PASS`. FAIL → plan a ruling for the behavior its `hit:` lines name.
- Live-check lift-lock after the deploy: "Hey Vector, fist bump" → lift still, eyes animate, stays docked; after 10 min `logcat -d | grep -c IActionRunner.Update.TracksLocked`
  and `logcat -d | grep -ci calibrat` are not rising. FAIL → Rollback in 2026-11-04-vector-lift-lock.md (docs/plans/).
- Next `make ship`: first on `robot/firmware/` + the clone's `my-vector` branch — step 2 prints `tree = pin + 4 patches`, build green.
