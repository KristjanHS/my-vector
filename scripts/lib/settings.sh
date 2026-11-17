# shellcheck shell=bash
# settings_load [--no-defaults]: exports the KEY=value layers, later wins: scripts/settings.defaults.env (skipped by --no-defaults)
# < ${DOTFILES_DIR:-<main>/../dotfiles}/shared-settings/settings.env < <overlay>/settings.env. The overlay dir: MY_VECTOR_OVERLAY_DIR
# from the environment when set (empty = none), else the last MY_VECTOR_OVERLAY_DIR= line of the dotfiles layer, else no overlay
# (no default path: it works under root load-guard.sh, whose environment lacks the caller's). An absent layer is skipped.
# Run as `bash scripts/lib/settings.sh --dump [--no-defaults]` to print the resolved keys (no *_OVERLAY_DIR pointer: never exported).
# The layers are parsed, never sourced (load-guard.sh runs this as root over user-owned files): see settings_parse.
settings_layers() {
  local lib main dot ov=
  [[ $# == 0 || $* == --no-defaults ]] || { echo "settings.sh: unknown arg $*" >&2; return 2; }
  lib=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
  main=$(git -C "$lib" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) && main=$(dirname "$main") ||
    main=$(cd "$lib/../.." && pwd) # git refused (root on a user-owned repo, no safe.directory): this checkout
  [[ ${1:-} == --no-defaults ]] || echo "$lib/../settings.defaults.env"
  dot=${DOTFILES_DIR:-$main/../dotfiles}/shared-settings/settings.env
  echo "$dot"
  if [[ -n ${MY_VECTOR_OVERLAY_DIR+set} ]]; then ov=$MY_VECTOR_OVERLAY_DIR
  elif [[ -f $dot ]]; then # an invalid dotfiles layer means no pointer here; settings_load then fails on that layer itself
    ov=$(settings_parse "$dot" 2>/dev/null | sed -n 's/^MY_VECTOR_OVERLAY_DIR=//p' | tail -1) || ov=
  fi
  if [[ -n $ov ]]; then echo "$ov/settings.env"; fi
}
# settings_parse <file>: its KEY=value lines on stdout, in file order. Strips a line-1 UTF-8 BOM and each line's trailing CR;
# skips empty and ^# lines; any other line must be KEY=value with no whitespace, quote, $, backtick, # or backslash in the
# value, else `settings: <file>:<lineno>: invalid line` on stderr (never the line: it may hold a real value) and return 1.
# Same contract as hands-on-llm's Python reader. LC_ALL=C: [A-Za-z] is ASCII-only whatever the caller's locale.
settings_parse() {
  local LC_ALL=C line n=0 re='^[A-Za-z_][A-Za-z0-9_]*=[^[:space:]"'\''$`#\\]*$'
  while IFS= read -r line || [[ -n $line ]]; do
    n=$((n + 1))
    if [[ $n == 1 ]]; then line=${line#$'\xef\xbb\xbf'}; fi
    line=${line%$'\r'}
    if [[ -z $line || $line == \#* ]]; then continue; fi
    [[ $line =~ $re ]] || { echo "settings: $1:$n: invalid line" >&2; return 1; }
    printf '%s\n' "$line"
  done <"$1"
}
settings_load() { # every layer is parsed before any key is exported: an invalid line leaves the environment untouched
  local _sl_f _sl_layers _sl_kv _sl_all=
  _sl_layers=$(settings_layers "$@") || return
  while IFS= read -r _sl_f; do
    if [[ -f $_sl_f ]]; then _sl_kv=$(settings_parse "$_sl_f") || return; _sl_all+=$_sl_kv$'\n'; fi
  done <<<"$_sl_layers"
  while IFS= read -r _sl_kv; do # later lines win; no *_OVERLAY_DIR pointer (any project's) is exported: the caller's env keeps winning in children
    if [[ -n $_sl_kv && ${_sl_kv%%=*} != *_OVERLAY_DIR ]]; then printf -v "${_sl_kv%%=*}" '%s' "${_sl_kv#*=}"; export "${_sl_kv%%=*}"; fi
  done <<<"$_sl_all"
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  [[ ${1:-} == --dump ]] || { echo "usage: settings.sh --dump [--no-defaults]" >&2; exit 2; }
  shift
  settings_load "$@"
  while IFS= read -r f; do
    if [[ -f $f ]]; then settings_parse "$f"; fi
  done < <(settings_layers "$@") | sed -e '/^[A-Za-z0-9_]*_OVERLAY_DIR=/d' -e 's/=.*//' | sort -u | while IFS= read -r k; do echo "$k=${!k}"; done
fi
