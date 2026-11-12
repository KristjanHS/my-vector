# SDK audio orphan — overlapping voice streams jam vic-anim and fill RAM

Status: ✅ SHIPPED (code on main; live confirm → `docs/backlog.md` §OWED[USER])

Repro: `voiceprobe overlap` (silent). Fixed when A hears `PlaybackComplete`, and no `starved` / `VERIFY(_audioPrepared)` line follows.

| id | ruling | edit site |
|---|---|---|
| A | Queue: a second answer waits for the first, never refused; one voice stream per robot at a time; `stream` sends Cancel on every exit after Prepare without Complete | `ttr` `StreamingKGSim` (fork glue + `scripts/upstream-drift.sh`), `voice` `stream` |
| B | A refused re-entrant Prepare leaves the playing stream alone; `ClearOperationData` ends `_waveData` unguarded (SDK + TTS); `StopActiveAudio` stops `kSdkGameObject` | firmware patch `0019-sdk-audio-orphan` (`sdkAudioComponent.cpp`, `textToSpeechComponent.cpp`) |
| C | Cap the RAM logs: syslog-ng size rotation on `/var/volatile/log/messages`, journald `RuntimeMaxUse` | `robot/logkeep/apply.sh` |
| A2 | Fix review of 6e69164 before ship: hold timer armed before `voice.Control`, pre-start `select` also exits on stop/done/hold; Control failure ends `StreamingKGSim` (close `start`); no Cancel after a robot verdict; stream ctx bounded by caller ctx + 2 s grace | `ttr` `forkBControl`, `voice` `stream`; failing tests: stop-before-start, never-starts, verdict→no Cancel |
| B2 | Fix review of 6e69164 before ship: one open audio stream at the gateway (2nd gets a gRPC error), plus Cancel to the engine when a prepared stream closes without Complete; null/identity check in `AddAudioChunk`; Complete on an unposted stream posts if it holds audio, else cleans up; code markers; overlap arm where B sends chunks + Complete | victor `cloud/cloud/message_handler.go` `ExternalAudioStreamPlayback` (vic-cloud serves :443), `sdkAudioComponent.cpp`, `markers.tsv`, `voiceprobe` `overlap` |
| C2 | Fix review of 6e69164: smoke `logcheck` WARNs when the log rotated past Startup; line-offset reads become a `logger` tag read across `messages.1` + `messages`; `$ETC_ROOT` paths + fake-root host test; restart a daemon when its config was written or is newer than the unit's start; `99-myvector.conf`; 0007 prose (reword the victor commit, `apply.sh --export`); logcheck rc-3/rc-1 fixture test | partial, uncommitted, unverified: `robot/lib/logmark.sh`, `robot/logkeep/{apply.sh,tests/}`, `smoke.sh`, `gate.sh`, `pcvoice.sh`, `scripts/check.sh` |
