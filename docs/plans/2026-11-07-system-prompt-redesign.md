# System prompt redesign

Status: critique + options; no code yet. Trigger: "mis nädalapäev täna on?" → the robot says it doesn't know the date.

## Rulings (user, 2026-11-07)

| # | Ruling |
|---|---|
| R1 | Awareness: date + time + weekday, who the user is, location context. Own situation (docked, worn battery) not picked. |
| R2 | Web search for both Claude and Ollama: LLM tools where possible, other approaches considered too. |
| R3 | Behaviour: animations while speaking, camera ("what do you see"), multi-turn conversation. The Estonian Ollama model (EstLLM 8B) has no vision, so alternatives need research. |
| R4 | Long-term memory: built from every conversation, with an occasional pass that categorizes and organizes all memories. Must respect the Ollama vs Claude context limits. |
| R5 | Persona: warm companion, playful, but it must not become annoying or repetitive. 2–4 sentences, sometimes a question back. |
| R6 | Language: the configured language; foreign names and terms stay as they are, not Estonianised. |
| R7 | Prompt size: small Ollama models have tight prompt and context limits. |
| R8 | Claude gets its own prompt and context-length config, separate from local Ollama. |

Personal facts (R1 "who you are", R4 memory) go in gitignored files only, because `origin` is public.

## Current prompt (measured from source)

Built in three places: `CreateAIReq` (persona), `CreatePrompt` (STT note and command block), `forkAIReq` (`forkETLine`).

| Live config (2026-11-07) | value |
|---|---|
| et-EE and en-US brain | Claude `claude-haiku-5-5` (both) |
| `commands_enable` / `save_chat` | false / false |
| `openai_prompt` | empty → default persona |
| ET Ollama model / EN + vision fallback | EstLLM 8B Q4_K_M (text-only) / `qwen3.5:4b` |

| Block | chars | ≈ tokens | sent when |
|---|---|---|---|
| persona | 91 | 22 | always |
| STT note | 164 | 40 | always |
| "Answer in Estonian." | 21 | 5 | et-EE |
| command intro + 4 commands + mode note | ~2,600 | ~650–800 | `commands_enable` |
| remembered chat | ≤16 messages | unbounded | `save_chat` |
| reply cap | `MaxCompletionTokens: 2048` | — | always |

## Critique: 10 angles

Severity: CRITICAL / HIGH / MEDIUM / LOW.

