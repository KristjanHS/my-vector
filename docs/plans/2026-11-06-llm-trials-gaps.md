# LLM trials: work `docs/llm-trials.md` names but nothing implements

**Status**: CANDIDATES — none ruled; the user picks which to build before any stage runs.
**Source**: `docs/llm-trials.md` audited against `scripts/llm.sh` and `chipper/pkg/wirepod/ttr/` on 2026-11-06.

| id | gap (doc step) | edit site | falsifier |
|---|---|---|---|
| G1 | HA plugin `panic(err)`s when HA is unreachable, restarting wire-pod (step 6) | `ha_plugin()` in `scripts/llm.sh` — patch the panic in the built copy, like its other `sed` lines | `scripts/llm.sh ha-down`, ask "assist, hello" → `journalctl -u wire-pod` shows no panic/restart |
| G2 | OpenAI photo follow-up ignores the set model, always `openai.GPT4oMini` (step 4) | `forkCameraReq` (`ttr/fork_llm.go`) sets `aireq.Model` from `vars.APIConfig.Knowledge.Model` | `fork_llm_test.go` case: provider `openai`, model `gpt-4.1` → request model `gpt-4.1` |
| G3 | No effort dial for Claude (step 3: "would need a code change") — the endpoint's support for `reasoning_effort` is unverified | `forkAIReq` (`ttr/fork_prompt.go`) + a `--effort` flag in `use()` (`scripts/llm.sh`) | `scripts/llm.sh use claude --effort low` → `ping` answers; a bad value returns the provider's 400 |
| G4 | STT stays Vosk; the doc defers Whisper "if Vosk often mishears" (step 1) — upstream already ships `whisper`/`whisper.cpp` | `STT_SERVICE` in `chipper/start.sh` / `source.sh`, via `setup.sh` | log "Transcribed text" matches 5 spoken test questions that Vosk got wrong |
