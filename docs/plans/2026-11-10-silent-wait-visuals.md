# Silent wait visuals — show what he's waiting on, make no sound

**Goal**: whenever the robot waits on wire-pod (web search, LLM reply, vision model, any other slow fetch), his screen
shows a visual that fits the wait, such as "searching" or "thinking", and he makes **no sound** until the answer starts.
The wake chime before the wait and the spoken answer after it stay as they are.
**Status**: 📝 PLANNED 2026-11-10. Nothing built yet.

## Today (audit 2026-11-10)

| wait | where | what he shows now | sound |
|---|---|---|---|
| KG turn (`isKG`): search + LLM until the first sentence | `kgsim.go` `StreamingKGSim`, `if isKG` loop | `anim_knowledgegraph_searching_01` looped every 1/3 s (face sprite `face_knowledgegraph_searching`) | loop sound (peak −42.4) that outlives the clip until getout stops it. Live 11-10 (0.25 s search): ~1.8 s −99, then ~2 s above −45 (−10 burst, ~−39 loop), getout −19 |
| Wake-word turn (`!isKG`): search + LLM until the first sentence | `StreamingKGSim`: `IntentPass` + `forkBControl` run only **after** the first sentence | nothing from wire-pod; vic-engine's own post-listen state | silent: live 11-10, 5.5 s at −99 with a 2.2 s tavily search; first sound is a −10 burst at the first sentence (getin `anim_getin_tts_01`, unconfirmed) |
| KG → answer handoff | `StreamingKGSim` `TTSGetinAnimation` = `anim_knowledgegraph_searching_getout_01` | getout anim | peak −18.9, loud50ms −28.9: 11.5 / 9.5 dB under stock speech (−7.4 / −19.4); not on the Animation bus (same at 0.1 and 1.0) |
| Photo turn: "Let me look." → vision model → first sentence | `fork_vision.go` `forkPhotoShot` → `DoGetImage` | nothing during the vision call (`DoGetImage` plays no anim) | live 11-10: ~5.5 s of −10..−34 after "Let me look." (shutter/countdown, source unconfirmed), then the 6.5 s vision call at −99 |
| Web search | inside `CreateAIReq` → `forkSearch` (`fork_prompt.go`), before the LLM stream opens | same as the enclosing turn: no separate visual | — |

Probes: `robot/volume-probe/anim.sh` (per-anim dBFS), `idle.sh` (s > −45 dBFS); mute knob: `:8889` `SetAudioParameter`
`Robot_Vic_Volume_Animation`. `PlayAnimationRequest` has no ignore-audio flag.

## Rulings

