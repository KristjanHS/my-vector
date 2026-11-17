#!/usr/bin/env bash
# Cite guard: every dated doc name (YYYY-MM-DD-<name>.md) cited in a tracked text file names a tracked file still present in the
# working tree (matched by basename, any directory). Known dangles sit in the baseline as "<citing path> <sha1 of the cited name, 12 hex>"
# (a hash, so the baseline never re-cites the name); a baseline row that no longer dangles fails too, so the baseline only shrinks.
# Usage: cites.sh [repo]   default repo = this checkout.
set -euo pipefail
repo=${1:-$(cd "$(dirname "$0")/../.." && pwd)}
g=(git -C "$repo")
baseline='chipper/pkg/myvector/usage/usage.go 19eaa6923470
chipper/pkg/myvector/usagepoll/usagepoll.go 19eaa6923470'
pat='[0-9]{4}-[0-9]{2}-[0-9]{2}-[[:alnum:]_.-]+\.md'
declare -A have=()
while IFS= read -r f; do have[${f##*/}]=1; done < <(LC_ALL=C comm -23 <("${g[@]}" ls-files | LC_ALL=C sort) <("${g[@]}" ls-files --deleted | LC_ALL=C sort))
declare -A seen=()
fail=0
while IFS= read -r hit; do
  f=${hit%%:*} n=${hit#*:}
  [[ -z ${have[$n]:-} ]] || continue
  key="$f $(printf %s "$n" | sha1sum | cut -c1-12)"
  seen[$key]=1
  grep -qxF "$key" <<<"$baseline" || { echo "FAIL: cites: $f cites $n, which no tracked file is named" >&2; fail=1; }
done < <("${g[@]}" grep -IoE "$pat" | LC_ALL=C sort -u)
while IFS= read -r row; do
  [[ -n ${seen[$row]:-} ]] || { echo "FAIL: cites: baseline row '$row' no longer dangles: drop it" >&2; fail=1; }
done <<<"$baseline"
((fail == 0)) || exit 1
echo "cites guard OK"
