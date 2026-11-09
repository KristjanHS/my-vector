#!/usr/bin/env bash
# Host test of patch 0017's silent-start guard (make check): builds 0017-silent-start_test.cpp on the my-vector silent
# start block cut from the committed patch, its flag path rewritten to a temp file. SKIP without g++.
# Usage: silent-start.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
die() { echo "FAIL: silent-start host test: $*" >&2; exit 1; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$fw/../../.git")")
command -v g++ >/dev/null || { echo "SKIP: silent-start host test: no g++"; exit 0; }
mkdir -p "$main/robot/work"
tmp=$(mktemp -d "$main/robot/work/silent-start-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# The block is all '+' lines of the one patch touching behaviorAnimSequence.cpp; a later patch editing it would make this copy stale.
mapfile -t touch < <(grep -l -- '^+++ b/engine/aiComponent/behaviorComponent/behaviors/animationWrappers/behaviorAnimSequence.cpp$' "$fw"/*.patch)
((${#touch[@]} == 1)) || die "want exactly one patch touching behaviorAnimSequence.cpp, got ${#touch[@]}: ${touch[*]##*/}"
start='^+namespace { // my-vector silent start$' end='^+} // namespace (my-vector silent start)$'
sed -n "\\@$start@,\\@$end@p" "${touch[0]}" >"$tmp/raw"
[[ $(tail -n 1 "$tmp/raw") == '+} // namespace (my-vector silent start)' ]] || die "no silent start block in ${touch[0]##*/}"
! grep -qv '^+' "$tmp/raw" || die "silent start block in ${touch[0]##*/} is not one added hunk"
block=$(sed 's/^+//' "$tmp/raw")
# The flag path must appear exactly once, so the rewrite below is the only path the block can check.
n=$(grep -cF -- '"/data/my-vector/silent-start"' <<<"$block" || true)
((n == 1)) || die "'\"/data/my-vector/silent-start\"' is in the silent start block $n times, want 1"
block=${block/'"/data/my-vector/silent-start"'/MV_TEST_SILENT_FILE}
printf '%s\n' "$block" >"$tmp/silent-start.inc"

g++ -std=c++14 -O1 -Wall -Werror -I"$tmp" -DMV_TEST_SILENT_FILE="\"$tmp/silent-start\"" \
  -o "$tmp/t" "$here/0017-silent-start_test.cpp"
rc=0
timeout 10 "$tmp/t" || rc=$?
((rc == 0)) || die "0017-silent-start_test.cpp exit $rc"
