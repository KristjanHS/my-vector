# Photo follow-up — ask about the last photo (option B)

Goal: "kus pildi peal asub rulli keeratud joogamatt?" after a photo turn is answered from THAT photo by the
vision model, with no new capture.

| id | ruling | edit site |
|---|---|---|
| F1 | Keep the last photo per robot: JPEG bytes + capture time + its lang; replaced by every capture | `chipper/pkg/myvector/` new store, set where `DoGetImage` gets `forkPhotoData` |
| F2 | Fresh = captured ≤ 120 s ago (`forkPhotoKeep`); stale or absent → today's path, no reuse | same store |
| F3 | Follow-up = utterance names the picture: et `pilt/pildi/pildil/pildile/pildilt/pildis/foto/fotol/fotolt/fotos`, en `picture/photo/image`, whole words via `forkPhotoWords`; a photo phrase (`forkPhotoAsk`) still takes a NEW photo | `fork_vision.go` beside `forkPhotoAsk` |
| F4 | Fresh + follow-up → `forkPhotoTurn`-style seam with no countdown, no mirror mode, no shutter; same relay (hop 0 translates the specific question — never bare), same `forkPhotoRemember` | `fork_vision.go` `forkPhotoTurn` / `forkPhotoShot` |
| F5 | Trace the reuse turn like any relay turn; trace.md notes "reused photo from <stamp>" | `fork_relay.go` / `relay.Trace` |

Falsifiers:
- `go test ./pkg/wirepod/ttr -run 'PhotoFollow'` (from chipper/): fresh+follow-up → one vision chat carrying the stored bytes, zero `CaptureSingleImage`; stale → no reuse; photo phrase → new capture.
- Replay: `robot/work/vision-trace/20261110-173339.367` + `expected.md` (mat = left edge, on the floor) — do not re-read the photo.
- Live (USER): "mida sa näed" then "kus pildi peal asub rulli keeratud joogamatt?" within 2 min → no countdown, answer says left.
