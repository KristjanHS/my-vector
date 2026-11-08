#!/usr/bin/env bash
# Robot sound lock control (shut-up-sleep L2): quiet.sh on [min] | off | status — on/off ask wire-pod (/myvector/quiet: lock,
# nap, volume); server down → on writes the lock file alone, off removes it (or expires it, keeping a saved volume). status exits 3 while locked. MYVECTOR_URL overrides the server.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/quiet/guard.sh
. "$here/guard.sh"
url=${MYVECTOR_URL:-http://127.0.0.1:8080}/myvector/quiet
f=$(quiet_file)
case ${1:-} in
  on)
    min=${2:-120}
    [[ $min =~ ^[1-9][0-9]*$ ]] || { echo "usage: quiet.sh on [minutes]" >&2; exit 2; }
    if ! curl -fsS -m 10 -X POST "$url?on=$min" >/dev/null 2>&1; then
      echo "WARN: wire-pod unreachable — lock file only (no nap, no volume change)" >&2
      end=$(($(date +%s) + min * 60))
      if ! quiet_locked || ((end > quiet_until)); then
        mkdir -p "$(dirname "$f")"
        tmp=$(mktemp "$(dirname "$f")/.quiet-until-XXXXXX")
        echo "$end ${quiet_vol:--} cli:quiet.sh" >"$tmp" && chmod 666 "$tmp" && mv -f "$tmp" "$f" # keeps a saved volume, even an expired file's
      fi
    fi
    quiet_locked && quiet_line ;;
  off)
    if ! curl -fsS -m 10 -X POST "$url?off=1" >/dev/null 2>&1; then
      if quiet_read && [[ $quiet_vol != - ]]; then
        echo "WARN: wire-pod unreachable — lock ended; nap and volume left as they are: set the robot's volume back to $quiet_vol by hand (an expired lock file keeps it for wire-pod's restore)" >&2
        tmp=$(mktemp "$(dirname "$f")/.quiet-until-XXXXXX")
        echo "$(date +%s) $quiet_vol $quiet_source" >"$tmp" && chmod 666 "$tmp" && mv -f "$tmp" "$f"
      else
        echo "WARN: wire-pod unreachable — lock file removed; nap and volume left as they are" >&2
        rm -f "$f"
      fi
    fi
    echo "robot sound unlocked" ;;
  status)
    if quiet_locked; then quiet_line; exit 3; fi
    echo "robot sound unlocked" ;;
  *) echo "usage: quiet.sh on [minutes] | off | status" >&2; exit 2 ;;
esac
