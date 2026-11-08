#!/usr/bin/env bash
# Fast checks (make check, syncwt's gate): upstream drift, robot/scripts shell syntax, wire-pod-guard.nft syntax (nft -c in a user+net namespace) + scripts/tests/guard.sh, firmware static checks (check-static.sh), patch 0010 + 0016 host tests (tests/pc-voice.sh, tests/mic-stream.sh), robot/lib/tests/etc.sh + robot/{sshkey,hostkey}/tests/apply.sh + robot/epod/tests/{mkcert,apply,interlock}.sh + robot/netguard/tests/{rules,hosts,apply}.sh + scripts/tests/secrets.sh + robot/openmic/tests/listen.sh, go vet + Fork tests of the patched sdkapp,
# chipper myvector + vars + localization fork Go tests (when that dir exists: go test exits 1 on a missing pattern dir), tts + stt pytest (when each venv exists).
# Usage: check.sh [--head [rev]]   --head checks only that commit (default HEAD) via a git archive copy;
#   make ship passes its pinned sha so the checked commit is the one it ships.
# Green writes a pass stamp robot/work/check-pass/<key>, key = content tree + toolchain + clone (stamp_key); --head exits 0 on a stamped key.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
main=$(dirname "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)")
tts_py=$main/robot/work/tts/venv/bin/python stt_py=$main/robot/work/et-stt/venv/bin/python
stamps=$main/robot/work/check-pass
# The working tree's content as a tree id (untracked non-ignored files included): equals HEAD^{tree} once all of it is committed.
wt_tree() {
  local idx; idx=$(mktemp "$main/robot/work/check-index.XXXXXX")
  cp "$(git -C "$repo" rev-parse --path-format=absolute --git-path index)" "$idx"
  GIT_INDEX_FILE=$idx git -C "$repo" add -A && GIT_INDEX_FILE=$idx git -C "$repo" write-tree; rm -f "$idx"
}
# stamp_key <tree>: the tree plus what the checks read from outside it (toolchain, venvs, userns, the victor clone).
stamp_key() {
  { echo "$1"; go version || echo "no go"; { g++ --version || echo "no g++"; } | head -1; unshare -rn true && echo userns
    for py in "$tts_py" "$stt_py"; do [[ -x $py ]] && "$py" --version && stat -c %Y "${py%/bin/python}"/lib/python*/site-packages || echo "no $py"; done
    echo "${VICTOR_SRC:-}"; git -C "${VICTOR_SRC:-$main/robot/work/wire-os-victor}" rev-parse HEAD || echo "no clone"
  } 2>&1 | sha1sum | cut -c1-40
}
dir=$repo head=()
if [[ ${1:-} == --head ]]; then
  head=(--head "$(git -C "$repo" rev-parse --verify "${2:-HEAD}^{commit}")")
  tree=$(git -C "$repo" rev-parse "${head[1]}^{tree}") key=$(stamp_key "$tree")
  dir=$(mktemp -d "$repo/robot/work/check-head.XXXXXX")
  trap 'rm -rf "$dir"' EXIT
  git -C "$repo" archive "${head[1]}" | tar -x -C "$dir"
else tree=$(wt_tree) key=$(stamp_key "$tree")
fi
(cd "$repo" && scripts/upstream-drift.sh "${head[@]}")
# A stamp hit still runs the commit-range checks (drift above, secrets in every unpushed commit): the key holds only the tree.
if [[ -n ${head[*]} && -f $stamps/$key ]]; then
  if [[ -f $dir/scripts/tests/secrets.sh ]]; then "$dir"/scripts/tests/secrets.sh "$repo" "${head[@]}" "$dir"; fi
  echo "check skipped: tree ${tree:0:12} passed already ($(<"$stamps/$key")); drift + secrets ran"; exit 0
fi
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
if [[ -f $dir/robot/firmware/tests/mic-stream.sh ]]; then "$dir"/robot/firmware/tests/mic-stream.sh; fi # SKIPs itself without g++/clone
if [[ -f $dir/robot/firmware/tests/night-check.sh ]]; then "$dir"/robot/firmware/tests/night-check.sh; fi
if [[ -f $dir/robot/lib/tests/etc.sh ]]; then "$dir"/robot/lib/tests/etc.sh; fi
if [[ -f $dir/robot/sshkey/tests/apply.sh ]]; then "$dir"/robot/sshkey/tests/apply.sh; fi
if [[ -f $dir/robot/hostkey/tests/apply.sh ]]; then "$dir"/robot/hostkey/tests/apply.sh; fi
if [[ -f $dir/robot/epod/tests/mkcert.sh ]]; then "$dir"/robot/epod/tests/mkcert.sh; fi
if [[ -f $dir/robot/epod/tests/apply.sh ]]; then "$dir"/robot/epod/tests/apply.sh; fi
for t in rules hosts apply; do if [[ -f $dir/robot/netguard/tests/$t.sh ]]; then "$dir"/robot/netguard/tests/$t.sh; fi; done
if [[ -f $dir/robot/epod/tests/interlock.sh ]]; then "$dir"/robot/epod/tests/interlock.sh; fi
if [[ -f $dir/scripts/tests/secrets.sh ]]; then "$dir"/scripts/tests/secrets.sh "$repo" "${head[@]}" ${head[0]:+"$dir"}; fi
if [[ -f $dir/robot/openmic/tests/listen.sh ]]; then "$dir"/robot/openmic/tests/listen.sh; fi
if [[ -f $dir/robot/quiet/tests/guard.sh ]]; then "$dir"/robot/quiet/tests/guard.sh; fi
(cd "$dir/chipper" && go vet ./pkg/wirepod/sdkapp/ && go test -run Fork ./pkg/wirepod/sdkapp/)
echo "go vet + Fork tests sdkapp OK"
if [[ -d $dir/chipper/pkg/myvector ]]; then (cd "$dir/chipper" && go test ./pkg/myvector/...); fi
if [[ -f $dir/chipper/pkg/vars/fork_intents_test.go ]]; then (cd "$dir/chipper" && go test -run Fork ./pkg/vars/); fi
(cd "$dir/chipper" && go test -run Fork ./pkg/wirepod/ttr/)
if [[ -f $dir/chipper/pkg/wirepod/localization/fork_lang_test.go ]]; then (cd "$dir/chipper" && go test -run Fork ./pkg/wirepod/localization/); fi
(cd "$dir/chipper" && go test -tags nolibopusfile -run Fork ./pkg/wirepod/preqs/)
if [[ -d $dir/tts/tests && -x $tts_py ]]; then "$tts_py" -m pytest -q -p no:cacheprovider "$dir/tts/tests"
elif [[ -d $dir/tts/tests ]]; then echo "tts tests skipped: no venv (make tts-setup)"; fi
if [[ -d $dir/et-stt/tests && -x $stt_py ]]; then "$stt_py" -m pytest -q -p no:cacheprovider "$dir/et-stt/tests"
elif [[ -d $dir/et-stt/tests ]]; then echo "stt tests skipped: no venv (make stt-setup)"; fi
# Stamp only a tree that did not change while the checks ran; stamps older than 30 days are pruned.
if [[ -n ${head[*]} || $(wt_tree) == "$tree" ]]; then
  mkdir -p "$stamps"; find "$stamps" -type f -mtime +30 -delete
  echo "${head[1]:-working tree} at $(date '+%F %T')" >"$stamps/$key"
  echo "check passed: stamped tree ${tree:0:12}"
else echo "check passed; tree changed during the run, not stamped"; fi
