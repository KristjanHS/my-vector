#!/usr/bin/env bash
# LLM bench (plan 2026-11-05 Stage 1.3): et = the et-EE default (gemma4:e4b-it-qat), en = E12 candidates still installed; then reloads Vector's English model.
# Usage: run.sh et|en [MODEL...]; score `python3 -I robot/et-llm/score.py <lang>`, view `bench.py --lang <lang> summary`.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
LANG_=${1:?usage: run.sh et|en [MODEL...]}; shift
MODELS=("$@")
if [[ ${#MODELS[@]} -eq 0 ]]; then
  case $LANG_ in
    et) MODELS=(gemma4:e4b-it-qat) ;;
    en) MODELS=(qwen3.5:4b gemma4:e4b-it-qat) ;;
    *) echo "lang must be et or en" >&2; exit 2 ;;
  esac
fi
trap '"$ROOT/scripts/llm.sh" warm 2>&1 | tail -3' EXIT
python3 -I "$ROOT/robot/et-llm/bench.py" --lang "$LANG_" gen "${MODELS[@]}"
gen_rc=$?
python3 -I "$ROOT/robot/et-llm/bench.py" --lang "$LANG_" summary
exit "$gen_rc"
