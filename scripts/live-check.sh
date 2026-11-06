#!/usr/bin/env bash
# Live checks without a human voice (backlog §OWED[USER]): live-check.sh <check>; exit 0 PASS, 1 FAIL, 2 setup.
#   stt   English phrases in af_bella (:8091) → myvector-stt (:8092) en-US; each transcript must match
#   picker  saved LLM profiles + Claude Haiku: listed, Test endpoint answers OK; no full Claude key served or in apiConfig.json
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
tts=${TTS_URL:-http://127.0.0.1:8091} stt=${STT_URL:-http://127.0.0.1:8092}
work=$repo/robot/work/live-check
mkdir -p "$work"
fails=0
pass() { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails + 1)); }
setup() { echo "SETUP: $*" >&2; exit 2; }

synth() { # synth <voice> <lang> <text> <out.pcm>
  jq -nc --arg t "$3" --arg v "$1" --arg l "$2" '{text: $t, voice: $v, lang: $l}' |
    curl -fsS --max-time 60 -H 'Content-Type: application/json' -d @- -o "$4" "$tts/tts"
}
transcribe() { # transcribe <lang> <file.pcm> → text
  curl -fsS --max-time 120 -H 'Content-Type: application/octet-stream' --data-binary @"$2" "$stt/stt?lang=$1" | jq -r '.text // ""'
}
norm() { tr '[:upper:]' '[:lower:]' | tr -d '[:punct:]' | tr -s ' ' | sed 's/^ //; s/ $//'; }

check_stt() {
  curl -fsS --max-time 5 "$tts/health" >/dev/null || setup "TTS not up at $tts"
  local phrases=("set a ten minute timer" "what is the capital of france" "how far away is the moon"
    "what should i cook tonight" "tell me a fun fact about octopuses" "what is the weather like on mars") p f got
  for p in "${phrases[@]}"; do
    f=$work/stt-$(tr ' ' _ <<<"$p").pcm
    synth af_bella en-US "$p" "$f" || { fail "tts: $p"; continue; }
    got=$(transcribe en-US "$f") || { fail "stt unreachable: $p"; continue; }
    # Whisper writes numbers as digits: "10-minute" → compare with digits spelled the same way
    if [[ $(norm <<<"$got" | sed 's/\b10 minute\b/ten minute/; s/\b10minute\b/ten minute/') == "$p" ]]; then pass "stt: $got"
    else fail "stt: want \"$p\", got \"$got\""; fi
  done
}

check_picker() {
  local api=${WIREPOD_URL:-http://127.0.0.1:8080} v models l p m r
  v=$(curl -fsS --max-time 5 "$api/myvector/api/voice") || setup "wire-pod not up at $api"
  models=$(curl -fsS --max-time 30 "$api/myvector/api/llm/models")
  [[ $(jq -r '.ollama.error // empty' <<<"$models") == "" ]] && pass "ollama list reachable" || fail "ollama list: $(jq -r .ollama.error <<<"$models")"
  # saved + default profiles, plus Claude Haiku in both languages: listed, and Test answers OK
  while read -r l p m; do
    if [[ $p == ollama ]]; then jq -e --arg m "$m" '.ollama.models | index($m)' <<<"$models" >/dev/null || fail "$l $m not in ollama list"
    else jq -e --arg m "$m" '[.claude.models[].id] | index($m)' <<<"$models" >/dev/null || fail "$l $m not in claude list"; fi
    r=$(jq -nc --arg l "$l" --arg p "$p" --arg m "$m" '{lang: $l, provider: $p, model: $m}' |
      curl -sS --max-time 120 -H 'Content-Type: application/json' -d @- "$api/myvector/api/llm/test")
    if jq -e '.reply | test("ok"; "i")' <<<"$r" >/dev/null 2>&1; then pass "test $l $p $m: $(jq -r '"\(.reply) \(.ms) ms"' <<<"$r")"
    else fail "test $l $p $m: $r"; fi
  done < <({ jq -r '(.llm, .defaults) | to_entries[] | "\(.key) \(.value.provider) \(.value.model)"' <<<"$v"; printf '%s claude claude-haiku-5-5\n' en-US et-EE; } | sort -u)
  # key hygiene: no full key outside the gitignored fork config
  [[ $(grep -c sk-ant "$repo/chipper/apiConfig.json" 2>/dev/null || true) == 0 ]] && pass "apiConfig.json holds no Claude key" || fail "apiConfig.json holds a Claude key"
  for r in /api/get_kg_api /myvector/api/voice; do
    curl -fsS --max-time 5 "$api$r" | grep -qE 'sk-ant-[A-Za-z0-9_-]{20,}' && fail "full key served by $r" || pass "no full key in $r"
  done
  git -C "$repo" check-ignore -q chipper/myvectorConfig.json && pass "myvectorConfig.json gitignored" || fail "myvectorConfig.json not gitignored"
}

case ${1:-} in
  stt) check_stt ;;
  picker) check_picker ;;
  *) sed -n '2,/^set /p' "$0" | sed '$d'; exit 2 ;;
esac
((fails == 0)) && echo PASS || { echo "FAIL ($fails)"; exit 1; }
