#!/usr/bin/env bash
# nft source on stdin -> stdout, after one `define KEY = value` line per settings.defaults.env key, valued by settings_load (scripts/lib/settings.sh).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=scripts/lib/settings.sh
. "$here/lib/settings.sh"
settings_load
while IFS= read -r line; do
  [[ -z $line || $line == \#* ]] && continue
  k=${line%%=*}; echo "define $k = ${!k}"
done <"$here/settings.defaults.env"
cat
