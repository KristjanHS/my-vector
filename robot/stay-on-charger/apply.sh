#!/usr/bin/env bash
# Clones wire-os-victor into the gitignored robot/work/ at the pinned commit and applies the stay-docked patch.
# Then build + deploy from that checkout: ./build/build-v.sh, then ./build/deploy-v.sh (needs Docker).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../work" && pwd)/wire-os-victor
url=https://github.com/os-vector/wire-os-victor
pin=505eb84cfaa6ec01c075730f4be855e93713bb59 # main on 2026-11-03; the patch was written against this

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
