# shellcheck shell=bash
# Fake clock for the arm-window host tests (syncwt-speed R3): robot/lib/etc.sh's arm() timers run `sleep` via PATH,
# so a fake one parks them until the test says the window is over — no wall-clock wait, no race with a slow run.
# fakeclock_install <bindir>: writes <bindir>/sleep (parks until the fire; kills its timer and exits once <bindir>/../clock is gone).
# fakeclock_fire: ends the window of every timer armed so far, waits until each has exited (its restore done),
# then re-parks: a timer armed after this call waits for the next fire.
fakeclock_install() {
  _fc_root=$(cd "$1/.." && pwd) _fc_real=$(command -v sleep)
  [[ $_fc_real == /* && $_fc_real != "$_fc_root"/* ]] || { echo "FAIL: fakeclock: no real sleep outside $_fc_root" >&2; return 1; }
  mkdir -p "$_fc_root/clock"
  cat >"$1/sleep" <<EOF
#!/bin/sh
while [ ! -e '$_fc_root/clock/fire' ]; do
  [ -d '$_fc_root/clock' ] || { kill \$PPID 2>/dev/null; exit 1; }
  $_fc_real 0.02
done
EOF
  chmod +x "$1/sleep"
}

fakeclock_fire() {
  local n=0
  : >"$_fc_root/clock/fire"
  # arm()'s timer: sh -c 'sleep "$1"; [ -e "$2" ] || eval "$3"' arm <s> <ETC_ROOT under the test tmp>/run/myvector/<name>.confirm <restore>
  while pgrep -f -- "^sh -c sleep .* arm [0-9]+ $_fc_root/" >/dev/null; do
    ((++n < 500)) || { echo "FAIL: fakeclock: an arm timer still runs 10 s after the fire" >&2; return 1; }
    "$_fc_real" 0.02
  done
  rm "$_fc_root/clock/fire"
}
