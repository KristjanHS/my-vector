# Backlog

## OWED[USER]

- WiFi baseline live (`2026-11-07-wifi-baseline-hardening.md` (docs/plans/) 4.4 row, 6.1): restart vic-cloud → `make server-pin` → `mv secrets/epod-new secrets/epod && make start` → `make syncwt` → `make ship` → `robot/netguard/apply.sh --selftest` → the 6.1 ranked e2e.
- WiFi baseline keys + leftovers (same plan, 4.1/1.2/2.1): `ca.key` + `secrets/myvector_ed25519` to the password manager, then `rm` `ca.key`; `make server-pin` nft 8084 check; live `robot/diskguard/apply.sh`.
- Thinking levels 2.1 gate (`2026-11-08-local-thinking-levels.md` (docs/plans/) R9): gemma4 × chat et exact 15/16 < 95% (n 16 needs 16/16), err +0/+1 — loosen to `err` within ±1, or debug first.
- et active listening: no Ollama model reached the `listenprobe` bar (`2026-11-07-et-listen-model.md` (docs/plans/) W1) — next lever owed.
- LLM picker page by eye (`2026-11-06-llm-picker-page.md` (docs/plans/) Stage 5 §2 items 3+5): Ollama-down warning, cleared key → Claude disabled + 400, upstream Knowledge save keeps the fork profile.
- PC voice loudness (V1): T1 `af_bella`/`cori` quieter than stock beyond 3 dB — rule.
- Prompt templates by ear + eye (`2026-11-07-prompt-templates.md` (docs/plans/) OW): et-EE weekday + number-heavy ask on the robot (persona, digits only for times), `/myvector/prompt` page.
- Voice listening: `2026-11-05-vector-voice-verify.md` (docs/plans/) V5 rows 3–5 (interrupt, sentence gaps, KG Acapela).
- Reply cap live (`2026-11-07-reply-cap-by-ask.md` (docs/plans/) 2.1–2.2): after `make ship` + wire-pod restart, `fakebot -say "tell me a story about a robot"` → `journalctl -u wire-pod | grep 'reply cap'` shows the long cap; story audible on Vector.
- Web search by ear (`2026-11-07-web-search.md` (docs/plans/) 1.5 + 6.1, volume 1, after `make ship`): say "mis ilm homme on?", "kas homme sajab?", "mis täna Eestis juhtus?" to the robot.
- SeAssert fix live (`2026-11-07-vic-anim-seassert.md` (docs/plans/archive/), patch 0012): `make ship`; its smoke must not show `nrgainv.c:191` — a recurrence after 0012 is a new bug.
- Active listening live (`2026-11-07-active-listening.md` (docs/plans/) 5.2, volume 1, after `make ship`): its 5.2 run; wake word mid-clip (3.3); `journalctl -u wire-pod | grep 'clip gap'` (C2); G5 curl 423 (1.3); en-US phrase STT round trip (1.1).

## Assistant-owed


- ⚠ et-EE story ask answers ~14 words, no story (seen on the deleted gemma4:e2b-it-qat; recheck on gemma4:e4b-it-qat; not cap-cut) · seam: et prompt/model · falsifier (from chipper/): `go run ./pkg/myvector/routeprobe -llm -lang et-EE "räägi mulle muinasjutt robotist" | awk '/^llm: /{exit NF<60}'`
