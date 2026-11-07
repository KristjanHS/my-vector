#!/usr/bin/env bash
# Load wire-pod-guard.nft as root from the commit in robot/work/server-sha (the tree's copy while unpinned),
# so wire-pod (run-server.sh) and myvector-tts (ExecStartPre) load the same rules.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
pin=$(cat "$repo/robot/work/server-sha" 2>/dev/null || true)
if [[ -n $pin ]]; then
  git -c safe.directory="$repo" -C "$repo" show "$pin:scripts/wire-pod-guard.nft" | /usr/sbin/nft -f -
else
  /usr/sbin/nft -f "$repo/scripts/wire-pod-guard.nft"
fi
