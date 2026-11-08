#!/usr/bin/env bash
# Fails when the victor clone's VICTOR_COMPAT_VERSION at <rev> differs from the robot's /etc/victor-compat-version (deploy-v.sh refuses it only after the build).
# Usage: compat.sh <victor-src> <rev>   exit 1 = mismatch or a compat file missing; robot not answering SSH (255) = WARN line, exit 0.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"
(($# == 2)) || { echo "usage: compat.sh <victor-src> <rev>" >&2; exit 2; }
want=$(git -C "$1" show "$2:VICTOR_COMPAT_VERSION" 2>&1) || { echo "FAIL: no VICTOR_COMPAT_VERSION at $2 in $1: $want" >&2; exit 1; }
want=${want//[[:space:]]/}
rc=0; have=$(ssh "${ssh_opts[@]}" "root@$ip" cat /etc/victor-compat-version 2>/dev/null) || rc=$?
((rc != 255)) || { echo "WARN: compat: SSH to $ip failed; firmware compat $want unchecked against the robot"; exit 0; }
have=${have//[[:space:]]/}
[[ $rc == 0 && -n $have ]] ||
  { echo "FAIL: robot $ip has no /etc/victor-compat-version (deploy reads 0 and refuses it)" >&2; exit 1; }
[[ $want == "$have" ]] ||
  { echo "FAIL: firmware compat $want != robot $ip compat $have: move pin in apply.sh to a victor commit with compat $have" >&2; exit 1; }
echo "compat $want = robot $ip"
