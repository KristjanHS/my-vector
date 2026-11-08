# shellcheck shell=bash
# Robot sound lock (shut-up-sleep L1/G1): quiet_locked is true while <main>/robot/work/quiet-until ($MYVECTOR_QUIET_FILE)
# holds a future epoch; quiet_guard then prints the QUIET line and exits 3. env.sh calls quiet_guard unless silent=1.
quiet_file() {
  if [[ -n ${MYVECTOR_QUIET_FILE:-} ]]; then printf '%s\n' "$MYVECTOR_QUIET_FILE"; return; fi
  local d common
  d=$(dirname "${BASH_SOURCE[0]}")
  common=$(git -C "$d" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || common=$(cd "$d/../.." && pwd)/.git
  printf '%s\n' "$(dirname "$common")/robot/work/quiet-until"
}

# quiet_locked: 0 while locked, sets quiet_until (epoch) and quiet_source; an unreadable or garbage file reads as unlocked.
quiet_locked() {
  local f u rest
  f=$(quiet_file)
  [[ -r $f ]] || return 1
  read -r u rest <"$f" || [[ -n ${u:-} ]] || return 1
  [[ $u =~ ^[0-9]+$ ]] && ((u > $(date +%s))) || return 1
  [[ $rest =~ ^(-|[0-9]+)\ (.*)$ ]] && rest=${BASH_REMATCH[2]} # a saved volume field (V1) precedes the source
  quiet_until=$u quiet_source=$rest
}

quiet_line() { echo "QUIET: robot sound locked until $(date -d "@$quiet_until" +%H:%M) ($quiet_source); make unquiet lifts it"; }

quiet_guard() {
  [[ ${silent:-} == 1 ]] && return 0
  quiet_locked || return 0
  quiet_line >&2
  exit 3
}
