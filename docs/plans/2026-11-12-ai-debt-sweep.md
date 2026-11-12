# my-vector — AI-debt sweep 2026-11-12

Status: OPEN. Census: `docs/plans/2026-11-12-ai-debt-sweep.census.json` @ `9a05cd3`; prior: none.
Execute with `/qimpag docs/plans/2026-11-12-ai-debt-sweep.md`.

Line numbers for census rows are in the census JSON; sites below are named by file and symbol.

Scope is fork-owned code only: `chipper/pkg/myvector/`, `fork_*.go`, `robot/`, `scripts/`, `tts/`, `et-stt/`.
Upstream wire-pod files are out of scope (the fork keeps its upstream diff small), including the 18 allowed
edits in `scripts/upstream-drift.sh`.

Overall verdict: the fork's Go is in good shape. On 125 non-test files (18k lines) `go vet` is clean,
staticcheck reports 1 style note, and `dupl` finds no clone at 100 tokens. The debt is concentrated in four
places: silent failures in gate scripts and config loaders, missing lint gates, small helper families copied
between packages, and a few oversized functions.

## Rulings

Asked 2026-11-12, one round, no notes on any answer.

| # | Class | Default policy | Ruling | Scope / exemptions |
|---|---|---|---|---|
| R1 | repo-helper-reimplementation, cognitive-complexity in probe/bench tools | judge+ask, engine | Dedup only | Move copied helpers to `probekit`. Probe `main` functions are not split. |
| R2 | regrowth guards (lint gates) | lint-rule promotion | Shell and Go | `shellcheck -x` at warning, `go vet` + staticcheck on fork Go. No ruff gate. |
| R3 | swallowed-exception | judge+ask | Fail and log | Gate scripts die when a feed command fails. Go loaders log a parse failure once and keep the defaults. |
| R4 | repo-helper-reimplementation in web pages and config reads | judge+ask | Both | `conf.Read` plus a `myvector/webkit` helper package. Touches settings pages while web-auth Stage 3 is open. |

Not asked, so held at report-only: Python `test-smells`, `unused-knobs`, `inefficient-logic`,
`stdlib-reinvention`, `protected-member-access` (see Report-only hints).

## Coverage

| Adapter / class | Status | Reason |
|---|---|---|
| Go (all classes) | hand-run, not in census JSON | The census has no Go adapter. Ran `go vet`, staticcheck 2024.1.1, gocognit v1.1.3, dupl v1.1.0 and scc on the 219 fork Go files, plus manual same-name-family and discarded-error scans. |
| census run location | clone | Ran on a clean `git clone` of `9a05cd3` under `robot/work/`. On the working tree the simplifier adapter crawls the gitignored `robot/work/` and times out (15 min per analyzer). |
| bats_smells | skipped | no bats files |
| shfmt | skipped | no `.editorconfig` formatter config |
| basedpyright, deptry | skipped | no pyproject / requirements.txt at the root |
| grimp | skipped | no root Python package |
| jscpd-semantic, sh_fanin | skipped | by-hand only tools; not run |
| osv-scanner, rls_static, dep_probe, instr_probe, residue-twin-file, astgrep-satd-agent | skipped | not built in the census |
| JS / HTML in `chipper/pkg/myvector/*/*.html` | unverified | No TS/JS adapter ran (stack detected as sh + py). Only the shared-CSS count in Stage 3.3 was measured by hand. |
| hardcoded-secret | handed off | see Hand-offs |
| webauth package, port exposure | subtracted | owned by `docs/plans/2026-11-11-webui-auth.md` and `2026-11-07-public-wifi-security.md` |
| upstream files (67 census rows) | subtracted | out of scope, see top |

## Ranked findings

