# Backlog

- ⚠ Server restart drops the hold until someone runs `sudo ./chipper/start.sh` · seam: WSL + `setup.sh daemon-enable` at Windows logon (Task Scheduler running `wsl`) · falsifier: reboot Windows → `pgrep -a chipper` is non-empty.

## OWED[USER]

- Live-check auto-hold: set `export AUTO_HOLD=override` in `chipper/source.sh` (later try `reserve`), restart chipper, reboot Vector → he ends up held on the charger; `top` shows chipper near 0% CPU.
- Choose a route that keeps "Hey Vector": battery swap, or 2026-11-03-vector-unlock-wireos.md (docs/plans/).
