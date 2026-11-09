#!/usr/bin/env bash
# Fast checks (make check, syncwt's gate): upstream drift, robot/scripts shell syntax, wire-pod-guard.nft syntax (nft -c in a user+net namespace), then in parallel (scripts/lib/check-runner.sh: first failure stops the rest) scripts/tests/{guard,check-runner}.sh, firmware static checks (check-static.sh), patch 0010 + 0011 + 0016 + 0017 host tests (tests/pc-voice.sh, tests/nap.sh, tests/mic-stream.sh, tests/silent-start.sh), robot/lib/tests/etc.sh + robot/{sshkey,hostkey}/tests/apply.sh + robot/epod/tests/{mkcert,apply,interlock}.sh + robot/netguard/tests/{rules,hosts,apply}.sh + scripts/tests/secrets.sh, go vet + Fork tests of the patched sdkapp,
# chipper myvector + vars + localization fork Go tests (when that dir exists: go test exits 1 on a missing pattern dir), tts + stt pytest (when each venv exists).
# Usage: check.sh [--head [rev]]   --head checks only that commit (default HEAD) via a git archive copy;
#   make ship passes its pinned sha so the checked commit is the one it ships.
# Green writes a pass stamp robot/work/check-pass/<key>, key = content tree + toolchain + clone (stamp_key); --head or a clean working tree skips on a stamped key, CHECK_FULL=1 runs all.
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
dir=$repo head=() tmp=()
trap 'rm -rf "${tmp[@]}"' EXIT
if [[ ${1:-} == --head ]]; then
  head=(--head "$(git -C "$repo" rev-parse --verify "${2:-HEAD}^{commit}")")
  tree=$(git -C "$repo" rev-parse "${head[1]}^{tree}") key=$(stamp_key "$tree")
  dir=$(mktemp -d "$repo/robot/work/check-head.XXXXXX") tmp+=("$dir")
  git -C "$repo" archive "${head[1]}" | tar -x -C "$dir"
else tree=$(wt_tree) key=$(stamp_key "$tree")
fi
(cd "$repo" && scripts/upstream-drift.sh "${head[@]}")
# A stamp hit still runs the commit-range checks (drift above, secrets in every unpushed commit): the key holds only the tree.
if [[ -z ${CHECK_FULL:-} && -f $stamps/$key ]] && [[ -n ${head[*]} || $tree == "$(git -C "$repo" rev-parse 'HEAD^{tree}')" ]]; then
  if [[ -f $dir/scripts/tests/secrets.sh ]]; then "$dir"/scripts/tests/secrets.sh "$repo" "${head[@]}" ${head[0]:+"$dir"}; fi
  echo "check skipped: tree ${tree:0:12} passed already ($(<"$stamps/$key")); drift + secrets ran"; exit 0
fi
shopt -s nullglob # a pre-R6 commit has no robot/*.sh
for f in "$dir"/robot/*.sh "$dir"/robot/*/*.sh "$dir"/robot/*/tests/*.sh "$dir"/scripts/*.sh "$dir"/scripts/lib/*.sh "$dir"/scripts/tests/*.sh "$dir"/tts/*.sh "$dir"/et-stt/*.sh; do bash -n "$f" || { echo "syntax: ${f#"$dir"/}" >&2; exit 1; }; done
echo "shell syntax OK"
. "$repo/scripts/lib/check-runner.sh"
opt() { if [[ -f $2 ]]; then job "$@"; fi; } # a test an older --head commit lacks is skipped
gotest() { cd "$dir/chipper" && go test "$@"; }
if [[ -f $dir/scripts/wire-pod-guard.nft ]] && unshare -rn true 2>/dev/null; then
  unshare -rn /usr/sbin/nft -c -f "$dir/scripts/wire-pod-guard.nft" || { echo "nft syntax: scripts/wire-pod-guard.nft" >&2; exit 1; }
  echo "nft guard syntax OK"
  job nft-guard "$dir"/scripts/tests/guard.sh
elif [[ -f $dir/scripts/wire-pod-guard.nft ]]; then echo "nft guard check skipped: no unprivileged unshare"; fi
job check-static "$dir"/robot/firmware/check-static.sh
for t in pc-voice nap silent-start mic-stream night-check; do opt "$t" "$dir/robot/firmware/tests/$t.sh"; done # each SKIPs itself without g++/clone
opt lib-etc "$dir"/robot/lib/tests/etc.sh
opt sshkey "$dir"/robot/sshkey/tests/apply.sh
opt hostkey "$dir"/robot/hostkey/tests/apply.sh
for t in mkcert apply interlock; do opt "epod-$t" "$dir/robot/epod/tests/$t.sh"; done
for t in rules hosts apply; do opt "netguard-$t" "$dir/robot/netguard/tests/$t.sh"; done
opt secrets "$dir"/scripts/tests/secrets.sh "$repo" "${head[@]}" ${head[0]:+"$dir"}
opt quiet-guard "$dir"/robot/quiet/tests/guard.sh
opt quiet-scan "$dir"/robot/quiet/tests/scan.sh
opt check-runner "$dir"/scripts/tests/check-runner.sh
job go-sdkapp sh -c 'cd "$1/chipper" && go vet ./pkg/wirepod/sdkapp/ && go test -run Fork ./pkg/wirepod/sdkapp/' sh "$dir"
if [[ -d $dir/chipper/pkg/myvector ]]; then job go-myvector gotest ./pkg/myvector/...; fi
if [[ -f $dir/chipper/pkg/vars/fork_intents_test.go ]]; then job go-vars gotest -run Fork ./pkg/vars/; fi
job go-ttr gotest -run Fork ./pkg/wirepod/ttr/
if [[ -f $dir/chipper/pkg/wirepod/localization/fork_lang_test.go ]]; then job go-localization gotest -run Fork ./pkg/wirepod/localization/; fi
job go-preqs gotest -tags nolibopusfile -run Fork ./pkg/wirepod/preqs/
if [[ -d $dir/tts/tests && -x $tts_py ]]; then job tts-pytest "$tts_py" -m pytest -q -p no:cacheprovider "$dir/tts/tests"
elif [[ -d $dir/tts/tests ]]; then echo "tts tests skipped: no venv (make tts-setup)"; fi
if [[ -d $dir/et-stt/tests && -x $stt_py ]]; then job stt-pytest "$stt_py" -m pytest -q -p no:cacheprovider "$dir/et-stt/tests"
elif [[ -d $dir/et-stt/tests ]]; then echo "stt tests skipped: no venv (make stt-setup)"; fi
logs=$(mktemp -d "$main/robot/work/check-logs.XXXXXX") tmp+=("$logs")
echo "running ${#_cr_names[@]} checks in parallel"
run_jobs "$logs" || exit 1
# Stamp only a tree that did not change while the checks ran; stamps older than 30 days are pruned.
if [[ -n ${head[*]} || $(wt_tree) == "$tree" ]]; then
  mkdir -p "$stamps"; find "$stamps" -type f -mtime +30 -delete
  echo "${head[1]:-working tree} at $(date '+%F %T')" >"$stamps/$key"
  echo "check passed: stamped tree ${tree:0:12}"
else echo "check passed; tree changed during the run, not stamped"; fi
