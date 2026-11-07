# Trying LLM options for Vector, one by one

Goal: let Vector answer free-form questions with an LLM, comparing the community options found on
2026-11-05. Each step below is self-contained and reversible; do them in order (cheapest first) and
fill in the results table as you go.

Almost everything runs through **`scripts/llm.sh`**. It changes wire-pod's LLM settings through the
running server's web API (no restart), and `ping` sends a request built like wire-pod's (same
endpoint, key and model), so you see the answer and its latency without talking to the robot.

⚠ Once the **Language, brain & voice** page (`/myvector/voice`) is saved, its per-language LLM (Ollama or
Claude) wins: `scripts/llm.sh use` no longer changes the brain. Delete `"llm"` from `chipper/myvectorConfig.json` to go back.

| # | option | cost | what it adds |
|---|--------|------|--------------|
| 1 | Local Ollama (your Windows Ollama) | free | plain Q&A, private — same setup as the dev.to "WirePod + Gemma" write-up |
| 2 | Ollama + robot actions + camera | free | animations, follow-up listening, "what do you see?" — stands in for VectorSoul |
| 3 | Claude Haiku 5.5 (Anthropic API) | your $100/month credit | fast, high-quality answers |
| 4 | OpenAI | paid key | wire-pod's original target; OpenAI voices |
| 5 | Together AI | paid key | big open models in the cloud |
| 6 | Home Assistant plugin | free (local) | "assist …" commands to HA's agent; useful only if you have smart-home devices |
| 7 | Other OpenAI-compatible clouds (Gemini, …) | varies | optional, untested |

## 0. One-time prep

```bash
scripts/llm.sh backup        # snapshot of the current settings -> robot/work/llm/kg-<time>.json
make server-pin              # once: pins + restarts wire-pod on HEAD so it runs the Claude fix (step 3); Vector reconnects in ~30 s
journalctl -u wire-pod -f    # optional, second terminal: watch requests ("Using <model>", "LLM returned no response")
```

