# Docs map

| path | what it covers |
|---|---|
| [architecture.md](architecture.md) | The robot, the PC services and their ports, the data flow of one spoken ask, and the main design decisions with what was rejected. |
| [how-it-was-built.md](how-it-was-built.md) | Worked trails from plan to commits to review to archive, with the diff that mattered quoted. |
| [upstream.md](upstream.md) | wire-pod's own overview, install links and credits, kept word for word. |
| [estonian-models.md](estonian-models.md) | Every Estonian model Vector uses (speech recognition, LLM brain, voice), all local, and how to get each. |
| [model-prompt-formats.md](model-prompt-formats.md) | How each LLM in use wants its prompt formatted, and what this fork sends. |
| [llm-trials.md](llm-trials.md) | Step-by-step trials of the LLM options for free-form answers, cheapest first, with a results table. |
| [crash-debugging.md](crash-debugging.md) | Runbook for robot crashes: catalogue, plain restarts, no-gdb deploys, gdb last. |
| [run-without-sudo-password.md](run-without-sudo-password.md) | One-time sudoers entry so `make start` runs without a password prompt. |
| [backlog.md](backlog.md) | Open work: live checks still owed on the robot, and follow-ups not yet planned. |
| [plans/](plans/) | Active plans: one file per change, staged with its verify and falsifier steps. |
| [plans/archive/](plans/archive/) | Closed plans, kept as the record of what was decided and why. |
| [../chipper/pkg/myvector/](../chipper/pkg/myvector/) | The fork's Go code, one package per feature: LLM, prompt, search, voice, memory, follow-up listening, WiFi and the probes that test them on a live robot. |
| `chipper/pkg/**/fork_*.go` | The glue: files added to upstream packages that hook them into `myvector`. Other upstream edits are listed in [../scripts/upstream-drift.sh](../scripts/upstream-drift.sh). |
| [../robot/](../robot/) | Software for the unlocked robot, one folder each: firmware patches, Tailscale, firewall, sound lock, SSH and host keys. |
| [../robot/firmware/](../robot/firmware/) | The robot firmware patch series (`series` lists the order) and the scripts that apply, check and ship it. |
| [../tts/](../tts/) | The TTS service on port 8091: Kokoro and Piper voices over HTTP. |
| [../et-stt/](../et-stt/) | The STT service on port 8092: faster-whisper with TalTech's Estonian Whisper model. |
| [../scripts/](../scripts/) | Run, check and deploy scripts behind the Makefile, the systemd units, the SearXNG launcher and the shell tests in `scripts/tests/`. |
