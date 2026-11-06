# Backlog

## OWED[USER]

- Live-confirm intent allowlist: "Hey Vector, what should I cook tonight?" → `journalctl -u wire-pod` shows `Making LLM request`, not `intent_greeting_goodnight`.
- Estonian clock (lang = et-EE): "Mis kell on?" → Vector says "Kell on …"; `journalctl -u myvector-tts | grep 'tts voice='` text ≠ `H:MM` → fit `_CLOCK` (`tts/et_lines.py`) to it.
- Estonian live run (et-EE): one LLM question answered in Estonian with ä/õ/ü/ö, "Pane taimer viieks minutiks" fires; flip to English without restart.
- English regression (en-US): an LLM question + "what time is it" in `af_bella`, a timer set.
- STT on Whisper (`2026-11-06-llm-trials-gaps.md`, docs/plans/): `make stt-enable`, `make start`, then en-US "set a 10-minute timer" + 5 questions → `journalctl -u wire-pod | grep 'Transcribed text (en-US)'` right.
- HA down: `scripts/llm.sh ha-plugin`, `ha-down`, "assist, hello" → Vector says "Home Assistant is not reachable", no wire-pod restart.
- LLM picker live run: `2026-11-06-llm-picker-page.md` (docs/plans/) Stage 5 §2 checklist (ET→Claude, EN→Ollama, key hygiene, Ollama down).
