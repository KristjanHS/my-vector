# my-vector

my-vector gives the Anki (now Digital Dream Labs) [Vector](https://web.archive.org/web/20190417120536if_/https://www.anki.com/en-us/vector) robot a large-language-model brain, PC-generated voices and Estonian speech recognition. It is built on top of [wire-pod](https://github.com/kercre123/wire-pod), the open-source Vector server.

<!-- TODO: 30-second robot video (slot 6) -->

## What my-vector adds

- **LLM answers** — each language picks its own brain, a local Ollama model or Claude, configured from the web UI (`chipper/pkg/myvector/llm`, trial notes in [docs/llm-trials.md](docs/llm-trials.md)).
- **PC voices** — Vector speaks LLM answers and its built-in lines through a local TTS service (Kokoro and Piper voices, Estonian voices included), falling back to the robot's own voice when the service is down (`tts/`, `chipper/pkg/myvector/voice`).
- **Estonian speech recognition** — a faster-whisper STT service runs TalTech's Estonian model on the GPU, with an English model for English; the language is switched from the web UI (`et-stt/`, `chipper/pkg/myvector/lang`).
- **Live answers** — weather, news and other current questions are routed to web search (Tavily, or a local SearXNG) and a weather forecast before the LLM answers (`chipper/pkg/myvector/search`).
- **Picture questions** — ask "what is in this picture?" and Vector describes what its camera sees; follow-up questions reuse the last photo (`chipper/pkg/myvector/photo`).

## Quickstart

The PC side runs from the [Makefile](Makefile); `make help` lists every target. In short: set up and enable the TTS and STT services (`make tts-setup`, `make tts-enable`, `make stt-setup`, `make stt-enable`), then install wire-pod as a service with `make daemon-enable` and start it with `make start`. The robot still needs wire-pod's own setup first — see [Base: wire-pod](#base-wire-pod).

## How it was built

<!-- TODO: stats block (slot 4 generator) + one hand-written paragraph -->

Method and agent configuration: [claudeconf](https://github.com/KristjanHS/claudeconf).

## Base: wire-pod

my-vector is a fork of `wire-pod`, fully-featured server software for Vector created thanks to Digital Dream Labs' [open-sourced code](https://github.com/digital-dream-labs/chipper). wire-pod lets voice commands work with any Vector 1.0 or 2.0 for no fee, including regular production robots.

- Installation: wire-pod's [installation guide](https://github.com/kercre123/wire-pod/wiki/Installation)
- Wiki: the [wire-pod wiki](https://github.com/kercre123/wire-pod/wiki) covers what wire-pod is, installing it, troubleshooting, developing for it, and general tips

## Credits

- [wire-pod](https://github.com/kercre123/wire-pod) by [Kerigan Creighton (kercre123)](https://github.com/kercre123) — the server my-vector is built on
- [Digital Dream Labs](https://github.com/digital-dream-labs) for open sourcing chipper and creating escape pod (which made wire-pod possible)
- [bliteknight](https://github.com/bliteknight) for making wire-pod more accessible with his easy-to-use pre-setup Linux boxes
- [dietb](https://github.com/dietb) for rewriting chipper and giving tips
- [fforchino](https://github.com/fforchino) for adding many features such as localization and multilanguage, and for helping out
- [xanathon](https://github.com/xanathon) for the publicity and web interface help
- Anyone who has opened an issue and/or created a pull request for wire-pod

## License

MIT — see [LICENSE](LICENSE) (upstream wire-pod copyright retained).
