#!/usr/bin/env bash
# Clones wire-os-victor into the gitignored robot/work/ and points its branch my-vector at pin + one commit per patch in series.
# Commits are built off a temp index with the patch's own dates (same SHAs every run); the checkout rewrites only differing files.
# Usage: apply.sh [--export]   --export writes my-vector's commits back out as *.patch + series (after editing them in the clone).
# Then build + deploy from that checkout: ./build/build-v.sh, then ./build/deploy-v.sh (needs Docker).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${VICTOR_SRC:-$(cd "$here/../work" && pwd)/wire-os-victor}
url=https://github.com/os-vector/wire-os-victor
# = anki/victor of wire-os tag 3.0.2.36 (dev.ota 3.0.2.36d, compat 209); bump it whenever dev.ota is bumped.
# deploy.sh refuses a VICTOR_COMPAT_VERSION that differs from the robot's /etc/victor-compat-version.
pin=ebf38c2871a0a69b3ac1a6fd592bb597b8e57724
br=my-vector
die() { echo "FAIL: $*" >&2; exit 1; }
g() { git -C "$src" "$@"; }

[[ -d $src/.git ]] || git clone --recurse-submodules "$url" "$src"
g cat-file -e "$pin^{commit}" 2>/dev/null || g fetch origin "$pin"

if [[ ${1:-} == --export ]]; then
  g rev-parse -q --verify "refs/heads/$br" >/dev/null || die "no $br branch in $src"
  rm -f "$here"/*.patch
  g format-patch --zero-commit --no-signature --no-numbered -o "$here" "$pin..$br" | xargs -n1 basename >"$here/series"
  echo "OK: exported $(wc -l <"$here/series") patches from $br to $here"
  exit 0
fi
[[ -z ${1:-} ]] || die "unknown arg: $1 (only --export)"

tmp=$(mktemp -d "$(g rev-parse --absolute-git-dir)/my-vector-apply.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
export GIT_INDEX_FILE=$tmp/index
g read-tree "$pin"
field() { sed -n "s/^$1: //p" "$tmp/info"; }
tip=$pin
while read -r p; do
  [[ -n $p ]] || continue
  git mailinfo "$tmp/msg" "$tmp/diff" <"$here/$p" >"$tmp/info"
  [[ -n $(field Author) && -n $(field Date) ]] || die "$p is not a format-patch file (no Author/Date)"
  g apply --cached "$tmp/diff" || die "$p does not apply on top of pin + the patches before it"
  tip=$({ field Subject; [[ ! -s $tmp/msg ]] || { echo; cat "$tmp/msg"; }; } |
    GIT_AUTHOR_NAME=$(field Author) GIT_AUTHOR_EMAIL=$(field Email) GIT_AUTHOR_DATE=$(field Date) \
    GIT_COMMITTER_NAME=$(field Author) GIT_COMMITTER_EMAIL=$(field Email) GIT_COMMITTER_DATE=$(field Date) \
    git -C "$src" commit-tree "$(g write-tree)" -p "$tip")
done <"$here/series"
unset GIT_INDEX_FILE

untracked=$(g ls-files --others --exclude-standard)
if [[ $(g rev-parse HEAD) == "$tip" && $(g symbolic-ref -q HEAD) == "refs/heads/$br" ]] &&
  g diff --quiet HEAD && g diff --cached --quiet && [[ -z $untracked ]]; then
  echo "OK: $br already = pin + $(grep -c . "$here/series") patches in $src"
elif g diff --quiet "$tip" && [[ -z $untracked ]]; then
  # Files already hold the series (a fresh export, or patches applied by hand): move the branch, touch no file.
  g update-ref "refs/heads/$br" "$tip"
  g symbolic-ref HEAD "refs/heads/$br"
  g reset -q
  echo "OK: $br -> ${tip:0:12}, no file rewritten"
elif g diff --quiet HEAD && g diff --cached --quiet && [[ -z $untracked ]]; then
  old=$(g rev-parse HEAD)
  g checkout -q -B "$br" "$tip"
  echo "OK: $br -> ${tip:0:12}, checked out the files that differ"
  [[ $old == "$pin" ]] || echo "  was ${old:0:12}; to keep it: git -C $src branch rescue $old"
else
  die "$src has edits beyond pin + series (another session in flight?): commit them on $br and run apply.sh --export, or discard them"
fi
g submodule update -q --init --recursive
echo "next: ./build/build-v.sh then ./build/deploy-v.sh in $src"
