# Vector voice: research every way to a softer, clearer voice

**Status**: OPEN — research only; output = a ranked pick + the plan stage that builds it.
**Want**: easier on the ear and easier to understand than the stock robot voice; user leans to a soft female voice.

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

Ranked table of A1–A8 against the criteria, top pick + runner-up, then a build stage
(`id | ruling | edit site`) appended here. Run A1 first: it costs nothing and may be enough.
