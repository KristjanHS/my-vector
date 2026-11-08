# et-EE chat prompt tuning per model

Status: 1.1–2.1 + 3.1 done (2026-11-08); next 3.1b combined round, then 3.2, then the 4.1 Q1 gate.

**Ask**: fix the two et picks of 2026-11-08-et-llm-research.md (docs/plans/) §Stage 2 with model-specific prompt changes:
`apertus-estllm:8b` answers long and list-shaped (`Samm 1:`, bullets); EstLLM-8B-1125 drops ‹nimi›/‹kodukoht›/family into unrelated replies.

## Recon (2026-11-08)

| id | finding | edit site |
|---|---|---|
| C1 | System prompt = `prompt.Render(s.Live(brain), prompt.Vars{…})` in `forkAIReq`; `{lang}` = `prompt.LangLine` ("Answer in Estonian. …"), last block of `order` | `ttr/fork_prompt.go`, `prompt/defaults.go` |
| C2 | Personal facts reach the prompt two ways: `{user}` = `prompt.UserBlock` (about-me, "About the person you are talking with:"), unconditional; `{facts}` = `memory.Block` profile (never dropped) + top `FactsOllama` facts | `prompt/store.go`, `memory/block.go` |
| C3 | Live today: the about-me is set, memory profile empty → `{user}` is the leak source; the saved `ollama` template replaces `persona`'s "2 to 4 short sentences" with "short sentences" | gitignored `myvectorPrompt.json` |
| C4 | No per-model prompt: templates key on brain (`prompt.Claude`/`prompt.Ollama`); per-model are only `llm.SamplingFor` `families` and `Thinking`. Neither et pick has a `families` row → temperature 1, top_p 1 | `llm/sampling.go` |
| C5 | `routeprobe -set` arms are thinking levels only (`probekit.Arms`); calls run `ForkProbeLLMFull` → `forkAIReq` on `isolate()`'s temp copy of the prompt + memory store, so a template arm = a `prompt.Save` into that copy, no prod edit | `routeprobe/main.go` |
| C6 | Ollama templates (`ollama show --template`): Apertus copy wraps our system turn + the card's `Deliberation: disabled` developer block, stops on `<|assistant_end|>`; EstLLM-1125's llama3.1 template writes `Cutting Knowledge Date: December 2023` only when tools are sent — never on our requests | Ollama models (no repo edit) |
| C7 | `{user}` is an optional tag; `{speech}`, `{commands}`, `{now}`, `{lang}` are `prompt.RequiredTags` (appended if missing); an Ollama template must render ≤ `prompt.Budget` 600 tokens | `prompt/store.go` |
| C8 | `judge` (`judgeRubric`) already scores 1–3 sentences + no markdown; it never sees the profile, so it can't score leaks. `ForkProbeLLMResult.Answer` is `removeSpecialCharacters`-cleaned → count markers on `RawAnswer` | `routeprobe/judge.go`, `ttr/fork_probe.go` |

**Sibling plans — overlap, no conflict, one rule revised:**
- 2026-11-07-system-prompt-redesign.md (docs/plans/) rows P/U/SP own the template + blocks; this plan adds variant text and at most one block seam, and keeps P-guard (budget, required tags).
- 2026-11-08-memory-m2.md (docs/plans/) M18 owns `memory.Block` ranking and "profile never dropped"; arm G (relevance gate) touches `{user}` only and leaves M18 alone until it wins — then M18 gets a ruling row, not a fork.
- 2026-11-08-local-thinking-levels.md (docs/plans/) T16 owns `routeprobe -set`; prompt arms are a second axis beside `-think`, and the dev/holdout sets stay byte-identical (`TestChatSets`).
- ⚠ docs/model-prompt-formats.md §Main rule says "same prompt text for every model" — Stage 4.1 rewrites that line if a per-model prompt ships.

## Metrics (pre-registered, before any run)

Counted per model × arm on et-EE replies; `{{cmd||param}}` stripped first; "long" = `vars.ForkLongAsk` asks, reported apart.

| id | metric | definition |
|---|---|---|
| W | words / reply | median + p90 of whitespace-split words of `Answer`, non-long and long asks separately |
| L | list rate | % replies whose `RawAnswer` matches `listRe` (routeprobe/count.go): a `-*•` bullet, `N.`/`N)` + capital, `Samm N`, `**` or a `#` heading — not an ordinal date |
| K | leak rate | % replies on every non-`self` ask with a reply word starting with a profile token's first min(5, len) runes and ≤ `suffixSlack` runes longer, case-folded |
| H | recall rate | % `self` asks (Stage 2.1) whose reply contains ≥ 1 profile token — guards against over-suppression |
| J, P | judge, latency | `judge` mean; `probekit.Summary` p50/p90 |

