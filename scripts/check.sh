#!/usr/bin/env bash
# Fast checks (make check, syncwt's gate): upstream drift, robot/scripts shell syntax, chipper myvector Go tests.
# Usage: check.sh [--head]   --head checks the committed HEAD only, via a git archive copy (make ship).
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
dir=$repo
if [[ ${1:-} == --head ]]; then
  dir=$(mktemp -d "$repo/robot/work/check-head.XXXXXX")
  trap 'rm -rf "$dir"' EXIT
  git -C "$repo" archive HEAD | tar -x -C "$dir"
fi
(cd "$repo" && scripts/upstream-drift.sh ${1:+--head})
for f in "$dir"/robot/*/*.sh "$dir"/scripts/*.sh; do bash -n "$f" || { echo "syntax: ${f#"$dir"/}" >&2; exit 1; }; done
echo "shell syntax OK"
(cd "$dir/chipper" && go test ./pkg/myvector/...)