Keys live in `~/.config/my-vector/keys.env` (outside the repo, chmod 600). `ANTHROPIC_API_KEY` is already in it
(copied from crash-dash's workspace key). Add other keys as you reach their step:

```bash
echo 'OPENAI_API_KEY=sk-...' >> ~/.config/my-vector/keys.env
```

**How to ask Vector** (the same for every option):

- Default: "Hey Vector" … *(ding)* … "I have a question" … *(he listens again)* … "Why is the sky blue?"
- With `--intentgraph`: anything wire-pod doesn't recognise goes to the LLM — "Hey Vector, why is the sky blue?"
- With `--commands`: the LLM can also play animations, keep listening for a follow-up, and take a photo ("what do you see?").
- `--remember` keeps the conversation history between questions.

**Results** (fill in as you go):

| # | works on robot? | first words (`ping`) | answer quality | notes |
|---|-----------------|----------------------|----------------|-------|
| 1 | yes, answers in ~1 s | 0.34 s (measured) | good, on-topic | Vector's voice hard to hear → wants a soft female voice |
| 2 | sad story: yes; "what do you see": failed → fixed `df40de0`, retest after restart | 0.32 s | long story, ok | voice → `2026-11-05-vector-voice-research.md` (docs/plans/) |
| 3 | | 0.5–0.6 s (measured) | | |
| 4 | | | | |
| 5 | | | | |
| 6 | | | | |

## 1. Local Ollama (free, private)

Your Windows Ollama is reachable from WSL at `localhost:11434` (mirrored networking), so nothing
needs to be opened up.

```bash
scripts/llm.sh warm                         # load qwen3.5:4b (first load ~60 s; then it stays loaded)
scripts/llm.sh use ollama                   # save + ping; expect an answer within ~1 s
```

Then ask Vector a question (see "How to ask" above). Things to try:

- Default `qwen3.5:4b` (blind mean 4.40 vs `gemma3:4b-it-qat` 3.70; wire-pod sends it `reasoning_effort: none`).
- Another installed model: `scripts/llm.sh models ollama`, then e.g. `scripts/llm.sh use ollama cas/mistral-7b-instruct-v0.3:latest`.
  Avoid `deepseek-r1` (it "thinks out loud", and Vector would read that out) and the coder models.
- `scripts/llm.sh use ollama --intentgraph` to skip the "I have a question" step.
  `--commands`/`--intentgraph`/`--remember` stick: a later `use` keeps them; `--no-intentgraph` (etc.) turns one off.

What can go wrong:

- **First answer never comes** — the model was still loading after a Windows Ollama restart. Run `warm` first.
- **Answer fits a different question** — that is speech-to-text (myvector-stt, large-v3-turbo for English),
  not the LLM. The log shows the transcribed text: `journalctl -u wire-pod | grep 'Transcribed text'`.

## 2. Ollama + robot actions + camera

The **VectorSoul** project (camera → local Gemma → spoken answer) is gone: `chatde/vector-gemma4` and
`chatura-dev/vector-gemma4` both return 404 as of 2026-11-05. wire-pod's built-in "LLM commands" cover
the same idea, and `qwen3.5:4b` can read images.

```bash
scripts/llm.sh use ollama --commands --intentgraph --remember
```

Try:

- "Hey Vector, what do you see?" → he says what he's doing, counts **3, 2, 1**, takes a photo
  (shutter animation), then describes it.
- "Hey Vector, tell me a sad story" → animations while he talks.
- Ask something open-ended → he may keep listening for your follow-up without the wake word.

Notes:

- Animations play while docked. The firmware patches keep him on the charger; if one ever tries to
  drive him off, drop `--commands`.
- Only 8 built-in intents still match first (time, timers, weather, "take a photo", names, "I have a question");
  the rest go to the LLM (`forkAllowedIntents`). Follow-up listening only works after "I have a question" or "let's talk".
- Small models sometimes write the command names out loud instead of running them. If that happens,
  try the bigger `mistral` model or go to step 3.

## 3. Claude Haiku 5.5 (Anthropic API, your monthly credit)

wire-pod reaches Claude through Anthropic's OpenAI-compatible endpoint. Two things make it work here:

- **The fork fix.** wire-pod always sent `top_p: 1`, and Claude rejects it
  (`400 "top_p is deprecated for this model"`). `chipper/pkg/wirepod/ttr/fork_llm.go` drops it for
  `claude*` models. Run `make server-pin` once (step 0) so the server picks it up.
- **The key must be workspace-scoped** (same rule as crash-dash). An org-level key fails every call.

```bash
scripts/llm.sh use claude                   # claude-haiku-5-5, cheapest; expect ~0.6 s to first words
scripts/llm.sh use claude --commands --intentgraph --remember   # then the step-2 tests, on Claude
```

- **Thinking:** claude-haiku-5-5 thinks by default inside `max_completion_tokens` (keep `prompt.ReplyCap` ≥1024);
  `reasoning_effort` does not disable it, body `"thinking":{"type":"disabled"}` does (`robot/work/live-check/claude_think.py`).
- Larger model later: `scripts/llm.sh models claude`, then e.g. `scripts/llm.sh use claude claude-sonnet-5-5`.
- Spend: check usage in the Anthropic Console (the workspace that owns the key). A spoken answer is a
  few hundred tokens, so the credit lasts a long time on Haiku.
- **Key exposure:** wire-pod stores the key in plain text and serves it, with no login, at
  `:80` and `:8080` (`/api/get_kg_api`); `scripts/wire-pod-guard.nft` limits both to this PC and Vector, only
  while wire-pod runs as the service (`make daemon-enable`). Keep a workspace spend limit. `use ollama` overwrites the key; `off` doesn't.

## 4. OpenAI

Needs a key with billing enabled (platform.openai.com → API keys; prepaid credit).

```bash
echo 'OPENAI_API_KEY=sk-...' >> ~/.config/my-vector/keys.env
scripts/llm.sh use openai                   # gpt-4o-mini
scripts/llm.sh use openai --voice nova      # answers in an OpenAI voice instead of Vector's
```

Voices: `alloy onyx fable shimmer nova echo`. Notes:

- The photo follow-up (step 2) always uses `gpt-4o-mini`, whatever model you set.
- If OpenAI rejects the model ("does not exist"), wire-pod falls back to `gpt-3.5-turbo`.

## 5. Together AI

Needs a key (api.together.ai; new accounts usually get trial credit).

```bash
echo 'TOGETHER_API_KEY=...' >> ~/.config/my-vector/keys.env
scripts/llm.sh models together | grep -i instruct          # model ids change; pick one
scripts/llm.sh use together meta-llama/Llama-3.3-70B-Instruct-Turbo
```

Always set the model explicitly (the script does). wire-pod's own fallback on the camera path is a
retired Llama-2 model.

## 6. Home Assistant plugin (NonaSuomy/WirePodVectorCommandHomeAssistant)

A wire-pod plugin: anything you say that starts with **"assist"** is sent to a Home Assistant
conversation agent, and Vector reads the reply aloud. HA's agent can be an LLM (your Ollama or Claude)
that can also switch your smart-home devices. Without smart-home devices, this is the same as steps 1–3
with extra parts.

1. **Start HA** (Docker Desktop):
   ```bash
   scripts/llm.sh ha-up                     # http://localhost:8123, config in robot/work/llm/ha-config
   ```
2. **In the browser** (http://localhost:8123): create the owner account, then
   Settings → Devices & services → Add integration:
   - **Ollama** → URL `http://host.docker.internal:11434`, model `gemma3:4b-it-qat`
     (optionally let it control Home Assistant), **or**
   - **Anthropic** → your Claude key (Haiku).

   Then Settings → Voice assistants → Assist → set **Conversation agent** to it.
3. **Token**: your profile (bottom-left) → Security → Long-lived access tokens → Create:
   ```bash
   echo 'HA_TOKEN=eyJ...' >> ~/.config/my-vector/keys.env
   scripts/llm.sh ha-agents                 # e.g. conversation.ollama_conversation
   echo 'HA_AGENT=conversation.ollama_conversation' >> ~/.config/my-vector/keys.env
   scripts/llm.sh ha-say "Why is the sky blue?"   # HA answers directly
   ```
4. **Plugin** (cloned and built from `robot/work/llm/`, so the GPL source never enters this repo):
   ```bash
   scripts/llm.sh ha-plugin                 # fills in URL/token/agent, builds chipper/plugins/commandha.so
   make start                               # plugins load at startup
   journalctl -u wire-pod | grep commandha  # expect: "Action func in plugin commandha.so is OK"
   ```
5. **Ask Vector:** "Hey Vector" … "assist, why is the sky blue?" / "assist, turn on the kitchen lights".

Caveats:

- If HA can't be reached, the plugin **crashes wire-pod** (upstream code panics). systemd restarts
  it within 5 s, but Vector drops that request.
- If HA can't reach Ollama at `host.docker.internal`, set the Windows environment variable
  `OLLAMA_HOST=0.0.0.0`, restart Ollama, and allow port 11434 through the Windows firewall.
- The built `.so` contains the HA token (gitignored, chmod 600).

Remove it all: `scripts/llm.sh ha-plugin remove && make start`, then `scripts/llm.sh ha-down`.

## 7. Optional: other OpenAI-compatible clouds (untested)

`custom` takes any server that speaks the OpenAI chat API. There's a preset for Gemini:

```bash
echo 'GEMINI_API_KEY=...' >> ~/.config/my-vector/keys.env      # aistudio.google.com
scripts/llm.sh models gemini
scripts/llm.sh use gemini gemini-2.5-flash
```

For anything else (OpenRouter, Groq, …), use the web UI's "Custom" provider at
http://localhost:8080. The endpoint is the base URL without `/chat/completions` (wire-pod appends that),
e.g. `https://openrouter.ai/api/v1`.

## Skipped

- **VectorSoul** — repository deleted (see step 2).
- **dev.to WirePod + Gemma write-up** — that is step 1; it has no extra config.
- **r/AnkiVector "best model" thread** — a discussion, not a project. Its "Vector asks me to check LLM
  settings after a month" is a key or credit that ran out. `scripts/llm.sh ping` shows the provider's
  exact error.

## Troubleshooting

- **Vector says nothing, or asks you to check the LLM settings:** run `scripts/llm.sh ping`. It uses the
  same endpoint, key and model, so a key, credit or model error shows up there with the provider's message.
  If `ping` works but Vector doesn't on Claude, the server predates the fix: `make server-pin`.
- **"Hey Vector" ignored:** behavior control may be held (SDK "Assume"); release it in the web UI.
- **Back to where you started:** `scripts/llm.sh restore robot/work/llm/kg-<time>.json` (from step 0), or `scripts/llm.sh off`.
