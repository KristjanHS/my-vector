#!/usr/bin/env bash
# Fast checks (make check, syncwt's gate): upstream drift, robot/scripts shell syntax, wire-pod-guard.nft syntax (nft -c in a user+net namespace) + scripts/tests/guard.sh, firmware static checks (check-static.sh), patch 0010 host test (tests/pc-voice.sh), robot/lib/tests/etc.sh + robot/{sshkey,hostkey}/tests/apply.sh + scripts/tests/secrets.sh + robot/openmic/tests/listen.sh, go vet + Fork tests of the patched sdkapp,
# chipper myvector + vars + localization fork Go tests (when that dir exists: go test exits 1 on a missing pattern dir), tts + stt pytest (when each venv exists).
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
for f in "$dir"/robot/*.sh "$dir"/robot/*/*.sh "$dir"/robot/*/tests/*.sh "$dir"/scripts/*.sh "$dir"/scripts/tests/*.sh "$dir"/tts/*.sh "$dir"/et-stt/*.sh; do bash -n "$f" || { echo "syntax: ${f#"$dir"/}" >&2; exit 1; }; done
echo "shell syntax OK"
if [[ -f $dir/scripts/wire-pod-guard.nft ]] && unshare -rn true 2>/dev/null; then
  unshare -rn /usr/sbin/nft -c -f "$dir/scripts/wire-pod-guard.nft" || { echo "nft syntax: scripts/wire-pod-guard.nft" >&2; exit 1; }
  echo "nft guard syntax OK"
  "$dir"/scripts/tests/guard.sh
elif [[ -f $dir/scripts/wire-pod-guard.nft ]]; then echo "nft guard check skipped: no unprivileged unshare"; fi
"$dir"/robot/firmware/check-static.sh
if [[ -f $dir/robot/firmware/tests/pc-voice.sh ]]; then "$dir"/robot/firmware/tests/pc-voice.sh; fi # SKIPs itself without g++/clone
if [[ -f $dir/robot/firmware/tests/night-check.sh ]]; then "$dir"/robot/firmware/tests/night-check.sh; fi
if [[ -f $dir/robot/lib/tests/etc.sh ]]; then "$dir"/robot/lib/tests/etc.sh; fi
if [[ -f $dir/robot/sshkey/tests/apply.sh ]]; then "$dir"/robot/sshkey/tests/apply.sh; fi
if [[ -f $dir/robot/hostkey/tests/apply.sh ]]; then "$dir"/robot/hostkey/tests/apply.sh; fi
if [[ -f $dir/scripts/tests/secrets.sh ]]; then "$dir"/scripts/tests/secrets.sh "$repo" "${head[@]}" ${head[0]:+"$dir"}; fi
if [[ -f $dir/robot/openmic/tests/listen.sh ]]; then "$dir"/robot/openmic/tests/listen.sh; fi
(cd "$dir/chipper" && go vet ./pkg/wirepod/sdkapp/ && go test -run Fork ./pkg/wirepod/sdkapp/)
echo "go vet + Fork tests sdkapp OK"
if [[ -d $dir/chipper/pkg/myvector ]]; then (cd "$dir/chipper" && go test ./pkg/myvector/...); fi
if [[ -f $dir/chipper/pkg/vars/fork_intents_test.go ]]; then (cd "$dir/chipper" && go test -run Fork ./pkg/vars/); fi
(cd "$dir/chipper" && go test -run Fork ./pkg/wirepod/ttr/)
if [[ -f $dir/chipper/pkg/wirepod/localization/fork_lang_test.go ]]; then (cd "$dir/chipper" && go test -run Fork ./pkg/wirepod/localization/); fi
(cd "$dir/chipper" && go test -tags nolibopusfile -run Fork ./pkg/wirepod/preqs/)
tts_py=$(dirname "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)")/robot/work/tts/venv/bin/python
if [[ -d $dir/tts/tests && -x $tts_py ]]; then "$tts_py" -m pytest -q -p no:cacheprovider "$dir/tts/tests"
elif [[ -d $dir/tts/tests ]]; then echo "tts tests skipped: no venv (make tts-setup)"; fi
stt_py=$(dirname "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)")/robot/work/et-stt/venv/bin/python
if [[ -d $dir/et-stt/tests && -x $stt_py ]]; then "$stt_py" -m pytest -q -p no:cacheprovider "$dir/et-stt/tests"
elif [[ -d $dir/et-stt/tests ]]; then echo "stt tests skipped: no venv (make stt-setup)"; fi
