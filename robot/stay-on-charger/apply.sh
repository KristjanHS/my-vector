#!/usr/bin/env bash
# Clones wire-os-victor into the gitignored robot/work/ at the pinned commit and applies the stay-docked patch.
# Then build + deploy from that checkout: ./build/build-v.sh, then ./build/deploy-v.sh (needs Docker).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../work" && pwd)/wire-os-victor
url=https://github.com/os-vector/wire-os-victor
# = anki/victor of wire-os tag 3.0.2.36 (dev.ota 3.0.2.36d, compat 209); bump it whenever dev.ota is bumped.
# deploy.sh refuses a VICTOR_COMPAT_VERSION that differs from the robot's /etc/victor-compat-version.
pin=ebf38c2871a0a69b3ac1a6fd592bb597b8e57724

[[ -d $src/.git ]] || git clone --recurse-submodules "$url" "$src"
git -C "$src" cat-file -e "$pin^{commit}" 2>/dev/null || git -C "$src" fetch origin "$pin"
git -C "$src" checkout -q "$pin"
git -C "$src" submodule update -q --init --recursive
if git -C "$src" apply --reverse --check "$here/stay-on-charger.patch" 2>/dev/null; then
  echo "OK: already applied in $src"
  exit 0
fi
git -C "$src" apply --check "$here/stay-on-charger.patch" ||
  { echo "FAIL: patch does not apply; the file has other edits, or HEAD isn't $pin" >&2; exit 1; }
git -C "$src" apply "$here/stay-on-charger.patch"
echo "OK: patched $src; next: ./build/build-v.sh then ./build/deploy-v.sh in that folder"
