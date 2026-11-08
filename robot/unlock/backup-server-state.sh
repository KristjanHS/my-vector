#!/usr/bin/env bash
# Copies wire-pod's private state (secrets/server/: keys, jdocs, certs, settings) + chipper/source.sh into the gitignored secrets/server-state/.
set -euo pipefail
umask 077 # the archive holds keys
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$root/secrets/server-state" && chmod 700 "$root/secrets"
out=$root/secrets/server-state/server-state-$(date +%Y%m%d-%H%M%S).tgz
items=()
for p in secrets/server chipper/source.sh; do
  [[ -e $root/$p ]] && items+=("$p")
done
((${#items[@]})) || { echo "FAIL: no bot state found under $root/secrets/server" >&2; exit 1; }
tar -czf "$out" -C "$root" "${items[@]}"
tar -tzf "$out" >/dev/null
echo "OK: $out (${items[*]})"
