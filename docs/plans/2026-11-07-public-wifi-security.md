# Vector on public WiFi: security plan (research, not implemented)

Status: **research only, nothing implemented.** Written 2026-11-07. Covers two scenarios:

- **S1:** Vector and the laptop (wire-pod in WSL2) are both on the same public WiFi (café, hotel, conference).
- **S2:** Vector is on public WiFi and the laptop stays at home behind the home router.

## TL;DR

1. **The current setup is not safe on any network you don't control, and not fully safe at home either** (any guest on
   the home WiFi has the same access). Verified on the robot 2026-11-07: (a) root SSH accepts exactly one key, the public
   `victor-dev@anki` key; (b) three unauthenticated web consoles (`:8080` WireOS settings, `:8888`/`:8889` Anki
   console vars and functions, `:8890` cloud debug) listen on all interfaces, with iptables all-ACCEPT; (c) the server's
   TLS key is public, so anyone can pretend to be the server; (d) laptop `:443`/`:8084` have no firewall rule; (e) the
   guard's IP allowlist means nothing on shared WiFi.
2. **Yes, a tunnel is the right answer.** Vector should never talk to anything on the public network except the tunnel's
   far end. Where the tunnel ends matters less than that Vector never shares an L2 network with strangers.
3. **Recommended:** a **travel router** (for example a GL.iNet running OpenWrt, which has WireGuard and Tailscale built in)
   that carries Vector on its own private WPA2 SSID. It works for S1 (laptop joins the same private SSID) and for S2
   (router tunnels to home over WireGuard or Tailscale). It needs no firmware work, and it also fixes captive portals and
   client isolation, which would otherwise stop Vector from working on most public WiFi at all.
   **Free stopgap for S1:** a phone hotspot or the laptop's Windows Mobile Hotspot.
4. **Most mature end state:** Tailscale on the robot itself (option D), plus robot iptables dropping everything inbound
   on `wlan0`. Vector is then safe on *any* network, home included. **Now looks feasible:** kernel 4.9.277 has
   `CONFIG_TUN=y` and `/dev/net/tun`, iptables is present, about 220 MB RAM is free, and `/data` has 637 MB free. Tailscale
   relays get past client isolation; only captive portals still need a phone hotspot or a travel router.
5. **Baseline hardening is owed in every option** (replace the SSH key, guard `:443`/`:8084`, robot-side firewall). It is
   cheap and also makes the home LAN safer.

## What is exposed today

Laptop facts come from this repo's code. Robot facts come from a read-only SSH census on 2026-11-07 (`netstat`,
`sshd_config`, `/proc/config.gz`, `curl 127.0.0.1:<port>/`).

| # | Surface | Where | Today's protection | On a shared network |
|---|---------|-------|--------------------|---------------------|
| T1 | **Robot root SSH** `:22` (all interfaces) | robot | `PasswordAuthentication no`, root password locked; `/etc/ssh/authorized_keys` holds **one key: `victor-dev@anki`, SHA256:9lZMgxdf…**, the private half of which is `robot_sshkey`, **tracked in the public `os-vector/wire-os-victor` repo** | **Anyone on the same WiFi can become root on Vector**: camera, 4 mics, motors, stored tokens. Worst item. `/etc` is on the read-only rootfs, so the key change ships via firmware (or `AuthorizedKeysFile` pointing into `/data`). |
| T2 | **Server identity** | robot → `escapepod.local:443` | TLS, but the bot trusts the DDL `escapepod.local` cert, and its private key `chipper/epod/ep.key` is **in the public wire-pod repo** (and ours). The name is resolved over mDNS, which anyone can spoof (first answer wins). | An attacker answers mDNS for `escapepod.local` and runs their own server with the public key. They then receive every voice stream, can push jdocs/settings, and issue tokens. **TLS gives no protection here.** |
| T3 | **chipper gRPC** `:443`, `:8084` | laptop | TLS with the public key above; **no nft rule** (the guard covers only 80/8080/8091); no client auth | Anyone can send STT/intent requests (costs LLM/search quota) and talk to the token server. The token server identifies robots **by source IP** (`token.go` `CreateJWT` → `GetEsnFromTarget(ipAddr)`). |
| T4 | **wire-pod web API** `:80`, `:8080` | laptop | nft guard: allowlist `192.168.1.76` + tailnet devices on `tailscale0` | Unauthenticated, and returns the LLM key. On another network Vector gets a different IP, so the guard must be edited. Worse, a source-IP allowlist on shared WiFi is **spoofable** (ARP or IP spoofing). |
| T5 | **TTS fetch** `:8091` | robot → laptop | firmware patch 0010 requires `http://` (plain HTTP/1.0 POST) | An attacker in the path can inject any audio Vector speaks, and can read every reply text. |
| T6 | **Robot SDK gateway** `:443` (vic-cloud, all interfaces) | robot | GUID token (issued by wire-pod) | Medium: the token can leak via T2 or T3. With the token: full SDK control, including camera and audio. |
| T7 | **Robot debug consoles**, all on `0.0.0.0`/`::` | robot | **none**: HTTP 200 with no auth; iptables policy ACCEPT everywhere | `:8080` wired "Robot Settings / Mods" page · `:8888` vic-engine and `:8889` vic-anim "Anki Webservices" (Console Vars **and Functions**: change engine and animation behaviour live) · `:8890` vic-cloud debug ("Connect tests") · UDP `:5102`/`:5103`/`:60947` vic-engine (unknown protocol) · `:5353` avahi. Roughly as bad as T1, with no key needed. |
| T8 | **Windows firewall profile** | laptop | Under WSL mirrored networking, the Hyper-V firewall plus a "Public" network profile block inbound by default | Not a security hole, but S1 *breaks*: Vector can't reach `:443` unless a rule allows it. Don't "fix" this by opening it to the whole public subnet. |

