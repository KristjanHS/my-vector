#!/usr/bin/env bash
# Crash catalogue: real crash dumps (.dmp + .dmp.log; .dmp~ = stop-cascade snapshots, skipped) and journal crash lines.
# Usage: crashes.sh [-p] [since='today']   since = any journalctl --since value; -p copies the dumps into
# robot/work/crash-review/dumps/ (read them with robot/seassert/md.py).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
pull=0
[[ ${1:-} == -p ]] && { pull=1; shift; }
since=${1:-today}
dumps=/data/data/com.anki.victor/cache/crashDumps
# shellcheck disable=SC2029 # the remote commands are built here on purpose
ssh_() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
ssh_ true || { echo "SETUP: robot unreachable over ssh" >&2; exit 2; }
t0=$(ssh_ "date -d '$since' +%s") || { echo "SETUP: robot date can't parse since='$since'" >&2; exit 2; }

echo "== crash dumps since $since"
list=$(ssh_ "cd $dumps && for f in *.dmp; do [ -f \"\$f\" ] && [ \$(date -r \"\$f\" +%s) -ge $t0 ] && echo \"\$f\"; done; true")
echo "${list:-none}"
echo "== journal (SeAssert, signals, fault codes, failed units)"
ssh_ "journalctl -o short-iso --no-pager --since @$t0" |
  grep -E 'SeAssert|terminated by signal|fault code [0-9]+|Failed with result|Start operation timed out' || echo none
if ((pull)) && [[ -n $list ]]; then
  out=$here/../work/crash-review/dumps
  mkdir -p "$out"
  for f in $list; do scp -q "${ssh_opts[@]}" "root@$ip:$dumps/$f" "root@$ip:$dumps/$f.log" "$out/" || echo "copy failed: $f"; done
  echo "== copied to $out"
fi
