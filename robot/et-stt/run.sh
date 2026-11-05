#!/usr/bin/env bash
# Estonian STT bench (plan 2026-11-05 Stage 1.1): runs every measurable candidate, JSON -> robot/work/et-stt/results/.
# Usage: run.sh [all|NAME...]. Keyed rows read robot/work/et-stt/keys.env (AZURE_SPEECH_KEY, AZURE_SPEECH_REGION, GOOGLE_STT_KEY) if it exists.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
W=$ROOT/robot/work/et-stt
PY=$W/venv/bin/python
NV=$W/venv/lib/python3.12/site-packages/nvidia
export LD_LIBRARY_PATH=$NV/cublas/lib:$NV/cudnn/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}
B=(-I "$ROOT/robot/et-stt/bench.py" run)
TT=$W/models/taltech-et/ct2
ST=$W/models/turbo-stock
want() { [[ $SEL == all ]] || [[ " $SEL " == *" $1 "* ]]; }
SEL=${*:-all}

# GPU room: unload the LLM for the run, always reload it afterwards
ollama stop gemma3:4b-it-qat 2>/dev/null
trap '"$ROOT/scripts/llm.sh" warm 2>&1 | tail -3' EXIT

run() { want "$1" || return 0; echo "== $1"; "$PY" "${B[@]}" --name "$@" || echo "!! $1 failed"; }
run taltech-et-cuda-fp16  --engine fw --model "$TT" --device cuda --compute float16
run taltech-et-cuda-i8f16 --engine fw --model "$TT" --device cuda --compute int8_float16
run taltech-et-cpu-i8     --engine fw --model "$TT" --device cpu --compute int8
run stock-turbo-cuda-i8f16 --engine fw --model "$ST" --device cuda --compute int8_float16
run vosk-en-baseline      --engine vosk --model "$ROOT/vosk/models/en-US/model" --device cpu --lang en --dataset fleurs-en
if [[ -d $W/cv ]]; then
  run taltech-et-cuda-fp16-cv --engine fw --model "$TT" --device cuda --compute float16 --dataset cv-et
  run taltech-et-cuda-i8f16-cv --engine fw --model "$TT" --device cuda --compute int8_float16 --dataset cv-et
  run stock-turbo-cuda-i8f16-cv --engine fw --model "$ST" --device cuda --compute int8_float16 --dataset cv-et
else echo "== CV: pending CV download ($W/cv absent)"; fi
if compgen -G "$W/mic/*.[op][gc][gm]" >/dev/null; then  # Stage 2.2 Vector mic captures (miccap)
  run taltech-et-cuda-i8f16-mic --engine fw --model "$TT" --device cuda --compute int8_float16 --dataset mic-et
  run taltech-et-cuda-fp16-mic --engine fw --model "$TT" --device cuda --compute float16 --dataset mic-et
  run stock-turbo-cuda-i8f16-mic --engine fw --model "$ST" --device cuda --compute int8_float16 --dataset mic-et
fi
if [[ -f $W/keys.env ]]; then
  # keys only enter these subshells' environment; values are never printed
  ( set -a; . "$W/keys.env"; set +a
    [[ -n ${AZURE_SPEECH_KEY:-} ]] && run azure-f0 --engine azure --device online
    [[ -n ${GOOGLE_STT_KEY:-} ]] && run google-v1 --engine google --device online
    [[ -d $W/cv && -n ${AZURE_SPEECH_KEY:-} ]] && run azure-f0-cv --engine azure --device online --dataset cv-et
    [[ -d $W/cv && -n ${GOOGLE_STT_KEY:-} ]] && run google-v1-cv --engine google --device online --dataset cv-et
    true )
else echo "== Azure/Google: pending keys ($W/keys.env absent)"; fi
"$PY" "${B[0]}" "${B[1]}" summary
