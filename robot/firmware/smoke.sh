#!/usr/bin/env bash
# On-robot smoke test (make robot-test; make ship after deploy): units up and stable, clean vic-robot log, wire-pod link, docked.
# Usage: smoke.sh   → PASS (exit 0); FAIL (1) names the first failing check; SETUP (2) = no ESN/key/wire-pod link.  (env: ESN, SERVER)
# Never commands motion: check-static.sh S5 fails on any wheel/lift call in this file.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
src=$main/robot/work/wire-os-victor
server=${SERVER:-http://localhost:8080}
units=(vic-engine vic-anim vic-robot)
fatal='Fatal|SIGSEGV|SIGABRT|terminate called|Backtrace'
die() { echo "FAIL: $*" >&2; exit 1; }
setup() { echo "SETUP: $*" >&2; exit 2; }
ok() { echo "ok: $*"; }

esn=${ESN:-$(sed -n 's/^esn: *//p' "$main"/robot/backup/robot-id-*.txt 2>/dev/null | head -1 || true)}
[[ -n $esn ]] || setup "no ESN; set ESN=<esn>"
[[ -s $src/robot_ip.txt && -f $src/robot_sshkey ]] || setup "need $src/robot_ip.txt and robot_sshkey"
ip=$(<"$src/robot_ip.txt")
ssh_() { ssh -i "$src/robot_sshkey" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa \
  -o HostKeyAlgorithms=+ssh-rsa "root@$ip" "$@"; }
pids() { ssh_ "for u in ${units[*]}; do systemctl is-active -q \$u || exit 1; pidof -s \$u || exit 1; done" 2>/dev/null | tr '\n' ' '; }
docked() {
  local b; b=$(curl -s -m 20 "$server/api-sdk/get_battery?serial=$esn" || true)
  [[ $b == *'"is_on_charger_platform":true'* ]] || die "docked ($1): not on the charger platform: ${b:0:200}"
  [[ $b == *'"is_charging":true'* ]] && ok "docked ($1), charging" || echo "WARN: docked ($1), not charging"
}

# P1: units active within 180 s, PIDs unchanged 15 s later (and at the end).
end=$((SECONDS + 180))
until up=$(pids); do ((SECONDS < end)) || die "units: ${units[*]} not all active within 180 s"; sleep 5; done
sleep 15
[[ $(pids || true) == "$up" ]] || die "units: a PID changed within 15 s (crash loop?): $up"
ok "units active, stable 15 s: $up"

# P1: wire-pod reaches the robot within 20 s (else the setup, not the firmware, is broken).
end=$((SECONDS + 20))
until [[ $(curl -s -m 20 "$server/api-sdk/conn_test?serial=$esn" || true) == success ]]; do
  ((SECONDS < end)) || setup "wire-pod at $server has no link to $esn (conn_test)"; sleep 2
done
ok "wire-pod conn_test"

# P2: docked before the checks.
docked before

# P1: since the running vic-robot started: lift calibrated, no crash lines.
log=$(ssh_ "FATAL='$fatal' sh -s" <<'EOF'
pid=$(pidof -s vic-robot) || { echo "vic-robot not running"; exit 1; }
awk -v tag="vic-robot $pid " '
  !on && index($0, tag) { on = 1 }
  !on { next }
  /LiftController\.Calibrated/ { cal++ }
  $0 ~ ENVIRON["FATAL"] { bad++; print "fatal: " substr($0, 1, 160) }
  END { printf "lift calibrated %d, crash lines %d", cal, bad; exit !(cal && !bad) }' $(ls -tr /var/log/messages.* 2>/dev/null) /var/log/messages
EOF
) || die "log: $log"
ok "log: $log"

# P1: vic-engine's web server answers.
code=$(curl -s -m 5 -o /dev/null -w '%{http_code}' "http://$ip:8888/consolevars" || true)
[[ $code == 200 ]] || die "vic-engine :8888/consolevars answered ${code:-nothing}"
ok "vic-engine :8888 answers"

# P2 + P1: still docked, and no unit restarted during the run.
docked after
[[ $(pids || true) == "$up" ]] || die "units: a PID changed during the run: $up"
ok "units unchanged at the end"
echo "PASS"
