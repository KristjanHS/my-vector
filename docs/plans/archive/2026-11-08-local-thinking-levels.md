# Local-model thinking levels: measure, then decide

Status: ✅ SHIPPED 2026-11-09 — verdict: keep off (3.1: low thinking lost quality and added cuts on every path; ceilings and `DefaultThink` stay none). 4.4 review done.

Goal: let qwen3.5:4b (en chat) and gemma4:e4b-it-qat (en chat, listen, follow-up) think, with each model's own thinking controls, and a cap that has room for both the thinking and the reply. Turn it on only where the probes show it helps.

## Rulings

| id | ruling | edit site |
|---|---|---|
| T1 | Scope (2026-11-09): chat (`forkAIReq`) en only, gemma4 + qwen3.5:4b; listen (`listen.Model`) and follow-up decider gemma4 only. A model without Ollama `thinking` capability is never tested (apertus-estllm:8b, EstLLM-8B-1125 answer "does not support thinking"), so et chat stays none. Vision and Claude out. | — |
| T2 | Mechanism is cap-only over /v1: thinking on = `reasoning_effort` ≠ "none"; `max_tokens` = path reply cap + level budget (Ollama 0.40.14 ignores `max_completion_tokens`). No budget forcing, no native `/api/chat` in production. | `llm/thinking.go` |
| T3 | Levels are per model. Each model row lists its native levels with the exact value it expects. A boolean-only model exposes `none\|on` natively, so our `low\|medium\|high` are emulated: on + budget, marked `emulated`. The level name is sent as `reasoning_effort` either way (Ollama maps a recognized effort to `think=true` on boolean-only models). | `llm/thinking.go` |
| T4 | API: `llm.ThinkingFor(provider, model string, path Path, level Level) Thinking` (no ceiling; probes) and `Config.Thinking(provider, model, path)` (saved level or `DefaultThink`, ceiling-clamped; production), where `Thinking{Effort string; Budget int; Native bool; Sampling Sampling}`. `Thinking.Apply(r, replyCap)` sets `ReasoningEffort`, `MaxTokens = replyCap + Budget`, and the thinking-on sampling when level ≠ none. Level none gives effort "none", budget 0 and today's `SamplingFor` values, the same request as today. | `llm/thinking.go`, `sampling.go` |
| T5 | Budgets are measured per model × path, with thinking on and the cap effectively removed (T13). low/medium/high = p25/p50/p90 of thinking tokens, rounded up to ×16, with the reply cap added on top. If Stage 2.3 shows native levels really differ, each level's budget comes from its own run's percentile (low p25 of the low run, and so on). | `llm/thinking.go` table, §Budget table |
| T6 | Cut-off = `finish_reason` "length" OR empty reply after TrimSpace. An arm with cut rate > 5% is disqualified. | `probekit` |
| T7 | Chat bar: judge mean ≥ +5 pt (0–100) AND added p50 ≤ +1.5 s against the none arm in the same run. Default; Stage 3.4 measurement validates. | — |
| T8 | Listen bar: the arm still passes listenprobe's own `pass` (median ≤ `maxMedian` 2 s, false SPEAK ≤ max, correct ≥ `passCorrect`/20), AND has ≥ +2 correct on dev+holdout per model-language, AND no more false SPEAK, AND added p50 ≤ +1.0 s. Default; Stage 3.4 validates. | — |
| T9 | Follow-up bar: ≥ +1/10 correct on both runs AND added p50 ≤ +250 ms (keeps the decider inside P1's ~250 ms median and F1's ≤ 1000 ms rule). **Expected to fail**: at R6's decode rate, even a low-budget think adds far more than 250 ms. Default; Stage 3.1 validates. | — |
| T10 | Stopping rules, pre-registered. (a) Run arms in order low → medium → high per path × model. Cap-only means a larger budget can only lengthen thinking, so latency never drops as the budget grows. If low fails the latency bar, skip medium/high, but keep low's quality row. (b) If an arm fails only on cut %, still run the next level (more budget means fewer cuts). (c) Follow-up runs low on dev only, unless low clears T9 latency. (d) Holdout runs only arms that passed every bar on dev. If holdout reverses the dev result, the path stays off. (e) Stop each path's runs once its verdict is fixed. | — |
| T11 | Consequence to expect: p90 = high leaves ~10% of calls thinking past the budget. On the 80-token listen cap and 8-token follow-up cap there is little slack, so even high may fail T6. That fails the arm (path stays off); it does not reopen T5. | — |
| T12 | Sampling with thinking on uses each model card's thinking-mode values (§Model thinking table). /v1 sends temperature, top_p and presence_penalty. top_k and min_p can't go over /v1, but the Modelfile values already match both modes. That is an accepted gap, not a reason for a native-API call. | `llm/thinking.go` |
| T13 | Thinking tokens per call = `usage.completion_tokens` − reply tokens (R4: no `reasoning_tokens`). Reply tokens are counted exactly (R7): `probekit.CountReply` = prompt_tokens({user: reply}) − prompt_tokens({user: ""}), + 1 on finish "stop". "Uncapped" = per path × model `-think-budget` min(3000, 4096 − max prompt − 16) − reply cap, max prompt from the 2.1 runs. | `probekit` |
| T14 | Context guard: prompt_tokens + reply cap + budget ≤ runtime `num_ctx` 4096 (R5). If a budget breaks it, clamp the budget and mark the table cell `clamped`. | `llm/thinking.go` |
| T15 | Production knob: a level per path in `myvectorConfig.json` `"think": {"chat","listen","followup"}`. When absent, `llm.DefaultThink` applies (all `none` until Stage 4.1). `ThinkingFor` clamps each path's level to the model's per-path ceiling (the highest level that passed Stage 3, else none). | `llm/llm.go` Config, `llm/thinking.go` |
| T16 | Chat is scored in `routeprobe -set dev\|holdout`, not a new chatprobe. That reuses the real request path (`ForkProbeLLM` → `CreateAIReq` → `forkAIReq`) and `isolate()`'s temp configs. The single-sentence mode and its `llm:` output stay byte-identical (`live-check.sh route` checks this). The temp config sets search off: no Tavily quota, no search variance. | `routeprobe/` |

