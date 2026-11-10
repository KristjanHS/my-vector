# Native photo follow-up — ask about a photo taken with "tee pilt" / "take a photo"

Goal: after the robot's own `intent_photo_take_extend` photo, "hey vector, mis pildi peal on?" is answered from THAT
photo by the vision model (2026-11-10-photo-followup.md (docs/plans/) store + kept-photo path), with no new capture.

| id | ruling | edit site |
|---|---|---|
| N1 | After wire-pod sends `intent_photo_take_extend`, poll `PhotosInfo` until a photo id newer than the pre-intent newest appears (≤ 15 s, every 500 ms); none → log, no store write | fork hook beside the intent send — `[AUDIT]` locate the send site (`IntentPass` / `intentparam.go` `intent_photo_take_extend`) |
| N2 | Fetch it with `Photo(id)` (full image, not thumbnail) and `forkPhotos.Put` + `Answered`, so F2's 120 s window starts at the save | new `chipper/pkg/wirepod/ttr/fork_native_photo.go` |
| N3 | A follow-up needs the wake word + an F3 word (`forkPhotoNamed`); the native turn opens no follow-up listen | none — F3/F4 already route it |
| N4 | `[USER DECISION]` "tee pilt" / "tee foto" sit in both the native intent keyphrases and `forkPhotoPhrases`; the native intent wins on a wake-word turn. Keep that split, or drop them from one side | `chipper/intent-data/et-EE.json` or `forkPhotoPhrases` |

Falsifiers:
- `go test ./pkg/wirepod/ttr -run 'ForkNativePhoto'` (from chipper/): a fake `PhotosInfo` gaining an id → `Photo` fetched once, store fresh with its bytes; no new id in 15 s → store untouched.
- Same run: after the native photo, `forkPhotoTurn` on "mis pildi peal on?" → zero `CaptureSingleImage`, one vision chat carrying the fetched bytes.
- Live (USER): "hey vector, tee pilt" → photo taken; then "hey vector, mis pildi peal on?" → no countdown, answer describes it.
