# Vector unlock + WireOS: stay on the charger and still answer "Hey Vector"

**Goal**: Vector lives on the charger, still answers "Hey Vector", and the server holds nothing
(`AUTO_HOLD=off`). The way there: unlock the production bot, install WireOS, then patch the firmware's
own stay-on-charger behavior.
**Status**: checklist ready, not started. Every step on the robot is `[USER]`. Scripts: `robot/unlock/`.
**Rulings (2026-11-03)**: all slices covered · a failed power stress test means proceed with the extra guards,
not stop · the dock stays on a wall adapter on a guarded strip (no UPS).

## Facts (research 2026-11-03; VERIFIED from source or the OTA file itself unless marked inferred)

- The robot is a Vector 1.0 on `2.0.1.6076ep`, with the factory recovery still in place. `Unlock-Prod.ota` is
  175,349,760 B, sha256 `0765574a…110b` (pinned in `robot/unlock/verify-ota.sh`).
- **The unlock runs in two phases.** In **Phase A** (about 5–8 min) the factory recovery writes boot_a and
  system_a. Recovery is untouched, so a failure there only means retrying from recovery.
- **Phase B** starts by itself on the next boot. It writes the bootloader (aboot, 419 KB, under 1 s),
  recovery (32 MiB) and recoveryfs (581 MiB), then zeroes **both** boot slots. That is about 1–2 min with
  no fallback, and the order of those writes is unknown.
