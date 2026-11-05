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
| E7 | EN or ET stack loaded, never both; each stack (STT + LLM + TTS) ≤ 8 GB VRAM (RTX 3070 Laptop); a candidate that cannot fit its stack is listed, not measured |
| E8 | Switch is manual only — no language auto-detect (D-RF C dropped) |
| E9 | STT test audio: FLEURS `et_ee` test (CC-BY-4.0, open) now; Common Voice `et` once the user downloads it from Mozilla Data Collective into `robot/work/et-stt/cv/`; Stage 2.2 commands rescored before Stage 3 |
| E10 | Keyed free tiers (Azure F0, Google) DROPPED: Azure F0 signup wants a credit card, Google STT a paid billing account — fail E4 |
| E11 | ET-mode LLM = EstLLM 8B, the only ET model (user pick); quant = largest `i1-*` passing the 1.3 ET stack test (Q4_K_M + Q4_K_S first; Q5_K_S only if Q4_K_M passes; Q6_K skipped); spill = no fit, speed reported only (user: full-VRAM speed is ok); blind scoring + gemma4-for-ET dropped |
| E12 | EN-mode LLM re-pick (user ask): gemma3:4b baseline vs `qwen3.5:4b`, `ministral-3:8b`, `gemma4:e2b-it-qat`, `gemma4:e4b-it-qat` via `bench.py --lang en`; af_bella must fit beside it |

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

## Research stages (each = one commit: findings written into §Deliverable)

| id | question | method / output |
|---|---|---|
| 1.1 R-a STT | Lead: `TalTechNLP/whisper-large-v3-turbo-et-verbatim` (CT2 `ct2/` subfolder, faster-whisper cuda float16 = stt-faster preset `et-large`; reuse = copy that config, stt-faster has no server; its WER/latency/VRAM are unmeasured); also Vosk et?, whisper.cpp, free online (TartuNLP ASR, Azure F0, Google) | harness `robot/et-stt/` (tracked), data/results `robot/work/et-stt/`; 50 FLEURS `et_ee` clips (+ CV `et` when present, E9); table WER · end-of-speech→text s · RAM/VRAM · offline. Targets: WER ≤ 25 %, ≤ 2.0 s (vosk `en-US` baseline measured alongside) |
| 1.2 R-b TTS | Free Estonian voices: TartuNLP Neurokõne (local model + free API), MMS-TTS `est` (license check), Piper `et_EE-news-medium`, Kokoro et (none), edge-tts `et-EE-AnuNeural` (A5 ToS risk), Azure F0 `et-EE-Anu/Kert`, espeak-ng `et` | add engine adapter(s) to `robot/voice-samples/{gen.sh,synth.py}` (`pkgs[]` + branch); `gen.sh et-<set> "<text>" <engine:voice>…` with a time, a weather, a timer and an LLM sentence; first-audio s on GPU/CPU; VRAM next to Kokoro (R20) |
| 1.3 R-c LLM | Ollama 0.40.14. EN fit = `bench.py` delta (no spill). ET fit = new `robot/et-llm/stack.py`: quant loaded + one warm-up generation, then TalTech STT clip (`robot/work/et-stt/venv`, faster-whisper `int8_float16`), then LLM generating ∥ Neurokõne sentence (`venv-tartulocal`, `synth.py tartulocal`), nvidia-smi 100 ms peak over all; fit = no spill ∧ peak − base + 2389 (worst base seen) + 256 ≤ 8192 | `robot/et-llm/run.sh`-style `bench.py [--lang en] gen` → `score.py en` (blind 1–5) → `bench.py --lang en summary`; data `robot/work/et-llm/<lang>/`; append the EN pick to `docs/llm-trials.md` |
| 1.4 R-d built-in lines | Which robot lines actually speak (inventory K14 + `CreateUtterance` callers); Estonian text path: D1 robot-side `et-EE` table + `RobotSetting.locale` + patched `GetTTSStringForTime`; D2 patch 0010 sends line key/args + `lang`, PC server renders Estonian; D3 wire-pod answers the 8 allowlisted intents itself via T1 | per option: patch size, what Acapela does with `et-EE` locale (no pack → crash/fallback?), behaviour/face animation kept? Fallback when PC down: English Acapela speaking Estonian text = unacceptable → name the fallback |
| 1.5 R-e switch | Where the toggle lives: extend `/myvector/voice` into language+voice vs new fork page; how STT, intents, GetText, LLM, T1, T2 read one setting; inventory K9 literals + every `STT.Language` reader; live vs restart | table consumer → read site → hook; feeds D-RF |

