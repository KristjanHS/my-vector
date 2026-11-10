#!/usr/bin/env bash
# Host test of .claude/hooks/gofmt-edited.sh (PostToolUse gofmt) and scripts/tests/gofmt.sh (make check gate) on a throwaway repo
# whose refs/remotes/upstream/main holds one clean and one unformatted "upstream" file.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$here/../../.git")")
tmp=$(mktemp -d "$main/robot/work/gofmt-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: gofmt host test: $*" >&2; exit 1; }
hook=$here/../../.claude/hooks/gofmt-edited.sh gate=$here/gofmt.sh
gofmt=$(command -v gofmt || echo "$(go env GOROOT)/bin/gofmt")
bad=$'package p\n\nvar m = map[string]int{\n"a": 1,\n"bbbbb": 2,\n}\n'
good=$("$gofmt" <<<"$bad")$'\n'
r=$tmp/repo
git init -q "$r"
gc() { git -C "$r" add -A && git -C "$r" -c user.email=t@t -c user.name=t commit -q --allow-empty -m "$1"; }
printf %s "$good" >"$r/clean.go"; printf %s "$bad" >"$r/up_dirty.go"; gc upstream
git -C "$r" update-ref refs/remotes/upstream/main HEAD

# run_hook FILE [ENV...]: feed the hook an Edit payload from an unrelated cwd; sets $out
run_hook() { local f=$1; shift; out=$(cd / && jq -cn --arg f "$f" '{tool_name:"Edit",tool_input:{file_path:$f}}' | env "$@" bash "$hook" 2>&1) || die "hook exit $? on $f: $out"; }
is() { [[ $(<"$1") == "$(printf %s "$2")" ]]; } # both sides lose trailing newlines

# --- hook
printf %s "$bad" >"$r/clean.go"; run_hook "$r/clean.go"
is "$r/clean.go" "$good" && [[ $out == *reformatted* ]] || die "clean-at-HEAD file not formatted: $out"
run_hook "$r/clean.go"; [[ -z $out ]] || die "already formatted file still reported: $out"
printf '%s// edit\n' "$bad" >"$r/up_dirty.go"; run_hook "$r/up_dirty.go"
is "$r/up_dirty.go" "$bad// edit" && [[ -z $out ]] || die "unformatted-at-HEAD (upstream) file touched: $out"
mkdir -p "$r/new dir"; printf %s "$bad" >"$r/new dir/new.go"; run_hook "$r/new dir/new.go"
is "$r/new dir/new.go" "$good" || die "untracked file not formatted: $out"
printf %s "$bad" >"$r/x.go.bak"; run_hook "$r/x.go.bak"; is "$r/x.go.bak" "$bad" || die "non-.go file touched"
printf 'package p\nfunc (\n' >"$r/syn.go"; run_hook "$r/syn.go"; [[ -z $out ]] || die "syntax error not silent: $out"
# gofmt off PATH (as on this host): the hook falls back to $(go env GOROOT)/bin/gofmt
bin=$tmp/bin; mkdir "$bin"
for c in bash env jq git go cat dirname basename sha1sum; do ln -s "$(command -v "$c")" "$bin/$c"; done
printf %s "$bad" >"$r/k.go"; run_hook "$r/k.go" PATH="$bin"; is "$r/k.go" "$good" || die "GOROOT fallback did not format: $out"
git -C "$r" checkout -q -- up_dirty.go; rm -f "$r"/{syn.go,k.go,x.go.bak}; rm -rf "$r/new dir"

# --- gate
gc fork-clean # clean.go = good (unchanged); up_dirty.go is unformatted but upstream-only
"$gate" "$r" >/dev/null || die "gate red on a clean fork with an untouched unformatted upstream file"
printf %s "$bad" >"$r/clean.go" # first sed -i on an upstream file: tracked, modified, unstaged (index still = merge-base)
o=$("$gate" "$r" 2>&1) && die "gate green on an unstaged unformatted edit"; [[ $o == *clean.go* ]] || die "gate did not name clean.go: $o"
git -C "$r" checkout -q -- clean.go
# upstream/main moves ahead (fetched, not merged) and touches up_dirty.go: the merge-base, not upstream/main, is the base
old=$(git -C "$r" rev-parse upstream/main); git -C "$r" checkout -q --detach "$old"
printf '%s// upstream later\n' "$bad" >"$r/up_dirty.go"; gc upstream-ahead
git -C "$r" update-ref refs/remotes/upstream/main HEAD; git -C "$r" checkout -q -
"$gate" "$r" >/dev/null || die "gate red on an unmerged upstream change (diffed against upstream/main, not the merge-base)"
git -C "$r" update-ref refs/remotes/upstream/main "$old"
printf %s "$bad" >"$r/bash_edit.go" # untracked, as a heredoc/redirect leaves a new file
o=$("$gate" "$r" 2>&1) && die "gate green on an unformatted untracked file"; [[ $o == *bash_edit.go* ]] || die "gate did not name it: $o"
printf '%s// fork\n' "$bad" >"$r/up_dirty.go"; rm "$r/bash_edit.go"; gc fork-dirty # fork edits the upstream file, commits it unformatted
git -C "$r" checkout -q upstream/main -- up_dirty.go # working tree back at upstream: only --head HEAD still sees the dirty edit
mkdir "$tmp/arch"; git -C "$r" archive HEAD | tar -x -C "$tmp/arch"
"$gate" "$r" >/dev/null || die "gate red on a working tree that matches upstream"
o=$("$gate" "$r" --head HEAD "$tmp/arch" 2>&1) && die "--head gate green on a commit with an unformatted fork-edited file"
[[ $o == *up_dirty.go* ]] || die "--head gate did not name it: $o"
echo "gofmt hook + gate tests OK"
