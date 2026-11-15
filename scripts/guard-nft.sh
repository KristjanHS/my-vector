#!/usr/bin/env bash
# nft source on stdin -> stdout, after one `define KEY = value` line per settings.defaults.env key.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
set -a
# shellcheck source=scripts/settings.defaults.env
. "$here/settings.defaults.env"
set +a
while IFS= read -r line; do
  [[ -z $line || $line == \#* ]] && continue
  k=${line%%=*}; echo "define $k = ${!k}"
done <"$here/settings.defaults.env"
cat
