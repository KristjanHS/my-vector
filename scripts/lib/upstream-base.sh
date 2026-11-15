# shellcheck shell=bash
# upstream_base <repo> [rev]: merge-base of rev (default HEAD) with ${UPSTREAM_REF:-upstream/main}, or <repo>/scripts/upstream-base when newer.
# The 2026-11-14 fold rewrote every upstream commit after 2022: the pin stands in until a real upstream merge lands.
upstream_base() {
  local base pin
  base=$(git -C "$1" merge-base "${2:-HEAD}" "${UPSTREAM_REF:-upstream/main}")
  pin=$(cat "$1/scripts/upstream-base" 2>/dev/null || true)
  if [[ -n $pin ]] && git -C "$1" merge-base --is-ancestor "$base" "$pin"; then base=$pin; fi
  echo "$base"
}