| Rank | Class | Tier | Hits | Δ since | Precision (n, seed) | Policy |
|---|---|---|---|---|---|---|
| 1 | swallowed-exception, shell SC2312 | 2 | 284 | n/a | 33% (15, seed 7) | demoted; only the 6 non-test feed sites are sliced (Stage 1.1) |
| 2 | swallowed-exception, Go discarded parse errors | 2 | 6 | n/a | 6/6 (all read) | fix (Stage 1.2, 1.3) |
| 3 | hallucinated-internal-symbol, shell SC2154 | 2 | 61 | n/a | 2% (61) | demoted: 60 are variables set by the sourced `robot/env.sh`; with `-x` 1 remains (Stage 2.1) |
| 4 | swallowed-exception, Python BLE001 + literal return | 2 | 8 | n/a | 0/8 | none: every site logs or relays the error at a process or request boundary |
| 5 | check-weakening, Python RUF100 | 3 | 9 | n/a | 0/9 | none: `noqa: E402` is live under ruff's default rules |
| 6 | test-smells, Python PT018 / PT009 / PT006 / PT011 | 3 | 57 | n/a | 3/57 behaviour-relevant (PT011) | report-only |
| 7 | cognitive-complexity, Go prod | 5 | 4 over 25 | n/a | 4/4 | engine (Stage 4.1) |
| 8 | cognitive-complexity, Go probes | 5 | 11 over 25 | n/a | 11/11 | report-only (R1) |
| 9 | cognitive-complexity, shell `<main>` | 5 | 43 | n/a | about half: test scripts are flat assertion lists | 1 slice (Stage 4.2); rest report-only |
| 10 | repo-helper-reimplementation, Go | 6 | 5 families | n/a | 5/5 | fix (Stage 3.1 to 3.3) |
| 11 | copy-paste-clones, shell | 6 | 4 pairs | n/a | 4/4 | engine (Stage 3.4) |
| 12 | copy-paste-clones, Python | 6 | 2 | n/a | 1/2 | report-only |
| 13 | unused-knobs, Python | 5 | 6 | n/a | 6/6 | report-only (not asked) |
| 14 | unused params, Python | 7 | 34 | n/a | 2/34: the rest are test stubs matching a real signature | report-only |
| 15 | magic-literals, comment-saturation.ratio, py.* | 6 to 9 | 400+ | n/a | not sampled | report-only by catalog |
| 16 | doc-drift | 9 | 1 | n/a | 0/1: `setup/ble.go` is shorthand for a file that exists | none |

## Hand-offs

- `security-reviewer`: 7 gitleaks rows, all in files that come from upstream wire-pod and are already public
  there. `chipper/epod/ep.key` (private key), `chipper/pkg/wirepod/setup/certs.go`,
  `chipper/webroot/sessions:1`, `vector-cloud/internal/testing/s3/s3.go`. No fork-owned file was flagged.

## Stage 1 — swallowed-exception (tier 2)

### Stage 1.1 — vacuous-pass feed commands in gate scripts

- Sites: `robot/firmware/check-static.sh` (the `series` and `rows` `mapfile` reads, the built-JSON `git diff` loop), `robot/firmware/apply.sh` (`series` read), `scripts/live-check.sh` (`check_picker` jq loop, `check_ttsdown` curl read)
- Problem: each loop or `mapfile` reads from `< <(cmd)`. When `cmd` fails the loop runs zero times and the
  script carries on. The built-JSON loop in `check-static.sh` then prints `built JSON = patched (0)` and passes.
- Op: `rewrite`. Run the feed command first, die on failure, then loop over its captured output
  (`out=$(cmd) || die ...; while read ...; done <<<"$out"`).
- Guard: host test `robot/firmware/tests/static-feed.sh` with a stub `git` on `PATH` that fails on `diff`;
  expects a non-zero exit. Register it in `scripts/check.sh` as an `opt` job. Must-fail demo in the commit message.
- Gate: refactor gate. `make check` green.
- Done when: the stubbed failure exits non-zero at all 6 sites, suite green.

### Stage 1.2 — `conf.Read` with a logged parse failure

- Sites: `chipper/pkg/myvector/conf/conf.go` (new `Read`), `llm.Load`, `memory.Model` (`configBytes`),
  `search.LoadConfig`
- Problem: three packages each `os.ReadFile(lang.Path())` and unmarshal on every call. A corrupt
  `myvectorConfig.json` silently reads as defaults.
- Op: `extract`. `conf.Read(path string, v any) error` beside the existing `conf.Update`. A missing file stays
  silent. A parse failure is logged once per file modification time (`myvector: bad config: <path>: <err>`),
  and callers keep their current defaults. `memory.configBytes` stays as the test seam or moves into `conf`.
