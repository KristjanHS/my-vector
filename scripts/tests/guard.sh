#!/usr/bin/env bash
# wire-pod-guard.nft loaded in a throwaway user+net namespace: the tree copy's rules equal HEAD's (normalised `nft list table`).
set -euo pipefail
dir=$(cd "$(dirname "$0")/../.." && pwd)
nft=/usr/sbin/nft
unshare -rn true 2>/dev/null || { echo "guard test skipped: no unprivileged unshare"; exit 0; }
load() { unshare -rn sh -c "$nft -f - && $nft list table inet wirepod_guard"; }
tree=$(load <"$dir/scripts/wire-pod-guard.nft")
head=$(git -C "$dir" show HEAD:scripts/wire-pod-guard.nft | load)
if ! diff <(echo "$head") <(echo "$tree"); then echo "FAIL guard: tree rules differ from HEAD" >&2; exit 1; fi
echo "guard OK: tree rules = HEAD"
