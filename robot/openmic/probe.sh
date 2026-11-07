#!/usr/bin/env bash
# Open-mic probe (shut-up-sleep Stage 1.2): probe.sh [-n N] [-q | -m [-o]] <lang> <set> [sample ...] — N trials of: robot plays the
# voice-samples set, a 15 s RecordMicDataClip runs, the PC speaker says the stop phrase in the pause after Vector's sentence (-o: over it;
# -q: silent control; -m: no WSL audio — keep C:\Users\PC\Documents\say-<lang>.wav open in a player, press play on ▶ PLAY NOW).
# Each clip (processed + raw 4-mic mix) → 2 s windows, 1 s hop → myvector-stt → quietmatch whole/-any; `at` = first whole-hit second.
# probe.sh -r <run dir> <lang> re-scores a saved run. Output in robot/work/openmic/ (saved lang must be <lang>).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
W=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work/openmic
tts=${TTS_URL:-http://127.0.0.1:8091} stt=${STT_URL:-http://127.0.0.1:8092} api=${WIREPOD_URL:-http://127.0.0.1:8080}
ffmpeg=$(command -v ffmpeg || echo /opt/ffmpeg/ffmpeg)
D=/data/data/com.anki.victor/cache/micdata/debugCapture
n=5 say=1 manual=0 over=0 rescore=
while [[ ${1:-} == -* ]]; do
  case $1 in
  -n) n=$2; shift 2 ;; -q) say=0; shift ;; -m) manual=1; shift ;; -o) over=1; shift ;; -r) rescore=$2; shift 2 ;;
  *) echo "unknown flag $1" >&2; exit 2 ;;
  esac
done
(($# >= 2)) || [[ -n $rescore && $# == 1 ]] ||
  { echo "usage: probe.sh [-n N] [-q | -m [-o]] <en-US|et-EE> <voice-samples set> [sample ...] | probe.sh -r <run dir> <lang>" >&2; exit 2; }
lang=$1; shift
case $lang in en-US) voice=af_bella phrase="shut up" ;; et-EE) voice=mari phrase="ole vait" ;; *) echo "lang: en-US or et-EE" >&2; exit 2 ;; esac
saved=$(curl -fsS --max-time 5 "$api/myvector/api/voice" | jq -r '.lang // empty') || { echo "SETUP: wire-pod not up at $api" >&2; exit 2; }
[[ $saved == "$lang" ]] || { echo "SETUP: saved lang is ${saved:-unset}, switch it to $lang in the web UI (STT serves only it)" >&2; exit 2; }

mkdir -p "$W/bin"
(cd "$repo/chipper" && go build -o "$W/bin/quietmatch" ./pkg/myvector/quiet/quietmatch)
transcribe() { curl -fsS --max-time 120 -H 'Content-Type: application/octet-stream' --data-binary @"$1" "$stt/stt?lang=$lang" | jq -r '.text // ""' | tr '\n\t' '  '; }
match() { "$W/bin/quietmatch" "$@" <<<"$text" >/dev/null && echo - || echo HIT; }
# score <trial> <dir>: one row per src; whole-clip STT drops a short phrase next to Vector's sentence, so windows carry the verdict.
score() {
  local src s at whole any hit text
  for src in proc raw; do
    at=- whole=- any=- hit=
    for ((s = 0; ; s++)); do
      dd if="$2/$src.pcm" of="$2/win.pcm" bs=32000 skip=$s count=2 status=none
      (($(stat -c %s "$2/win.pcm") >= 16000)) || break
      text=$(transcribe "$2/win.pcm")
      if [[ $(match) == HIT ]]; then
        [[ $at == - ]] && at=$s hit=$text
        whole=HIT
      fi
      [[ $(match -any) == HIT ]] && any=HIT && hit=${hit:-$text}
    done
    rm -f "$2/win.pcm"
    text=${hit:-$(transcribe "$2/$src.pcm")}
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$src" "$whole" "$any" "$at" "$text"
  done
}
summary() {
  for src in proc raw; do
    awk -F'\t' -v s="$src" -v n="$2" '$2 == s { w += $3 == "HIT"; a += $4 == "HIT" } END { printf "%s: whole %d/%d  any %d/%d\n", s, w, n, a, n }' "$1"
  done
}
header() { printf 'trial\tsrc\twhole\tany\tat\ttext\n'; }

if [[ -n $rescore ]]; then
  out=$rescore/rescore.tsv
  header | tee "$out"
  n=0
  for t in "$rescore"/t*/; do
    n=$((n + 1))
    score "$(basename "$t" | tr -d t)" "${t%/}" | tee -a "$out"
  done
  summary "$out" "$n" | tee "$rescore/rescore-summary.txt"
  exit 0
fi

say_pcm=$W/say-$lang.pcm
jq -nc --arg t "$phrase" --arg v "$voice" --arg l "$lang" '{text: $t, voice: $v, lang: $l}' |
  curl -fsS --max-time 60 -H 'Content-Type: application/json' -d @- -o "$say_pcm" "$tts/tts"
if ((manual)); then # no PC audio from WSL: the user plays this file on cue
  win_dir=${WIN_DIR:-/mnt/c/Users/PC/Documents}
  "$ffmpeg" -v error -y -f s16le -ar 16000 -ac 1 -i "$say_pcm" "$W/say-$lang.wav"
  # Never rewrite an existing copy: a player holding it open loses the file. Phrase + voice are fixed per lang,
  # so it is the same clip — delete it to regenerate.
  if [[ -s $win_dir/say-$lang.wav ]]; then
    echo "keeping existing $win_dir/say-$lang.wav (delete it to regenerate)"
  elif ! err=$(cp "$W/say-$lang.wav" "$win_dir/say-$lang.wav" 2>&1); then
    echo "SETUP: $err" >&2; exit 2
  fi
  win_path="C:${win_dir#/mnt/c}/say-$lang.wav" win_path=${win_path//\//\\}
fi
rsh() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
cue() { # the stop phrase, now
  if ((manual)); then printf '\a▶ PLAY NOW: %s (trial %d)\n' "$win_path" "$i" >&2
  else paplay --raw --rate=16000 --channels=1 --format=s16le "$say_pcm"; fi
}

run=$W/$(date +%Y%m%d-%H%M%S)-$lang$( ((say)) || echo -quiet)$( ((say && over)) && echo -over)
mkdir -p "$run"
curl -fsS -o /dev/null "http://$ip:8889/consolevarset?key=MicData_ClipRecordTime_ms&value=15000"
header | tee "$run/results.tsv"
for ((i = 1; i <= n; i++)); do
  rsh "rm -rf $D"
  ((manual)) && printf 'trial %d: get ready…\n' "$i" >&2
  "$here/../voice-samples/play.sh" "$@" >/dev/null &
  play=$!
  # pause cue: the clip starts late so the cue, given when play.sh ends (sentence + 2 s), still lands well inside 15 s
  if ((say && !over)); then sleep 3; else sleep 1; fi
  curl -fsS -o /dev/null "http://$ip:8889/consolefunccall?func=RecordMicDataClip&args="
  start=$SECONDS
  if ((say && over)); then
    sleep 3
    cue
  fi
  wait "$play" || true
  ((say && !over)) && cue
  sleep $((start + 16 - SECONDS > 0 ? start + 16 - SECONDS : 0))
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
  score "$i" "$t" | tee -a "$run/results.tsv"
done
summary "$run/results.tsv" "$n" | tee "$run/summary.txt"
