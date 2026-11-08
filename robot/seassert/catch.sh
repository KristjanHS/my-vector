#!/usr/bin/env bash
# Catch vic-anim's "SeAssert: se_lib/nrgainv.c:191" in the act. Each round restarts the robot's services (plain:
# stop, 5 s, start; -d: the ship deploy = the clone's build/deploy-v.sh, then settings.sh), attaches gdbserver to the
# new vic-anim and breaks on NrGainLookupCore's line-191 failure branch, before the assert's fprintf clobbers the
# registers. A hit dumps the operands (result s16, ratio s18, gain s4, 1-floor s2), the gain table at r4, the stack
# (saved d8 = caller floor at sp+8, saved lr names the caller) and fpscr.
# Every restart plays the power-on animation sound (volume stays as set).
# Usage: catch.sh [-d] [max_rounds=10]   stops at the first hit; exit 0 = hit, 1 = no hit, 2 = setup.
# Output: robot/work/seassert/catch-<timestamp>/ (rounds.log, round-N.gdb.log, deploy-N.log).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"
# shellcheck source=robot/crash-review/lib.sh
. "$here/../crash-review/lib.sh"
deploy=0
[[ ${1:-} == -d ]] && { deploy=1; shift; }
max=${1:-10}
window=30 # s attached per round; both crashes came ~13 s after vic-anim started
src=$here/../work/wire-os-victor
bin=$src/_build/vicos/Release/bin

setup() { echo "SETUP: $*" >&2; exit 2; }
command -v gdb-multiarch >/dev/null || setup "no gdb-multiarch on the host (sudo apt install gdb-multiarch)"
[[ -f $bin/vic-anim && -f $bin/vic-anim.full ]] || setup "no vic-anim build in $bin (build the clone first)"
# Breakpoint = the line-191 failure branch: the `ldr r0` right before `mov r3, #191` in NrGainLookupCore, read from
# the clone's symbol build; the robot must run that same build (md5 of the stripped binary).
want_md5=$(md5sum "$bin/vic-anim" | cut -d' ' -f1)
off=$(gdb-multiarch -q -batch -ex 'set arm force-mode arm' -ex 'disas NrGainLookupCore' "$bin/vic-anim.full" 2>/dev/null |
  grep -B1 'mov[[:space:]]*r3, #191' | sed -n '1s/^ *\(0x[0-9a-f]*\) .*ldr[[:space:]]*r0.*/\1/p') || off=
[[ $off =~ ^0x[0-9a-f]+$ ]] || setup "no ldr r0 / mov r3, #191 pair in NrGainLookupCore of $bin/vic-anim.full"
# shellcheck disable=SC2029 # the remote commands are built here on purpose
ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
robot_md5() { ssh_ md5sum /anki/bin/vic-anim | cut -d' ' -f1; }
((deploy)) || [[ $(robot_md5) == "$want_md5" ]] ||
  setup "robot /anki/bin/vic-anim is not the clone's build (ship it, or use -d to deploy it)"
ssh_ 'command -v gdbserver >/dev/null' || setup "no gdbserver on the robot"
((deploy)) && clone_ready "$src" "$here/../firmware"

