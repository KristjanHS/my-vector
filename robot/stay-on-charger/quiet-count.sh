#!/usr/bin/env bash
# Quiet-idle check: idle-sound hits on the robot since the running vic-engine started — 2026-11-04-vector-quiet-idle.md (docs/plans/).
# Usage: quiet-count.sh   → PASS (exit 0) = 0 hits and a Sleeping start seen; FAIL (1) = hits; INCOMPLETE (3) = no sleep yet, or the log no longer reaches the engine start.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
src=$main/robot/work/wire-os-victor
[[ -s $src/robot_ip.txt && -f $src/robot_sshkey ]] || { echo "FAIL: need $src/robot_ip.txt and robot_sshkey" >&2; exit 2; }

ssh -i "$src/robot_sshkey" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa \
  -o HostKeyAlgorithms=+ssh-rsa "root@$(<"$src/robot_ip.txt")" sh -s <<'EOF'
pid=$(pidof -s vic-engine) || { echo "FAIL: vic-engine not running"; exit 2; }
awk -v tag="vic-engine $pid " '
  index($0, tag) == 0 { next }
  !since { since = $1 " " $2; whole = index($0, "cozmo_start") > 0 }
  /@behavior\.feature\.start\037Sleeping\037/ { sleep++ }
  /@behavior\.feature\.start|@action\.play_animation/ && /RTS_OnCharger|ListeningForBeats/ { hits++; l = $0; gsub("\037", "|", l); print "  hit: " substr(l, 1, 160) }
  END {
    printf "since %s: idle-sound hits %d, Sleeping starts %d\n", since, hits, sleep
    v = hits ? "FAIL" : sleep && whole ? "PASS" : "INCOMPLETE"; print v
    exit v == "PASS" ? 0 : v == "FAIL" ? 1 : 3
  }' $(ls -tr /var/log/messages.* 2>/dev/null) /var/log/messages
EOF
