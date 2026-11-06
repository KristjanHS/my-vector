# Backlog

## OWED[USER]

- TTS-down fallback (V5 row 6): `! scripts/live-check.sh ttsdown` (asks sudo; Vector speaks twice) → PASS.
- LLM picker page by eye (`2026-11-06-llm-picker-page.md` (docs/plans/) Stage 5 §2 items 3+5): Ollama-down warning, cleared key → Claude disabled + 400, upstream Knowledge save keeps the fork profile.
- PC voice loudness (V1): T1 `af_bella`/`cori` quieter than stock beyond 3 dB — rule.
- Night window (0011, v1 shipped c75442f 2026-11-06 20:28; the review-fix revision needs `make syncwt` + `make ship`): the next morning before ~13:00 (the kept log holds ~20 h) run `robot/firmware/night-check.sh` → PASS (hits 0; `falling asleep … NightWindow` ≥ 1 if he was awake or light-asleep at 21:00).
- mari noise 0.4/0.6 (`PIPER_NOISE`) goes live on `! sudo systemctl restart myvector-tts`; then `scripts/live-check.sh clock` → PASS.
- Voice listening: `2026-11-05-vector-voice-verify.md` (docs/plans/) V5 rows 3–5 (interrupt, sentence gaps, KG Acapela).

## Assistant-owed

