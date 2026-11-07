#!/usr/bin/env bash
# Active-listening capture (listen.Runner runs it): listen.sh <esn> — silent CLIP_MS (default 10000) RecordMicDataClip clips back to back,
# each POSTed as 16 kHz pcm to wire-pod /myvector/listen/clip; exit 0 on 410 (listening off), 1 on a robot/POST failure, 2 on SETUP.
set -Eeuo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/openmic/lib.sh
. "$here/lib.sh"
(($# == 1)) || { echo "usage: listen.sh <esn>" >&2; exit 2; }
[[ $1 == "$esn" ]] || { echo "SETUP: listen.sh $1: robot.env's robot is $esn" >&2; exit 2; }
trap 'exit 1' ERR
ms=${CLIP_MS:-10000}
tmp=$(mktemp -d "$robot_dir/work/openmic-listen.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
trap 'exit 143' TERM INT

now_ms() { date +%s%3N; }
rsh "rm -rf $D $D.prev"
clip_ms "$ms"
start=$(now_ms)
record_clip
while :; do
  sleep "$((ms / 1000))"
  # Park the saved clip so the next one records while this one is fetched and posted.
  park_clip >/dev/null || { echo "robot: clip from $start not saved" >&2; exit 1; }
  prev=$start start=$(now_ms)
  record_clip
  rm -rf "${tmp:?}"/*
  scp -q "${ssh_opts[@]}" "root@$ip:$D.prev/miccapture_*/*.wav" "$tmp/" && rsh "rm -rf $D.prev"
  proc_pcm "$tmp"
  code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 150 -H 'Content-Type: application/octet-stream' \
    --data-binary @"$tmp/proc.pcm" "$api/myvector/listen/clip?esn=$esn&start=$prev") || code=000
  case $code in
  204) ;;
  410) exit 0 ;;
  *) echo "wire-pod: clip POST $code" >&2; exit 1 ;;
  esac
done
