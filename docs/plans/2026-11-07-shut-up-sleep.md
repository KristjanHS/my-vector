# Shut up → sound lock + nap

Status: staged, no code yet. "Shut up" / "turn off" (en + et, whole phrase) → 2 h lock on every robot sound test
Claude starts + the night-window deep sleep for 2 h. Stage 1 (open-mic research) runs first; its FAIL closes Stage 5 only.

## Rulings

| id | ruling | edit site |
|---|---|---|
| RF | Refactor-first: one, inside patch 0011 — its three `kSleepCycle_NightWindow && IsNightTime()` copies become one helper `MyVectorNight()` before the nap joins it (Stage 4.1, behavior-neutral). Go/bash side: skip test fired — the seam is one call in an already-listed upstream function, no existing code to restructure. | `0011-night-window.patch` |
| M1 | Whole-phrase match: lowercase, strip punctuation, drop filler tokens, the remaining token sequence must EQUAL a phrase. Both lists match in every saved language. | `quiet.Match` |
| M2 | Phrases (tunable, source is the home): en `shut up` `shutup` `turn off`; et `ole vait` `jää vait` `vait` `lülita välja` `lülitu välja`. Fillers: `hey` `hei` `vector` `victor` `vektor` `please` `palun` `now` `nüüd`. | `quiet.Phrases` `quiet.Fillers` |
| M3 | Seam: first statement after `ForkNormalizeText` in `ProcessTextAll` — `if forkQuiet(req, voiceText, botSerial) { return true }` — ahead of plugins (HA), custom intents, built-ins and the LLM; covers `intent.go`, `intent_graph.go`, `routeprobe`. `intent_imperative_shutup` is already outside `forkAllowedIntents`, so nothing built-in to disable. | `matchIntentSend.go` (listed; extend its drift comment); `ttr/fork_quiet.go` |
| M4 | On a match: lock until max(current, now+`LockFor`), source `voice:<phrase>`; async nap of the requesting robot (`vars.BotInfo.Robots` by ESN → `IPAddress`; no IP → log + skip); reply `IntentPass(req, "intent_system_sleep", …)`. ⚠ hedge: that the robot plays go-to-sleep for it is unverified (Stage 6 user step). | `forkQuiet` |
| L1 | Lock = one gitignored file `<main checkout>/robot/work/quiet-until`: `<until-epoch> <source>`; written tmp+rename (root server and user scripts can both replace it). Override `$MYVECTOR_QUIET_FILE`. Server path: first existing of `robot/work`, `../robot/work` (miccap precedent). | `quiet.Path` `quiet.Set` `quiet.Until`; `robot/quiet/guard.sh` |
| L2 | Control: `robot/quiet/quiet.sh {on [min]\|off\|status}` — on = lock + nap, off = rm lock + nap 0, status exits 3 while locked. Make targets `quiet`, `unquiet`; Claude runs `unquiet` only on the user's ask. | `robot/quiet/quiet.sh`; `Makefile` |
| G1 | **Default-block with a silent allowlist** (picked over opt-in: a new robot script is blocked until it declares itself silent). `env.sh` sources `robot/quiet/guard.sh` and calls `quiet_guard` unless the sourcing script set `silent=1` (unexported) before sourcing. Locked → stderr `QUIET: robot sound locked until HH:MM (<source>); make unquiet lifts it`, exit 3. | `robot/env.sh`; `robot/quiet/guard.sh` |
| G2 | Silent allowlist (`silent=1`): `diskguard/apply.sh` `firmware/{compat,quiet-count,settings,ship,night-check,voice-push}.sh` `firmware/tests/pc-voice.sh` `logkeep/apply.sh` `wifi/wifi.sh`. Everything else sourcing `env.sh`/`volume-probe/lib.sh` is a sound script: `voice-samples/{play,onset}.sh` `volume-probe/{sweep,idle,anim,pcvoice}.sh` `firmware/smoke.sh` `unlock/power-stress.sh`. `settings.sh` is silent (volume set = no sound, G5). | each listed script |
| G3 | Loops re-check: `play.sh` per sample, `sweep.sh`/`idle.sh`/`anim.sh` per step, `power-stress.sh` per minute call `quiet_guard` — a lock set mid-test stops it. | those scripts |
| G4 | Entries not sourcing `env.sh`: `scripts/live-check.sh ttsdown` sources `robot/quiet/guard.sh` and guards. Other `live-check` checks are PC-only (no robot sound) and stay unguarded. | `live-check.sh` `check_ttsdown` |
| G5 | Server refuses robot sound endpoints while locked (423 + the QUIET line): `/api-sdk/{say_text,play_sound,trigger_wake_word,button_hey_vector,button_alexa,cloud_intent}` registered more-specifically in a fork `init` that falls through to `SdkapiHandler`; `fork_say` and `voice.DemoAPI` check in place. Zero upstream edits. LLM/intent replies are untouched. `volume` passes (ruled 2026-11-07: `ApplySettingMasterVolume` → `SetRobotMasterVolume` sets state, posts no sound). | `sdkapp/fork_quiet.go`; `forkSay`; `voice.DemoHandler` |
| C1 | Repo `CLAUDE.md` gains `## Robot sound lock` (≤5 lines): no sound-producing robot command of any kind while `robot/quiet/quiet.sh status` exits 3 — scripts, ad-hoc curl to `:8080/api-sdk`, `:8888`/`:8889`, ssh playback; check before EVERY such command, not once per session. Ask before any robot sound test; volume stays 1. After each sound test run `robot/quiet/scan.sh <start>`; a hit → stop all sound tests, `make quiet`, tell the user. | `CLAUDE.md` |
| S1 | `robot/quiet/scan.sh <since>`: `journalctl -u wire-pod --since` "Transcribed text" lines (+ open-mic transcripts if Stage 5 ships) piped through `quietmatch`; exit 1 + the line on a hit. Phrase table has one home (Go). | `robot/quiet/scan.sh`; `pkg/myvector/quiet/quietmatch` |
| N1 | Firmware nap: console var `kSleepCycle_NapUntil_s` (int, epoch s, 0 = off); `MyVectorNight()` = `kSleepCycle_NightWindow && (IsNightTime() \|\| MyVectorNapActive(kSleepCycle_NapUntil_s, time(nullptr)))`. Set via vic-engine `:8888/consolevarset?key=SleepCycle_NapUntil_s&value=<epoch>`. ⚠ hedge: nap gets the patch's three night rules, not every stock `IsNightTime()` consumer; resets on engine restart (a ship or reboot ends the nap, the lock file stays). | `0011-night-window.patch`; `quiet.Nap` |
| N2 | `MyVectorNapActive(until, now)` = `until > now && until - now <= NapMax`; a ms-epoch or garbage value is refused (logs `my-vector.NightWindow` `nap refused`). | patch block `// my-vector nap` |

