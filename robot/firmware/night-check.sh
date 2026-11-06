#!/usr/bin/env bash
# Night-window check (0011-night-window.patch): did Vector do anything overnight besides sleep and answer "Hey Vector"?
# Reads the robot's kept log (/data/log/keep, ~20 h deep) for the night that starts on DATE at NIGHT_FROM.
# Usage: night-check.sh [YYYY-MM-DD]   default: the most recent night start (yesterday's if run before NIGHT_FROM).
#   env NIGHT_FROM=21:00 NIGHT_TO=09:00 (the firmware's SleepTracker defaults).
# Allowed: GoToSleep anims, and anything within 60 s of a voice event (wake word, listening, TTS, SDK, voice intent).
# Hit: any other animation or feature start, a wake_up not for VoiceCommand/TimerShouldRing, a fall into LightSleep.
# Exit: 0 PASS (no hits, whole night in the log) | 1 FAIL (hits) | 3 INCOMPLETE (night not over, or log starts too late).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
from=${NIGHT_FROM:-21:00} to=${NIGHT_TO:-09:00}
day=${1:-$(if [[ $(date +%H:%M) < $from ]]; then date -d yesterday +%F; else date +%F; fi)}
start=$(date -d "$day $from" +%s) || { echo "FAIL: bad date or NIGHT_FROM: $day $from" >&2; exit 2; }
end=$(date -d "$day $to" +%s) || { echo "FAIL: bad NIGHT_TO: $to" >&2; exit 2; }
((end > start)) || end=$((end + 86400))
fmt() { date -d "@$1" '+%m-%d %H:%M:%S'; }
now=$(date +%s)
echo "night $(fmt "$start") .. $(fmt "$end")"

# Log stamps are "MM-DD HH:MM:SS.mmm" with no year: awk compares them as strings, months before the start month
# (a New Year night) shifted past 12.
ssh "${ssh_opts[@]}" "root@$ip" sh -s -- "'$(fmt "$start")'" "'$(fmt "$end")'" "$((now >= end))" <<'EOF'
s=$1 e=$2 over=$3
cd /data/log/keep || { echo "FAIL: no /data/log/keep on the robot (robot/logkeep/apply.sh)"; exit 2; }
cat $(ls -tr messages.* 2>/dev/null) messages | awk -v s="$s" -v e="$e" -v over="$over" '
  function key(stamp,   m) { m = substr(stamp, 1, 2) + 0; if (m < sm) m += 12; return sprintf("%02d%s", m, substr(stamp, 3)) }
  function sec(t) { return substr(t, 7, 2) * 3600 + substr(t, 10, 2) * 60 + substr(t, 13, 2) }
  BEGIN { sm = substr(s, 1, 2) + 0; ks = key(s); ke = key(e); vlast = -1e9 }
  $1 !~ /^[0-9][0-9]-[0-9][0-9]$/ { next }
  { k = key($1 " " $2) }
  first == "" { first = k }
  k < ks || k >= ke { next }
  index($0, "@") == 0 { next }
  {
    l = $0; gsub("\037", "|", l); t = sec(k) + (substr(k, 1, 5) != substr(ks, 1, 5)) * 86400
    voice = l ~ /wakeword\.triggered|speech_trigger_recognized|\|VC_|tts|\|SDK\||voice_command|VoiceIntent|\|Voice\|/
    if (voice) vlast = t
    invoice = t - vlast <= 60
  }
  l ~ /@behavior\.sleeping\.falling_asleep/ { sleeps++; if (l ~ /NightWindow/) nw++ }
  l ~ /@behavior\.sleeping\.falling_asleep\|[^|]*\|LightSleep/ { hit("light sleep at night") }
  l ~ /@behavior\.sleeping\.wake_up/ && l !~ /wake_up\|(VoiceCommand|TimerShouldRing)\|/ && !invoice { hit("woke up") }
  l ~ /@action\.play_animation/ && l !~ /\|GoToSleep(Sleeping|GetIn)\|/ && !invoice { hit("animation") }
  l ~ /@behavior\.feature\.start/ && l !~ /feature\.start\|(Sleeping|ObservingOnCharger)\|/ && !invoice { hit("feature") }
  function hit(what) { hits++; print "  hit (" what "): " substr(l, 1, 170) }
  END {
    covered = first != "" && first <= ks
    printf "log from %s: hits %d, falling asleep %d (NightWindow %d)\n", first == "" ? "nothing" : first, hits, sleeps, nw
    v = hits ? "FAIL" : covered && over ? "PASS" : "INCOMPLETE"
    if (v == "INCOMPLETE") print "  (" (covered ? "night not over yet" : "log does not reach back to the night start") ")"
    print v; exit v == "PASS" ? 0 : v == "FAIL" ? 1 : 3
  }'
EOF
