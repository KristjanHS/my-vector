# Backlog

## OWED[USER]

- Active-listening model (`2026-11-07-active-listening.md` (docs/plans/) P1): saved profiles fail the Stage 2.1 bar, `gemma4:e4b-it-qat` held-out 16/20 (0 false SPEAK) — accept, prompt round, or park.
- LLM picker page by eye (`2026-11-06-llm-picker-page.md` (docs/plans/) Stage 5 §2 items 3+5): Ollama-down warning, cleared key → Claude disabled + 400, upstream Knowledge save keeps the fork profile.
- PC voice loudness (V1): T1 `af_bella`/`cori` quieter than stock beyond 3 dB — rule.
- Prompt templates by ear + eye (`2026-11-07-prompt-templates.md` (docs/plans/) OW): et-EE weekday + number-heavy ask on the robot (persona, digits only for times), `/myvector/prompt` page.
- Voice listening: `2026-11-05-vector-voice-verify.md` (docs/plans/) V5 rows 3–5 (interrupt, sentence gaps, KG Acapela).
- Reply cap live (`2026-11-07-reply-cap-by-ask.md` (docs/plans/) 2.1–2.2): after `make ship` + wire-pod restart, `fakebot -say "tell me a story about a robot"` → `journalctl -u wire-pod | grep 'reply cap'` shows the long cap; story audible on Vector.

## Assistant-owed

- ⚠ et-EE story ask answers ~14 words, no story (gemma4:e2b-it-qat, not cap-cut) · seam: et prompt/model · falsifier (from chipper/): `go run ./pkg/myvector/routeprobe -llm -lang et-EE "räägi mulle muinasjutt robotist" | awk '/^llm: /{exit NF<60}'`
