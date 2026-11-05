# Vector Estonian mode: research, pick, build

**Status**: 🟡 PLANNED 2026-11-05 — Stage 1 next; robot runs wait for the user's go.
**Want**: one switch makes Vector hear, understand, answer and speak Estonian, built-in lines included; English unchanged.
**Parent**: `2026-11-05-vector-voice-research.md` (docs/plans/) — R1–R23 live there; R2, R4, R6 carry over.
**Overlap**: `2026-11-05-vector-voice-verify.md` (docs/plans/) stays QUEUED LAST and checks the English PC voice path; this plan must not wait for it. Its V5 rows 1–2 become Stage 10's English regression.

## Rulings (2026-11-05 questions round)

| id | ruling |
|---|---|
| E1 | Estonian mode sits NEXT TO English: one switch flips STT + intent matching + LLM answer language + T1/T2 voice together; switch = English → behaviour byte-identical to today |
| E2 | Robot built-in lines (time, weather, timer, names… — robot-internal SayText on T2 via patch 0010) are in scope: Estonian mode speaks Estonian TEXT, not just an Estonian voice |
| E3 | One plan: research → listen test → [USER DECISION] pick → build → final stage = `make check` + live e2e on the robot + code-reviewer review |
| E4 | Free only (R2): local, or an online free tier; paid-only options are listed but cannot win. Listen tests per R4 (Vector's speaker, user scores 1–5, `play.sh -v 60`) |
| E5 | Language codes: `en-US`, `et-EE` (wire-pod's `xx-XX` convention); fresh-install default `en-US` |
| E6 | Robot runs (Stage 2, live parts of 9 and 10) start only on the user's explicit go; robot stays docked |

## Seams already known (verify, don't re-derive)

| id | fact | where |
|---|---|---|
| K1 | STT engine = env `STT_SERVICE` (running: vosk, binary per `chipper/cmd/<engine>`); language = env `STT_LANGUAGE` (vosk/whisper.cpp only) else `APIConfig.STT.Language` | `chipper/start.sh`, `vars` config.go |
| K2 | Vosk model at `vars.VoskModelPath/<lang>/model`; `ValidVoskModels` has 14 langs, no `et-EE`; `DownloadVoskModel`, `ReloadVosk` | `stt/vosk/Vosk.go`, `localization` pkg |
| K3 | whisper.cpp takes a language param; Estonian supported by the model family, unmeasured here | `stt/whisper.cpp/WhisperCpp.go` |
| K4 | Web UI offers `en-US` only | `webroot/initial.html`, `setup.html`, `js/initial.js` |
| K5 | Intents load once from `intent-data/<Language>.json` (`LoadIntents`); no `et-EE.json` | `vars.go` |
| K6 | Fork intent filter: `forkAllowedIntents` (8 names), `forkDroppedKeyphrases`, `forkAddedKeyphrases` — all English; `ForkContainsPhrase`, `forkFilterIntents` | `vars/fork_intents.go` + `_test.go` |
| K7 | `localization.GetText` = positional column table `texts` (13 langs) keyed off `vars.APIConfig.STT.Language`; 118 call sites (timer/volume number words, weather `" in "`) | `localization.go` |
| K8 | LLM prompt built in `CreateAIReq` (default "You are a helpful, animated robot called Vector…", override `Knowledge.OpenAIPrompt`, `CreatePrompt`); no language line; chat history kept | `ttr/kgsim.go` |
| K9 | 6 non-test `"en-US"` literals in `chipper/pkg` (one in `kgsim_cmds.go` gates the OpenAI voice) — each must read the switch or be proven inert | `grep -rn '"en-US"' chipper/pkg --include=*.go` |
| K10 | TTS server: `VOICES` = `cori` (Piper), `af_bella` (Kokoro), neither Estonian; polls `chipper/myvectorConfig.json` `{t1,t2,slow}`; `DEFAULT_VOICE` | `tts/server.py`, `tts/tests` |
| K11 | Go voice settings: `Settings{T1,T2,Slow}`, `Voices` (must equal server `VOICES` keys + `stock`; no test pins it today), `Validate`, `Save`, `pushT2` → `voice-push.sh` → robot `/data/data/myvector-voice.json {url,voice,slow}` | `myvector/voice/settings.go` |
| K12 | T1 = `forkSayText` → `voice.Speak`; T2 = patch 0010 in vic-anim; SDK-origin SayText stays Acapela (R19) | `ttr/fork_say.go`, `robot/firmware/0010-pc-voice.patch` |
| K13 | Robot has a locale stack: `RobotSetting.locale` → `SettingsManager::ApplySettingLocale` → `Robot::SetLocale` → `LocaleComponent` + anim `SetLocale` msg → `_ttsComponent->SetLocale` | `robot/work/wire-os-victor/engine/`, `animProcess/src/cozmoAnim/animEngine.cpp` |
| K14 | Robot string tables `resources/assets/LocalizedStrings/{en-US,de-DE}/BehaviorStrings.json` (weather, enroll face, how-old, KG, messages, say-name); clock line = `GetTTSStringForTime` (code, not table) | wire-os-victor `resources/`, `behaviorWallTimeCoordinator.cpp` |

## Refactor-first assessment

Today language is one global read at startup (`APIConfig.STT.Language` → K1, K5, K7; STT binary fixed by `STT_SERVICE`). E1 needs every consumer to read ONE setting. Folded into Stage 3 (the STT pick decides it).

**[USER DECISION] D-RF — how the switch reaches STT/intents/GetText**

| alt | shape | cost | blocks |
|---|---|---|---|
| A | Bolt-on: switch writes `STT.Language` + `STT_SERVICE`, restarts chipper (`sudo` restart from the page) | S; upstream untouched | needs one engine good at both languages, or a restart that changes binary; ~restart gap per switch |
| B | Fork provider `myvector/lang.Current()` (persisted in `myvectorConfig.json`); `fork_*.go` hooks in `LoadIntents`/`GetText`/STT entry consult it per request; a fork STT router holds the en + et engines | M; ~4 drift-listed hook lines | none; live switch, no restart |
| C | B + per-utterance auto-detect (whisper language-ID picks en/et; switch = preferred/forced) | M–L | needs R-a to show reliable en/et ID on short commands |

## Research stages (each = one commit: findings written into §Deliverable)

| id | question | method / output |
|---|---|---|
| 1.1 R-a STT | Lead: `TalTechNLP/whisper-large-v3-turbo-et-verbatim` (CT2 `ct2/` subfolder, faster-whisper cuda float16 = stt-faster preset `et-large`; reuse = copy that config, stt-faster has no server; its WER/latency/VRAM are unmeasured); also Vosk et?, whisper.cpp, free online (TartuNLP ASR, Azure F0, Google) | 50 Common Voice `et` clips (CC0) → `robot/work/et-stt/`; table WER · end-of-speech→text s · RAM/VRAM · offline. Targets: WER ≤ 25 %, ≤ 2.0 s (vosk `en-US` baseline measured alongside) |
| 1.2 R-b TTS | Free Estonian voices: TartuNLP Neurokõne (local model + free API), MMS-TTS `est` (license check), Piper/Kokoro et (expect none), edge-tts `et-EE-AnuNeural` (A5 ToS risk), Azure F0 `et-EE-Anu/Kert`, espeak-ng `et` | add engine adapter(s) to `robot/voice-samples/{gen.sh,synth.py}` (`pkgs[]` + branch); `gen.sh et-<set> "<text>" <engine:voice>…` with a time, a weather, a timer and an LLM sentence; first-audio s on GPU/CPU; VRAM next to Kokoro (R20) |
| 1.3 R-c LLM | Estonian answer quality: `gemma3:4b-it-qat` vs `gemma3:12b`, EuroLLM-9B, Llammas, qwen3; prompt line "Answer in Estonian"; alt: answer in English + free MT (TartuNLP Neurotõlge) | 10 fixed Estonian questions, each scored 1–5 by the user for language + content; first-token s; VRAM fit with STT+TTS; append to `docs/llm-trials.md` |
| 1.4 R-d built-in lines | Which robot lines actually speak (inventory K14 + `CreateUtterance` callers); Estonian text path: D1 robot-side `et-EE` table + `RobotSetting.locale` + patched `GetTTSStringForTime`; D2 patch 0010 sends line key/args + `lang`, PC server renders Estonian; D3 wire-pod answers the 8 allowlisted intents itself via T1 | per option: patch size, what Acapela does with `et-EE` locale (no pack → crash/fallback?), behaviour/face animation kept? Fallback when PC down: English Acapela speaking Estonian text = unacceptable → name the fallback |
| 1.5 R-e switch | Where the toggle lives: extend `/myvector/voice` into language+voice vs new fork page; how STT, intents, GetText, LLM, T1, T2 read one setting; inventory K9 literals + every `STT.Language` reader; live vs restart | table consumer → read site → hook; feeds D-RF |

## Stage 2 — [USER] listen test on Vector (robot run, needs the go)

| id | ruling | edit site |
|---|---|---|
| 2.1 | Play every R-b candidate × the 4 sentences via `play.sh -v 60`; user scores 1–5 per T1 / T2 use | `robot/voice-samples/play.sh` |
| 2.2 | User speaks 10 Estonian commands to Vector ("Mis kell on?", "Pane taimer viieks minutiks", "Mis ilm on Tallinnas?", "Kes ma olen?" + 6 LLM questions); record mic audio, score each STT candidate offline | `robot/work/et-stt/` |

## Stage 3 — [USER DECISION] picks

| pick | options from | shown with |
|---|---|---|
| STT engine (+ en engine stays vosk?) | 1.1 | WER, latency, offline |
| T1 / T2 Estonian voice | 1.2 + 2.1 | listen scores, first audio |
| LLM model + prompt line | 1.3 | scores, VRAM |
| Built-in-line method D1/D2/D3 | 1.4 | patch size, fallback |
| Switch UX + D-RF A/B/C | 1.5 | ASCII page mock |

## Build stages (shape set by Stage 3 where marked)

| id | ruling | edit site | test → `make check` |
|---|---|---|---|
| 4.1 | Lang provider: `lang.Current()` / `lang.Save()`, `"lang"` field in `myvectorConfig.json`, default `en-US`, invalid → `en-US` (shape set by D-RF) | `chipper/pkg/myvector/lang/` | `go test ./pkg/myvector/...` (`TestLangDefault`, `TestLangInvalidFallsBack`) — already in gate |
| 4.2 | Hook `STT.Language` readers (K7 `GetText`, K5 `LoadIntents`, K9 literals) through 4.1 via `fork_*.go`; drift-list each hook line | `vars/fork_lang.go`, `localization/fork_lang.go`, `scripts/upstream-drift.sh` | `go test -run Fork ./pkg/vars/` (`TestForkLangEnglishUnchanged`) + drift check |
| 5.1 | Estonian STT engine wired + model fetch into `robot/work/`-style gitignored path (shape set by STT pick) | `chipper/pkg/wirepod/stt/<engine>/` or fork router `fork_stt.go` | new `go test -run Fork ./pkg/wirepod/stt/...` with a stub engine; add the package to `scripts/check.sh` |
| 6.1 | `et-EE` intent table for the 8 `forkAllowedIntents`; per-language `forkDroppedKeyphrases`/`forkAddedKeyphrases` (`map[lang]…`); Estonian stem matching in `ForkContainsPhrase` (kell/kella/kellaaeg) | `vars/fork_intents.go`, `chipper/intent-data/et-EE.json` | `TestForkIntentsEstonian` (10 Stage 2.2 phrases → intent), English table test unchanged |
| 6.2 | Estonian `GetText` words (numbers for timer, weather locative "Tallinnas") via fork override, not a 14th upstream column | `localization/fork_texts.go` | `TestForkTimerEstonian` ("viieks minutiks" → 300 s), `TestForkWeatherCityEstonian` |
| 7.1 | LLM language line appended in `CreateAIReq` when `lang = et-EE`; model per Stage 3 | `ttr/fork_prompt.go` (hook in kgsim.go, drift-listed) | `go test -run Fork ./pkg/wirepod/ttr/` (`TestForkPromptEstonian`, `TestForkPromptEnglishUnchanged`) |
| 8.1 | TTS server: Estonian engine + voice key(s) in `VOICES`, gain to -14 LUFS (R23, `tts/calibrate.py`), Estonian sentence split (shape set by voice pick) | `tts/server.py`, `tts/gains.json`, `tts/requirements.txt` | pytest `tts/tests` (stub engine) + live `tts/smoke.sh` |
| 8.2 | Per-language T1/T2 in settings (`Settings` gains et pair; `Voices` per lang); page gets the language switch (shape set by UX pick) | `myvector/voice/settings.go`, `settings.html` | `TestSettingsValidate` + `TestSettingsSaveRoundTrip` extended; add pytest `test_voices_match_go` (none today) |
| 9.1 | Built-in lines in Estonian (shape set by D1/D2/D3): D1/D2 = new patch `0011-*.patch` + `series` + `markers.tsv`; push `lang` in `myvector-voice.json` via `voice-push.sh` | `robot/firmware/`, `voice-push.sh`, `myvector/voice/` | `robot/firmware/check-static.sh`; host test `robot/firmware/tests/et-lines.sh` added to `scripts/check.sh`; `TestPushT2Args` extended |
| 9.2 | [USER] live: apply patch, `make robot-test` (robot/firmware/smoke.sh); "Mis kell on?" in Estonian | robot | smoke exit 0 |

## Stage 10 — verify + e2e + review

| id | ruling | edit site |
|---|---|---|
| 10.1 | `make check` green | `scripts/check.sh` |
| 10.2 | [USER] English regression (switch = en-US): LLM question + "what time is it" in `af_bella`, timer set | robot |
| 10.3 | [USER] Estonian (switch = et-EE): "Mis kell on?" (built-in, Estonian text + voice), one LLM question answered in Estonian, "Pane taimer viieks minutiks" fires; then flip back to English without restart (if D-RF B/C) | robot |
| 10.4 | `Agent(code-reviewer)` over the plan's full diff; fix, then archive per `plan-hygiene.md` | — |

## Deliverable

Filled by Stages 1.1–1.5: one ranked table per component (STT, T1, T2, LLM, built-in lines, switch), columns `rank · option · free · latency · score · verdict`.
