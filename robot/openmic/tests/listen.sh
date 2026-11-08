#!/usr/bin/env bash
# Host test of robot/openmic/listen.sh (make check): a temp copy of robot/ with its own robot.env, fake ssh/scp/curl/ffmpeg on PATH.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/listen-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: listen.sh host test: $*" >&2; exit 1; }

tree=$tmp/tree/robot
mkdir -p "$tree/openmic" "$tree/work" "$tmp/bin"
mkdir -p "$tree/quiet" && cp "$robot/env.sh" "$tree/" && cp "$robot/quiet/guard.sh" "$tree/quiet/"
cp "$robot/openmic/lib.sh" "$robot/openmic/listen.sh" "$tree/openmic/"
: >"$tree/key"
printf '%s\n' ROBOT_ESN=00e20100 ROBOT_IP=10.9.9.9 ROBOT_SERVER=10.9.9.1 ROBOT_KEY=key >"$tree/robot.env"
cat >"$tmp/bin/ssh" <<'F'
#!/bin/sh
for c; do :; done
case $c in
*"mv \$D \$D.prev"*) echo "ssh park" >>"$T/calls" ;;
*) echo "ssh $c" >>"$T/calls" ;;
esac
F
cat >"$tmp/bin/scp" <<'F'
#!/bin/sh
for d; do :; done
echo "scp" >>"$T/calls"
: >"$d/c.wav"; : >"$d/c_raw.wav"
F
cat >"$tmp/bin/ffmpeg" <<'F'
#!/bin/sh
for o; do :; done
echo "ffmpeg" >>"$T/calls"
[ "$(grep -c '^ffmpeg$' "$T/calls")" = "${FFMPEG_FAIL_AT:-0}" ] && exit 2
echo pcm >"$o"
F
cat >"$tmp/bin/curl" <<'F'
#!/bin/sh
for u; do :; done
case $u in
*/myvector/listen/clip*)
  echo "post $u" >>"$T/calls"
  code=$(head -n1 "$T/codes"); sed -i 1d "$T/codes"; printf %s "$code" ;;
*) echo "curl $u" >>"$T/calls" ;;
esac
F
chmod +x "$tmp/bin/"*
# run <codes> [esn]: listen.sh against the fakes; the POSTs answer <codes> in order; prints the exit code
run() {
  T=$tmp/run; rm -rf "$T"; mkdir -p "$T"; printf '%s\n' $1 >"$T/codes"
  local rc=0
  T=$T CLIP_MS=0 GIT_CEILING_DIRECTORIES=$tmp PATH="$tmp/bin:$PATH" "$tree/openmic/listen.sh" "${2:-00e20100}" 2>"$T/err" || rc=$?
  echo "$rc"
}

[[ $(run "204 204 410") == 0 ]] || die "410 did not end in exit 0: $(cat "$tmp/run/err")"
c=$tmp/run/calls
(($(grep -c '^post .*esn=00e20100&start=[0-9]\{13\}$' "$c") == 3)) || die "want 3 clip POSTs: $(grep post "$c")"
(($(grep -c 'RecordMicDataClip' "$c") == 4)) || die "want 4 recordings (next one starts before each POST): $(grep -c RecordMicDataClip "$c")"
(($(grep -c '^ssh park$' "$c") == 3)) || die "want 3 clips parked, one ssh each: $(grep -c '^ssh park$' "$c")"
(($(grep -c '^ssh rm -rf .*debugCapture.prev$' "$c") == 4)) || die "want the start cleanup + 3 parked clips removed"
grep -q 'MicData_ClipRecordTime_ms&value=0' "$c" || die "CLIP_MS not set on the robot"
[[ -z $(find "$tree/work" -mindepth 1) ]] || die "temp clip dir left in work/"

[[ $(run "204 500") == 1 ]] || die "a 500 POST did not exit 1"
[[ $(FFMPEG_FAIL_AT=2 run "204 204") == 1 ]] || die "a tool exiting 2 mid-loop did not exit 1"
(($(grep -c '^post ' "$tmp/run/calls") == 1)) || die "want the tool failure on the 2nd clip, after 1 POST"
[[ $(run "204" 00e2ffff) == 2 ]] || die "another ESN did not exit 2"
grep -q '^ssh' "$tmp/run/calls" 2>/dev/null && die "another ESN reached the robot"
echo "listen.sh host test OK"
