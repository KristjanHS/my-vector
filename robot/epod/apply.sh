#!/usr/bin/env bash
# Make the robot trust our server CA (T5): ROBOT_TRUST = ca.crt + its current certs while vic-cloud lacks the
# my-vector.PodTrust marker (dual trust), ca.crt alone once it has it. No vic-cloud restart: the next deploy does that.
# Usage: robot/epod/apply.sh   idempotent; settings.sh runs it. ca.crt: EPOD_CA, else secrets/epod/, else secrets/epod-new/.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"

ca=${EPOD_CA:-}
for d in "$robot_dir/../secrets/epod" "$robot_dir/../secrets/epod-new"; do
  [[ -n $ca || ! -f $d/ca.crt ]] || ca=$d/ca.crt
done
[[ -n $ca ]] || { echo "epod: not applied — no ca.crt (run robot/epod/mkcert.sh)"; exit 0; }
CA=$(base64 -w0 "$ca")
# shellcheck disable=SC2029 # CA expands on the client: the point
cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" "CA=$CA sh -s"
set -e
trust=/data/data/wirepod-cert.crt anki=/anki/etc/wirepod-cert.crt bin=/anki/bin/vic-cloud
[ ! -e "$ETC_ROOT$anki" ] || { echo "FAIL: $anki exists and would override $trust"; exit 1; }
ca=$(echo "$CA" | base64 -d)
case $ca in "-----BEGIN CERTIFICATE-----"*) ;; *) echo "FAIL: ca.crt is not a PEM certificate (CA)"; exit 1 ;; esac
if grep -q my-vector.PodTrust "$ETC_ROOT$bin" 2>/dev/null; then mode=ours new=$ca
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
  echo "epod: wrote $trust ($mode, $(grep -c 'BEGIN CERTIFICATE' "$ETC_ROOT$trust") certs)"
fi
EOF
