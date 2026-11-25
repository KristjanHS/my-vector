# my-vector

A Vector robot server that answers in Estonian and English with LLMs.

[![Docker](https://github.com/KristjanHS/my-vector/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/KristjanHS/my-vector/actions/workflows/docker-publish.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

my-vector is a fork of [wire-pod](https://github.com/kercre123/wire-pod), the open-source server for the Anki [Vector](https://web.archive.org/web/20190417120536if_/https://www.anki.com/en-us/vector) robot. It adds LLM answers, PC voices, Estonian speech recognition and a long-term memory.

## Quick start

Prerequisites: Linux or WSL2 with systemd, Go (version in `chipper/go.mod`; `setup.sh` installs one if none is found), an NVIDIA GPU, [Ollama](https://ollama.com), and a robot already set up on wire-pod ([docs/upstream.md](docs/upstream.md)).

```bash
git clone https://github.com/KristjanHS/my-vector.git && cd my-vector
sudo ./setup.sh
ollama pull gemma4:e4b-it-qat
make tts-setup tts-enable stt-setup stt-enable
make daemon-enable
```

`make help` lists every other target.

## What it does

| Feature | Where |
|---|---|
| Each language picks its own LLM (local Ollama or Claude) behind an editable prompt template. Current questions go to web search and a weather forecast first, and Vector can describe what its camera sees ([trial notes](docs/llm-trials.md)) | `chipper/pkg/myvector/llm`, `prompt`, `search`, `photo` |
| Speech in and out: Kokoro and Piper voices from a local TTS service, falling back to the robot's own voice, and faster-whisper STT with TalTech's Estonian Whisper model on the GPU ([Estonian models](docs/estonian-models.md)) | `tts/`, `et-stt/`, `chipper/pkg/myvector/voice`, `lang` |
| Long-term memory: a nightly pass pulls facts from finished conversations, merges them, rebuilds a short profile and keeps a diary. A guard reverts the pass if a fixed question loses its answer | `chipper/pkg/myvector/memory` |
| Follow-up listening: after each answer a yes/no call decides whether Vector listens again without the wake word, up to a capped chain | `chipper/pkg/myvector/followup` |
| Robot firmware: WireOS plus a patch series that keeps Vector on the charger and still answering "Hey Vector". Saying "shut up" or `make quiet` locks every robot sound path | `robot/unlock/`, `robot/firmware/`, `robot/quiet/` |
| Network: a Tailscale tunnel on the robot, a replaced root SSH key, per-robot host keys, the server's own CA, a robot firewall, and a WiFi page that roams between saved networks | `robot/tailscale/`, `robot/netguard/`, `robot/sshkey/`, `robot/hostkey/`, `chipper/pkg/myvector/wifi` |

## How it works

The robot sends each voice request to chipper, wire-pod's Go server. chipper hands speech to the STT service (`et-stt/`, port 8092), sends the text to the LLM, and speaks the answer through the TTS service (`tts/`, port 8091).

The main design decision is where the fork ends. New code lives in `chipper/pkg/myvector` (32 packages) and reaches into upstream packages through added `fork_*.go` files. Only 15 upstream wire-pod files under `chipper/` and `vector-cloud/` are edited, and `make check` fails when an edit lands outside the list in `scripts/upstream-drift.sh`. That keeps merges from wire-pod small. Components and data flow: [docs/architecture.md](docs/architecture.md).

## How it was built

I built my-vector with coding agents. Every change starts as a written plan with my rulings in it, is built in small committed stages with tests, and is checked by a separate reviewer agent before the plan is archived. What went well: the plans made long work resumable from one session to the next, and the reviewer, reading a whole stage range cold, caught what green stage tests could not: a memory fact I had removed on the settings page could still reach the model during the nightly pass. What the agents got wrong was mostly settling too early. One blamed an animation crash on memory corruption and archived the hunt as "no repro"; when the crash came back, a re-read of the call site found a soft-float library calling a hard-float libc function with no bridge. Another picked a news-answer opener because it won the replay, accepting that it credited items to the wrong outlet; I rejected it and asked for new arms. And a one-language helper for the camera model would have stayed beside the general setting if I had not ruled it deleted.

- [Hard debugging: the crash the agent blamed on memory corruption](docs/how-it-was-built.md#hard-debugging-the-crash-the-agent-blamed-on-memory-corruption)
- [Design rejected: the agent's winning opener credited the wrong source](docs/how-it-was-built.md#design-rejected-the-agents-winning-opener-credited-the-wrong-source)
- [Agent bug caught: a removed memory fact could still reach the model](docs/how-it-was-built.md#agent-bug-caught-a-removed-memory-fact-could-still-reach-the-model)
- [Agent debt deleted: the one-off vision helper the setting replaced](docs/how-it-was-built.md#agent-debt-deleted-the-one-off-vision-helper-the-setting-replaced)

Map of the docs, plans and archived plans: [docs/README.md](docs/README.md). Method and agent configuration: [claudeconf](https://github.com/KristjanHS/claudeconf).

<!-- agentic-stats:start -->
| Metric | Value | Definition |
|---|---|---|
| Tests | +609 over wire-pod | static count: lines matching `^func Test\w*\(` in `*_test.go`; `^\s*(async\s+)?def test_` in `test_*.py`, `*/test_*.py`, `*_test.py` at HEAD, excl. `.claude`; `.each`/parametrized cases count once (delta vs wire-pod) |
| CI gates | 1 | workflows triggered on push / pull_request / merge_group; schedule- or dispatch-only ops jobs excluded |
| Releases | — | not tracked |
| ADRs | — | not tracked |
| Lines | +45641 over wire-pod | scc code lines (no blanks/comments) at HEAD, excl. docs/data formats, `.claude` (delta vs wire-pod) |
| In prod since | — | first production deploy (manual) |
<!-- agentic-stats:end -->

## Tests

`make check` runs the upstream-drift guard, shell syntax checks, the fork's Go tests and the host tests for the firmware patches. CI builds the Docker image and signs it with cosign on each push to main. It runs no tests.

## Limits

- STT needs an NVIDIA GPU, and the first `make stt-setup` downloads about 6 GB. The first `make tts-setup` downloads about 3 GB.
- wire-pod, TTS and STT run as systemd units, so the host needs systemd (enable it under WSL2).
- The robot must be unlocked and run WireOS with the 22 patches in `robot/firmware/series`.
- The web UI on ports 80 and 8080 has no login in the shipped build ([plan](docs/plans/2026-11-11-webui-auth.md)).

## Credits

Built on [wire-pod](https://github.com/kercre123/wire-pod) by Kerigan Creighton. Its overview, install links and full credits list are in [docs/upstream.md](docs/upstream.md).

## License

MIT, under three upstream notices that are kept as they are: [LICENSE](LICENSE) (Copyright (c) 2022 Kerigan Creighton, wire-pod), [chipper/LICENSE](chipper/LICENSE) and [vector-cloud/LICENSE](vector-cloud/LICENSE) (both Copyright (c) 2021 Digital Dream Labs).
