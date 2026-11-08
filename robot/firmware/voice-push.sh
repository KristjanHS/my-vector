#!/usr/bin/env bash
# Write the robot's T2 voice file (plan R15), read by the vic-anim TTS patch: /data/data/myvector-voice.json {url, voice, slow, lang}.
# Usage: voice-push.sh <voice> <slow> [lang]   lang en-US (default) | et-EE; wire-pod runs it on a voice-settings save, settings.sh at ship. Idempotent; one SSH.
#   Exit 0 = file in place; 1 = bad args or SSH/write failed; 2 = SETUP (robot/env.sh; ROBOT_TTS_URL in robot/robot.env).
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"
file=/data/data/myvector-voice.json
(($# == 2 || $# == 3)) || { echo "usage: voice-push.sh <voice> <slow> [lang]" >&2; exit 1; }
voice=$1 slow=$2 lang=${3:-en-US}
[[ $voice =~ ^[a-z_]+$ ]] || { echo "voice-push: bad voice '$voice'" >&2; exit 1; }
[[ $slow =~ ^[0-9]+(\.[0-9]+)?$ ]] || { echo "voice-push: bad slow '$slow'" >&2; exit 1; }
[[ $lang =~ ^(en-US|et-EE)$ ]] || { echo "voice-push: bad lang '$lang'" >&2; exit 1; }
[[ -n $tts_url ]] || { echo "SETUP: $robot_dir/robot.env needs ROBOT_TTS_URL (e.g. http://<pc-lan-ip>:8091)" >&2; exit 2; }
# Only what patch 0010's MyVectorLoadVoice takes (else the robot silently speaks Acapela); tests/tts-urls.txt holds both to it.
oct='(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])'
port=0
[[ ! $tts_url =~ ^http://($oct\.){3}$oct(:([0-9]{1,5}))?(/[A-Za-z0-9._/-]*)?$ ]] || port=$((10#${BASH_REMATCH[5]:-80}))
((port >= 1 && port <= 65535)) ||
  { echo "SETUP: ROBOT_TTS_URL '$tts_url' must be http://<IPv4>[:port][/path] (the robot takes no https or hostnames)" >&2; exit 2; }
# Root (a foreground `sudo chipper/start.sh` wire-pod) checks the robot's host key against the repo owner's known_hosts.
if ((EUID == 0)) && owner_home=$(getent passwd "$(stat -c %U "$robot_dir")" | cut -d: -f6) && [[ -n $owner_home ]]; then
  ssh_opts+=(-o "UserKnownHostsFile=$owner_home/.ssh/known_hosts")
fi
json=$(printf '{"url":"%s","voice":"%s","slow":%s,"lang":"%s"}' "$tts_url" "$voice" "$slow" "$lang")
# Remote: compare, then temp + mv so vic-anim never reads a half-written file.
remote="new=\$(cat); [ \"\$(cat $file 2>/dev/null)\" = \"\$new\" ] && echo unchanged ||
  { printf '%s\n' \"\$new\" >$file.tmp && mv $file.tmp $file && echo written; }"
out=$(ssh "${ssh_opts[@]}" "root@$ip" "$remote" <<<"$json" 2>&1)
case ${out##*$'\n'} in
  unchanged) echo "ok: T2 voice file already $json" ;;
  written) echo "ok: T2 voice file written: $json" ;;
  *) echo "voice-push: SSH to $ip / write of $file failed: ${out##*$'\n'}" >&2; exit 1 ;;
esac
