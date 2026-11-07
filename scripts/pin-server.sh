#!/usr/bin/env bash
# Pin the commit wire-pod.service builds and runs (robot/work/server-sha, read by run-server.sh + load-guard.sh).
# Usage: pin-server.sh [rev]   (default HEAD; written as a full sha, atomically). Takes effect on the next (re)start.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
sha=$(git -C "$repo" rev-parse --verify "${1:-HEAD}^{commit}")
mkdir -p "$repo/robot/work"
printf '%s\n' "$sha" >"$repo/robot/work/server-sha.tmp"
mv "$repo/robot/work/server-sha.tmp" "$repo/robot/work/server-sha"
echo "server pinned to $(git -C "$repo" rev-parse --short "$sha")"
