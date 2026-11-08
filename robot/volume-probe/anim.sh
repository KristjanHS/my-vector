#!/usr/bin/env bash
# Real animations (vic-anim PlayAnimation, behavior control held) under RTPC conditions, captured → dBFS.
# Usage: [CONDS="base:1.0 Animation:0.1"] [ANIMS="anim_rtsound_oncharger_asleep_30right_01 …"] anim.sh — clip names: consolefunccall?func=ListAnimations.
set -uo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
OUT=${OUT:-$main/robot/work/volume-probe/anim}; mkdir -p "$OUT"; W=${W:-5}
ANIMS=${ANIMS:-anim_rtsound_oncharger_asleep_30right_01 anim_rtsound_oncharger_observe_behind_left_01}
CONDS=${CONDS:-base:1.0 Animation:0.25 Animation:0.1 Behavior:0.1 Procedural:0.1}
on_exit release; hold
for c in $CONDS; do r=${c%:*} l=${c#*:}; reset_rtpcs; [[ $r == base ]] || rtpc "$r" "$l"
  for a in $ANIMS; do quiet_guard; cap_on; curl -s -o /dev/null "${C}PlayAnimation&args=$a+1"; sleep "$W"; cap_off "$OUT/${r}_${l}__$a.wav"; done; done
python3 -I "$here/db.py" "$OUT"/*.wav
