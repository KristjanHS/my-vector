#!/usr/bin/env bash
# Mic-stream Stage 1.2 CPU gate (docs/plans/2026-11-08-mic-stream.md): vic-anim CPU with the patch-0016 stream off vs on.
# "on" points the robot at a PC byte sink on PORT (default 8094; not wire-pod, so no Hearer and no robot sound); each
# window prints vic-anim % of one core (/proc utime+stime) and the bytes the sink got (0 in silence: G1 gate).
# Usage: [SECS=300] [PORT=8094] gate.sh [window …]   windows: off | on (default: off on off — the offs bracket the drift).
# Talk during an "on" for the speech row, e.g. gate.sh off on on off. Exit 0 PASS (worst on − mean off ≤ LIMIT=2 % and
# 0 ring drops), 1 FAIL, 2 SETUP (no robot, or the robot never reached the sink), 3 INCOMPLETE (no off or no on window).
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (stream port console var + /proc reads only)
. "$here/../env.sh"
SECS=${SECS:-300} PORT=${PORT:-8094} LIMIT=${LIMIT:-2.0}
wins=("$@"); ((${#wins[@]})) || wins=(off on off)
offs=()
out=$robot_dir/work/mic-stream; mkdir -p "$out"; log=$out/sink-$(date +%H%M%S).log
setport() { curl -fsS -m 5 -o /dev/null "http://$ip:8889/consolevarset?key=MyVector_MicStreamPort&value=$1"; }
python3 -I - "$PORT" >"$log" 2>&1 <<'EOF' &
import signal, socket, sys, threading
total = 0
def rd(c):
    global total
    print("conn", flush=True)
    while (b := c.recv(65536)):
        total += len(b)
    c.close()
signal.signal(signal.SIGUSR1, lambda *_: print("bytes", total, flush=True))
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", int(sys.argv[1]))); s.listen(4)
while True:
    c, _ = s.accept(); threading.Thread(target=rd, args=(c,), daemon=True).start()
EOF
sink=$!
trap 'setport 0; kill "$sink" 2>/dev/null' EXIT
sleep 1; kill -0 "$sink" 2>/dev/null || { echo "SETUP: sink on :$PORT did not start: $(cat "$log")"; exit 2; }
setport 0 || { echo "SETUP: robot console :8889 unreachable"; exit 2; }
sample() { # one ssh: vic-anim utime+stime ticks over SECS → % of one core (USER_HZ 100)
  ssh "${ssh_opts[@]}" "root@$ip" "p=\$(pidof -s vic-anim) || exit 2
    a=\$(cut -d' ' -f14,15 /proc/\$p/stat); u=\$(cut -d' ' -f1 /proc/uptime); sleep $SECS
    b=\$(cut -d' ' -f14,15 /proc/\$p/stat); v=\$(cut -d' ' -f1 /proc/uptime)
    echo \$a \$b \$u \$v" | awk 'NF == 6 { printf "%.2f", (($3 + $4) - ($1 + $2)) / 100 / ($6 - $5) * 100; ok = 1 } END { exit !ok }'
}
bytes() { kill -USR1 "$sink"; sleep 0.2; grep '^bytes' "$log" | tail -1 | cut -d' ' -f2; }
start=$(date '+%m-%d %H:%M:%S') off="" worst="" rc=0
for w in "${wins[@]}"; do
  case $w in
    off) setport 0 ;;
    on) setport "$PORT"; for _ in $(seq 30); do grep -q '^conn' "$log" && break; sleep 1; done
        grep -q '^conn' "$log" || { echo "SETUP: robot never reached :$PORT (Windows firewall? plan §1.2)"; exit 2; } ;;
    *) echo "usage: gate.sh [off|on …]"; exit 2 ;;
  esac
  sleep 5; b0=$(bytes); pct=$(sample) || { echo "SETUP: no vic-anim sample"; exit 2; }; b1=$(bytes)
  echo "$w  vic-anim $pct % of one core  sink +$((b1 - b0)) bytes in ${SECS}s"
  if [[ $w == off ]]; then offs+=("$pct")
  elif [[ -z $worst ]] || awk "BEGIN { exit !($pct > $worst) }"; then worst=$pct; fi
done
((${#offs[@]})) && off=$(printf '%s\n' "${offs[@]}" | awk '{ s += $1 } END { printf "%.2f", s / NR }')
drops=$(ssh "${ssh_opts[@]}" "root@$ip" "awk -v s='$start' '\$1 \" \" \$2 >= s && /my-vector.MicStream: .*ring dropped/' /var/log/messages" | tail -1)
echo "ring drops: ${drops:-none}"; [[ -z $drops ]] || rc=1
if [[ -n $off && -n $worst ]]; then
  d=$(awk "BEGIN { printf \"%.2f\", $worst - $off }"); echo "delta (worst on − mean off): $d % (limit $LIMIT)"
  awk "BEGIN { exit !($d > $LIMIT) }" && rc=1
elif ((rc == 0)); then
  echo "INCOMPLETE: need an off and an on window"; exit 3
fi
((rc == 0)) && echo PASS || echo FAIL
exit "$rc"
