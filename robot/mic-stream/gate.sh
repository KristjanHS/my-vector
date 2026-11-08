#!/usr/bin/env bash
# Mic-stream Stage 1.2 CPU gate (docs/plans/2026-11-08-mic-stream.md): vic-anim CPU with the patch-0016 stream off vs on.
# "on" points the robot at a PC byte sink on PORT (default 8094; not wire-pod, so no Hearer and no robot sound); each
# window prints vic-anim % of one core (/proc utime+stime) and the bytes the sink got (0 in silence: G1 gate).
# Usage: [SECS=300] [PORT=8094] gate.sh [window …]   windows: off | on (default: off on off — the offs bracket the drift).
# Talk during an "on" for the speech row, e.g. gate.sh off on on off. Exit 0 PASS (worst on − lowest off ≤ LIMIT=2 % and
# 0 ring drops), 1 FAIL, 2 SETUP (robot unreachable, port changed under the run, stream dropped), 3 INCOMPLETE (no off or no on).
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
silent=1 # robot sound lock: makes no robot sound (stream port console var + /proc reads only)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
SECS=${SECS:-300} PORT=${PORT:-8094} LIMIT=${LIMIT:-2.0}
[[ $LIMIT =~ ^[0-9]+(\.[0-9]+)?$ && $SECS =~ ^[0-9]+$ ]] || { echo "SETUP: LIMIT and SECS must be numbers"; exit 2; }
wins=("$@"); ((${#wins[@]})) || wins=(off on off)
for w in "${wins[@]}"; do [[ $w == off || $w == on ]] || { echo "usage: gate.sh [off|on …]"; exit 2; }; done
out=$robot_dir/work/mic-stream; mkdir -p "$out"; log=$out/sink-$(date +%H%M%S).log
console=http://$ip:8889
setport() { curl -fsS -m 5 -o /dev/null "$console/consolevarset?key=MyVector_MicStreamPort&value=$1"; }
getport() { curl -fsS -m 5 "$console/consolevarget?key=MyVector_MicStreamPort" | tr -dc 0-9; }
robot() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
python3 -I - "$PORT" >"$log" 2>&1 <<'EOF' &
import signal, socket, sys, threading
total = 0
def rd(c):
    global total
    print("conn", flush=True)
    while (b := c.recv(65536)):
        total += len(b)
    print("eof", flush=True)
    c.close()
signal.signal(signal.SIGUSR1, lambda *_: print("bytes", total, flush=True))
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", int(sys.argv[1]))); s.listen(4)
while True:
    c, _ = s.accept(); threading.Thread(target=rd, args=(c,), daemon=True).start()
EOF
sink=$!
trap 'setport 0 || echo "WARN: robot left on :$PORT — set MyVector_MicStreamPort 0 by hand" >&2; kill "$sink" 2>/dev/null' EXIT
sleep 1; kill -0 "$sink" 2>/dev/null || { echo "SETUP: sink on :$PORT did not start: $(cat "$log")"; exit 2; }
setport 0 || { echo "SETUP: robot console :8889 unreachable"; exit 2; }
n0=$(robot 'wc -l </var/log/messages') || { echo "SETUP: no ssh to the robot"; exit 2; }
sample() { # one ssh: vic-anim utime+stime ticks over SECS → % of one core (USER_HZ 100)
  # shellcheck disable=SC2029 # SECS expands here on purpose
  robot "p=\$(pidof -s vic-anim) || exit 2
    a=\$(cut -d' ' -f14,15 /proc/\$p/stat); u=\$(cut -d' ' -f1 /proc/uptime); sleep $SECS
    b=\$(cut -d' ' -f14,15 /proc/\$p/stat); v=\$(cut -d' ' -f1 /proc/uptime)
    echo \$a \$b \$u \$v" | awk 'NF == 6 { printf "%.2f", (($3 + $4) - ($1 + $2)) / 100 / ($6 - $5) * 100; ok = 1 } END { exit !ok }'
}
count() { grep -c "^$1\$" "$log"; }
bytes() { kill -USR1 "$sink"; sleep 0.5; grep '^bytes' "$log" | tail -1 | cut -d' ' -f2; }
offs=() worst="" rc=0
for w in "${wins[@]}"; do
  want=0; [[ $w == on ]] && want=$PORT
  c0=$(count conn)
  setport 0 && { [[ $w == off ]] || setport "$PORT"; } || { echo "SETUP: :8889 refused the port set"; exit 2; }
  if [[ $w == on ]]; then # the 0 → PORT flip makes the robot redial: wait for a NEW connection
    for _ in $(seq 30); do (($(count conn) > c0)) && break; sleep 1; done
    (($(count conn) > c0)) || { echo "SETUP: robot never reached :$PORT (Windows firewall? plan §1.2)"; exit 2; }
  fi
  sleep 5; e0=$(count eof) b0=$(bytes)
  pct=$(sample) || { echo "SETUP: no vic-anim sample"; exit 2; }
  b1=$(bytes) e1=$(count eof) now=$(getport) || { echo "SETUP: :8889 unreachable after the $w window"; exit 2; }
  [[ $now == "$want" ]] || { echo "SETUP: port is $now after the $w window, want $want (wire-pod listening on?)"; exit 2; }
  [[ $w == off ]] || ((e1 == e0)) || { echo "SETUP: stream dropped during the on window"; exit 2; }
  echo "$w  vic-anim $pct % of one core  sink +$((b1 - b0)) bytes in ${SECS}s"
  if [[ $w == off ]]; then offs+=("$pct")
  elif [[ -z $worst ]] || awk "BEGIN { exit !($pct > $worst) }"; then worst=$pct; fi
done
((${#offs[@]})) && [[ -n $worst ]] || { echo "INCOMPLETE: need an off and an on window"; exit 3; }
off=$(printf '%s\n' "${offs[@]}" | awk 'NR == 1 || $1 < m { m = $1 } END { printf "%.2f", m }') # lowest off: a hot off window must not hide an on cost
# shellcheck disable=SC2029 # n0 expands here on purpose
drops=$(robot "tail -n +$((n0 + 1)) /var/log/messages") || { echo "SETUP: robot log unreadable"; exit 2; }
drops=$(grep 'my-vector.MicStream: .*ring dropped' <<<"$drops" | tail -1)
echo "ring drops: ${drops:-none}"; [[ -z $drops ]] || rc=1
d=$(awk "BEGIN { printf \"%.2f\", $worst - $off }"); echo "delta (worst on − lowest off): $d % (limit $LIMIT)"
awk "BEGIN { exit !($d > $LIMIT) }" && rc=1
((rc == 0)) && echo PASS || echo FAIL
exit "$rc"