## Model thinking table

Rows are pre-filled from recon; Stage 0 re-verifies every cell.

| model | how to enable (/v1) | native levels → mapping | sampling: thinking on / off (sendable on /v1) | where thinking comes back | budgets low/med/high (tokens, per path) |
|---|---|---|---|---|---|
| qwen3.5:4b | `reasoning_effort` ≠ "none" → Ollama `think=true` → the template drops the empty `<think>\n\n</think>` prefill (prompt_tokens −2 vs none, R2) | **no**: `/api/show` `thinking.values [false,true]` (R1). low/medium/high **emulated** = on + budget | on T1.0 top_p 0.95 top_k 20 min_p 0 PP 1.5; off T0.7 top_p 0.8 top_k 20 PP 1.5 (T, top_p, PP sendable; top_k from Modelfile = 20) | `choices[0].message.reasoning` (go-openai reads `reasoning_content`, so it drops the text); `usage.completion_tokens` = thinking + reply | pending 2.2 |
| gemma4:e4b-it-qat | `reasoning_effort` ≠ "none" → Ollama writes `<\|think\|>` at the start of the system turn (prompt_tokens +2 vs none, R2) | **no**: `thinking.values [false,true]` (R1). **emulated** as above | one set for all uses: T1.0 top_p 0.95 top_k 64 (T, top_p sendable; top_k from Modelfile = 64); no change when thinking is on | same as qwen | pending 2.2 |

Sources:
- qwen: https://huggingface.co/Qwen/Qwen3.5-4B (on by default, no graded levels; on/off values are the card's "general tasks" sets).
- gemma4: https://huggingface.co/google/gemma-4-E4B-it (one sampling set, on/off boolean) and https://ai.google.dev/gemma/docs/core/prompt-formatting-gemma4 (`<|think|>` in the system turn, thoughts stripped from history).
- Ollama: https://docs.ollama.com/capabilities/thinking (`think` bool or a `thinking.values` level), https://docs.ollama.com/api/openai-compatibility (efforts → `true`, "none" → `false`; only `max_tokens`), https://github.com/ollama/ollama/blob/main/openai/openai.go (`reasoning` field).

