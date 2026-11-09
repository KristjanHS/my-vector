#!/usr/bin/env bash
# Make the robot trust our server CA (T5): ROBOT_TRUST = ca.crt + its current certs while vic-cloud lacks the
# my-vector.PodTrust marker in /anki/etc/vic-cloud.env (dual trust; the vic-cloud binary is UPX-packed), ca.crt alone once it has it. A rewrite restarts anki-robot.target (silent start while the sound lock holds).
# Usage: robot/epod/apply.sh   idempotent; settings.sh runs it. ca.crt: $epod_dir/ (robot/env.sh), else <main>/secrets/epod-new/ (pre-4.4).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"

ca=$epod_dir/ca.crt
[[ -f $ca ]] || ca=${robot_dir%/robot}/secrets/epod-new/ca.crt
[[ -f $ca ]] || { echo "epod: not applied — no ca.crt (run robot/epod/mkcert.sh)"; exit 0; }
CA=$(base64 -w0 "$ca")
quiet_silent_start 300 || exit 1
# shellcheck disable=SC2029 # CA expands on the client: the point
cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" "CA=$CA sh -s"
set -e
trust=/data/data/wirepod-cert.crt anki=/anki/etc/wirepod-cert.crt marker=/anki/etc/vic-cloud.env
[ ! -e "$ETC_ROOT$anki" ] || { echo "FAIL: $anki exists and would override $trust"; exit 1; }
ca=$(echo "$CA" | base64 -d)
case $ca in "-----BEGIN CERTIFICATE-----"*) ;; *) echo "FAIL: ca.crt is not a PEM certificate (CA)"; exit 1 ;; esac
if grep -q my-vector.PodTrust "$ETC_ROOT$marker" 2>/dev/null; then mode=ours new=$ca
else
  mode=dual
  # our CA first, then each distinct cert block already trusted
  new=$({ printf '%s\n' "$ca"; cat "$ETC_ROOT$trust" 2>/dev/null || :; } | awk '
    { sub(/\r$/, "") }
    /-----BEGIN CERTIFICATE-----/ { blk = ""; inb = 1 }
    inb { blk = blk $0 "\n" }
    inb && /-----END CERTIFICATE-----/ { inb = 0; if (!(blk in seen)) { seen[blk] = 1; out = out blk } }
    END { printf "%s", out }')
fi
if [ "$(cat "$ETC_ROOT$trust" 2>/dev/null)" = "$new" ]; then
  echo "epod: already installed ($mode)"
else
  mkdir -p "$(dirname "$ETC_ROOT$trust")"
  printf '%s\n' "$new" >"$ETC_ROOT$trust.new"
  sync
  mv -f "$ETC_ROOT$trust.new" "$ETC_ROOT$trust"
  sync
  # whole stack, as the deploy does: a vic-cloud-only restart leaves switchboard's gateway socket tied to the dead
  # client (EPERM forever, no :443, wire-pod unlinked)
  # a fault-code restart in flight (deploy start hit a unit fault) would race ours, as in crash-review/restarts.sh
  for _ in $(seq 30); do systemctl is-active -q fault-code.service || break; sleep 2; done
  systemctl restart anki-robot.target
  echo "epod: wrote $trust ($mode, $(grep -c 'BEGIN CERTIFICATE' "$ETC_ROOT$trust") certs), restarted anki-robot.target"
fi
EOF
