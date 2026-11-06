# Backlog

## OWED[USER]

- Live-confirm intent allowlist: "Hey Vector, what should I cook tonight?" → `journalctl -u wire-pod` shows `Making LLM request`, not `intent_greeting_goodnight`.
- Estonian clock (lang = et-EE): "Mis kell on?" → Vector says "Kell on …"; `journalctl -u myvector-tts | grep 'tts voice='` text ≠ `H:MM` → fit `_CLOCK` (`tts/et_lines.py`) to it.
- Estonian live run (et-EE): one LLM question answered in Estonian with ä/õ/ü/ö, "Pane taimer viieks minutiks" fires; flip to English without restart.
- English regression (en-US): an LLM question + "what time is it" in `af_bella`, a timer set.
- STT on Whisper (`2026-11-06-llm-trials-gaps.md`, docs/plans/): `make stt-enable`, `make start`, then en-US "set a 10-minute timer" + 5 questions → `journalctl -u wire-pod | grep 'Transcribed text (en-US)'` right.
- HA down: `scripts/llm.sh ha-plugin`, `ha-down`, "assist, hello" → Vector says "Home Assistant is not reachable", no wire-pod restart.
- LLM picker live run: `2026-11-06-llm-picker-page.md` (docs/plans/) Stage 5 §2 checklist (ET→Claude, EN→Ollama, key hygiene, Ollama down).
- PC voice loudness (V1): T1 `af_bella` −6.6 dB, `cori` −3.5 dB vs stock (`mari` passes) — accept, or open a fix plan.
- Voice listening: `2026-11-05-vector-voice-verify.md` (docs/plans/) V5 rows 3–6 (interrupt, sentence gaps, KG Acapela, TTS-down fallback).

## Owed

- ⚠ T2 lines in `af_bella` always fall back to Acapela (`no response in 1.5 s`; cori/mari ≈0.6 s) · `tts/` af_bella first audio vs `0010-pc-voice.patch` timeout · `RUNS=1 robot/voice-samples/onset.sh af_bella | awk '$2>1{exit 1}'`
