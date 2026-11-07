#!/usr/bin/env bash
# Night-window check (0011-night-window.patch): did Vector do anything overnight besides sleep and answer "Hey Vector"?
# Reads the robot's kept log (/data/log/keep, ~20 h deep) for the night that starts on DATE at NIGHT_FROM.
# Usage: night-check.sh [YYYY-MM-DD]   default: the most recent night start (yesterday's if run before NIGHT_FROM).
#   env NIGHT_FROM=21:00 NIGHT_TO=09:00 (the firmware's SleepTracker defaults); NIGHT_LOG_DIR=<dir> reads that local dir, no ssh (host test).
# Allowed: GoToSleep anims, the InitialWakeUp anim ≤30 s after a vic-engine start ≤60 s after a system-partition remount
#   (a ship; printed + counted as ships — a reboot's remount carries the stale boot clock, so a reboot stays a hit),
#   and anything within 60 s after a voice event (wake word, listening, TTS, SDK, voice intent).
# Hit: any other logged animation or feature start, a wake_up not for VoiceCommand/TimerShouldRing, a fall into LightSleep.
# Blind spots: DAS-muted idle anims (e.g. ObservingLookUp) and scripted head moves are never logged as animations.
# Exit: 0 PASS (no hits; the log spans the whole night and shows him asleep) | 1 FAIL (hits) | 2 setup error
#   | 3 INCOMPLETE (night not over, log starts too late or stops early, or no sleeping anim in the window).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
[[ -n ${NIGHT_LOG_DIR:-} ]] || . "$here/../env.sh"
from=${NIGHT_FROM:-21:00} to=${NIGHT_TO:-09:00}
day=${1:-$(if [[ $(date +%H:%M) < $from ]]; then date -d yesterday +%F; else date +%F; fi)}
start=$(date -d "$day $from" +%s) || { echo "FAIL: bad date or NIGHT_FROM: $day $from" >&2; exit 2; }
# A window past midnight ends on the next calendar day (not start + 24 h: a DST night is 23 or 25 h long).
end_day=$day
[[ $to > $from ]] || end_day=$(date -d "$day +1 day" +%F)
end=$(date -d "$end_day $to" +%s) || { echo "FAIL: bad NIGHT_TO: $to" >&2; exit 2; }
fmt() { date -d "@$1" '+%m-%d %H:%M:%S'; }
now=$(date +%s)
echo "night $(fmt "$start") .. $(fmt "$end")"

# Log stamps are "MM-DD HH:MM:SS.mmm" with no year: awk compares them as strings; a January line after a December
# start (a New Year night) is shifted to month 13. ssh joins the remote args into one string, hence the inner quotes.
prog=$(cat <<'EOF'
s=$1 e=$2 over=$3
cd "${4:-/data/log/keep}" || { echo "FAIL: no ${4:-/data/log/keep} (robot/logkeep/apply.sh)"; exit 2; }
cat $(ls -tr messages.* 2>/dev/null) messages | awk -v s="$s" -v e="$e" -v over="$over" '
  function key(stamp,   m) { m = substr(stamp, 1, 2) + 0; if (sm == 12 && m == 1) m = 13; return sprintf("%02d%s", m, substr(stamp, 3)) }
  function sec(t) { return substr(t, 7, 2) * 3600 + substr(t, 10, 2) * 60 + substr(t, 13, 2) }
  function hit(what) { hits++; print "  hit (" what "): " substr(l, 1, 170) }
  BEGIN { sm = substr(s, 1, 2) + 0; ks = key(s); ke = key(e); vlast = estart = rmnt = -1e9 }
  $1 !~ /^[0-9][0-9]-[0-9][0-9]$/ { next }
  # first/last in file order: a boot line can carry the stale pre-sync clock (e.g. "10-25" in a November log)
  { k = key($1 " " $2); if (first == "") first = k; last = k }
  k < ks || k >= ke { next }
  { t = sec(k) + (substr(k, 1, 5) != substr(ks, 1, 5)) * 86400 }
  / kernel  EXT4-fs \(mmcblk0p22\): re-mounted\./ { rmnt = t; next }
  / systemd 1 Started Victor engine daemon\./ { if (t - rmnt <= 60) estart = t; next }
  index($0, "@") == 0 { next }
  {
    l = $0; gsub("\037", "|", l)
    if (l ~ /wakeword\.triggered|speech_trigger_recognized|\|VC_|tts|\|SDK\||voice_command|VoiceIntent|\|Voice\|/) vlast = t
    invoice = t >= vlast && t - vlast <= 60
  }
  l ~ /@action\.play_animation\|[^|]*\|[^|]*\|InitialWakeUp\|/ && t >= estart && t - estart <= 30 { ships++; print "  ship: " substr(l, 1, 170); next }
  l ~ /\|GoToSleepSleeping\|/ { asleep++ }
  l ~ /@behavior\.sleeping\.falling_asleep/ { sleeps++; if (l ~ /NightWindow/) nw++ }
  l ~ /@behavior\.sleeping\.falling_asleep\|[^|]*\|LightSleep/ { hit("light sleep at night") }
  l ~ /@behavior\.sleeping\.wake_up/ && l !~ /wake_up\|(VoiceCommand|TimerShouldRing)\|/ && !invoice { hit("woke up") }
  l ~ /@action\.play_animation/ && l !~ /\|GoToSleep(Sleeping|GetIn)\|/ && !invoice { hit("animation") }
  l ~ /@behavior\.feature\.start/ && l !~ /feature\.start\|(Sleeping|ObservingOnCharger)\|/ && !invoice { hit("feature") }
  END {
    why = first == "" || first > ks ? "log does not reach back to the night start" \
      : !over ? "night not over yet" : last < ke ? "log stops at " last " (robot down or syslog stalled?)" \
      : !asleep ? "no sleeping anim in the window" : ""
    printf "log %s .. %s: hits %d, ships %d, sleeping anims %d, falling asleep %d (NightWindow %d)\n", first, last, hits, ships, asleep, sleeps, nw
    v = hits ? "FAIL" : why != "" ? "INCOMPLETE" : "PASS"
    if (v == "INCOMPLETE") print "  (" why ")"
    print v; exit v == "PASS" ? 0 : v == "FAIL" ? 1 : 3
  }'
EOF
)
if [[ -n ${NIGHT_LOG_DIR:-} ]]; then sh -s -- "$(fmt "$start")" "$(fmt "$end")" "$((now >= end))" "$NIGHT_LOG_DIR" <<<"$prog"
else ssh "${ssh_opts[@]}" "root@$ip" sh -s -- "'$(fmt "$start")'" "'$(fmt "$end")'" "$((now >= end))" <<<"$prog"; fi
