# Vector on public WiFi: security plan (research, not implemented)

Status: **research only, nothing implemented.** Written 2026-11-07. Covers two scenarios:

- **S1:** Vector and the laptop (wire-pod in WSL2) are both on the same public WiFi (café, hotel, conference).
- **S2:** Vector is on public WiFi and the laptop stays at home behind the home router.

## TL;DR

1. **The current setup is not safe on any network you don't control.** It only works because the home LAN is trusted. Four
   issues each make a shared network unsafe: (a) the robot's root SSH key is public, (b) the server's TLS key is public, so
   anyone can pretend to be the server, (c) `:443`/`:8084` have no firewall rule at all, and (d) the guard's IP allowlist
   means nothing on a shared L2 network.
2. **Yes, a tunnel is the right answer.** Vector should never talk to anything on the public network except the tunnel's
   far end. Where the tunnel ends matters less than that Vector never shares an L2 network with strangers.
3. **Recommended:** a **travel router** (for example a GL.iNet running OpenWrt, which has WireGuard and Tailscale built in)
   that carries Vector on its own private WPA2 SSID. It works for S1 (laptop joins the same private SSID) and for S2
   (router tunnels to home over WireGuard or Tailscale). It needs no firmware work, and it also fixes captive portals and
   client isolation, which would otherwise stop Vector from working on most public WiFi at all.
   **Free stopgap for S1:** a phone hotspot or the laptop's Windows Mobile Hotspot.
4. **Most mature end state (option C, later):** Tailscale or WireGuard on the robot itself, so Vector is safe on *any*
   network. This depends on facts we haven't checked yet (TUN support in the 3.x kernel, RAM, read-only rootfs), so it
   gets a spike before any commitment.
5. **Baseline hardening is owed in every option** (replace the SSH key, guard `:443`/`:8084`, robot-side firewall). It is
   cheap and also makes the home LAN safer.

## What is exposed today

Facts below come from this repo's code. Items marked **[verify]** need the robot online. It was unreachable on
2026-11-07 (`No route to host`).

| # | Surface | Where | Today's protection | On a shared network |
|---|---------|-------|--------------------|---------------------|
| T1 | **Robot root SSH** (port 22) | robot | key auth with `ROBOT_KEY=work/wire-os-victor/robot_sshkey`, a file **tracked in the public `os-vector/wire-os-victor` repo** | **Anyone on the same WiFi can become root on Vector**: camera, 4 mics, motors, stored tokens. Worst item. [verify the robot still accepts only that key, and whether password login is off] |
| T2 | **Server identity** | robot → `escapepod.local:443` | TLS, but the bot trusts the DDL `escapepod.local` cert, and its private key `chipper/epod/ep.key` is **in the public wire-pod repo** (and ours). The name is resolved over mDNS, which anyone can spoof (first answer wins). | An attacker answers mDNS for `escapepod.local` and runs their own server with the public key. They then receive every voice stream, can push jdocs/settings, and issue tokens. **TLS gives no protection here.** |
| T3 | **chipper gRPC** `:443`, `:8084` | laptop | TLS with the public key above; **no nft rule** (the guard covers only 80/8080/8091); no client auth | Anyone can send STT/intent requests (costs LLM/search quota) and talk to the token server. The token server identifies robots **by source IP** (`token.go` `CreateJWT` → `GetEsnFromTarget(ipAddr)`). |
| T4 | **wire-pod web API** `:80`, `:8080` | laptop | nft guard: allowlist `192.0.2.76` + tailnet devices on `tailscale0` | Unauthenticated, and returns the LLM key. On another network Vector gets a different IP, so the guard must be edited. Worse, a source-IP allowlist on shared WiFi is **spoofable** (ARP or IP spoofing). |
| T5 | **TTS fetch** `:8091` | robot → laptop | firmware patch 0010 requires `http://` (plain HTTP/1.0 POST) | An attacker in the path can inject any audio Vector speaks, and can read every reply text. |
| T6 | **Robot SDK gateway** `:443` on the robot | robot | GUID token (issued by wire-pod) | Medium: the token can leak via T2 or T3. With the token: full SDK control, including camera and audio. [verify] |
| T7 | **Other robot listeners** | robot | unknown | WireOS may expose debug webservers or console-var ports. [verify: `netstat -tlnp` / `-ulnp` on the robot] |
| T8 | **Windows firewall profile** | laptop | Under WSL mirrored networking, the Hyper-V firewall plus a "Public" network profile block inbound by default | Not a security hole, but S1 *breaks*: Vector can't reach `:443` unless a rule allows it. Don't "fix" this by opening it to the whole public subnet. |