## Recon (2026-11-08, n=1 each, local Ollama 0.40.14)

| id | finding | command |
|---|---|---|
| R1 | Both models: capabilities include `thinking`; `thinking.values [false,true]`, default true | `curl -s localhost:11434/api/show -d '{"model":"<m>"}'` |
| R2 | qwen: none 33 / high 31 prompt_tokens. gemma4: none 30 / low 32 / high 32. So the switch shows in prompt_tokens. | `/v1/chat/completions` "17×23", `max_tokens` 2048 |
| R3 | qwen high: 630 completion tokens, 1593 reasoning chars; low: 318 / 753; none: 17. gemma4 high: 138 / 328; low: 13 / 0 (switch on, but it chose not to think); none: 13 | same |
| R4 | Response message keys `role, content, reasoning`; usage has no `completion_tokens_details` | same |
| R5 | Runtime `context_length` 4096 for both (and for gemma4:e2b); three models resident at once | `curl -s localhost:11434/api/ps` |
| R7 | Exact reply count: et think-off reply, prompt-token delta + 1 = completion_tokens (qwen 103/103, gemma4 74/74), repeat identical; runes/token 2.83 / 3.03, not 4.0 | `/v1` reply → `{user: reply}`, `max_tokens` 1, minus `{user: ""}` |
| R8 | Seed 7, T 0, "Is 391 prime?": low vs high identical (qwen pt 20 ct 800, gemma4 pt 26 ct 342, same reasoning) — efforts are one request | `/v1` `seed`, `temperature` 0 |
| R9 | 2.1 think-off dev (b074c03), exact % and signed err: chat qwen en 100 (+0/+0), gemma4 en 100 (+0/+0, max prompt 292), gemma4 et 94 = 15/16 (+0/+1); listen and follow-up 100 on every prompt × model. Max prompt: chat 222, listen 290, follow-up 111 | `routeprobe -set dev -think none -judge none`; `listenprobe`/`followupprobe -think none` |
| R10 | Decode slows with context (2026-11-09): qwen `think` precheck 85 tok/s at short context; user saw 18 tok/s at a 1731-token context; uncapped qwen chat calls ran ~80 s for ~3000 tokens. R6 holds for short thinks only | Ollama verbose; 2.2 qwen log |
| R11 | 2.3 (2026-11-09): gemma4 low/medium/high byte-identical (reply + reasoning) on 5/5 listen prompts after each prompt's first call; that first think-on call may differ at any level (cache, not level); none ≠ high 5/5 → emulated stands | `robot/work/think/23/run2.py` |
| R6 | Decode tok/s (thinking calls, model alone): qwen p50 74 (n=9), gemma4 p50 69 (n=5); prompt eval ~0.1–0.2 s | `robot/work/think/zz_switch_test.go` (Stage 0.2/0.3) |

## Budget table

Filled by Stage 2.2 (2026-11-09, gemma4 at the T13 uncapped budgets 2700 / 2920 / 2992, 1 rep, dev; follow-up = prompt 1). No call hit the cap or timed out, so no row is censored. The code table in `llm/thinking.go` holds the values; this table holds what was measured. Listen and follow-up low = 0: p25 is 0 because 30–40% of calls don't think, so low leaves no room to think, and 3.1 should expect T6 cuts there.

