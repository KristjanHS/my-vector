# Search news quality — a news ask returns dated, on-topic headlines and the robot uses them

**Goal**: "find news about X that affects stocks" (et or en, including a follow-up like "search what you just said")
gives the brain 2–3 dated, on-topic news hits. The robot answers from them and never says it can't search.
**Status**: ✅ SHIPPED (code; live check → `docs/backlog.md` §OWED[USER] "Search news live"). Q1–Q5 ruled; slices 1 (N1+N7+N3+Q4), 2 (N5) and 3 (N2 = search-trigger-fixes F5, its R6–R8) committed; N12 committed; N11 committed (news-ask prefill); N13–N16/Q10 replays run and ruled; Q8 A and N6 committed; N4 closed by Q15; N9 ruled (Q16) and committed; N8 + N10 → `docs/backlog.md` §Assistant-owed. Builds on `2026-11-11-search-trigger-fixes.md (docs/plans/)` (F5 lives there).

## Incident 2026-11-11 09:31–09:32 (et-EE mode, brain `apertus-estllm:8b`, provider Tavily)

| turn | heard (STT) | query sent | what came back | reply |
|---|---|---|---|---|
| 1 | "otsin semikontaktorstide teemalisi finantsuudiseid, mis mõjutavad aktsiaturve." ("semikontaktorstide" is misheard *semikonduktorite / pooljuhtide*) | the sentence verbatim | VanEck ETF page (2022 figures), an Estonian "what raises share prices" explainer, an MIS Solutions email-format page | Opened with "Väga hea küsimus!" (the persona bans this), then a generic list ("new orders, capacity, geopolitics…"). Hit the 300-token cap. No news. |
| 2 | "otsi uudiseid seoses sellega, mis sa just rääkisid." | the sentence verbatim | "Räägime asjast" YouTube, Wikiquote "Uudis", ERR homepage | "Vabandust, ma ei saa sind aidata uudiste otsimisega", then "try Bloomberg or Reuters". `voice fallback: tts 400 … non-empty string required` |

Both turns: `search live tavily ok` (2.2 s and 2.3 s), so routing worked. The failure is in what was searched and in how the brain used the results.

## Root causes

| id | cause | evidence |
|---|---|---|
| C1 | **The query is the sentence verbatim** (web-search W4). Command words ("otsin", "otsi"), filler ("mis", "teemalisi") and the STT error all go to the engine. | The "mis" hit is MIS Solutions. Measured below: a keyword query fixes it on both providers. |
| C2 | **No news mode or time window.** Tavily gets `topic` general with no `time_range`, and SearXNG gets no `categories`/`time_range`, so evergreen pages beat this week's news. General Tavily hits also come without dates. | A2 vs A1 below |
| C3 | **A follow-up has no topic.** Turn 2 refers back ("what you just said"), but the search sees only the last user message. | = search-trigger-fixes **F5** |
| C4 | **The brain gets results without instructions.** `search.render` prefixes an English header ("Live web results for the user's question:") to an Estonian-only model (ollama-et-prompt P14: the et model needs Estonian instructions), with no rule to answer from them, cite them, or admit they're off-topic. Off-topic snippets plus the history's "I can't…" pattern led to the turn-2 denial. | turn 2 reply, `search.render` |
| C5 | **No relevance gate.** Results sharing no content word with the ask still fill `{search}`, so the brain gets junk with no way to tell it's junk. | turn 2 (ERR homepage, Wikiquote) |
| C6 | **The misheard word is never fixed before the search.** The `{speech}` line tells the brain STT may mishear, but the search runs before the brain sees anything. | turn 1 |
| C7 | Side bug: the voice fallback sent an empty string to TTS in turn 2 (cause unknown, not investigated). | `myvector voice fallback: tts 400` |

## Measurements (2026-11-11, 3 Tavily calls, SearXNG local)

**Tavily** (`max_results` 3):

