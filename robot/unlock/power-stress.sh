#!/usr/bin/env bash
# Slice 0 power stress test: holds Vector on the dock (same as AUTO_HOLD=override), then every minute
# tilts his head and makes him talk, to load him while docked without moving him on the contacts.
# Pings him every 5 s; a gap of 20 s or more means he probably rebooted. Needs chipper running.
# Usage: power-stress.sh [minutes]   (default 25). Ctrl-C stops early and still releases the hold.
set -uo pipefail
mins=${1:-25}
id=$(ls "$(dirname "$0")"/../backup/robot-id-*.txt 2>/dev/null | tail -1)
[[ -n $id ]] || { echo "FAIL: no robot/backup/robot-id-*.txt" >&2; exit 1; }
esn=$(awk '/^esn:/{print $2}' "$id"); ip=$(awk '/^ip:/{print $2}' "$id")
api="http://localhost:8080/api-sdk"
sdk() { curl -s -m 20 -G "$api/$1" --data-urlencode "serial=$esn" ${2:+--data-urlencode "$2"}; } # encodes spaces in say_text

[[ $(sdk conn_test) == success ]] || { echo "FAIL: chipper can't reach $esn; start chipper first" >&2; exit 1; }
sdk get_battery | grep -q '"is_on_charger_platform":true' || { echo "FAIL: not on the charger" >&2; exit 1; }
sdk assume_behavior_control priority=high >/dev/null
trap 'sdk release_behavior_control >/dev/null; echo "hold released"' EXIT
echo "Holding $esn ($ip) for $mins min. Don't touch him or the strip."

gaps=0; down_since=; end=$((SECONDS + mins * 60)); next_load=$SECONDS; n=0
while ((SECONDS < end)); do
  if ping -c1 -W2 "$ip" >/dev/null 2>&1; then
    if [[ -n $down_since ]]; then
      gap=$((SECONDS - down_since))
      if ((gap >= 20)); then gaps=$((gaps + 1)); echo "$(date +%T) BACK after ${gap}s (probable reboot #$gaps)"
        sdk assume_behavior_control priority=high >/dev/null; fi
      down_since=
    fi
    if ((SECONDS >= next_load)); then
      n=$((n + 1)); next_load=$((SECONDS + 60))
      # Head only: a lift pressing on a blocker can pry him off the contacts, and he has no battery to survive that.
      sdk move_head speed=5 >/dev/null; sleep 1; sdk move_head speed=-5 >/dev/null
      sdk say_text "text=Power test, minute $n" >/dev/null
      echo "$(date +%T) load #$n  $(sdk get_battery | grep -oE '"battery_volts":[0-9.]+|"is_on_charger_platform":(true|false)' | tr '\n' ' ')"
    fi
  else
    [[ -n $down_since ]] || { down_since=$SECONDS; echo "$(date +%T) no ping reply"; }
  fi
  sleep 5
done
[[ -n $down_since ]] && { gaps=$((gaps + 1)); echo "still unreachable at the end (counted as a reboot)"; }
if ((gaps == 0)); then echo "PASS: no reboot in $mins min"; else echo "REBOOTED $gaps time(s): guards are mandatory (ruling: proceed)"; fi
