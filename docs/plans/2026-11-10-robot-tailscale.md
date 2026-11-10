# Robot Tailscale (option D: spike, then productise)

Status: planned 2026-11-10. Executes item 3 of §Recommended plan in `2026-11-07-public-wifi-security.md (docs/plans/)`
(§D. Tunnel on the robot itself) and extends the netguard shipped by `2026-11-07-wifi-baseline-hardening.md (docs/plans/)`.
`[USER]` = a live run on the robot, server or admin console: Claude asks first, never runs it unasked. `[SOUND]` = makes
robot sound: `robot/quiet/quiet.sh status` ≠ 3 right before it, ask, note the start time, then `robot/quiet/scan.sh '<start>'`
(exit 1 → stop all sound tests, `make quiet`, tell the user). `make ship` only on the user's ask, from `main`.

## Refactor-first

Skipped — netguard's phases (`hosts`/`fw`/`undo`/`confirm`) and `robot/lib/etc.sh` (`etc_put`, `arm`, `confirm`) carry the
D shape unchanged; the change is two inputs (D7), a new `rules.sh` signature (D8) and a second reach target (D10), each a
rewrite of the lines it supersedes inside Stage 4, not a seam move ahead of it.

## Rulings

| id | ruling | edit site |
|---|---|---|
| D1 | Target = Tailscale on the robot. Spike (Stages 1–2) gates productise (3–5). Spike NO-GO → stop here, fall back to option C; no C work in this plan. | — |
| D2 | `tailscaled` kernel TUN (`tailscale0`), official static `arm` tgz from pkgs.tailscale.com, `TS_VERSION`/`TS_SHA256` pinned in source; `fetch.sh` refuses a sha mismatch. Binaries + statedir in `/data` (survives OTA). Flags: `tailscaled --statedir=<TS_STATEDIR> --socket=<TS_SOCK> --port=<TS_PORT> --tun=tailscale0`; `tailscale up --hostname=<TS_HOSTNAME> --advertise-tags=<TS_TAG> --accept-dns=false --netfilter-mode=off` (netguard stays the only iptables writer). | `robot/tailscale/fetch.sh` |
| D3 | Node identity `[USER]` in the admin console: ACL `tagOwners` for `TS_TAG`; the default allow-all grant is replaced by `autogroup:member` → `autogroup:self:*`, `autogroup:member` → `TS_TAG:*` and `TS_TAG` → `wsl-ubuntu` tcp `80,443,8084,8091,8093` only; one-off pre-approved tagged auth key; key expiry disabled; revoke the node if the robot is lost. Never `tailscale serve`/`funnel`. | admin console |
| D4 | Auth key lives at `TS_AUTHKEY` (mode 600); it reaches the robot only on ssh stdin into `/run`, is used as `--auth-key=file:<run path>` and deleted in the same phase; never in laptop argv. | `secrets/README.md` row; `scripts/tests/secrets.sh` check-ignore list |
| D5 | Boot unit `TS_UNIT` via `etc_put` + daemon-reload + enable: `After=network-pre.target myvector-netguard.service`, `RequiresMountsFor=/data`, `Restart=on-failure`, `WantedBy=multi-user.target`. `apply.sh` runs `tailscale up` only when not logged in and `TS_AUTHKEY` exists, else `WARN:`. | `robot/tailscale/apply.sh` |
| D6 | Extends K6: `settings.sh` order … → epod → **tailscale** → netguard (tailnet path exists before wlan0 closes); `ship.sh` `ls-tree` list gains `robot/tailscale`. | `robot/firmware/{settings.sh,ship.sh}` |
| D7 | Supersedes N1 (address split): hosts pin = host of `ROBOT_TTS_URL`, which moves to the laptop tailnet IP (the robot's TTS fetch goes over the tunnel too); wlan0 SSH allow = `PC_LAN_IP`; `ROBOT_IP` = robot tailnet IP, `ROBOT_LAN_IP` = its wlan0 address (reach checks). All four in `robot/robot.env`; missing or non-IPv4 → SETUP 2. ⚠ hedge: the docker deploy's rsync `:1873` reaching `ROBOT_IP` over WSL `tailscale0`. | `robot/netguard/apply.sh`; `robot/robot.env.example` |
| D8 | Supersedes N2 for the D shape (no more tcp 22 from anyone, no more all-ports from `pc_ip`). `MYVECTOR_IN` (still jumped from `INPUT -i wlan0` only) v4: ESTABLISHED,RELATED; tcp 22 `-s PC_LAN_IP`; udp `--dport TS_PORT`; udp 67→68; icmp echo-request; DROP. v6: ESTABLISHED,RELATED; ipv6-icmp; udp 546; udp `--dport TS_PORT`; DROP (no v6 :22: the PC is v4-only on the LAN). | `robot/netguard/rules.sh` |
| D9 | `tailscale0` gets no `MYVECTOR_IN` hook: the WireOS base `INPUT` (policy DROP) accepts its listed ports (22, 443, 1873, 8888–8889, …) on any interface; the D3 ACL is the tailnet gate. | — |
| D13 | Base `OUTPUT` is policy DROP (egress only `lo`/`wlan0`/`tether`): netguard renders `-A OUTPUT -o tailscale0 -j ACCEPT` (v4+v6) and its `undo` removes it. Spike (1.3 → 2.x): `[USER]` adds the same pair by hand, runtime only. | `robot/netguard/rules.sh` |
| D10 | Extends N3/N5: `fw` refuses unless `${SSH_CLIENT%% *}` ∈ {`PC_LAN_IP`, laptop tailnet IP} **and** a fresh ssh to `ROBOT_IP` works before `arm`. After apply: via `ROBOT_IP` ssh ok + `:8888` 200; via `ROBOT_LAN_IP` ssh ok + `:8888` 000; any miss → `undo`. `--selftest` decoy renders D8 with `PC_LAN_IP` = decoy → LAN ssh must be refused while tailnet ssh works. `NETGUARD_OK` holds `PC_LAN_IP`. | `robot/netguard/apply.sh` |
| D11 | Laptop guard: robot tailnet IP joins `tailnet4` (+`tailnet6`) at 2.2; robot LAN IP leaves `allowed4` at 4.5 (re-add for a BLE re-setup after a factory reset). | `scripts/wire-pod-guard.nft` |
| D12 | Spike gate (pre-registered, from the public plan): GO iff robot joins the tailnet **and** a voice turn works with `escapepod.local` → laptop tailnet IP **and** `tailscaled` VmRSS < `RSS_CAP` **and** no vic-* restart in a `SOAK_MIN` soak. Stopping rule: the first failed clause is NO-GO — no tuning, retry or extra arm; run 2.6 undo and stop. | Stage 2.6 |

## Constants (source is the home; values below)

| symbol | value | home / validated by |
|---|---|---|
| `TS_VERSION` / `TS_SHA256` | 1.104.5 (stable 2026-11-10); sha256 = its `.sha256` on pkgs.tailscale.com | `robot/tailscale/fetch.sh`; `robot/tailscale/tests/fetch.sh` |
| `TS_DIR` / `TS_STATEDIR` | `/data/myvector/tailscale/` / `/data/myvector/tailscale/state/` | `robot/tailscale/tests/apply.sh` |
| `TS_SOCK` | `/run/tailscale/tailscaled.sock` | same |
| `TS_UNIT` | `myvector-tailscaled.service` | same |
| `TS_PORT` | 41641 (udp) | `robot/netguard/tests/rules.sh` |
| `TS_TAG` / `TS_HOSTNAME` | `tag:vector` / `vector` | admin console (D3) |
| `TS_AUTHKEY` | `secrets/tailscale-authkey` | `git check-ignore -v secrets/tailscale-authkey` |
| `SOAK_MIN` / `RSS_CAP` | 30 min / 40 MB (40960 kB) | `robot/tailscale/soak.sh`; `robot/tailscale/tests/soak.sh` |
| `PC_LAN_IP` | 192.0.2.249 (router-reserved) | `robot/robot.env` (example holds a placeholder) |
| `ROBOT_LAN_IP` | 192.0.2.76 (today's `allowed4`) | `robot/robot.env` |
| laptop / robot tailnet IP | `tailscale ip -4` in WSL / on the robot, at execution | `ROBOT_TTS_URL` host / `ROBOT_IP` + `tailnet4` |
| work dir (laptop, gitignored) | `robot/work/tailscale/` | `git check-ignore -v robot/work/tailscale/x` |

## Stage 1 — spike: tailscaled by hand (no sound)

Focus: `robot/tailscale/fetch.sh` (new); `secrets/`.

| slice | edit sites | falsifier (red demo first) | verify |
|---|---|---|---|
| 1.1 | `[USER]` admin console per D3; key → `(umask 077; cat >secrets/tailscale-authkey)` | `git check-ignore -q secrets/tailscale-authkey && [ "$(stat -c %a secrets/tailscale-authkey)" = 600 ]` | `bash scripts/tests/secrets.sh` |
| 1.2 | `fetch.sh`: download tgz + `.sha256` into `robot/work/tailscale/`, check against `TS_SHA256`, unpack `tailscale`+`tailscaled` | `robot/tailscale/tests/fetch.sh` (fake `curl` serving a local tgz): wrong `TS_SHA256` → exit 1, nothing unpacked. Red: skip the compare → FAIL | `bash robot/tailscale/tests/fetch.sh`; live `robot/tailscale/fetch.sh` exit 0 |
| 1.3 | `[USER]` silent: `scp` binaries to `TS_DIR`; `setsid tailscaled` per D2 (log in `/run/myvector/`); key via stdin → `tailscale up` per D2/D4; robot `rm` of the key copy | robot `tailscale --socket=<TS_SOCK> status --json \| grep -q '"BackendState": "Running"'`; laptop `tailscale ping -c 3 <robot-ts>` and robot `tailscale ping -c 3 <laptop-ts>` exit 0; robot `iptables -S \| grep -c '^-N ts-'` = 0 (`--netfilter-mode=off` held); robot `ls /run/myvector/` has no key; D7 hedge (non-gating): a deploy-image container `nc -zv -w3 <robot-ts> 1873` → result into D7 | `[USER]` console: node tagged `tag:vector`, expiry disabled |

## Stage 2 — spike gate (D12)

Focus: `robot/tailscale/soak.sh` (new, `silent=1`); `scripts/wire-pod-guard.nft`; a scratch `robot/work/robot.env.ts`.

| slice | edit sites | falsifier (red demo first) | verify |
|---|---|---|---|
| 2.1 | `soak.sh [min]`: every 60 s over ssh: `VmRSS` of `pidof tailscaled`, `MemAvailable`, `tailscale0` UP, `systemctl show -p MainPID` of each `vic-*` unit; prints max RSS / min MemAvailable; exit 1 on RSS ≥ `RSS_CAP`, any vic-* MainPID change or `tailscale0` gone | `robot/tailscale/tests/soak.sh` (fake ssh replaying samples, interval 0): an RSS sample 41000 kB → exit 1; a vic-anim MainPID change → exit 1; clean samples → 0. Red: compare with `>` against a cap of 0 → FAIL | `make check` (2.1 also plugs `tailscale-{fetch,soak}` into `check.sh` like `netguard-$t`) |
| 2.2 | robot tailnet IP → `tailnet4` (+`tailnet6`); commit; `[USER]` `make server-pin` | `scripts/tests/guard.sh`; `sudo nft list set inet wirepod_guard tailnet4 \| grep -q <robot-ts>` | `make check` |
| 2.3 | `[USER]` silent temp pin: copy `robot.env` → `robot/work/robot.env.ts` with `ROBOT_TTS_URL=http://<laptop-ts>:8091`; `ROBOT_ENV=robot/work/robot.env.ts robot/netguard/apply.sh` (pin only: `NETGUARD_OK` ≠ that host → `WARN`, no fw) and `ROBOT_ENV=… robot/firmware/voice-push.sh <voice> <slow>` | robot `grep -q '^<laptop-ts> escapepod.local' /etc/hosts`; robot `curl -sk -m4 -o /dev/null -w '%{http_code}' https://escapepod.local/ok` = 200; robot `/data/data/myvector-voice.json` url host = laptop-ts | — |
| 2.4 | `[USER][SOUND]` one voice turn (wake word + a question) | vic-cloud honours `/etc/hosts` (N4 hedge): WSL `ss -Htn state established '( sport = :443 or sport = :8084 )'` shows `<robot-ts>` and no `ROBOT_LAN_IP`; wire-pod log has the turn's STT + reply; wire-pod's robot IP (`vars.BotInfoPath`, `ip_address`) = `<robot-ts>` (the `jdocspinger` mDNS browse must not flip it back to LAN) | `robot/quiet/scan.sh '<start>'` exit 0 |
| 2.5 | `[USER]` silent `robot/tailscale/soak.sh 30` (connman unknown: `tailscale0` must survive the window) | exit 0; re-check `ip_address` = `<robot-ts>` after the soak | — |
| 2.6 | GO/NO-GO per D12. GO → Stage 3. NO-GO → `[USER]`: kill tailscaled, `rm -r` `TS_DIR`, revoke the node, plain `robot/netguard/apply.sh` (pin back), revert 2.2 + `make server-pin`; rewrite Status to NO-GO + the failed clause, archive this plan, one §OWED[USER] line in `docs/backlog.md` for the C pick | NO-GO undo: robot `grep -c '# my-vector' /etc/hosts` = 1 with the LAN host; `pidof tailscaled` exit 1 | verdict + numbers in the commit message |

## Stage 3 — productise install

Focus: `robot/tailscale/{apply.sh,tests/apply.sh}`; `robot/firmware/{settings.sh,ship.sh}`; `scripts/check.sh`; `secrets/README.md`; `scripts/tests/secrets.sh`.

| slice | edit sites | falsifier (red demo first) | verify |
|---|---|---|---|
| 3.1 | `apply.sh` (`silent=1`, `etc.sh` heredoc): binaries to `TS_DIR` when their sha differs (from `fetch.sh`'s output), `TS_UNIT` per D5, `tailscale up` per D4/D5 | `robot/tailscale/tests/apply.sh` (netguard `fakes.sh` pattern, `ETC_ROOT`): unit text = D5; 2nd run writes nothing; the fake ssh argv log never contains the key value; no key + logged out → `WARN:` exit 0; no binaries in `robot/work/tailscale/` → exit 2. Red: pass the key in argv → FAIL | `make check` |
| 3.2 | D6 wiring; `check.sh` `tailscale-apply` line; D4 README row + check-ignore entry | `awk '/tailscale\/apply.sh/{t=NR}/netguard\/apply.sh/{n=NR}END{exit !(t&&n&&t<n)}' robot/firmware/settings.sh`; `grep -q 'robot/tailscale' robot/firmware/ship.sh` | `make check` |
| 3.3 | `[USER]` silent: stop the hand-run tailscaled, `robot/tailscale/apply.sh`; then `[USER][SOUND]` reboot (boot ordering unknown) | ≤ 3 min after boot: robot `systemctl is-active myvector-tailscaled` and `systemctl show -p NRestarts myvector-tailscaled` = 0; laptop `tailscale ping -c 3 <robot-ts>` exit 0; robot `ip -o link show tailscale0 \| grep -q UP` | `robot/quiet/scan.sh '<start>'` |

## Stage 4 — netguard D shape

Focus: `robot/netguard/{apply.sh,rules.sh,tests/}`; `robot/robot.env.example`; `scripts/wire-pod-guard.nft`.

| slice | edit sites | falsifier (red demo first) | verify |
|---|---|---|---|
| 4.1 | D7 inputs + SETUP checks; example gains `PC_LAN_IP`, `ROBOT_LAN_IP` and the tailnet comments on `ROBOT_IP`/`ROBOT_TTS_URL` | `robot/netguard/tests/hosts.sh`: pin = `ROBOT_TTS_URL` host while `PC_LAN_IP` differs; missing `PC_LAN_IP` → exit 2. Red: pin from `PC_LAN_IP` → FAIL | `make check` |
| 4.2 | `rules.sh <ssh-from-ipv4> <ts-port>` renders D8 | `robot/netguard/tests/rules.sh` (fake iptables argv): D8 order, DROP last; the only tcp 22 rule carries `-s <ssh-from>`; no all-ports `-s … -j ACCEPT`; udp `TS_PORT` on v4+v6; v6 has no tcp 22; `INPUT` never names `tailscale0`; D13 `OUTPUT -o tailscale0 -j ACCEPT` on v4+v6, removed by `undo`. Red: render 22 without `-s` → FAIL | `make check` |
| 4.3 | D10 in `fw`/`--selftest`/`confirm`; `NETGUARD_OK` = `PC_LAN_IP` | `robot/netguard/tests/apply.sh` (fake ssh/curl per address): LAN `:8888` 200 after apply → undo, no `confirm`; decoy phase with LAN ssh still accepted → FAIL; tailnet ssh down → refuse before `arm`; `SSH_CLIENT` neither IP → refuse. Red: skip the tailnet pre-check → FAIL | `make check` |
| 4.4 | `[USER]` silent: `robot/robot.env` per D7 (`ROBOT_IP`=robot-ts, `ROBOT_LAN_IP`, `PC_LAN_IP`, `ROBOT_TTS_URL`=`http://<laptop-ts>:8091`) | `robot/netguard/apply.sh` exit 0 with the pin on laptop-ts; robot voice file url host = laptop-ts | — |
| 4.5 | D11: drop the robot LAN IP from `allowed4`; commit; `[USER]` `make server-pin` | `scripts/tests/guard.sh`; `! sudo nft list set inet wirepod_guard allowed4 \| grep -q 192.0.2.76` | `make check` |

## Stage 5 — full verify + live e2e

| slice | edit sites | falsifier (red demo first) | verify |
|---|---|---|---|
| 5.1 | — | `make check` | `[USER]` `make ship` (from main) green; `[USER]` `robot/netguard/apply.sh --selftest` PASS (decoy bit) |
| 5.2 | — | `[USER]` e2e, ranked: (1) laptop `curl -m3 http://<ROBOT_LAN_IP>:8888/consolevars` FAILS; (2) `ssh -o ControlPath=none root@<ROBOT_LAN_IP> true` from the PC works; (3) over `ROBOT_IP`: ssh works, `:8888` = 200, wire-pod SDK `:443` established to robot-ts (`ss`); (4) `[SOUND]` voice turn + `scan.sh`; (5) `robot/tailscale/soak.sh 30` under the unit exit 0 | robot `iptables -S MYVECTOR_IN` = rendered D8 |
| 5.3 | — | `Agent(subagent_type="code-reviewer")` over this plan's commits | archive per `plan-hygiene.md`; owed live runs → §OWED[USER] |