## Constants (source is the home; defaults below)

| symbol | value | validated by |
|---|---|---|
| `quiet.LockFor` / `quiet.NapFor` | 2 h / 2 h | `TestSetExtendsNeverShortens` |
| `NapMax` (patch) | 4 h | `tests/nap.sh` |
| `quiet.NapTimeout` | 5 s | `TestNapTimeout` |
| guard exit code | 3 (2 stays SETUP) | `robot/quiet/tests/guard.sh` |

## Stage 1 — open-mic research (verdict gates Stage 5 only)

Order (ruled 2026-11-07): 1.1 ∥ Stage 2.1 (`quietmatch`, which 1.2 pipes through) → `probe.sh` → STOP for the user-run live test (1.2–1.3) → 1.4 → Stages 2.2→4.

Focus: `robot/openmic/probe.sh` (sound script → obeys the lock); clone `robot/work/wire-os-victor/animProcess/src/cozmoAnim/micData/` and `animProcessMessages.cpp`.
Candidates: vic-anim console func `RecordMicDataClip` (`kMicData_ClipRecordTime_ms` 500–15000), `MicDataSystem::RecordRawAudio` (find its message and whether a console path reaches it), `kMicData_SaveRawFullIntent_WakeWordless`, `StartWakeWordlessStreaming` (streams to wire-pod STT; `playGetInFromAnimProcess` must be false). Echo: `kMicData_SpeakerNoiseDisablesMics` (default false).

| slice | work | pass bar |
|---|---|---|
| Stage 1.1 | Source read: per candidate — trigger path from WSL, output file/location, audible side effect (earcon/get-in anim), firmware change needed? | ≥1 candidate with no audible side effect, reachable without a firmware patch or with ≤1 small patch |
| Stage 1.2 | `probe.sh -m`: record a clip while `play.sh` runs a sample set at volume 1, STT 2 s windows / 1 s hop via myvector-stt → `quietmatch`. PC speaker says "shut up" (af_bella) / "ole vait" (mari) at ~1 m in the pause after Vector's sentence (`-o` over it: recorded, not gated). **Live audio test — ask the user first.** | R2 (ruled 2026-11-07, pause bar): pause run ≥4/5 proc whole hits per language; `-q` 0/5 |
| Stage 1.3 | Same rig: "Hey Vector, shut up" over the robot's own speech — does the wake word fire? Recorded, not a gate (sets expectations for the wake-word path). | — |
| Stage 1.4 | Verdict: write `PASS`/`FAIL` + one evidence line per R1/R2 (+ R3 result) into §Stage 1 verdict below; FAIL → Stage 5 status line becomes `CLOSED: <why>`. | — |