Profile tokens (the name included — a greeting by ‹nimi› is a leak, user 2026-11-08): read at run time from the isolated copies (about-me + memory profile + active facts): capitalised words ≥ 3 runes (a sentence starter only if also capitalised mid-sentence), minus `stopTokens` (routeprobe/arms.go) and words in the ask. Never written to the doc, a test fixture or stdout — only counts print.

**Selection rule (default; Stage 3.1 validates):** an arm passes for a model when, vs its baseline arm on the same run: K ≤ 5 %, L ≤ 5 %, non-long W median ≤ 35 and p90 ≤ 60, H ≥ 75 %, J drop ≤ 5 points, p90 latency ≤ baseline + 20 %.
Winner = highest J among passing arms; tie → fewest prompt tokens (`MaxPrompt`).

**Stopping point (default):** stage 3.1 runs every arm once (Apertus dev + leakdev `-reps 2`; EstLLM-1125 leakdev only, arms `0,D,F,G,N`, `-reps 1` — user cut runs 8×, 2026-11-08); a model whose baseline already passes stops there. No arm passes → one round of the two best arms combined, then stop.
Stage 3.2 holdout (`-reps 1`, user 2026-11-08) confirms only the winner; failing holdout = no wiring, the result goes to the commit message and `docs/backlog.md`, not a new arm hunt.

## Prompt variants

