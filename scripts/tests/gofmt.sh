#!/usr/bin/env bash
# gofmt gate: every Go file the fork adds or changes vs the upstream merge-base (working tree: untracked .go too) is gofmt-clean.
# Upstream files the fork never touched are not checked. Catches what .claude/hooks/gofmt-edited.sh can't: Bash/sed edits,
# and a fork file committed unformatted (the hook then skips it on every later edit).
# Usage: gofmt.sh [repo] [--head rev tree]   default repo = this checkout; --head checks that commit (tree = its archive copy).
set -euo pipefail
repo=${1:-$(cd "$(dirname "$0")/../.." && pwd)}
g=(git -C "$repo" -c core.quotePath=false) # non-ASCII paths unquoted for gofmt
gofmt=$(command -v gofmt || echo "$(go env GOROOT)/bin/gofmt")
rev='' dir=$repo
if [[ ${2:-} == --head ]]; then rev=$3 dir=$4; fi
base=$("${g[@]}" merge-base "${rev:-HEAD}" "${UPSTREAM_REF:-upstream/main}")
files=$("${g[@]}" diff --name-only --no-renames --diff-filter=AM "$base" ${rev:+"$rev"} -- '*.go')
if [[ -z $rev ]]; then files+=$'\n'$("${g[@]}" ls-files --others --exclude-standard -- '*.go'); fi
mapfile -t list < <(grep -v '^$' <<<"$files" | sort -u)
((${#list[@]})) || { echo "gofmt OK: no fork Go files"; exit 0; }
bad=$(cd "$dir" && "$gofmt" -l -- "${list[@]}")
[[ -z $bad ]] || { echo "FAIL: gofmt: not gofmt-clean (gofmt -w them): $bad" >&2; exit 1; }
echo "gofmt OK: ${#list[@]} fork Go files"
