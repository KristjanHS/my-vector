# README rewrite, worked trails, docs map

Status: ✅ DONE — 1.1–5.1 shipped (251f1dc..); [USER] owed: Stage 4 video, Stage 5.2 reader test.
Out of scope: pushes, docker files (another session).

## Rulings (USER 2026-11-24)

| id | ruling |
|---|---|
| R1 | Four trails in `docs/how-it-was-built.md`, one per type: hard debugging, design rejected, agent bug caught, agent debt deleted. |
| R2 | Each trail: plan → commits → review fix → archive; diff hunk quoted inline; link to the archived plan. |
| R3 | Robot video is [USER] (Stage 4); demo ships now with `images/custom-intent-1.png`, `images/custom-intent-2.png`. |
| R4 | No volume or process-meta wording; no volume headlines (commit counts, agent-%, lines/day). |
| R5 | Keep MIT notices (Kerigan Creighton 2022; Digital Dream Labs 2021 in `chipper/`, `vector-cloud/`) + wire-pod credit. |
| R6 | No internal repo or file names; no private voice-chat text (paraphrase). |
| R7 | No secret paths/hostnames/key vars, no absolute home-directory paths, no emails in the new docs. |

## Constants

| name | value |
|---|---|
| `REPO` | `~/projects/my-vector` (all commands below run with `-C $REPO` or absolute paths) |
| `NEW_DOCS` | `README.md docs/README.md docs/how-it-was-built.md` |
| `SENS`, `VOL` | grep them with `grep -nEi`, run from `$REPO` |
| `GH` | `https://github.com/KristjanHS/my-vector/commit/` (all cited SHAs are on `origin/main`) |
| `UPSTREAM_BASE` | `f78793e44d6c3c5d9533b0a7ac970ce3baa9e080` (`git merge-base HEAD upstream/main`) |
| `STATS` | `python3 ~/projects/claudeconf/scripts/agentic_stats.py --config $CFG --repo my-vector` |
| `CFG` | `<executor scratchpad>/agentic-stats.toml` — no user config exists; write the TOML below there |

`SENS` is the sensitive-term pattern; it is kept outside git and supplied at gate time.

```bash
VOL='[0-9][0-9,]* (commits|plans)|lines ?(/|per) ?day|agent-written|% (of|by) (the )?(code|agents?)'
```

```toml
[[repo]]
name = "my-vector"
path = "~/projects/my-vector"
langs = ["go", "py"]
upstream_ref = "f78793e44d6c3c5d9533b0a7ac970ce3baa9e080"
upstream_name = "wire-pod"
url = "https://github.com/KristjanHS/my-vector"
```

## Trail sources (verified 2026-11-24; SHAs in plan status lines do not resolve — use these)

| trail | plan anchors | commits | quote (SHA · file · selector) | paraphrase, never quote |
|---|---|---|---|---|
| T1 hard debugging | `archive/2026-11-07-vic-anim-seassert.md` L35-37, L41-44 | 52759a3 hunt → 09da0bb archived "no repro" → 323f20b root cause, reopened → 97812eb fix → 63bc3aa review + archive | 323f20b · plan · removed `runtime memory corruption` / `register/VFP-state corruption` lines + `+ ⇒ Wrong (19:30)`; 97812eb · `robot/firmware/0012-se-isinff-bridge.patch` L169-170 (`__se_isinff`, `__se_sin`) | robot crash-dump path, `robot/work/` paths, ship SHAs eaf543d/1fb54ab/4e74538/44a8b57/cfec485a (dangling), md5s |
| T1 review fix | — | 63bc3aa | 63bc3aa · `robot/firmware/check-static.sh` hunk `@@ -113,16 +113,33 @@`: `-math_re=` hand regex → `+float_fns=` SDK exports (trim ≤15 lines) | — |
| T2 design rejected | `archive/2026-11-11-search-news-quality.md` L117 (source arm, wrong credit), L120 Q9, L122-129 table, L131 verdict | 0c5b2d0 opener + replay arms → 79ce09b review → e187702 Q16 → ba580f9 review + archive | 0c5b2d0 · `chipper/pkg/wirepod/ttr/fork_prefill.go` L14-26 (`forkNewsOpener` + `forkPrefillSet`); Q9 table rows quoted from the plan | incident spoken asks (L15-16, L110 Estonian ask text), `robot/work/search-trigger/` paths |
| T3 agent bug caught | `archive/2026-11-08-memory-m2.md` L61 (6.2 review), L62 (6.3) | 9d59701..01dddbd stages → f6cdbd4 findings → M26/M27 rulings → a68b590 + 2f8db93 fixes → 28f8211 archive | a68b590 · `chipper/pkg/myvector/memory/run.go` runESN hunk (`pre.DropRemoved` before each Merge batch + before guard); 2f8db93 · same file, `check` re-ask hunk (`removedN`, `again`) | commit-message test facts (Estonian fixture sentence, pets), real robot serial in `<esn>` paths, 2f8db93's dangling e6b0a08 |
| T4 agent debt deleted | `archive/2026-11-08-vision-setting.md` L3 status, L11 V3 | fba3499 VisionModel deleted → 516c1b1 review fix (+ fd28127 drops dead `forkTopP`) → 7829a54 archive | fba3499 · `chipper/pkg/myvector/llm/llm.go` `@@ -175,12 +175,18 @@` (`-func VisionModel` → `+func ResolveVision`); 516c1b1 · `chipper/pkg/wirepod/ttr/fork_prompt.go` `-func forkResolveVision` → `+func forkVision(…, t llm.Target)` | V12 config path, `claude_key`, photo/bench paths, L3's dangling 699fed0 |

