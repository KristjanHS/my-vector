# Tailscale recovery — plan B per failure

Since the robot-tailscale plan (`docs/plans/2026-11-10-robot-tailscale.md`, option D), Vector reaches wire-pod
**only** over the tailnet: `escapepod.local` is pinned to the laptop's tailnet IP, the laptop guard drops the
robot's LAN IP, and the robot's wlan0 firewall (netguard, D8) accepts only ssh from `PC_LAN_IP`. So a tailnet
failure means no voice. This page lists each failure, how to tell, what heals by itself, and the plan B.

Facts used below (values in gitignored `robot/robot.env`): `ROBOT_IP` = robot tailnet IP, `ROBOT_LAN_IP` =
robot wlan0 IP, `PC_LAN_IP` = laptop LAN IP (192.0.2.249), `ROBOT_TTS_URL` = `http://<laptop tailnet IP>:8091`.
Robot shell: `. robot/env.sh; ssh "${ssh_opts[@]}" "root@$ip"`.

**The LAN rescue path.** From the PC on the home LAN, `ssh root@$ROBOT_LAN_IP` always works (D8 keeps tcp 22
from `PC_LAN_IP`) as long as the robot is up. Most plan Bs below go through it. Run a script over LAN with a
LAN env copy:

```
cp robot/robot.env robot/work/robot-lan.env
# in robot/work/robot-lan.env: ROBOT_IP=<ROBOT_LAN_IP>
ROBOT_ENV=robot/work/robot-lan.env robot/tailscale/apply.sh
```

## Quick triage

| check (laptop) | healthy | if not → |
|---|---|---|
| `tailscale status` | `vector … active; direct …` (relay also OK) | robot row offline → F3/F5; whole list offline → F4 |
| `tailscale ping -c 2 $ROBOT_IP` | pong | F2/F3/F5 |
| `curl -s -m3 localhost:8080/api-sdk/conn_test?serial=<esn>` | `success` | wire-pod's robot IP stale → F6; wire-pod down → F0 |
| robot: `systemctl is-active myvector-tailscaled` | `active` | F3/F9 |
| robot: `iptables -C OUTPUT -o tailscale0 -j ACCEPT` | exit 0 | F8 |
| robot: `curl -sk -m4 https://escapepod.local/ok` | 200 | pin wrong → F6; ACL → F7 |

## F0 — wire-pod (the server) down or WSL asleep

- **Tell**: `conn_test` fails on localhost; `journalctl -u wire-pod` silent.
- **Self-heal**: the `wire-pod` unit restarts on crash and starts with WSL.
- **Plan B**: `make start`, or the Windows shortcut that runs it (WSL not running). Not a Tailscale issue,
  but it looks the same from the robot.

## F1 — Tailscale coordination server (control plane) down

- **Tell**: admin console / `tailscale up` fail; existing peers still ping.
- **Self-heal**: connected nodes keep their keys and peer map, and existing tunnels keep working.
- **Plan B**: wait. **Don't reboot the robot or re-login during the outage**. It's unverified whether
  tailscaled reconnects from cached state with no control plane. Need voice now → F12 (LAN fallback, at home only).

## F2 — no path between the nodes (DERP relays down, or NAT blocks it while away)

- **Tell**: both nodes online in `tailscale status`, but `tailscale ping` times out. At home the path is
  direct over the LAN (`via 192.0.2.76:41641`), so this mostly hits when the laptop is away.
- **Self-heal**: tailscaled keeps retrying direct and relay paths.
- **Plan B**: away from home, none. The robot is unreachable until the path returns or you're home. At home,
  check that the robot's `udp 41641` rule is in `iptables -S MYVECTOR_IN`, then F12 if needed.

## F3 — tailscaled crashed on the robot

- **Tell**: robot row offline; over LAN ssh, `systemctl status myvector-tailscaled` shows failed or restarting.
- **Self-heal**: `Restart=on-failure`, `RestartSec=5` (D5). A boot also starts it after `/data` mounts.
- **Plan B**: over LAN ssh, run `journalctl -u myvector-tailscaled -n 50` and `systemctl restart myvector-tailscaled`.
  Still failing → re-install over LAN (`ROBOT_ENV=robot/work/robot-lan.env robot/tailscale/apply.sh`).
  A new version started the crash loop → F9.

## F4 — tailscaled down on the laptop (WSL)

- **Tell**: `tailscale status` errors or lists everyone offline.
- **Self-heal**: `tailscaled` is a systemd unit, enabled, and starts with WSL.
- **Plan B**: `sudo systemctl restart tailscaled`. If logged out, run `sudo tailscale up` and log in in the
  browser. The laptop's tailnet IP changed → F6 (laptop half).

## F5 — robot logged out, node expired or revoked

- **Tell**: robot over LAN: `tailscale --socket=/run/tailscale/tailscaled.sock status` says `NeedsLogin`;
  console shows the node gone or expired (key expiry is disabled per D3, so expect "revoked").
- **Self-heal**: `robot/tailscale/apply.sh` logs in by itself on a settled `NeedsLogin` **if**
  `secrets/tailscale-authkey` exists. That key was one-off, so it's probably spent.
- **Plan B**: in the admin console, make a new pre-approved `tag:vector` key (D3), store it with
  `(umask 077; cat >secrets/tailscale-authkey)`, and run apply.sh over LAN. Then check the robot's new tailnet IP
  (`tailscale ip -4` on the robot). If it changed → F6.

## F6 — a tailnet IP changed (robot or laptop)

**Robot IP changed** (re-login as a new node):