Practical blockers on public WiFi, separate from security:

- **Captive portals.** Vector has no browser and can't click through a hotel or café login page.
- **Client isolation.** Most public or guest WiFi blocks client-to-client traffic, so in S1 Vector often can't reach the
  laptop at all, even on the same SSID.
- **WPA2-Enterprise / 802.1X** networks: probably not supported by Vector's BLE WiFi setup (untested).
- Every new network means a BLE WiFi re-setup, plus the guard IP change.

Together these make "both on the public WiFi directly" unreliable *and* unsafe. That points to giving Vector a private
network that you carry with you.

## Options

### A. Same public WiFi, harden in place (no tunnel)

Fix T1–T7 at the application layer. Put an own CA/cert in firmware so the bot pins *our* key instead of the public DDL
one, pin `escapepod.local` in `/etc/hosts` instead of mDNS, add auth to the web API, switch TTS to HTTPS, and firewall
both ends.

- **Gains:** nothing extra to carry.
- **Costs:** the most firmware patching (vic-cloud trust store, patch 0010 TLS client, hosts pinning), and the IP-based
  token logic stays fragile. Captive portals and client isolation still stop it from working on most public WiFi.
- **Verdict:** not recommended as the main approach. Parts of it (cert pinning, `/etc/hosts`) are worth keeping as
  defence in depth.

### B. Phone hotspot or laptop hotspot (free stopgap, S1 only)

Vector and the laptop both join a WPA2 network you own: the phone's hotspot, or Windows Mobile Hotspot on the laptop
(the laptop stays on the public WiFi upstream and shares a separate private SSID). Strangers are never on Vector's L2
segment.

- **Gains:** zero cost. Windows' hotspot subnet is fixed at `192.168.137.0/24`, so the guard and setup stay stable.
- **Costs:** phone data, or the laptop's WiFi adapter must support hosted network alongside a client connection. Vector
  needs one BLE WiFi setup per hotspot (once, then it's a saved network). The laptop itself is still on public WiFi, so
  the guard must stay tight (bind the allowlist to the hotspot interface, not just an IP).
- **Doesn't cover S2.**

### C. Travel router (recommended)

A pocket OpenWrt router (GL.iNet Beryl AX / Slate class, about €40–120). It joins the public WiFi upstream (repeater
mode, handles the captive portal in the router UI, can clone a MAC) and broadcasts **your own SSID**. To Vector it looks
like the same network everywhere, so it's set up once over BLE, and the DHCP reservation stays fixed.

- **S1:** the laptop also joins the travel router's SSID, so this is effectively the home LAN on the road. The guard
  allowlist keeps one fixed IP.
- **S2:** the travel router runs a **WireGuard client to home** (home router as WG server, or the laptop/WSL itself), or
  joins the **tailnet** (GL.iNet firmware ships a Tailscale client and can act as a subnet router). Vector reaches
  `escapepod.local` → home laptop through the tunnel. Fail-closed "kill switch" mode keeps Vector's traffic from leaking
  onto the public network if the tunnel drops.
- **Gains:** no robot or firmware changes. Fixes captive portals, client isolation and the re-setup-per-network problem.
  Mature, widely used hardware.
- **Costs:** one more device to carry and power. Name resolution in S2 needs care: Vector looks up `escapepod.local`
  over mDNS, and mDNS doesn't cross a routed tunnel. Either pin it on the robot (`/etc/hosts` → laptop's tailnet/WG IP)
  or have the router answer for it. The robot's nsswitch is `files myhostname mdns4_minimal dns`, so an `/etc/hosts`
  entry wins over mDNS. Voice latency over WireGuard is fine; Tailscale
  DERP relay (when NAT traversal fails) adds latency. [verify on hotel networks]

