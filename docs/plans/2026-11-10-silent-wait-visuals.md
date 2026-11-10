# Silent wait visuals — show what he's waiting on, make no sound

**Goal**: whenever the robot waits on wire-pod (web search, LLM reply, vision model, any other slow fetch), his screen
shows a visual that fits the wait, such as "searching" or "thinking", and he makes **no sound** until the answer starts.
The wake chime before the wait and the spoken answer after it stay as they are.
**Status**: 📝 PLANNED 2026-11-10. Nothing built yet.

## Today (audit 2026-11-10)

| wait | where | what he shows now | sound |
|---|---|---|---|
| KG turn (`isKG`): search + LLM until the first sentence | `kgsim.go` `StreamingKGSim`, `if isKG` loop | `anim_knowledgegraph_searching_01` looped every 1/3 s (face sprite `face_knowledgegraph_searching`) | loop sound (peak −42.4) that outlives the clip until getout stops it: −99 before, −42 after, −99 after getout (11-10) |
| Wake-word turn (`!isKG`): search + LLM until the first sentence | `StreamingKGSim`: `IntentPass` + `forkBControl` run only **after** the first sentence | nothing from wire-pod; vic-engine's own post-listen state | `[AUDIT]` |
| KG → answer handoff | `StreamingKGSim` `TTSGetinAnimation` = `anim_knowledgegraph_searching_getout_01` | getout anim | peak −18.9, loud50ms −28.9: 11.5 / 9.5 dB under stock speech (−7.4 / −19.4); not on the Animation bus (same at 0.1 and 1.0) |
| Photo turn: "Let me look." → vision model → first sentence | `fork_vision.go` `forkPhotoShot` → `DoGetImage` | `[AUDIT]` whatever `DoGetImage` plays during the vision call | `[AUDIT]` |
| Web search | inside `CreateAIReq` → `forkSearch` (`fork_prompt.go`), before the LLM stream opens | same as the enclosing turn: no separate visual | — |

Probes: `robot/volume-probe/anim.sh` (per-anim dBFS), `idle.sh` (s > −45 dBFS); mute knob: `:8889` `SetAudioParameter`
`Robot_Vic_Volume_Animation`. `PlayAnimationRequest` has no ignore-audio flag.

## Rulings

| id | ruling | edit site |
|---|---|---|
| W1 | Measure before building: `anim.sh` on each wait anim + one live KG turn and one wake-word turn captured end to end. The loud baseline also shows the W8 falsifier can fail | Stage 1, no code |
| W2 | `[USER DECISION]` silence: (A) W1-silent anims only (kg searching is out: its loop) · (B) `Robot_Vic_Volume_Animation=0` for the wait · (C, rec.) firmware `kMyVectorSilentWait`: drop anim `PostAudioEvent` (never TTS/`Wake_Word_*`) until a self-expiring deadline | `0022-silent-wait.patch` (C) / `fork_wait.go` (A, B) |
| W3 | `[USER DECISION]` visuals: (A, rec. first) stock anim per wait kind · (B) drawn 184×96 frames via `DisplayFaceImageRGB`. A candidates, unmeasured (11-10 audition ran inside the kg loop): search `anim_knowledgegraph_searching_01` · think `anim_avs_think_loop_01` / `anim_explorer_planning_idle_01` · look `anim_rtsound_oncharger_observe_front_01` / `anim_eyepose_curious`; `anim_lookatdevice_icon` out (−18.3, left a sound running) | `fork_wait.go` |
| W4 | One wait loop for every path: `forkWait(robot, kind)` with `kind` ∈ {search, think, look}, changeable while it runs. It ends before `TTSGetinAnimation`, on the LLM-error path (`KGSim` error line), and on an interrupt | new `chipper/pkg/wirepod/ttr/fork_wait.go`; `kgsim.go` loop → one `forkWait` call (drift-listed) |
| W5 | Kind switches: `forkSearch` start → search; search done / LLM stream open → think; photo turn after "Let me look." → look | hook in `fork_prompt.go` around `forkSearch`; `fork_vision.go` |
| W6 | Wake-word turns (`!isKG`) get the wait too: send `IntentPass(intent_greeting_hello)` + `forkBControl` at stream open, not at the first sentence (as `forkPhotoTurn` does). `[AUDIT]` that an early intent doesn't cut vic-engine's listen or the follow-up arm (`fork_followup.go`) | `kgsim.go` `StreamingKGSim` |
| W7 | Out of scope: weather and other intent-path answers (no LLM wait), and the answer's own TTS loop anim | — |
| W8 | Done = wait window (end of speech → getout start, else first TTS sample) has 0 s above −45 dBFS, visual on screen throughout; getout keeps its sound (user 2026-11-10) | Stage 4 |
| W9 | Getout no louder than speech at the same master (user 11-10): holds today, no change. Falsifier: `anim.sh` getout loud50ms ≤ `pcvoice.sh` stock loud50ms | — |

## Checklist

### Stage 1: measure (no code) — sound test, ask the user first
- [ ] `robot/quiet/quiet.sh status` ≠ 3, user OK, note start time.
- [x] `ANIMS="anim_knowledgegraph_searching_01 anim_knowledgegraph_searching_getout_01" CONDS="base:shipped Animation:1.0" robot/volume-probe/anim.sh` → dBFS per anim.
- [ ] One live KG question + one wake-word question with capture on: seconds above −45 dBFS inside the wait window.
- [ ] `robot/quiet/scan.sh '<start>'`. Exit 1 → stop, `make quiet`, tell the user.
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
- Same wait for the relay/camera path (`fork_relay.go`) if it has a visible gap.

## Falsifiers
- `go test ./pkg/wirepod/ttr -run 'ForkWait'` (from `chipper/`): a fake that records `PlayAnimation`/console calls fails
  if any wait anim is outside the silent set, or if the mute isn't cleared on the error/interrupt paths.
- Live: W1's baseline capture is loud (shows the check can fail); after the change the same capture shows 0 s above −45 dBFS.

## Rollback
B: `SetAudioParameter&args=Robot_Vic_Volume_Animation+1.0`. C: consolevar `kMyVectorSilentWait` off, or redeploy
without `0022`. wire-pod: revert `fork_wait.go` and the `kgsim.go` call.
