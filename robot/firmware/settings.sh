#!/usr/bin/env bash
# Re-apply the robot's runtime settings that a re-onboard or factory reset loses (make ship runs it after deploy): sshd host keys, root SSH key, 24h clock + Celsius + Tallinn time zone, no-auto-update, T2 voice file, /data fill guard, server CA trust, escapepod.local pin + firewall.
# Usage: settings.sh   idempotent: reads first, sets only what differs. Exit 0 = all in place; 1 = a WARN line printed; 2 = SETUP (robot/env.sh).  (env: ESN, SERVER)
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"
settings=/data/data/com.anki.victor/persistent/jdocs/vic.RobotSettings.json
tz=Europe/Tallinn # robot clock + time_zone jdoc (user ruling 2026-11-06; onboarding left America/Los_Angeles)
flag=/data/data/user-do-not-auto-update
conf=$robot_dir/../secrets/server/myvectorConfig.json # voice.SettingsPath: beside apiConfig.json (vars/fork_paths.go)
jdocs=$robot_dir/../secrets/server/jdocs/jdocs.json # vars.JdocsPath (vars/fork_paths.go)
rc=0
ok() { echo "ok: $*"; }
warn() { echo "WARN: $*"; rc=1; }

# sshd host keys first (robot/hostkey): a reflash restores the image ones, which only hostkey's own login still trusts.
hk=$here/../hostkey/apply.sh
if [[ ! -f $hk ]]; then warn "hostkey: $hk missing (older sha?); not checked"
elif out=$(bash "$hk" 2>&1); then ok "${out//$'\n'/; }"
else warn "hostkey: ${out//$'\n'/; }"; fi
# Root SSH key next (robot/sshkey): a reflash restores the image key, and every check below logs in with ROBOT_KEY.
sk=$here/../sshkey/apply.sh
if [[ ! -f $sk ]]; then warn "sshkey: $sk missing (older sha?); not checked"
elif out=$(bash "$sk" 2>&1); then if [[ $out == WARN:* ]]; then warn "${out#WARN: }"; else ok "${out//$'\n'/; }"; fi
else warn "sshkey: ${out//$'\n'/; }"; fi

ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
ssh_ true 2>/dev/null || { warn "settings: SSH to $ip failed; nothing checked"; exit 1; }

# A field of the robot's own settings jdoc (wire-pod's secrets/server/jdocs copy goes stale: it missed a 4→1 set); empty if unreadable.
robot_setting() { ssh_ "sed -n 's/.*\"$1\" *: *\"\{0,1\}\([A-Za-z0-9_/+-]*\).*/\1/p' $settings" 2>/dev/null; }
# The same field in wire-pod's copy (vic.RobotSettings in $jdocs; intentparam's weather units + clock read it); empty if unreadable.
wirepod_setting() {
  jq -r --arg t "vic:$esn" --arg f "$1" '.[] | select(.thing == $t and .name == "vic.RobotSettings")
    | .jdoc.json_doc | fromjson | if has($f) then .[$f] | tostring else empty end' "$jdocs" 2>/dev/null
}

# sdk_ready: wire-pod's SDK link to the robot works; checked once, and only when a set is due. make ship runs this right after
# the deploy restarted the robot's :443 gateway: a set sent before it is back gets connection refused, upstream
# setSettingSDKstring panics and curl sees an empty reply (ship 2026-11-09 11:03). Same wait as smoke.sh P1: get_battery
# tries 2 s apart for 60 s (wall clock: a hanging gateway makes each try up to 20 s; ship's silent-start flag lasts 600 s),
# at most 30 tries. Else sdk_why says whether wire-pod or its robot link is down.
sdk_why=unchecked
sdk_ready() {
  [[ $sdk_why == unchecked ]] || { [[ -z $sdk_why ]]; return; }
  local b= c=0 t0=$SECONDS
  for _ in $(seq 30); do
    c=0; b=$(curl -s -m 20 "$server/api-sdk/get_battery?serial=$esn") || c=$?
    [[ $b == *'"battery_volts"'* ]] && { sdk_why=; return 0; }
    ((SECONDS - t0 < 60)) || break
    sleep 2
  done
  if ((c == 7)); then sdk_why="wire-pod not answering at $server (down?)"
  else sdk_why="wire-pod at $server has no SDK link to $esn after $((SECONDS - t0)) s (robot gateway down?)${b:+: ${b:0:120}}"; fi
  return 1
}

# ensure <label> <jdoc field> <want> <api-sdk call> [<what follows wire-pod's copy>]: set via wire-pod only if the robot differs
# (with the 5th arg: or wire-pod's copy does), after sdk_ready; ≤15 s for the robot jdoc to show it, then get_sdk_settings
# re-pulls wire-pod's copy.
ensure() {
  local label=$1 field=$2 want=$3 call=$4 follows=${5:-} was now wp=
  was=$(robot_setting "$field")
  [[ -z $follows ]] || wp=$(wirepod_setting "$field")
  if [[ $was == "$want" && (-z $follows || $wp == "$want") ]]; then ok "$label already $want"; return; fi
  if ! sdk_ready; then warn "$label: not set to $want: $sdk_why; robot has ${was:-unknown}"; return; fi
  if ! curl -sf -m 20 -o /dev/null "$server/api-sdk/$call"; then
    warn "$label: set to $want failed at $server (wire-pod or its robot link dropped); robot has ${was:-unknown}"; return
  fi
  now=$was
  for _ in $(seq 15); do now=$(robot_setting "$field"); [[ $now == "$want" ]] && break; sleep 1; done
  if [[ $now == "$want" ]]; then ok "$label $want (was ${was:-unknown}${follows:+, wire-pod ${wp:-unknown}})"
  else warn "$label: set to $want sent, robot still has ${now:-unknown} after 15 s"; fi
  [[ -n $follows ]] || return
  curl -sf -m 20 -o /dev/null "$server/api-sdk/get_sdk_settings?serial=$esn" || true
  wp=$(wirepod_setting "$field")
  [[ $wp == "$want" ]] || warn "$label: wire-pod's copy ($jdocs) still has ${wp:-unknown}, want $want; $follows follow it"
}
# E32: 24h clock + Celsius in every language (the ET clock/weather lines assume them).
ensure clock_24_hour clock_24_hour true "time_format_24?serial=$esn" "the clock"
ensure temp_is_fahrenheit temp_is_fahrenheit false "temp_c?serial=$esn" "weather units"
ensure time_zone time_zone "$tz" "timezone?serial=$esn&timezone=$tz"

