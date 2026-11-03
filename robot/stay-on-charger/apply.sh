#!/usr/bin/env bash
# Clones wire-os-victor into the gitignored robot/work/ at the pinned commit and applies the stay-docked patch.
# Then build + deploy from that checkout: ./build/build-v.sh, then ./build/deploy-v.sh (needs Docker).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../work" && pwd)/wire-os-victor
url=https://github.com/os-vector/wire-os-victor
# The anki/victor submodule of wire-os ab4b32daf9 (2026-10-25), the base dev.ota 3.0.2.36d was built from.
# It must match: deploy.sh refuses /anki whose VICTOR_COMPAT_VERSION (209 here) differs from the robot's.
# Newer main (505eb84, compat 210) is refused. The patched file is identical in both commits.
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
