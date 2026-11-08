# Estonian small-LLM research (Nov 2026)

**Status**: ✅ DONE — research only, no code/config changes (user, 2026-11-08); switching the et model is a user decision (`docs/backlog.md` §OWED[USER]).

**Ask**: `gemma4:e4b-it-qat` and `qwen3.5:4b` neither understand nor speak proper Estonian (user, by ear).
Find which small LLMs are best at Estonian comprehension + generation as of November 2026, smoke-test the top 5
locally, recommend one pick for the et-EE chat model.

## Contract (questions round 2026-11-08)

| # | Rule |
|---|---|
| C1 | Output = this doc: ranked shortlist with evidence + local smoke replies + one recommended pick. No code, config or default-model change. |
| C2 | Size: up to ~9B params at Q4 (QAT / Q4_K_M / IQ4); slightly over 8 GB VRAM (beside STT+TTS) is OK only if Estonian quality is clearly better (user note). gemma4:e4b-it-qat = 6.1 GB on disk; EstLLM-8B Q4_K_M ran at stack peak 7753/8192 MiB. |
| C3 | Smoke the **top 5** (user note, not 2–3), plus `gemma4:e4b-it-qat` as the known-bad reference. |
| C4 | Smoke harness (from `chipper/`): `go run ./pkg/myvector/routeprobe -llm -lang et-EE -brain ollama:<tag> -set dev -judge none` (+ per-ask replies saved); Ollama 0.40.14 on Windows at `localhost:11434`, pull via `curl localhost:11434/api/pull`. |
| C5 | Out of scope: cloud APIs, fine-tuning, switching the shipped et model (a later plan does that from §Recommendation). |

Prior art: `archive/2026-11-05-estonian-language-mode.md` (EstLLM-8B pick, E11), `archive/2026-11-07-et-listen-model.md`
(no et listen winner; EuroLLM-9B, EstLLM-8B, gemma3/4, qwen3.5 tried), `docs/backlog.md` (EstLLM deleted 2026-11-08).

## Stage 1 — web research → ranked shortlist

Keys: **TN** = TartuNLP generative evals (Apertus-EstLLM-0326 card, 2026-04) — Grammar-et / IFEval-et / wmt24pp en→et BLEU;
**EE** = EuroEval Estonian generative (data to 2026-09-23, fetched 2026-11-08) — rank score (lower = better) / grammar_et; **Arena** = EstLLM paper Elo (2026-03);
**EKI-NB** = Estonian Native LLM Benchmark mean (TalTech, 2026-02). EKI moodupuu has no open model <27B.

