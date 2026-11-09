# shellcheck shell=bash
# Robot sound lock (shut-up-sleep L1/G1): quiet_locked is true while <main>/robot/work/quiet-until ($MYVECTOR_QUIET_FILE)
# holds a future epoch; quiet_guard then prints the QUIET line and exits 3. env.sh calls quiet_guard unless an unexported silent=1.
quiet_file() {
  if [[ -n ${MYVECTOR_QUIET_FILE:-} ]]; then printf '%s\n' "$MYVECTOR_QUIET_FILE"; return; fi
  local d common
  d=$(dirname "${BASH_SOURCE[0]}")
  common=$(git -C "$d" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || common=$(cd "$d/../.." && pwd)/.git
  printf '%s\n' "$(dirname "$common")/robot/work/quiet-until"
}

# quiet_read: sets quiet_until (epoch), quiet_vol (saved volume, digits or -) and quiet_source, expired or not; 1 when unreadable or garbage.
quiet_read() {
  local f u rest
  quiet_vol=-
  f=$(quiet_file)
  [[ -r $f ]] || return 1
  read -r u rest <"$f" || [[ -n ${u:-} ]] || return 1
  [[ $u =~ ^[0-9]+$ ]] || return 1
  # shellcheck disable=SC2034 # quiet.sh reads it
  [[ $rest =~ ^(-|[0-9]+)\ (.*)$ ]] && quiet_vol=${BASH_REMATCH[1]} rest=${BASH_REMATCH[2]} # V1's volume field precedes the source
  quiet_until=$u quiet_source=$rest
}

# quiet_locked: 0 while locked (quiet_read's fields set); an expired, unreadable or garbage file reads as unlocked.
quiet_locked() { quiet_read && ((quiet_until > $(date +%s))); }

quiet_line() { echo "QUIET: robot sound locked until $(date -d "@$quiet_until" +%H:%M) ($quiet_source); make unquiet lifts it"; }

quiet_guard() {
  [[ ${silent:-} == 1 && ${silent@a} != *x* ]] && return 0 # N3: an exported silent (env silent=1 play.sh) is no opt-out
  quiet_locked || return 0
  quiet_line >&2
  exit 3
}

# quiet_silent_start <secs>: while locked, write Q1's flag (robot clock + secs) over env.sh's ssh before a restart, as ship.sh
# does; 1 + WARN when the write fails, so the caller skips that restart.
quiet_silent_start() {
  quiet_locked || return 0
  # shellcheck disable=SC2029,SC2154 # $1 expands on the client; ssh_opts and ip come from env.sh
  ssh "${ssh_opts[@]}" "root@$ip" "mkdir -p /data/myvector && echo \$((\$(date +%s) + $1)) >/data/myvector/silent-start" && return 0
  echo "WARN sound lock: could not write /data/myvector/silent-start, skipping the anki-robot.target restart" >&2
  return 1
}
