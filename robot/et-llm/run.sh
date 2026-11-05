#!/usr/bin/env bash
# Estonian LLM bench (plan 2026-11-05 Stage 1.3): answers for every candidate, then reloads Vector's model.
# Usage: run.sh [MODEL...]; score with `python3 -I robot/et-llm/score.py`, view `python3 -I robot/et-llm/bench.py summary`.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
MODELS=("$@")
[[ ${#MODELS[@]} -gt 0 ]] || MODELS=(
  gemma3:4b-it-qat
  hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M
  gemma4:e2b
  gemma4:e4b
)
trap '"$ROOT/scripts/llm.sh" warm 2>&1 | tail -3' EXIT
python3 -I "$ROOT/robot/et-llm/bench.py" gen "${MODELS[@]}"
python3 -I "$ROOT/robot/et-llm/bench.py" summary