## Stage 2 — [USER] listen test on Vector (robot run, needs the go)

| id | ruling | edit site |
|---|---|---|
| 2.1 | DONE: blind `et-llm` ×18 → liked edge Anu, Piper Mari, Neurokõne mari, Neurokõne vesta; those 4 × time/weather/timer → Piper Mari + Neurokõne mari (T1 and T2) | `robot/voice-samples/play.sh` |
| 2.2 | DONE: `robot/et-stt/mic-commands.txt` spoken to Vector, captured by `myvector/miccap` (on while `robot/work/et-stt/mic/` exists), scored set moved to `mic-et/`; `run.sh` `*-mic` rows | `robot/work/et-stt/` |

## Stage 3 — [USER DECISION] picks

| pick | options from | shown with |
|---|---|---|
| STT engine (+ en engine stays vosk?) | 1.1 | WER, latency, offline |
| T1 / T2 Estonian voice | 1.2 + 2.1 | listen scores, first audio |
| EN LLM model + ET quant (E11) | 1.3 / E12 | EN scores, VRAM headroom, first sentence / total s |
| Built-in-line method D1/D2a/D2b/D3 | 1.4 | patch size, fallback |
| Switch UX + D-RF A/B | 1.5 | ASCII page mock |

## Build stages (shape set by Stage 3 where marked)

| id | ruling | edit site | test → `make check` |
|---|---|---|---|
| 4.1 | Lang provider: `lang.Current()` / `lang.Save()`, `"lang"` field in `myvectorConfig.json`, default `en-US`, invalid → `en-US` (shape set by D-RF) | `chipper/pkg/myvector/lang/` | `go test ./pkg/myvector/...` (`TestLangDefault`, `TestLangInvalidFallsBack`) — already in gate |
| 4.2 | Hook `STT.Language` readers (K7 `GetText`, K5 `LoadIntents`, K9 literals) through 4.1 via `fork_*.go`; drift-list each hook line | `vars/fork_lang.go`, `localization/fork_lang.go`, `scripts/upstream-drift.sh` | `go test -run Fork ./pkg/vars/` (`TestForkLangEnglishUnchanged`) + drift check |
| 5.1 | Estonian STT engine wired + model fetch into `robot/work/`-style gitignored path (shape set by STT pick) | `chipper/pkg/wirepod/stt/<engine>/` or fork router `fork_stt.go` | new `go test -run Fork ./pkg/wirepod/stt/...` with a stub engine; add the package to `scripts/check.sh` |
| 6.1 | `et-EE` intent table for the 8 `forkAllowedIntents`; per-language `forkDroppedKeyphrases`/`forkAddedKeyphrases` (`map[lang]…`); Estonian stem matching in `ForkContainsPhrase` (kell/kella/kellaaeg) | `vars/fork_intents.go`, `chipper/intent-data/et-EE.json` | `TestForkIntentsEstonian` (10 Stage 2.2 phrases → intent), English table test unchanged |
| 6.2 | Estonian `GetText` words (numbers for timer, weather locative "Tallinnas") via fork override, not a 14th upstream column | `localization/fork_texts.go` | `TestForkTimerEstonian` ("viieks minutiks" → 300 s), `TestForkWeatherCityEstonian` |
| 7.1 | LLM language line appended in `CreateAIReq` when `lang = et-EE`; model per Stage 3; send `ReasoningEffort: "none"` only when `Provider == "custom"` (Ollama; qwen3.5/gemma4 think by default — OpenAI rejects it on non-reasoning models) | `ttr/fork_prompt.go` (hook in kgsim.go, drift-listed) | `go test -run Fork ./pkg/wirepod/ttr/` (`TestForkPromptEstonian`, `TestForkPromptEnglishUnchanged`, `TestForkPromptOpenAIOmitsReasoning`) |
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