| # | Angle | What's broken | Why it matters | Correct looks like |
|---|---|---|---|---|
| 1 | Grounding (CRITICAL) | No date, time, weekday, place or user in the prompt. | The bug that started this: the model refuses because it can't see the date. Any "today/tomorrow/this weekend" question fails or gets guessed. | A per-request context line with local date, weekday, time and timezone, home city, user name (from gitignored config). |
| 2 | Capability boundaries (HIGH) | Never says what the robot can or can't do: no internet, no live data, can't leave the charger. | It either makes up fresh facts (prices, news, opening hours) or refuses vaguely, as with the date. A future web-search tool needs a "when to search" rule. | 1–2 lines: what it knows (context line, memory), what it can fetch (search, camera), and to say "I don't know" briefly otherwise. |
| 3 | Speech output (HIGH) | Bans `.` and `-` ("No special characters, especially these: & ^ * # @ - ."). Gives no rule for numbers, dates, units, abbreviations or URLs. | Banning the full stop fights TTS sentence splitting and prosody; the model mostly ignores it, which teaches it to ignore the rest. Piper Mari reads digits and abbreviations unreliably. | Allow normal punctuation. Ban markdown, emoji and lists. Write numbers, dates and units as spoken Estonian words, and spell out abbreviations, or normalise them in code instead. |
| 4 | Persona and tone (MEDIUM) | Generic "helpful, animated robot". The command block then demands "many animations" and says "Animation takes precedence over words". No rule against repeating itself. | Conflicting instructions produce random tone. R5 wants warm, playful and not annoying, which needs explicit "vary openings, no stock catchphrases, ask back at most sometimes". | A short character sketch plus 3 anti-annoyance rules. |
| 5 | Length (MEDIUM) | Only "concise yet informative". The reply cap is 2048 tokens. | A voice answer of 2048 tokens is minutes of speech, and a long ramble ties up the robot. | "2–4 short sentences unless asked for more", plus a reply cap sized for speech (~200–300 tokens), set per provider. |
| 6 | Token budget / provider tiering (HIGH) | One prompt for every model. With commands on, the prompt grows from ~70 to ~800 tokens; with `save_chat`, 16 more messages are added with no token count. | EstLLM runs with a small `num_ctx` (2048–4096 to fit VRAM). Ollama silently drops tokens on overflow (which part goes, and whether the system prompt survives, is unverified). Claude has room for far more. | Per-provider profile (R8): prompt tier, history turns, memory budget, reply cap, `num_ctx`. The Ollama tier is a compact core; Claude gets the full tier. |
| 7 | Command block quality (HIGH) | Shouting (FAR, MANY, DONT BE AFRAID), a threat ("If you include an emoji, I will make you start over"), contradictions ("Use this MANY times" vs "Don't put two next to each other"), `getImage` params marked "not impl yet" but still offered, a confusing "Square brackets ([]) are not valid". | Small models copy the shouting and overuse animations; contradictions cost tokens and obedience. Every word is spent on every request. | One compact grammar line + one example + the emotion list; ≈150 tokens. Write in calm, positive voice. |
| 8 | Conversation and memory (HIGH) | The conversation-mode note and `newVoiceRequest` only exist when commands are on, so multi-turn is impossible with commands off. History is RAM-only (lost on restart), has no timestamps, and a chat from yesterday continues as if seconds ago. No long-term memory. | R3 multi-turn and R4 memory have no foundation. Stale history confuses replies. | History expires after a gap (e.g. 10 min). Follow-up listening is decoupled from animations. Long-term memory is a separate, budgeted block (see Research). |
| 9 | Language contract (MEDIUM) | The prompt is English, with "Answer in Estonian." tacked on last. No rule for names or terms (R6). en-US gets no language line at all. | EstLLM was benchmarked with exactly this line, so changing it needs a re-check. Without R6 the model may Estonianise names ("Tšatt-GPT"). | One language line per language: reply language + "keep names, brands and titles as written". |
| 10 | Maintainability (MEDIUM) | Assembled by string concatenation across 3 files in upstream code. `openai_prompt` replaces only the persona. The rendered prompt is untested; it is only visible via `DEBUG_PRINT_PROMPT`. | Changes drift unseen. The upstream diff grows if we keep editing `kgsim_cmds.go`. | One fork-owned builder (`chipper/pkg/myvector/prompt/`) from blocks with token estimates; a golden test of the rendered prompt per profile; upstream calls it through one `fork_*.go` hook. |

## Research (2026-11-07, 4 agents; † = unverified)

| Topic | Finding | Source |
|---|---|---|
| Search, Claude | Compat `/v1/chat/completions` supports function `tools`, including streaming. The server-side `web_search` tool is native `/v1/messages` only, at $10 per 1k searches plus result tokens. | platform.claude.com/docs/en/api/openai-sdk, …/web-search-tool |
| Search, Ollama | All 4 local models list `tools`. Probe (n=1): EstLLM searched for every prompt, qwen3.5/gemma4 answered fresh facts from memory and made them up. The compat endpoint has no `tool_choice`. | probe in session scratchpad; docs.ollama.com/api/openai-compatibility |
| Search provider | Tavily: 1k free per month, no card. Brave: $5 credit per month, card required, Estonian support unverified. Google custom-search API: closed. DuckDuckGo scraping: against ToS. SearXNG: free, self-hosted. | tavily.com/pricing, brave.com/search/api |
| Vision, ET | No Estonian vision model exists. qwen3.5:4b's Estonian is poor (Qwen3-4B scores 0.21 vs EstLLM 0.45 on the Estonian Native benchmark). Keeping both models loaded needs ≈10.5 GB, more than the card has. | arxiv 2510.21193 |
| Vision, ET options | Claude for camera turns: ~2–4 s†, EstLLM stays loaded. Offline: vision model writes English → EstLLM answers, ≈ today's total time because the EstLLM reload happens anyway. | bench `robot/work/et-llm/*/models.jsonl` |
| Memory, patterns | Reimplement in Go: Mem0-style extraction, Generative Agents scoring, Zep-style "supersede, never delete". Letta, Graphiti and Cognee are Python sidecars. Anthropic's memory tool only works on Claude and adds round-trips. | arxiv 2504.19413, 2304.03442 |
| Memory, sleep | When an LLM keeps rewriting memory, its usefulness first rises, then falls below no memory. Keep raw episodes; consolidate only on new evidence; keep a daily diary layer (MemoryBank); predict-calibrate (Nemori); store event time and learned time separately; rank by heat (MemoryOS). | arxiv 2605.12978, 2305.10250, 2508.03341, 2506.06326 |
| Memory, budgets | Ollama: core profile ≤150 tokens + ≤3 keyword facts ≈ 300 tokens. Claude: ≤3k tokens. Use 5-character prefixes or Vabamorf lemmas instead of embeddings until there are about 300 facts; then bge-m3 on CPU. | ollama.com/library/bge-m3 |

