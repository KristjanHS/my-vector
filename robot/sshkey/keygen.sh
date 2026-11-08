#!/usr/bin/env bash
# Make the robot root SSH key (ed25519, no passphrase: wire-pod uses it unattended). Refuses to overwrite.
# Usage: robot/sshkey/keygen.sh [path]   default <main checkout>/secrets/myvector_ed25519 (gitignored)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
out=${1:-$main/secrets/myvector_ed25519}
[[ ! -e $out && ! -e $out.pub ]] || { echo "FAIL: $out or $out.pub exists; not overwriting" >&2; exit 1; }
mkdir -p "$(dirname "$out")"
ssh-keygen -q -t ed25519 -N '' -C "my-vector robot root" -f "$out"
ssh-keygen -lf "$out.pub"
