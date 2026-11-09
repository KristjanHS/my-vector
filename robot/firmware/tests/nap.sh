#!/usr/bin/env bash
# Host test of patch 0011's nap guard (make check): builds 0011-nap_test.cpp on the my-vector nap block cut from the
# committed patch. SKIP without g++.
# Usage: nap.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
die() { echo "FAIL: nap host test: $*" >&2; exit 1; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$fw/../../.git")")
command -v g++ >/dev/null || { echo "SKIP: nap host test: no g++"; exit 0; }
mkdir -p "$main/robot/work"
tmp=$(mktemp -d "$main/robot/work/nap-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# The block is all '+' lines of the one patch touching behaviorSleepCycle.cpp; a later patch editing it would make this copy stale.
mapfile -t touch < <(grep -l -- '^+++ b/engine/aiComponent/behaviorComponent/behaviors/sleeping/behaviorSleepCycle.cpp$' "$fw"/*.patch)
((${#touch[@]} == 1)) || die "want exactly one patch touching behaviorSleepCycle.cpp, got ${#touch[@]}: ${touch[*]##*/}"
start='^+namespace { // my-vector nap$' end='^+} // namespace (my-vector nap)$'
sed -n "\\@$start@,\\@$end@p" "${touch[0]}" >"$tmp/raw"
[[ $(tail -n 1 "$tmp/raw") == '+} // namespace (my-vector nap)' ]] || die "no nap block in ${touch[0]##*/}"
! grep -qv '^+' "$tmp/raw" || die "nap block in ${touch[0]##*/} is not one added hunk"
sed 's/^+//' "$tmp/raw" >"$tmp/nap.inc"

g++ -std=c++14 -O1 -Wall -Werror -I"$tmp" -o "$tmp/t" "$here/0011-nap_test.cpp"
rc=0
timeout 10 "$tmp/t" || rc=$?
((rc == 0)) || die "0011-nap_test.cpp exit $rc"
