#!/usr/bin/env bash
# Fails when the victor clone's VICTOR_COMPAT_VERSION at <rev> differs from the robot's /etc/victor-compat-version (deploy-v.sh refuses it only after the build).
# Usage: compat.sh <victor-src> <rev>   exit 1 = mismatch; robot not answering SSH = WARN line, exit 0.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
(($# == 2)) || { echo "usage: compat.sh <victor-src> <rev>" >&2; exit 2; }
want=$(git -C "$1" show "$2:VICTOR_COMPAT_VERSION" | tr -d '[:space:]')
[[ -n $want ]] || { echo "FAIL: no VICTOR_COMPAT_VERSION at $2 in $1" >&2; exit 1; }
if ! have=$(ssh "${ssh_opts[@]}" "root@$ip" cat /etc/victor-compat-version 2>/dev/null | tr -d '[:space:]') || [[ -z $have ]]; then
  echo "WARN: compat: no /etc/victor-compat-version over SSH from $ip; firmware compat $want unchecked against the robot"
  exit 0
fi
[[ $want == "$have" ]] ||
  { echo "FAIL: firmware compat $want != robot $ip compat $have: move pin in apply.sh to a victor commit with compat $have" >&2; exit 1; }
echo "compat $want = robot $ip"
