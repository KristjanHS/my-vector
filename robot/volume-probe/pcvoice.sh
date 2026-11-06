#!/usr/bin/env bash
# Loudness of the PC voices vs stock at robot output, per track — V1 of 2026-11-05-vector-voice-verify.md (docs/plans/).
# T1 = wire-pod demo endpoint (LLM-answer path: SDK stream, or SayText for stock);
# T2 = engine console SayText with the robot voice file set (robot-internal path). Restores the voice file.
# Pass = a PC voice's active level within $BAR dB of the same track's stock line.
# Usage: [RUNS=2] [BAR=3] [WORDS=a-b-c] pcvoice.sh [voice…] — default voices: af_bella cori; mari speaks T1 as et-EE, the rest en-US.
set -uo pipefail
self=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/volume-probe/lib.sh
. "$self/lib.sh"
RUNS=${RUNS:-2} BAR=${BAR:-3}
WORDS=${WORDS:-The-time-is-ten-past-two-and-it-is-sunny-outside} # console args split on spaces: one word only
OUT=$main/robot/work/volume-probe/pcvoice; mkdir -p "$OUT"; rm -f "$OUT"/t[12]-*
F=/data/data/myvector-voice.json
# shellcheck disable=SC2029 # expanding on the client is the point
r() { ssh "${so[@]}" "root@$ip" "$@"; }
orig=$(r "cat $F") || { echo "FAIL: no $F on the robot (ship / voice-push first)" >&2; exit 2; }
url=$(sed -n 's/.*"url": *"\([^"]*\)".*/\1/p' <<<"$orig"); slow=$(sed -n 's/.*"slow": *\([0-9.]*\).*/\1/p' <<<"$orig")
trap 'r "printf %s '"'"'$orig'"'"' > $F"' EXIT
drop() { mv "$1" "$1.bad"; echo "WARN: $2 — run dropped" >&2; } # a fallback or failed run never reaches the averages
voices=(stock "$@"); ((${#voices[@]} > 1)) || voices=(stock af_bella cori)
for v in "${voices[@]}"; do
  case $v in # the demo speaks lang's T1 (default: the active lang) — name it, or another lang's pair would speak
  mari) pick="\"lang\":\"et-EE\",\"t1\":\"af_bella\",\"t1_et\":\"$v\"" ;; # t1 must still be a valid en-US voice
  *) pick="\"lang\":\"en-US\",\"t1\":\"$v\"" ;;
  esac
  for i in $(seq "$RUNS"); do # T1: the call returns once the line has played
    cap_on
    code=$(curl -s -o "$OUT/t1-$v-$i.json" -w '%{http_code}' -m 60 -H 'Content-Type: application/json' \
      -d "{$pick,\"slow\":$slow,\"text\":\"${WORDS//-/ }\"}" "$server/myvector/api/voice/test?serial=$serial")
    sleep 1
    cap_off "$OUT/t1-${v}-$i.wav"
    grep -q "\"voice\":\"$v\"" "$OUT/t1-$v-$i.json" ||
      drop "$OUT/t1-$v-$i.wav" "T1 $v run $i: HTTP $code, spoke $(cat "$OUT/t1-$v-$i.json")"
  done
  r "printf '{\"url\":\"%s\",\"voice\":\"%s\",\"slow\":%s}' '$url' '$v' '$slow' > $F" || { echo "FAIL: voice file write" >&2; exit 1; }
  for i in $(seq "$RUNS"); do
    n0=$(r "wc -l < /var/log/messages")
    cap_on
    curl -s -o /dev/null -m 5 "http://$ip:8888/consolefunccall?func=SayText&args=$WORDS"
    sleep 8
    cap_off "$OUT/t2-${v}-$i.wav"
    log=$(r "tail -n +$((n0 + 1)) /var/log/messages | grep 'my-vector.Voice'")
    [[ $v == stock ]] || { grep -q "pc voice=$v" <<<"$log" && ! grep -q acapela <<<"$log"; } ||
      drop "$OUT/t2-$v-$i.wav" "T2 $v run $i: not spoken by the PC voice (${log:-no my-vector.Voice log})"
  done
done
# db.py: "<file> peak P loud50ms L active A" → mean active dB per track×voice, delta vs that track's stock
python3 -I "$self/db.py" "$OUT"/t[12]-*.wav | awk -v bar="$BAR" '
  $NF <= -90 { print "WARN: " $1 " silent — run dropped" > "/dev/stderr"; next }
  { split($1, n, "-"); k = n[1] "-" n[2]; s[k] += $NF; c[k]++; if (!(k in seen)) { seen[k]; o[++m] = k } }
  END {
    bad = 0
    for (j = 1; j <= m; j++) { k = o[j]; a = s[k] / c[k]; split(k, n, "-")
      if (!c[n[1] "-stock"]) { printf "%-4s %-9s no stock reference\n", toupper(n[1]), n[2]; bad = 1; continue }
      ref = s[n[1] "-stock"] / c[n[1] "-stock"]
      d = a - ref; v = (n[2] == "stock") ? "ref" : ((d >= -bar && d <= bar) ? "PASS" : "GAP")
      if (v == "GAP") bad = 1
      printf "%-4s %-9s active %6.1f dBFS  vs stock %+5.1f dB  %s\n", toupper(n[1]), n[2], a, d, v }
    exit bad }'