| model × path | n | think-rate | T p25 / p50 / p90 / max | reply cap | low / medium / high budget | clamped? |
|---|---|---|---|---|---|---|
| qwen × chat (en) | 4 | | 3 of 4 hit cap (≥ 2700) | 300 (long 1000) | off (T6) | |
| gemma4 × chat (en) | 16 | 94% | 384 / 448 / 764 / 849 | 300 (long 1000) | 384 / 448 / 768 | no (max prompt 294) |
| gemma4 × listen | 20 | 70% | 0 / 214 / 380 / 428 | 80 | 0 / 224 / 384 | no (max prompt 204) |
| gemma4 × follow-up | 10 | 60% | 0 / 136 / 214 / 257 | 8 | 0 / 144 / 224 | no (max prompt 91) |

## Results table

Stage 3.1, 2026-11-09, dev, 1 rep; ms = the probekit arm line's p50/p90 (logs `robot/work/think/31/`).

| path × model | arm | set | quality (Δ vs none) | p50 / p90 ms (Δ p50) | cut % | verdict |
|---|---|---|---|---|---|---|
| chat × gemma4 (en) | none | dev | judge 74.8 | 475 / 903 | 0 | baseline |
| chat × gemma4 (en) | low~ | dev | judge 62.5 (−12.2) | 7380 / 33179 (+6905) | 12 | off: T6 cut, T7 quality + latency |
| listen × gemma4 | none | dev | 18/20 | 423 / 713 | 0 | baseline |
| listen × gemma4 | low~ | dev | 13/20 (−5) | 2735 / 3311 (+2312) | 65 | off: T6 cut, T8 correct + latency |
| follow-up × gemma4 | none | dev | 10/10 | 111 / 141 | 0 | baseline (prompt 1) |
| follow-up × gemma4 | low~ | dev | 8/10 (−2) | 117 / 203 (+6) | 30 | off: T6 cut, T9 correct |

## Out of scope

Vision / camera request (`forkCameraReq`), Claude thinking, et chat and any non-thinking model, qwen × listen/follow-up, native `/api/chat`, budget forcing, settings-page UI for `think`, raising Ollama `num_ctx`, editing `docs/model-prompt-formats.md`.

## Stages

**Refactor-first: needed.** Three probes (routeprobe, listenprobe, followupprobe) each need the same `-think` parsing, `ThinkingFor` apply, p50/p90, cut % and token stats. With three callers, a shared helper passes its own skip test, so it is built: new `pkg/myvector/probekit`. The Claude judge call has two users (searchbench + routeprobe), so it is extracted too (1.1).

Every new ttr test is named `TestFork*` so `make check` runs it. Ollama-touching runs are local and free; only judge calls spend Claude quota.

Probe runs read gitignored `secrets/server/`: in a worktree, `ln -s <main>/secrets/server secrets/server` for the run and `rm` it before `make check` (its secrets test fails on the link).

