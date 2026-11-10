#!/usr/bin/env bash
# Host test of robot/tailscale/fetch.sh (make check): a fake curl serves a local tgz + .sha256 from $tmp/srv.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
tmp=$(mktemp -d "$main/robot/work/tailscale-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: tailscale fetch test: $*" >&2; exit 1; }

v=0.0.1 name=tailscale_0.0.1_arm
mkdir -p "$tmp/bin" "$tmp/srv" "$tmp/pkg/$name"
printf '#!/bin/sh\necho %s\n' tailscale >"$tmp/pkg/$name/tailscale"
printf '#!/bin/sh\necho %s\n' tailscaled >"$tmp/pkg/$name/tailscaled"
chmod +x "$tmp/pkg/$name/"*
tar -czf "$tmp/srv/$name.tgz" -C "$tmp/pkg" "$name"
sum=$(sha256sum "$tmp/srv/$name.tgz" | awk '{ print $1 }')
echo "$sum" >"$tmp/srv/$name.tgz.sha256"
cat >"$tmp/bin/curl" <<'EOF'
#!/bin/sh
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift 2 ;; -*) shift ;; *) url=$1; shift ;; esac; done
echo "$url" >>"$FAKE_SRV/../calls"
cp "$FAKE_SRV/${url##*/}" "$out"
EOF
chmod +x "$tmp/bin/curl"
run() { # work-dir sha
  PATH="$tmp/bin:$PATH" FAKE_SRV=$tmp/srv TS_VERSION=$v TS_SHA256=$2 TS_WORK=$1 "$here/../fetch.sh" 2>&1
}

w=$tmp/bad
if out=$(run "$w" "${sum//?/0}"); then die "pin mismatch passed: $out"; fi
[[ $out == *FAIL* ]] || die "pin mismatch out: $out"
[[ ! -e $w/tailscale && ! -e $w/tailscaled && ! -e $w/$name.tgz ]] || die "pin mismatch left files: $(ls "$w")"

w=$tmp/pub
cp "$tmp/srv/$name.tgz.sha256" "$tmp/good.sha256"; echo "${sum//?/1}" >"$tmp/srv/$name.tgz.sha256"
if out=$(run "$w" "$sum"); then die "published .sha256 mismatch passed: $out"; fi
[[ ! -e $w/tailscaled ]] || die "published mismatch unpacked tailscaled"
cp "$tmp/good.sha256" "$tmp/srv/$name.tgz.sha256"

w=$tmp/ok
out=$(run "$w" "$sum") || die "good tgz failed: $out"
[[ $("$w/tailscale") == tailscale && $("$w/tailscaled") == tailscaled ]] || die "binaries not unpacked: $(ls "$w")"
: >"$tmp/calls"
out=$(run "$w" "$sum") || die "2nd run failed: $out"
[[ $(grep -c '\.tgz$' "$tmp/calls") == 0 ]] || die "2nd run re-downloaded the tgz"
echo "tailscale fetch test OK"
