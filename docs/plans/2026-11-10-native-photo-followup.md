# Native photo follow-up — ask about a photo taken with "pildista" / "take a selfie"

Goal: after the robot's own `intent_photo_take_extend` photo, "hey vector, mis pildi peal on?" is answered from THAT
photo by the vision model (2026-11-10-photo-followup.md (docs/plans/) store + kept-photo path), with no new capture.

| id | ruling | edit site |
|---|---|---|
| N1 | After wire-pod sends `intent_photo_take_extend`, poll `PhotosInfo` until a photo id newer than the pre-intent newest appears (≤ 30 s, every 500 ms); none → log, no store write | `forkIntentPass` (fork_followup.go): newest id read before the send, poll in a goroutine |
| N2 | Fetch it with `Photo(id)` (full image, not thumbnail) and `forkPhotos.PutFresh` (fresh, no per-turn flag: N3), so F2's 120 s window starts at the save | new `chipper/pkg/wirepod/ttr/fork_native_photo.go` |
| N3 | A follow-up needs the wake word + an F3 word (`forkPhotoNamed`); the native turn opens no follow-up listen | none — F3/F4 already route it |
| N4 | Ruled B (user): "tee pilt/pilti/foto/fotot", "take a photo/picture" = vision only; native keeps pildista, tee üks/minust pilt, tee selfi, take a selfie | `et-EE.json`, `forkAddedKeyphrases`, `forkPhotoPhrases` |

Falsifiers:
- `go test ./pkg/wirepod/ttr -run 'ForkNativePhoto'` (from chipper/): a fake `PhotosInfo` gaining an id → `Photo` fetched once, store fresh with its bytes; no new id in 30 s → store untouched.
- Same run: after the native photo, `forkPhotoTurn` on "mis pildi peal on?" → zero `CaptureSingleImage`, one vision chat carrying the fetched bytes.
- Live (USER): "hey vector, pildista" → photo taken; then "hey vector, mis pildi peal on?" → no countdown, answer describes it.