- The unlocker cannot be split further, because its installer (`install-recovery`) is a closed binary.
  What you can control is what reaches it: verify the image first, serve it yourself over plain HTTP
  (recovery can't do HTTPS), and change nothing while Phase B runs.
- **After the unlock there is no OS**, because both boot slots are zeroed. Slice 2 must follow right away;
  `ep` does not come back.
- There is no known way to revive a bot whose bootloader or recovery write was cut off, short of a CPU
  swap plus EDL. `Revert-To-Prod.ota` exists, but it is another bootloader rewrite.
- On the charger, the boot battery check passes even when the battery is too low. The update engines do
  no battery check at all; only `install-recovery` reads the charger voltage. It's unknown whether he runs
  straight from dock power.
- WireOS `dev.ota` is 163,686,400 B with no published hash. Installed from the unlocked recovery it
  writes slot a only, so recovery stays as the fallback. WireOS can start OTAs on its own unless
  `/data/data/user-do-not-auto-update` exists.

## Open questions

| id | ruling | edit site |
|---|---|---|
| Q1 | The unlock runs from the factory recovery; the OS being `ep` doesn't matter (inferred from `ankidev=0`) | slice 1 |
| Q2 | WireOS points at its author's cloud. Re-run the fork's SSH setup (`setup/ssh.go`) to point it back at the fork | slice 2 |
| Q3 | `kSafeguardTimeout_s` can be set at runtime via `:8888/consolevarset`. It is probably lost on reboot (unconfirmed) | slice 3a |
| Q4 | `ddn on` lasts across reboots but probably stops wake-word replies (inferred), so skip it | slice 3a |
| Q5 | Does `/data` (user data) survive the unlock? Unknown, so back up the server side | slice 0 |

## Checklist

### Slice 0: prepare (nothing on the robot changes)
- [ ] `robot/unlock/backup-server-state.sh` → `OK: robot/backup/server-state-*.tgz`.
- [ ] Write down the ESN and current firmware from the fork's web UI, keeping the note in `robot/backup/`.
- [ ] `robot/unlock/verify-ota.sh unlock` → `OK unlock … 0765574a…`, and `verify-ota.sh wireos` → `OK wireos`.
- [ ] Have a 2.4 GHz **WPA2** network ready (not WPA3) that reaches this PC. Use Chrome on Windows with
      Bluetooth on; WSL has no Bluetooth.
- [ ] `robot/unlock/serve-ota.sh`, then open `http://<pc-ip>:8123/` from your phone on the same Wi-Fi.
      It must list `unlock-prod/`. If it doesn't, allow port 8123 in the Windows firewall or use the fallback URL.
- [ ] Power: the dock's adapter is the only load on the strip, nothing on it switches (heater, kettle), the
      strip switch is taped, and there's no storm or planned power work. Pick a quiet hour.
- [ ] Dock: wipe the contacts and seat him firmly. The fork's web UI should show "on charger". Nobody may
      move him, press the button or touch the strip until slice 2 is done.
- [ ] Power stress test, 20–30 min on the dock with `AUTO_HOLD=override` keeping him put: play animations
      or say-text from the SDK web UI every minute, and watch the chipper log for robot reconnects.
      **If he reboots**, the risk is recorded and the guards above become mandatory; proceed (ruling).

### Slice 1: unlock (Phase A can be retried; Phase B can't be undone)
- [ ] Recovery: on the dock, hold the button about 15 s until the backpack lights turn dark blue, then
      double-press to show the pairing key.
- [ ] websetup.skittle.dev → pair → Wi-Fi (the WPA2 network).
- [ ] With `serve-ota.sh` running, enter in websetup's terminal
      `ota-start http://<pc-ip>:8123/unlock-prod/Unlock-Prod.ota`. Fallback: Utility → Unlock-Prod.ota.
- [ ] Phase A: an error or Wi-Fi drop here is safe. Go back into recovery and retry.
- [ ] **Phase B, from the first reboot onward: hands off.** No button, no lifting, nobody near the strip.
      The screen shows Writing ABOOT / Recovery / RecoveryFS. If it shows "Put Vector on Charger!", reseat him
      gently; that check runs before any write.
- [ ] Wait until he reboots into the new recovery by himself. If it seems slow, keep waiting at least
      15 min before calling it stuck. Then stop `serve-ota.sh`.

### Slice 2: install WireOS (A/B slots, recovery stays as the fallback)
- [ ] From the unlocked recovery: websetup → pair → Wi-Fi → `serve-ota.sh` →
      `ota-start http://<pc-ip>:8123/wireos-dev/dev.ota`. Fallback: Custom Firmware.
- [ ] After it boots: `ssh root@<vector-ip>`, run `touch /data/data/user-do-not-auto-update`, then log out.
- [ ] Re-run the fork's bot setup over SSH (Q2), then check that "Hey Vector, what time is it?" is answered.
- [ ] Done when `:8080` (`wired`) loads and the voice answer comes from the fork.

### Slice 3: stay on the charger without a hold (stop at the first that works)
- [ ] a. `curl 'http://<vector-ip>:8888/consolevarset?key=kSafeguardTimeout_s&value=86400'`. Check that
      `features.json` has `StayOnChargerUntilCharged` enabled. The behavior ends when the battery reads full
      (it once read "full" at 3.57 V), and the setting is probably lost on reboot (Q3). Skip `ddn` (Q4).
- [ ] b. Edit the JSON over SSH: `mount -o rw,remount /` and copy
      `/anki/data/assets/cozmo_resources/config/engine/behaviorComponent/behaviors/victorBehaviorTree/stayOnChargerUntilCharged.json`
      into `robot/backup/`.
- [ ] Edit the copy in `robot/work/`, gate it with `python3 -m json.tool <file>`, upload it, then run
      `mount -o ro,remount /` and reboot. Bad JSON crash-loops the engine but SSH keeps working: restore the backup.
- [ ] c. Rebuild: clone `wire-os-victor` into `robot/work/`, apply the C++ patch, then `deploy-v.sh` (needs Docker).
- [ ] Done when a full day on the dock passes with no drive-off and "Hey Vector" works throughout, with
      `AUTO_HOLD=off`.

### Slice 4: retire the server hold
- [ ] Set `AUTO_HOLD=off` in `chipper/source.sh` once slice 3 is done.

## Rollback

- Slice 0, or a Phase A failure: nothing has changed, so retry from recovery.
- Phase B cut off: probably a permanent brick, with hardware repair (CPU swap + EDL) the only known route.
- After slice 1: there is no OS until slice 2. A bad WireOS OTA falls back to the unlocked recovery, so reinstall.
- Slice 3: restore the saved JSON over SSH. If he still won't stay put, set `AUTO_HOLD=override` again.

## Alternative that makes this plan optional

Replace the battery. The original is a FullRiver 320 mAh 3.7 V LiPo, soldered on a 1.0. iFixit guide:
ifixit.com/Guide/Anki+Vector++Battery+Replacement/123279.