Practical blockers on public WiFi, separate from security:

- **Captive portals.** Vector has no browser and can't click through a hotel or café login page.
- **Client isolation.** Most public or guest WiFi blocks client-to-client traffic, so in S1 Vector often can't reach the
  laptop at all, even on the same SSID.
- **WPA2-Enterprise / 802.1X** networks: probably not supported by Vector's connman setup. [verify]
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
  or have the router answer for it. [verify how Vector resolves `.local`]. Voice latency over WireGuard is fine; Tailscale
  DERP relay (when NAT traversal fails) adds latency. [verify on hotel networks]

### D. Tunnel on the robot itself (most mature end state; spike first)

Run WireGuard or Tailscale **on Vector**. The laptop is already on the tailnet (`tailscale0` in WSL). Vector then talks
only to the laptop's tunnel IP, on any network, home included, and nothing needs to listen on `wlan0` at all.

- **Gains:** zero-trust on any network; the guard becomes "tailnet only"; S2 works with no port forwarding.
- **Unknowns (spike):**
  - Kernel support. Vector's kernel is an old Qualcomm 3.x, with no in-kernel WireGuard. It needs `CONFIG_TUN` for
    `wireguard-go`/`tailscaled`; without it, Tailscale's `--tun=userspace-networking` works only as a SOCKS5/HTTP proxy,
    not a system VPN. Then vic-cloud (Go gRPC, which honours `HTTPS_PROXY`) and patch 0010's HTTP client would have to
    use the proxy. [verify `/dev/net/tun`, `zcat /proc/config.gz`]
  - RAM and disk: `tailscaled` on armv7 is tens of MB resident on a 512 MB robot that already has memory-sensitive
    services.
  - Startup ordering with connman, `/etc/hosts` pinning on a possibly read-only rootfs (bake it in through `ship.sh`), and
    keeping a tailnet key on the robot (key rotation, revocation if the robot is lost).
  - Robot-side firewall (`iptables` present? [verify]): drop everything inbound on `wlan0` except DHCP, allow SSH and SDK
    on the tunnel only.
- **Doesn't fix** captive portals or client isolation on its own (the robot's WiFi still has to get online), so even with
  D, a travel router or hotspot is often still needed on public WiFi. D is what you add when you want *home* LAN
  compromise or a shared flat also covered.

### Rejected: port-forward wire-pod at home to the internet (S2)

Exposes the unauthenticated `:80/:8080` API (which leaks the LLM key) and a gRPC server whose TLS key is public. Never do
this. The same goes for `tailscale funnel` (the guard file already warns: serve/funnel bypasses the address checks via
`lo`).

## Comparison

| | A harden-in-place | B hotspot | C travel router | D on-robot tunnel |
|---|---|---|---|---|
| Safe from other WiFi users | partly | yes | yes | yes |
| Works with captive portal / isolation | no | yes | yes | no (needs B/C too) |
| S2 (laptop at home) | no | no | yes | yes |
| Firmware work | heavy | none | none (maybe `/etc/hosts`) | heavy + unknowns |
| Money | 0 | 0 | ~€40–120 | 0 |
| Re-setup per network | yes | once per hotspot | never | yes |

## Recommended plan (stages, cheapest visible first)

0. **Baseline, all options, also improves home.** Do this before Vector leaves the house.
   - **0.1** Replace the robot's root SSH key with a freshly generated key of our own (keep the old public repo key
     out of `authorized_keys`), and turn password login off. Keep a recovery path: confirm the new key works in a
     second session before removing the old one. Update `ROBOT_KEY` in `robot/robot.env`.
   - **0.2** Extend `scripts/wire-pod-guard.nft` to `:443` and `:8084` (Vector + localhost + tailnet only), so chipper
     isn't open to the LAN.
   - **0.3** Robot `netstat` census (T7) and a robot-side firewall decision, based on what's listening.
   - **0.4** Pin `escapepod.local` on the robot (`/etc/hosts` via firmware ship) so mDNS spoofing no longer works.
     Needed anyway for C-S2 and D.
1. **S1 now:** use B (phone or laptop hotspot). Add the hotspot SSID to Vector over BLE and add the hotspot subnet to the
   guard, bound to that interface.
2. **S1 + S2 properly:** buy a C travel router. Set Vector up on its SSID, reserve the IP, and set up WireGuard or
   Tailscale to home, with the kill switch on.
3. **Optional later:** a D spike (TUN/RAM/rootfs facts first, with a pre-registered rule: no `/dev/net/tun` *and* no
   proxy support in vic-cloud → drop D).
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
