# Vector unlock + WireOS: stay on the charger and still answer "Hey Vector"

**Goal**: Vector lives on the charger, still answers "Hey Vector", and the server holds nothing
(`AUTO_HOLD=off`). The way there: unlock the production bot, install WireOS, then patch the firmware's
own stay-on-charger behavior.
**Status**: slice 0 PC steps done; every step on the robot is `[USER]`. Scripts: `robot/unlock/`, `robot/stay-on-charger/`.
**Rulings (2026-11-03)**: all slices covered · a failed power stress test means proceed with the extra guards,
not stop · the dock stays on a wall adapter on a guarded strip (no UPS).
**Rulings (slice 3)**: docked, he never leaves on his own (not for battery full, timeout or a voice command) ·
the C++ patch is the route, 3a is only a stopgap, the JSON edit is dropped.

## Facts (research 2026-11-03; VERIFIED from source or the OTA file itself unless marked inferred)

- The robot is a Vector 1.0 on `2.0.1.6076ep`, with the factory recovery still in place. `Unlock-Prod.ota` is
  175,349,760 B, sha256 `0765574a…110b`. That is our own hash of the 2026-11-03 download, pinned in
  `robot/unlock/verify-ota.sh`; the robot's own `manifest.sha256` signature check is the real guard.
- **The unlock runs in two phases.** In **Phase A** (about 5–8 min) the factory recovery writes boot_a and
  system_a. Recovery is untouched, so a failure there means retrying from recovery (inferred).
- **Phase B** starts by itself after he reboots into the unlocker. Payload sizes, uncompressed, from the
  decrypted unlocker system image's `/anki/`:
  - bootloader (aboot): 419 KB, written in under 1 s
  - recovery: 32 MiB
  - recoveryfs: 581 MiB
- After those writes it zeroes **both** boot slots. Phase B takes about 1–2 min (inferred from sizes) with no
  fallback, and the order of the writes is unknown.
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
- WireOS `dev.ota` is 163,686,400 B (`ankidev=1`) with no published hash and no signature, and both values are pinned in
  `verify-ota.sh`. Installed from the unlocked recovery it writes slot a only, so recovery stays as the
  fallback (inferred). WireOS can start OTAs on its own unless
  `/data/data/user-do-not-auto-update` exists.

## Open questions

| id | ruling | edit site |
|---|---|---|
| Q1 | The unlock runs from the factory recovery; the OS being `ep` doesn't matter (inferred from `ankidev=0`) | slice 1 |
| Q2 | WireOS points at its author's cloud. Re-run the fork's SSH setup (`setup/ssh.go`) to point it back at the fork | slice 2 |
| Q3 | `kSafeguardTimeout_s` can be set at runtime via `:8888/consolevarset`. It is probably lost on reboot (unconfirmed) | slice 3a |
| Q4 | `ddn on` lasts across reboots but probably stops wake-word replies (inferred), so skip it | slice 3a |
| Q5 | Does `/data` (user data) survive the unlock? Unknown, so back up the server side | slice 0 |
| Q6 | Only intents handled under HighLevelAI (explore) are blocked; voice behaviors above it still run, and one that drives off ends the stay | slice 3b done |
| Q7 | On the platform but off the contacts he stays put and doesn't charge (the patch checks the platform, not the contacts) | slice 3b done |
| Q8 | Pin = the `anki/victor` submodule of the wire-os build `dev.ota` came from (compat 209); `deploy.sh` refuses a mismatch. Before building: `ssh root@<vector-ip> cat /etc/os-version /etc/victor-compat-version` → `3.0.2.36d`, `209` | slice 3b |

## Checklist

### Slice 0: prepare (nothing on the robot changes)
- [x] `robot/unlock/backup-server-state.sh` → `OK: robot/backup/server-state-*.tgz`.
- [x] ESN and IP saved in `robot/backup/robot-id-*.txt`; confirm the firmware line there against the web UI.
- [x] `robot/unlock/verify-ota.sh unlock` → `OK unlock … 0765574a…`, and `verify-ota.sh wireos` → `OK wireos`.
- [x] 2.4 GHz network `Linwifi2_2k` (WPA2/WPA3 mixed; not WPA3-only). If websetup won't join, set the router
      to WPA2-only. Use Chrome on Windows with Bluetooth on; WSL has no Bluetooth.
- [x] `robot/unlock/serve-ota.sh`; the phone (not the Windows browser) must list `unlock-prod/` at `http://<pc-ip>:8123/`.
      Firewall, admin PowerShell: `New-NetFirewallHyperVRule -Name VectorOTA8123 -DisplayName "Vector OTA 8123" -Direction Inbound -VMCreatorId '{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}' -Protocol TCP -LocalPorts 8123 -Action Allow` + `New-NetFirewallRule -DisplayName "Vector OTA 8123" -Direction Inbound -Protocol TCP -LocalPort 8123 -Action Allow`.
