#!/usr/bin/env bash
# Catch vic-anim's "SeAssert: se_lib/nrgainv.c:191" in the act. Each round restarts anki-robot.target (like a ship
# deploy: stop, 5 s, start), attaches gdbserver to the new vic-anim and breaks on NrGainLookupCore's line-191 failure
# branch, i.e. before the assert's fprintf clobbers the registers. A hit dumps the operands (result s16, ratio s18,
# gain s4, 1-floor s2), the gain table the code read, the caller's inputs and the backtrace.
# Every restart plays the power-on animation sound (volume stays as set). The breakpoint sits on a path that crashes
# anyway, so a hit costs nothing extra.
# Usage: catch.sh [max_rounds=60]   stops at the first hit; exit 0 = hit, 1 = no hit in max_rounds, 2 = setup.
# Output: robot/work/seassert/catch-<timestamp>/ (rounds.log, round-N.gdb.log).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
max=${1:-60}
# The offset was read from this exact vic-anim build (built 2026-11-06 21:03); refuse any other binary.
want_md5=74a2435a7abac2e12fbc9c32f0922cf2
off=0x157804
window=30 # s attached per round; both crashes came ~13 s after vic-anim started

setup() { echo "SETUP: $*" >&2; exit 2; }
command -v gdb-multiarch >/dev/null || setup "no gdb-multiarch on the host (sudo apt install gdb-multiarch)"
# shellcheck disable=SC2029 # the remote commands are built here on purpose
ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
[[ $(ssh_ md5sum /anki/bin/vic-anim | cut -d' ' -f1) == "$want_md5" ]] ||
  setup "robot /anki/bin/vic-anim is not the build $off was read from"
ssh_ 'command -v gdbserver >/dev/null' || setup "no gdbserver on the robot"

out=$here/../work/seassert/catch-$(date +%Y%m%d-%H%M%S)
mkdir -p "$out"
log() { printf '%s %s\n' "$(date +%T)" "$*" | tee -a "$out/rounds.log"; }
ssh_cmd=$(printf '%q ' ssh "${ssh_opts[@]}" "root@$ip")
trap 'ssh_ "systemctl start anki-robot.target" || true' EXIT

for ((i = 1; i <= max; i++)); do
  t0=$(ssh_ 'date +%s')
  ssh_ 'systemctl stop anki-robot.target; sleep 5; systemctl start anki-robot.target'
  pid=
  for _ in $(seq 40); do
    pid=$(ssh_ 'pidof -s vic-anim' 2>/dev/null) && break
    sleep 0.5
  done
  [[ -n $pid ]] || { log "round $i: vic-anim did not start"; continue; }
  # PIE load base = start of the first mapping of the binary.
  base=$(ssh_ "awk '/\/anki\/bin\/vic-anim\$/ { split(\$1, a, \"-\"); print a[1]; exit }' /proc/$pid/maps")
  bp=$(printf '0x%x' $((0x$base + off)))
  age=$(($(ssh_ 'date +%s') - t0))
  g=$out/round-$i.gdb.log
  cat >"$out/round.gdb" <<EOF
set pagination off
set confirm off
set architecture arm
target remote | $ssh_cmd gdbserver - --attach $pid
handle all nostop noprint pass
break *$bp
commands
  silent
  echo \n=== HIT\n
  info registers
  info registers s0 s1 s2 s4 s16 s18
  echo --- table at r4 (x, gain pairs)\n
  x/36fw \$r4
  echo --- caller floor and saved d8/d9 (sp+8)\n
  x/4fw \$sp+8
  echo --- caller inputs: a=[r6] b=[r5] (flt32 path)\n
  x/fw \$r6
  x/fw \$r5
  bt 8
  detach
  quit
end
continue
detach
quit
EOF
  timeout -s INT -k 10 "$window" gdb-multiarch -q -batch -x "$out/round.gdb" >"$g" 2>&1 || true
  if grep -q '=== HIT' "$g"; then
    log "round $i: HIT (pid $pid, attached at +${age}s) -> $g"
    exit 0
  fi
  crash=$(ssh_ "journalctl -o short-iso --no-pager --since @$t0 | grep -E 'SeAssert|terminated by signal' | head -3" || true)
  log "round $i: no hit (pid $pid, attached at +${age}s)${crash:+ — UNCAUGHT crash: $crash}"
done
log "no hit in $max rounds"
exit 1