- T1 framing: L35's `⇒ Wrong` marks the `.data`/VFP-corruption hypotheses wrong; the isinf line after it is the real mechanism.
- Extract a hunk: `git -C $REPO show <SHA> -- <file> | sed -n '/<selector>/,+N p'`; quote as a ```diff block, trimmed, never retyped.

## Refactor-first

Skip — docs-only plan, no code touched.

## Stages

### Stage 1.1 — `docs/how-it-was-built.md` skeleton + T1 + T2

- Focus: new `docs/how-it-was-built.md` (`git check-ignore -v` returned nothing: trackable).
- Change: 1-line intro + `## <type>: <title>` per trail with Plan · Commits (`$GH<sha>` links) · Review fix · Archive · quoted ```diff.
- Verify: `git -C ~/projects/my-vector add docs/how-it-was-built.md && ~/projects/my-vector/scripts/tests/cites.sh 2>&1 | tail -20`
- Falsifier: `git -C ~/projects/my-vector show --format= 97812eb | grep -F '__se_isinff(float x)' | head -1` line (`--format=`: patch only, never the commit message) also in the doc (`grep -cF`) ≥1; same for `forkNewsOpener = ` vs 0c5b2d0.
- Falsifier: `grep -nEi "$SENS|$VOL" docs/how-it-was-built.md` → 0 hits, after a known-hit file returns ≥1 (proves `SENS` can fail).
- Commit: `docs: how it was built — debugging + rejected-design trails`.

### Stage 1.2 — T3 + T4