1. In `robot/robot.env`, set `ROBOT_IP` to the new IP.
2. In `scripts/wire-pod-guard.nft`, put the new IP in `tailnet4` (and `tailnet6`). Commit, then `make server-pin`.
   The guard loads the **pinned** commit, so an uncommitted edit doesn't take effect.
3. Add a `~/.ssh/known_hosts` line for the new IP with the robot's LAN host key (BatchMode refuses unknown hosts).
4. Fix wire-pod's stored robot IP: set `ip_address` in `secrets/server/jdocs/botSdkInfo.json`, then `make start`.
   It only updates itself when the robot makes a jdocs call. The ship smoke prints this exact fix when it's stale.

**Laptop IP changed**:

1. In `robot/robot.env`, set `ROBOT_TTS_URL=http://<new laptop IP>:8091`.
2. Re-pin `escapepod.local`: run `robot/netguard/apply.sh` over the tunnel. If the tunnel is down, use the
   manual `sed` from F12 with the new IP (netguard refuses `ROBOT_IP` = `ROBOT_LAN_IP`).
3. Re-push the voice file: `robot/firmware/voice-push.sh <voice> <slow> <lang>`. Copy the current values from
   the robot's `/data/data/myvector-voice.json`.
4. The ACL targets the laptop by name (`wsl-ubuntu`), so it needs no change.

## F7 — ACL wrong in the admin console

- **Tell**: `tailscale ping` works both ways, but robot `curl -sk https://escapepod.local/ok` gets no 200.
- **Plan B**: restore the D3 grants: `autogroup:member` → `autogroup:self:*` and `tag:vector:*`; `tag:vector` →
  `wsl-ubuntu` tcp `80,443,8084,8091,8093`. Never `tailscale serve`/`funnel`.

## F8 — the robot's outbound tunnel rule is missing (D13)

- **Tell**: robot `tailscale0` is UP and the node is online, but nothing leaves the robot over it;
  `iptables -C OUTPUT -o tailscale0 -j ACCEPT` exits 1. The base OUTPUT policy is DROP.
- **Plan B**: over LAN ssh, `systemctl restart myvector-tailscaled`. Its `ExecStartPre` re-adds the v4+v6 pair.

## F9 — a Tailscale upgrade crash-loops

- **Guard**: `robot/tailscale/apply.sh` refuses to restart tailscaled over the tailnet. An upgrade only happens
  over LAN, from home.
- **Plan B (roll back)**:
  1. `git revert` the `TS_VERSION`/`TS_SHA256` bump in `robot/tailscale/fetch.sh`.
  2. `robot/tailscale/fetch.sh`, then `ROBOT_ENV=robot/work/robot-lan.env robot/tailscale/apply.sh`.
  3. `systemctl show -p NRestarts myvector-tailscaled` should stay 0.

## F10 — locked out by the wlan0 firewall (netguard)

- **Self-heal**: while `robot/netguard/apply.sh` applies, a 5-minute rollback timer is armed, and only the
  `confirm` step cancels it. Off the home LAN (no LAN ssh and no ping reply), apply.sh leaves the firewall as it is:
  WARN if it's hooked, FAIL if it isn't.
- **Plan B** (any working root shell, over tailnet or LAN): unhook and disable the firewall:

```
for t in iptables ip6tables; do for c in MYVECTOR_IN MYVECTOR_IN_NEW; do
  while $t -D INPUT -i wlan0 -j $c 2>/dev/null; do :; done; done; done
mount -o remount,rw /; systemctl disable myvector-netguard; mount -o remount,ro /
```

## F11 — no tailnet AND no LAN ssh

1. Power-cycle the robot. At boot, netguard re-applies D8 (ssh from `PC_LAN_IP` allowed) and tailscaled starts
   after `/data` mounts. The battery is worn, so do it on the charger.
2. Still nothing → recovery mode and BLE re-setup (watch for the recovery-mode double-press gotcha). For the BLE
   re-setup, re-add `ROBOT_LAN_IP` to `allowed4` first (D11), then `make ship` re-applies the robot settings
   (tailscale before netguard).

## F12 — LAN fallback: run without Tailscale (at home)

Use this when the tailnet is down for a while and voice is needed now. You'll need the LAN ssh path.

**Fast, until the next guard reload**:

```
# laptop: let the robot's LAN IP past the wire-pod guard
sudo nft add element inet wirepod_guard allowed4 '{ 192.0.2.76 }'
# robot (LAN ssh): point escapepod.local back at the PC
mount -o remount,rw /
sed -i 's/^[0-9.]* escapepod.local # my-vector$/192.0.2.249 escapepod.local # my-vector/' /etc/hosts
mount -o remount,ro /
```

Then re-push the voice file with a LAN URL: in `robot/work/robot-lan.env`, set
`ROBOT_TTS_URL=http://192.0.2.249:8091` and run `ROBOT_ENV=robot/work/robot-lan.env robot/firmware/voice-push.sh <voice> <slow> <lang>`.
wire-pod notices the robot's LAN IP on its next jdocs call. If `conn_test` still fails, set `ip_address` back
to the LAN IP and run `make start` (F6 step 4).

**Durable**: put the robot LAN IP back in `allowed4` in `scripts/wire-pod-guard.nft`, commit, then `make server-pin`.

**Undo the fallback**: delete the `allowed4` element (`sudo nft delete element …`, or `make server-pin`),
then run `robot/netguard/apply.sh` with the normal `robot.env` (re-pins to the laptop tailnet IP), then
`voice-push.sh` with the normal env.

## F13 — robot lost, or the auth key leaked

Revoke the `vector` node in the admin console (D3). Without the node key, a robot that's been taken can't
reach the tailnet. Then delete `secrets/tailscale-authkey` and make a fresh key only when you re-enrol (F5).
