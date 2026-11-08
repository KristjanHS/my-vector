#!/usr/bin/env bash
# Vision bench photos (2026-11-08-vision-setting.md (docs/plans/) V10): grab.sh [n] waits for Enter, grabs one camera frame
# via wire-pod's /api-sdk/fork_capture, saves robot/work/vision-bench/NN.jpg after the highest existing NN. Exit 1 on a non-JPEG body.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
silent=1 # robot sound lock: camera read only, makes no robot sound
# shellcheck source=robot/env.sh
. "$here/../env.sh"
n=${1:-1}
[[ $n =~ ^[1-9][0-9]*$ ]] || { echo "usage: grab.sh [n]  (n ≥ 1)"; exit 2; }
out=$robot_dir/work/vision-bench; mkdir -p "$out"
last=0
for f in "$out"/[0-9][0-9].jpg; do [[ -f $f ]] || continue; k=$((10#$(basename "$f" .jpg))); ((k > last)) && last=$k; done
for ((i = 1; i <= n; i++)); do
  name=$(printf '%02d.jpg' $((last + i)))
  read -rp "set scene $i of $n, Enter to grab $name... " _ </dev/tty
  tmp=$out/.grab.tmp
  curl -fsS -m 25 -o "$tmp" "$server/api-sdk/fork_capture?serial=$esn" || { echo "FAIL: curl to $server"; rm -f "$tmp"; exit 1; }
  if [[ $(head -c 2 "$tmp" | od -An -tx1 | tr -d ' ') != ffd8 ]]; then
    echo "FAIL: not a JPEG: $(head -c 200 "$tmp")"; rm -f "$tmp"; exit 1
  fi
  mv "$tmp" "$out/$name"
  echo "  $name  $(($(stat -c %s "$out/$name") / 1024)) KB  ok"
done