### Switch (1.5 R-e)

| rank | option | free | latency | score | verdict |
|---|---|---|---|---|---|
| 1 | D-RF B: `myvector/lang.Current()` (atomic cache, path injected — `lang` must not import `vars`) + `lang.Switch` swaps ollama via HTTP `keep_alive` and pins ET STT/TTS through `myvectorConfig.json` `lang`; STT via `forkSTTRouter` (`preqs/fork_stt.go`); toggle = `/myvector/voice` grown to "Language & voice" | yes | chipper 0 s; model swap ~15–60 s (unmeasured) | 4 upstream hook lines: `vars.go`, `kgsim.go` (listed), `localization.go`, `preqs/server.go` (new drift entries) | lead |
| 2 | D-RF A: write `STT.Language` + `STT_SERVICE`, restart chipper | yes | + ~20–60 s `go run` restart (unmeasured) | same GetText/LLM hooks + `start.sh`/`source.sh`; `et-EE` in `STT.Language` breaks `vosk.Init` | reject |

### Built-in lines (1.4 R-d)

Speaking lines under the 8 intents: clock (`GetTTSStringForTime`, digits), weather (`BehaviorDisplayWeather.*`, `{0}` = temp), names/enroll (name + `AlreadyKnow*`, `DontKnow`, `HaveWeMetBefore`), KG `Ready`; timer/photo speak nothing.

| rank | option | free | latency | score | verdict |
|---|---|---|---|---|---|
| 1 | D2a: 0010 adds `lang` (~6 robot lines); PC server maps English template → Estonian (keys from en-US `BehaviorStrings.json`) + et number/time normaliser (~100 py lines); set `clock_24_hour` in et mode | yes | same as T2 today | animations kept; PC down → Acapela English text (R17) | lead |
| 2 | D2b: 0010 sends key + args + `lang` (~10 files, ~120 lines) | yes | same | animations kept | only if D2a templates are ambiguous |
| 3 | D3: wire-pod speaks clock/weather via T1 (~120 Go lines) | yes | T1 | loses face clock + weather anims; no names/enroll | stopgap |
| 4 | D1: robot `et-EE` table + `RobotSetting.locale` + rebuild | yes | — | `et-EE` locale nulls Acapela (`_BAB_Obj`, no voice pack) → all Acapela incl. R19 + T1 fallback silent (source-traced, unrun) | reject |

### STT (1.1 R-a)

50 shortest FLEURS `et_ee` test clips (median 7.05 s) + 50 shortest Common Voice 27.0 `et` test clips (median 2.86 s), beam 5; re-run (CV / keys): `robot/et-stt/run.sh [NAME…]`, view `robot/work/et-stt/venv/bin/python -I robot/et-stt/bench.py summary`.

| rank | option | free | latency med/p90 | score (WER · VRAM peak) | verdict |
|---|---|---|---|---|---|
| 1 | TalTech `whisper-large-v3-turbo-et-verbatim`, faster-whisper cuda `int8_float16` | yes, offline | 0.36 / 0.44 s (CV 0.30 / 0.38) | 8.5 % (CV 13.7 %, Vector mic 2.6 %) · 1.31 GB | lead |
| 2 | same, cuda `float16` (stt-faster `et-large`) | yes, offline | 0.38 / 0.43 s (CV 0.32 / 0.39) | 7.6 % (CV 13.7 %, Vector mic 2.6 %) · 2.28 GB | +1 GB for −0.9 pt FLEURS, 0 pt CV |
| 3 | stock large-v3-turbo cuda `int8_float16` via faster-whisper (proxy for whisper.cpp, not run) | yes, offline | 0.39 / 0.46 s | 18.1 % (CV 39.5 %, Vector mic 25.6 %) · 1.31 GB | 2–3× TalTech WER |
| 4 | TalTech CPU `int8` | yes, offline | 7.15 / 7.69 s | 8.5 % · – (CPU) | fails 2.0 s |
| – | Vosk et · TartuNLP/TalTech keyless API | — | — | — | no model · no public API |
| – | Azure F0 · Google v1 `et-EE` | card / paid billing | — | — | dropped (E10) |
| base | Vosk `en-US` on FLEURS `en_us` | yes, offline | 0.02 / 0.04 s | 31.6 % (en) · CPU | today's English |

