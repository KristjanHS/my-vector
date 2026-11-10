# Camera et-EE relay — English vision answer, et brain translates

Status: ✅ SHIPPED to main 2026-11-10 (code + review fixes); 3.1 live check → `docs/backlog.md` §OWED[USER]. Hop 0 added by the qimpag round (USER). Parent: 2026-11-10-vision-et-prompt-bench.md (docs/plans/) row D2 (its stage 2.1); 2026-11-08-vision-setting.md (docs/plans/) rows V1, V14.
Goal: an et-EE photo answer = hop 0 (the et brain puts the user's question in English) → unload brain → hop 1 (vision model, English) → unload vision → hop 2 (the et brain translates, `{{…}}` re-attached in front); en-US unchanged.

## Rulings

| id | ruling | edit site |
|---|---|---|
| R1 | Relay only when `lang.Current()` = et-EE, `llm.ResolveVision` gives a real target (not the brain fallback), `forkResolve` gives a brain, vision is Ollama, brain model ≠ vision model; else today's direct `forkCameraReq` path. | `forkCameraReq` |
| R2 | Seam = transport, no `DoGetImage` edit: `*cl` = a client whose `HTTPClient.Transport` is `forkRelay`; `RoundTrip` runs both hops buffered and answers a synthetic SSE stream (one chunk + `[DONE]`). `upstream-drift.sh` unchanged. | new `ttr/fork_relay.go` |
| R3 | Hop 1 buffered: forward the raw body to the vision URL with `stream` false, `stream_options` dropped; patch the JSON as a map, never re-marshal `MultiContent`. Per-hop `forkRelayTimeout` 60 s. | `forkRelay` |
| R4 | Hop 1 prompt: `forkOllamaTier(r, esn, lang.English)` + `forkReplyCap(r, true, false, prompt.Ollama)` (`forkCameraReq`); `RoundTrip` appends a text part = hop 0's English question to the photo message, `forkRelayAsk` = "What do you see?" when hop 0 has no question or fails (USER). | `forkCameraReq`, `forkRelay` |
| R11 | Hop 0 = `relay.ToEnglish(brain, q)` on the still-loaded brain, q = the last user message with text before the photo; `ToEnglish` asks for the question only, in English. | `forkRelay`, `myvector/relay` |
| R5 | Hop 2 = `relay.Translate(brain, hop1)` + `llm.SamplingFor(brain.Provider, brain.Model)` on `llm.NewClient(brain)` (Claude: same, no unload). Reply = `relay.Attach(hop1, hop2)`: commands in front (the measured shape; `forkPhotoActs` drops `getImage`, V15). Skip hop 2 → R7 when hop 1 text is commands-only. | `forkRelay`, `myvector/relay` |
| R6 | `finish_reason` = `length` if either hop hit its cap (`forkDropCut` drops the last sentence). | `forkRelay` |
| R7 | Unload on the relay path, all inside `RoundTrip` (success or failure): brain after hop 0, vision after hop 1; brain stays loaded after hop 2; `forkCameraReq` skips its pre-call brain unload and returns a no-op. | `forkRelay`, `forkCameraReq` |
| R8 | Hop 1/hop 2 error or empty hop 2 → `RoundTrip` errors → `DoGetImage`'s stream-create branch → `forkVisionDown` "Ma ei näe praegu."; never speak hop 1's English. | `forkRelay` |
| R9 | Log per relay: `myvector llm: camera relay "<hop0>" <brain> <ms>ms → <vision> <ms>ms "<hop1>" → <brain> <ms>ms`; failure: `myvector llm: camera relay failed: hop<N>: <err>` (hop 0 logs and falls back, R4). | `forkRelay` |
| R10 | Shared helpers in new package `myvector/relay` (imports `llm` + `prompt`; neither imports the other); `visionbench` imports it. | `pkg/myvector/relay/` |

## Stages

| id | ruling | edit site |
|---|---|---|
| 1.1 | Pure move: `translateRequest`/`translateSystem`/`translateAsk`/`splitCommands`/`attachCommands`/`commandRe` → `relay.Translate`/`System`/`Ask`/`Split`/`Attach`; `visionbench` calls them. Proof `TestTranslate` (the arm-d asserts moved); `go test ./pkg/myvector/relay/ ./pkg/myvector/visionbench/`. | `pkg/myvector/relay/`, `visionbench/` |
| 1.2 | `relay.ToEnglish` + `forkRelay` transport (R2–R9, R11), unwired; `newFakeLLM` gets per-model replies + an ordered event log. Proof `TestForkRelay` (events: chat brain, unload brain, chat vision `"stream":false` with hop 0's text part, unload vision, chat brain; go-openai stream reads `{{cmd}} <et text>`), `TestForkRelayHop0Down` (vision text part = `forkRelayAsk`), `TestForkRelayHop2Down`; `go test -run ForkRelay ./pkg/wirepod/ttr/`. | `ttr/fork_relay.go` + `_test.go`, `fork_client_test.go` |
| 1.3 | Wire R1, R4, R7 into `forkCameraReq`. Proof `TestForkCameraRelay` (hop-1 body holds `LangLine(Ollama, en-US)` + hop 0's question; unloads = brain, vision, both after hop 0) + `TestForkCameraRelaySkips` (en-US, legacy, Claude vision, same model, no vision target → one direct chat); move `TestForkCameraEstonianUnloads`/`TestForkCameraOllamaTierEt` to the relay shape. | `ttr/fork_prompt.go`, `fork_client_test.go`, `fork_prompt_test.go` |
| 2.1 | Rewrite 2026-11-08-vision-setting.md (docs/plans/) rows V1 (et-EE photo = this relay, hop 0 included; en-US direct) and V14 (+ hop-1 English tier). | parent plan |
| 3.1 | `make check` judged by exit code (ttr runs `-run Fork`: every new ttr test name carries `Fork`); `code-reviewer` on 1.1–1.3. USER: `make ship`, `robot/quiet/quiet.sh status` ≠ 3, ask before the sound test; et-EE "mida sa näed?" and "mis värvi mu särk on?" → Estonian answer to each question, ~5 s; `journalctl -u wire-pod --since '<t>' \| grep -c 'camera relay '` ≥ 1; `robot/quiet/scan.sh '<t>'`. | — |
