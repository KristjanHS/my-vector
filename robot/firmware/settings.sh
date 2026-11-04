#!/usr/bin/env bash
# Re-apply the robot's runtime settings that a re-onboard or factory reset loses (make ship runs it after deploy).
# Usage: settings.sh   idempotent: reads first, sets only what differs. Exit 0 = all in place; 1 = a WARN line printed; 2 = SETUP (robot/env.sh).  (env: ESN, SERVER)
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
settings=/data/data/com.anki.victor/persistent/jdocs/vic.RobotSettings.json
volume=4 # master_volume: MUTE 0 … HIGH 5; quiet idle comes from the per-bus patches, not from this
flag=/data/data/user-do-not-auto-update
rc=0
ok() { echo "ok: $*"; }
warn() { echo "WARN: $*"; rc=1; }

ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
ssh_ true 2>/dev/null || { warn "settings: SSH to $ip failed; nothing checked"; exit 1; }

# master_volume from the robot's own settings jdoc (wire-pod's chipper/jdocs copy goes stale: it missed a 4→1 set); empty if unreadable.
robot_volume() { ssh_ "sed -n 's/.*\"master_volume\" *: *\([0-9]*\).*/\1/p' $settings" 2>/dev/null; }

was=$(robot_volume)
if [[ $was == "$volume" ]]; then
  ok "volume already $volume"
elif ! curl -sf -m 20 -o /dev/null "$server/api-sdk/volume?serial=$esn&volume=$volume"; then
  warn "volume: set to $volume failed at $server (wire-pod down?); robot has ${was:-unknown}"
else
  now=$was
  for _ in $(seq 15); do now=$(robot_volume); [[ $now == "$volume" ]] && break; sleep 1; done
  if [[ $now == "$volume" ]]; then ok "volume $volume (was ${was:-unknown})"
  else warn "volume: set to $volume sent, robot still has ${now:-unknown} after 15 s"; fi
fi

case $(ssh_ "[ -e $flag ] && echo present || { touch $flag && echo created; }" 2>/dev/null) in
  present) ok "no-auto-update: $flag present" ;;
  created) ok "no-auto-update: $flag created" ;;
  *) warn "no-auto-update: could not check or create $flag over SSH to $ip" ;;
esac
exit $rc
