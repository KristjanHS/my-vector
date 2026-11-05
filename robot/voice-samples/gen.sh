#!/usr/bin/env bash
# Build a listen-test sample set: gen.sh [-s SLOW] <set> "<text>" [engine:voice ...] → $W/<set>/norm/*.pcm
# (16 kHz s16le mono, loudness-matched to -20 LUFS). Venvs + Piper voices are created on first use.
set -euo pipefail
shopt -s inherit_errexit nullglob
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
W=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work/voice-samples
ffmpeg=$(command -v ffmpeg || echo /opt/ffmpeg/ffmpeg)
slow=1.0
[[ ${1:-} == -s ]] && { slow=${2:?-s needs a value}; shift 2; }
(($# >= 2)) || { echo "usage: gen.sh [-s SLOW] <set> \"<text>\" [engine:voice ...]" >&2; exit 2; }
set_dir=$W/$1 text=$2; shift 2
(($#)) || set -- piper:en_US-lessac-high piper:en_GB-cori-high piper:en_GB-jenny_dioco-medium \
  edge:en-US-AriaNeural edge:en-US-JennyNeural edge:en-US-AvaNeural \
  kokoro:af_heart kokoro:af_bella kokoro:bf_emma kokoro:af_nicole

declare -A pkgs=(
  [piper]="piper-tts==1.8.0"
  [edge]="edge-tts==7.3.1"
  [kokoro]="kokoro==0.9.4 misaki[en]==0.9.4 soundfile==0.14.0 torch==2.15.0
    https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl"
)
venv() { # venv <engine>: create + install on first use
  local v=$W/venv-$1
  # shellcheck disable=SC2086 # pkgs[] is a space-separated package list
  [[ -x $v/bin/python ]] || { uv venv -q -p 3.12 "$v" && uv pip install -q -p "$v/bin/python" ${pkgs[$1]}; }
}
piper_model() { # piper_model en_US-lessac-high → local .onnx path, downloaded on first use
  local f=$W/piper-voices/$1.onnx loc=${1%%-*} rest=${1#*-}
  local u=https://huggingface.co/rhasspy/piper-voices/resolve/main/${loc%%_*}/$loc/${rest%-*}/${rest##*-}/$1.onnx
  if [[ ! -f $f ]]; then # .onnx lands last, via .part, so a partial download never counts as present
    mkdir -p "$W/piper-voices"
    curl -fsSL -o "$f.json" "$u.json" && curl -fsSL -o "$f.part" "$u" && mv "$f.part" "$f" || return 1
  fi
  echo "$f"
}

for spec in "$@"; do
  [[ -v pkgs[${spec%%:*}] && $spec == *:?* ]] || { echo "bad voice spec $spec (engine:voice, engine = ${!pkgs[*]})" >&2; exit 2; }
done
rm -rf "$set_dir/raw" "$set_dir/norm"
mkdir -p "$set_dir/raw" "$set_dir/norm"
for engine in piper edge kokoro; do
  voices=()
  for spec in "$@"; do
    [[ ${spec%%:*} == "$engine" ]] || continue
    v=${spec#*:}; [[ $engine == piper ]] && v=$(piper_model "$v")
    voices+=("$v")
  done
  ((${#voices[@]})) || continue
  venv "$engine"
  TXT=$text HF_HOME=$W/hf-cache "$W/venv-$engine/bin/python" -I "$here/synth.py" "$engine" "$set_dir/raw" "$slow" "${voices[@]}" 2>&1 |
    { grep -vE 'Warning|warn|WeightNorm|super\(\).__init__' || true; }
done
for f in "$set_dir"/raw/*; do
  b=$(basename "${f%.*}")
  "$ffmpeg" -nostdin -loglevel error -y -i "$f" -af loudnorm=I=-20:TP=-2:LRA=11 -ar 16000 -ac 1 -f s16le "$set_dir/norm/$b.pcm"
done
printf '%s\n' "$text" >"$set_dir/text.txt"
ls "$set_dir/norm"
