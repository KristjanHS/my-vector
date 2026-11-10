#!/usr/bin/env bash
# KG search anim looped natively for W s while Stop__…_Searching_Loop_Stop posts on game object OBJ (default 4, empty = console default 1) every 0.2 s.
# Usage: [W=8] [OBJ=4] loopstop.sh <outdir> → during/after dBFS; after > -99 = the loop outlived the stops.
set -uo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
OUT=$1; mkdir -p "$OUT"; W=${W:-8} OBJ=${OBJ-4}
stop() { curl -s -o /dev/null "${C}PostAudioEvent&args=Stop__Robot_Vic_Sfx__Knowledge_Graph_Searching_Loop_Stop${OBJ:++$OBJ}"; }
on_exit release; hold; reset_rtpcs; quiet_guard
cap_on; play_anim anim_knowledgegraph_searching_01 16
end=$((SECONDS + W)); while ((SECONDS < end)); do stop; sleep 0.2; done
cap_off "$OUT/during.wav"; stop
cap_on; sleep 4; cap_off "$OUT/after.wav"
python3 -I "$here/db.py" --ticks "$OUT"/*.wav
