#!/usr/bin/env bash
# Open-mic probe (shut-up-sleep Stage 1.2): probe.sh [-n N] [-q] <lang> <set> [sample ...] — N trials of: robot plays the
# voice-samples set, a 15 s RecordMicDataClip runs, the PC speaker says the stop phrase at +5 s (-q: silent control run;
# -m: no WSL audio — prints ▶ PLAY NOW and the user plays C:\Users\PC\Documents\say-<lang>.wav; run it in your own terminal so the cue shows live).
# Each clip (processed + raw 4-mic mix) → myvector-stt (saved lang must be <lang>) → quietmatch whole/-any. Output in robot/work/openmic/.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
W=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work/openmic
tts=${TTS_URL:-http://127.0.0.1:8091} stt=${STT_URL:-http://127.0.0.1:8092} api=${WIREPOD_URL:-http://127.0.0.1:8080}
ffmpeg=$(command -v ffmpeg || echo /opt/ffmpeg/ffmpeg)
D=/data/data/com.anki.victor/cache/micdata/debugCapture
n=5 say=1 manual=0
while [[ ${1:-} == -* ]]; do
  case $1 in -n) n=$2; shift 2 ;; -q) say=0; shift ;; -m) manual=1; shift ;; *) echo "unknown flag $1" >&2; exit 2 ;; esac
done
(($# >= 2)) || { echo "usage: probe.sh [-n N] [-q | -m] <en-US|et-EE> <voice-samples set> [sample ...]" >&2; exit 2; }
lang=$1; shift
case $lang in en-US) voice=af_bella phrase="shut up" ;; et-EE) voice=mari phrase="ole vait" ;; *) echo "lang: en-US or et-EE" >&2; exit 2 ;; esac
saved=$(curl -fsS --max-time 5 "$api/myvector/api/voice" | jq -r '.lang // empty') || { echo "SETUP: wire-pod not up at $api" >&2; exit 2; }
[[ $saved == "$lang" ]] || { echo "SETUP: saved lang is ${saved:-unset}, switch it to $lang in the web UI (STT serves only it)" >&2; exit 2; }

mkdir -p "$W/bin"
(cd "$repo/chipper" && go build -o "$W/bin/quietmatch" ./pkg/myvector/quiet/quietmatch)
say_pcm=$W/say-$lang.pcm
jq -nc --arg t "$phrase" --arg v "$voice" --arg l "$lang" '{text: $t, voice: $v, lang: $l}' |
  curl -fsS --max-time 60 -H 'Content-Type: application/json' -d @- -o "$say_pcm" "$tts/tts"
if ((manual)); then # no PC audio from WSL: the user plays this file on cue
  win_dir=${WIN_DIR:-/mnt/c/Users/PC/Documents}
  "$ffmpeg" -v error -y -f s16le -ar 16000 -ac 1 -i "$say_pcm" "$W/say-$lang.wav"
  # A player holding the file open blocks the overwrite; phrase + voice are fixed per lang, so its copy is the same clip.
  if ! cp "$W/say-$lang.wav" "$win_dir/say-$lang.wav" 2>/dev/null; then
    [[ -f $win_dir/say-$lang.wav ]] || { echo "SETUP: cannot write $win_dir/say-$lang.wav" >&2; exit 2; }
    echo "say-$lang.wav is open in your player — keeping that copy"
  fi
  win_path="C:${win_dir#/mnt/c}/say-$lang.wav" win_path=${win_path//\//\\}
fi
rsh() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
transcribe() { curl -fsS --max-time 120 -H 'Content-Type: application/octet-stream' --data-binary @"$1" "$stt/stt?lang=$lang" | jq -r '.text // ""'; }
match() { "$W/bin/quietmatch" "$@" <<<"$text" >/dev/null && echo - || echo HIT; }

run=$W/$(date +%Y%m%d-%H%M%S)-$lang$( ((say)) || echo -quiet)
mkdir -p "$run"
curl -fsS -o /dev/null "http://$ip:8889/consolevarset?key=MicData_ClipRecordTime_ms&value=15000"
printf 'trial\tsrc\twhole\tany\ttext\n' | tee "$run/results.tsv"
for ((i = 1; i <= n; i++)); do
  rsh "rm -rf $D"
  ((manual)) && printf 'trial %d: get ready…\n' "$i" >&2
  "$here/../voice-samples/play.sh" "$@" >/dev/null &
  play=$!
  sleep 1
  curl -fsS -o /dev/null "http://$ip:8889/consolefunccall?func=RecordMicDataClip&args="
  if ((manual && say)); then
    sleep 3
    printf '\a▶ PLAY NOW: %s (trial %d)\n' "$win_path" "$i" >&2
    sleep 2
  else
    sleep 5
    ((say)) && paplay --raw --rate=16000 --channels=1 --format=s16le "$say_pcm"
  fi
  wait "$play" || true
  sleep 11
  for _ in {1..10}; do
    a=$(rsh "ls -l $D/miccapture_*/*.wav 2>/dev/null | awk '{s+=\$5} END {print NR, s}'"); sleep 1
    b=$(rsh "ls -l $D/miccapture_*/*.wav 2>/dev/null | awk '{s+=\$5} END {print NR, s}'")
    [[ $a == "$b" && $a == 2\ * ]] && break
  done
  [[ $a == "$b" && $a == 2\ * ]] || { echo "trial $i: clip not saved ($b)" >&2; exit 1; }
  t=$run/t$i
  mkdir -p "$t"
  scp -q "${ssh_opts[@]}" "root@$ip:$D/miccapture_*/*.wav" "$t/"
  rsh "rm -rf $D"
  proc=$(find "$t" -name '*.wav' ! -name '*_raw.wav')
  raw=$(find "$t" -name '*_raw.wav')
  "$ffmpeg" -v error -y -i "$proc" -ar 16000 -ac 1 -f s16le "$t/proc.pcm"
  "$ffmpeg" -v error -y -i "$raw" -af 'pan=mono|c0=0.25*c0+0.25*c1+0.25*c2+0.25*c3,dynaudnorm' -ar 16000 -f s16le "$t/raw.pcm"
  for src in proc raw; do
    text=$(transcribe "$t/$src.pcm" | tr '\n\t' '  ')
    printf '%s\t%s\t%s\t%s\t%s\n' "$i" "$src" "$(match)" "$(match -any)" "$text" | tee -a "$run/results.tsv"
  done
done
for src in proc raw; do
  awk -F'\t' -v s="$src" -v n="$n" '$2 == s { w += $3 == "HIT"; a += $4 == "HIT" } END { printf "%s: whole %d/%d  any %d/%d\n", s, w, n, a, n }' "$run/results.tsv"
done | tee "$run/summary.txt"
