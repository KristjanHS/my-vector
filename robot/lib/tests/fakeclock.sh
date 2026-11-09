# shellcheck shell=bash
# Fake clock for the arm-window host tests: robot/lib/etc.sh's arm() timers run `sleep` via PATH,
# so a fake one parks them until the test says the window is over — no wall-clock wait, no race with a slow run.
# fakeclock_install <bindir>: writes <bindir>/sleep — parks an arm() timer until the fire (or the test shell exits: kills the timer); any other caller gets the real sleep.
# fakeclock_fire: ends the window of every timer armed so far, waits until each has exited (its restore done), then re-parks later arms.
fakeclock_install() {
  _fc_root=${1%/bin} _fc_real=$(command -v sleep)
  [[ $1 == */bin && $_fc_real == /* && $_fc_real != "$_fc_root"/* ]] || { echo "FAIL: fakeclock: want <tmp>/bin and a real sleep outside it" >&2; return 1; }
  mkdir -p "$_fc_root/clock"
  cat >"$1/sleep" <<EOF
#!/bin/sh
case \$(tr '\0' ' ' </proc/\$PPID/cmdline) in "sh -c sleep "*" arm "*) ;; *) exec $_fc_real "\$@" ;; esac
while [ ! -e '$_fc_root/clock/fire' ]; do
  kill -0 $$ 2>/dev/null || { kill \$PPID 2>/dev/null; exit 1; }
  $_fc_real 0.02
done
EOF
  chmod +x "$1/sleep"
}

# _fakeclock_live: a process whose ETC_ROOT is under the test tmp — a timer from fork through its restore (runs are synchronous).
_fakeclock_live() { grep -qzsF -e "ETC_ROOT=$_fc_root/" /proc/[0-9]*/environ 2>/dev/null; }

fakeclock_fire() {
  local end=$((SECONDS + 10))
  : >"$_fc_root/clock/fire"
  while _fakeclock_live; do
    ((SECONDS < end)) || { echo "FAIL: fakeclock: an arm timer still runs 10 s after the fire" >&2; return 1; }
    "$_fc_real" 0.02
  done
  rm "$_fc_root/clock/fire"
}
