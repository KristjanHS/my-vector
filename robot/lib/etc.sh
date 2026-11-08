# POSIX-sh helpers for writing the robot's read-only rootfs; an installer sends `cat lib/etc.sh -` ahead of its heredoc.
# ETC_ROOT prefixes every path (tests use a temp root); ETC_ARM_SCALE = seconds per arm minute (tests: 1); ROLLBACK_MIN = installers' arm window.
ETC_ROOT=${ETC_ROOT:-} ETC_RW= ETC_CHANGED= ROLLBACK_MIN=${ROLLBACK_MIN:-5}

etc_rw() {
  [ -n "$ETC_RW" ] && return 0
  mount -o remount,rw /
  ETC_RW=1
  trap 'mount -o remount,ro /' EXIT
}

etc_ro() {
  [ -z "$ETC_RW" ] && return 0
  trap - EXIT
  mount -o remount,ro /
  ETC_RW=
}

# etc_put <path> <content>: writes content + newline only when it differs; prints `changed <path>`, sets ETC_CHANGED.
etc_put() {
  [ -f "$ETC_ROOT$1" ] && [ "$(cat "$ETC_ROOT$1")" = "$2" ] && return 0
  etc_rw
  mkdir -p "$(dirname "$ETC_ROOT$1")"
  printf '%s\n' "$2" >"$ETC_ROOT$1"
  ETC_CHANGED=1
  echo "changed $1"
}

etc_ro_check() {
  awk '$2 == "/" && $4 ~ /^ro(,|$)/ { ro = 1 } END { exit !ro }' "$ETC_ROOT/proc/mounts" || { echo "FAIL: / is not read-only"; exit 1; }
}

# arm <name> <min> <restore-cmd>: runs restore-cmd (self-contained: the lib is not loaded there) after <min> unless confirmed.
arm() {
  command -v setsid >/dev/null || { echo "FAIL: arm $1: no setsid"; exit 1; }
  mkdir -p "$ETC_ROOT/run/myvector"
  rm -f "$ETC_ROOT/run/myvector/$1.confirm"
  setsid sh -c 'sleep "$1"; [ -e "$2" ] || eval "$3"' arm "$(($2 * ${ETC_ARM_SCALE:-60}))" \
    "$ETC_ROOT/run/myvector/$1.confirm" "$3" </dev/null >/dev/null 2>&1 &
}

confirm() {
  mkdir -p "$ETC_ROOT/run/myvector"
  : >"$ETC_ROOT/run/myvector/$1.confirm"
}
