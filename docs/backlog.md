# Backlog

## OWED[USER]
- G3 et self-dialogue reproduces 4/20 and the prompt line does not fix it (`2026-11-07-follow-up-listening.md` (docs/plans/) P7): pick the next lever or accept (brain swap and decider-NO on filler asks ruled out, G3).

- Follow-up "jah" by voice (`2026-11-07-follow-up-listening.md` (docs/plans/) 7.5, after 7.3's ship): `mkdir robot/work/et-stt/mic` (capture is on only while it exists), answer a follow-up with "jah", then `robot/work/et-stt/venv/bin/python et-stt/nospeech.py --max 0.6 <newest robot/work/et-stt/mic/*.ogg>` must exit 0.
- SDK audio orphan live (`2026-11-09-sdk-audio-orphan.md` (docs/plans/)): `make ship` (patch 0019 + vic-cloud gateway), then `voiceprobe overlap` — B refused (ResourceExhausted), A/C/D/F hear PlaybackComplete, no `starved` / `VERIFY(_audioPrepared)` line.
- WiFi baseline live (`2026-11-07-wifi-baseline-hardening.md` (docs/plans/) 4.4 row, 6.1): restart vic-cloud → `make server-pin` → `mv secrets/epod-new secrets/epod && make start` → `make syncwt` → `make ship` → `robot/netguard/apply.sh --selftest` → the 6.1 ranked e2e.
- WiFi baseline keys + leftovers (same plan, 4.1/1.2/2.1): `ca.key` + `secrets/myvector_ed25519` to the password manager, then `rm` `ca.key`; `make server-pin` nft 8084 check; live `robot/diskguard/apply.sh`.
- et active listening: no Ollama model reached the `listenprobe` bar (`2026-11-07-et-listen-model.md` (docs/plans/) W1) — next lever owed.
- LLM picker page by eye (`2026-11-06-llm-picker-page.md` (docs/plans/) Stage 5 §2 items 3+5): Ollama-down warning, cleared key → Claude disabled + 400, upstream Knowledge save keeps the fork profile.
- PC voice loudness (V1): T1 `af_bella`/`cori` quieter than stock beyond 3 dB — rule.
- Prompt templates by ear + eye (`2026-11-07-prompt-templates.md` (docs/plans/) OW): et-EE weekday + number-heavy ask on the robot (persona, digits only for times), `/myvector/prompt` page.
- Voice listening: `2026-11-05-vector-voice-verify.md` (docs/plans/) V5 rows 3–5 (interrupt, sentence gaps, KG Acapela).
- Reply cap live (`2026-11-07-reply-cap-by-ask.md` (docs/plans/) 2.1–2.2): after `make ship` + wire-pod restart, `fakebot -say "tell me a story about a robot"` → `journalctl -u wire-pod | grep 'reply cap'` shows the long cap; story audible on Vector.
- Web search by ear (`2026-11-07-web-search.md` (docs/plans/) 1.5 + 6.1, volume 1, after `make ship`): say "mis ilm homme on?", "kas homme sajab?", "mis täna Eestis juhtus?" to the robot.
- SeAssert fix live (`2026-11-07-vic-anim-seassert.md` (docs/plans/archive/), patch 0012): `make ship`; its smoke must not show `nrgainv.c:191` — a recurrence after 0012 is a new bug.
- Active listening live (`2026-11-07-active-listening.md` (docs/plans/) 5.2, volume 1, after `make ship`): its 5.2 run; wake word mid-clip (3.3); `journalctl -u wire-pod | grep 'clip gap'` (C2); G5 curl 423 (1.3); en-US phrase STT round trip (1.1).
- Hey Vector while listening live (`2026-11-08-hey-vector-while-listening.md` (docs/plans/) 2.1, volume 1, after `make ship`): on/off by voice both languages in the PC voice; clock, name, one LLM ask → each answered once, no listen SPEAK.
- Memory M2 live (`2026-11-08-memory-m2.md` (docs/plans/) 6.1, volume 1, after `make ship`): chat turn → line in `chipper/myvectorMemory/<esn>/`; tell a fact, "Run pass now" → `active` + evidence + diary; ask it back; `journalctl -u wire-pod | grep 'memory pass'` shows `guard=ok`.
- Listen-switch prompt line (same plan, H6): "never claim you switched active listening" breaks `TestPromptArmsLongAbout` (arm X 611 > `prompt.Budget` 600) — raise the budget, drop arm X, trim a line, or skip it.
- syncwt speed live (`2026-11-09-syncwt-speed.md` (docs/plans/) stop rule): next `make syncwt` report — no-rebase ≤ 3 s, rebased gate ≤ 20 s; a re-gate seen in practice → re-pick S3/S4.
- Shut-up-sleep live (`2026-11-07-shut-up-sleep.md` (docs/plans/) 6.4, volume 1, after `make ship`): its 6.4 row; then `robot/openmic/probe.sh -m` et-EE + en-US → Stage 1 verdict, which re-cuts Stage 5.

## Assistant-owed


- ⚠ et-EE story ask answers ~14 words, no story (seen on the deleted gemma4:e2b-it-qat; recheck on gemma4:e4b-it-qat; not cap-cut) · seam: et prompt/model · falsifier (from chipper/): `go run ./pkg/myvector/routeprobe -llm -lang et-EE "räägi mulle muinasjutt robotist" | awk '/^llm: /{exit NF<60}'`
