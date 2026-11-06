#!/usr/bin/env bash
# Capture what he plays while idle (no behavior control), SECS per condition → seconds with a peak above -45 dBFS.
# Usage: [SECS=30] idle.sh [Bus:level …]   e.g. idle.sh base:1.0 Procedural:0.1 — RTPCs are left at 1.0 afterwards.
set -uo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
OUT=${OUT:-$main/robot/work/volume-probe/idle}; mkdir -p "$OUT"; SECS=${SECS:-30}
on_exit reset_rtpcs
for c in "${@:-base:1.0}"; do r=${c%:*} l=${c#*:}; reset_rtpcs; [[ $r == base ]] || rtpc "$r" "$l"
  cap_on; sleep "$SECS"; cap_off "$OUT/${r}_$l.wav"; done
python3 -I "$here/db.py" --ticks "$OUT"/*.wav
