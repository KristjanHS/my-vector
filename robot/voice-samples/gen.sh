#!/usr/bin/env bash
# Build a listen-test sample set: gen.sh [-a] [-s SLOW] <set> "<text>" [engine:voice ...] → $W/<set>/norm/*.pcm
# (16 kHz s16le mono, loudness-matched to -20 LUFS). Venvs, Piper voices + engine assets are created on first use.
# -a appends to an existing set (no wipe; same text required); a failing engine is skipped (!! on stderr, exit 1 at end).
# Voice forms: piper:et_EE-news-medium@Mari (multi-speaker) · xtts:<FLEURS et_ee test clip id | ref .wav>
# · azure:* needs robot/work/et-stt/keys.env (else skipped). BENCH=1 (timings → <set>/bench.log) / DEVICE=cpu|cuda pass to synth.py.
set -euo pipefail
shopt -s inherit_errexit nullglob
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
W=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")/robot/work/voice-samples
ffmpeg=$(command -v ffmpeg || echo /opt/ffmpeg/ffmpeg)
slow=1.0 keep=0
while getopts as: o; do case $o in a) keep=1 ;; s) slow=$OPTARG ;; *) exit 2 ;; esac; done
shift $((OPTIND - 1))
(($# >= 2)) || { echo "usage: gen.sh [-a] [-s SLOW] <set> \"<text>\" [engine:voice ...]" >&2; exit 2; }
set_dir=$W/$1 text=$2; shift 2
(($#)) || set -- piper:en_US-lessac-high piper:en_GB-cori-high piper:en_GB-jenny_dioco-medium \
  edge:en-US-AriaNeural edge:en-US-JennyNeural edge:en-US-AvaNeural \
  kokoro:af_heart kokoro:af_bella kokoro:bf_emma kokoro:af_nicole

declare -A pkgs=(
  [piper]="piper-tts==1.8.0"
  [edge]="edge-tts==7.3.1"
  [kokoro]="kokoro==0.9.4 misaki[en]==0.9.4 soundfile==0.14.0 torch==2.15.0
    https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl"
  [tartu]="" [azure]="" [espeak]=""
  [tartulocal]="tensorflow==2.13.0 librosa==0.11.0 ruamel.yaml==0.17.40 nltk==3.9.2 speechbrain==1.0.2 torch==2.1.2 torchaudio==2.1.2
    huggingface-hub==0.29.2 pydantic pydantic-settings pyyaml git+https://github.com/TartuNLP/tts_preprocess_et.git@v1.1.0"
  [xtts]="coqui-tts==0.27.2 torch==2.8.0 torchaudio==2.8.0 git+https://github.com/TartuNLP/tts_preprocess_et.git@v1.1.0"
)
declare -A pyv=([tartulocal]=3.11) # TF 2.13 has no 3.12 wheel
etstt=$(dirname "$W")/et-stt
venv() { # venv <engine>: create + install on first use
  local v=$W/venv-$1
  # shellcheck disable=SC2086 # pkgs[] is a space-separated package list
  [[ -x $v/bin/python ]] || { uv venv -q -p "${pyv[$1]:-3.12}" "$v" && { [[ -z ${pkgs[$1]} ]] || uv pip install -q -p "$v/bin/python" ${pkgs[$1]}; }; }
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
assets() { # assets <engine> → NAME=path env lines for synth.py; downloads the engine's model/binary on first use
  local d f
  case $1 in
    tartulocal) d=$W/tartu/worker
      [[ -f $d/models/multispeaker/model_weights.hdf5 ]] || {
        rm -rf "$d" && git clone -q --recurse-submodules -b v3.1.0 https://github.com/TartuNLP/text-to-speech-worker.git "$d" &&
          curl -fsSL -o "$W/tartu/multispeaker.zip" https://github.com/TartuNLP/text-to-speech-worker/releases/download/v3.1.0/multispeaker.zip &&
          unzip -q -o -d "$d/models" "$W/tartu/multispeaker.zip"; } >&2 || return 1
      echo "TARTU_DIR=$d" ;;
    xtts) d=$W/xtts-est
      for f in config.json vocab.json mel_stats.pth dvae.pth model.pth; do # .part: a partial download never counts
        [[ -f $d/$f ]] || { mkdir -p "$d" && curl -fsSL -o "$d/$f.part" "https://huggingface.co/tartuNLP/XTTS-v2-est/resolve/main/$f" &&
          mv "$d/$f.part" "$d/$f"; } >&2 || return 1
      done
      echo "XTTS_DIR=$d" ;;
    espeak) d=$W/espeak # no sudo: unpack the distro debs
      [[ -x $d/root/usr/bin/espeak-ng ]] || {
        mkdir -p "$d/deb" && (cd "$d/deb" && apt-get download -q espeak-ng libespeak-ng1 espeak-ng-data libpcaudio0 libsonic0 >/dev/null) &&
          for f in "$d"/deb/*.deb; do dpkg -x "$f" "$d/root" || return 1; done; } >&2 || return 1
      echo "ESPEAK_ROOT=$d/root" ;;
  esac
}
synth() { # synth <engine> <voice...> → $set_dir/raw/*; errexit is off here (caller uses ||), so every step checks
  local engine=$1 a extra=() run
  shift
  venv "$engine" || return 1
  a=$(assets "$engine") || return 1
  [[ -z $a ]] || mapfile -t extra <<<"$a"
  run=(env "${extra[@]}" TXT="$text" BENCH_LOG="$set_dir/bench.log" "$W/venv-$engine/bin/python" -I "$here/synth.py" "$engine" "$set_dir/raw" "$slow" "$@")
  # shellcheck disable=SC2016 # $0/$@ expand in the inner bash
  if [[ $engine == azure ]]; then run=(bash -c 'set -a; . "$0"; set +a; exec "$@"' "$etstt/keys.env" "${run[@]}"); fi
  "${run[@]}" 2>&1 | { grep --line-buffered -vE 'Warning|warn|WeightNorm|super\(\).__init__' || true; } # pipefail: synth.py's status
}

for spec in "$@"; do
  [[ -v pkgs[${spec%%:*}] && $spec == *:?* ]] || { echo "bad voice spec $spec (engine:voice, engine = ${!pkgs[*]})" >&2; exit 2; }
done
if ((keep)); then
  [[ ! -f $set_dir/text.txt || $(<"$set_dir/text.txt") == "$text" ]] || { echo "-a: text differs from $set_dir/text.txt" >&2; exit 2; }
else rm -rf "$set_dir/raw" "$set_dir/norm"; fi
mkdir -p "$set_dir/raw" "$set_dir/norm"
failed=()
for engine in piper edge kokoro tartu tartulocal xtts azure espeak; do
  voices=()
  for spec in "$@"; do
    [[ ${spec%%:*} == "$engine" ]] || continue
    v=${spec#*:}
    if [[ $engine == piper ]]; then
      m=$(piper_model "${v%@*}") || { echo "!! $spec failed (voice download)" >&2; failed+=("$spec"); continue; }
      v=$m${v#"${v%@*}"}
    fi
    [[ $engine == xtts && $v != *.wav ]] && v=$etstt/fleurs/data/et_ee/audio/test/$v.wav
    voices+=("$v")
  done
  ((${#voices[@]})) || continue
  [[ $engine != azure || -f $etstt/keys.env ]] || { echo "azure: pending keys (no $etstt/keys.env), skipped" >&2; continue; }
  synth "$engine" "${voices[@]}" || { echo "!! $engine failed" >&2; failed+=("$engine"); }
done
for f in "$set_dir"/raw/*; do
  b=$(basename "${f%.*}")
  "$ffmpeg" -nostdin -loglevel error -y -i "$f" -af loudnorm=I=-20:TP=-2:LRA=11 -ar 16000 -ac 1 -f s16le "$set_dir/norm/$b.pcm" ||
    { echo "!! ffmpeg $b failed" >&2; rm -f "$set_dir/norm/$b.pcm"; failed+=("ffmpeg:$b"); }
done
printf '%s\n' "$text" >"$set_dir/text.txt"
ls "$set_dir/norm"
((${#failed[@]} == 0)) || { echo "!! failed: ${failed[*]}" >&2; exit 1; }
