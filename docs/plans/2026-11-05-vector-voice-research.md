# Vector voice: research every way to a softer, clearer voice

**Status**: OPEN — B1–B4 built; next = B5 live verify (after `make ship`).
**Want**: easier on the ear and easier to understand than the stock robot voice; user leans to a soft female voice.

## Rulings (2026-11-05 questions round)

| id | ruling |
|---|---|
| R1 | English only; Estonian voice = its own follow-up research plan, authored when this one closes |
| R2 | Free only — local, or an online API's free tier; paid-only options are listed but cannot win |
| R3 | Two separate picks, never tied: T1 = LLM-answer voice, T2 = built-in robot voice (time, weather…) → soft female |
| R4 | Listen tests live on Vector's speaker; user scores 1–5; tools = `robot/voice-samples/{gen,play}.sh` |
| R5 | A1 out: stock voice scored 1–2 in every combo; slower helps → test candidates at `gen.sh -s` > 1 |
| R6 | Sample volume: `play.sh -v 60` (SayText at master 2 was a bit loud) |
| R7 | Listen favourites: `piper:en_GB-cori-high` (public domain) and `kokoro:af_bella` (Apache-2.0) |
| R8 | Both favourites selectable in the solution, per track: T1 and T2 each pick `cori` · `af_bella` · `stock` |
| R9 | Fallback voice (new voice unavailable) = A1 combo Four: `UseVectorVoice: false`, `DurationScalar: 1.2` |
| R10 | Voice picker = fork page `/myvector/voice` + one drift-listed link line in upstream `index.html` |
| R11 | Fresh-install slow factor = 1.2 (range 0.5–2.0, the TTS server's clamp) |
| R12 | Slow drives `stock` too: T1 `stock` speaks `SayText(UseVectorVoice: true, DurationScalar: slow)` (B3) |
| R13 | T1 voice covers `DoSayText` (LLM answers) only; `KGSim` server lines, camera countdown, face message stay stock |
| R14 | `/myvector/voice` gets "Test on Vector": speaks editable sample text in the page's unsaved voice + slow via `voice.Speak`, shows the voice that spoke |
| R15 | T2 setting reaches the robot as `/data/data/myvector-voice.json {url, voice, slow}`, pushed over SSH by `robot/firmware/voice-push.sh` on page save + at ship (`settings.sh`) |
| R16 | T2 shares the one slow slider; robot sends `slow` = saved slow × its own per-line `durationScalar` |
| R17 | Robot POSTs one sentence per request (1.5 s to first byte, `kMyVectorSentence_ms` cap); after a failure the rest of the line speaks in Acapela |
| R18 | PC URL = new `ROBOT_TTS_URL` in `robot/robot.env` (e.g. `http://<pc-lan-ip>:8091`); file missing/bad or voice `stock` → Acapela |
| R19 | Patch 0010 skips wire-pod/SDK-originated SayText (R12/R13 stand): those stay Acapela; only robot-internal lines use T2 |

## Seams already known (verify, don't re-derive)

| id | fact | where |
|---|---|---|
| K1 | LLM answers speak via `robot.Conn.SayText(UseVectorVoice: true, DurationScalar: 0.95)` | `DoSayText` (kgsim_cmds.go) |
| K2 | Non-robot voice path: server TTS → 16 kHz PCM → `ExternalAudioStreamPlayback`, `AudioVolume: 100` | `DoSayText_OpenAI` |
| K3 | K2 only fires for provider `openai` + non-English, or `OpenAIVoiceWithEnglish`; it reuses `Knowledge.Key`, so it fails with Ollama/Claude | `DoSayText` |
| K4 | K2 buffers the whole clip (`io.ReadAll`) before playing → latency grows with answer length | `DoSayText_OpenAI` |
| K5 | Built-in intents (time, weather, …) speak from the robot's own TTS, not wire-pod — a server-side voice only covers LLM answers | to verify on robot |
| K6 | Master volume sits at interim 2 (target 4) | `2026-11-04-vector-speech-volume.md` (docs/plans/archive/) |

## Alternatives to research

| id | option | questions to answer |
|---|---|---|
| A1 | Stock voice, tuned: `DurationScalar` slower, `UseVectorVoice: false` (SDK's generic human voice), master volume 4 | does any combo fix it at zero cost? record a listen test per combo |
| A2 | Other voices already on the robot (Acapela language packs in the firmware) | which voice files ship in WireOS; can `vic-anim` load an English female one? |
| A3 | OpenAI TTS (`nova`, `shimmer`; `gpt-4o-mini-tts` takes a style instruction like "soft, warm") | cost per answer; latency; fork change to use a separate OpenAI key with any LLM |
| A4 | Other cloud TTS: ElevenLabs, Azure Neural (e.g. Jenny/Aria), Google, Amazon Polly | free tier, price, streaming PCM support, voice quality at 16 kHz |
| A5 | Free online Microsoft neural voices via `edge-tts` | terms-of-use risk, reliability, latency |
| A6 | Local TTS on this PC: Piper, Kokoro-82M, others (XTTS, F5) | quality vs latency on CPU/GPU, female voices, runs in WSL or Windows, license |
| A7 | Whole-robot voice swap (also covers K5): replace/redirect the robot's TTS in firmware | feasibility on WireOS; does any community project do it |
| A8 | Community prior art (wire-pod forks, plugins, Discord/Reddit) | anything already built for custom TTS on Vector |

## Evaluation criteria

Listen test on Vector's speaker (not headphones) · first-audio latency · cost/month at a few answers a day ·
works with every LLM provider · offline/private · fork code size (`fork_*.go` only?) · face animation while speaking.

## Deliverable

### T1 — LLM-answer voice (ranked)

| rank | id | option | free | first audio | listen | verdict |
|---|---|---|---|---|---|---|
| 1 | A6 | Kokoro `af_bella` (local) | yes | 0.38 s GPU · 3.8 s CPU / 14 s clip | H ★ | pick (selectable) |
| 1 | A6 | Piper `cori-high` (local) | yes | 1.5 s CPU / 14 s clip | B ★ | pick (selectable) |
| 3 | A4 | Azure Neural F0 (0.5M chars/mo) | yes, key | 0.5–1 s | D–F (same voices) not preferred | runner-up, ToS-clean |
| 4 | A5 | edge-tts | yes, no key | 0.65 s | D–F not preferred | out: undocumented endpoint |
| – | A1 | stock tuned | yes | 0 | 1–2 | out (R5) |
| – | A3 | OpenAI TTS | no | – | – | out (R2) |
| – | A4 | ElevenLabs free (10k credits ≈ 30 answers/mo) · Polly (new accounts: credits only) | partly | – | – | out: limit |

### T2 — built-in robot voice (ranked)

| rank | id | option | effort | verdict |
|---|---|---|---|---|
| 1 | A7 | vic-anim patch 0010: `TextToSpeechProviderImpl::GetFirstAudioData` fetches PCM from the PC service, Acapela fallback on error/timeout | M | pick — same voices as T1 (R8) |
| 2 | A7 | same patch, espeak-ng `en+f*` on the robot | S–M | runner-up: offline but robotic |
| – | A2 | `tts_config.json` remap/tune (only female = Japanese Sakura) | S | out: no English female |
| – | A2 | licensed Acapela English female | – | out: paid, license-gated |
| – | A8 | prior art | – | none: nobody has replaced the on-robot voice |

### Build stages

| id | ruling | edit site |
|---|---|---|
| B1 | PC TTS service on :8091 (guard: loopback + Vector): `POST /tts {text, voice, slow}` → chunked 16 kHz s16le PCM, one chunk per sentence; `cori`, `af_bella` warm at start; Kokoro GPU, CPU fallback; fixed per-voice gain to -20 LUFS + peak limit; non-root unit; stubbed pytest in `make check` + live `tts/smoke.sh` | `tts/`, `scripts/myvector-tts.service`, `scripts/wire-pod-guard.nft`, `Makefile` `tts-*` |
| B2 | T1 voice setting (`cori`·`af_bella`·`stock`) + slow factor, stored in fork config, selectable in the web UI | `chipper/pkg/myvector/voice/` + `fork_*.go` glue in `sdkapp` |
| B3 | T1 playback: `DoSayText` → `voice.Speak(voice.Current())` (non-`stock` → `voice.Say` → B1 stream; B1 down → R9; `stock` → R12) + R14 demo endpoint `voice.DemoAPI` | `chipper/pkg/myvector/voice/`; `ttr/fork_say.go` (hook in kgsim_cmds.go, drift-listed); `sdkapp/fork_voice.go` |
| B4.1 | T2 field + picker (R8, R16) and push on save (R15) | `chipper/pkg/myvector/voice/`, `robot/firmware/voice-push.sh`, `settings.sh`, `robot/robot.env.example` |
| B4.2 | patch 0010: vic-anim provider reads R15's file per line, POSIX-socket HTTP/1.0 `POST /tts` per sentence (R17), Acapela fallback (R18); robot `curl` :8091/health first — a Windows firewall rule may be owed | `robot/firmware/0010-*.patch`, `series`, `markers.tsv` |
| B4.3 | R19: SDK-origin SayText bypasses the PC path — prefer a robot-side origin flag (engine → anim TTS request), else a wire-pod text tag the patch strips | `0010-pc-voice.patch`; if tag: voice pkg / `fork_*.go` |
| B5 | Verify: listen on Vector per track × voice; LLM answer + "what time is it" both soft female; B1 stopped → stock voice; "Hey Vector" mid PC sentence cuts it (else: cancel `forkSayText` ctx from the KGSim interrupt); gaps between sentences acceptable (else: prefetch the next chunk); time an LLM answer T2 stock vs cori, PC up/down — slower ⇒ KG placeholder line (`streamingKG` "bla bla") gets sdkOrigin too | `robot/voice-samples/play.sh`, `make robot-test` |