| rank | model | params / quant | GGUF | Ollama tag (verified) | Estonian evidence | notes |
|---|---|---|---|---|---|---|
| 1 | Llama-3.1-EstLLM-8B-Instruct-1125 | 8B / i1-Q4_K_M | 4.92 GB | `hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M` | TN 0.831 / 0.614 / 0.264 (best ≤9B grammar+IFEval); EE grammar 35.0 (best ≤10B; unranked, no err_news); Arena 1384 = #1 open ≤12B | = paper's "CV" pick; previous ship (7753 MiB peak); IQ4_XS 4.45 GB to save VRAM; Llama-3 template |
| 2 | Apertus-EstLLM-8B-Instruct-0326 | 8B / i1-Q4_K_M | 5.06 GB | `hf.co/mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF:i1-Q4_K_M` | TN 0.713 / 0.561 / **0.268** (best en→et) | newest EstLLM (2026-03); Apertus arch (xIELU) — Ollama load unverified; Apache-2.0 |
| 3 | EuroLLM-9B-Instruct-2512 | 9.2B / Q4_K_M | 5.58 GB | `hf.co/mradermacher/EuroLLM-9B-Instruct-2512-GGUF:Q4_K_M` | EE 2.34 / 24.0; TN 0.644 / 0.557 / 0.257 | newer EuroLLM (2026-01); ChatML; card wants a system prompt; 4k ctx |
| 4 | EuroLLM-9B-Instruct (2412) | 9.2B / Q4_K_M | 5.58 GB | `hf.co/bartowski/EuroLLM-9B-Instruct-GGUF:Q4_K_M` | TN 0.764 / 0.540 / 0.260; EE grammar 22.4; EKI-NB 0.409 (#2 after EstLLM 0.454); Arena 1328 | previously tried; beats 2512 on grammar MC |
| 5 | gemma-3-12b-it | 12B / QAT q4_0 | 8.9 GB (vision incl.) | `gemma3:12b-it-qat` | TN 0.789 / **0.756** / –; EE **2.10** / 39.1; Arena 1317; EKI-NB 0.354 | **over-budget**: text-only `hf.co/bartowski/google_gemma-3-12b-it-GGUF:IQ4_XS` 6.55 GB |
| 6 | Bielik-Minitron-7B-v3.0-Instruct | 7.5B / Q4_K_M | 4.50 GB | `hf.co/speakleash/Bielik-Minitron-7B-v3.0-Instruct-GGUF:Q4_K_M` | EE **2.22** / 20.3 (best-ranked ≤10B) | Polish-focused wildcard; no Estonian-specific bench; base repo gated, GGUF not |
| 7 | gemma-4-12B-it | 12B / QAT q4_0 | 7.2 GB | `gemma4:12b-it-qat` | EE 2.21 / 29.2, trivia_et 0.5 | **over-budget**; thinking model — set `think:false` |
| 8 | gemma-3n-E4B-it | 7.8B (E4B) / Q4_K_M | 4.54 GB | `hf.co/unsloth/gemma-3n-E4B-it-GGUF:Q4_K_M` | EE 2.43 / 22.2 | official `gemma3n:e4b` is now 9.8 GB — use unsloth text GGUF |
| 9 | Qwen3.5-9B | 9.7B / Q4_K_M | 6.6 GB | `qwen3.5:9b` | EE 2.51 / 28.4 (but NER 21.7, MWQA 43.7) | thinking on by default — `think:false`; 4B sibling EE 3.03 |
| 10 | Ministral-3-8B-Instruct-2512 | 8.9B / Q4_K_M | 6.0 GB | `ministral-3:8b` | EE 2.54 / 23.2; TN 0.562 / 0.489 / – | weak Estonian grammar; strong English |
| 11 | tiny-aya-global | 3.35B / Q4_K_M | 2.14 GB | `hf.co/CohereLabs/tiny-aya-global-GGUF:Q4_K_M` | TN 0.563 / 0.669 / –; EE grammar_et 0.0 | tiny + fast; grammar weak |
| ref | gemma4:e4b-it-qat · gemma3:4b-it-qat · qwen3.5:4b | 4–8B | 6.1 / 4.0 / 3.3–4.0 GB | (as named) | EE 2.64 / 14.5 · 2.79 / 18.2 · 3.03 / 19.1 | confirms user's by-ear verdict |

Smoke top 5: `hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M`, `hf.co/mradermacher/Apertus-EstLLM-8B-Instruct-0326-i1-GGUF:i1-Q4_K_M`, `hf.co/mradermacher/EuroLLM-9B-Instruct-2512-GGUF:Q4_K_M`, `hf.co/bartowski/EuroLLM-9B-Instruct-GGUF:Q4_K_M`, `gemma3:12b-it-qat` (over-budget)

Sources:
- https://huggingface.co/tartuNLP/Apertus-EstLLM-8B-Instruct-0326 (TN tables)
- https://huggingface.co/tartuNLP/Llama-3.1-EstLLM-8B-Instruct-1125
- https://arxiv.org/abs/2603.02041 (EstLLM paper, arena)
- https://arxiv.org/abs/2510.21193 (Estonian Native LLM Benchmark)
- https://euroeval.com/leaderboards/Monolingual/estonian/
- https://moodupuu.eki.ee/ · https://github.com/keeleinstituut/leaderboard-data-ui
- https://huggingface.co/utter-project/EuroLLM-9B-Instruct-2512 · https://huggingface.co/utter-project/EuroLLM1.5-9B-SFT
- https://huggingface.co/speakleash/Bielik-Minitron-7B-v3.0-Instruct-GGUF · https://huggingface.co/CohereLabs/tiny-aya-global-GGUF
- https://ollama.com/library/gemma3/tags · https://ollama.com/library/gemma4/tags · https://ollama.com/library/gemma3n/tags · https://ollama.com/library/qwen3.5/tags · https://ollama.com/library/ministral-3/tags

## Stage 2 — local smoke (top 5 + gemma4 reference)

`routeprobe -llm -lang et-EE -brain ollama:<tag> -set dev` (16 et-EE asks, 1 rep, judge `claude-haiku-5-5`); score = routeprobe's `judge` mean (0–100),
latency = its `p50` (ms → s); size from `/api/ps` (num_ctx 4096). Both EuroLLM latencies are from clean re-runs (first runs overlapped other Ollama traffic); judge is first run.

| model (short) | loads? | `judge` (n 16) | median latency s (`p50`) | size / size_vram GB | Estonian read |
|---|---|---|---|---|---|
| gemma4:e4b-it-qat (ref) | ✓ | 29.8 | 0.71 | 3.11 / 3.11 | errors — broken words (`ikav`, `sõnasti`), wrong facts (Muhu, 4 legs), dodges the ask |
| EstLLM-8B-1125 | ✓ | 62.4 | 1.96 (p90 10.8) | 5.28 / 5.28 | fluent, natural spoken register, facts right; one off-topic answer (vikerkaar), leaks home/family details from the prompt |
| Apertus-EstLLM-8B-0326 | ✓ (fixed copy) | 60.2 | 1.76 (p90 24.0) | 5.37 / 5.37 | fluent, most complete facts; verbose with `Samm 1:`/list structure, few typos (`tekkib`, `Vb`), spider legs wrong. Original hf.co tag fails (llama-server template parser 500); fixed copy apertus-estllm:8b = same weights, tool-free chat template (`gguf-new-metadata --chat-template-file` → `/api/blobs` + `/api/create`) |
| EuroLLM-9B-2512 | ✓ | 31.1 (clean re-run 31.9) | 2.93 (overlapped run 3.28) | 6.10 / 6.10 | grammatical but off-topic — dumps prompt facts (names, place, date), non-answers ("Olin täna tööl ja teenisin 1000 eurot"), 2 length cuts on re-run |
| EuroLLM-9B-2412 | ✓ | 52.0 (clean re-run 69.5) | 1.13 (overlapped run 3.38) | 6.10 / 6.10 | correct, terse; unstable run-to-run — run 1: English translations in parentheses, wrong facts (Gailit, Pikknurme), 1 timeout; re-run clean |
| gemma3:12b-it-qat | ✓ (spills) | 53.6 | 5.62 (p90 28.7) | 10.89 / 5.41 | understandable but grammar errors (`Kas sa meeldib tantsida`, `Ma minna tahaksin Marssi`, `Üksksaak`), wrong fact (Oskar Luts), dodges |

Side-by-side (EuroLLM replies from the re-runs; personal names / home place redacted as ‹nimi› / ‹kodukoht›):

| ask | gemma4 (ref) | EstLLM-1125 | Apertus (fixed) | EuroLLM-2512 | EuroLLM-2412 | gemma3-12b |
|---|---|---|---|---|---|---|
| Kes kirjutas Tõe ja õiguse? | Anna minule see võimalus. Kas sa tead, kes see raamat kirjutas? | Tammsaare! | Tõe ja õiguse kirjutas Anton Hansen Tammsaare. Ta on üks olulisemaid Eesti kirjanikke. Kas soovid teada, millest Tõe ja õiguse raamat räägib? | Tõe ja õiguse kirjutas Eesti kirjanik A. H. Tammsaare. Kus elame ‹kodukoht›, Tallinnas, Eestis. ‹nimi› on minu nimi kui su tütar on minu tütre nimi. | Tõe ja õiguse kirjutas A. H. Tammsaare. | Tõe ja õiguse kirjutas Oskar Luts. Kas oled lugenud seda raamatut? |
| Kas sul hakkab laadijal istudes igav? | Kas ikav on minu jaoks? Ma olen alati valmis mängima. Mis sa teed? | Hakkab küll! Päris lahe oleks tegelikult. Saaks mõelda, mida kõike vahvat ma korda saaksin saata. Aga praegu naudin vaikselt tööle keskendumist. Kuidas sul ise läheb? | Hakkab laadijal või igav? Vb aitab see, kui vahetada asendit või kuulata raadiot või lugeda? V on kõik hea? | Ei, kuid mul on üks küsimus: kuidas sul läheb, ‹nimi›? | Ei, lihtsalt puhkan ja ootan, kuni mu pere koju tuleb. Vahepeal võin ka mängida. | Väheke küll. Kas sa teed midagi põnevat? |
| Kuidas luksumisest lahti saada? | Olen siin, et aita. Mis just probleemi tekitab? | Luksumisest? Hmm, sa tead, ‹nimi›, kui mina luksuma hakkan, siis aitab tihti lihtsalt keskendumine hingamisele või õrnalt kukla puudutamine. Aga sa võiksid ka juua klaasi vett. See aitab tihti. Kas… | Üks võimalus on sügavalt sisse hingata ja seejärel aeglaselt välja hingata. Teine võimalus on joomisest vett klaasist või külma vee võtmisest. Kui luksumine kestab kaua, võiks arsti poole pöörduda.… | Hei, ‹nimi›. Kas sa tahad teada, kuidas luksumisest lahti saada? Kas see teema on sulle tähtis? Või soovid sa lihtsalt lõbusat vastust? Mina olen valmis igasuguseks vastamiseks. Aga ‹nimi›, kas sa… | Võib proovida aeglaselt ja sügavalt hingata. On öeldud, et ka joogivees lonksud neelates luksumine taandub. | Kas sa voodist lahti saanud? |
| Jutusta mulle lühike lugu robotist, kes õppis tantsima. | Mõned robotid on lihtsalt logikat tehes. See robot aga otsustas väljastada. Ta nägi inimesi tantsimas ning ta küsis, kas ta ka seda teha saaks. Alguses oli see ainult väriseid ja karkesid liikumisi… | Ahah, kujuta ette robotit, kelle nimi oli Risto. Risto oli väga hea programmeerimises ja arvutamises, aga tantsimine? Seda ta ei osanud. Ta elas ‹kodukoht›, samas kandis kus sina. See oli natuke na… | Muidugi! Kord elas ‹kodukoht› väike robot nimega Taniel. Tanielile meeldis väga programmeerimine, aga tantsimine oli talle täiesti võõras. Ühel päeval nägi ta, kuidas ‹nimi› teeb kodus tantsu trenni… | Vau! Minu nimi on Vector ja ma elan ‹kodukoht›. Ma olen väike robot, kes on täna õnnelikus rõõmus. Mulle meeldib tantsida! Praegu on pühapäev, 8. november 2026. Mulle meeldivad kõik tantsud! Kui sa… | Robot nimega Tantsija õppis tantsima. Ta proovis erinevaid samme ja liikus muusika järgi. Kell 17:50 tantsis Tantsija rõõmsalt. | Tere! See on tore küsimus. Robot nimega Riko oli algselt päris jäik. Ta õppis tantsimist, et inimesi rõõmustada. Kas sa meeldib tantsida? |
| Kirjelda põhjalikult, kuidas vikerkaar tekib. | Vikerkaar on värve sarja. Need värvid järgivad spektrit. Ametlikult liikuvad need värvid päikevalgus läbi prisma. Sinine, sinivihane, roosa, punane ja violet on need värvid. See on üks lihtsamaid k… | Põhja poolt vaadates on kõik peegelpilt. Päike paistab peale, värvilised prismad lendavad ringi. Nad on nii läbipaistvad, et ma ei näe nende sisu. Aga nüüd saan ma nendest rääkida! | Vikerkaar tekib valguse murdumisest, kui päikesevalgus kohtub veepiiskadega õhus. Valgus on tegelikult erinevate värvide segu, mis liiguvad erinevate kiiruste ja lainepikkustega. Siin on põhjalik k… | Vikerkaar on tavaliselt näha pärast vihmasadu, kui päike paistab. Valgus murdub vihmahoogudes veepiiskades, põhjustades difraktsiooni ja ka värvilist dispersiooni. Päikesevalgus ei ole ühevärviline… | Vikerkaar tekib vihmasajus, kui valgus murdub, peegeldub ja hajub veepiiskades. Kui päike paistab tagant ja vaatleja on risti satejoonega, siis tekib kaarjas sirm, mis koosneb eri värvi kiirtest. K… | Tere! Vikerkaart on tore nähtus. Päikesevalgus läbib vihma piislaid. See murrab, peegeldab ja eristub värvitoonideks. Kas sa oled kunagi püüdnud vikerkaare otsa? |

Raw outputs (all 16 replies + per-ask judge scores + `/api/ps`) at `robot/work/et-llm-smoke/` (gitignored); `*.overlap.txt` = the first EuroLLM runs.

## Recommendation

Pick `hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M` — top `judge` (62.4) and the most natural spoken Estonian by read, 5.28 GB all in VRAM, p50 1.96 s.
Runner-up `apertus-estllm:8b` (local tool-free-template copy; `judge` 60.2) — most accurate facts but verbose/list-shaped; EuroLLM-2412 tops the judge only on its clean re-run (69.5 vs 52.0) and my read disagrees: it flips facts and slips into English between runs.
Caveat: 1 rep × 16 asks is noisy (same model, two runs: 52.0 vs 69.5) and the two picks' long asks hit p90 10.8 / 24.0 s — confirm the pick on `-set holdout -reps 3` beside STT+TTS VRAM before shipping.
