#!/usr/bin/env bash
# make tts-setup: venv in robot/work/tts + Piper cori model, then one warm run (fills the shared HF cache; online).
# Models are shared with robot/voice-samples (robot/work/voice-samples/{piper-voices,hf-cache}). Rerun after a requirements.txt change.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work
venv=$work/tts/venv samples=$work/voice-samples
uv venv -q -p 3.12 --allow-existing "$venv"
uv pip install -q -p "$venv/bin/python" -r "$here/requirements.txt"
m=$samples/piper-voices/en_GB-cori-high.onnx
u=https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/cori/high/en_GB-cori-high.onnx
if [[ ! -f $m ]]; then # .onnx lands last, via .part, so a partial download never counts as present
  mkdir -p "${m%/*}"
  curl -fsSL -o "$m.json" "$u.json" && curl -fsSL -o "$m.part" "$u" && mv "$m.part" "$m"
fi
HF_HOME=$samples/hf-cache TTS_PIPER_DIR=${m%/*} "$venv/bin/python" -I "$here/server.py" --warm-only
