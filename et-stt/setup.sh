#!/usr/bin/env bash
# make stt-setup: venv + TalTech turbo-et (et-EE) and stock large-v3-turbo (en-US) CT2 models in robot/work/et-stt, then one warm decode each on the GPU (online).
# Rerun after a requirements.txt change.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
w=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work/et-stt
venv=$w/venv
uv venv -q -p 3.12 --allow-existing "$venv"
uv pip install -q -p "$venv/bin/python" -r "$here/requirements.txt"
if [[ ! -f $w/models/taltech-et/ct2/model.bin ]]; then
  "$venv/bin/python" -I -c 'import sys; from huggingface_hub import snapshot_download as s; s("TalTechNLP/whisper-large-v3-turbo-et-verbatim", allow_patterns=["ct2/*"], local_dir=sys.argv[1])' "$w/models/taltech-et"
fi
if [[ ! -f $w/models/turbo-stock/model.bin ]]; then
  "$venv/bin/python" -I -c 'import sys; from huggingface_hub import snapshot_download as s; s("deepdml/faster-whisper-large-v3-turbo-ct2", local_dir=sys.argv[1])' "$w/models/turbo-stock"
fi
nv=$("$venv/bin/python" -I -c 'import nvidia, os; print(os.path.dirname(nvidia.__path__[0]))')/nvidia
LD_LIBRARY_PATH=$nv/cublas/lib:$nv/cudnn/lib STT_MODEL=$w/models/taltech-et/ct2 STT_MODEL_EN=$w/models/turbo-stock "$venv/bin/python" -I "$here/server.py" --warm-only
