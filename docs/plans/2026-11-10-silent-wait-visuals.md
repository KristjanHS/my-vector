# Silent wait visuals — show what he's waiting on, make no sound

**Goal**: whenever the robot waits on wire-pod (web search, LLM reply, vision model, any other slow fetch), his screen
shows a visual that fits the wait, such as "searching" or "thinking", and he makes **no sound** until the answer starts.
The wake chime before the wait and the spoken answer after it stay as they are.
**Status**: 📝 PLANNED 2026-11-10. Nothing built yet.

## Today (audit 2026-11-10)

| wait | where | what he shows now | sound |
|---|---|---|---|
| KG turn (`isKG`): search + LLM until the first sentence | `kgsim.go` `StreamingKGSim`, loop at ~L231 | `anim_knowledgegraph_searching_01` looped every 1/3 s (face sprite `face_knowledgegraph_searching`) | `[AUDIT]` unmeasured. Audio IDs are numeric in the `.bin`, so decode as in eye-chimes E3, or capture (W1) |
| Wake-word turn (`!isKG`): search + LLM until the first sentence | `kgsim.go` L404–417: `IntentPass` and `forkBControl` run only **after** the first sentence | nothing from wire-pod; vic-engine's own post-listen state | `[AUDIT]` |
| KG → answer handoff | `kgsim.go` L446: `TTSGetinAnimation` = `anim_knowledgegraph_searching_getout_01` | getout anim | `[AUDIT]` |
| Photo turn: "Let me look." → vision model → first sentence | `fork_vision.go` `forkPhotoShot` → `DoGetImage` | `[AUDIT]` whatever `DoGetImage` plays during the vision call | `[AUDIT]` |
| Web search | inside `CreateAIReq` → `forkSearch` (`fork_prompt.go`), before the LLM stream opens | same as the enclosing turn: no separate visual | — |

Tools on hand: `robot/volume-probe/anim.sh` captures one animation's output as dBFS (`ANIMS=… CONDS=base:1.0`).
`idle.sh` counts seconds above −45 dBFS. `Robot_Vic_Volume_Animation` is a live RTPC through
`:8889/consolefunccall?func=SetAudioParameter` (speech-volume V3/V6). `PlayAnimationRequest` has no ignore-audio flag
(only `IgnoreBody/Head/LiftTrack`).

## Rulings

| id | ruling | edit site |
|---|---|---|
| W1 | Measure before building: `anim.sh` on each wait anim + one live KG turn and one wake-word turn captured end to end. The loud baseline also shows the W8 falsifier can fail | Stage 1, no code |
| W2 | `[USER DECISION]` **How the wait goes silent** — **(A)** play only animations W1 measured silent; **(B)** wire-pod sets `Robot_Vic_Volume_Animation=0` for the wait and back to 1.0 before the answer's getin; **(C)** firmware `kMyVectorSilentWait` in `cozmoAudioController.cpp`: drops every animation-object `PostAudioEvent` (never TTS, never `Wake_Word_*`) until a deadline wire-pod sets via consolevar, and expires on its own. Recommend **C**: it keeps the stock visuals and a wire-pod crash can't leave him mute. B is the no-build stopgap, but eye-chimes E1 saw an RTPC come back after a turn, and a crash mid-wait leaves animations mute until the next turn | `0022-silent-wait.patch` (C) / new `fork_wait.go` (A, B) |
| W3 | `[USER DECISION]` **Which visuals** — **(A)** stock animations per wait kind (picked from `ListAnimations` by W1 + eye check); **(B)** wire-pod-drawn 184×96 face frames via `DisplayFaceImageRGB` (magnifier = search, dots = thinking, lens = photo), silent by construction and independent of W2. Recommend A first (thin slice), B as backlog if stock faces don't read as "relevant" | `fork_wait.go` |
| W4 | One wait loop for every path: `forkWait(robot, kind)` with `kind` ∈ {search, think, look}, changeable while it runs. It ends before `TTSGetinAnimation`, on the LLM-error path (`KGSim` error line), and on an interrupt | new `chipper/pkg/wirepod/ttr/fork_wait.go`; `kgsim.go` loop → one `forkWait` call (drift-listed) |
| W5 | Kind switches: `forkSearch` start → search; search done / LLM stream open → think; photo turn after "Let me look." → look | hook in `fork_prompt.go` around `forkSearch`; `fork_vision.go` |
| W6 | Wake-word turns (`!isKG`) get the wait too: send `IntentPass(intent_greeting_hello)` + `forkBControl` at stream open, not at the first sentence (the photo turn already does this, `fork_vision.go` L192–196). `[AUDIT]` that an early intent doesn't cut vic-engine's listen or the follow-up arm (`fork_followup.go`) | `kgsim.go` L404–417 |
| W7 | Out of scope: weather and other intent-path answers (no LLM wait), and the answer's own TTS loop anim | — |
| W8 | Done = during the wait window (end of speech → first TTS sample) the capture has 0 s above −45 dBFS; the visual is on screen for the whole window | Stage 4 |

## Checklist

### Stage 1: measure (no code) — sound test, ask the user first
- [ ] `robot/quiet/quiet.sh status` ≠ 3, user OK, note start time.
- [ ] `ANIMS="anim_knowledgegraph_searching_01 anim_knowledgegraph_searching_getout_01" CONDS=base:1.0 robot/volume-probe/anim.sh` → dBFS per anim.
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
