#!/usr/bin/env bash
# Host test of patch 0016 (make check): builds 0016-mic-stream_test.cpp on the my-vector mic-stream blocks (pure + sender)
# cut from the committed patch. SKIP without g++ or the victor clone (jsoncpp).
# Usage: mic-stream.sh   (env: VICTOR_SRC)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
die() { echo "FAIL: mic-stream host test: $*" >&2; exit 1; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$fw/../../.git")")
src=${VICTOR_SRC:-$main/robot/work/wire-os-victor}
json=$src/lib/util/source/3rd/jsoncpp
command -v g++ >/dev/null || { echo "SKIP: mic-stream host test: no g++"; exit 0; }
[[ -f $json/jsoncpp.cpp ]] || { echo "SKIP: mic-stream host test: no jsoncpp in a victor clone at $src"; exit 0; }
mkdir -p "$main/robot/work" # gitignored scratch; absent with VICTOR_SRC elsewhere
tmp=$(mktemp -d "$main/robot/work/mic-stream-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# The blocks are '+' lines of the one patch touching micDataProcessor.cpp; a later patch editing them would make this copy stale.
mapfile -t touch < <(grep -l -- '^+++ b/animProcess/src/cozmoAnim/micData/micDataProcessor.cpp$' "$fw"/*.patch)
((${#touch[@]} == 1)) || die "want exactly one patch touching micDataProcessor.cpp, got ${#touch[@]}: ${touch[*]##*/}"
start='^+namespace { // my-vector mic-stream$' end='^+} // namespace (my-vector mic-stream sender)$'
sed -n "\\@$start@,\\@$end@p" "${touch[0]}" >"$tmp/raw"
[[ $(tail -n 1 "$tmp/raw") == '+} // namespace (my-vector mic-stream sender)' ]] || die "no mic-stream blocks in ${touch[0]##*/}"
! grep -qv '^+' "$tmp/raw" || die "mic-stream blocks in ${touch[0]##*/} are not one added hunk"
block=$(sed 's/^+//' "$tmp/raw")
n=$(grep -cF -- '"/data/data/myvector-voice.json"' <<<"$block" || true)
((n == 1)) || die "the voice file path is in the blocks $n times, want 1"
block=${block/'"/data/data/myvector-voice.json"'/MV_TEST_VOICE_FILE}
printf '%s\n' "$block" >"$tmp/mic-stream.inc"
# The trigger-loop hook (comment line to its closing brace), included as the test's hook() body.
sed -n '\@^+    // my-vector M4/C2:@,\@^+    }$@p' "${touch[0]}" >"$tmp/raw"
(($(wc -l <"$tmp/raw") == 5)) || die "no 5-line trigger-loop hook in ${touch[0]##*/}"
sed 's/^+//' "$tmp/raw" >"$tmp/mic-stream-hook.inc"

g++ -std=c++14 -O1 -w -I"$json" -c -o "$tmp/jsoncpp.o" "$json/jsoncpp.cpp"
g++ -std=c++14 -O1 -Wall -Werror -pthread -I"$json" -I"$tmp" -DMV_TEST_VOICE_FILE="\"$tmp/voice.json\"" \
  -o "$tmp/t" "$here/0016-mic-stream_test.cpp" "$tmp/jsoncpp.o"
rc=0
timeout 60 "$tmp/t" || rc=$?
((rc != 124)) || die "timed out (sender never stops or a read never ends)"
((rc == 0)) || die "0016-mic-stream_test.cpp exit $rc"
echo "mic-stream host test OK (${touch[0]##*/}; gate, ring race/overflow, port 0, URL host, loopback sender: backoff, keepalive, toggle, stall, no-thread)"
