#!/usr/bin/env bash
# Host test (make check): a failing feed command makes each gate script die, never loop zero times and pass.
# Sites: check-static.sh series/markers/built-JSON git diff, apply.sh series, live-check.sh picker + ttsdown voice reads.
# Usage: static-feed.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
repo=$(cd "$fw/../.." && pwd)
die() { echo "FAIL: static-feed test: $*" >&2; exit 1; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
mkdir -p "$main/robot/work"
tmp=$(mktemp -d "$main/robot/work/static-feed-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# Stubs: git answers only what the scripts ask (STUB_DIFF_FAIL fails `diff`); nm fakes the SDK libm + bridged se_lib;
# curl serves the wire-pod API from STUB_VOICE (`fail` = HTTP error).
bin=$tmp/bin
mkdir -p "$bin"
cat >"$bin/git" <<'EOF'
#!/usr/bin/env bash
[[ $1 != -C ]] || shift 2
case $1 in
  show) [[ $2 != tipsha:* ]] || echo MARKER_ONE ;;
  diff) [[ -z ${STUB_DIFF_FAIL:-} ]] || exit 128 ;;
  cat-file | check-ignore) ;;
  rev-parse) [[ $2 == --absolute-git-dir ]] || exit 1; echo "$STUB_GITDIR" ;;
  *) exit 1 ;;
esac
EOF
cat >"$bin/nm" <<'EOF'
#!/usr/bin/env bash
case $1 in
  -D) printf '00000000 T sin\n00000000 T __isinff\n' ;;
  -A) printf 'se.a:math_abi_bridge.o:00000000 T bridge_sin\n' ;;
esac
EOF
cat >"$bin/curl" <<'EOF'
#!/usr/bin/env bash
case ${*: -1} in
  */myvector/api/voice) [[ ${STUB_VOICE:-} != fail ]] || exit 22; echo "${STUB_VOICE:-}" ;;
  */llm/models) echo '{"ollama":{"models":[]},"claude":{"models":[{"id":"claude-haiku-5-5"}]}}' ;;
  */llm/test) cat >/dev/null; echo '{"reply":"OK","ms":1}' ;;
  *) echo '{}' ;;
esac
EOF
chmod +x "$bin"/*

# run <want-exit> <want-output-ERE> <cmd…>: cmd under the stubs; exit status and combined output must match.
run() {
  local want=$1 re=$2 out rc=0
  shift 2
  out=$(PATH="$bin:$PATH" "$@" 2>&1) || rc=$?
  [[ $rc == "$want" ]] || die "$*: exit $rc, want $want: $out"
  [[ $out =~ $re ]] || die "$*: output lacks /$re/: $out"
}

# check-static.sh fixture: one patch with a src and a bin marker, a stub apply.sh, a built tree, an SDK sysroot.
f=$tmp/cs/robot/firmware src=$tmp/victor
mkdir -p "$f" "$src/.git" "$src/_build/vicos/Release/bin" "$src/anki-deps/vicos-sdk/dist/6.0.0-r08/sysroot/usr/lib"
cp "$fw/check-static.sh" "$f/"
printf '#!/usr/bin/env bash\npin=pinsha\necho tipsha\n' >"$f/apply.sh"
chmod +x "$f/apply.sh"
echo 0001-a.patch >"$f/series"
: >"$f/0001-a.patch"
printf '0001-a.patch\tsrc\tfoo.c\tMARKER_ONE\n0001-a.patch\tbin\tbin/x\tMARKER_ONE\n' >"$f/markers.tsv"
echo MARKER_ONE >"$src/_build/vicos/Release/bin/x"
: >"$src/anki-deps/vicos-sdk/dist/6.0.0-r08/sysroot/usr/lib/libm.so.6"
: >"$src/anki-deps/vicos-sdk/dist/6.0.0-r08/sysroot/usr/lib/libc.so.6"
export VICTOR_SRC=$src

run 0 'built JSON = patched \(0\)' "$f/check-static.sh" --built
STUB_DIFF_FAIL=1 run 1 'FAIL: git diff pinsha\.\.tipsha failed' "$f/check-static.sh" --built
mv "$f/markers.tsv" "$f/markers.keep" && mkdir "$f/markers.tsv"
run 1 'FAIL: cannot read .*/markers\.tsv' "$f/check-static.sh"
rmdir "$f/markers.tsv" && mv "$f/markers.keep" "$f/markers.tsv"
rm "$f/series" && mkdir "$f/series"
run 1 'FAIL: cannot read .*/series' "$f/check-static.sh"

# apply.sh --check with an unreadable series.
a=$tmp/ap/robot/firmware
mkdir -p "$a/series"
cp "$fw/apply.sh" "$a/"
STUB_GITDIR=$src/.git run 1 'FAIL: cannot read .*/series' "$a/apply.sh" --check

# live-check.sh: picker on a voice API that is not JSON; ttsdown on a voice API that errors.
l=$tmp/lc
mkdir -p "$l/scripts" "$l/robot/quiet" "$l/secrets/server/jdocs"
cp "$repo/scripts/live-check.sh" "$l/scripts/"
cp "$repo/robot/quiet/guard.sh" "$l/robot/quiet/"
: >"$l/secrets/server/apiConfig.json"
echo '{"robots":[{"esn":"00000000"}]}' >"$l/secrets/server/jdocs/botSdkInfo.json"
export MYVECTOR_QUIET_FILE=$tmp/no-quiet
STUB_VOICE='{"llm":{},"defaults":{}}' run 0 'ok   test en-US claude claude-haiku-5-5' "$l/scripts/live-check.sh" picker
STUB_VOICE='<html>' run 1 'FAIL voice API: saved profiles unreadable' "$l/scripts/live-check.sh" picker
STUB_VOICE=fail run 2 'SETUP: wire-pod not up' "$l/scripts/live-check.sh" ttsdown
echo "static-feed OK (6 feed sites die on a failed feed)"
