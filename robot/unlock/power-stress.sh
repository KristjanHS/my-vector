#!/usr/bin/env bash
# Slice 0 power stress: holds Vector on the dock (AUTO_HOLD=override), tilts his head and talks every minute;
# a ping-reply gap >= 20 s counts as one reboot. Needs chipper. Usage: power-stress.sh [minutes] (default 25)
set -uo pipefail
mins=${1:-25}
ids=("$(dirname "$0")"/../backup/robot-id-*.txt); id=${ids[-1]}
[[ -f $id ]] ||{ echo "FAIL: no robot/backup/robot-id-*.txt" >&2; exit 1; }
esn=$(awk '/^esn:/{print $2}' "$id"); ip=$(awk '/^ip:/{print $2}' "$id")
[[ -n $esn && -n $ip ]] || { echo "FAIL: esn or ip missing in $id" >&2; exit 1; }
ping -c1 -W2 "$ip" >/dev/null 2>&1 || { echo "FAIL: $ip doesn't answer ping; is he on and on Wi-Fi?" >&2; exit 1; }
api="http://localhost:8080/api-sdk"
sdk() { curl -s -m 20 -G "$api/$1" --data-urlencode "serial=$esn" ${2:+--data-urlencode "$2"}; } # encodes spaces in say_text
# assume_behavior_control answers success before doing anything; chipper's log is the only confirmation.
hold() { sdk assume_behavior_control priority=high >/dev/null; }

bat=$(sdk get_battery)
grep -q '"is_on_charger_platform":true' <<<"$bat" || { echo "FAIL: not on the charger; get_battery said: ${bat:-no reply}" >&2; exit 1; }

log=$(mktemp); pinger=
cleanup() {
  [[ -n $pinger ]] && kill "$pinger" 2>/dev/null
  if [[ $(sdk release_behavior_control) == success ]]; then echo "hold released"; else echo "WARN: release failed; release it from the web UI" >&2; fi
  rm -f "$log"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
hold
echo "Holding $esn ($ip) for $mins min. Don't touch him or the strip."
(while :; do ping -c1 -W1 "$ip" >/dev/null 2>&1 && echo "$EPOCHSECONDS" >>"$log"; sleep 1; done) & pinger=$!

gaps=0; seen=0; last=$EPOCHSECONDS; quiet=0; n=0; next_load=$EPOCHSECONDS; end=$((EPOCHSECONDS + mins * 60))
scan() { # reads new reply timestamps; sets back=1 when one ends a gap >= 20 s
  local new t; back=0
  mapfile -t -s "$seen" new <"$log"; seen=$((seen + ${#new[@]}))
  for t in "${new[@]}"; do
    if ((t - last >= 20)); then gaps=$((gaps + 1)); back=1; echo "$(date +%T) BACK after $((t - last))s (probable reboot #$gaps)"; fi
    last=$t
  done
}
while ((EPOCHSECONDS < end)); do
  scan
  if ((back)); then
    until_t=$((EPOCHSECONDS + 120))
    until [[ $(sdk get_battery) == \{* ]] || ((EPOCHSECONDS >= until_t)); do sleep 5; done
    ((EPOCHSECONDS < until_t)) || echo "$(date +%T) WARN: no get_battery reply for 120 s; re-taking the hold anyway"
    hold
  fi
  if ((EPOCHSECONDS - last >= 20)); then
    ((quiet)) || { quiet=1; echo "$(date +%T) no ping reply since $(date -d "@$last" +%T)"; }
  else quiet=0; fi
  if ((EPOCHSECONDS - last < 5 && EPOCHSECONDS >= next_load)); then
    n=$((n + 1)); next_load=$((EPOCHSECONDS + 60))
    # Head only: a lift pressing on a blocker can pry him off the contacts, and he has no battery to survive that.
    sdk move_head speed=5 >/dev/null; sleep 1; sdk move_head speed=-5 >/dev/null
    sdk say_text "text=Power test, minute $n" >/dev/null
    bat=$(sdk get_battery)
    if [[ $bat != \{* ]]; then st="get_battery failed: ${bat:-no reply}"
    else # false fields are omitted from the JSON, so a missing true means off the charger
      st=$(grep -oE '"battery_volts":[0-9.]+' <<<"$bat")
      if grep -q '"is_on_charger_platform":true' <<<"$bat"; then st+=" ON-CHARGER"; else st+=" OFF-CHARGER"; fi
    fi
    echo "$(date +%T) load #$n  $st"
  fi
  sleep 5
done
scan
gap=$((EPOCHSECONDS - last))
((gap >= 20)) && { gaps=$((gaps + 1)); echo "still unreachable at the end, ${gap}s (counted as a reboot)"; }
if ((gaps == 0)); then echo "PASS: no reboot in $mins min"; else echo "REBOOTED $gaps time(s): guards are mandatory (ruling: proceed)"; fi
