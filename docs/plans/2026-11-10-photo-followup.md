# Photo follow-up — ask about the last photo (option B)

Goal: "kus pildi peal asub rulli keeratud joogamatt?" after a photo turn is answered from THAT photo by the
vision model, with no new capture.

| id | ruling | edit site |
|---|---|---|
| F1 | Keep the last photo per robot: JPEG bytes + capture time + its lang; replaced by every capture | new `chipper/pkg/myvector/photo` store, set in `forkPhotoCapture` |
| F2 | Fresh = last photo answer (new or reused) ≤ 120 s ago (`photo.Keep`), so a follow-up chain keeps the photo; stale or absent → today's path, no reuse | same store |
| F6 | Every photo answer (phrase, reuse, brain `getImage`) that was spoken listens again under `forkMax`; failed capture / V8 / interrupt → no listen. An utterance heard in a photo-armed listen goes to F4 even without an F3 word | `forkPhotoTurn` calls `forkFollowUp` (`fork_followup.go`); an intent-graph-handled request ends the chain and the photo listen |
| F3 | Follow-up = utterance names the picture: et `pilt/pildi/pildil/pildile/pildilt/pildis/foto/fotol/fotolt/fotos`, en `picture/photo/image`, whole words via `forkPhotoWords`; a photo phrase (`forkPhotoAsk`) still takes a NEW photo |
| F7 | `tee`/`take`/`uus`/`uue`/`new`/`another` + an F3 word, or any `pildist*` word ("tee uus pilt", "pildistada") takes a NEW photo everywhere, beating F6's reuse; `veel`/`teine` excluded ("mis veel pildil on" reuses) | `forkPhotoNew` inside `forkPhotoAsk`, `fork_vision.go` |
| F4 | Fresh + follow-up → `forkPhotoShot` with param `kept`: "Vaatan." stays, no countdown/mirror/shutter; same relay, same `forkPhotoRemember` | `DoGetImage` capture block → `forkPhotoCapture` (one hook, `upstream-drift.sh`) |
| F5 | The relay trace of a photo whose bytes equal the previous one notes "reused photo from <stamp>" | `relay.Trace.savePhoto` |

Falsifiers:
- `go test ./pkg/wirepod/ttr -run 'PhotoFollow'` (from chipper/): fresh+follow-up → one vision chat carrying the stored bytes, zero `CaptureSingleImage`; stale → no reuse; photo phrase → new capture.
- Replay: `robot/work/vision-trace/20261110-173339.367` + `expected.md` (mat = left edge, on the floor) — do not re-read the photo.
- `go test ./pkg/wirepod/ttr -run 'PhotoFollow'`: a photo turn whose reply has no "?" still listens again; the heard utterance (no F3 word) reuses the photo.
- F7: `go test ./pkg/wirepod/ttr -run 'PhotoFollow'`: in a photo-armed listen "tee uus pilt" → captures +1, the new bytes in the vision chat.
- Live (USER): "mida sa näed", then — no wake word, while he listens — "kus pildi peal asub rulli keeratud joogamatt?" → no countdown, answer says left.
