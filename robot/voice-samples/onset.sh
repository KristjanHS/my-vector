#!/usr/bin/env bash
# Time-to-first-sound of a robot-internal (T2) line per arm: engine console SayText → output capture → onset.
# Arms: stock · cori · af_bella · mari (et-EE) (PC up) · refused (PC port closed) · blackhole (PC host gone). Restores the voice file.
# Usage: [RUNS=3] [WORD=bla-bla-…] onset.sh [arm…] — V2 of 2026-11-05-vector-voice-verify.md (docs/plans/).
set -uo pipefail
self=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/volume-probe/lib.sh
. "$self/../volume-probe/lib.sh"
RUNS=${RUNS:-3}
WORD=${WORD:-bla-bla-bla-bla-bla-bla-bla-bla-bla-bla} # console args split on spaces: one word only
OUT=$main/robot/work/voice-samples/onset; mkdir -p "$OUT"; rm -f "$OUT"/*.wav
F=/data/data/myvector-voice.json
arms=("$@"); ((${#arms[@]})) || arms=(stock cori af_bella mari refused blackhole)
# Under et-EE the PC frees Kokoro per request (tts/server.py kokoro_selected): af_bella's cold load misses the 1.5 s cut.
if [[ " ${arms[*]} " == *" af_bella "* ]] && grep -q '"lang": *"et-EE"' "$main/chipper/myvectorConfig.json" 2>/dev/null; then
  echo "FAIL: af_bella arm needs PC lang en-US (now et-EE) — switch language in the web UI" >&2; exit 2
fi
# shellcheck disable=SC2029 # expanding on the client is the point
r() { ssh "${so[@]}" "root@$ip" "$@"; }
orig=$(r "cat $F") || { echo "FAIL: no $F on the robot (ship / voice-push first)" >&2; exit 2; }
url=$(sed -n 's/.*"url": *"\([^"]*\)".*/\1/p' <<<"$orig"); slow=$(sed -n 's/.*"slow": *\([0-9.]*\).*/\1/p' <<<"$orig")
# shellcheck disable=SC2016 # expands at exit, like a trap string
on_exit 'r "printf %s '"'"'$orig'"'"' > $F"'
for arm in "${arms[@]}"; do
  l=''
  case $arm in
    refused) v=cori u=${url%:*}:8099 ;; blackhole) v=cori u=http://10.255.255.1:8091 ;;
    mari) v=mari u=$url l=',"lang":"et-EE"' ;; *) v=$arm u=$url ;;
  esac
  r "printf '{\"url\":\"%s\",\"voice\":\"%s\",\"slow\":%s%s}' '$u' '$v' '$slow' '$l' > $F"
  for i in $(seq "$RUNS"); do
    cap_on
    curl -s -o /dev/null -m 5 "http://$ip:8888/consolefunccall?func=SayText&args=$WORD"
    sleep 6
    cap_off "$OUT/${arm}_$i.wav"
  done
done
python3 -I "$self/onset.py" "$OUT"/*.wav
