#!/usr/bin/env bash
# Downloads the pinned static arm Tailscale tgz into <main>/robot/work/tailscale/ and unpacks tailscale + tailscaled there (D2).
# Refuses (exit 1, nothing unpacked) unless the tgz and its published .sha256 both equal TS_SHA256.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
TS_VERSION=${TS_VERSION:-1.104.5}
TS_SHA256=${TS_SHA256:-39e2bdf0042f38bf49beec357375c11c711f9c60f8e7242f385b5616c63dcd6e} # tailscale_1.104.5_arm.tgz, 2026-11-10
work=${TS_WORK:-$main/robot/work/tailscale}
name=tailscale_${TS_VERSION}_arm
url=https://pkgs.tailscale.com/stable/$name.tgz
mkdir -p "$work"
tgz=$work/$name.tgz

fetch() { # url dest
  curl -fL --retry 3 -o "$2.part" "$1"
  mv "$2.part" "$2"
}

[[ -s $tgz ]] || fetch "$url" "$tgz"
fetch "$url.sha256" "$tgz.sha256"
pub=$(awk '{ print $1; exit }' "$tgz.sha256")
got=$(sha256sum "$tgz" | awk '{ print $1 }')
if [[ $pub != "$TS_SHA256" || $got != "$TS_SHA256" ]]; then
  rm -f "$tgz"
  echo "FAIL: tailscale fetch: $name.tgz sha256 $got, published $pub, pinned $TS_SHA256" >&2
  exit 1
fi
x=$(mktemp -d "$work/unpack.XXXXXX")
trap 'rm -rf "$x"' EXIT
tar -xzf "$tgz" -C "$x" "$name/tailscale" "$name/tailscaled"
mv -f "$x/$name/tailscale" "$x/$name/tailscaled" "$work/"
echo "tailscale $TS_VERSION: $work/{tailscale,tailscaled}"