- Guard: Go test in `conf/conf_test.go`: a corrupt file logs exactly once across two reads and logs again after
  the file changes.
- Gate: refactor gate. Characterize `llm.Load`, `memory.Model`, `search.LoadConfig` on missing, empty, corrupt
  and valid files before the edit.
- Done when: `grep -n 'os.ReadFile(lang.Path())' chipper/pkg/myvector` shows only probe sandboxes; tests green.

### Stage 1.3 — remaining unchecked parses

- Sites: `chipper/pkg/wirepod/ttr/fork_prompt.go` (the `time_zone` jdoc parse), `fork_weather.go`
  (the `weatherAPICladStruct` map read, read error also dropped), `chipper/pkg/myvector/memory/guard.go` (the `snap.Facts` parse)
- Op: `guard`. Log the error and keep the current fallback value. No control-flow change.
- Guard: staticcheck gate from Stage 2.2 does not flag unchecked `json.Unmarshal`; add one table test per site
  that feeds bad JSON and asserts the fallback plus one log line.
- Gate: refactor gate.
- Not sites: `json.Marshal` of maps and plain structs with `_` (cannot fail), `io.ReadAll` on error bodies
  used only for messages, `strconv.Atoi` on the volume token in `quiet/quiet.go` (range-checked on the same line).

## Stage 2 — lint gates (R2)

### Stage 2.1 — `shellcheck -x` at warning severity

- Sites, 16 warning-or-error rows over 97 scripts:
  - SC1007 ×7: `scripts/llm.sh` (2), `scripts/live-check.sh`, `robot/lib/etc.sh` (2),
    `robot/firmware/settings.sh`. Judge each: write `var=''` where an empty assignment is meant.
  - SC1090 ×3: `scripts/tests/check-runner.sh`, `scripts/llm.sh`. Add `# shellcheck source=` directives.
  - SC2148 ×2: `scripts/lib/check-runner.sh`, `robot/lib/etc.sh`. Add `# shellcheck shell=bash` (or `sh` for
    `etc.sh`, which runs under `sh -e` on the robot).
  - one each: SC2120 `robot/netguard/tests/fakes.sh`, SC2034 `robot/lib/etc.sh`,
    SC2154 `robot/firmware/smoke.sh`, SC2155 `robot/firmware/ship.sh`.
- Op: autofix by hand, one code per commit.
- Guard: `.shellcheckrc` (`external-sources=true`, `source-path=SCRIPTDIR`) plus `scripts/tests/shellcheck.sh`
  running `shellcheck -x -S warning` over the same globs as the `bash -n` loop in `scripts/check.sh`;
  registered as an `opt` job. It prints SKIP when shellcheck is absent, as the g++ tests do. Add the shellcheck
  version to `stamp_key`.
- Gate: autofix gate. Must-fail: seed an unassigned variable, see non-zero, restore.
- Info-level rows stay out of the gate: SC2015 ×49 (`a && b || c` is used on purpose), SC2016 ×22, SC2029 ×7.

### Stage 2.2 — `go vet` and staticcheck on fork Go

