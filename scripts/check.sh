#!/usr/bin/env bash
# Fast checks (make check, syncwt's gate): upstream drift, robot/scripts shell syntax, chipper myvector Go tests.
# Usage: check.sh [--head [rev]]   --head checks only that commit (default HEAD) via a git archive copy;
#   make ship passes its pinned sha so the checked commit is the one it ships.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
dir=$repo head=()
if [[ ${1:-} == --head ]]; then
  head=(--head "$(git -C "$repo" rev-parse --verify "${2:-HEAD}^{commit}")")
  dir=$(mktemp -d "$repo/robot/work/check-head.XXXXXX")
  trap 'rm -rf "$dir"' EXIT
  git -C "$repo" archive "${head[1]}" | tar -x -C "$dir"
fi
(cd "$repo" && scripts/upstream-drift.sh "${head[@]}")
for f in "$dir"/robot/*/*.sh "$dir"/scripts/*.sh; do bash -n "$f" || { echo "syntax: ${f#"$dir"/}" >&2; exit 1; }; done
echo "shell syntax OK"
(cd "$dir/chipper" && go test ./pkg/myvector/...)
