#!/usr/bin/env bash
# Slice 3b soak: log volts + on-dock from the fork every INTERVAL s for HOURS h, then judge the log.
# Usage: soak.sh [hours=24] [interval_s=300] | soak.sh --summary <log>   (env: ESN, SERVER, LOG)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)

# PASS = no off-dock sample and the mean of the last 6 readings is above the mean of the first 6.
summary() {
  awk '
    $3 == "off" { off++; print "off-dock: " $1 " " $2 }
    $3 == "?"   { miss++ }
    $3 == "on" || $3 == "off" { v[n++] = $4 }
    END {
      if (n < 12) { printf "INCOMPLETE: %d readings (need 12), %d failed polls\n", n, miss; exit 1 }
      for (i = 0; i < 6; i++) { a += v[i]; b += v[n - 6 + i] }
      d = (b - a) / 6
      verdict = (off == 0 && d > 0) ? "PASS" : "FAIL"
      printf "%s: %d readings, %d off-dock, %d failed polls, volts %.3f -> %.3f (%+.3f)\n",
        verdict, n, off, miss, a / 6, b / 6, d
      exit verdict == "PASS" ? 0 : 1
    }' "$1"
}

if [[ ${1:-} == --summary ]]; then summary "${2:?log path}"; exit; fi

hours=${1:-24} interval=${2:-300}
server=${SERVER:-http://localhost:8080}
esn=${ESN:-$(sed -n 's/^esn: *//p' "$here"/../backup/robot-id-*.txt 2>/dev/null | head -1)}
[[ -n $esn ]] || { echo "FAIL: no ESN; set ESN=<esn>" >&2; exit 2; }
log=${LOG:-$(cd "$here/../work" && pwd)/soak-$(date +%F-%H%M).log}
echo "logging to $log every ${interval}s for ${hours}h"
trap 'echo; summary "$log"; exit' INT TERM

end=$(( $(date +%s) + hours * 3600 ))
while (( $(date +%s) < end )); do
  resp=$(curl -s -m 10 "$server/api-sdk/get_battery?serial=$esn" || true)
  volts=$(sed -n 's/.*"battery_volts":\([0-9.]*\).*/\1/p' <<<"$resp")
  # The fork omits false booleans, so a valid reading without the true flag means off the dock.
  if [[ -z $volts ]]; then dock='?'
  elif [[ $resp == *'"is_on_charger_platform":true'* ]]; then dock=on
  else dock=off; echo "DRIVE-OFF at $(date +%T)" >&2
  fi
  echo "$(date '+%F %T') $dock ${volts:--}" >>"$log"
  sleep "$interval"
done
summary "$log"