## Design rulings (user, 2026-11-07)

| id | ruling | edit site |
|---|---|---|
| P | Builder with 4 editable templates (et-EE, en-US) × (ollama, claude), in gitignored config, editable in the web UI. Shared content is written once as a block and referenced by tag (`{now}` `{user}` `{facts}` `{diary}` `{search}` `{speech}` `{commands}`), never copied into template text. | new `pkg/myvector/prompt`; `forkAIReq` calls it; UI in `webroot` |
| P-guard | A required tag missing from a template is appended at the end by the builder and flagged in the UI, so a block can never drop. Saving rejects unknown tags and anything over the tier's token budget. A preview shows the template rendered with sample data. Built-in defaults include "reset". A saved template that fails checks at load falls back to the default and logs. Keep the last 5 saved versions. | `prompt` validate + render; golden test per language × tier |
| P-budget | Per-tier config: reply cap, history turns, history gap, memory and search token budgets, Ollama `num_ctx`. Claude and Ollama are configured separately (R8). | `prompt` profile struct; `CreateAIReq` reply cap through the fork hook |
| S | Ollama: keyword router + pre-fetched snippets first. Claude tool comes later. Free, non-Claude providers are compared first: Tavily, Brave free credit, SearXNG, Ollama web search, on 10 Estonian queries. | new `pkg/myvector/search`; bench script under `robot/` or `scripts/` |
| S-guard | Per-provider monthly counter with a hard cap; results cached per query with a TTL. The future Claude tool gets max 1 search per turn and a daily cap. | `search` quota + cache in gitignored state |
| V | Claude sees photos only when explicitly chosen in a separate vision setting in the wire-pod UI, even when the brain is Claude. Default: the best local Ollama vision model, picked by a 3–5 photo bench (gemma4:e4b, gemma4:e2b, qwen3.5:4b, gemma3:4b; direct Estonian vs English → EstLLM). | `llm.VisionModel` + config field; `forkCameraReq`; UI selector |
| M | M2 now: transcripts → per-session fact extraction (evidence-substring check, unconfirmed) → nightly diary entry (append-only) + merge of touched categories + profile rebuilt from facts + fixed question set with revert. M3 extras later. | new `pkg/myvector/memory`; gitignored store |
| N | `{now}`: zone = robot jdoc `vic.RobotSettings.time_zone` by ESN, else `time.Local`; native line per language ("Praegu on laupäev, 7. november 2026, kell 9.43 (Europe/Tallinn)."), placed before the language line. | `prompt.Now`; `forkAIReq(aireq, esn)` |
| M-later | M3: bge-m3 search, weekly Claude pass, "mida sa minust tead?" / "unusta see". Revisit once M2 has run ~2 weeks. | — |

## Build order (each slice visible on the robot)

1. `{now}` line through a minimal builder → "mis nädalapäev täna on?" answered. Falsifier: golden test contains the Estonian weekday.
2. Templates + tags + P-guard + UI editor/preview; speech, persona and language fixes (critique 3–5, 7, 9).
3. History gap and follow-up listening decoupled from commands (critique 8).
4. Search bench → router + snippets for Ollama (S, S-guard).
5. Vision setting + local vision bench (V).
6. Memory M2: transcripts → extraction → nightly pass → `{facts}`/`{diary}` blocks.