### D. Tunnel on the robot itself (most mature end state; spike first)

Run WireGuard or Tailscale **on Vector**. The laptop is already on the tailnet (`tailscale0` in WSL). Vector then talks
only to the laptop's tunnel IP, on any network, home included, and nothing needs to listen on `wlan0` at all.

- **Gains:** zero-trust on any network; the guard becomes "tailnet only"; S2 works with no port forwarding.
- **Robot facts (census 2026-11-07):** kernel `4.9.277-wrynose-perf`, `CONFIG_TUN=y`, `/dev/net/tun` present, so kernel
  TUN mode works and userspace-proxy mode isn't needed. There is no in-kernel WireGuard (that arrived in 5.6), so it's
  `tailscaled` or `wireguard-go`. `iptables`/`ip6tables` are present (no `nft`), and all policies are ACCEPT. RAM is 474 MB
  total, about 220 MB available, no swap. The rootfs is read-only ext4; `/data` is rw with 637 MB free.
- **Remaining unknowns (spike):** `tailscaled` resident memory next to vic-engine/vic-anim (budget ~40 MB); startup ordering
  with connman; keeping the tailnet node key in `/data` (tag the node, turn key expiry off, revoke if the robot is lost);
  vic-cloud reaching `escapepod.local` → laptop tailnet IP via `/etc/hosts` (ship through `ship.sh`, rootfs is RO).
- **WireGuard variant:** `wireguard-go` + `wg` + iproute2 6.19 (present) works on the same TUN, but needs a reachable UDP
  endpoint (home port-forward + DDNS, or a WG server on the home router): no NAT traversal or relay, so it fails in S1 under client isolation.
- **Robot firewall:** iptables INPUT on `wlan0` → allow ESTABLISHED, DHCP and Tailscale's UDP; drop the rest
  (22/443/8080/8888–8890/5102–5103). SSH and SDK are reachable on `tailscale0` only.
- **Doesn't fix** captive portals (the robot's WiFi still has to get online), so a phone hotspot or travel router is still
  needed on networks with a login page. **Does work through client isolation**: traffic goes via the internet, or a DERP
  relay when direct paths are blocked (slower, but fine for voice).

### Rejected: port-forward wire-pod at home to the internet (S2)

Exposes the unauthenticated `:80/:8080` API (which leaks the LLM key) and a gRPC server whose TLS key is public. Never do
this. The same goes for `tailscale funnel` (the guard file already warns: serve/funnel bypasses the address checks via
`lo`).

## Comparison

| | A harden-in-place | B hotspot | C travel router | D on-robot tunnel |
|---|---|---|---|---|
| Safe from other WiFi users | partly | yes | yes | yes |
| Works through client isolation | no | yes | yes | yes (relay) |
| Works behind a captive portal | no | yes | yes | no (needs B/C) |
| Protects at home too | partly | no | no | yes |
| S2 (laptop at home) | no | no | yes | yes |
| Firmware work | heavy | none | none (maybe `/etc/hosts`) | medium (binary in `/data`, init hook, hosts, iptables) |
| Money | 0 | 0 | ~€40–120 | 0 |
| Re-setup per network | yes | once per hotspot | never | yes |

## Recommended plan (stages, cheapest visible first)

