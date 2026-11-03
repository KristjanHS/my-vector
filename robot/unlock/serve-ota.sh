#!/usr/bin/env bash
# Serves robot/ota/ over plain HTTP (recovery can't download HTTPS) so the robot installs the copy you verified.
# Run verify-ota.sh first. Stop with Ctrl-C once the robot has finished downloading.
set -euo pipefail
ota=$(cd "$(dirname "$0")/../ota" && pwd)
port=${PORT:-8123}
ip=$(ip -4 route get 1.1.1.1 | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}') # LAN-facing source IP, not just the first one listed
[[ -n $ip ]] || { echo "FAIL: no default route; is Wi-Fi up?" >&2; exit 1; }
echo "In websetup's terminal, run one of:"
for f in "$ota"/unlock-prod/Unlock-Prod.ota "$ota"/wireos-dev/dev.ota; do
  [[ -f $f ]] && echo "  ota-start http://$ip:$port/${f#"$ota"/}"
done
echo "Check from another device first: http://$ip:$port/ must list the folders."
exec python3 -m http.server "$port" --bind 0.0.0.0 --directory "$ota"
