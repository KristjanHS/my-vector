# Vector voice: research every way to a softer, clearer voice

**Status**: OPEN — research done (rankings below); next = build stages B1–B5.
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
| B1 | PC TTS service: `POST /tts {text, voice, slow}` → 16 kHz s16le PCM per sentence; voices `cori`, `af_bella`; models warm at start | `tts/server.py` (reuses `robot/voice-samples/synth.py` engines) + `scripts/myvector-tts.service` |
| B2 | T1 voice setting (`cori`·`af_bella`·`stock`) + slow factor, stored in fork config, selectable in the web UI | `chipper/pkg/myvector/voice/` + `fork_*.go` glue in `sdkapp` |
| B3 | T1 playback: non-`stock` → B1 → K2 stream, sentence by sentence (fixes K4); B1 down → stock SayText | `fork_*.go` beside `DoSayText` (kgsim_cmds.go — drift-listed hook) |
| B4 | T2: patch 0010 in vic-anim provider calls B1 with the T2 voice; 1.5 s timeout → Acapela; robot→PC port must pass `wire-pod-guard.nft` (:8080 is PC-only) | `robot/firmware/0010-*.patch`, `series` |
| B5 | Verify: listen on Vector per track × voice; LLM answer + "what time is it" both soft female; B1 stopped → stock voice | `robot/voice-samples/play.sh`, `make robot-test` |
