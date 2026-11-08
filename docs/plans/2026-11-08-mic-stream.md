# Continuous mic stream for active listening

Status: 1.1 + 2.1 + 2.2 shipped 2026-11-08; 1.2 gate next (`:8094` firewall rule owed by USER). Replaces C2/C3 (clip capture) of `2026-11-07-active-listening.md`; Hearer, State, echo gate, backoff, phrases stay.

## Rulings

| id | ruling | edit site |
|---|---|---|
| M1 | USER: replace the `RecordMicDataClip` clip capture with a continuous processed-mic stream robot → wire-pod. | new firmware patch, new `listen` stream receiver |
| M2 | USER: "Hey Vector" keeps working the whole time active listening is on. | M4 |
| M3 | USER: keep robot CPU low — C1–C5 plus the Stage 1.2 gate. | firmware patch, Stage 1.2 |
| M4 | Tap, not a mic job: in `MicDataProcessor::ProcessTriggerLoop`, beside `job->CollectProcessedAudio`, copy `processedAudio` (16 kHz mono s16, 10 ms block) into the stream ring. No `MicDataInfo` job → `HasStreamingJob()` stays false, the wake word and button work, no countdown face. | `robot/firmware/0016-mic-stream.patch`, `series`, `markers.tsv` |
| M5 | Server cuts an utterance when no bytes arrive for `GapCut` = 300 ms (+ G1 hangover = P1), at `MaxUtter` = 15 s, or on disconnect → myvector-stt → `Hearer.Heard`; no server VAD; Start/End on the server arrival clock (`State.Echo` unchanged). Replaces `Joiner`. | new `chipper/pkg/myvector/listen/stream.go` |
| M6 | `forkListen` on → set the robot's stream-port console var 0, hold `PortHold`, then 8093 over :8889 (the toggle forces a redial); off/idle/night → set 0 and close the socket. Lost connection while on → re-arm on `RestartWait`, last miss → off (`capture`). | `ttr/fork_listen.go`, `listen.Runner` |
| T1 | USER: robot dials out raw TCP to the PC `:8093`, host = the pc-voice URL's IPv4 (patch 0010 `MyVectorLoadVoice`); no token; port 0 = off (C2). | patch 0016; `scripts/wire-pod-guard.nft` `guarded` += 8093 |
| G1 | USER: on — send only while the raw per-block `DoSVad` flag is on (not `activeState`: its 1 s cooldown breaks P1), 300 ms pre-roll, 400 ms hangover (`kMyVectorMicHangover` 40 blocks). | patch 0016 |
| P1 | USER: pause cut 700 ms = G1 400 ms hangover + server `GapCut` 300 ms. | patch 0016 |
| M7 | After Stage 3.1: delete `robot/openmic/listen.sh` + its test, `listen.Joiner`, `ClipHandler`, the Runner's script path; `probe.sh`/`lib.sh` stay for `listenprobe`. | — |
| C1 | Reuse the wake word's processed audio (M4): no extra beamforming, VAD or resampling. | M4 |
| C2 | No target set → one atomic flag check per block; no thread work. | M4 |
| C3 | Trigger loop writes a fixed 2 s (64 KB) ring, never blocks; a sender thread drains it; on stall drop oldest, log the drop count once a minute. | patch 0016 |
| C4 | Raw PCM, no codec, one `send()` per 100 ms (3.2 KB). | patch 0016 |
| C5 | Sender thread `nice` 10, affinity off the mic-processing cores. | patch 0016 |

## Stages

| id | step | proof |
|---|---|---|
| 1.1 | Patch 0016: M4 tap, G1 gate, C3 ring, C4/C5 sender, port console var (T1). | `robot/firmware/check-static.sh`; USER `make ship NO_DEPLOY=1` |
| 1.2 | Shipped. USER opens :8094 (Windows + Hyper-V inbound, mirrored WSL); Claude runs `robot/mic-stream/gate.sh` (off on off, silent room, byte sink, no robot sound); USER: `gate.sh off on on off` talking through the 2nd on, then listening on, talk 30 s → `myvector listen: heard`, "Hey Vector" 3/3. Gate: worst on − mean off ≤ 2 % of one core, 0 ring drops; fail → stop, revisit C4/G1. | `gate.sh` exit 0; numbers into this row |
| 2.1 | M5 receiver on `:8093`, utterances logged only (`myvector listen: heard N words`); guard port. | `TestStreamCut` (gap → one utterance, `MaxUtter` → split, disconnect → flush); `scripts/tests/guard.sh`; `make check` |
| 2.2 | M6 arm/re-arm, feed `Hearer.Heard`. | `TestStreamArm` (on → target set; off → cleared + closed; lost → re-arm → off); `make check` |
| 3.1 | USER live (volume 1, ask first): 5 min talk, one "Hey Vector" ask mid-talk, off by voice. | `journalctl -u wire-pod \| grep 'myvector listen'` |
| 3.2 | M7 deletions. | `make check`; `scripts/upstream-drift.sh` |
