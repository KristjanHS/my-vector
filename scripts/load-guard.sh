#!/usr/bin/env bash
# Load wire-pod-guard.nft as root from the commit in robot/work/server-sha (the tree's copy while unpinned), defines
# from the tree's guard-nft.sh (make ship checks it equals the pin's; server-pin and restarts do not), so wire-pod (run-server.sh) and myvector-tts (ExecStartPre) load the same rules.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
pin=$(cat "$repo/robot/work/server-sha" 2>/dev/null || true)
if [[ -n $pin ]]; then
  git -c safe.directory="$repo" -C "$repo" show "$pin:scripts/wire-pod-guard.nft" | "$repo/scripts/guard-nft.sh" | /usr/sbin/nft -f -
else
  "$repo/scripts/guard-nft.sh" <"$repo/scripts/wire-pod-guard.nft" | /usr/sbin/nft -f -
fi
