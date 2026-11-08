# Vision setting + local vision bench

Status: V1–V13 ruled 2026-11-08; D1 pending Stage 1.4; no stage shipped. Parent: 2026-11-07-system-prompt-redesign.md (docs/plans/) rung 5, row V.

## Rulings

| id | ruling | edit site |
|---|---|---|
| V1 | Photo question goes DIRECT in the configured language to the vision model; no English-describe-then-ET-model two-step. The bench picks the model per language. | `forkCameraReq`, `visionbench` |
| V2 | Per-language setting `"vision": {"en-US": Profile, "et-EE": Profile}` beside `"llm"` in `myvectorConfig.json`. Provider `ollama` \| `claude`; validated like `"llm"` (Claude needs `claude_key`). | `llm.Config.Vision`, `llm.Load` / `Save` / `Validate` |
| V3 | Defaults `llm.DefaultVisionEN`, `llm.DefaultVisionET` on the saved Ollama URL = D1 winners (value set by Stage 1.4). `llm.VisionModel` is deleted. | `llm/llm.go` |
| V4 | `llm.ResolveVision(c, l, kg) (Target, bool)`: saved Ollama → that model @ `Effective` URL + `/v1`; saved Claude + key → Claude target; saved Claude, no key → log + local default; unsaved → local default; false only on legacy (`forkResolve` false and Knowledge not Ollama). | `llm.ResolveVision` |
| V5 | `forkCameraReq(r, &c)` sets the request AND swaps `*c` to `llm.NewClient(vision target)`; logs `myvector llm: camera <l> → <provider> <model> @ <base>`. Brain Claude + vision local → Ollama. | `ttr/fork_prompt.go`, hook line in `ttr/kgsim_cmds.go`, `scripts/upstream-drift.sh` comment |
| V6 | Sampling / cap / reasoning follow the vision target: `llm.SamplingFor(t.Provider, t.Model)`, `forkReplyCap(r, t.Ollama(), false)`, Ollama → `ReasoningEffort = "none"`. | `forkCameraReq` |
| V7 | Unload (E26 generalised): brain target Ollama with model ≠ vision model → `forkUnload` brain before; local vision model ≠ brain model → unload vision after (brain Claude included); equal → no unload. | `forkCameraReq` |
| V8 | Ollama down: the existing `DoGetImage` path (stream error → `LLM error:` log, return, silent); never a Claude retry. | `forkCameraReq` (no fallback) |
| V9 | `/myvector/voice`: per language a "Camera (vision)" select under the brain select — optgroup Local (Ollama) = `/api/tags` models whose `capabilities` hold `"vision"`; optgroup "Claude API — sends the photo to Anthropic" = `listClaude` models. Models API `ollama` gains `vision []string`; GET serves effective vision + defaults; POST `vision` absent keeps. | `llm/http.go`, `voice/settings.go`, `settings.html` |
| V10 | Photos: `robot/vision-bench/grab.sh [n]` sources `robot/env.sh`, curls `$server/api-sdk/fork_capture?serial=$esn` → `robot/work/vision-bench/NN.jpg`; exit 1 on a non-JPEG body. Endpoint: `CaptureSingleImage` high-res, raw JPEG; no SayText, animation, mirror mode or motion. Robot stays docked; USER sets each scene. | new `robot/vision-bench/grab.sh`, new `sdkapp/fork_vision.go` |
| V11 | Checklist `robot/work/vision-bench/checklist.json` (gitignored with the photos): `[{"photo":"01.jpg","et":["tass","laud"],"en":["cup","table"]}]`; USER writes it. | — |
| V12 | `visionbench` flags `-config myvectorConfig.json`, `-dir ../robot/work/vision-bench`, `-lang`, `-models` (default `gemma4:e4b-it-qat,qwen3.5:4b`), `-show`. Per photo: system `prompt.Render(prompt.Default(prompt.Ollama), prompt.Sample(l))`, user "What do you see?" / "Mida sa näed?" + JPEG data URL `ImageURLDetailLow`; `llm.SamplingFor(llm.Ollama, m)`, reasoning none, `prompt.ReplyCap(prompt.Ollama)`; unload each model after its run. | new `pkg/myvector/visionbench/main.go` |
| V13 | Score = matched / expected words (case-folded prefix match). Prints `model \| lang \| keywords % \| median ms \| errors`; `-show` prints every photo's answers side by side. Decision per language: highest keywords %, tie → median ms; the et winner also needs the USER fluency OK from `-show`, else the next model. | `visionbench` |
| D1 | (Stage 1.4 writes the per-language winners.) | `llm.DefaultVisionEN` / `DefaultVisionET` |

