#!/usr/bin/env bash
# scripts/tests/cites.sh --head <rev> reads the rev's tree, not the working tree or index: in a scratch repo a dangling cite present at
# the rev but gone from the working tree must FAIL under --head, and one only in the working tree must not; a cited file tracked at the
# rev but deleted from the working tree resolves under --head only. cites.sh's own baseline
# rows never dangle in the scratch repo, so only the scratch cite's FAIL line is asserted on.
set -euo pipefail
dir=$(cd "$(dirname "$0")/../.." && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
g=(git -C "$t" -c user.name=t -c user.email=t@example.invalid -c core.hooksPath=/dev/null -c commit.gpgsign=false)
"${g[@]}" init -q
n=2026-01-01-gone; n+=.md # built at runtime: a literal here would itself be a dangling cite in this repo
nbad=0; fail() { echo "FAIL cites-rev: $*" >&2; nbad=$((nbad + 1)); }
dangle() { grep -cxF "FAIL: cites: a.txt cites $n, which no tracked file is named" <<<"$1" || true; } # 1 = reported
echo "see $n" >"$t/a.txt"; "${g[@]}" add a.txt; "${g[@]}" commit -qm dangle; rev1=$("${g[@]}" rev-parse HEAD)
echo clean >"$t/a.txt" # the dangle is fixed only in the working tree
out=$(bash "$dir/scripts/tests/cites.sh" "$t" --head "$rev1" 2>&1 || true)
[[ $(dangle "$out") == 1 ]] || fail "--head $rev1: the rev's dangling cite was not reported (read the working tree?): $out"
out=$(bash "$dir/scripts/tests/cites.sh" "$t" 2>&1 || true)
[[ $(dangle "$out") == 0 ]] || fail "no rev: the working tree has no dangle, yet it was reported: $out"
"${g[@]}" commit -qam clean; rev2=$("${g[@]}" rev-parse HEAD)
echo "see $n" >"$t/a.txt" # the dangle exists only in the working tree (dirty tree)
out=$(bash "$dir/scripts/tests/cites.sh" "$t" --head "$rev2" 2>&1 || true)
[[ $(dangle "$out") == 0 ]] || fail "--head $rev2: a working-tree-only dangle was reported: $out"
out=$(bash "$dir/scripts/tests/cites.sh" "$t" 2>&1 || true)
[[ $(dangle "$out") == 1 ]] || fail "no rev: the working tree's dangle was not reported: $out"
mkdir "$t/docs"; touch "$t/docs/$n"; "${g[@]}" add -A; "${g[@]}" commit -qm cited; rev3=$("${g[@]}" rev-parse HEAD)
rm "$t/docs/$n" # the cited file is tracked at the rev but deleted from the working tree
out=$(bash "$dir/scripts/tests/cites.sh" "$t" --head "$rev3" 2>&1 || true)
[[ $(dangle "$out") == 0 ]] || fail "--head $rev3: the rev tracks the cited file, yet the cite was reported (tracked files read off the working tree?): $out"
out=$(bash "$dir/scripts/tests/cites.sh" "$t" 2>&1 || true)
[[ $(dangle "$out") == 1 ]] || fail "no rev: the cited file is deleted from the working tree, yet the cite was not reported: $out"
((nbad == 0)) || exit 1
echo "cites-rev OK: --head reads the rev's tree (cites + tracked files), no rev reads the working tree"