- Sites: `chipper/pkg/myvector/memory/profile.go` (ST1016: one method uses receiver `s`, 16 others use `st`).
- Op: rename the receiver.
- Guard: extend the Go jobs in `scripts/check.sh`: `go vet ./pkg/myvector/...` and the packages holding
  `fork_*.go`, then `go run honnef.co/go/tools/cmd/staticcheck@2024.1.1` (the last release that builds with the
  repo's go 1.22) with checks `all,-ST1000,-ST1003,-ST1005,-ST1020,-ST1021,-ST1022`. Keep only output lines
  matching `pkg/myvector/|/fork_[^/]*\.go` so the 26 upstream rows never fail the gate.
- Gate: autofix gate. Must-fail: seed an unused function in a `fork_*.go` file.
- Check first: staticcheck needs the module cache on first run. Confirm `make ship`'s `--head` archive copy
  can run it offline, else give the job the same SKIP rule as 2.1.

## Stage 3 — duplication (tier 6)

### Stage 3.1 — probe helpers into `probekit` (R1)

- Sites:
  - `die`: `chainprobe/main.go`, `routeprobe/main.go` (takes an exit code), `visionbench/main.go`,
    `listenprobe/main.go`
  - `median`: `followupjudge/main.go` (copies), `visionbench/main.go` (sorts the caller's slice),
    `searchbench/main.go` (`int64`)
  - config sandbox, `copyFile` plus the `lang.Path()` snapshot: `chainprobe/main.go`,
    `routeprobe/main.go`
  - model unload: `unload` in `visionbench/main.go` and `forkUnload` in `chipper/pkg/wirepod/ttr/fork_prompt.go`,
    identical except for where the error is printed
- Op: `move`. `probekit.Die(code, format, ...)`, `probekit.Median[T cmp.Ordered]` (copying),
  `probekit.Sandbox(...)`. The unload goes to `llm.Unload(base, model) error` because `ttr` should not import
  `probekit`; both callers print the error their own way.
- Guard: count ratchet on same-name families. One Go test in `probekit` that parses `pkg/myvector/*/main.go`
  and fails if `die`, `median` or `copyFile` is declared outside `probekit`.
- Gate: refactor gate. Two commits: helpers, then unload.
- Left alone: TTS request bodies in `fakebot/main.go`, `routeprobe/main.go`, `voice/voice.go`
  (three different field sets), and `ValidModel` in `llm` and `memory` (different rules: memory admits `off`).

### Stage 3.2 — `myvector/webkit`: JSON decode and page serving (R4)

- Sites: `PageHandler` and `decode` in `prompt/http.go`, `voice/settings.go` and `wifi/wifi.go`
- Problem: `decode` in `prompt` and `voice` differs only in the body limit (`maxBody` vs `8192`). `wifi` has
  the same media-type check with a typed argument. `PageHandler` is the same three lines three times.
- Op: `extract`. `webkit.Decode(w, r, v, max, valid) bool` and `webkit.Page(html []byte) http.HandlerFunc`.
- Guard: Go test in `webkit` covering the wrong content type, an oversized body, bad JSON and a failing
  `valid`; plus the family test from 3.1 extended to `decode` and `PageHandler`.
- Gate: refactor gate. Existing handler tests in `prompt`, `voice` and `wifi` stay read-only.
- Sequence: these files are wired through `webauth.Handler`. Land this either before web-auth Stage 3.1 starts
  or after Stage 5 closes, never in the middle of a web-auth slice. `webauth/login.html` is not touched.

### Stage 3.3 — shared page CSS (R4)

- Sites: `voice/settings.html` (95 style lines), `prompt/prompt.html` (88), `wifi/wifi.html` (74); 46 lines
  are common to all three.
- Op: `extract`. `webkit/base.css` embedded and injected into each page at init through a placeholder comment,
  so there is no new route and nothing new for `webauth` to exempt. The login page keeps its inline CSS
  because it renders before auth.
- Guard: Go test that each served page contains the base rules exactly once and that no base rule text is left
  in the three source files.
- Gate: refactor gate, plus an eyeball of the three pages (the only pure-visual step in this plan).
- Also here: JS helpers `call` (3 pages), `post` and `show` (2 pages). Judge when the slice opens; move them
  only if the bodies match.

### Stage 3.4 — shell clones

- Sites:
  - `robot/hostkey/tests/apply.sh` and `robot/sshkey/tests/apply.sh`: the fake `mount` and `setsid`
    stubs. Move to `robot/lib/tests/` beside `fakeclock.sh`; check `robot/netguard/tests/fakes.sh` for the same
    stubs first.
  - `robot/firmware/voice-push.sh` and `robot/wifi/wifi.sh`: the root `UserKnownHostsFile` block.
    Move into `robot/env.sh`, which already builds `ssh_opts`.
  - `robot/crash-review/restarts.sh` and `robot/seassert/catch.sh`: 78 identical tokens near the top (exact lines in the census JSON, class `copy-paste-clones`).
  - `robot/voice-samples/onset.sh` and `robot/volume-probe/pcvoice.sh`: the `r()` ssh wrapper and
    restore-on-exit.
- Op: `extract` into an existing lib file, one pair per commit.
- Guard: none. jscpd would be a new Node dependency and R2 limits gates to shellcheck and Go.
- Gate: refactor gate. The two test-stub moves are covered by the tests themselves; the other three pairs have
  no host test, so each gets a dry-run check written before the edit.

## Stage 4 — cognitive-complexity (tier 5)

### Stage 4.1 — Go production functions over 25

- Sites: `voice.stream` (49), `memory.runESN` (32), `search.Fill` (29), `prompt.Save` (26)
- Op: `extract`, one function per commit.
- Guard: open decision. A watermark needs gocognit in the Go job, which R2 did not cover. Ask before adding it;
  without it this slice ships no guard.
- Gate: refactor gate. `voice.stream` is the robot audio path: characterize with `fakebot` and the existing
  `voice_test.go` only, no robot sound test without asking (robot sound lock).
- Done when: each function scores 25 or less and the package tests are unchanged and green.

### Stage 4.2 — `robot/firmware/check-static.sh` (90)

- Sites: `robot/firmware/check-static.sh` top level, 144 lines, nesting 4
- Op: `extract`. One function per check family (series, markers, built binaries, built JSON), after Stage 1.1
  has changed its feed loops.
- Guard: none beyond its existing tests.
- Gate: refactor gate.

## Report-only hints

No slices. Pick up when the file is next touched.

- Go probe functions (R1): `listenprobe/main.go` main 179, `routeprobe/main.go` runSet 103,
  `followupprobe/main.go` main 84, `chainprobe/main.go` main 68, `visionbench/main.go` runArms 56,
  `voiceprobe/main.go` overlap 43.
- Go test functions over 35: `routeprobe/arms_test.go` 68, `memory/run_test.go` 49,
  `search/fill_test.go` 38, `ttr/fork_prompt_test.go` 37, `ttr/fork_relay_test.go` 36.
- `ttr` fork glue holds about 30 package-level test seams and timeouts (`fork_wait.go` 10, `fork_vision.go` 8,
  `fork_quiet.go` 7). This follows from keeping glue inside the upstream package. A struct per feature would
  cut the shared state, but it is a design change, not a sweep slice.
- Shell `<main>` complexity without a slice: `robot/firmware/ship.sh` 96, `robot/seassert/catch.sh` 70,
  `robot/firmware/smoke.sh` 65, `robot/firmware/settings.sh` 51. These are the ship path and have no host
  tests. Test scripts with high scores (`hostkey/tests/apply.sh` 84, `quiet/tests/guard.sh` 73) are flat
  assertion lists and fine as they are.
- Python, not ruled:
  - `tts/tests/test_et_text.py` (2), `tts/tests/test_server.py` (1): `pytest.raises(ValueError)` without
    `match`.
  - `tts/server.py` `limit(ceiling)`, `:140` `trim_silence(floor_db, head_s, tail_s)`,
    `robot/et-llm/bench.py` `settled_gpu(tol, tries)`: optional parameters no caller passes.
  - `robot/et-llm/bench.py` summary 38, `robot/et-stt/bench.py` clips 32, `tts/server.py`
    `make_handler` (length and complexity).
  - `tts/server.py` and `et-stt/server.py` share handler scaffolding (`make_handler`, the model-load error
    path). They run in separate venvs and services, so a shared module needs a packaging decision first.
  - 7 `open()` calls without a context manager in one-shot scripts, 4 percent-format strings, 8 `re.I`/`re.M`
    aliases.

## Stage 5 — Verify + review

1. `git clone` HEAD into `robot/work/`, then
   `python3 ~/.claude/skills/ai-debt-sweep/scripts/census.py <clone> --since docs/plans/2026-11-12-ai-debt-sweep.census.json`.
   Each fixed class shows its count down and no new keys.
2. Re-run the Go pass: `go vet`, staticcheck, `gocognit -over 25` on the fork files; the four Stage 4.1
   functions are gone from the list.
3. `CHECK_FULL=1 make check`.
4. Dispatch `code-reviewer` over `9a05cd3..HEAD`.