Every arm is a delta on arm `0` (the live saved `ollama` template + today's request fields); each model runs only its targeted arms (user, 2026-11-08): Apertus `0,P,R,X,E,S,N` · EstLLM-1125 `0,X,E,S,D,F,G,N`.

| id | change | target | source |
|---|---|---|---|
| P | persona length line → positive budget: "Answer in one to three short spoken sentences of flowing prose unless asked for more, as if talking; your words are read aloud by text-to-speech." | Apertus | Anthropic prompting best practices; LiveKit + ElevenLabs agent prompting guides |
| R | one reminder line after `{lang}`, last in the prompt: "Unless asked for more, reply in one to three spoken sentences — no lists, no steps." | Apertus | Anthropic long-context tips (instructions at the end) |
| X | three Estonian exemplars (ask → 1–2 sentence reply): a fact, a how-to answered without steps, a chit-chat ask answered with no personal detail | both | OpenAI Realtime prompting guide (sample phrases); Ollama Modelfile `MESSAGE` |
| E | persona + P's budget line in Estonian (`{speech}`/`{lang}` stay English code blocks) | both | arXiv 2512.02841 vs 2409.07054 (mixed, not Estonian-specific) |
| S | sampling temperature 0.4, top_p 0.9 instead of today's 1 / 1 (no `families` row) | both | tartuNLP model cards' example (0.4); swiss-ai Apertus card (top_p 0.9) |
| D | first lines `Cutting Knowledge Date: December 2023` + `{now}` (the header EstLLM-1125 was trained with, C6) | EstLLM-1125 | tartuNLP EstLLM-1125 card §Limitations; Meta Llama 3.1 prompt format |
| F | `{user}` fenced: "Background about the user, shown in every chat and irrelevant to almost every ask. Use it only when they ask about themselves, their family or home; otherwise never mention it." | EstLLM-1125 | ChatGPT custom-instructions wrapper (Willison 2023); Shi et al. 2023; Anthropic XML-tag guidance |
| G | gated: about-me sent only when the ask has a `firstPerson` form (routeprobe/arms.go) or a profile token; else `{user}` empty (per call, Stage 1.3) | EstLLM-1125 | OP-Bench arXiv 2601.13722; CIMemories arXiv 2511.14937 |
| G2 | G's gate minus the "to me" forms (`toMe`: mulle/minule/meile); K = 0 on both leak sets by construction, so it tests H and J only (user 2026-11-08) | EstLLM-1125 | G's 3.1 leaks were all on `mulle` asks |
| N | `{user}` removed — diagnostic only (K floor, J ceiling); fails H by construction, never a winner | both | CIMemories arXiv 2511.14937 |

## Stages

| id | ruling | edit site |
|---|---|---|
| 1.1 | `-prompt A,B,…` on `routeprobe -set`: each arm id maps to a func in a new `promptArms` table that rewrites the isolated store's `ollama` template (`prompt.Save`) and/or sets a new `llm.ProbeSampling` override (mirrors `llm.ProbeThinking`, nil in prod); arms × `-think` interleave in a per-item seeded shuffle (`order`; S runs with `-think none` only); arm label = `<think>/<prompt>`. Baseline arm `0` = the live saved template, untouched. Proof `TestPromptArms`: every arm's template passes `prompt.Validate`; arm `0` leaves the temp store byte-equal. | `routeprobe/main.go`, new `routeprobe/arms.go` + `_test.go`, `llm/sampling.go` |
| 1.2 | Counters W, L, K, H (§Metrics) in a pure func `count(ask, raw, answer, tokens)`; the arm line gains `words m/p90 · list % · leak % · recall %`. Proof `TestCount`: a `Samm 1:` reply → list; `**x**` → list; "Vesikülas" vs token Vesiküla → leak; ask containing the token → not a leak; fictional tokens only. | new `routeprobe/count.go` + `_test.go`, `routeprobe/main.go` |
| 1.3 | Per-call arms: an arm may rewrite the isolated about-me before each call (`prompt.Save(prompt.Ollama, nil, &about)`); `forkAIReq` re-`Load`s the store per request, so fence/gate/drop arms need no prod edit. Proof `TestPromptArms`: arm G on a `tempt` ask → temp about-me empty; on a `self` ask → unchanged. | `routeprobe/arms.go` |
| 2.1 | Sets `leakdev` + `leakholdout` (et-EE): 8 `tempt` asks each (story, weekend plan, "kuidas sul läheb?", recipe, book tip, joke, "mida sa täna teeksid?", gift idea) + 3 `self` asks each ("kus ma elan?", "mis mu nimi on?", "kes ma olen?" / holdout paraphrases — each answerable by a capitalised token). `-set` takes a comma list. Proof: `TestChatSets` extended — leak sets disjoint from each other and from dev/holdout, dev/holdout unchanged. | `routeprobe/chatset.go` + `_test.go` |
| 3.1 | Dev run, per model: `routeprobe -lang et-EE -brain ollama:<tag> -set dev,leakdev -think none -prompt <that model's arms, §Prompt variants> -reps 2`; record each arm line in §Results. Apply §Metrics stop rule. Raw output to `robot/work/et-prompt-tuning/` (gitignored). | this plan |
| 3.1b | §Metrics stop rule, no arm passed: `-prompt` takes `A+B` (template rewrites chained in order; at most one part sets sampling, one the about-me; `TestPromptArms` covers S+R, D+G2, P+R). Run Apertus `-set dev,leakdev -prompt 0,S+R -reps 2` (108 calls), EstLLM-1125 `-set leakdev -prompt 0,D+G,D+G2 -reps 2` (66 calls; user 2026-11-08); rows → §Results. | `routeprobe/arms.go`, `_test.go` |
| 3.2 | Holdout confirm of each model's passing arm: `-set holdout,leakholdout -prompt 0,<winner> -reps 1` (EstLLM `-set leakholdout`); none passes → no wiring, close per §Metrics. | this plan |
| 4.1 | `[USER DECISION]` wiring: see §Open questions Q1. Then the winning text lands in `prompt` defaults or per-model store keys, `llm.SamplingFor` gets the et rows, the golden tests regenerate, docs/model-prompt-formats.md §Main rule is rewritten. | `prompt/defaults.go`/`store.go`, `llm/sampling.go`, `prompt/testdata/*.golden` |
| 5.1 | `make check` green. `[USER]` live check (robot sound lock: `robot/quiet/quiet.sh status` first; volume as the user left it): ask 1 story + 1 how-to + "kus ma elan?" in Estonian on the shipped model → short prose, no ‹nimi›/‹kodukoht› in the first two, correct in the third. | — |
| 5.2 | `code-reviewer` subagent on the 1.1–4.1 commit range. | — |

## Results

| model | arm | W m/p90 | L % | K % | H % | J | p50/p90 s |
|---|---|---|---|---|---|---|---|
| Apertus | 0 | 26/85 | 20 | 10 | 50 | 55.9 | 2.6/21.2 |
| Apertus | P | 39/110 | 15 | 23 | 83 | 45.0 | 3.9/29.6 |
| Apertus | R | 25/76 | 7 | 12 | 50 | 61.5 | 3.0/21.5 |
| Apertus | X | 34/93 | 17 | 2 | 17 | 59.1 | 2.7/23.0 |
| Apertus | E | 45/119 | 11 | 12 | 67 | 51.8 | 3.5/39.1 |
| Apertus | S | 25/81 | 13 | 8 | 50 | 66.9 | 1.9/32.0 |
| Apertus | N | 28/79 | 15 | 0 | 0 | 55.3 | 2.8/23.9 |
| EstLLM-1125 | 0 | 15/51 | 0 | 25 | 100 | 51.8 | 1.5/6.8 |
| EstLLM-1125 | D | 29/44 | 0 | 25 | 33 | 52.5 | 3.1/8.6 |
| EstLLM-1125 | F | 21/28 | 0 | 50 | 67 | 44.0 | 1.6/5.0 |
| EstLLM-1125 | G | 11/25 | 0 | 38 | 100 | 49.7 | 1.5/5.0 |
| EstLLM-1125 | N | 20/29 | 0 | 0 | 33 | 71.5 | 2.1/4.8 |
| Apertus | 0 (3.1b) | 24/85 | 13 | 12 | 50 | 55.4 | 1.5/13.8 |
| Apertus | S+R | 21/44 | 6 | 4 | 83 | 77.8 | 1.3/4.6 |
| EstLLM-1125 | 0 (3.1b) | 18/26 | 0 | 44 | 67 | 45.1 | 1.0/1.6 |
| EstLLM-1125 | D+G | 16/35 | 0 | 12 | 83 | 57.7 | 1.0/1.9 |
| EstLLM-1125 | D+G2 | 22/39 | 0 | 0 | 100 | 52.8 | 1.2/2.6 |

## Open questions

- `[USER DECISION]` Q4 (3.1b, no strict pass): Apertus S+R fails only L (6 % vs ≤ 5 %), EstLLM-1125 D+G2 only p90 latency (2.6 s vs ≤ 1.86 s);
  waive that one bar → Stage 3.2 holdout of that arm, or close the plan per §Metrics (no wiring).
- `[USER DECISION]` Q1 (Stage 4.1) wiring: one shared `ollama` template change if a single arm wins both models; else per-model text (store key `ollama:<model>` vs a style line beside `llm.SamplingFor` `families`) — which model ships stays the `docs/backlog.md` §OWED[USER] et-model line.
- `[USER DECISION]` Q3 (Stage 4.1, only if G wins): gating `{user}` in prod drops the about-me from most asks, and the same gate on `{facts}` needs a 2026-11-08-memory-m2.md (docs/plans/) M18 ruling.

## Sources

Fetched 2026-11-08; dates are the source's own (model cards: from the version tag).
- swiss-ai Apertus-8B-Instruct-2509 card (2025-09): https://huggingface.co/swiss-ai/Apertus-8B-Instruct-2509
- tartuNLP Apertus-EstLLM-8B-Instruct-0326 card (2026-03): https://huggingface.co/tartuNLP/Apertus-EstLLM-8B-Instruct-0326
- tartuNLP Llama-3.1-EstLLM-8B-Instruct-1125 card (2025-11): https://huggingface.co/tartuNLP/Llama-3.1-EstLLM-8B-Instruct-1125
- EstLLM paper, arXiv 2603.02041 (2026-03, v2 2026-08): https://arxiv.org/html/2603.02041
- Meta Llama 3.1 prompt format: https://dev.meta.ai/llama/docs/model-cards-and-prompt-formats/llama3_1
- Ollama llama3.1 template: https://ollama.com/library/llama3.1/blobs/948af2743fc7 · Modelfile docs: https://docs.ollama.com/modelfile
- Anthropic prompting best practices (current): https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices
- OpenAI Realtime prompting guide: https://developers.openai.com/cookbook/examples/realtime_prompting_guide
- LiveKit agents prompting: https://docs.livekit.io/agents/build/prompting/ · ElevenLabs agents prompting guide: https://elevenlabs.io/docs/agents-platform/best-practices/prompting-guide
- Yuan et al., Following Length Constraints in Instructions, arXiv 2406.17744 (2024-06): https://arxiv.org/abs/2406.17744
- Singhal et al., A Long Way to Go: length in RLHF, arXiv 2310.03716 (2023-10): https://arxiv.org/abs/2310.03716
- Prompt language: arXiv 2512.02841 (2025-12) https://arxiv.org/pdf/2512.02841 · arXiv 2409.07054 (2024-09) https://arxiv.org/abs/2409.07054
- Willison, ChatGPT custom instructions prompt (2023-07-20): https://simonwillison.net/2023/Jul/20/custom-instructions/
- OP-Bench over-personalization, arXiv 2601.13722 (2026-01): https://arxiv.org/abs/2601.13722 · RPEval arXiv 2601.16621: https://arxiv.org/pdf/2601.16621
- CIMemories, arXiv 2511.14937 (2025-11): https://arxiv.org/abs/2511.14937
- Shi et al., LLMs Can Be Easily Distracted by Irrelevant Context, arXiv 2302.00093 (2023-02): https://arxiv.org/abs/2302.00093
- Jain et al., memory profiles and sycophancy, arXiv 2509.12517 (2025-09): https://arxiv.org/abs/2509.12517
