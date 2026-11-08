#!/usr/bin/env bash
# Live checks without a human voice (backlog §OWED[USER]): live-check.sh <check>; exit 0 PASS, 1 FAIL, 2 setup.
#   stt   (saved lang en-US) English phrases in af_bella (:8091) → myvector-stt (:8092) en-US; each transcript must match
#   route   routeprobe: allowlist, clock + timer intents (en/et), LLM answers per brain; Estonian ones carry ä/õ/ö/ü
#   picker  saved LLM profiles + Claude Haiku: listed, Test endpoint answers OK; no full Claude key served or in apiConfig.json
#   clock   (saved lang et-EE) the firmware's clock text (%H:%M / %I:%M) in mari → STT must hear "Kell on …" in one of 3 takes
#   ha      HA plugin built against a dead URL (llm.sh ha-plugin probe): "assist, hello" → "Home Assistant is not reachable."
#   ttsdown (asks sudo; Vector speaks) fork_say with myvector-tts up → the PC voice, stopped → stock; restarts the service
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
need_lang() { # need_lang <lang>: STT serves only the saved language
  local api=${WIREPOD_URL:-http://127.0.0.1:8080} got
  got=$(curl -fsS --max-time 5 "$api/myvector/api/voice" | jq -r '.lang // empty') || setup "wire-pod not up at $api"
  [[ $got == "$1" ]] || setup "saved lang is ${got:-unset}, this check needs $1 (switch it in the web UI)"
}
# routeprobe must write nothing under secrets/server/ (vars.ReadConfig rewrites apiConfig.json): mtimes before/after
mtimes() { (cd "$repo/secrets/server" && stat -c '%n %.9Y' apiConfig.json myvectorConfig.json jdocs/*.json 2>/dev/null) || true; }
same_mtimes() { # same_mtimes <before>
  local now; now=$(mtimes)
  [[ $now == "$1" ]] && pass "secrets/server/ config + jdocs untouched" ||
    fail "secrets/server/ files changed during the check: $(diff <(echo "$1") <(echo "$now") | grep '^>' | tr '\n' ' ')"
}
probe() { # probe <args…>: runs routeprobe in chipper/; out = its stdout+stderr; returns its status
  local rc=0
  out=$(cd "$repo/chipper" && "$work/routeprobe" "$@" 2>&1) || rc=$?
  return "$rc"
}

check_stt() {
  need_lang en-US
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
  local api=${WIREPOD_URL:-http://127.0.0.1:8080} v models l p m r body
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
  [[ $(grep -c sk-ant "$repo/secrets/server/apiConfig.json" 2>/dev/null || true) == 0 ]] && pass "apiConfig.json holds no Claude key" || fail "apiConfig.json holds a Claude key"
  for r in /api/get_kg_api /myvector/api/voice; do
    body=$(curl -fsS --max-time 5 "$api$r") || { fail "fetch $r"; continue; }
    grep -qE 'sk-ant-[A-Za-z0-9_-]{20,}' <<<"$body" && fail "full key served by $r" || pass "no full key in $r"
  done
  git -C "$repo" check-ignore -q secrets/server/myvectorConfig.json && pass "myvectorConfig.json gitignored" || fail "myvectorConfig.json not gitignored"
}

check_route_build() { (cd "$repo/chipper" && go build -o "$work/routeprobe" ./pkg/myvector/routeprobe) || setup "routeprobe does not build"; }

check_route() {
  local et_llm=gemma4:e4b-it-qat before out
  check_route_build
  before=$(mtimes)
  route() { # route <want-regex> <probe args…>: the probe's last line must match
    local want=$1 last; shift
    probe "$@" || { fail "${*: -1}: routeprobe exit $?: $(tail -3 <<<"$out" | tr '\n' ' ')"; return; }
    last=$(tail -1 <<<"$out")
    [[ $last =~ $want ]] && pass "${*: -1} → $last" || fail "${*: -1}: want /$want/, got: $last"
  }
  route_llm() { # route_llm <lang> <brain> <want-regex> <text>: "llm-target:" names -brain's provider + model; the answer matches
    local l=$1 brain=$2 want=$3 target last
    probe -lang "$l" -llm -brain "$brain" "$4" || { fail "$l $brain: routeprobe exit $?: $(tail -3 <<<"$out" | tr '\n' ' ')"; return; }
    target=$(grep '^llm-target: ' <<<"$out" || true)
    [[ $target == "llm-target: ${brain%%:*} ${brain#*:}" ]] && pass "$l $target" ||
      fail "$l $brain: want llm-target: ${brain%%:*} ${brain#*:}, got: ${target:-none}"
    last=$(tail -1 <<<"$out")
    [[ $last =~ $want ]] && pass "$l $4 → $last" || fail "$l $brain $4: want /$want/, got: $last"
  }
  route '^llm: \(not asked\)$' -lang en-US "what should i cook tonight"                 # allowlist: not goodnight
  route '^intent: intent_clock_time$' -lang en-US "what time is it"
  route '^intent: intent_clock_settimer_extend \{"timer_duration":"600"\}$' -lang en-US "set a timer for ten minutes"
  route '^intent: intent_clock_settimer_extend \{"timer_duration":"300"\}$' -lang et-EE "pane taimer viieks minutiks"
  route '^intent: intent_clock_time$' -lang et-EE "mis kell on"
  route_llm en-US ollama:qwen3.5:4b '^llm: .{20,}' "what should i cook tonight"
  route_llm en-US claude:claude-haiku-5-5 '^llm: .{20,}' "what should i cook tonight"
  route_llm et-EE "ollama:$et_llm" '^llm: .*[äõöü]' "mis on eesti pealinn ja miks see on tähtis"
  route_llm et-EE claude:claude-haiku-5-5 '^llm: .*[äõöü]' "mis on eesti pealinn ja miks see on tähtis"
  same_mtimes "$before"
}

check_clock() {
  local t got f=$work/clock.pcm take heard
  need_lang et-EE
  # each take is a fresh Piper sample that can blur "Kell" (tts/server.py PIPER_NOISE) → up to 3 takes; a service error FAILs at once
  for t in 08:05 20:47 12:00; do
    heard=() got= filler=
    for take in 1 2 3; do
      synth mari et-EE "$t" "$f" || { heard+=("tts failed"); break; }
      got=$(transcribe et-EE "$f") || { got= heard+=("stt failed (saved lang must be et-EE)"); break; }
      heard+=("$got")
      [[ $got =~ (kuni|või) ]] && filler=1 # mari's digit-seam filler (tts/et_text.py _time): any take fails, no retry
      [[ $got =~ ^Kell\ on\  ]] && break
    done
    [[ $got =~ ^Kell\ on\  && -z $filler ]] && pass "clock $t → $got (take $take)" ||
      fail "clock $t → $(printf '"%s" ' "${heard[@]}")want Kell on … without kuni/või"
  done
}

check_ttsdown() {
  # shellcheck source=robot/quiet/guard.sh
  . "$repo/robot/quiet/guard.sh" && quiet_guard # Vector speaks: obeys the sound lock (G4)
  local api=${WIREPOD_URL:-http://127.0.0.1:8080} esn lang t1 r one="Test one." two="Test two."
  esn=$(jq -r '.robots[0].esn // empty' "$repo/secrets/server/jdocs/botSdkInfo.json" 2>/dev/null) || true
  [[ -n $esn ]] || setup "no robot in secrets/server/jdocs/botSdkInfo.json"
  read -r lang t1 < <(curl -fsS --max-time 5 "$api/myvector/api/voice" | jq -r '"\(.lang) \(if .lang == "et-EE" then .t1_et else .t1 end)"') &&
    [[ -n ${t1:-} ]] || setup "wire-pod not up"
  [[ $t1 != stock ]] || setup "T1 is stock for the saved lang: nothing to fall back from"
  [[ $lang != et-EE ]] || one="Proov üks." two="Proov kaks." # the saved lang's voice reads its own language
  say() { curl -sS --max-time 35 "$api/api-sdk/fork_say?serial=$esn" --data-urlencode "text=$1"; }
  r=$(say "$one") || r="curl failed: $?"
  [[ $r == "FINISHED $t1" ]] && pass "TTS up → $r" || fail "TTS up: want FINISHED $t1, got $r"
  sudo systemctl stop myvector-tts || setup "sudo refused"
  trap 'sudo systemctl start myvector-tts' EXIT # restart even if what follows aborts
  # voice.Say falls back to SayText: fork_say writes "FINISHED fallback: <why>" (voice.Fallback + ": " + why)
  r=$(say "$two") || r="curl failed: $?"
  [[ $r =~ ^FINISHED\ fallback:\  ]] && pass "TTS down → $r" || fail "TTS down: want FINISHED fallback: …, got $r"
  sudo systemctl start myvector-tts && trap - EXIT
  for _ in {1..60}; do curl -fsS --max-time 2 "$tts/health" >/dev/null 2>&1 && break; sleep 1; done
  curl -fsS --max-time 2 "$tts/health" >/dev/null && pass "myvector-tts back up" || fail "myvector-tts not healthy after restart"
}

check_ha() {
  local so=$work/commandha-dead.so out last before
  "$repo/scripts/llm.sh" ha-plugin probe "$so" >/dev/null || setup "HA plugin does not build"
  check_route_build
  before=$(mtimes)
  if probe -lang en-US -plugin "$so" "assist, hello"; then last=$(tail -1 <<<"$out")
    [[ $last == "plugin: Home Assistant is not reachable." ]] && pass "HA down → $last" || fail "HA down: got $last"
  else fail "HA down: routeprobe exit $?: $(tail -3 <<<"$out" | tr '\n' ' ')"; fi
  if probe -lang en-US -plugin "$so" "what time is it"; then last=$(tail -1 <<<"$out")
    [[ $last == "intent: intent_clock_time" ]] && pass "plugin loaded, other intents still route → $last" || fail "with plugin: got $last"
  else fail "with plugin: routeprobe exit $?: $(tail -3 <<<"$out" | tr '\n' ' ')"; fi
  same_mtimes "$before"
}

case ${1:-} in
  stt) check_stt ;;
  ha) check_ha ;;
  clock) check_clock ;;
  ttsdown) check_ttsdown ;;
  route) check_route ;;
  picker) check_picker ;;
  *) sed -n '2,/^set /p' "$0" | sed '$d'; exit 2 ;;
esac
((fails == 0)) && echo PASS || { echo "FAIL ($fails)"; exit 1; }
