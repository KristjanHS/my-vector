#!/usr/bin/env bash
# Crash rate per robot start, no gdb: N plain restarts of anki-robot.target (stop, 5 s, start), or with -d N ship-style
# deploys of the clone's current _build (deploy-v.sh, then settings.sh — build first); 60 s watch each.
# Per round: S1 (vic-anim SeAssert), S2 (vic-engine SIGSEGV), S3 (vic-anim start timeout), other crashes, new dumps.
# Every round plays the power-on sound. Usage: restarts.sh [-d] [rounds=5]   exit 0 = no crash, 1 = crash seen, 2 = setup.
# Output: robot/work/crash-review/restarts-<timestamp>/rounds.log (+ round-N.journal, deploy-N.log).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"
# shellcheck source=robot/crash-review/lib.sh
. "$here/lib.sh"
deploy=0
[[ ${1:-} == -d ]] && { deploy=1; shift; }
max=${1:-5}
src=$here/../work/wire-os-victor
[[ $max =~ ^[1-9][0-9]*$ ]] || { echo "SETUP: rounds must be a positive integer" >&2; exit 2; }
watch=60
dumps=/data/data/com.anki.victor/cache/crashDumps
mark=/tmp/myvector-restarts.mark

# shellcheck disable=SC2029 # the remote commands are built here on purpose
ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
ssh_ true || { echo "SETUP: robot unreachable over ssh" >&2; exit 2; }
((deploy)) && clone_ready "$src" "$here/../firmware"

out=$here/../work/crash-review/restarts-$(date +%Y%m%d-%H%M%S)
mkdir -p "$out"
log() { printf '%s %s\n' "$(date +%T)" "$*" | tee -a "$out/rounds.log"; }
trap 'ssh_ "systemctl start --no-block anki-robot.target" >/dev/null 2>&1 || true' EXIT

declare -A total=([S1]=0 [S2]=0 [S3]=0 [other]=0)
for ((i = 1; i <= max; i++)); do
  # A fault-code restart in flight (fault 800 = a unit failed) would race our restart.
  for _ in $(seq 30); do ssh_ 'systemctl is-active -q fault-code.service' 2>/dev/null || break; sleep 2; done
  t0=$(ssh_ "touch $mark; date +%s") || { log "round $i: ssh failed"; sleep 10; continue; }
  if ((deploy)); then
    # Same order as robot/firmware/ship.sh: deploy-v.sh (stop, rsync, start), then settings.sh (warn-only here).
    (cd "$src" && script -qec ./build/deploy-v.sh /dev/null) >"$out/deploy-$i.log" 2>&1 </dev/null ||
      { log "round $i: deploy failed, see $out/deploy-$i.log"; exit 2; }
    "$here/../firmware/settings.sh" >>"$out/deploy-$i.log" 2>&1 </dev/null || log "round $i: settings.sh exit $? (warn only)"
  else
    ssh_ 'systemctl stop anki-robot.target; sleep 5; systemctl start --no-block anki-robot.target' ||
      { log "round $i: restart ssh failed"; continue; }
  fi
  sleep "$watch"
  j=$out/round-$i.journal
  ssh_ "journalctl -o short-iso --no-pager --since @$t0" >"$j" 2>/dev/null || true
  hits=()
  grep -q 'SeAssert' "$j" && hits+=(S1)
  grep -qE 'vic-engine terminated by signal 11|fault code 914' "$j" && hits+=(S2)
  grep -qE "vic-anim.service: (Start operation timed out|Failed with result 'timeout')" "$j" && hits+=(S3)
  # Fault-handler stop cascade (switchboard SIGQUIT, cloud SIGTERM) is not a crash.
  # shellcheck disable=SC2143 # grep -q would SIGPIPE the first grep under pipefail
  [[ -n $(grep 'terminated by signal' "$j" | grep -vE 'vic-engine terminated by signal 11|vic-anim terminated by signal 4|signal (3|15)$') ]] &&
    hits+=(other)
  for h in "${hits[@]}"; do total[$h]=$((total[$h] + 1)); done
  new=$(ssh_ "find $dumps -type f -name '*.dmp' -newer $mark 2>/dev/null | tr '\n' ' '" || true)
  faults=$(grep -oE 'fault code [0-9]+' "$j" | sort -u | tr '\n' ' ' || true)
  log "round $i: ${hits[*]:-ok}${faults:+ ($faults)}${new:+ dumps: $new}"
  # Two fault restarts inside 97 s escalate the robot's fault handler; let its counter clear.
  [[ -n $faults ]] && { log "fault-code restart seen, waiting 100 s"; sleep 100; }
done
log "summary: S1 ${total[S1]}/$max S2 ${total[S2]}/$max S3 ${total[S3]}/$max other ${total[other]}/$max"
((total[S1] + total[S2] + total[S3] + total[other] == 0))
