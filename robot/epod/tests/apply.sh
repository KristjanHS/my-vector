#!/usr/bin/env bash
# Host test of robot/epod/apply.sh's robot half (make check): a temp ETC_ROOT, the lib prepended, under sh -e; a fake systemctl logs calls.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/epod-apply-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: epod apply test: $*" >&2; exit 1; }
body=$(sed -n "/<<'EOF' |/,/^EOF\$/{/<<'EOF' |/d;/^EOF\$/d;p}" "$robot/epod/apply.sh")
pem() { printf -- '-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n' "$1"; }
ours=$(pem OURS) old=$(pem OLD)
CA=$(printf '%s\n' "$ours" | base64 -w0)
fresh() { mkdir -p "$tmp/$1/data/data" "$tmp/$1/anki/etc"; echo "$tmp/$1"; }
mkdir "$tmp/bin"
printf '#!/bin/sh\necho "systemctl $*" >>"$ETC_ROOT/calls"\n' >"$tmp/bin/systemctl"
chmod +x "$tmp/bin/systemctl"
run() { (cat "$robot/lib/etc.sh"; echo "$body") | CA=$CA ETC_ROOT=$1 PATH="$tmp/bin:$PATH" sh -e; }
calls() { cat "$1/calls" 2>/dev/null || :; }
trust=data/data/wirepod-cert.crt

r=$(fresh dual); printf '%s\n' "$old" >"$r/$trust"
out1=$(run "$r") || die "dual 1st run: $out1"
[[ $(cat "$r/$trust") == "$ours"$'\n'"$old" ]] || die "dual: trust = [$(cat "$r/$trust")], want ours + old"
[[ $(calls "$r") == 'systemctl restart anki-robot.target' ]] || die "dual 1st run: systemctl calls [$(calls "$r")], want one anki-robot.target restart"
sum=$(sha256sum "$r/$trust")
out2=$(run "$r") || die "dual 2nd run: $out2"
[[ $out1 == "epod: wrote /$trust (dual, 2 certs), restarted anki-robot.target" && $out2 == "epod: already installed (dual)" ]] ||
  die "dual runs: 1st=[$out1] 2nd=[$out2]"
[[ $(sha256sum "$r/$trust") == "$sum" ]] || die "dual 2nd run changed the trust file (dedup)"
[[ $(calls "$r") == 'systemctl restart anki-robot.target' ]] || die "dual 2nd run (unchanged) restarted: [$(calls "$r")]"

r=$(fresh ours); printf '%s\n' "$old" >"$r/$trust"; echo '# my-vector.PodTrust' >"$r/anki/etc/vic-cloud.env"
out=$(run "$r") || die "marker run: $out"
[[ $(cat "$r/$trust") == "$ours" ]] || die "marker: trust = [$(cat "$r/$trust")], want ours only"
[[ $(calls "$r") == 'systemctl restart anki-robot.target' ]] || die "marker run: systemctl calls [$(calls "$r")]"

r=$(fresh anki); printf '%s\n' "$old" >"$r/$trust"; : >"$r/anki/etc/wirepod-cert.crt"
if out=$(run "$r"); then die "/anki/etc/wirepod-cert.crt present but passed: $out"; fi
[[ $(cat "$r/$trust") == "$old" && -z $(calls "$r") ]] || die "/anki/etc copy present: trust file was written or the stack restarted"
echo "epod apply host test PASS"
