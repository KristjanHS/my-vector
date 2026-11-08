# Continuous mic stream for active listening

Status: M1–M3 ruled 2026-11-08; T1/G1/P1 open; nothing built. Replaces C2/C3 (clip capture) of `2026-11-07-active-listening.md`; Hearer, State, echo gate, backoff, phrases stay.

## Rulings

| id | ruling | edit site |
|---|---|---|
| M1 | USER: replace the `RecordMicDataClip` clip capture with a continuous processed-mic stream robot → wire-pod. | new firmware patch, new `listen` stream receiver |
| M2 | USER: "Hey Vector" keeps working the whole time active listening is on. | M4 |
| M3 | USER: keep robot CPU low — C1–C5 plus the Stage 1.2 gate. | firmware patch, Stage 1.2 |
| M4 | Tap, not a mic job: in `MicDataProcessor::ProcessTriggerLoop`, beside `job->CollectProcessedAudio`, copy `processedAudio` (16 kHz mono s16, 10 ms block) into the stream ring. No `MicDataInfo` job → `HasStreamingJob()` stays false, the wake word and button work, no countdown face. | `robot/firmware/0015-mic-stream.patch`, `series`, `markers.tsv` |
| M5 | Server cuts an utterance at VAD-off ≥ `PauseCut` (P1) or `MaxUtter` = 15 s → myvector-stt → `Hearer.Heard`; Start/End on the server arrival clock (`State.Echo` unchanged). Replaces `Joiner`. | new `chipper/pkg/myvector/listen/stream.go` |
| M6 | `forkListen` on → set the robot's stream target (console var over :8889, as `clip_ms`); off/idle/night → clear it and close the socket. Lost connection while on → re-arm on `RestartWait`, last miss → off (`capture`). | `ttr/fork_listen.go`, `listen.Runner` |
| M7 | After Stage 3.1: delete `robot/openmic/listen.sh` + its test, `listen.Joiner`, `ClipHandler`, the Runner's script path; `probe.sh`/`lib.sh` stay for `listenprobe`. | — |
| C1 | Reuse the wake word's processed audio (M4): no extra beamforming, VAD or resampling. | M4 |
| C2 | No target set → one atomic flag check per block; no thread work. | M4 |
| C3 | Trigger loop writes a fixed 2 s (64 KB) ring, never blocks; a sender thread drains it; on stall drop oldest, log the drop count once a minute. | patch 0015 |
| C4 | Raw PCM, no codec, one `send()` per 100 ms (3.2 KB). | patch 0015 |
| C5 | Sender thread `nice` 10, affinity off the mic-processing cores. | patch 0015 |

## Open (USER)

| id | question | options, proposed first |
|---|---|---|
| T1 | Transport | **a** robot dials out TCP to 192.0.2.249 with a token, opens no port (raw audio on home LAN) · **b** robot listens, wire-pod connects · **c** SSH pipe (encrypted; dropbear crypto CPU) |
| G1 | Robot-side VAD gate on `activeState`, 300 ms pre-roll, 700 ms hangover | **on** · **off** |
| P1 | `PauseCut` | **700 ms** · other |

## Stages

| id | step | proof |
|---|---|---|
| 1.1 | Patch 0015: M4 tap, C3 ring, C4/C5 sender, target console var (T1). | `robot/firmware/check-static.sh`; USER `make ship NO_DEPLOY=1` |
| 1.2 | USER `make ship`; host listener writes `robot/work/mic-stream/*.pcm`; 30 s through myvector-stt. Gate: docked idle, robot `top -b -d 5`, 5 min off then 5 min on → vic-anim CPU delta ≤ 2 % of one core, 0 ring drops, "Hey Vector" answers 3/3 mid-stream. Fail → stop, revisit C4/G1. | numbers into this row |
| 2.1 | M5 receiver, utterances logged only (`myvector listen: heard N words`). | `TestStreamCut` (pause → one utterance, `MaxUtter` → split, disconnect → flush); `make check` |
| 2.2 | M6 arm/re-arm, feed `Hearer.Heard`. | `TestStreamArm` (on → target set; off → cleared + closed; lost → re-arm → off); `make check` |
| 3.1 | USER live (volume 1, ask first): 5 min talk, one "Hey Vector" ask mid-talk, off by voice. | `journalctl -u wire-pod \| grep 'myvector listen'` |
| 3.2 | M7 deletions. | `make check`; `scripts/upstream-drift.sh` |
