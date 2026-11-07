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

## Alternatives

Applies to every option: the date/weekday/time line ships first. It is ~20 tokens, the weekday comes from a Go Estonian name table, and it fixes the original bug.

| Decision | Option 1 (thin) | Option 2 | Option 3 (most mature) |
|---|---|---|---|
| P: prompt build | Patch the strings in place | Fork builder `pkg/myvector/prompt`, two tiers (Ollama ≈250 tokens core, Claude ≈600 tokens + extras), budgets per profile, golden tests | P2 + user-editable templates per language and tier in gitignored config |
| S: web search | Keyword router + pre-fetched snippets for both backends | Claude: function tool; Ollama: router + snippets (one Go search client, Tavily) | S2, but Claude uses the native server `web_search` (citations, dynamic filtering) |
| V: ET vision | Claude for camera turns when a key is saved, else today's qwen | V1 + offline fallback: vision model writes English → EstLLM answers | Offline only (V2's fallback), photos never leave home |
| M: memory | Hand-edited gitignored profile file only | Daily diary + facts (evidence-checked) + nightly per-category merge + profile rebuilt each night + fixed question set | M2 + bge-m3 search + weekly Claude whole-store pass + voice "mida sa minust tead / unusta see" |
