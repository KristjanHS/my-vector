# Vector auto-hold: keep a worn-battery Vector on the charger

**Goal**: the my-vector server holds behavior control of Vector automatically, so he never drives off the
charger — and the hold survives server restarts and robot reconnects. Battery is worn (~3.57 V on charger,
blacks out within seconds off it); a stock `ep` robot's firmware can't be changed, so the server must do it.

## Facts (true at capture, 2026-11-03 — re-grep before acting)

- WirePod's "Assume" = `/api-sdk/assume_behavior_control?priority=high` → `assumeBehaviorControl`
  (`chipper/pkg/wirepod/sdkapp/bcassume.go`) opens a `BehaviorControl` gRPC stream at `OVERRIDE_BEHAVIORS`;
  any other `priority` value → `DEFAULT`. `RESERVE_CONTROL` is not exposed.
- The hold lives in a goroutine keyed on `Robot.BcAssumption`. Release only flips the flag. If the stream
  errors, the goroutine returns but the flag stays `true` — no re-assume. `removeRobot` clears the flag.
- The "held" loop is `for { select { case <-stop: …; default: continue } }` — a busy spin, ~1 core at 100%
  for as long as control is held.
- At `OVERRIDE_BEHAVIORS`, "Hey Vector" is ignored.
- Host: WSL2 is already `networkingMode=mirrored` (WSL eth on the LAN as 192.0.2.249). Go not installed in
  WSL; `setup.sh` installs Go 1.22.4 + Vosk and needs root. The upstream Windows WirePod app currently owns
  :443 (`chipper.exe`) — must be quit before the fork can bind.

## Slices (each one visible and committable)

1. **Run the fork in WSL2, no code change.** Quit Windows WirePod (and disable its autostart);
   `sudo STT=vosk ./setup.sh`; `sudo ./chipper/start.sh`; open the web UI; re-run Bot Setup → Authenticate
   if Vector doesn't reconnect by itself. Done when "Hey Vector, what time is it?" works and the request
   shows in the fork's log.
   Ruling: Hyper-V inbound stays default-Block; allow rules `wirepod-vector-*` for Vector's IP only
   (TCP 80/443/8084, UDP 5353). Falsifier: `sudo tcpdump -ni any host <vector-ip>` shows Vector's :443 SYNs.
2. **Fix the busy spin.** In `assumeBehaviorControl`, block on `<-stop` instead of `select … default`.
   Falsifier: chipper CPU while held drops from ~100% of a core to ~0 (`top`).
3. **Auto-hold.** New file `chipper/pkg/wirepod/sdkapp/autohold.go` (keeps upstream diff small): when a robot
   connects, assume control; a watchdog re-assumes when the stream drops; toggled by an env var in
   `chipper/source.sh` (e.g. `AUTO_HOLD=override|reserve|off`). Done when a server restart and a robot
   reboot both end with Vector held and still on the charger, with no button click.

## Backlog (unordered — re-pick after each slice)

- Try `RESERVE_CONTROL`: does Vector stay put *and* answer "Hey Vector"? If yes, make it the default.
- Auto-start: WSL VM + `setup.sh daemon-enable` at Windows logon (Task Scheduler running `wsl`).
- Show "held / not held" in the web UI next to the Assume/Release buttons.
- Battery replacement — the real fix; makes the hold optional.
