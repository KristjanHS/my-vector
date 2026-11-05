#!/usr/bin/env bash
# Host test of patch 0010 (make check): builds 0010-pc-voice_test.cpp on the my-vector voice block cut from the committed patch,
# then runs voice-push.sh's URL check (stub ssh) on the same tts-urls.txt rows. SKIP without g++ or the victor clone (jsoncpp).
# Usage: pc-voice.sh   (env: VICTOR_SRC; PC_VOICE_LIVE=<voice> adds POSTs to a live tts/server.py on :8091 — opt-in only)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
die() { echo "FAIL: pc-voice host test: $*" >&2; exit 1; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$fw/../../.git")")
src=${VICTOR_SRC:-$main/robot/work/wire-os-victor}
json=$src/lib/util/source/3rd/jsoncpp
command -v g++ >/dev/null || { echo "SKIP: pc-voice host test: no g++"; exit 0; }
[[ -f $json/jsoncpp.cpp ]] || { echo "SKIP: pc-voice host test: no jsoncpp in a victor clone at $src"; exit 0; }
tmp=$(mktemp -d "$main/robot/work/pc-voice-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# The block is all '+' lines of the one patch touching the provider; a later patch editing it would make this copy stale.
mapfile -t touch < <(grep -l -- '^+++ b/animProcess/src/cozmoAnim/textToSpeech/textToSpeechProvider_vicos.cpp$' "$fw"/*.patch)
((${#touch[@]} == 1)) || die "want exactly one patch touching textToSpeechProvider_vicos.cpp, got ${#touch[@]}: ${touch[*]##*/}"
start='^+namespace { // my-vector voice$' end='^+} // namespace (my-vector voice)$'
sed -n "\\@$start@,\\@$end@p" "${touch[0]}" >"$tmp/raw"
[[ $(tail -n 1 "$tmp/raw") == '+} // namespace (my-vector voice)' ]] || die "no voice block in ${touch[0]##*/}"
! grep -qv '^+' "$tmp/raw" || die "voice block in ${touch[0]##*/} is not one added hunk"
block=$(sed 's/^+//' "$tmp/raw")
# sub <from> <to>: each test-only rewrite must hit exactly once.
sub() {
  local n
  n=$(grep -cF -- "$1" <<<"$block" || true)
  ((n == 1)) || die "'$1' is in the voice block $n times, want 1"
  block=${block/"$1"/"$2"}
}
sub '"/data/data/myvector-voice.json"' 'MV_TEST_VOICE_FILE'
sub 'kMyVectorSentence_ms = 20000;' 'kMyVectorSentence_ms = 600;'
printf '%s\n' "$block" >"$tmp/pc-voice.inc"

g++ -std=c++14 -O1 -w -I"$json" -c -o "$tmp/jsoncpp.o" "$json/jsoncpp.cpp"
g++ -std=c++14 -O1 -Wall -Werror -I"$json" -I"$tmp" -DMV_TEST_VOICE_FILE="\"$tmp/voice.json\"" \
  -o "$tmp/t" "$here/0010-pc-voice_test.cpp" "$tmp/jsoncpp.o"
rc=0
timeout 30 "$tmp/t" "$here/tts-urls.txt" || rc=$?
((rc != 124)) || die "timed out (parser or POST loop never ends)"
((rc == 0)) || die "0010-pc-voice_test.cpp exit $rc"

# voice-push.sh in a throwaway robot/ (GIT_DIR=/nonexistent: env.sh falls back to the copy's own robot.env); ssh = stub.
vp=$tmp/vp
mkdir -p "$vp/robot/firmware" "$vp/bin"
cp "$fw/../env.sh" "$vp/robot/"
cp "$fw/voice-push.sh" "$vp/robot/firmware/"
: >"$vp/robot/key"
printf '#!/bin/sh\ncat >/dev/null; echo unchanged\n' >"$vp/bin/ssh"
chmod +x "$vp/bin/ssh"
n=0
while read -r want url; do
  [[ -n $want && $want != \#* ]] || continue
  printf "ROBOT_ESN=0\nROBOT_IP=127.0.0.1\nROBOT_SERVER=http://localhost:8080\nROBOT_KEY=key\nROBOT_TTS_URL='%s'\n" "$url" >"$vp/robot/robot.env"
  rc=0
  out=$(GIT_DIR=/nonexistent PATH="$vp/bin:$PATH" "$vp/robot/firmware/voice-push.sh" af_bella 1.0 2>&1) || rc=$?
  case $want in
    ok) [[ $rc == 0 && $out == "ok: T2 voice file already "* ]] || die "voice-push.sh rejects $url (exit $rc: $out)" ;;
    bad) [[ $rc == 2 && $out == *"must be http://<IPv4>"* ]] || die "voice-push.sh: $url gave exit $rc, want 2 SETUP (got: $out)" ;;
    *) die "tts-urls.txt: bad row: $want $url" ;;
  esac
  n=$((n + 1))
done <"$here/tts-urls.txt"
((n >= 8)) || die "tts-urls.txt has $n rows"
echo "pc-voice host test OK (${touch[0]##*/}; voice-push.sh agrees on $n tts-urls.txt rows)"
