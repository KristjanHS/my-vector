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
echo "night-check host test OK"
