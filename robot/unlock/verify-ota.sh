#!/usr/bin/env bash
# Downloads (if missing) and verifies an OTA in robot/ota/ before the robot ever sees it.
#   unlock: Unlock-Prod.ota against its known sha256.
#   wireos: dev.ota has no published hash, so a second download must match the first.
set -euo pipefail
ota=$(cd "$(dirname "$0")/../ota" && pwd)

unlock_url=http://websetup.skittle.dev:8000/static/firmware/utility/Unlock-Prod.ota
unlock_sha=0765574a6f3a9b65ff2d3d4181f9fc0eea800c810468f480e8b822b73d38110b # 175349760 bytes, 2026-11-03
wireos_url=http://ota.pvic.xyz/vic/latest/dev.ota
wireos_size=163686400 # 2026-11-03; a new "latest" fails here on purpose: review it, then update the pin

fetch() { # url dest
  mkdir -p "$(dirname "$2")"
  curl -fL --retry 3 -o "$2.part" "$1"
  mv "$2.part" "$2"
}

# An OTA is a tar holding manifest.ini plus the boot/system images.
check_tar() {
  tar -tf "$1" | grep -qx manifest.ini || { echo "FAIL: $1 has no manifest.ini" >&2; exit 1; }
}

case ${1:-} in
  unlock)
    f=$ota/unlock-prod/Unlock-Prod.ota
    [[ -f $f ]] || fetch "$unlock_url" "$f"
    got=$(sha256sum "$f" | cut -d' ' -f1)
    [[ $got == "$unlock_sha" ]] || { echo "FAIL: $f sha256 $got, want $unlock_sha (delete it and re-run)" >&2; exit 1; }
    check_tar "$f"
    echo "OK unlock: $f sha256 $got" ;;
  wireos)
    f=$ota/wireos-dev/dev.ota
    [[ -f $f ]] || fetch "$wireos_url" "$f"
    fetch "$wireos_url" "$f.check"
    a=$(sha256sum "$f" | cut -d' ' -f1)
    b=$(sha256sum "$f.check" | cut -d' ' -f1)
    rm -f "$f.check"
    [[ $a == "$b" ]] || { echo "FAIL: two downloads differ ($a vs $b); delete $f and re-run" >&2; exit 1; }
    check_tar "$f"
    size=$(stat -c %s "$f")
    [[ $size == "$wireos_size" ]] || { echo "FAIL: $f is $size bytes, pinned $wireos_size (new release?)" >&2; exit 1; }
    tar -xOf "$f" manifest.ini | grep -qx 'ankidev=1' || { echo "FAIL: $f manifest is not ankidev=1" >&2; exit 1; }
    echo "OK wireos: $f sha256 $a, $size bytes, ankidev=1" ;;
  *)
    echo "usage: $0 unlock|wireos" >&2; exit 2 ;;
esac
