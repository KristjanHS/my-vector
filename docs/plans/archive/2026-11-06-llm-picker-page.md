# Plan: Language, brain & voice page (per-language LLM profiles)

Status: ✅ SHIPPED · 2026-11-06 (live checklist → docs/backlog.md §OWED[USER]) · from `/qimpag` questions round (user ruled placement, lang→LLM rule, Claude transport, layout)

## Contract (ruled; not reopened)
- `/myvector/voice` retitled **"Language, brain & voice"**: top = active-language toggle `[EN | ET]` + **Save**; grid rows **LLM / Answer voice (T1) / Robot lines voice (T2)** × columns **EN | ET**; then speed slider + inline test row (text + ▶); collapsed **▸ Claude key / Ollama URL**. Compact.
- User's rule: "under both languages, there should be a selection of suitable models, defaulting to the ollama model that is currently defined for this model. both languages also provide claude api as alternative to ollama." ⇒ each language gets one model dropdown with optgroups **Local (Ollama)** (from Ollama `/api/tags`) and **Claude API** (from `GET https://api.anthropic.com/v1/models`, headers `x-api-key` + `anthropic-version: 2023-06-01`, only when a key is set). Provider follows the chosen model.
- Defaults: ET = existing `et_model` (else `DefaultETModel`); EN = `Knowledge.Model` when Knowledge is Ollama (provider `custom`, port 11434), else `qwen3.5:4b` (`scripts/llm.sh` ollama preset). Switching active language switches provider+model on the next request.
- Edge cases: Ollama down → Local group shows saved model only + warning; no Claude key → Claude group disabled with hint; saved model no longer installed → stays selected, marked "(not installed)".
- Claude via the OpenAI-compatible endpoint `https://api.anthropic.com/v1`; key entered on the page, masked on GET (never echoed in full); **Test** pings the chosen model.
- Fork-only code; no native Anthropic client; upstream `setup.html` untouched (Knowledge Graph, STT "Set Language").
- Out of scope: G1–G4 of `docs/plans/2026-11-06-llm-trials-gaps.md`. **Not subsumed: G2** (`openai` provider's photo model; `openai` is not a profile provider) — G2 edits the same `forkCameraReq`, so land it after Stage 2 in the legacy branch.

## Refactor-first assessment (ruled; no [USER DECISION] needed)
1. **New package `chipper/pkg/myvector/llm`, not `lang`.** `lang` is the language switch (hot-path `ForkContainsPhrase` via cache); the LLM profile needs HTTP listing, a ping and a secret. Move `DefaultETModel`/`ETModel`/`SaveETModel`/`ValidETModel`/`ETModelMax` from `lang` → `llm`; `lang.cached` drops `etModel`. `llm` imports `lang` (constants, `lang.Path()`) and `conf`, never `vars` — `ttr`/`voice` callers pass the Knowledge fields in.
2. **Resolver first (Stage 1), behaviour unchanged.** `forkAIReq`/`forkCameraReq` today hard-code lang checks + `forkOllama()`; they become thin appliers of `llm.Resolve(...)`. With no saved `"llm"` key, `Resolve` reproduces today's rules exactly (the legacy branch = backward-compatible `et_model` read). Existing `fork_prompt_test.go` cases pass with only identifier renames.
3. **Key lives in `chipper/myvectorConfig.json`, not `apiConfig.json`.** Both gitignored, but upstream `GET /api/get_kg_api` (`handleGetKGAPI`) echoes `Knowledge.Key` in full and `handleSetKGAPI`/`WriteConfigToDisk` overwrite Knowledge wholesale. The fork file is written only by `conf.Update` (atomic, key-preserving). File mode stays 0644 (`tts/server.py`, `et-stt/server.py`, `robot/firmware/settings.sh` read it as the user; root 0600 would break them) — acceptable on a single-user WSL host. Never logged, never copied into `vars.APIConfig`.
4. **Authority:** once `"llm"` is saved, the fork profile owns endpoint/key/model for both languages; upstream KG form still owns `Enable`, `IntentGraph`, prompt, `SaveChat`, `CommandsEnable`; its provider/model/endpoint/key are ignored. Deleting the `"llm"` key restores legacy. Document in `docs/llm-trials.md` (`scripts/llm.sh use` no longer changes the brain after a page Save).

## Constants
| Name | Value |
|---|---|
| `llm.DefaultENModel` | `"qwen3.5:4b"` |
| `llm.DefaultETModel` | moved from `lang`, unchanged |
| `llm.DefaultOllamaURL` | `"http://localhost:11434"` |
| `llm.AnthropicBase` (var, tests override) | `"https://api.anthropic.com"`; chat BaseURL = `AnthropicBase+"/v1"` |
| `llm.AnthropicVersion` | `"2023-06-01"` |
| `llm.OllamaPort` (var; replaces `forkOllamaPort`) | `"11434"` |
| `TagsTimeout` | 3 s |
| `ModelsTimeout` | 5 s, `?limit=100` |
| `ListCacheTTL` | 60 s, successes only, keyed by Ollama URL / sha256(key)[:8]; bypass `?refresh=1`; cleared on URL/key save |
| `ListMaxBody` | 1 MiB |
| `TestTimeout` | 60 s (cold EstLLM 8B load on Ollama) |
| Test prompt | `"Reply with the single word OK."`, `MaxCompletionTokens` 32, non-stream; reply truncated to 200 runes |
| `ModelMax` | 200 runes, no whitespace (old `ValidETModel` rule) |
| Key validity | trimmed, 20–200 chars, printable ASCII, no whitespace |
| Key mask | first 7 + `"…"` + last 4 (`sk-ant-…WxYz`); `"set"` if <16 chars; `""` when unset |
| Ollama URL validity | `http`/`https` scheme, non-empty host, ≤200 chars, stored without trailing `/` or `/v1` |

## Stage 1 — `pkg/myvector/llm` resolver, ET model moved out of `lang` (no behaviour change)
Focus: new `llm/llm.go`, `llm/llm_test.go`; `lang/lang.go`, `lang/lang_test.go`, `ttr/fork_prompt.go`, `ttr/fork_prompt_test.go`, `voice/settings.go`, `voice/settings_test.go`.
1. Types: `Profile{Provider, Model}` (Provider `"ollama"|"claude"`); `Config{Profiles map[string]Profile; OllamaURL, ClaudeKey, LegacyETModel string}` (file keys `llm`, `ollama_url`, `claude_key`, `et_model`; `Profiles==nil` = never saved); `KG{Provider, Endpoint, Model string}`; `Target{Provider, BaseURL, Key, Model string}` + `Ollama()`.
2. `Load()` reads `lang.Path()` per call (no cache — once per question; hand edits picked up). `ETModel()` = `LegacyETModel` or `DefaultETModel`. Move `ValidModel`, `SaveETModel` unchanged.
3. `Effective(c, kg)` → page-facing profiles + Ollama URL with the Contract defaults.
4. `Resolve(c, l, kg) (Target, bool)` legacy branch: EN → `false` (use upstream Knowledge as-is); ET → `Target{ollama, kg.Endpoint, "ollama", ETModel()}` only if `kg` is Ollama.
5. Rewrite `forkAIReq`/`forkCameraReq` on `Resolve`; `forkOllama()` → `llm.IsOllama(kg)`. `voice` handler keeps its `et_model` field, calls `llm.SaveETModel`/`llm.ETModel`.
6. Tests: `cd ~/projects/my-vector/chipper && go test ./pkg/myvector/... 2>&1 | tail -20 && go test -run Fork ./pkg/wirepod/ttr/ 2>&1 | tail -20`.
7. Falsifier: `Resolve` table test (legacy EN, ET+Ollama, ET+OpenAI); flipping the ET Ollama check fails `TestForkPromptEstonian`. `TestLangETModel` moves to `llm` with the same cases.

## Stage 2 — saved profiles reach the request: client, model, top_p, reasoning, unload
Focus: `llm/llm.go`, new `ttr/fork_client.go` + `ttr/fork_client_test.go`, `ttr/fork_prompt.go`, `kgsim.go`, `kgsim_cmds.go` (both already allowed), `scripts/upstream-drift.sh` comments.
1. `llm.Save(Config)` validates + `conf.Update`s `"llm"`, `"ollama_url"`, `"claude_key"` together (`et_model` left in place, read only while `"llm"` absent). Reject: provider ∉ {ollama, claude}; Claude profile with no key; clearing the key while a profile uses Claude.
2. `Resolve` saved branch: Ollama → `BaseURL=OllamaURL+"/v1"`, key `"ollama"`; Claude → `AnthropicBase+"/v1"`, `ClaudeKey`. Claude profile with empty key (hand edit) → `false` + log `myvector llm: <l> claude profile has no key — using wire-pod Knowledge`.
3. `forkClient(c *openai.Client) *openai.Client`: when `Resolve` ok, new client from Target, else `c`; logs `myvector llm: <lang> → <provider> <model> @ <BaseURL>` (never the key). One-line `c = forkClient(c)` after each provider `switch` in `StreamingKGSim` (kgsim.go ~:262) and `DoGetImage` (kgsim_cmds.go ~:474); update those two `allowed_edits` comments.
4. `forkAIReq`: `r.Model = t.Model`, then `r.TopP = forkTopP(r.Model)` — required because `CreateAIReq` set TopP from the Knowledge model (a Claude profile under a qwen Knowledge would send `top_p` → 400). `ReasoningEffort = "none"` only when `t.Ollama()`.
5. `forkCameraReq`: same model/top_p swap. ET+Ollama vision model = EN profile's model if EN is Ollama, else `DefaultENModel`; unload ET model first and vision model after (E26). `forkUnload(base, model)` takes the Target's Ollama base, not `Knowledge.Endpoint`.
6. Tests (httptest fake as Ollama/Claude; `AnthropicBase` pointed at it): `cd ~/projects/my-vector/chipper && go test ./pkg/myvector/llm/ 2>&1 | tail -20 && go test -run Fork ./pkg/wirepod/ttr/ 2>&1 | tail -20 && cd .. && scripts/upstream-drift.sh`.
7. Falsifiers: `TestForkClientClaude` — fake sees `Authorization: Bearer <key>`, path `/v1/chat/completions`, body without `top_p`/`reasoning_effort` (delete the TopP recompute → fails); `TestForkClientOllamaURL` — request hits saved `ollama_url`, not `Knowledge.Endpoint`; `TestForkKeyNotLeaked` — `vars.APIConfig.Knowledge.Key` unchanged after a Claude request; `TestForkCameraETUnloadsAtProfileURL`; Save rejection cases.

## Stage 3 — API: settings fields, model lists, ping
Focus: `voice/settings.go`, `voice/settings_test.go`, new `llm/http.go` + `llm/http_test.go`, `sdkapp/fork_voice.go`.
1. `GET /myvector/api/voice` adds `llm` (effective profiles), `ollamaUrl`, `claudeKey` (masked), `defaults{en-US,et-EE}`, `kgEnabled` (`Knowledge.Enable`; page warns when false).
2. POST accepts optional `llm{en-US,et-EE:{provider,model}}`, `ollama_url` (`*string`), `claude_key` (`*string`: absent = keep, `""` = clear); `MaxBytesReader` 4096 → 8192. Drop the `et_model` field (the page is its only client). `valid()` checks all before any write; `save()` calls `llm.Save` under `saveMu` before the lang switch; rollback unchanged.
3. `GET /myvector/api/llm/models[?refresh=1]` → `{ollama:{models[],error}, claude:{models[{id,name}],error,keySet}}`; both fetched concurrently; Claude skipped with `keySet:false` when no key. Errors carry HTTP status, never the key.
4. `POST /myvector/api/llm/test` `{lang, provider, model}` (uses the saved URL/key) → `{reply, ms}` or 502 with the provider message.
5. Register both routes in `fork_voice.go`.
6. Tests: `cd ~/projects/my-vector/chipper && go test ./pkg/myvector/... 2>&1 | tail -20 && go vet ./pkg/wirepod/sdkapp/`.
7. Falsifiers: GET body never contains the full test key (`strings.Contains`; an unmasked GET fails); fake `/v1/models` asserts both Anthropic headers; fake sleeping past `TagsTimeout` → `error` within 3.5 s; two GETs inside TTL hit the fake once, `?refresh=1` twice, an error is not cached; POST Claude profile with no key → 400 and file unchanged.

## Stage 4 — the page
Focus: `voice/settings.html`, `voice/settings_test.go` (page-strings test); `webroot/index.html` link text if it names the page (already allowed).
```
Language, brain & voice          [ EN | ET ]  [Save]
              EN                        ET
LLM         [qwen3.5:4b      ▾][Test] [EstLLM-8B…   ▾][Test]
Answer (T1) [af_bella ▾]              [mari ▾]
Robot (T2)  [af_bella ▾]              [mari ▾]
Speed ───●──── 1.20   [sample text…………] [▶]
▸ Claude key / Ollama URL   key: sk-ant-…WxYz [clear]  url: [http://localhost:11434]
status line / warnings
```
1. `<select>` with optgroups: "Local (Ollama)" from `/api/tags` (+ saved model as "(not installed)" / "(Ollama unreachable)"); "Claude API" `disabled` with hint "add a Claude key below" when `!keySet`. Provider derived from the option's group.
2. Voice dropdowns replace radios; `af_bella` stays first/default. ▶ speaks the active language's T1 through the existing `DemoAPI`.
3. Models list loads after the settings GET (page paints immediately, dropdowns fill async); ↻ re-fetches with `?refresh=1`. Key field is blank with the mask as placeholder; blank = keep; **clear** sends `""`.
4. Tests: `cd ~/projects/my-vector/chipper && go test ./pkg/myvector/voice/ 2>&1 | tail -20`.
5. Falsifier: page-strings test asserts `<optgroup label="Local (Ollama)"`, `Claude API`, the `/myvector/api/llm/models` and `/test` fetches, and no `id="et_model"` — fails against the current page.

## Stage 5 — gate, live e2e, review
1. `cd ~/projects/my-vector && make check`. The user starts wire-pod (Windows shortcut / `make start`) and ships from main (`make ship`) — Claude does neither.
2. Live checklist (highest impact first):
   1. ET → Claude `claude-haiku-5-5`, EN → Ollama `qwen3.5:4b`, active ET: ask an Estonian question → Estonian answer; `journalctl -u wire-pod | grep 'myvector llm'` → `et-EE → claude claude-haiku-5-5 @ https://api.anthropic.com/v1`. Toggle EN, Save, ask → `en-US → ollama qwen3.5:4b @ http://localhost:11434/v1`.
   2. Page open: defaults match the Contract; Local list = `ollama list`; Test in each column returns `OK` + ms.
   3. Stop Ollama, reload: warning + saved model only. Clear key: Claude group disabled with hint; saving a Claude profile without key → 400.
   4. Key hygiene: `grep -c sk-ant chipper/apiConfig.json` → 0; `curl -s localhost:8080/api/get_kg_api` and `/myvector/api/voice` show no full key; `git check-ignore -v chipper/myvectorConfig.json` matches.
   5. Save the upstream setup page's Knowledge form, ask again → journal still shows the fork profile. ET+Ollama "what do you see" → journal shows ET-model unload then vision-model unload.
3. `Agent(subagent_type="code-reviewer")` over the Stage 1–4 commits.
4. Update `docs/llm-trials.md` with the page-wins rule (Refactor-first §4).
