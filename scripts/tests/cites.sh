#!/usr/bin/env bash
# Cite guard: every dated doc name (YYYY-MM-DD-<name>.md) cited in a tracked text file names a tracked file still present in the
# working tree (matched by basename, any directory); with --head, both sides are read from that commit's tree instead.
# Known dangles sit in the baseline as "<citing path> <sha1 of the cited name, 12 hex>"
# (a hash, so the baseline never re-cites the name); a baseline row that no longer dangles fails too, so the baseline only shrinks.
# Usage: cites.sh [repo] [--head rev [tree]]   default repo = this checkout; tree (check.sh's archive copy) is unused: git reads the rev.
set -euo pipefail
repo=${1:-$(cd "$(dirname "$0")/../.." && pwd)}
g=(git -C "$repo")
rev=''
if [[ ${2:-} == --head ]]; then rev=${3:?cites.sh: --head needs a rev}; fi
baseline='chipper/pkg/myvector/usage/usage.go 19eaa6923470
chipper/pkg/myvector/usagepoll/usagepoll.go 19eaa6923470'
pat='[0-9]{4}-[0-9]{2}-[0-9]{2}-[[:alnum:]_.-]+\.md'
declare -A have=()
tracked() { # the tracked paths: the rev's tree, else the index minus files deleted from the working tree
  if [[ -n $rev ]]; then "${g[@]}" ls-tree -r --name-only "$rev"
  else LC_ALL=C comm -23 <("${g[@]}" ls-files | LC_ALL=C sort) <("${g[@]}" ls-files --deleted | LC_ALL=C sort); fi
}
while IFS= read -r f; do have[${f##*/}]=1; done < <(tracked)
declare -A seen=()
fail=0
while IFS= read -r hit; do
  hit=${hit#"${rev:+$rev:}"} # git grep <rev> prefixes each hit with "<rev>:"
  f=${hit%%:*} n=${hit#*:}
  [[ -z ${have[$n]:-} ]] || continue
  key="$f $(printf %s "$n" | sha1sum | cut -c1-12)"
  seen[$key]=1
  grep -qxF "$key" <<<"$baseline" || { echo "FAIL: cites: $f cites $n, which no tracked file is named" >&2; fail=1; }
done < <("${g[@]}" grep -IoE "$pat" ${rev:+"$rev"} | LC_ALL=C sort -u)
while IFS= read -r row; do
  [[ -n ${seen[$row]:-} ]] || { echo "FAIL: cites: baseline row '$row' no longer dangles: drop it" >&2; fail=1; }
done <<<"$baseline"
((fail == 0)) || exit 1
echo "cites guard OK"
