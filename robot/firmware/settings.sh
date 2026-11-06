#!/usr/bin/env bash
# Re-apply the robot's runtime settings that a re-onboard or factory reset loses (make ship runs it after deploy): volume, 24h clock + Celsius, no-auto-update, T2 voice file, /data fill guard.
# Usage: settings.sh   idempotent: reads first, sets only what differs. Exit 0 = all in place; 1 = a WARN line printed; 2 = SETUP (robot/env.sh).  (env: ESN, SERVER)
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
settings=/data/data/com.anki.victor/persistent/jdocs/vic.RobotSettings.json
volume=4 # master_volume: MUTE 0 … HIGH 5; quiet idle comes from the per-bus patches, not from this
flag=/data/data/user-do-not-auto-update
conf=$robot_dir/../chipper/myvectorConfig.json # voice.SettingsPath: wire-pod (cwd chipper/) saves it beside apiConfig.json
rc=0
ok() { echo "ok: $*"; }
warn() { echo "WARN: $*"; rc=1; }

ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
ssh_ true 2>/dev/null || { warn "settings: SSH to $ip failed; nothing checked"; exit 1; }

# A field of the robot's own settings jdoc (wire-pod's chipper/jdocs copy goes stale: it missed a 4→1 set); empty if unreadable.
robot_setting() { ssh_ "sed -n 's/.*\"$1\" *: *\([a-z0-9]*\).*/\1/p' $settings" 2>/dev/null; }

# ensure <label> <jdoc field> <want> <api-sdk call>: wire-pod sets it only if the robot differs; then ≤15 s for the jdoc to show it.
ensure() {
  local label=$1 field=$2 want=$3 call=$4 was now
  was=$(robot_setting "$field")
  if [[ $was == "$want" ]]; then ok "$label already $want"; return; fi
  if ! curl -sf -m 20 -o /dev/null "$server/api-sdk/$call"; then
    warn "$label: set to $want failed at $server (wire-pod down?); robot has ${was:-unknown}"; return
  fi
  now=$was
  for _ in $(seq 15); do now=$(robot_setting "$field"); [[ $now == "$want" ]] && break; sleep 1; done
  if [[ $now == "$want" ]]; then ok "$label $want (was ${was:-unknown})"
  else warn "$label: set to $want sent, robot still has ${now:-unknown} after 15 s"; fi
}
ensure volume master_volume "$volume" "volume?serial=$esn&volume=$volume"
# E32: 24h clock + Celsius in every language (the ET clock/weather lines assume them).
ensure clock_24_hour clock_24_hour true "time_format_24?serial=$esn"
ensure temp_is_fahrenheit temp_is_fahrenheit false "temp_c?serial=$esn"

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
exit $rc
