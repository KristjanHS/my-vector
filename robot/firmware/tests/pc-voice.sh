#!/usr/bin/env bash
# Host test of patch 0010 (make check): builds 0010-pc-voice_test.cpp on the my-vector voice block cut from the committed patch,
# then voice-push.sh's URL check on the same tts-urls.txt rows + its JSON, and settings.sh's T2 pick + wire-pod jdoc sync
# (stub ssh/curl). SKIP without g++ or the victor clone (jsoncpp).
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

# voice-push.sh + settings.sh in a throwaway robot/ (GIT_DIR=/nonexistent: env.sh falls back to the copy's own robot.env).
# Stub robot (ssh runs settings.sh's sed on robot_jdoc's file): clock 24h, Celsius; the voice file lands in $vp/voice.json, curl calls in $vp/curl.log.
vp=$tmp/vp
mkdir -p "$vp/robot/firmware" "$vp/robot/diskguard" "$vp/robot/sshkey" "$vp/robot/hostkey" "$vp/robot/epod" "$vp/robot/netguard" "$vp/bin" "$vp/secrets/server/jdocs"
cp "$fw/../env.sh" "$vp/robot/"
cp "$fw/voice-push.sh" "$fw/settings.sh" "$vp/robot/firmware/"
printf '#!/bin/sh\necho diskguard stub\n' >"$vp/robot/diskguard/apply.sh"
printf '#!/bin/sh\necho sshkey stub\n' >"$vp/robot/sshkey/apply.sh"
printf '#!/bin/sh\necho hostkey stub\n' >"$vp/robot/hostkey/apply.sh"
printf '#!/bin/sh\necho epod stub\n' >"$vp/robot/epod/apply.sh"
printf '#!/bin/sh\necho netguard stub\n' >"$vp/robot/netguard/apply.sh"
: >"$vp/robot/key"
cat >"$vp/bin/ssh" <<'STUB'
#!/usr/bin/env bash
case $* in
  *'new=$(cat)'*) cat >"${0%/bin/ssh}/voice.json"; echo unchanged ;;
  *"sed -n "*) c=${!#}; sh -c "${c//\/data\/data\/com.anki.victor\/persistent\/jdocs\/vic.RobotSettings.json/${0%/bin/ssh}/vic.RobotSettings.json}" ;;
  *user-do-not-auto-update*) echo present ;;
esac
STUB
cat >"$vp/bin/curl" <<'STUB'
#!/bin/sh
echo "$*" >>"${0%/bin/curl}/curl.log"
STUB
printf '#!/bin/sh\n' >"$vp/bin/sleep"
chmod +x "$vp/bin/ssh" "$vp/bin/curl" "$vp/bin/sleep"
# robot_jdoc <time_zone>: the robot's pretty-printed vic.RobotSettings.json that the ssh stub's sed reads.
robot_jdoc() {
  printf '{\n   "clock_24_hour" : true,\n   "master_volume" : 1,\n   "temp_is_fahrenheit" : false,\n   "time_zone" : "%s",\n}\n' "$1" \
    >"$vp/vic.RobotSettings.json"
}
robot_jdoc Europe/Tallinn
vp_run() { local s=$1; shift; GIT_DIR=/nonexistent PATH="$vp/bin:$PATH" "$vp/robot/firmware/$s" "$@" 2>&1; }
n=0
while read -r want url; do
  [[ -n $want && $want != \#* ]] || continue
  printf "ROBOT_ESN=0\nROBOT_IP=127.0.0.1\nROBOT_SERVER=http://localhost:8080\nROBOT_KEY=key\nROBOT_TTS_URL='%s'\n" "$url" >"$vp/robot/robot.env"
  rc=0
  out=$(vp_run voice-push.sh af_bella 1.0) || rc=$?
  case $want in
    ok) [[ $rc == 0 && $out == "ok: T2 voice file already "* ]] || die "voice-push.sh rejects $url (exit $rc: $out)" ;;
    bad) [[ $rc == 2 && $out == *"must be http://<IPv4>"* ]] || die "voice-push.sh: $url gave exit $rc, want 2 SETUP (got: $out)" ;;
    *) die "tts-urls.txt: bad row: $want $url" ;;
  esac
  n=$((n + 1))
