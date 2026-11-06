#!/usr/bin/env bash
# On-robot smoke test (make robot-test; make ship after deploy): units up and stable, clean vic-robot log, wire-pod link, docked,
# head moves to both limits, face + speech work, log-level intent, wheels never moved.
# Usage: smoke.sh   → PASS (exit 0); FAIL (1) names the first failing check; SETUP (2) = no ESN/key/SSH/wire-pod link.  (env: ESN, SERVER)
# Moves only the head: check-static.sh S5 fails on a wheel/lift name in this file or fork_smoke.go (denylist), on any `sdk`/api-sdk
# endpoint here not in its allowlist or not literal (cloud_intent only via intent my_vector_log_*), and on any Conn method in fork_smoke.go
# other than EventStream/DisplayFaceImageRGB/SayText (myvector/voice: ExternalAudioStreamPlayback/SayText/BehaviorControl). A2 speaks in the saved language's T1 (PC voice :8091 or stock), WARN on fallback.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
units=(vic-engine vic-anim vic-robot)
fatal='Fatal|SIGSEGV|SIGABRT|terminate called|Backtrace'
die() { # once the units are up ($up), a failing check also reports a unit that died meanwhile
  echo "FAIL: $*" >&2
  [[ -z ${up:-} || $(pids) == "$up" ]] || crash_report >&2
  exit 1
}
setup() { echo "SETUP: $*" >&2; exit 2; }
ok() { echo "ok: $*"; }
steps=9 step_n=0
step() { echo "== [$((++step_n))/$steps] $*  (+${SECONDS} s)"; } # progress header before each stage
on_exit=() # cleanup commands, run in order by the EXIT trap
trap 'for c in "${on_exit[@]}"; do eval "$c" || true; done' EXIT

ssh_() { ssh "${ssh_opts[@]}" -o ServerAliveInterval=3 -o ServerAliveCountMax=2 "root@$ip" "$@"; }
pids() { # the units' PIDs; status 1 = a unit is down, 255 = ssh transport failure (setup, not firmware)
  local out; out=$(ssh_ "for u in ${units[*]}; do systemctl is-active -q \$u || exit 1; pidof -s \$u || exit 1; done" 2>/dev/null) || return $?
  echo $out # unquoted: one line
}
same_pids() { # same_pids <when>: PIDs still $up, else die; lost SSH = setup
  local now rc=0; now=$(pids) || rc=$?
  ((rc != 255)) || setup "lost SSH to $ip"
  [[ $now == "$up" ]] || die_down "units: a PID changed $1: $up → ${now:-a unit down}"
}
# On a unit failure: each unit's systemd state + the robot journal's crash lines since a minute before the smoke start ($t0, robot clock).
crash='Fatal|SIGSEGV|SIGABRT|terminate called|Backtrace|SeAssert|caught signal|terminated by signal|#[0-9]+ pc |Main process exited'
# First crash first: backtraces capped at 5 frames, 25 lines total; 30 s max (systemctl can stall while the fault handler tears down).
crash_report() {
  timeout 30 ssh "${ssh_opts[@]}" -o ServerAliveInterval=3 -o ServerAliveCountMax=2 "root@$ip" "
for u in ${units[*]}; do echo \"\$u: \$(systemctl show -p ActiveState -p SubState -p MainPID -p ExecMainStatus \$u | tr '\n' ' ')\"; done
echo 'crash lines (robot journal):'
if j=\$(journalctl -o short-iso --no-pager --since @$((t0 - 60))); then
  printf '%s\n' \"\$j\" | grep -E '$crash' | awk '/#[0-9]+ pc /{ if (++f > 5) next; print; next } { f = 0; print }' | head -n 25 | grep . || echo '  none'
else echo '  journalctl failed'; fi" 2>/dev/null || echo "(no crash report: SSH failed or timed out)"
}
die_down() { echo "FAIL: $*" >&2; crash_report >&2; exit 1; }
docked() {
  local b; b=$(curl -s -m 20 "$server/api-sdk/get_battery?serial=$esn" || true)
  [[ $b == *'"is_on_charger_platform":true'* ]] || die "docked ($1): not on the charger platform: ${b:0:200}"
  local bat; bat=$(jq -r '"level \(["UNKNOWN","LOW","NOMINAL","FULL"][.battery_level // 0] // .battery_level), \((.battery_volts // 0) * 100 | round / 100) V"' <<<"$b" 2>/dev/null || true)
  [[ $b == *'"is_charging":true'* ]] && ok "docked ($1), charging ($bat)" || echo "WARN: docked ($1), not charging ($bat)"
}

