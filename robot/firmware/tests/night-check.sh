#!/usr/bin/env bash
# Host test of night-check.sh (make check): its awk over a fixture log via NIGHT_LOG_DIR, no robot.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$fw/../../.git")")
tmp=$(mktemp -d "$main/robot/work/night-check-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: night-check host test: $*" >&2; exit 1; }
u=$'\037'

# night <name> <wake anim stamp> [engine start stamp] [remount stamp]: a 2026-11-01 night, asleep, one InitialWakeUp anim.
night() {
  mkdir -p "$tmp/$1"
  {
    echo "11-01 20:00:00.000 info vic-engine 100 @behavior.sleeping.falling_asleep${u}NightWindow${u}DeepSleep"
    echo "11-01 21:30:00.000 info vic-engine 100 @action.play_animation${u}anim_sleeping_01${u}ag${u}GoToSleepSleeping${u}x"
    [[ -z ${4:-} ]] || echo "$4 info kernel  EXT4-fs (mmcblk0p22): re-mounted. Opts: (null)"
    [[ -z ${3:-} ]] || echo "$3 info systemd 1 Started Victor engine daemon."
    echo "$2 info vic-engine 200 @action.play_animation${u}anim_power_offon_02${u}ag_power_offon_01${u}InitialWakeUp${u}LowStim${u}-22"
    echo "11-02 09:30:00.000 info vic-engine 200 @behavior.feature.start${u}Sleeping${u}AI"
  } >"$tmp/$1/messages"
}
run() { NIGHT_LOG_DIR=$tmp/$1 "$fw/night-check.sh" 2026-11-01 2>&1; }

night ship '11-01 22:14:53.587' '11-01 22:14:40.855' '11-01 22:14:38.145'
out=$(run ship) || die "power-on anim 13 s after a ship's engine start: want PASS, got: $out"
grep -q 'hits 0, ships 1,' <<<"$out" || die "want 'hits 0, ships 1' in: $out"

night late '11-01 22:15:20.000' '11-01 22:14:40.855' '11-01 22:14:38.145'
out=$(run late) && die "power-on anim 40 s after a ship's engine start: want FAIL, got: $out"
grep -q 'hits 1, ships 0,' <<<"$out" || die "late: want 'hits 1, ships 0' in: $out"

# A reboot: its remount carries the stale boot clock (outside the window), so only the engine start shows.
night boot '11-01 22:14:53.587' '11-01 22:14:40.855'
out=$(run boot) && die "power-on anim after a reboot's engine start: want FAIL, got: $out"
grep -q 'hit (animation):.*InitialWakeUp' <<<"$out" || die "boot: want an InitialWakeUp hit in: $out"
# --nap: the window is the LAST 'nap until <epoch>' line to its epoch. Fixed clock (NIGHT_NOW): the stamps carry no year.
# nap <name> <now> <earlier nap line stamp> <nap line stamp> <until> <InitialWakeUp anim stamp> <last line stamp>
nap() {
  mkdir -p "$tmp/$1"
  {
    echo "$3 info vic-engine 100 [@Behaviors] my-vector.NightWindow: (tc1) nap until $(date -d "$2" +%s)"
    echo "$4 info vic-engine 100 [@Behaviors] my-vector.NightWindow: (tc2) nap until $(date -d "$5" +%s)"
    echo "$4 info vic-engine 100 @action.play_animation${u}anim_sleeping_01${u}ag${u}GoToSleepSleeping${u}x"
    echo "$6 info vic-engine 200 @action.play_animation${u}anim_power_offon_02${u}ag_power_offon_01${u}InitialWakeUp${u}LowStim${u}-22"
    echo "$7 info vic-engine 200 @behavior.feature.start${u}Sleeping${u}AI"
  } >"$tmp/$1/messages"
}
runnap() { NIGHT_NOW=$(date -d "$2" +%s) NIGHT_LOG_DIR=$tmp/$1 "$fw/night-check.sh" --nap 2>&1; }
nap napin '2026-11-01 18:00' '11-01 13:00:00.000' '11-01 14:00:00.000' '2026-11-01 16:00' '11-01 15:00:00.000' '11-01 17:00:00.000'
out=$(runnap napin '2026-11-01 18:00') && die "--nap: power-on anim inside the nap: want FAIL, got: $out"
grep -q 'hit (animation):.*InitialWakeUp' <<<"$out" || die "napin: want an InitialWakeUp hit in: $out"
nap napout '2026-11-01 18:00' '11-01 13:00:00.000' '11-01 14:00:00.000' '2026-11-01 16:00' '11-01 16:30:00.000' '11-01 17:00:00.000'
out=$(runnap napout '2026-11-01 18:00') || die "--nap: power-on anim after the nap: want PASS, got: $out"
grep -q '^nap 11-01 14:00:00 .. 11-01 16:00:00$' <<<"$out" || die "napout: want the last nap line's window in: $out"
# A nap across New Year: the stamp's year is the previous one.
nap napny '2027-01-01 02:00' '12-31 22:00:00.000' '12-31 23:00:00.000' '2027-01-01 01:00' '12-31 23:30:00.000' '01-01 01:30:00.000'
out=$(runnap napny '2027-01-01 02:00') && die "--nap across New Year: power-on anim inside: want FAIL, got: $out"
grep -q '^nap 12-31 23:00:00 .. 01-01 01:00:00$' <<<"$out" && grep -q 'hits 1,' <<<"$out" || die "napny: want the 12-31 window and 1 hit in: $out"
mkdir -p "$tmp/nonap" && echo "11-01 20:00:00.000 info vic-engine 100 @behavior.feature.start${u}Sleeping${u}AI" >"$tmp/nonap/messages"
rc=0; out=$(NIGHT_LOG_DIR=$tmp/nonap "$fw/night-check.sh" --nap 2>&1) || rc=$?
((rc == 2)) || die "--nap with no nap line: want exit 2, got $rc: $out"
echo "night-check host test OK"