- [x] Power: the dock's adapter is the only load on the strip, nothing on it switches (heater, kettle), the
      strip switch is taped, and there's no storm or planned power work. Pick a quiet hour.
- [x] Dock: wipe the contacts and seat him firmly; `get_battery` shows `is_on_charger_platform:true`. Off the dock
      he goes dark at once (dead battery), so nobody moves him, presses the button or touches the strip until slice 2 is done.
- [ ] `robot/unlock/power-stress.sh 25` → `PASS: no reboot`. **If he reboots**, the guards above become mandatory; proceed (ruling).
- [ ] Remove anything touching his lift (a blocker): a lift move against it pries him off the contacts.

### Slice 1: unlock (Phase A can be retried; Phase B can't be undone)
- [ ] Recovery: on the dock, hold the button about 15 s until the backpack lights turn dark blue, then
      double-press to show the pairing key.
- [ ] websetup.skittle.dev → pair → Wi-Fi (the WPA2 network).
- [ ] With `serve-ota.sh` running, enter in websetup's terminal
      `ota-start http://<pc-ip>:8123/unlock-prod/Unlock-Prod.ota`. Fallback: Utility → Unlock-Prod.ota.
- [ ] Phase A ends when he reboots by himself. Retry (back into recovery) **only** if websetup shows an error
      **and** he has not rebooted. Once he has rebooted, treat it as Phase B, even if it looks stalled.
- [ ] **Phase B, from that reboot onward: hands off.** No button, no lifting, nobody near the strip.
      The screen shows Writing ABOOT / Recovery / RecoveryFS.
- [ ] If "Put Vector on Charger!" appears **before any Writing line**, reseat him gently (he may go dark; that
      is a Phase A retry). Once any Writing
      line has shown, touch nothing, even if that message appears. The write order is unknown.
- [ ] Wait until he reboots into the new recovery by himself. If it seems slow, keep waiting at least
      15 min before calling it stuck. Then stop `serve-ota.sh`. He has no OS now, so go straight to slice 2.

### Slice 2: install WireOS (A/B slots, recovery stays as the fallback)
- [ ] From the unlocked recovery: websetup → pair → Wi-Fi → `serve-ota.sh` →
      `ota-start http://<pc-ip>:8123/wireos-dev/dev.ota`. Fallback: Custom Firmware.
- [ ] First thing after it boots: `ssh root@<vector-ip>`, run `touch /data/data/user-do-not-auto-update`, then log out.
- [ ] Re-run the fork's bot setup over SSH (Q2), then check that "Hey Vector, what time is it?" is answered.
- [ ] Done when `:8080` (`wired`) loads and the voice answer comes from the fork.

### Slice 3: stay on the charger without a hold (3a is the stopgap, 3b the fix)
- [ ] a. Stopgap: `curl 'http://<vector-ip>:8888/consolevarset?key=kSafeguardTimeout_s&value=86400'`. Check that
      `features.json` has `StayOnChargerUntilCharged` enabled. He still leaves when the battery reads full (3.57 V once), and the setting is probably lost on reboot (Q3). Skip `ddn` (Q4).
- [x] b. `robot/stay-on-charger/apply.sh` → `OK: patched …/robot/work/wire-os-victor` (PC side, done).
- [ ] Back up first: `ssh root@<vector-ip> tar -C / -czf - anki > robot/backup/anki-$(date +%F).tgz`.
- [ ] In `robot/work/wire-os-victor`, overwrite the bundled `robot_sshkey` with the robot's key, then run
      `./build/build-v.sh` and `./build/deploy-v.sh` (needs Docker; the first build is slow).
- [ ] Done when a full day on the dock passes with no drive-off, the battery voltage rises (Q7), one voice command
      that moves him is tried (Q6), and "Hey Vector" works throughout, with
      `AUTO_HOLD=off`.

### Slice 4: retire the server hold
- [ ] Set `AUTO_HOLD=off` in `chipper/source.sh` once slice 3 is done, then restart chipper.

## Rollback

- Slice 0, or a Phase A failure: nothing has changed, so retry from recovery.
- Phase B cut off: probably a permanent brick, with hardware repair (CPU swap + EDL) the only known route.
- After slice 1: there is no OS until slice 2. A bad WireOS OTA falls back to the unlocked recovery, so reinstall.
- Slice 3: if the patched engine misbehaves, restore the `/anki` backup over SSH after `mount -o rw,remount /`;
  the fallback is reinstalling `dev.ota` from recovery. If he still won't stay put, set `AUTO_HOLD=override` again.

## Alternative that makes this plan optional

A physical block in front of the dock, being tried since 2026-11-03: he spins his tracks for a while, stays on
the dock, and stays powered. If it holds for a day with `AUTO_HOLD=off`, this plan is optional.

Replace the battery. The original is a FullRiver 320 mAh 3.7 V LiPo, soldered on a 1.0. iFixit guide:
ifixit.com/Guide/Anki+Vector++Battery+Replacement/123279.
