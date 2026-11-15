# shellcheck shell=bash
# upstream_base <repo> [rev]: merge-base of rev (default HEAD) with ${UPSTREAM_REF:-upstream/main}, or <repo>/scripts/upstream-base when newer.
# The 2026-11-14 fold rewrote every upstream commit after 2022: the pin stands in until a real upstream merge lands.
upstream_base() {
  local base pin
  base=$(git -C "$1" merge-base "${2:-HEAD}" "${UPSTREAM_REF:-upstream/main}") || { echo "no merge-base with ${UPSTREAM_REF:-upstream/main}: git fetch upstream" >&2; return 1; }
  pin=$(cat "$1/scripts/upstream-base" 2>/dev/null || true)
  [[ -z $pin ]] || git -C "$1" cat-file -e "$pin^{commit}" 2>/dev/null || { echo "pin ${pin:0:12} missing: git fetch upstream" >&2; return 1; }
  if [[ -n $pin ]] && git -C "$1" merge-base --is-ancestor "$base" "$pin"; then base=$pin; fi
  echo "$base"
}