# SSH first: no SSH is a setup failure (exit 2), not a firmware crash.
for try in 1 2 3; do
  ! ssh_ true 2>/dev/null || break
  ((try < 3)) || setup "no SSH to $ip"; sleep 3
done

step "units up and stable (up to 195 s)"
# P1: units active within 180 s, PIDs unchanged 15 s later (and at the end).
t0=$(ssh_ 'date +%s' 2>/dev/null) || t0=$(date +%s)
end=$((SECONDS + 180))
while :; do
  rc=0; up=$(pids) || rc=$?
  ((rc)) || break
  if ((SECONDS >= end)); then
    ((rc != 255)) || setup "lost SSH to $ip"
    die_down "units: ${units[*]} not all active within 180 s"
  fi
  sleep 5
done
sleep 15
same_pids "within 15 s (crash loop?)"
ok "units active, stable 15 s: $up"

step "wire-pod link"
# P1: wire-pod reaches the robot within 20 s (else the setup, not the firmware, is broken).
end=$((SECONDS + 20))
until [[ $(curl -s -m 20 "$server/api-sdk/conn_test?serial=$esn" || true) == success ]]; do
  ((SECONDS < end)) || setup "wire-pod at $server has no link to $esn (conn_test)"; sleep 2
done
ok "wire-pod conn_test"

step "docked, clean vic-robot log"
# P2: docked before the checks.
docked before

# P1: /data has room. Full, syslog-ng stalls and the log check below finds no Startup line (2026-11-05: an 822 MB output capture).
use=$(ssh_ "df -P /data | awk 'NR == 2 { sub(\"%\", \"\", \$5); print \$5 }'") || setup "lost SSH to $ip"
[[ $use =~ ^[0-9]+$ ]] || setup "df /data on $ip gave '$use', not a percentage"
((use < 90)) || die "/data ${use}% full; biggest (KB): $(ssh_ 'find /data -xdev -type f -exec du -k {} + 2>/dev/null | sort -n | tail -3' | tr '\n' ' ')"
ok "/data ${use}% used"

# P1: since the running vic-robot's last Startup lift calibration: no crash lines, no recalibration loop (3 within 60 s).
logcheck() {
  local log
  log=$(ssh_ "FATAL='$fatal' sh -s" <<'EOF'
pid=$(pidof -s vic-robot) || { echo "vic-robot not running"; exit 1; }
awk -v tag="vic-robot $pid " '
  index($0, tag) && /LiftController\.Calibrated.*reason Startup/ { on = 1; n = 0; loop = 0; bad = 0; msg = "" }
  !on { next }
  /LiftController\.Calibrated/ { split($2, t, ":"); c[++n] = t[1] * 3600 + t[2] * 60 + t[3]
    if (n >= 3 && (c[n] - c[n - 2] + 86400) % 86400 < 60) loop++ }
  $0 ~ ENVIRON["FATAL"] { bad++; msg = msg "fatal: " substr($0, 1, 160) "\n" }
  END { printf "%slift calibrated %d since Startup, loop hits %d, crash lines %d", msg, n, loop, bad; exit !(on && !loop && !bad) }' $(ls -tr /var/log/messages.* 2>/dev/null) /var/log/messages
EOF
) || die "log ($1): $log"
  ok "log ($1): $log"
}
logcheck start

step "vic-engine web server"
# P1: vic-engine's web server answers.
code=$(curl -s -m 5 -o /dev/null -w '%{http_code}' "http://$ip:8888/consolevars" || true)
[[ $code == 200 ]] || die "vic-engine :8888/consolevars answered ${code:-nothing}"
ok "vic-engine :8888 answers"