- Focus: `docs/how-it-was-built.md`.
- Change: append T3 and T4 in the 1.1 shape; end with a link to [claudeconf](https://github.com/KristjanHS/claudeconf) for the method.
- Verify: `~/projects/my-vector/scripts/tests/cites.sh 2>&1 | tail -20`
- Falsifier: `pre.DropRemoved(esn)` (a68b590) and `-func VisionModel(c Config, kg KG) string {` (fba3499) each found in commit AND doc: `l=$(git -C ~/projects/my-vector show --format= <sha> | grep -F -- '<str>' | head -1)` non-empty, then `grep -cxF -- "$l" docs/how-it-was-built.md` ≥1 (`--format=`: patch only, never the commit message).
- Falsifier: `grep -c '^## ' docs/how-it-was-built.md` = 4; `SENS`/`VOL` grep → 0 hits.
- Commit: `docs: how it was built — review-catch + debt-deletion trails`.

### Stage 2.1 — `docs/README.md` map

- Focus: new `docs/README.md`.
- Change: one line per `docs/*.md` + `plans/` (active) + `plans/archive/` (closed, kept) + `../robot/firmware/` patch series.
- Verify: `cd ~/projects/my-vector && for f in docs/*.md; do [ "$f" = docs/README.md ] || grep -qF "${f#docs/}" docs/README.md || echo "MISSING $f"; done 2>&1 | tail -20`
- Falsifier: loop prints nothing; delete one line locally → it prints `MISSING` (restore). `SENS` grep → 0 hits.
- Commit: `docs: docs/README.md map`.

### Stage 3.1 — README restructure

- Focus: `README.md` (51 lines). Replace the L28-30 "How it was built" body; never add a second such section.
- Order: `# my-vector` + 1–2-line tagline → `## Demo` → `## What my-vector adds` → `## Quickstart` → `## Architecture` → `## Estonian models` → `## How it was built` → `## Base: wire-pod` → `## Credits` → `## License`.
- Demo: both PNGs, captioned as wire-pod's custom-intent editor (upstream screenshots), then a `<!-- robot-video -->` line for Stage 4.
- Adds: keep L7-11 bullets; add memory M2, follow-up listening, prompt templates, sound lock (`make quiet`), Tailscale + WireOS unlock, wifi hardening.
- Architecture: ≤12-line ASCII — robot (WireOS + `robot/firmware` patches) ↔ chipper (Go, `chipper/pkg/myvector`) ↔ STT `et-stt/` · TTS `tts/` · LLM Ollama/Claude · search Tavily/SearXNG + weather.
- How it was built: markers `<!-- agentic-stats:start -->` / `<!-- agentic-stats:end -->` (empty), one paragraph (went well / agents got wrong), links to the 4 trail anchors, `docs/README.md`, claudeconf.
- License: MIT; name all three notices (R5); wire-pod credit + Credits list unchanged.
- Verify: `cd ~/projects/my-vector && grep -n '^## \|agentic-stats' README.md 2>&1 | tail -20`
- Falsifier: `grep -c '^## How it was built' README.md` = 1; each marker count = 1; `grep -c 'Kerigan Creighton' README.md LICENSE` ≥1 each.
- Falsifier: `cd ~/projects/my-vector && grep -oE '\]\([^)#]+' README.md | cut -c3- | grep -v '://' | while read p; do [ -e "$p" ] || echo "BROKEN $p"; done` prints nothing (`scheme://` links skipped; the old `[^)#:]+` cut every https link to `https` → 19 false `BROKEN`); `SENS`/`VOL` → 0.
- Commit: `docs(readme): replace upstream README — demo, architecture, how it was built`.

### Stage 3.2 — stats block

- Focus: `README.md` between the markers. Precondition: 3.1 committed (`STATS` reads HEAD); `python3 -c 'import yaml'` ok; `scc` on PATH.
- Change: write `CFG` from the TOML constant; paste `$STATS | tail -n +3 | sed '/^$/d'` (table only, no `### my-vector`, no trailing blank line — it makes `--check` stale) between the markers.
- Verify: `$STATS --check 2>&1 | tail -20; echo "rc=${PIPESTATUS[0]}"` → `rc=0`.
- Falsifier: run `--check` BEFORE pasting → `rc=1` (`stale: …README.md`); after → `rc=0`. Rows hold no SHA, path or email.
- Commit: `docs(readme): agentic-stats block`.

### Stage 4 — [USER] robot video drop-in

- [USER] record ≤30 s: one Estonian ask + one English ask answered aloud; no faces, home audio or private chat content.
- [USER] upload by dragging the file into GitHub's web README editor; copy the `https://github.com/user-attachments/assets/<id>` URL.
- Change: replace the `<!-- robot-video -->` line in `## Demo` with that bare URL on its own line; no video file in the repo.
- Verify: `grep -c 'user-attachments/assets' ~/projects/my-vector/README.md 2>&1 | tail -20` → 1; `grep -c 'robot-video' README.md` → 0.
- Commit: `docs(readme): robot demo video`.

### Stage 5.1 — gates + review

- Verify: `make -C ~/projects/my-vector check 2>&1 | tail -20` (my-vector has no `test` target; `check` runs cites + secrets + Go tests).
- Falsifier: `grep -nEi "$SENS|$VOL" $NEW_DOCS` → 0 hits, after a known-hit file returns ≥1.
- Review: `Agent(subagent_type="code-reviewer")` over 1.1..3.2 — check each quoted hunk against `git show <SHA>`, R4-R7, link targets.
- Commit review fixes as `fix(docs): review of <range> — …`; then set Status ✅ DONE and `git mv` this plan to `docs/plans/archive/`.

### Stage 5.2 — [USER] reader test

- [USER] one cold reader opens the README on GitHub: can say what it does, how to run it, and open one trail in <2 min.
