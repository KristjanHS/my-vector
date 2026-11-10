# Vision et-EE prompt bench — native Estonian vs English relay

Status: ✅ CLOSED 2026-11-10 — D2 = `d`; implementation → 2026-11-10-camera-et-relay.md (docs/plans/).
Question: for `gemma4:e4b-it-qat` and `qwen3.5:4b`, which et-EE camera prompt gives the best Estonian answer?

## Rulings

| id | ruling | edit site |
|---|---|---|
| B1 | Arm `a` (today, fe19969): system `Render(Default(OllamaEt), Sample(OllamaEt, et))`, user "Mida sa näed?" + photo → Estonian answer. | `visionbench` |
| B2 | Arm `b` (pre-fe19969 shape): system `Render(Default(Ollama), Sample(Ollama, et))` (English persona + "Answer in Estonian…"), user "Mida sa näed?" + photo. | `visionbench` |
| B3 | Arm `c` (relay, reopens V1): hop 1 = the en-US run (English prompt, "What do you see?", scored on `en` words); hop 2 = the saved et-EE brain (`llm.Resolve(c, et, kg)`), text only. | `visionbench` |
| B4 | Hop 2 system `Render(Default(<brain tier>), Sample(<brain tier>, et))`; user `"Kaamera nägi (inglise keeles): <hop 1>\n\nMida sa näed?"`. Latency = hop 1 + hop 2. | `visionbench` |
| B5 | Budget: 5 photos × 2 models × (a, b, c hop 1, c hop 2 cold, c hop 2 warm) = 50 calls, 1 rep (probe-runs-small). No robot, no audio. | — |
| B6 | Score per model × arm (a, b, c, c-warm): et keyword % (existing `score` on `et` words), median ms, errors; `-show` prints a/b/c side by side per photo. | `visionbench` |
| B9 | Hop 2 per photo: unload the brain → cold call (row `c`, the B7 gate) → warm repeat (row `c-warm`, info only). Hop 1 = the warm en-US run. | `visionbench` |
| B7 | Decision, per model then overall: highest et keyword %; within 10 points → USER fluency pick from `-show`; `c`/`d` (cold, B9) also need median ≤ ~5 s (USER 2026-11-10: a 5 s photo delay is fine for correct Estonian). | this plan |
| B8 | Stop after one pass: the verdict is written as row D2 whatever it is; no second rep or extra arm unless USER asks. | this plan |
| B10 | USER ask after pass 1 (qwen fluency pick b over a): arm `d` = hop 1 as `c`, hop 2 = the et brain translates it (`translateSystem`, user = hop 1 verbatim), cold + warm per B9. | `visionbench` |
| B11 | Pass 2 = `-arms a,d` (40 calls; `a` re-runs as the latency reference + warm-up); D2 weighs pass-1 a/b/c and pass-2 d by B7. | this plan |
| B12 | USER after pass 2: gemma4 out (loses every path); fix arm d — strip `{{…}}` before translating, `attachCommands` in front after, translator system + one worked example. | `visionbench` |
| B13 | Pass 3 = `-models qwen3.5:4b -arms a,d` (20 calls); USER rules D2 on it; latency per B7. | this plan |
| D2 | USER 2026-11-10: `d` — qwen3.5:4b answers in English, the et-EE brain translates (commands stripped/re-attached); ~5.3 s cold accepted; gemma4 out. | → 2.1 |

## Stages

| id | ruling | edit site |
|---|---|---|
| 1.1 | `-arms a,b,c` flag (default: today's single run) + relay hop 2 via `llm.NewClient(Resolve target)`. Proof `TestArmPrompt`: a holds `SpeechBlock(OllamaEt)`, b holds `LangLine(Ollama, et)`, c hop 2 user text holds hop 1; `go test ./pkg/myvector/visionbench/`. | `pkg/myvector/visionbench/main.go` + `_test.go` |
| 1.2 | From the MAIN checkout's `chipper/`: `go run ./pkg/myvector/visionbench -lang et-EE -arms a,b,c -show`, then `-arms a,d` (B11); USER fluency read only on a B7 near-tie; write D2 (scores in the commit). | this plan |
| 2.1 | D2 = `d` → plan: 2026-11-10-camera-et-relay.md (docs/plans/) (camera relay in `forkCameraReq`; rewrites V1, V14). | new plan |