sdk() { curl -s -m 20 "$server/api-sdk/$1?serial=$esn${2:+&$2}" || true; }
state() { # state <label>: one robot_state sample as JSON, or die
  local s; s=$(sdk fork_robot_state)
  jq -e '.pose.x != null and .origin_id != null and .wheels_mm_s.l != null and .wheels_mm_s.r != null' <<<"$s" >/dev/null 2>&1 ||
    die "robot state ($1): ${s:0:200} (wire-pod lacks the current fork_smoke.go? restart it)"
  echo "$s"
}

step "A4 baseline: wheels, lift bit, pose"
# A4 start: wheels at rest before anything moves.
pose_bound_mm=5 # max(5 mm, 3x rest drift); rest drift measured 0 mm over 10 s (2026-11-04)
# Measured wheel speed, not the ARE_WHEELS_MOVING flag: the flag trips on any encoder speed > 1e-5 mm/s (or point-turn mode)
# and did so, with no wheel power evident, while Vector fell asleep after boot (2026-11-05); a point turn shows up as base
# movement in the end pose bound. 5 mm/s is a first guess; read 0, 0 mm/s at rest (2026-11-05).
wheel_bound_mm_s=5
wheels_still() { # wheels_still <label> <state>: |left| and |right| wheel speed <= the bound, else die
  jq -e --argjson b "$wheel_bound_mm_s" '[.wheels_mm_s.l, .wheels_mm_s.r] | map(fabs) | max <= $b' <<<"$2" >/dev/null ||
    die "A4: wheels moving at $1 (bound $wheel_bound_mm_s mm/s): $2"
}
s0=$(state start)
wheels_still start "$s0"
# A4 lift bit: ENCODER_LIFT_INVALID (0x400000) set = the lift-lock recalibration loop is live (0002 must not latch syscon's lift_invalid).
lift_bit() {
  local st; st=$(jq -r .status <<<"$2")
  [[ $st =~ ^[0-9]+$ ]] || die "A4: robot status not numeric at $1: '$st'"
  (((st & 0x400000) == 0)) || die "A4: ENCODER_LIFT_INVALID set at $1 (status $st): recalibration loop"
}
lift_bit start "$s0"
ok "A4 start: wheels at rest ($(jq -r '"\(.wheels_mm_s.l), \(.wheels_mm_s.r)"' <<<"$s0") mm/s), lift encoder bit clear"

step "A1 head to both limits"
# A1 + A2 under behavior control (default priority), released on exit.
sdk assume_behavior_control >/dev/null # answers before the grant; A1's head reads are the check
on_exit+=('sdk move_head speed=0 >/dev/null') # before release: a die inside head_to must not leave the head motor running
on_exit+=('sdk release_behavior_control >/dev/null')
sleep 2
head_to() { # head_to <speed> <label> <jq test on .head_angle_rad>: re-sent each second (the control grant can override the first), 6 s max
  local end=$((SECONDS + 6)) a
  while :; do
    sdk move_head "speed=$1" >/dev/null; sleep 1
    a=$(state "head $2" | jq -r .head_angle_rad)
    jq -en --argjson a "$a" "\$a $3" >/dev/null && break
    ((SECONDS < end)) || { sdk move_head speed=0 >/dev/null; die "A1: head $2 reads $a rad after 6 s, want $3"; }
  done
  sdk move_head speed=0 >/dev/null
  ok "A1 head $2: $a rad"
}
head_to 1 up '>= 0.7767 - 0.1'    # MAX_HEAD_ANGLE 44.5°
head_to -1 down '<= -0.3840 + 0.1' # MIN_HEAD_ANGLE -22°
step "A2 face + speech"
r=$(sdk fork_face); [[ $r == ok ]] || die "A2: fork_face: ${r:0:200}"
ok "A2 face shown"
# The phrase in wire-pod's current language (fork_say speaks that language's T1); English if the voice API is unreadable.
# By ear: English = exclamation, colon/comma pauses, a number list, question rise, several streamed sentences; Estonian = õ/ö, comma pause.
said="Hello! This is Vector's speech test: one, two, three. Can you hear me clearly?"
[[ $(curl -s -m 5 "$server/myvector/api/voice" | jq -r .lang 2>/dev/null) == et-EE ]] && said='Tere, kõik töötab'
r=$(sdk fork_say "text=$(jq -rn --arg s "$said" '$s|@uri')"); [[ $r == FINISHED\ * ]] || die "A2: fork_say ended '${r:0:200}', want FINISHED <voice>"
[[ $r != 'FINISHED fallback'* ]] && ok "A2 said '$said' in ${r#FINISHED }" ||
  echo "WARN: A2 said '$said' in the fallback voice, not the saved language's T1 (TTS service :8091 down? make tts-enable): ${r#FINISHED fallback: }"