case $(ssh_ "[ -e $flag ] && echo present || { touch $flag && echo created; }" 2>/dev/null) in
  present) ok "no-auto-update: $flag present" ;;
  created) ok "no-auto-update: $flag created" ;;
  *) warn "no-auto-update: could not check or create $flag over SSH to $ip" ;;
esac

# T2 voice (plan R15, E30): the saved language's T2 (t2, or t2_et for et-EE) + slow, as voice.Settings.For;
# voice.Defaults() (af_bella / mari, 1.2) for a missing file or field.
conf_field() { sed -n "s/.*\"$1\": *\"\{0,1\}\([A-Za-z_0-9.-]*\).*/\1/p" "$conf" 2>/dev/null | head -1; }
t2_lang=$(conf_field lang)
if [[ $t2_lang == et-EE ]]; then t2=$(conf_field t2_et) t2=${t2:-mari}
else t2_lang=en-US t2=$(conf_field t2) t2=${t2:-af_bella}; fi
slow=$(conf_field slow)
if out=$("$here/voice-push.sh" "$t2" "${slow:-1.2}" "$t2_lang" 2>&1); then ok "${out#ok: }"
else warn "T2 voice: not pushed: ${out##*$'\n'}"; fi

# /data fill guard (robot/diskguard): it lives on the rootfs, so a reflash drops it; this re-installs it.
dg=$here/../diskguard/apply.sh
if [[ ! -f $dg ]]; then warn "diskguard: $dg missing (older sha?); not installed"
elif out=$(bash "$dg" 2>&1); then ok "${out//$'\n'/; }"
else warn "diskguard: ${out//$'\n'/; }"; fi
# RAM log caps + /data/log/keep (robot/logkeep): /etc syslog-ng + journald conf, dropped by a reflash.
lk=$here/../logkeep/apply.sh
if [[ ! -f $lk ]]; then warn "logkeep: $lk missing (older sha?); not applied"
elif out=$(bash "$lk" 2>&1); then ok "${out//$'\n'/; }"
else warn "logkeep: ${out//$'\n'/; }"; fi
# Server CA trust (robot/epod): ROBOT_TRUST lives on /data; re-applied so the 0015 marker switches it to ours only.
ep=$here/../epod/apply.sh
if [[ ! -f $ep ]]; then warn "epod: $ep missing (older sha?); not applied"
elif out=$(bash "$ep" 2>&1); then ok "${out//$'\n'/; }"
else warn "epod: ${out//$'\n'/; }"; fi
# escapepod.local hosts pin + wlan0 firewall (robot/netguard; the firewall only after its --selftest, plan N5).
ng=$here/../netguard/apply.sh
if [[ ! -f $ng ]]; then warn "netguard: $ng missing (older sha?); not applied"
elif out=$(bash "$ng" 2>&1); then if [[ $out == *WARN:* ]]; then warn "${out//$'\n'/; }"; else ok "${out//$'\n'/; }"; fi
else warn "netguard: ${out//$'\n'/; }"; fi
# Shut-up nap (shut-up-sleep N3), last: the deploy and epod's trust rewrite above restart vic-engine, which zeroes its console
# vars; re-arm the nap while the lock holds. Its console needs a while after a restart: up to 18 tries 5 s apart (~90 s).
# vic-engine answers 200 even for an unknown var or a bad value; only a body echoing the value is a set.
# Nap cap = Go quiet.NapMax - quiet.NapMargin (4 h - 5 min): patch 0011 refuses past 4 h on the robot clock.
if quiet_locked; then
  nap_until=$(($(date +%s) + 4 * 60 * 60 - 5 * 60)) body='' up=''
  ((quiet_until > nap_until)) || nap_until=$quiet_until
  for _ in $(seq 18); do
    if body=$(curl -sf -m 5 "http://$ip:8888/consolevarset?key=SleepCycle_NapUntil_s&value=$nap_until" 2>/dev/null); then up=1; break; fi
    sleep 5
  done
  if [[ $body == "$nap_until<br>"* ]]; then ok "nap re-armed until $(date -d "@$nap_until" +%H:%M) (sound lock)"
  elif [[ -n $up ]]; then warn "nap: vic-engine did not set SleepCycle_NapUntil_s=$nap_until: ${body//<br>/ } (firmware without the nap?)"
  else warn "nap: re-arm to $nap_until failed at $ip:8888 for ~90 s (vic-engine down?)"; fi
fi
exit $rc
