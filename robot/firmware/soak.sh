#!/usr/bin/env bash
# Slice 3b soak: log volts + on-dock from the fork every INTERVAL s for HOURS h, then judge the log.
# Usage: soak.sh [hours=24] [interval_s=300] | soak.sh --summary [log=$LOG or newest soak-*.log] | soak.sh --restart [hours] [interval_s]
#   (env: ESN, SERVER, LOG, WORK=robot/work). --restart stops the running soak and starts a detached one (make ship runs it).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=${WORK:-$here/../work}
mkdir -p "$work"; work=$(cd "$work" && pwd)

# PASS = no off-dock reading, last-6 mean volts above first-6 mean, the target span covered,
# no gap over 3 intervals between readings, failed polls at most 10%, no ship mid-run. Otherwise FAIL or INCOMPLETE.
summary() {
  awk '
    BEGIN { n = 0 }
    /^#/ { for (i = 2; i <= NF; i++) { split($i, kv, "="); h[kv[1]] = kv[2] }; next }
    $3 == "?" { miss++; next }
    $3 == "off" { off++; print "off-dock: " $1 " " $2 }
    $3 == "on" || $3 == "off" {
      v[n] = $4; t[n] = $5
      if (n && t[n] - t[n - 1] > gap) gap = t[n] - t[n - 1]
      if (!n || $4 < lo) lo = $4
      n++
    }
    END {
      th = h["target_h"] ? h["target_h"] : 24; iv = h["interval_s"] ? h["interval_s"] : 300
      shipped = "shipped" in h && h["shipped"] != h["sha"]
      if (shipped) printf "firmware changed mid-soak: %s -> %s\n", substr(h["sha"], 1, 12), substr(h["shipped"], 1, 12)
      if (n < 12) { printf "INCOMPLETE: %d readings (need 12), %d failed polls\n", n, miss; exit 1 }
      for (i = 0; i < 6; i++) { a += v[i]; b += v[n - 6 + i] }
      d = (b - a) / 6; span = t[n - 1] - t[0]
      if (off || d <= 0) verdict = "FAIL"
      else if (shipped || span < th * 3600 - iv || gap > 3 * iv || miss * 10 > n + miss) verdict = "INCOMPLETE"
      else verdict = "PASS"
      printf "%s: %.1fh of %dh, %d readings, %d off-dock, %d failed polls, max gap %ds, volts min %.3f, %.3f -> %.3f (%+.3f)\n",
        verdict, span / 3600, th, n, off, miss, gap, lo, a / 6, b / 6, d
      exit verdict == "PASS" ? 0 : 1
    }' "$1"
}

if [[ ${1:-} == --summary ]]; then
  log=${2:-${LOG:-$(ls -t "$work"/soak-*.log 2>/dev/null | head -1 || true)}}
  [[ -f $log ]] || { echo "FAIL: no soak log found; pass one: soak.sh --summary <log>" >&2; exit 2; }
  echo "$log"; summary "$log"; exit
fi

if [[ ${1:-} == --restart ]]; then
  shift
  pids=$(cat "$work/soak.pid" 2>/dev/null || true)
  # Soaks started before the pidfile existed: only the real work dir has them.
  [[ -n $pids || -n ${WORK:-} ]] || pids=$(pgrep -f '^(/usr)?(/bin/)?bash [^ ]*/soak\.sh( [0-9]+){0,2}$' || true)
  for p in $pids; do
    [[ $(tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null) == *soak.sh* ]] || continue
    echo "stopping soak pid $p"
    kill -TERM "$p"; pkill -TERM -P "$p" -x sleep || true
    for _ in {1..20}; do kill -0 "$p" 2>/dev/null || break; sleep 0.5; done
    ! kill -0 "$p" 2>/dev/null || { echo "FAIL: soak pid $p still running after 10 s" >&2; exit 1; }
  done
  [[ -z $pids ]] || tail -n 3 "$work/soak.out" 2>/dev/null || true
  rm -f "$work/soak.pid"
  setsid nohup "$here/soak.sh" "$@" >"$work/soak.out" 2>&1 </dev/null &
  for _ in {1..20}; do [[ -s $work/soak.pid ]] && break; sleep 0.5; done
  [[ -s $work/soak.pid ]] || { echo "FAIL: soak did not start; see $work/soak.out:" >&2; cat "$work/soak.out" >&2; exit 1; }
  echo "soak started: pid $(<"$work/soak.pid"), $(head -1 "$work/soak.out")"
  exit
fi

hours=${1:-24} interval=${2:-300}
[[ $hours =~ ^[1-9][0-9]*$ && $interval =~ ^[1-9][0-9]*$ ]] || { echo "FAIL: hours and interval_s must be whole numbers" >&2; exit 2; }
server=${SERVER:-http://localhost:8080}
esn=${ESN:-$(sed -n 's/^esn: *//p' "$here"/../backup/robot-id-*.txt 2>/dev/null | head -1 || true)}
[[ -n $esn ]] || { echo "FAIL: no ESN; set ESN=<esn>" >&2; exit 2; }
# make ship writes the full sha (line 1) and the comma-joined series (line 2) before deploying.
shipped() { sed -n 1p "$work/shipped" 2>/dev/null | grep . || echo unknown; }
sha=$(shipped) series=$(sed -n 2p "$work/shipped" 2>/dev/null || true)
log=${LOG:-$work/soak-$(date +%F-%H%M%S).log}
echo "logging to $log every ${interval}s for ${hours}h (firmware ${sha:0:12})"
echo "# target_h=$hours interval_s=$interval sha=$sha series=${series:--}" >>"$log"
echo $$ >"$work/soak.pid"
stop() { [[ $(shipped) == "$sha" ]] || echo "# shipped=$(shipped)" >>"$log"; echo; summary "$log"; }
trap 'stop; exit' INT TERM
trap '[[ $(cat "$work/soak.pid" 2>/dev/null) != $$ ]] || rm -f "$work/soak.pid"' EXIT

end=$(( $(date +%s) + hours * 3600 ))
while (( $(date +%s) < end )) && [[ $(shipped) == "$sha" ]]; do
  resp=$(curl -s -m 20 "$server/api-sdk/get_battery?serial=$esn" || true)
  volts=$(sed -n 's/.*"battery_volts":\([0-9.]*\).*/\1/p' <<<"$resp")
  # The fork omits false booleans, so a valid reading without the true flag means off the dock.
  if [[ -z $volts ]]; then dock='?'
  elif [[ $resp == *'"is_on_charger_platform":true'* ]]; then dock=on
  else dock=off; echo "DRIVE-OFF at $(date +%T)" >&2
  fi
  echo "$(date '+%F %T') $dock ${volts:--} $(date +%s)" >>"$log"
  sleep "$interval" & wait $!
done
stop
