# shellcheck shell=bash
# settings_load [--no-defaults]: exports the KEY=value layers, later wins: scripts/settings.defaults.env (skipped by --no-defaults)
# < ${DOTFILES_DIR:-<main>/../dotfiles}/shared-settings/settings.env < ${MY_VECTOR_OVERLAY_DIR:-<main>/../<overlay>}/settings.env.
# An absent layer is skipped. Run as `bash scripts/lib/settings.sh --dump [--no-defaults]` to print the resolved keys.
settings_layers() {
  local lib main
  lib=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
  main=$(git -C "$lib" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) && main=$(dirname "$main") ||
    main=$(cd "$lib/../.." && pwd) # git refused (root on a user-owned repo, no safe.directory): this checkout
  [[ ${1:-} == --no-defaults ]] || echo "$lib/../settings.defaults.env"
  echo "${DOTFILES_DIR:-$main/../dotfiles}/shared-settings/settings.env"
  echo "${MY_VECTOR_OVERLAY_DIR:-$main/../<overlay>}/settings.env"
}
settings_load() {
  local - f
  set -a
  while IFS= read -r f; do
    # shellcheck source=/dev/null
    if [[ -f $f ]]; then . "$f"; fi
  done < <(settings_layers "$@")
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  [[ ${1:-} == --dump ]] || { echo "usage: settings.sh --dump [--no-defaults]" >&2; exit 2; }
  shift
  settings_load "$@"
  while IFS= read -r f; do
    if [[ -f $f ]]; then sed -nE 's/^([A-Za-z_][A-Za-z0-9_]*)=.*/\1/p' "$f"; fi
  done < <(settings_layers "$@") | sort -u | while IFS= read -r k; do echo "$k=${!k}"; done
fi
