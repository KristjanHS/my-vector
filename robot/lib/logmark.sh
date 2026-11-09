# shellcheck shell=bash
# Robot-side sh for reading /var/log/messages from a logger tag on: it rotates at 8 MB (robot/logkeep), so a line count breaks.
# log_mark <word>: logs a unique tag, prints it once it is in messages* (≤5 s; exit 1 = never landed). log_since <tag> [word]: lines after it.
log_mark() {
  [[ $1 =~ ^[A-Za-z0-9-]+$ ]] || { echo 'exit 1'; return; }
  # shellcheck disable=SC2016 # expands on the robot
  printf 't=my-vector.mark.%s.$(date +%%s)-$$; logger $t; i=0
until grep -qsF $t /var/log/messages /var/log/messages.1; do i=$((i + 1)); [ $i -le 5 ] || exit 1; sleep 1; done; echo $t' "$1"
}
# log_since: oldest first across messages.* + messages, only lines containing [word]; exit 1 = tag rotated out (or never there): never "none".
log_since() {
  [[ $1 =~ ^my-vector\.mark\.[A-Za-z0-9-]+\.[0-9]+-[0-9]+$ && ${2:-} =~ ^[A-Za-z0-9.-]*$ ]] || { echo 'exit 1'; return; }
  # shellcheck disable=SC2016 # expands on the robot
  printf 'cat $(ls -tr /var/log/messages.* 2>/dev/null) /var/log/messages | awk -v t=%s -v g=%s %s' "$1" "${2:-}" \
    "'f && (g == \"\" || index(\$0, g)); index(\$0, t) { f = 1 } END { exit !f }'"
}