### Stage 1 verdict

(pending) — owed: user re-runs `robot/openmic/probe.sh -m et-EE et-llm piper-et_EE-news-medium-Mari` and the en-US equivalent with the windowed probe.

## Stage 2 — server whole-phrase matcher

Focus: `chipper/pkg/myvector/quiet/{quiet.go,quiet_test.go,quietmatch/main.go}`; `chipper/pkg/wirepod/ttr/fork_quiet.go` + `fork_quiet_test.go`; `matchIntentSend.go` (one line); `scripts/upstream-drift.sh` comment; `routeprobe` `isolate`.

| slice | edit sites | falsifiers (red demo first) | verify |
|---|---|---|---|
| Stage 2.1 | `quiet.Match` `Phrases` `Fillers` `Path` `Set` `Until`; `quietmatch` (stdin lines → matching lines, exit 1 on any hit) | `TestMatchTable`: hits `shut up`, `Shut up!`, `turn off`, `vector, shut up please`, `hey vector turn off`, `ole vait`, `Jää vait.`, `palun lülita välja`; misses `turn off the kitchen light`, `don't turn off the music`, `shut up and dance`, `lülita tuled välja`, `ära ole vait`, `vaikselt`. Red: switch `Match` to `ForkContainsPhrase` → the misses fail. `TestSetExtendsNeverShortens`, `TestSetAtomic` (tmp+rename, readable mid-write) | `cd chipper && go test ./pkg/myvector/quiet/` |
| Stage 2.2 | `quiet.Nap(ip, until)` (`NapURL` overridable for tests); `forkQuiet`; the `ProcessTextAll` line; `isolate` sets `MYVECTOR_QUIET_FILE` to a temp file and `MYVECTOR_NAP=off` | `TestForkQuietShortCircuits` (fake stream: returns true, sent `intent_system_sleep`, lock file set, httptest nap hit with `SleepCycle_NapUntil_s`); `TestForkQuietReachesPlugin` (`turn off the kitchen light` → no lock, falls through); `TestForkQuietNoIPSkipsNap`; `TestNapTimeout`. Red: delete the `ProcessTextAll` call → ShortCircuits fails | `make check`; `cd chipper && go run ./pkg/myvector/routeprobe "vector, shut up please"` → `intent: intent_system_sleep`; `… "don't turn off the music"` → LLM answer |

## Stage 3 — sound guard + Claude instructions

Focus: `robot/env.sh`; `robot/quiet/{guard.sh,quiet.sh,scan.sh,tests/guard.sh}`; G2/G3 scripts; `scripts/live-check.sh`; `scripts/check.sh`; `Makefile`; `chipper/pkg/wirepod/sdkapp/fork_quiet.go` + test, `fork_smoke.go`, `pkg/myvector/voice`; `CLAUDE.md`.

> **UD1 (ruled 2026-11-07): ship never blocks** — while locked, `smoke.sh` skips its A2 speech with a `WARN: sound lock until <T>` and runs the rest; `ship.sh` proceeds to push.

> **UD2 (ruled 2026-11-07): rule only** — C1 in `CLAUDE.md`, no `PreToolUse` hook, no `.claude/settings.json` edit.

