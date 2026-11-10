#!/usr/bin/env bash
# secrets/ guard: only secrets/{.gitignore,README.md} may be tracked (head mode: in any unpushed commit too), and every other path under secrets/ is ignored.
# Usage: secrets.sh [repo] [--head rev tree]   default repo = this checkout; --head checks that commit (tree = its archive copy) instead of the index.
set -euo pipefail
repo=${1:-$(cd "$(dirname "$0")/../.." && pwd)}
g=(git -C "$repo")
if [[ ${2:-} == --head ]]; then
  rev=$3 g+=(--work-tree="$4")
  tracked=$("${g[@]}" ls-tree -r --name-only "$rev" -- secrets/)
  base=$("${g[@]}" rev-parse -q --verify origin/main) && range=("$base..$rev") || range=("$rev")
  tracked+=$'\n'$("${g[@]}" log -m --format= --name-only "${range[@]}" -- secrets/) # a key added then removed before ship still reaches origin
else
  tracked=$("${g[@]}" ls-files -- secrets/)
  tracked+=$'\n'$("${g[@]}" ls-files --others --exclude-standard -- secrets/) # present now and not ignored = the next git add -A
fi
extra=$(grep -vxE 'secrets/(\.gitignore|README\.md)|' <<<"$tracked" | sort -u || true)
[[ -z $extra ]] || { echo "FAIL: secrets: tracked or unignored: $extra" >&2; exit 1; }
for p in secrets/myvector_ed25519 secrets/tailscale-authkey secrets/zz-any secrets/zz-dir/zz-any secrets/server/apiConfig.json secrets/server/certs/cert.key; do
  "${g[@]}" check-ignore -q --no-index "$p" || { echo "FAIL: secrets: $p is not gitignored" >&2; exit 1; }
done
! "${g[@]}" check-ignore -q --no-index secrets/README.md || { echo "FAIL: secrets: README.md is ignored" >&2; exit 1; }
echo "secrets guard OK"
