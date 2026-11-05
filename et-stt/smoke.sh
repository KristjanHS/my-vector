#!/usr/bin/env bash
# Live check of a running myvector-stt (switch must be et-EE): POSTs one FLEURS et_ee clip, prints its transcript; exit 1 on no text.
# Usage: et-stt/smoke.sh [clip.wav]   (default: the smallest clip in robot/work/et-stt/fleurs/data/et_ee)
set -euo pipefail
w=$(cd "$(dirname "$0")/.." && pwd)/robot/work/et-stt
clip=${1:-$(find "$w/fleurs/data/et_ee" -name '*.wav' -printf '%s %p\n' 2>/dev/null | sort -n | awk 'NR == 1 { sub(/^[0-9]+ /, ""); print }' || true)} # awk reads all: head would SIGPIPE sort
[[ -f $clip ]] || { echo "no clip (robot/et-stt/run.sh fetches FLEURS)" >&2; exit 1; }
curl -fsS http://127.0.0.1:8092/health; echo
pcm=$(mktemp "$w/smoke.XXXXXX")
trap 'rm -f "$pcm"' EXIT
"$w/venv/bin/python" -I -c 'import sys, soundfile as sf, numpy as np
a, r = sf.read(sys.argv[1], dtype="float32")  # dtype=int16 on a FLOAT wav reads ~0, not scaled
assert r == 16000 and a.ndim == 1, (r, a.shape)
open(sys.argv[2], "wb").write(np.clip(a * 32768, -32768, 32767).astype("<i2").tobytes())' "$clip" "$pcm"
out=$(curl -fsS --max-time 60 --data-binary @"$pcm" -H 'Content-Type: application/octet-stream' http://127.0.0.1:8092/stt)
echo "$clip -> $out"
ref=$(awk -F'\t' -v f="${clip##*/}" '$2 == f { print $4 }' "$w/fleurs/data/et_ee/test.tsv" 2>/dev/null || true)
"$w/venv/bin/python" -I -c 'import json, re, sys
hyp = re.findall(r"\w+", json.loads(sys.argv[1])["text"].lower())
ref = sys.argv[2].split()
hit = sum(1 for x in ref if x in hyp) / len(ref) if ref else 1.0
print("reference:", sys.argv[2] or "(none)", f" words matched {hit:.0%}")
sys.exit(0 if hyp and hit >= 0.6 else 1)' "$out" "$ref"