| slice | edit sites | falsifiers (red demo first) | verify |
|---|---|---|---|
| Stage 3.1 | `guard.sh` (`quiet_locked`, `quiet_guard`); `env.sh` G1 (+ `MYVECTOR_ROBOT_DIR` override for tests); `quiet.sh`; `make quiet` / `make unquiet` / `make quiet-status` | `robot/quiet/tests/guard.sh`, fake `robot.env` + key, fake PATH `curl`/`ssh` that log calls: with an active lock every G2 sound script exits 3, prints `QUIET:`, logs zero curl/ssh calls; with an expired lock they get past the guard; `firmware/tests/night-check.sh` passes under an active lock (silent script still runs). Red: drop `silent=1` from `night-check.sh` → test fails | `robot/quiet/tests/guard.sh`; `make check` (runs it) |
| Stage 3.2 | `silent=1` on G2 allowlist; G3 in-loop checks; G4 `ttsdown`; UD1 outcome in `smoke.sh` | guard test adds: lock written by the fake `curl` after `play.sh`'s first sample → exit 3 after exactly one call; static: every `robot/**/*.sh` sourcing `env.sh`/`lib.sh` is either in the test's sound list or sets `silent=1` (a new unlisted script fails). Red: add a scratch script sourcing `env.sh` → static check fails | `make check` |
| Stage 3.3 | G5 `sdkapp/fork_quiet.go` `init`; `forkSay`; `voice.DemoHandler` | `TestForkQuietBlocksSound`: each G5 path → 423 while locked, reaches the stub handler when unlocked; `TestForkQuietLeavesSilent` (`get_battery` passes while locked). Red: unregister `say_text` → fails | `make check`; locked live: `curl -s -o /dev/null -w '%{http_code}' -G localhost:8080/api-sdk/say_text --data-urlencode text=x` → `423` |
| Stage 3.4 | C1 `CLAUDE.md` section; `scan.sh` | `scan.sh` host test: fixture journal with `Transcribed text: vector shut up` → exit 1; `turn off the kitchen light` → exit 0 | `make check`; `grep -n 'Robot sound lock' CLAUDE.md` |

## Stage 4 — firmware nap switch

Focus: `robot/firmware/0011-night-window.patch`; `markers.tsv`; `tests/{nap.sh,0011-nap_test.cpp}` (cut-block pattern of `tests/pc-voice.sh`); `night-check.sh` + `tests/night-check.sh`; `scripts/check.sh`.

| slice | edit sites | falsifiers (red demo first) | verify |
|---|---|---|---|
| Stage 4.1 | RF: `MyVectorNight()` helper replaces the three copies; no behavior change | `check-static.sh` markers unchanged + `git diff` of the patch shows only the helper swap | `robot/firmware/check-static.sh`; `make check` |
| Stage 4.2 | N1/N2: `kSleepCycle_NapUntil_s`, `MyVectorNapActive` in a `// my-vector nap` block, log `my-vector.NightWindow` `nap until <epoch>`; markers.tsv rows (src + bin `kSleepCycle_NapUntil_s`); `quiet.Nap` already calls it | `tests/nap.sh`: 0 → off; until=now+1h → on; until=now → off; until=now+5h → refused; ms epoch → refused. Red: drop the `NapMax` bound → fails. SKIPs without g++/clone like `pc-voice.sh` | `robot/firmware/tests/nap.sh`; `make check` |
| Stage 4.3 | `night-check.sh --nap` judges the window from the last `nap until` log line to its epoch with the same wake-hit awk | fixture in `tests/night-check.sh`: nap window + `InitialWakeUp` anim inside → FAIL, outside → PASS | `robot/firmware/tests/night-check.sh` |
| Stage 4.4 | user runs `make ship` (never Claude); then the Stage 6 live nap step | — | user-owed |

## Stage 5 — open-mic build

Status: blocked on §Stage 1 verdict. Shape if PASS (re-cut by the verdict's capture path): `quiet_guard` in a sound script, when open-mic is on, starts `robot/openmic/listen.sh` in the background (trap-killed on exit): clip loop → scp → myvector-stt → `quietmatch`; a hit → `quiet.sh on` + kill the test's process group. Falsifier: fake-clip fixture with "ole vait" → lock set and the parent test exits 3.

## Stage 6 — final

| slice | work | verify |
|---|---|---|
| Stage 6.1 | full gate | `make check` |
| Stage 6.2 | no-voice e2e: routeprobe M2 table; `robot/work/live-check/fakebot -esn 000fake -lang <saved> -say "vector, shut up please" -want intent_system_sleep` → lock file set, log `nap skipped`; `robot/voice-samples/play.sh <set>` → exit 3 `QUIET:`; G5 `say_text` → 423; `make unquiet` → play.sh passes the guard (Ctrl-C before audio) | each command's exit code |
| Stage 6.3 | `code-reviewer` dispatch over the whole diff | findings fixed or answered |
| Stage 6.4 | user-owed (after ship, ask first, volume 1): real "Hey Vector, shut up" and "Hey Vector, ole vait" → he goes to sleep, stays down through a sound and a wave, wakes on touch and wake word; `robot/firmware/night-check.sh --nap` PASS; `make unquiet` → `SleepCycle_NapUntil_s` 0 | `docs/backlog.md` §OWED[USER] one line at archive |
