# shellcheck shell=bash
# Shared by the volume-probe scripts: robot ssh, vic-anim console, wire-pod SDK, output capture.
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
src=$main/robot/work/wire-os-victor
[[ -s $src/robot_ip.txt && -f $src/robot_sshkey ]] || { echo "FAIL: need $src/robot_ip.txt and robot_sshkey" >&2; exit 2; }
ip=$(<"$src/robot_ip.txt")
so=(-i "$src/robot_sshkey" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa)
C="http://$ip:8889/consolefunccall?func="; A=http://127.0.0.1:8080/api-sdk
D=/data/data/com.anki.victor/cache/sound
serial=$(curl -s "$A/get_sdk_info" | grep -o '"esn":"[^"]*"' | head -1 | cut -d'"' -f4)
[[ -n $serial ]] || { echo "FAIL: wire-pod has no robot (is chipper running?)" >&2; exit 2; }
rtpc() { curl -s -o /dev/null "${C}SetAudioParameter&args=Robot_Vic_Volume_$1+$2"; }
reset_rtpcs() { for b in ${RESET:-Animation Behavior Procedural}; do rtpc "$b" 1.0; done; }
cap_on() { ssh "${so[@]}" "root@$ip" "rm -f $D/*.wav"; curl -s -o /dev/null "${C}SetWriteAudioOutputCapture&args=true"; }
cap_off() { curl -s -o /dev/null "${C}SetWriteAudioOutputCapture&args=false"; sleep 1.1; scp -q "${so[@]}" "root@$ip:$D/*.wav" "$1"; }
hold() { curl -s "$A/assume_behavior_control?serial=$serial&priority=high" >/dev/null; sleep 3; }
release() { reset_rtpcs; curl -s "$A/release_behavior_control?serial=$serial" >/dev/null; }