| arm | query | params | result |
|---|---|---|---|
| A0 | verbatim (turn 1) | general (today's config) | junk (see incident) |
| A1 | verbatim | `topic:news, time_range:week` | 1 hit: a Swedish ChartCompass glossary page. 3.3 s. **News mode alone doesn't help.** |
| A2 | `pooljuhtide aktsiad uudised` | news, week | 3 on-topic Estonian items, dated 7–9 Nov (tradersunion.com: Asia rotating out of chips, Taiwan short-sale limit, Wall Street recovery). 1.6 s |
| A3 | `semiconductor stocks news` | news, week, `include_answer:basic` | 3 on-topic dated hits (Bloomberg, Yahoo Finance: SOX −9.2 % on Thursday, Goldman calls chips "oversold") **plus a 4-sentence `answer` summary**. 1.7 s |

**SearXNG** (`127.0.0.1:8888`, free):

| arm | query | params | result |
|---|---|---|---|
| S0 | verbatim | `language=et` (today's config) | finantsuudised.ee homepage, an EMTA PDF, a school curriculum |
| S1 | verbatim | et + `categories=news, time_range=week` | 0 results, 2 engines unresponsive |
| S2 | `pooljuhtide aktsiad uudised` | et + news + week | 1 off-topic Äripäev hit (only Bing News answers) |
| S3 | `semiconductor stocks news` | et or en + news + week | 9–10 on-topic hits (Yahoo, AP, Economic Times), all from Bing News, no dates in JSON |

**Query rewrite on the loaded brain** (VRAM is 7.6 / 8 GB with apertus loaded, so a second local model won't fit):

| arm | method | result |
|---|---|---|
| R0 | apertus, chat mode, system instruction | 2/2 off task ("Tere! Siin on…", "Vabandust, ma ei saa aru…"). 0.6–0.9 s |
| R1 | apertus, chat-templated few-shot | 3/3 echoed the first example. 0.4–0.7 s |
| R2 | apertus, `raw:true` few-shot completion, `stop:"\n"` | `semikontaktorstide finantsuudised`: keywords extracted, STT error kept. 0.6 s |

## Tavily vs SearXNG: do they need different queries?

**Not different query text, but different parameters and different expectations.** Both do best with a short keyword query
(A2/A3, S3), and both do badly with the spoken sentence (A0/A1, S0/S1). So build one query and adapt only the
parameters:

| | Tavily | SearXNG |
|---|---|---|
| what it is | AI-oriented search API with its own index and ranking. Takes natural language, but ranks poorly on long conversational sentences (A1). | Metasearch: passes the string verbatim to Bing, DDG, Brave and others, so exact keywords matter more |
| news mode | `topic:"news"` + `time_range` (`day`/`week`), returns `published_date` | `categories=news` + `time_range`. Here only Bing News answers, with no dates |
| language | no language param. Estonian keywords work (A2), English gets broader coverage (A3) | `language=` must match the query's language. For news, English is the only usable path here (S2 vs S3) |
| extra | `include_answer:"basic"`: a ready-made dated summary, ideal for an 8B brain (A3) | none. Quality depends on which engines are up (1–2 unresponsive in every news run) |
| Estonian news | usable (A2) | thin (S2) |

## Proposed fixes

| id | fix | edit site | falsifier |
|---|---|---|---|
| N1 | **Query builder** `search.Query(sentence, l) string`: lowercase, drop command cues (`otsi`, `otsin`, `otsige`, `guugelda`, `search`, `google`, `look up`…), self/ask frames (`kas sa saad`, `palun`, `can you`), deictic and stop words, and filler like `teemalisi`/`seoses`/`related`. Keep content words in order, cap at 6. Deterministic, adds 0 ms. Providers get `Query(...)`, and the cache key stays the sentence. | new `search/query.go`, `search.Fill` | `TestQuery`: turn 1 → contains `finantsuudis`, `aktsiatur` and no `otsin`/`mis`. Turn 2 → only `uudis` left, so N2 must fire |
| N2 | **Follow-up topic** = search-trigger-fixes F5, rules R6–R8 there (fewer than 2 content words or a pointer word → the last search ask's topic) | `search.followQuery`, `forkPrevAsks` | `go test ./pkg/myvector/search/ -run 'TestFollowQuery\|TestFillFollowUp'` |
| N3 | **News mode**: a `News` flag on the query when a news/markets cue fires (`uudis*`, `news`, `headlines`, `börs*`, `aktsia*`, `stock*`, `market*`). Tavily gets `topic:"news", time_range:"week"`, SearXNG gets `categories=news&time_range=week`. Weather and opening hours stay general. | `Provider.Search` signature (query struct instead of string), `search/provider.go` | provider tests: the request body/URL carries the params only when `News`. Replay A2 ≥ 2 on-topic dated hits |
| N4 | **Tavily answer**: `include_answer:"basic"` in news mode, rendered as the block's first line ("Kokkuvõte: …") ahead of the snippets. Drop snippets before the answer when over `SearchBudget`. | `search/provider.go`, `search.Block` | `TestBlockAnswer`: the answer survives the budget and the last snippet is dropped first. Latency ≤ 2.0 s median on 5 news queries (inside the 2.5 s `Timeout`) |
| N5 | **Estonian block wrapper + usage rule** for `OllamaEt`: header "Värsked veebitulemused (kuupäev, allikas):" and the rule "Vasta nende põhjal 2–3 lausega, nimeta allikas ja kuupäev. Kui need ei vasta küsimusele, ütle, et ei leidnud sobivat vastust. Ära ütle, et sa ei saa otsida." The Ollama (en) brain gets it in English; Claude's block is unchanged (Ollama-only prompt fixes). Render `Date` short (et `10.11`, en `Nov 10`). | `search/block.go` (`render` takes brain) or a `prompt.SearchWrap(brain)` | routeprobe replay of both turns, 3 reps: 0/3 denials, ≥ 2/3 name a source. Run `TestSearchBudget` again |
| N6 | **Relevance gate**: results sharing no stem with the query's topic words are dropped (match rule Q13, per result Q14); an emptied set counts as "no results" (→ next provider, then `Unavailable`) | `search.look`, `search.Relevant` | `TestFillRelevance`: turn 2's cached ERR/Wikiquote set + query `finantsuudis` → next provider. A2's set passes |
| N7 | **English query via word list** (Q1, Q3): `search.English(q)` maps et stems to en through a fixed lexicon (`aktsia*` → stocks, `pooljuht*`/`semikond*`/`semikont*` → semiconductor, `börs*` → stock market, `uudis*` → news, …) and passes other words through. SearXNG gets `English(q)` on news asks (Q3); Tavily always gets the et keywords (Q4). | `search/query.go`, `search.Fill` | `TestEnglish`: turn 1's query → contains `semiconductor`, `stocks`, `news`. N9 replay: ≥ 4/5 news asks get on-topic hits |
| N8 | **STT domain hints** (backlog): faster-whisper `hotwords` / `initial_prompt` with market and tech terms. This touches every turn's transcription, so it needs its own STT bench (FLEURS + command set, et-listen-model method) before shipping. | `et-stt/server.py` | WER on FLEURS et_ee doesn't worsen; "semikonduktor" heard correctly in a recorded ask |
| N9 | **Bench**: extend `searchbench` with 5 news/markets asks (verbatim spoken form) and the arms verbatim / N1 / N1+N3 / N1+N3+N7. Judge relevance 0–3 plus "dated this week" 0–1. Small run (~5 asks × 4 arms × 2 providers = 40 calls). | `searchbench/main.go` | `TestParseScore` extended. Decision rule per arm: mean relevance gain ≥ 0.5 over verbatim |
| N10 | **Side bug C7**: find why the voice fallback got empty text (likely a cleaned-away sentence). `systematic-debugging` first. | `pkg/myvector/voice` | a unit test reproducing the empty sentence → no TTS call |
| N12 | **Before `server-pin` of the weather template** (review of 070d744): skip it when the ask names another city (fork_weather's location parse ≠ forecast place); pick the day by matching today's date in `Daily.Time` (midnight memo); `WeatherReply` memo-only + Weather skips the persisted block cache; pair the canned reply with its ask; a `forkAIReq`-level wiring test + a kgsim.go source check for `forkSignalIntent(successIntent)` (fork_wait_test's `forkStreamFailed` pattern); "vihma või lund" → `isFallWord`, PrecipP < 50 → "võib" | `search.WeatherReply` / `speakWeather`, `forkCannedSet`, `forkLLMStream` | `go test ./pkg/myvector/search/ ./pkg/wirepod/ttr/ -run 'Weather\|Canned'` (from `chipper/`) with cases for each |
| N11 | **Prio 1 — web results ignored**: `apertus-estllm:8b`'s first-words refusal prior (~3/8). Weather ruled = template (`search.WeatherReply`, shipped). Web: try an assistant prefill opener ("<allikas> andmetel", native `/api/chat`), then detect-and-resample | ttr LLM stream (`forkLLMStream`), `prompt.SearchWrap` | `python3 -I robot/work/search-trigger/r5_replay.py chipper/pkg/myvector/prompt/testdata/ollama-et_et-EE.golden`: results arm "ei saa … otsi" ≤1/8 |
| N13 | **Try an opener for weather too** (user idea, 2026-11-11). The weather ask was ruled = template (`search.WeatherReply`) after no *prompt* variable moved apertus's ~3/8 refusals (`robot/work/search-trigger/n11/replay.py`). A prefill was never tried on weather, and on news a sentence-shaped opener ("Värskete uudiste järgi", Q9) took refusals to 0/8. Arms, 8 runs each on n11's 2 weather asks with the live Open-Meteo block: base / "Ilmateate järgi" / "Värske ilmateate järgi". Score refusals, wrong numbers or wrong day against the block, lists, and naturalness. **Rule**: the LLM path with the opener replaces the template only at 0/8 refusals AND 0/8 wrong numbers or days (the template is exact by construction). Otherwise the opener goes only to asks the template skips (wind, weekdays, dates, another town → `replyDay`/`namesOtherPlace` false), where the LLM answers anyway. | `scripts/n11-prefill-replay.py` (weather arms), `forkCannedSet` / the N11 prefill site | replay table in this plan; if it ships: the same `forkAIReq` wiring test as N11 |

## Rulings (USER, 2026-11-11)

| id | ruling | edit site |
|---|---|---|
| Q1 | English queries come from a small built-in et→en word list (N7 arm c); no Claude Haiku, no apertus translation | `search/query.go` |
| Q2 | news window = `week` for both providers | `search/provider.go` |
| Q3 | SearXNG gets `English(q)` + `language=en` for **news asks only**; a non-news et ask goes as et keywords + `language=et` (4 local asks: et ≥ en) | `search/provider.go`, `search.look` |
| Q5 | Ollama `SearchBudget` 300 → 340: the N5 rule counts inside the block and 3 snippets still fit | `prompt.SearchBudget` |
| Q4 | News mode: **Tavily (keywords in the robot's language) and SearXNG (English; `English(q)` only in et-EE) race together** under the one `Timeout`; the block **merges both** (interleaved, deduped by URL, Tavily first); whichever is missing at the deadline is skipped. Non-news Live/Recent keep the provider chain with the N1 query. | `search.Fill` |
| Q6 | N11 = prefill first; detect-and-resample only if the winning opener still refuses >1/8 in the replay | `forkLLMStream`, `forkAIReq` |
| Q7 | N11 opener: replay both "<first result's source> andmetel" and "Otsingu järgi" (8 runs each, local Ollama); lower refusal count wins, tie → named source. Prefill = trailing assistant message on the existing go-openai `/v1` client (probed 2026-11-11: Ollama 0.40.14 continues it on `/v1` and `/api/chat`; stream deltas omit the prefill, so the fork prepends it to the spoken text). OllamaEt only | replay script, `search.Fill`, ttr |
| Q8 | N12 "another city" = a known town/country/region stem ≠ the forecast place stem → skip the template (LLM path); no capital-letter half (STT is lower-case) | `search.WeatherReply` |
| Q10 | N11 prefill "Värskete uudiste järgi" on OllamaEt **news** asks with a web block; non-news web asks get "Otsingu järgi" only if an n11 replay (2 non-news asks, base vs generic, 8 runs each) gives ≤ 1/8 strict refusals | `forkAIReq`, `forkLLMStream`, `scripts/n11-prefill-replay.py` |
| Q11 | Source naming dropped from the Goal (no credit beats wrong credit); the N5 wrap stays unchanged | — |
| Q12 | N14 after N11: Ollama snippets cut back to their last full sentence when they end in "…"; keep only if the sentgen replay's made-up count drops below 2/8 | `search.Block` |
| Q13 | N6 match: news/time/ask words (`cueWord`) never count, and a query of only those keeps every result; a ≥ `matchRunes` word's stem may sit inside a result word or its first `matchRunes`−1 start one, a shorter one (≥ `minMatchRunes`) must start one; et asks also match `English(q)` | `search.Relevant`, `search.look` |
| Q14 | N6 drops single off-topic results from a passing set too, before `Block` trims to the snippet count | `search.look` |
| Q15 | N4 ships only at no-worse replay AND ≤ 0.3 s paired added median latency (5 fresh asks); measured +0.34 s → N4 closed, no code | — |
| Q16 | N9: news asks race under `NewsTimeout` 5 s; an N6-emptied SearXNG news set retries once as the N1 keywords without news mode (`-arms verbatim,N1+N3+N7,N1+N3+N7+fb`: 0/5 empty, 2.40 vs 1.80) | `search.Fill`, `search.look`, `searchbench` |

### N11 opener replay (2026-11-11, `python3 -I scripts/n11-prefill-replay.py 4`)

apertus-estllm:8b, `/v1/chat/completions` 0.4/0.9/300, the captured live et prompt (`robot/work/search-trigger/n11/sys.txt`) with a real `search.Block(OllamaEt, …)` news block (3 dated, sourced results); 2 news asks (incident "semi" + "otsi uudiseid Nvidia aktsia kohta") × 4 reps per arm; replies in `robot/work/search-trigger/n11/prefill.jsonl`.

| arm | strict refusals | broad refusals | names a source | markdown list |
|---|---|---|---|---|
| base | 2/8 | 2/8 | 6/8 | 7/8 |
| source "<first host> andmetel" | 0/8 | 0/8 | 8/8 | 0/8 |
| generic "Otsingu järgi" | 0/8 | 1/8 (false positive: "puudutavad") | 5/8 | 6/8 |

**Verdict (Q7/Q6)**: source wins (0 vs 0 strict, tie → source; 0 vs 1 broad), ≤ 1/8 → no resample. Source string = first result's host minus `www.` and TLD, ≤ 3 letters upper-cased, else capitalised (`err.ee` → "ERR", `reuters.com` → "Reuters"). Known cost: on the multi-source "semi" ask 4/4 replies put the TSMC (Reuters) and Äripäev items under "ERR andmetel", and one invented "Nasdaq langes 1,7%" from a cut-off title. Next: implement — `search.Fill` hands back the first source, forkAIReq appends the trailing assistant message (OllamaEt, web block only), and the spoken text is prefill + stream.

**Q9 (USER, 2026-11-11)**: the source arm's wrong credit on multi-source answers is not acceptable — re-measure two arms before building: all sources ("ERRi, Reutersi ja Äripäeva andmetel") vs a sentence-shaped generic ("Värskete uudiste järgi"), 8 runs each. Rule: of the arms with ≤ 1/8 strict refusals, fewest (wrong credit + made-up facts) wins, then fewest lists, full tie → all sources.

**Q9 replay** (`python3 -I scripts/n11-prefill-replay.py 4 allsrc,sentgen`, same block and sampling; per arm: replies with ≥ 1 flag /8, wrong credit and made-up facts hand-judged against the block):

| arm | strict | lists | wrong credit | made-up | wc+mf | names source |
|---|---|---|---|---|---|---|
| source | 0 | 0 | 5 | 2 | 7 | 8 |
| generic "Otsingu järgi" | 0 | 6 | 1 | 2 | 3 | 5 |
| allsrc "ERRi, Reutersi ja Äripäeva andmetel" | 0 | 1 | 1 | 3 | 4 | 8 |
| sentgen "Värskete uudiste järgi" | 0 | 0 | 0 | 2 | 2 | 0 |

**Verdict (Q9 rule)**: sentgen. Fragile: one hand call ("Nvidia warned demand would slow") flips it to generic (3 vs 4), and sentgen never names a source (the wrap asks for one). A host → genitive rule gets 2/4 hosts wrong (aripaev.ee → "Aripaevi", cnbc.com → "CNBCi"), which counts against allsrc in production. Every arm completes numbers from cut-off "…" snippet tails: trimming the tail is a candidate fix whichever opener ships. Build: prefill "Värskete uudiste järgi" (OllamaEt, web block only), with no `Fill` signature change.

### Replays after N11 (2026-11-11, `scripts/n11-prefill-replay.py <reps> <arms> {web|trim|weather}`) — ruled (USER, 2026-11-11)

| id | measured | ruling |
|---|---|---|
| Q10′ | non-news web (Rimi hours, PM), 8 each: base 0/8 strict, generic 0/8 | no opener on non-news asks (held back) |
| N13 | weather asks + live block, 8 each: refusals broad base 4/8, wx 0/8, wxfresh 0/8; wrong facts (snow vs drizzle, "kraadi külma") base 1/8, wx 4/8, wxfresh 5/8 | template stays; no weather opener. → N15 |
| N14 | sentgen "semi", 8 each, one blind judge: made-up untrimmed 8/8, snippet trim 8/8, snippet+title trim 5/8 (Nasdaq claim gone; "Nvidia warned demand will fall" stays) | no trim ships. → N16 |
| N15 | blind judge, refusals / wrong: etsent 0/8, 2/8; etdemo 0/8, 1/8; etshort (temp 0) 0/8, 0/8 on 2 distinct replies, then `weather-held` 8 new asks 0/8, 3/8 (+ 2/8 cut off) | template stays; no weather LLM change ships |
| N16 | blind judge, refusals / made-up on `semi`: facts 0/8, 2/8; xq 1/8, 7/8; shot0 0/8, 8/8 | no arm clears < 2/8; nothing ships |
| Q8 | lower-case STT (`preqs/fork_stt.go` `strings.ToLower`); held-out 32 asks: A (wider list) 0/20 needless skips, 3/12 wrong-town keeps; B (place-case ending + stop list) and C (A or B) 9/20 needless skips | A: `etTowns` + countries, regions, more towns; drop the capital-letter check (`namesOtherPlace`) |
| N9 | `searchbench -news`, 5 asks, gain vs verbatim: Tavily N1+N3 +0.80 at fresh-run median 4751 ms (> `search.Timeout`); SearXNG N1+N3+N7 +0.00 after the N6 gate, 2/5 empty | Q16 |

## Order (thin slice first)

1. **N1 + N7 + N3 + Q4**: keyword query, English word list, news mode, news race + merge. This turns A0/S0 into A2/S3 for turn 1.
2. **N5**: Estonian wrapper and usage rule, so the brain actually uses the hits and stops denying.
3. **N2 / F5**: follow-up topic, fixing turn 2.
4. **N11 (prio 1)**: the brain denies searching despite results; before every item below. Shipped in code (news asks).
5. **N6**: relevance gate (per provider, before the merge). Shipped in code (`search.Relevant` in `look`).
6. **N4**: closed by Q15.
7. **N9**: ruled Q16. Shipped in code (`NewsTimeout`, SearXNG keyword fallback in `look`).
8. Backlog: N8, N10.

Run `make check` per slice. Re-run the incident's two turns as a routeprobe replay after slices 1–3. Ship on the user's ask.
