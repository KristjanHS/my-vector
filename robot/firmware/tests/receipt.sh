#!/usr/bin/env bash
# Host test of robot/firmware/receipt.sh + its ship.sh wiring (make check): tags in a throwaway repo; the body never
# carries a dotted quad; the receipt step sits after every smoke die and before the push (a failed ship creates no tag).
# Usage: receipt.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fw=$(dirname "$here")
die() { echo "FAIL: receipt test: $*" >&2; exit 1; }
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
mkdir -p "$main/robot/work"
tmp=$(mktemp -d "$main/robot/work/receipt-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
# shellcheck source=robot/firmware/receipt.sh
. "$fw/receipt.sh"

[[ $(receipt_link 100.101.2.3) == tailnet ]] || die "100.101.2.3 is not tailnet"
[[ $(receipt_link 100.64.0.1) == tailnet && $(receipt_link 100.127.255.1) == tailnet ]] || die "100.64/10 edges are not tailnet"
for lan in 100.63.0.1 100.128.0.1 192.168.1.20 10.0.0.5; do [[ $(receipt_link "$lan") == lan ]] || die "$lan is not lan"; done
if receipt_body "$(printf %040d 0)" 1.2.3.4 unchanged server lan >/dev/null 2>&1; then die "a body with a dotted quad was accepted"; fi

r=$tmp/repo
git init -q "$r"
git -C "$r" -c user.name=t -c user.email=t@example.invalid commit -q --allow-empty -m one
c=$(git -C "$r" rev-parse HEAD)
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
srv=$(printf %040d 7)
tag=$(receipt_tag "$r" "$c" "$srv" unchanged server 100.101.2.3) || die "receipt_tag failed"
[[ $tag =~ ^ship/[0-9]{8}-[0-9]{4}-${c:0:7}$ ]] || die "tag name '$tag'"
[[ $(git -C "$r" cat-file -t "$tag") == tag ]] || die "$tag is not annotated"
[[ $(git -C "$r" rev-parse "$tag^{commit}") == "$c" ]] || die "$tag does not point at the shipped commit"
body=$(git -C "$r" tag -l --format='%(contents)' "$tag")
grep -qxF "ship ${c:0:7}" <<<"$body" || die "heading is not the shipped commit: $body"
for k in "server $srv" "fw-tree unchanged" "smoke server PASS" "link tailnet"; do grep -qxF "$k" <<<"$body" || die "body lacks '$k': $body"; done
if [[ $body =~ [0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ ]]; then die "body carries an address: $body"; fi

# Wiring: receipt_tag runs after the last smoke die and before the push; no die between it and the push stops on the tag.
ship=$fw/ship.sh
ln_of() { grep -nE "$1" "$ship" | grep -v '^[0-9]*:[[:space:]]*#' | cut -d: -f1; }
tag_ln=$(ln_of 'receipt_tag ' | tail -1)
[[ $(ln_of 'receipt_tag ' | wc -l) == 1 ]] || die "ship.sh calls receipt_tag more than once"
smoke_ln=$(ln_of 'smoke\.sh.*\|\| die' | tail -1)
# shellcheck disable=SC2016 # a literal $repo in the pattern
push_ln=$(ln_of 'git -C "\$repo" push ')
[[ -n $tag_ln && -n $smoke_ln && -n $push_ln ]] || die "ship.sh lacks receipt_tag, a smoke die or the push"
((smoke_ln < tag_ln && tag_ln < push_ln)) || die "receipt_tag at $tag_ln is not between the last smoke die ($smoke_ln) and the push ($push_ln)"
grep -q "refs/tags/\$tag" <<<"$(sed -n "${push_ln}p" "$ship")" || die "the push does not carry the receipt tag"

# Ship-window wiring (wire-pod's usage poll): ship-active is written right where ship_start is set, and removed only
# by cleanup, after the window line is appended (the poll holds its diff while the marker exists).
start_ln=$(ln_of '^ship_start=\$\(date')
# shellcheck disable=SC2016 # a literal $repo in the pattern
active_ln=$(ln_of '>"\$repo/robot/work/ship-active"')
[[ -n $start_ln && $active_ln == "$((start_ln + 1))" ]] || die "ship.sh does not write ship-active right after ship_start (lines $start_ln/$active_ln)"
[[ $(ln_of 'rm -f[^;]*ship-active' | wc -l) == 1 ]] || die "ship.sh removes ship-active other than once"
# shellcheck disable=SC2016 # a literal $repo in the pattern
grep -qE 'ship-windows\.tsv";[[:space:]]*rm -f "\$repo/robot/work/ship-active"' <<<"$(sed -n "$(ln_of 'rm -f[^;]*ship-active')p" "$ship")" ||
  die "cleanup does not remove ship-active right after appending the ship window"
echo "ok: receipt (link, no address, annotated tag, after smoke + before push; ship-active around the window)"