| id | ruling | edit site |
|---|---|---|
| 0.1 | Web re-verify every §Model thinking table cell. Write a source URL per row; replace the gemma4 card URL if it differs. Falsifier: a cell with no source URL fails the stage. If either model's thinking can't be switched on over /v1, raise **[USER DECISION]**: native `think` path vs keep off (`docs/backlog.md` §OWED[USER]). | this plan |
| 0.2 | Switch falsifier per model, 10 listen-dev prompts × {none, high}. On = prompt_tokens(high) ≠ prompt_tokens(none) on 10/10 AND `reasoning` non-empty on ≥ 5/10. The none arm (empty reasoning, R4 keys) shows the check can return "off". **Stop rule:** if gemma4 shows no prompt-token delta, or < 2/10 non-empty reasoning, drop all gemma4 arms, record it in the Results table, raise the 0.1 [USER DECISION], and continue with qwen. | `curl` loop in `robot/work/think/` (gitignored scratch) |
| 0.3 | R6: decode tok/s = completion_tokens ÷ (total − prompt-eval) on the 0.2 high calls. Run with wire-pod idle and only the target model loaded (`ollama stop` the rest). | this plan R6 |
| 1.1 | Extract `probekit.Claude(key, model, system, user string, maxTokens int) (string, error)` (Messages API, `"thinking":{"type":"disabled"}`) from searchbench `judge`; searchbench keeps its prompt and `parseScore`. Proof: `go test ./pkg/myvector/searchbench/` green (TestParseScore, TestPick) with no test edits. | new `pkg/myvector/probekit/claude.go`, `searchbench/main.go` |
| 1.2 | T3/T4/T12/T14/T15 skeleton: `Level` (none\|low\|medium\|high), model rows (Native=false for both, budgets 0 = unmeasured, thinking-on sampling), `DefaultThink` all none, Config `think` map, per-path ceilings all none. Wire `forkAIReq` + `forkReplyCap` and `listen.Ask` through `ThinkingFor(path)`, no behaviour change. Proof: `TestThinkingFor` table (qwen none → "none"/0/off sampling; qwen medium → "medium"/budget/1.0-0.95-1.5; gemma4 high → "high"/unchanged sampling; Claude any → zero; unknown → none; ceiling clamps medium → none), `TestThinkingApply` (MaxTokens = cap + budget), existing `fork_prompt_test.go` `ReasoningEffort` pins green unedited, `TestSamplingFor` green. | new `llm/thinking.go` + `_test.go`, `llm/llm.go`, `ttr/fork_prompt.go`, `listen/heard.go` |
| 1.3 | `probekit` flags + stats: `-think none,low,medium,high` (a list sweeps arms in one run, interleaved per prompt), `-think-budget N` (overrides the table budget; marks the arm `budget=N`), one discarded warm-up call per model. Per-arm line: n, p50/p90 ms, cut %, think-rate, T p25/p50/p90, max(prompt+completion). Emulated levels print `low~`. A level a model doesn't support natively runs only as an emulated arm. Wire into listenprobe and followupprobe; followupprobe gains `-models`. Proof: `TestStats` (p50/p90 on fixed slices, cut counts "length" and empty), `TestParseThink` (bad level → error); `-think none` gives the same verdict lines as a pre-change run (diff = falsifier). | new `probekit/think.go` + `_test.go`, `listenprobe/main.go`, `followupprobe/main.go` |
| 1.4 | T16 chat scorer: `ForkProbeLLMResult{Target, Answer, Finish, Usage, Dur}` behind `ForkProbeLLM` (which keeps its signature). In-file chat sets: dev 16 en + 16 et, holdout 12 en + 12 et, a mix of fact / chit-chat / how-to / long-ask, no search-dependent asks. Judge = `probekit.Claude` claude-haiku-5-5, rubric 0–100 (answers the ask, correct, 1–3 spoken sentences, language matches, no markdown), JSON `{"score"}`. `-think` sets `llm.ProbeThinking` per arm (a saved think.chat would be ceiling-clamped). Proof: `TestChatSets` (disjoint, sizes, both languages), `TestParseJudge` (fence/prose tolerant, out-of-range → error); `scripts/live-check.sh route` PASS (single-sentence contract unchanged). | `ttr/fork_probe.go`, new `routeprobe/chatset.go`, `routeprobe/judge.go` + `_test.go`, `routeprobe/main.go` |
| 1.5 | `make check` green, `scripts/upstream-drift.sh` green (no upstream file touched), then commit 1.1–1.4. | — |
| 2.1 | T13 exact count: `probekit.CountReply`, filled by `probekit.Chat` and routeprobe (`ForkProbeLLMResult.BaseURL`); `RunesPerToken` goes; the arm line adds `exact %` and `max prompt`; routeprobe `-judge none` skips the judge. Run `-think none` on each path's dev set per model × language. Run gemma4 × chat en too (`-lang en-US -brain ollama:gemma4:e4b-it-qat`). Falsifier: |err| ≤ 1 on ≥ 95% of calls of every think-off arm (`err` min/max inside ±1 suffices). Proof: `TestStats`, `TestCountReply`. | `probekit/think.go`, `ttr/fork_probe.go`, 3 probes |
| 2.2 | Precheck: one qwen `think` call ≥ 50 tok/s (`/api/chat` eval_count ÷ eval_duration), else stop (routeprobe's 2-min call deadline times out). Uncapped distributions (`-think high -think-budget` per T13) per path × model on dev sets: chat 16 en per model × 2 reps, listen all dev snippets × 2 reps, follow-up 10 pairs × 3 reps. Fill §Budget table and the `llm/thinking.go` budgets; apply the T14 clamp. If > 2% of calls hit the uncapped cap, the distribution is censored: mark p90 as a lower bound. Any call timeout (deadline exceeded / no answer) stops that path: precheck again, then re-run the whole path (user ruling 2026-11-09). Reps from separate runs pool through `-think-dump <file>` (one JSON line per call, `probekit.Dump`); listen runs `-arm "prompt 2"`, follow-up counts prompt 1 only. **Cut (user ruling 2026-11-09):** qwen × chat dropped — off by T6 (3 of its first 4 uncapped calls hit the 2700 cap, so any budget ≤ 2700 cuts ≥ 3/32 = 9% > 5%); gemma4 runs at the plan budgets, 1 rep: chat 16, listen 20, follow-up 1 run (R10). Proof: `TestThinkingFor` rows updated to the measured budgets. Commit. | `llm/thinking.go`, `_test.go`, this plan |
| 2.3 | Seeded identity check (R8) per model: 5 listen dev prompts × {none, low, medium, high}, seed 7, T 0, uncapped. Low/medium/high byte-identical (reply + reasoning) ⇒ rows stay emulated; any differ ⇒ `Native=true` + T5 per-level budgets. Control: none ≠ high on every prompt. | `robot/work/think/`, this plan |
| 3.1 | Dev, none vs low only, 1 rep, quality Δ + latency per path (user ruling 2026-11-09): `routeprobe -set dev -lang en-US -brain ollama:gemma4:e4b-it-qat -think none,low` (haiku judge); `listenprobe -set dev -models gemma4:e4b-it-qat -arm "prompt 2" -think none,low`; `followupprobe -models gemma4:e4b-it-qat -think none,low`. Listen/follow-up low budget is 0: expect T6 cuts. | this plan |
| 3.2 | Cut (user ruling 2026-11-09). | — |
| 3.3 | Cut (user ruling 2026-11-09). | — |
| 3.4 | Verdict row per path × model under T6–T9: the adopted level, or `off` with the failing bar named. Write the per-path ceilings into `llm/thinking.go` (still `DefaultThink` none). Commit. | this plan, `llm/thinking.go` |
| 4.1 | Apply: set `DefaultThink` per path to the lowest passing level shared by that path's models (a model without a pass is clamped to none by its ceiling). If no path passed, leave `DefaultThink` none and set Status "verdict: keep off". If the follow-up decider has landed, route it through `ThinkingFor(followup)`; if not, add one row to the follow-up-listening plan saying its decider call goes through `ThinkingFor`. Before turning chat on, check `ttr/kgsim.go` + `fork_*.go` stream loops for a first-content deadline shorter than T p90 ÷ R6 tok/s (`grep -n 'time.After\|Timeout' pkg/wirepod/ttr/kgsim.go pkg/wirepod/ttr/fork_*.go`). Proof: `TestForkAIReqThink` (config absent → `DefaultThink`; config high on a model with ceiling none → none; MaxTokens = cap + budget). | `llm/thinking.go`, `ttr/fork_*_test.go` |
| 4.2 | `make check` green; `scripts/upstream-drift.sh` green. | — |
| 4.3 | Live e2e: `make daemon-enable` restart, then `scripts/live-check.sh route` PASS. If chat changed, USER asks one en + one et question (volume stays 1, ask first). Falsifier: `journalctl -u wire-pod --since -5min \| grep 'myvector'` shows a non-empty spoken reply, and no reply-cap line with an empty answer. | — |
| 4.4 | `code-reviewer` subagent over the 1.1–4.1 commit range. Then commit, and archive this plan at ship. | — |

**Probe cost:**
- Claude judge: 32 haiku calls (3.1 chat, thinking disabled).
- Ollama runs are local: 3.1 is 32 chat + 40 listen + 20 follow-up calls.
- Searchbench and Tavily quota: none (T16 sets search off).
