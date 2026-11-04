# Backlog

- ⚠ Server restart drops "Hey Vector" until someone runs `sudo ./chipper/start.sh` · seam: WSL + `setup.sh daemon-enable` at Windows logon (Task Scheduler running `wsl`) · falsifier: reboot Windows → `pgrep -a chipper` is non-empty.

## OWED[USER]

- Judge the stay-docked soak (all three patches, AUTO_HOLD unset) after 2026-11-05 08:12: `robot/stay-on-charger/soak.sh --summary` → `PASS`; keep the PC awake until then.
  also try one voice command that moves him (Q6) and confirm "Hey Vector" answers. PASS → R4 delete in 2026-11-04-architecture-refactors.md (docs/plans/); FAIL → Rollback in 2026-11-03-vector-unlock-wireos.md (docs/plans/).
- Only if the soak FAILs: live-check auto-hold: set `export AUTO_HOLD=override` in `chipper/source.sh` (later try `reserve`), restart chipper, reboot Vector → he ends up held on the charger; `top` shows chipper near 0% CPU.
- Live-check lift-lock after the deploy: "Hey Vector, fist bump" → lift still, eyes animate, stays docked; after 10 min `logcat -d | grep -c IActionRunner.Update.TracksLocked`
  and `logcat -d | grep -ci calibrat` are not rising. FAIL → Rollback in 2026-11-04-vector-lift-lock.md (docs/plans/).
