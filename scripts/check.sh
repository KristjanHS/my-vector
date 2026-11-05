#!/usr/bin/env bash
# Fast checks (make check, syncwt's gate): upstream drift, robot/scripts shell syntax, firmware static checks (check-static.sh), go vet of the patched sdkapp,
# chipper myvector + vars fork Go tests (when that dir exists: go test exits 1 on a missing pattern dir), tts pytest (when the TTS venv exists).
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
shopt -s nullglob # a pre-R6 commit has no robot/*.sh
for f in "$dir"/robot/*.sh "$dir"/robot/*/*.sh "$dir"/scripts/*.sh "$dir"/tts/*.sh; do bash -n "$f" || { echo "syntax: ${f#"$dir"/}" >&2; exit 1; }; done
echo "shell syntax OK"
"$dir"/robot/firmware/check-static.sh
(cd "$dir/chipper" && go vet ./pkg/wirepod/sdkapp/)
echo "go vet sdkapp OK"
if [[ -d $dir/chipper/pkg/myvector ]]; then (cd "$dir/chipper" && go test ./pkg/myvector/...); fi
if [[ -f $dir/chipper/pkg/vars/fork_intents_test.go ]]; then (cd "$dir/chipper" && go test -run Fork ./pkg/vars/); fi
(cd "$dir/chipper" && go test -run Fork ./pkg/wirepod/ttr/)
tts_py=$(dirname "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)")/robot/work/tts/venv/bin/python
if [[ -d $dir/tts/tests && -x $tts_py ]]; then "$tts_py" -m pytest -q -p no:cacheprovider "$dir/tts/tests"
elif [[ -d $dir/tts/tests ]]; then echo "tts tests skipped: no venv (make tts-setup)"; fi