E7 fit: gemma3:4b 5.8 GB + STT 1.31 GB leaves ≤ 0.9 GB for ET TTS + desktop — R-b/R-c size against it.

### TTS (1.2 R-b)

Numbers unsaved (R-b terminal; GPU shared with a sibling job, VRAM = torch running peak, first audio = full synth for non-streaming) — re-measure on an idle GPU with `BENCH=1` (→ `bench.log`) before Stage 3. Samples: `robot/work/voice-samples/et-*/norm/`.

| rank | option | free | first audio GPU / CPU (short · llm) | score (VRAM · license) | verdict |
|---|---|---|---|---|---|
| 1 | TartuNLP Neurokõne local (`tartulocal:`, 10 speakers) | yes, offline | 0.61 · 1.02 / 1.1 · 2.3 s | 0.12–0.41 GB · worker MIT, weights unstated | co-lead; trim ~0.5 s lead silence |
| 1 | Piper `et_EE-news-medium` (Mari/Vesta/Albert/Kalev) | yes, offline | – / 0.10 · 0.20 s | 0 · data CC-BY, piper GPL-3 | co-lead |
| 3 | edge-tts `et-EE-Anu/KertNeural` | keyless, online | 1.1 · 1.2 s | 0 · unofficial endpoint (A5 ToS) | backup only |
| 4 | XTTS-v2-est (`tartuNLP/XTTS-v2-est`) | yes, offline | 0.78 · 0.73 / 3.4 · 4.1 s | ~2.3 GB · CPML non-commercial | over E7, quality reference |
| 5 | espeak-ng `et` | yes, offline | – / 0.01 · 0.02 s | 0 · GPL-3 | robotic baseline |
| ref | Kokoro `af_bella` (English, R20; same run) | yes, offline | 1.45 s (GPU) | 0.65 GB · Apache-2.0 | today's English |
| – | Neurokõne API · Azure F0 · MMS `est` · Kokoro et | — | — | — | HTTP 408 ×3 (retry `tartu:mari`) · dropped (E10) · no model · no voice |

### LLM (1.3 R-c)

Ollama 0.40.14, `reasoning_effort: "none"`; view `bench.py --lang <lang> summary`. Base (Ollama unloaded, TTS server up) seen 1318–2389 MiB, Windows alone 1144 — ET rows await `stack.py`. EN headroom = total − LLM − 2389 − 256. Rows predate the warm-up (max = cold first question): re-run `run.sh en` before Stage 3.

| rank | option | free | latency (first sentence med / max · total med) | score (LLM GiB · headroom) | verdict |
|---|---|---|---|---|---|
| ET | EstLLM 8B `i1-Q4_K_M` | yes, offline | 0.20 / 27.77 · 1.46 s | 5.06 · −128 MiB (old formula) | stack test |
| ET | EstLLM 8B `i1-Q4_K_S` | yes, offline | 0.22 / 9.55 · 1.23 s | 4.85 · +45 MiB (old formula) | stack test |
| EN | gemma4:e2b-it-qat | yes, offline | 0.29 / 0.90 · 0.35 s | 2.74 · 2740 | user score |
| EN | gemma4:e4b-it-qat | yes, offline | 0.43 / 1.42 · 0.53 s | 4.14 · 1304 | user score |
| EN | qwen3.5:4b | yes, offline | 0.69 / 1.89 · 1.22 s | 3.71 · 1753 | user score |
| ref | gemma3:4b-it-qat (today's EN) | yes, offline | 0.16 / 8.53 · 0.41 s | 4.32 · 1120 | EN baseline |
| – | ministral-3:8b | yes, offline | 0.66 / 9.43 · 5.12 s | 5.44 · spill | out (CPU spill) |
