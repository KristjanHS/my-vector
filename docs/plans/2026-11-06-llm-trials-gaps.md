# LLM trials: work `docs/llm-trials.md` names but nothing implements

**Status**: RULED 2026-11-06 — build all four, in order G2 → G1 → G3 → G4.
**Source**: `docs/llm-trials.md` audited against `scripts/llm.sh` and `chipper/pkg/wirepod/ttr/` on 2026-11-06.

| id | ruling | edit site | falsifier |
|---|---|---|---|
| G1 | HA plugin replies "Home Assistant is not reachable" instead of `panic(err)` | `ha_plugin()` in `scripts/llm.sh` — one more `sed` on the built copy + a `grep -q` that it applied | built `commandha.go` has no `panic(`; live: `scripts/llm.sh ha-down`, "assist, hello" → no restart in `journalctl -u wire-pod` |
| G2 | legacy (no saved page) photo follow-up uses `Knowledge.Model` when provider is `openai`, not `GPT4oMini` | `forkCameraReq` `!ok` branch (`ttr/fork_prompt.go`) | `fork_client_test.go` case: provider `openai`, model `gpt-4.1`, no saved page → request model `gpt-4.1` |
| G3 | per-language effort field on the picker page, Claude provider only; Ollama keeps `none`; no `llm.sh` flag | `pkg/myvector/llm` config + page, read in `forkAIReq`/`forkCameraReq` | probe FIRST: one Claude call with `reasoning_effort: low` — a 400 drops G3 (record in commit); then a `fork_prompt_test.go` case pins the field |
| G4 | English STT moves from Vosk to `myvector-stt` (:8092); failure is an error, never Vosk (as ET, plan E20) | `forkSTTRouter` (`preqs/fork_stt.go`); `et-stt/server.py` resident model = current lang's (swap on switch, one Whisper on GPU) | POST English TTS PCM to :8092 → transcript matches; `fork_stt_test.go` en-US routes to the service |
| G4.m | EN model: faster of `large-v3-turbo` / `distil-large-v3` (never full `large-v3`) | `et-stt/server.py` EN model constant + `et-stt/setup.sh` download | bench 5 English clips: lower median latency with correct text wins; within 10% → `large-v3-turbo` |
