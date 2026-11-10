#!/usr/bin/env bash
# Per-bus RTPC ladder: wake chime, SDK voice and console-fired gesture sounds, each captured → dBFS (master stays as set).
# Usage: [CONDS="base:shipped Animation:1.0 …"] (base = shipped levels) [SOUNDS="chime voice …"] [RESET="… Master"] sweep.sh — 2026-11-04-vector-speech-volume.md (docs/plans/).
set -uo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
OUT=${OUT:-$main/robot/work/volume-probe/sweep}; mkdir -p "$OUT"
SOUNDS=${SOUNDS:-chime voice curious headup blink}
CONDS=${CONDS:-base:shipped Animation:1.0 Animation:0.5 Animation:0.25 Behavior:0.5 Behavior:0.25 Behavior:0.1 Procedural:0.5 Procedural:0.25 Procedural:0.1}
ev() { curl -s -o /dev/null "${C}PostAudioEvent&args=Play__Robot_Vic_Sfx__$1"; sleep 2.5; }
play() { case $1 in
  chime) ev Wake_Word_On ;; curious) ev Scrn_Curious ;; headup) ev Head_Up_Short_Curious ;; blink) ev Blink ;;
  voice) curl -s -m 20 "$A/say_text?serial=$serial&text=It+is+two+thirty+p.m." >/dev/null; sleep 0.5 ;; esac; }
on_exit release; hold
for c in $CONDS; do r=${c%:*} l=${c#*:}; reset_rtpcs; [[ $r == base ]] || rtpc "$r" "$l"
  for s in $SOUNDS; do quiet_guard; cap_on; play "$s"; cap_off "$OUT/${r}_${l}__$s.wav"; done; done
python3 -I "$here/db.py" "$OUT"/*.wav