0. **Baseline, all options, also improves home.** Do this before Vector leaves the house.
   → plan: `2026-11-07-wifi-baseline-hardening.md (docs/plans/)` (0.1–0.4 + T2 server key; D follows it).
   - **0.1** Replace the robot's root SSH key with a freshly generated key of our own (keep the old public repo key
     out of `authorized_keys`), and turn password login off. Keep a recovery path: confirm the new key works in a
     second session before removing the old one. Update `ROBOT_KEY` in `robot/robot.env`.
   - **0.2** Extend `scripts/wire-pod-guard.nft` to `:443` and `:8084` (Vector + localhost + tailnet only), so chipper
     isn't open to the LAN.
   - **0.3** Robot iptables, shipped via firmware: on `wlan0` allow only 22 (until D lands) plus what wire-pod needs on
     the robot (`:443` SDK gateway); drop `:8080`, `:8888`–`:8890` and UDP `:5102`/`:5103`/`:60947`. Falsifier: from the
     laptop, `curl -m3 http://<robot>:8888/` must fail and `ssh` must still work.
   - **0.4** Pin `escapepod.local` on the robot (`/etc/hosts` via firmware ship) so mDNS spoofing no longer works.
     Needed anyway for C-S2 and D.
1. **S1 now:** use B (phone or laptop hotspot). Add the hotspot SSID to Vector over BLE and add the hotspot subnet to the
   guard, bound to that interface.
2. **S1 + S2 properly:** buy a C travel router. Set Vector up on its SSID, reserve the IP, and set up WireGuard or
   Tailscale to home, with the kill switch on.
3. **D spike** (TUN confirmed, so it's no longer optional-later): `tailscaled` armv7 static binary in `/data`, started
   by hand. Pre-registered rule: robot joins the tailnet **and** a voice turn works with `escapepod.local` → laptop
   tailnet IP **and** `tailscaled` RSS < 40 MB with no vic-* crash in 30 min → productise (init hook, iptables, hosts).
   Otherwise → C.
4. **Defence in depth (from A), if wanted:** our own server cert pinned in firmware (closes T2 even on the home LAN),
   HTTPS or a shared-secret header for TTS `:8091` (T5), and auth on the web API (T4).

## Open questions for the user

- Which scenario is real first: S1 (café with the laptop) or S2 (laptop at home)? This decides whether stage 1 or 2 comes first.
- OK to buy a travel router? If so, does the home router support a WireGuard server, or should the tunnel end at the
  laptop's existing Tailscale?
- Windows side (user runs it, WSL interop is off): the current network profile and Hyper-V firewall rules for WSL, e.g.
  `Get-NetConnectionProfile` and `Get-NetFirewallHyperVRule` in an admin PowerShell, to see what S1 inbound would hit.

## Sources

- Tailscale userspace mode when TUN is missing: [OpenWrt Tailscale userspace guide](https://fl0w1nd.github.io/openwrt-tailscale/en/guide/userspace-mode),
  [Tailscale forum: TUN on IoT devices](https://forum.tailscale.com/t/can-i-copy-binary-for-tun-kernel-module-onto-iot-device-and-load-it-manually-to-run-tailscale/549),
  [tailscale#634](https://github.com/tailscale/tailscale/issues/634)
- mDNS spoofing on a LAN: [GNUCitizen: mDNS poisoning](https://www.gnucitizen.org/blog/name-mdns-poisoning-attacks-inside-the-lan/),
  [Horizon3 H3-2021-0033](https://docs.horizon3.ai/knowledge_base/weaknesses/H3-2021-0033/)
- wire-pod relies on `escapepod.local` resolving: [wire-pod troubleshooting wiki](https://github.com/kercre123/wire-pod/wiki/Troubleshooting)
- Travel router + WireGuard to home: [GL.iNet: WireGuard home server with two routers](https://docs.gl-inet.com/router/en/4/tutorials/build_your_own_wireguard_home_server_with_two_glinet_routers/),
  [GL.iNet setup: VPN, DNS, guest network](https://privacygear.pages.dev/en/guides/glinet-travel-router-setup/),
  [GL-MT3000 review (subnet routing to home devices)](https://jalcocert.github.io/JAlcocerT/travel-router-gl-mt3000-review)
- Client isolation on public WiFi: [Antamedia: wireless client isolation](https://explore.antamedia.com/explore/configure-wireless-client-isolation),
  [Purple: is hotel WiFi safe](https://www.purple.ai/guides/is-hotel-wifi-safe)
- WireOS overview: [os-vector docs: WireOS](https://os-vector.github.io/vector-docs/2.%20Available%20CFW/1.%20WireOS.html)
- Not found online: any write-up of Vector/WireOS on a VPN, or of wire-pod's security on untrusted networks. The T1–T7
  findings come from this repo's code, not from published advisories.
