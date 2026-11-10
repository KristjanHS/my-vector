# shellcheck shell=bash
# Shared by the volume-probe scripts: robot ssh, vic-anim console, wire-pod SDK, output capture.
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck disable=SC2034 # read by the sourcing scripts
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
# shellcheck source=robot/env.sh
. "$here/../env.sh"
so=("${ssh_opts[@]}")
C="http://$ip:8889/consolefunccall?func="; A=$server/api-sdk
D=/data/data/com.anki.victor/cache/sound
serial=$(curl -s "$A/get_sdk_info" | grep -o '"esn":"[^"]*"' | head -1 | cut -d'"' -f4)
[[ -n $serial ]] || { echo "FAIL: wire-pod has no robot (is chipper running?)" >&2; exit 2; }
rtpc() { curl -s -o /dev/null "${C}SetAudioParameter&args=Robot_Vic_Volume_$1+$2"; }
# Shipped levels: firmware 0008-speech-volume.patch sets Animation 0.1 at audio init; the rest stay 1.0.
reset_rtpcs() { local b l; for b in ${RESET:-Animation Behavior Procedural}; do l=1.0; [[ $b == Animation ]] && l=0.1; rtpc "$b" "$l"; done; }
# Capture left on fills /data (2026-11-05: 822 MB, /data 100 %): the EXIT trap stops it. Scripts add cleanup with on_exit, never trap.
capturing=0 exit_hooks=()
on_exit() { exit_hooks+=("$1"); }
cap_stop() { ((capturing)) || return 0; curl -s -m 5 -o /dev/null "${C}SetWriteAudioOutputCapture&args=false"; capturing=0; }
# KG searching's loop sfx outlives the clip; only its getout stops it (W10). play_anim flags it, the trap plays the getout once.
kg=anim_knowledgegraph_searching_01 kg_played=0
play_anim() { [[ $1 == "$kg" ]] && kg_played=1; curl -s -o /dev/null "${C}PlayAnimation&args=$1+${2:-1}"; }
kg_getout() { ((kg_played)) || return 0; kg_played=0
  if quiet_locked; then echo "WARN sound lock: KG searching loop sfx left running; after make unquiet play anim_knowledgegraph_searching_getout_01" >&2; return 0; fi
  play_anim anim_knowledgegraph_searching_getout_01; sleep 2; }
# Exit order: capture off, KG getout (behavior control still held), then on_exit hooks in order (release last).
# shellcheck disable=SC2154 # h: the trap's own loop variable
trap 'cap_stop; kg_getout; for h in "${exit_hooks[@]}"; do eval "$h" || true; done' EXIT
cap_on() { ssh "${so[@]}" "root@$ip" "rm -f $D/*.wav"; capturing=1; curl -s -o /dev/null "${C}SetWriteAudioOutputCapture&args=true"; }
# A copy failure (e.g. diskguard removed a capture over 50 MB, ~13 min) is loud: a stale $1 from an earlier run would read as new.
cap_off() { cap_stop; sleep 1.1; rm -f "$1"; scp -q "${so[@]}" "root@$ip:$D/*.wav" "$1" || echo "WARN: no capture copied to $1" >&2; }
hold() { curl -s "$A/assume_behavior_control?serial=$serial&priority=high" >/dev/null; sleep 3; }
release() { reset_rtpcs; curl -s "$A/release_behavior_control?serial=$serial" >/dev/null; }