out=$here/../work/seassert/catch-$(date +%Y%m%d-%H%M%S)
mkdir -p "$out/no-sysroot" # empty: gdb must not pull ~50 robot libs over ssh (ate the whole window); bp is a raw address
log() { printf '%s %s\n' "$(date +%T)" "$*" | tee -a "$out/rounds.log"; }
ssh_cmd=$(printf '%q ' ssh "${ssh_opts[@]}" "root@$ip")
gpid=
# Robot-side cleanup: no gdbserver left attached, no vic-anim left stopped (a SIGSTOP that lands after detach).
heal='sleep 1; killall gdbserver 2>/dev/null; for p in $(pidof vic-anim); do f=/proc/$p/status
  [ "$(sed -n "s/^State:[[:space:]]*\(.\).*/\1/p" $f)" = T ] && [ "$(sed -n "s/^TracerPid:[[:space:]]*//p" $f)" = 0 ] &&
  kill -CONT $p && echo "CONTed $p"; done; true'
cleanup() {
  [[ -n $gpid ]] && kill "$gpid" 2>/dev/null
  ssh_ "$heal; systemctl start --no-block anki-robot.target" || true
}
trap cleanup EXIT

attached=0
for ((i = 1; i <= max; i++)); do
  # A fault-code restart in flight (fault 800 = a unit failed) would race our restart.
  for _ in $(seq 30); do ssh_ 'systemctl is-active -q fault-code.service' 2>/dev/null || break; sleep 2; done
  old=$(ssh_ 'pidof -s vic-anim' 2>/dev/null) || old=none
  t0=$(ssh_ 'date +%s') || { log "round $i: ssh failed"; sleep 10; continue; }
  if ((deploy)); then
    # Same order as robot/firmware/ship.sh: deploy-v.sh (stop, rsync, start), then settings.sh.
    (
      cd "$src" && script -qec ./build/deploy-v.sh /dev/null
      echo "== deploy exit $?"
      "$here/../firmware/settings.sh"
      echo "== settings exit $?"
    ) >"$out/deploy-$i.log" 2>&1 </dev/null &
    dpid=$!
    tries=360 # 180 s: the deploy starts docker and rsyncs first
  else
    ssh_ 'systemctl stop anki-robot.target; sleep 5; systemctl start --no-block anki-robot.target' ||
      { log "round $i: restart ssh failed"; continue; }
    tries=60
  fi
  pid=
  for ((n = 0; n < tries; n++)); do
    p=$(ssh_ 'pidof -s vic-anim' 2>/dev/null) && [[ $p != "$old" ]] && { pid=$p; break; }
    sleep 0.5
  done
  # -d: the new vic-anim must be the clone's build before the clone's offset means anything.
  if [[ -n $pid ]] && ((deploy)) && [[ $(robot_md5) != "$want_md5" ]]; then
    log "round $i: robot vic-anim is not the clone build after the deploy — stop"
    wait "$dpid" || true
    exit 2
  fi
  if [[ -z $pid ]]; then
    log "round $i: no new vic-anim"
    ((deploy)) && { wait "$dpid" || true; }
    continue
  fi
  base=$(ssh_ "awk '/\/anki\/bin\/vic-anim\$/ { split(\$1, a, \"-\"); print a[1]; exit }' /proc/$pid/maps" 2>/dev/null) || base=
  [[ $base =~ ^[0-9a-f]+$ ]] || { log "round $i: no maps for pid $pid (died?)"; ((deploy)) && { wait "$dpid" || true; }; continue; }
  bp=$(printf '0x%x' $((0x$base + off)))
  # vic-anim's age at attach: uptime minus its start time (clock ticks, USER_HZ 100).
  age=$(ssh_ "echo \$(( \$(cut -d. -f1 /proc/uptime) - \$(awk '{print \$22}' /proc/$pid/stat) / 100 ))" 2>/dev/null) || age='?'
  g=$out/round-$i.gdb.log
  cat >"$out/round.gdb" <<EOF
set pagination off
set confirm off
set architecture arm
set auto-solib-add off
set arm force-mode arm
set sysroot $out/no-sysroot
target remote | $ssh_cmd gdbserver - --attach $pid
handle all nostop noprint pass
handle SIGSTOP stop print nopass
break *$bp
commands
  silent
  echo \n=== HIT\n
  info registers
  echo --- table at r4 (x, gain pairs)\n
  x/36fw \$r4
  echo --- stack: locals, saved d8 d9 (caller floor = sp+8), r4 r10 r11 lr (lr-base: 0x1576b8 flt32, 0x157930 i32)\n
  x/12xw \$sp
  x/4fw \$sp+8
  info registers s0 s1 s2 s4 s16 s18
  info registers fpscr
  echo --- caller inputs: a=[r6] b=[r5] (flt32 path; i32 path: [r6-4] [r5-4] as ints)\n
  x/fw \$r6
  x/fw \$r5
  x/2dw \$r6-4
  x/2dw \$r5-4
  bt 8
  detach
  quit
end
echo --- live code at bp (want: ldr r0 / mov r3, #191)\n
x/2i $bp
echo --- vfp readable\n
info registers s16
continue
detach
quit
EOF
  gdb-multiarch -q -batch -x "$out/round.gdb" >"$g" 2>&1 &
  gpid=$!
  for ((s = 0; s < window; s++)); do
    kill -0 $gpid 2>/dev/null || break
    grep -q '=== HIT' "$g" && break
    sleep 1
  done
  if kill -0 $gpid 2>/dev/null && ! grep -q '=== HIT' "$g"; then
    # gdbserver's interrupt signals vic-anim's process group, which vic-anim doesn't lead; SIGSTOP it directly
    # (gdb stops on it, drops it — nopass — and detaches).
    ssh_ "grep -q 'TracerPid:[[:space:]]*[1-9]' /proc/$pid/status && kill -STOP $pid" || true
  fi
  for _ in $(seq 20); do kill -0 $gpid 2>/dev/null || break; sleep 1; done
  kill -0 $gpid 2>/dev/null && { kill -9 $gpid; log "round $i: gdb did not detach, killed"; }
  wait $gpid || true
  gpid=
  if grep -q '=== HIT' "$g" && grep -q 'mov[[:space:]]*r3, #191' "$g"; then
    log "round $i: HIT (pid $pid, age at attach ${age}s) -> $g"
    exit 0
  fi
  st=$(ssh_ "$heal; grep -E '^(State|TracerPid)' /proc/$pid/status 2>/dev/null | tr -s '\t\n' '  '" || echo ssh-failed)
  if ((deploy)); then
    wait "$dpid" || true
    st+=" | $(grep '^== ' "$out/deploy-$i.log" | tr '\n' ' ')"
  fi
  crash=$(ssh_ "journalctl -o short-iso --no-pager --since @$t0 | grep -E 'SeAssert|terminated by signal|Handle fault code|Failed with result' | head -4" || true)
  if grep -q 'mov[[:space:]]*r3, #191' "$g" && grep -q '^s16 ' "$g" && ((s >= window)); then
    attached=$((attached + 1))
    log "round $i: no hit (pid $pid, age at attach ${age}s; after: ${st:-gone})${crash:+ — UNCAUGHT: $crash}"
  else
    log "round $i: NOT ATTACHED for the window (gdb ran ${s}s, see $g; after: ${st:-gone})${crash:+ — UNCAUGHT: $crash}"
  fi
  # Two fault restarts inside 97 s escalate the robot's fault handler; let its counter clear.
  [[ $crash == *'fault code'* ]] && { log "fault-code restart seen, waiting 100 s"; sleep 100; }
done
log "no hit: $attached of $max rounds attached for the full window"
exit 1
