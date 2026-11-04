# Backlog

- ⚠ Server restart drops "Hey Vector" until someone runs `sudo ./chipper/start.sh` · seam: WSL + `setup.sh daemon-enable` at Windows logon (Task Scheduler running `wsl`) · falsifier: reboot Windows → `pgrep -a chipper` is non-empty.

## OWED[USER]

- Live-check auto-hold: set `export AUTO_HOLD=override` in `chipper/source.sh` (later try `reserve`), restart chipper, reboot Vector → he ends up held on the charger; `top` shows chipper near 0% CPU.
- Judge the stay-docked soak after 2026-11-05 07:21: `robot/stay-on-charger/soak.sh --summary robot/work/soak-2026-11-04-072053.log` → `PASS`;
  also try one voice command that moves him (Q6) and confirm "Hey Vector" answers. FAIL → Rollback in 2026-11-03-vector-unlock-wireos.md (docs/plans/).
