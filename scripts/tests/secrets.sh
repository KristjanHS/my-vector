#!/usr/bin/env bash
# secrets/ guard: only secrets/{.gitignore,README.md} may be tracked, and any other path under secrets/ must be ignored.
# Usage: secrets.sh [repo] [--head rev]   default repo = this checkout; --head checks that commit's tree instead of the index.
set -euo pipefail
repo=${1:-$(cd "$(dirname "$0")/../.." && pwd)}
if [[ ${2:-} == --head ]]; then tracked=$(git -C "$repo" ls-tree -r --name-only "$3" -- secrets/)
else tracked=$(git -C "$repo" ls-files -- secrets/); fi
extra=$(grep -vxE 'secrets/(\.gitignore|README\.md)' <<<"$tracked" || true)
[[ -z $extra ]] || { echo "FAIL: secrets: tracked secret file(s): $extra" >&2; exit 1; }
for p in secrets/myvector_ed25519 secrets/x.pub secrets/epod/ep.key secrets/server-state/s.tgz; do
  git -C "$repo" check-ignore -q --no-index "$p" || { echo "FAIL: secrets: $p is not gitignored" >&2; exit 1; }
done
! git -C "$repo" check-ignore -q --no-index secrets/README.md || { echo "FAIL: secrets: README.md is ignored" >&2; exit 1; }
echo "secrets guard OK"
