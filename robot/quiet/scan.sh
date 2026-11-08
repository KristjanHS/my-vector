#!/usr/bin/env bash
# After a robot sound test (CLAUDE.md §Robot sound lock): scan.sh <since> — wire-pod "Transcribed text" lines since <since>
# (journalctl time) through quietmatch; exit 1 + the text on a "shut up" hit, 0 clean, 2 setup. SCAN_JOURNAL=<file> reads a file instead.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
(($# == 1)) || { echo "usage: scan.sh <since>   e.g. scan.sh '10 min ago'" >&2; exit 2; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$repo/.git")")
bin=$main/robot/work/bin/quietmatch
mkdir -p "$(dirname "$bin")"
(cd "$repo/chipper" && go build -o "$bin" ./pkg/myvector/quiet/quietmatch) || { echo "SETUP: quietmatch build failed" >&2; exit 2; }
if [[ -n ${SCAN_JOURNAL:-} ]]; then cat "$SCAN_JOURNAL"; else journalctl -u wire-pod --since "$1" --no-pager -o cat; fi |
  sed -n 's/.*Transcribed text[^:]*: //p' >"$main/robot/work/.scan-$$"
rc=0; out=$("$bin" <"$main/robot/work/.scan-$$") || rc=$?
rm -f "$main/robot/work/.scan-$$"
case $rc in
  0) echo "scan: no stop phrase since $1" ;;
  1) printf 'HIT: %s\n' "$out"; echo "stop every sound test, run make quiet, tell the user" >&2; exit 1 ;;
  *) exit 2 ;;
esac
