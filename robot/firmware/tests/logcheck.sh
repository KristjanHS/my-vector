#!/usr/bin/env bash
# Host test of smoke.sh's logcheck exit codes (make check): its robot half under sh against fixture messages.1 + messages.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$fw/../../.git")")
mkdir -p "$main/robot/work"
tmp=$(mktemp -d "$main/robot/work/logcheck-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: logcheck host test: $*" >&2; exit 1; }

mkdir -p "$tmp/bin"
printf '#!/bin/sh\necho 1234\n' >"$tmp/bin/pidof"
chmod +x "$tmp/bin/pidof"
body=$(awk '/^logcheck\(\) \{/ { in_fn = 1 } in_fn && grab && /^EOF$/ { exit } grab { print } in_fn && /<<'\''EOF'\''$/ { grab = 1 }' "$fw/smoke.sh")
[[ $body == *'awk -v tag='* ]] || die "no logcheck robot body in smoke.sh"
fatal=$(sed -n "s/^fatal='\(.*\)'\$/\1/p" "$fw/smoke.sh")
[[ -n $fatal ]] || die "no fatal= in smoke.sh"

L() { echo "11-09 $1 info vic-robot 1234 $2"; }
start=$(L 10:00:00.000 'robot.main: Starting robot process')
boot=$(L 10:00:05.000 'LiftController.Calibrated lift reason Startup')
cal() { L "$1" 'LiftController.Calibrated lift reason Fall'; }
ns() { echo "11-09 11:00:00.000 info vic-engine $1 BehaviorSystemManager.BehaviorStack.UpdateBehaviorStack.NoStackInitialized: (tc0059)"; }
# run <name> <messages.1 lines> <messages lines>: prints "<rc> <output>"
run() {
  local d=$tmp/$1 rc=0 out; mkdir -p "$d"
  printf '%s' "$2" >"$d/messages.1"; printf '%s' "$3" >"$d/messages"
  touch -d '1 hour ago' "$d/messages.1"
  out=$(PATH="$tmp/bin:$PATH" FATAL=$fatal sh -c "${body//\/var\/log\//$d/}" 2>&1) || rc=$?
  echo "$rc $out"
}
want() { # want <rc> <name> <messages.1> <messages> [text]
  local got; got=$(run "$2" "$3" "$4")
  [[ ${got%% *} == "$1" ]] || die "$2: want rc $1, got: $got"
  [[ -z ${5:-} || $got == *"$5"* ]] || die "$2: want '$5' in: $got"
}

want 0 whole "$start"$'\n'"$boot"$'\n' "$(cal 11:00:00.000)"$'\n' 'since Startup'
want 3 rotated "$(cal 11:00:00.000)"$'\n' "$(cal 12:00:00.000)"$'\n' 'log rotated past Startup'
want 1 rotated-loop "$(cal 11:00:00.000)"$'\n'"$(cal 11:00:20.000)"$'\n' "$(cal 11:00:40.000)"$'\n' 'loop hits 1'
want 1 rotated-crash "$(cal 11:00:00.000)"$'\n' "$(L 11:00:01.000 'SIGSEGV in vic-robot')"$'\n' 'crash lines 1'
want 1 empty-stack "$start"$'\n'"$boot"$'\n' "$(ns 1234)" 'behavior stack empty: 1 NoStackInitialized'
want 0 empty-stack-old-engine "$start"$'\n'"$boot"$'\n' "$(ns 999)" 'since Startup'
want 1 no-startup "$start"$'\n' "$(cal 11:00:00.000)"$'\n'
echo "logcheck host test OK"
