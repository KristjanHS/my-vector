#!/usr/bin/env bash
# Copies the fork's bot state (keys, jdocs, certs, source.sh) into the gitignored robot/backup/.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
out=$root/robot/backup/server-state-$(date +%Y%m%d-%H%M%S).tgz
items=()
for p in chipper/apiConfig.json chipper/jdocs chipper/session-certs chipper/source.sh; do
  [[ -e $root/$p ]] && items+=("$p")
done
((${#items[@]})) || { echo "FAIL: no bot state found under $root/chipper" >&2; exit 1; }
tar -czf "$out" -C "$root" "${items[@]}"
tar -tzf "$out" >/dev/null
echo "OK: $out (${items[*]})"