## Stages

Refactor-first: needed — `VisionModel` returns a bare model and the camera client comes from `forkClient` (brain target per language, no per-request routing); Stage 2 adds `ResolveVision` + the client swap, behaviour-preserving, before the setting lands.
ttr / sdkapp tests run under `make check` only when named `TestFork*` (`-run Fork`); every test uses httptest fakes, none touches the robot or Ollama.

| id | ruling | edit site |
|---|---|---|
| 1.1 | V10 endpoint + grab script. Proof `TestForkCapture`: unknown serial → body `error: …`, no Conn call; `bash -n robot/vision-bench/grab.sh`; `make check` (S5 scans only `fork_smoke.go` — the endpoint stays out of it). | new `sdkapp/fork_vision.go` + `_test.go`, `robot/vision-bench/grab.sh` |
| 1.2 | USER: restart wire-pod on this build, set 3–5 scenes in front of the docked robot, run `robot/vision-bench/grab.sh 5` (exits 1 on a bad grab), write V11 `checklist.json`. No robot audio; volume untouched. | — |
| 1.3 | V12 + V13 `visionbench`. Proof `TestScore` (table: all words → 1.0, none → 0, "Tassid" hits "tass", case fold) + `TestLoadChecklist` (missing photo → error, empty word list → error); `go test ./pkg/myvector/visionbench/`. | new `pkg/myvector/visionbench/main.go` + `_test.go` |
| 1.4 | From `chipper/`: `go run ./pkg/myvector/visionbench -lang et-EE -show`, then `-lang en-US -show`; USER fluency read; write row D1. No model passes → stop, one `docs/backlog.md` §OWED[USER] line. | this plan |
| 2.1 | V4 / V5 shape, today's rules: `ResolveVision` = brain target, except et-EE on Ollama → the en-US Ollama model or `DefaultENModel`; delete `VisionModel`; `forkCameraReq(r, &c)` + drift comment. Proof: `TestForkCamera*` green with only the call-signature edit; `TestResolveVision` table pins today's matrix; `TestForkCameraClient` → the chat lands on the vision target's fake; `scripts/upstream-drift.sh` green. | `llm/llm.go`, `llm_test.go`, `ttr/fork_prompt.go`, `ttr/kgsim_cmds.go`, `ttr/fork_client_test.go`, `scripts/upstream-drift.sh` |
| 3.1 | V2 + V3 + V4 in full; constants from D1. Proof `TestResolveVision` rows: brain Claude + unsaved → Ollama `DefaultVision<l>`; saved Claude + key → Claude; saved Claude, no key → local default; legacy openai Knowledge → false. `TestValidateVision`: bad provider / Claude without key / bad language → error; `Save` keeps `"llm"`. | `llm/llm.go`, `llm_test.go` |
| 3.2 | V5–V8 semantics. Proof `TestForkCameraLocalWithClaudeBrain`: Claude fake 0 hits, Ollama fake gets `DefaultVisionET`; `TestForkCameraOllamaDown`: Ollama fake 500 → Claude fake 0 hits; `TestForkCameraUnload`: brain = vision → 0 unloads; brain qwen, vision gemma → qwen before, gemma after; brain Claude → vision after only. | `ttr/fork_prompt.go`, `ttr/fork_client_test.go`, `ttr/fork_prompt_test.go` |
| 4.1 | V9 page + API. Proof `TestModelsVision`: tags fixture with `capabilities` → vision subset; no `capabilities` → empty. `TestSettingsVision`: POST round trip; absent keeps; Claude without key → 400; unknown provider → 400. | `llm/http.go`, `http_test.go`, `voice/settings.go`, `settings.html`, `voice/settings_test.go` |
| 5.1 | `make check` green. | — |
| 5.2 | USER live e2e (volume stays 1, ask first): vision = local default, et-EE brain = Claude; ask "mida sa näed?". Falsifier: `journalctl -u wire-pod --since -5min \| grep 'myvector llm: camera'` shows `ollama <DefaultVisionET>` (no match = fail). | — |
| 5.3 | `code-reviewer` subagent on the 1.1–4.1 commit range. | — |