| id | ruling | edit site |
|---|---|---|
| W1 | Measure before building: `anim.sh` on each wait anim + one live KG turn and one wake-word turn captured end to end. The loud baseline also shows the W8 falsifier can fail | Stage 1, no code |
| W2 | `[USER DECISION]` silence: (A) W1-silent anims only (kg searching is out: its loop) · (B) `Robot_Vic_Volume_Animation=0` for the wait · (C, rec.) firmware `kMyVectorSilentWait`: drop anim `PostAudioEvent` (never TTS/`Wake_Word_*`) until a self-expiring deadline | `0022-silent-wait.patch` (C) / `fork_wait.go` (A, B) |
| W3 | `[USER DECISION]` visuals: (A, rec. first) stock anim per wait kind · (B) drawn 184×96 frames via `DisplayFaceImageRGB`. User look 11-10 (`LOOPS=10 anim.sh`): face = search `anim_knowledgegraph_searching_01`, think `anim_avs_think_loop_01` (−99, silent); `explorer_planning_idle_01`, `rtsound_oncharger_observe_front_01`, `eyepose_curious` head-only/blank → out; `lookatdevice_icon` out (−18.3); look kind still needs a face anim | `fork_wait.go` |
| W4 | One wait loop for every path: `forkWait(robot, kind)` with `kind` ∈ {search, think, look}, changeable while it runs. It ends before `TTSGetinAnimation`, on the LLM-error path (`KGSim` error line), and on an interrupt | new `chipper/pkg/wirepod/ttr/fork_wait.go`; `kgsim.go` loop → one `forkWait` call (drift-listed) |
| W5 | Kind switches: `forkSearch` start → search; search done / LLM stream open → think; photo turn after "Let me look." → look | hook in `fork_prompt.go` around `forkSearch`; `fork_vision.go` |
| W6 | Wake-word turns (`!isKG`) get the wait too: send `IntentPass(intent_greeting_hello)` + `forkBControl` at stream open, not at the first sentence (as `forkPhotoTurn` does). `[AUDIT]` that an early intent doesn't cut vic-engine's listen or the follow-up arm (`fork_followup.go`) | `kgsim.go` `StreamingKGSim` |
| W7 | Out of scope: weather and other intent-path answers (no LLM wait) | — |
| W8 | Done = wait window (end of speech → getout start, else first TTS sample) has 0 s above −45 dBFS, visual on screen throughout; getout keeps its sound (user 2026-11-10) | Stage 4 |
| W9 | Getout no louder than speech at the same master (user 11-10): holds today, no change. Falsifier: `anim.sh` getout loud50ms ≤ `pcvoice.sh` stock loud50ms | — |
| W10 | KG loop: clip posts `Play__Robot_Vic_Sfx__Knowledge_Graph_Searching_Loop_Play`, getout posts `Stop__…_Searching_Loop_Stop`. Console `PostAudioEvent` Stop every 0.2 s on obj 4 and obj 1: loop survives (`loopstop.sh`, after −42.5). Next: (C) drop that Play in vic-anim, or getout-to-stop; user hears it in every KG wait | `0022-silent-wait.patch` / `fork_wait.go` |
| W11 | Answer face (A, user 11-10): `commands_enable` on skips `TTSLoopAnimation`, so the face is blank while he answers. Play the loop with commands on, paused while an LLM `{{playAnimation}}` cue plays; first `anim.sh` `anim_tts_loop_02` + `anim_knowledgegraph_answer_01` silent | `kgsim.go` TTS loop (drift-listed) |

## Checklist

### Stage 1: measure (no code) — sound test, ask the user first
- [ ] `robot/quiet/quiet.sh status` ≠ 3, user OK, note start time.
- [x] `ANIMS="anim_knowledgegraph_searching_01 anim_knowledgegraph_searching_getout_01" CONDS="base:shipped Animation:1.0" robot/volume-probe/anim.sh` → dBFS per anim.
- [x] Per-anim loudness + face look, 11-10 (W3, W10).
- [x] Live KG, wake-word and photo turns captured (`robot/work/volume-probe/live-{kg,photo,kg-long}/`), Today table.
- [x] `robot/quiet/scan.sh '<start>'`. Exit 1 → stop, `make quiet`, tell the user.
- [ ] Record results in this file's Today table, then put the W2/W3 decisions to the user.

### Stage 2: thinnest visible slice — KG wait silent
- [ ] `fork_wait.go` with the W2 pick, KG path only; `kgsim.go` loop → `forkWait` (add to `scripts/upstream-drift.sh`).
- [ ] Unit (fake robot): wait plays only W3-listed anims; mute (B/C) set before the first and cleared before getin, on the error path and on interrupt.
- [ ] `make check`; commit; user ships (`make ship`).

### Stage 3: wake-word path (W6)
- [ ] Early `IntentPass` + control; unit test the order (intent → control → wait → getin); live check that follow-up still arms.

### Stage 4: per-kind visuals (W5) + photo (W4 look)
- [ ] Kind switches at search start/end and in the photo turn; unit test the switch order.
- [ ] Live W8 check per kind (sound test → ask first, `scan.sh` after).

### Backlog (re-pick after each stage)
- W3-B drawn face frames.
- W11 answer-face loop.
- Same wait for the relay/camera path (`fork_relay.go`) if it has a visible gap.

## Falsifiers
- `go test ./pkg/wirepod/ttr -run 'ForkWait'` (from `chipper/`): a fake that records `PlayAnimation`/console calls fails
  if any wait anim is outside the silent set, or if the mute isn't cleared on the error/interrupt paths.
- Live: W1's baseline capture is loud (shows the check can fail); after the change the same capture shows 0 s above −45 dBFS.

## Rollback
B: `SetAudioParameter&args=Robot_Vic_Volume_Animation+1.0`. C: consolevar `kMyVectorSilentWait` off, or redeploy
without `0022`. wire-pod: revert `fork_wait.go` and the `kgsim.go` call.