sdk release_behavior_control >/dev/null

step "A3 log-level intent"
# A3: patch 0007's app intents turn vic-engine DEBUG on (marker + the DEBUG probe line, channel filtered at rest) and off.
ssh_ "grep -qF my-vector.LogLevel /anki/lib/libcozmo_engine.so" || die "A3: firmware lacks patch 0007; skipped the log-level intent"
intent() { curl -s -m 10 "$server/api-sdk/cloud_intent?serial=$esn&intent=$1" >/dev/null || true; }
seen() { # seen <ERE>...: every pattern appears in /var/log/messages after line $n0 within 10 s; $new = the lines seen
  local end=$((SECONDS + 10)) p miss
  while :; do
    new=$(ssh_ "tail -n +$((n0 + 1)) /var/log/messages | grep -F my-vector.LogLevel" 2>/dev/null || true) miss=
    for p; do grep -qE -- "$p" <<<"$new" || miss=1; done
    [[ -n $miss ]] || return 0
    ((SECONDS < end)) || return 1; sleep 2
  done
}
n0=$(ssh_ 'wc -l </var/log/messages') || die "A3: cannot read /var/log/messages"
on_exit+=('intent my_vector_log_info')
intent my_vector_log_debug
seen 'my-vector\.LogLevel.*debug on' 'vic-engine [0-9]+ \[@BehaviorSystem\] my-vector\.LogLevel\.Probe: (\(tc[0-9]+\) )?gate on' ||
  die "A3: no 'my-vector.LogLevel debug on' + DEBUG probe line within 10 s; saw: ${new:-nothing}"
n0=$(ssh_ 'wc -l </var/log/messages') || die "A3: cannot read /var/log/messages"
intent my_vector_log_info
seen 'my-vector\.LogLevel.*info \(app\)' || die "A3: no 'my-vector.LogLevel info (app)' within 10 s; saw: ${new:-nothing}"
ok "A3 log-level intent: DEBUG on, back to info"

step "end checks: A4 base still, docked, log, units"
# A4 end: the base did not move during the run (head and lift do not change the pose).
s1=$(state end)
wheels_still end "$s1"
lift_bit end "$s1"
ok "A4 lift encoder bit clear at start and end"
[[ $(jq -r .origin_id <<<"$s1") == $(jq -r .origin_id <<<"$s0") ]] || die "A4: pose origin changed (delocalized?): $s0 → $s1"
d=$(jq -rn --argjson a "$s0" --argjson b "$s1" '[($b.pose.x - $a.pose.x), ($b.pose.y - $a.pose.y)] | map(fabs) | max')
jq -en --argjson d "$d" "\$d <= $pose_bound_mm" >/dev/null || die "A4: base moved $d mm (bound $pose_bound_mm mm): $s0 → $s1"
ok "A4 base still: moved $d mm (bound $pose_bound_mm)"

# P2 + P1: still docked, and no unit restarted during the run.
docked after
sleep 8 # a recalibration loop starts ~3 s after control is released and repeats ~1/s
logcheck end
same_pids "during the run"
ok "units unchanged at the end"
echo "PASS"
