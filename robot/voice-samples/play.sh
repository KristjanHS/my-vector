#!/usr/bin/env bash
# Play a sample set on Vector: play.sh [-v VOL] <set> [name ...] — Ben announces A, B, … before each sample.
# Names are norm/ basenames without .pcm (default: all, sorted). Robot facts: robot/env.sh; GUID: wire-pod's botSdkInfo.json.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
W=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work/voice-samples
vol=60
[[ ${1:-} == -v ]] && { vol=$2; shift 2; }
(($#)) || { echo "usage: play.sh [-v VOL] <set> [name ...]" >&2; exit 2; }
dir=$W/$1/norm; shift
[[ -d $dir ]] || { echo "no sample set at $dir (run gen.sh)" >&2; exit 2; }
names=("$@")
if ((!$#)); then
  for f in "$dir"/*.pcm; do names+=("$(basename "$f" .pcm)"); done
fi

probe=$W/bin/voiceprobe
(cd "$repo/chipper" && go build -o "$probe" ./pkg/myvector/voiceprobe)
letters=({A..Z})
for i in "${!names[@]}"; do
  l=${letters[$i]}
  echo "$l ${names[$i]}"
  "$probe" say -dur 1.0 "$l." >/dev/null
  sleep 0.5
  "$probe" play -vol "$vol" "$dir/${names[$i]}.pcm"
  sleep 2
done