done <"$here/tts-urls.txt"
((n >= 8)) || die "tts-urls.txt has $n rows"

printf "ROBOT_ESN=0\nROBOT_IP=127.0.0.1\nROBOT_SERVER=http://wp\nROBOT_KEY=key\nROBOT_TTS_URL=http://10.0.0.1:8091\n" >"$vp/robot/robot.env"
: >"$vp/voice.json"; vp_run voice-push.sh af_bella 1.0 >/dev/null || die "voice-push.sh af_bella 1.0 failed"
grep -qF '"lang":"en-US"' "$vp/voice.json" || die "voice-push.sh 2-arg: no \"lang\":\"en-US\" in $(cat "$vp/voice.json")"
: >"$vp/voice.json"; vp_run voice-push.sh mari 1.0 et-EE >/dev/null || die "voice-push.sh mari 1.0 et-EE failed"
grep -F '"voice":"mari"' "$vp/voice.json" | grep -qF '"lang":"et-EE"' || die "voice-push.sh et-EE wrote $(cat "$vp/voice.json")"
rc=0; out=$(vp_run voice-push.sh af_bella 1.0 fi-FI) || rc=$?
((rc == 1)) || die "voice-push.sh fi-FI: exit $rc, want 1 ($out)"

# settings.sh: <wire-pod jdoc clock_24_hour> <temp_is_fahrenheit> <myvectorConfig.json>; sets out, rc.
settings_run() {
  jq -n --arg d "{\"clock_24_hour\" : $1, \"temp_is_fahrenheit\" : $2}" \
    '[{thing: "vic:0", name: "vic.RobotSettings", jdoc: {json_doc: $d}}]' >"$vp/secrets/server/jdocs/jdocs.json"
  printf '%s\n' "$3" >"$vp/secrets/server/myvectorConfig.json"
  : >"$vp/curl.log" && : >"$vp/voice.json"
  rc=0; out=$(vp_run settings.sh) || rc=$?
}
settings_run true false '{"lang": "et-EE", "t2": "af_heart", "t2_et": "mari", "slow": 1.1}'
[[ $rc == 0 && ! -s $vp/curl.log ]] || die "settings.sh, all in place: exit $rc, curl $(cat "$vp/curl.log"): $out"
[[ $(cat "$vp/voice.json") == '{"url":"http://10.0.0.1:8091","voice":"mari","slow":1.1,"lang":"et-EE"}' ]] ||
  die "settings.sh et-EE pushed $(cat "$vp/voice.json")"
settings_run true true '{"lang": "en-US", "t2": "af_heart", "t2_et": "mari", "slow": 1.1}'
grep -qF '"voice":"af_heart","slow":1.1,"lang":"en-US"' "$vp/voice.json" || die "settings.sh en-US pushed $(cat "$vp/voice.json")"
[[ $(cat "$vp/curl.log") == *"/api-sdk/temp_c?serial=0"*"/api-sdk/get_sdk_settings?serial=0"* && $(cat "$vp/curl.log") != *time_format* ]] ||
  die "settings.sh, wire-pod copy Fahrenheit: curl calls $(cat "$vp/curl.log")"
[[ $rc == 1 && $out == *"WARN: temp_is_fahrenheit: wire-pod's copy"*"still has true"*"weather units follow it"* ]] ||
  die "settings.sh, wire-pod copy stays Fahrenheit: exit $rc, want 1 + WARN ($out)"
robot_jdoc America/Los_Angeles
settings_run true false '{"lang": "en-US"}'
[[ $(cat "$vp/curl.log") == *"/api-sdk/timezone?serial=0&timezone=Europe/Tallinn"* && $rc == 1 &&
  $out == *"WARN: time_zone: set to Europe/Tallinn sent, robot still has America/Los_Angeles after 15 s"* ]] ||
  die "settings.sh, robot on America/Los_Angeles: exit $rc, curl $(cat "$vp/curl.log"): $out"
echo "pc-voice host test OK (${touch[0]##*/}; voice-push.sh agrees on $n tts-urls.txt rows; voice-push.sh lang + settings.sh T2/jdoc stubs)"
