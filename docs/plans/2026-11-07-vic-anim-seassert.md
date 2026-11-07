# vic-anim SeAssert nrgainv.c:191 — root-cause hunt (option C)

Status: **parked — no repro** (0 hits in 10 gdb rounds + 5 plain restarts, 2026-11-07). Resume on the next S1 at step 2.

## Background

`make ship` b70e53a (2026-11-07 11:53) failed its smoke test: vic-anim died ~13 s after the deploy restart with
`SeAssert: ../../../se_lib/nrgainv.c:191`, then SIGILL (exit status 246). The robot's fault-code-handler restarted
anki-robot.target ~15 s later and it recovered. Nothing was pushed; `main` is 6 commits ahead of `origin`.

The user suspected same-day changes. User's options round: A = smoke tolerates this one signature, B = do nothing and
re-ship, **C = dig deeper with robot restarts (chosen; the power-on sound on each restart is accepted).**

## Evidence so far

- **Not a same-day change.** The robot crash dumps (`/data/data/com.anki.victor/cache/crashDumps/`, they survive
  reboots; the journal does not) show the identical assert on **2026-11-05 09:22**, also during a ship-deploy restart,
  before patch 0011, the Estonian/lang work and the WiFi page existed.
- **Same binary passed and failed.** The robot's vic-anim (md5 `74a2435a…`, built 2026-11-06 21:03) passed 5 ships
  earlier on 11-07. The deploys copy only `lib/libc++.so.1` every time: same content (md5 `d315fd6c…`), the build
  only refreshes its mtime. Every ship log since 11-05 shows it.
- **Rate:** 2 crashes, both on deploys (~21 ship deploys); 0 in 5 gdb deploys, 5 gdb restarts, 5 plain restarts (11-07).
- **Timing:** both asserts fire in the same instant vic-engine reaches `engine_ready` and plays `anim_power_offon`,
  the first speaker audio after start. In `micDataProcessor.cpp`, speaker-active → `hasRobotNoise` →
  `SigEsBeamformingOff` → SE policy `FBF_FORCE_ECHO_CANCEL_WITH_NR`. The SE echo reference is a constant zero
  buffer (`dummySpeakerOut`). This lines up in time; it is **not proven** to be the cause.
- **Where:** symbolized against `robot/work/wire-os-victor/_build/vicos/Release/bin/vic-anim.full`. Call path
  `DoSenrPostProcessing` (se_nr.c:1984) → `DoNrGainLookup_flt32` → `NrGainLookupCore` (nrgainv.c:191), the same in
  both dumps. se_lib is closed source (prebuilt only).
- **The assert (from the disassembly):** `result = floor + table[i].gain * (1 - floor)`, assert `0 <= result <= 1`
  (NaN fails). `i` = first entry with `a/b <= x`; `isinf(a/b)` → i = 17. A NaN ratio gives i = 0. The tables
  (`GainLookupTable_Range6dB` @ +0x37F09C, `_Range12dB` @ +0x37F12C, 18 × {x, gain}, FLT_MAX sentinel, gains in
  [0,1]) sit in **writable `.data`**. Both callers assert `0 <= floor <= 1` first (lines 221, 257; the stack shows
  both checks passed).
  ⇒ With a valid table and floor, line 191 **cannot** fail. So either the table memory was overwritten at runtime,
  or a VFP register value (floor in d8) got corrupted.
- **Minidumps** (`robot/work/seassert/*.dmp`, read with `robot/seassert/md.py`): `r8` = caller's table pointer =
  base + 0x37F12C (the valid 12dB table) in both. The VFP registers are not captured (all 0) and breakpad scrubs
  non-pointer stack words to `0x0defaced`, so the operand values are lost. Caller loop count left: 254 (11-07),
  257 (11-05).

## Plan (pre-registered)

1. On a new SeAssert (smoke FAIL or `robot/crash-review/crashes.sh`): `crashes.sh -p` first — keeps the dump + `.dmp.log`.
2. With the user's OK: `robot/seassert/catch.sh -d N` (deploy rounds, gdb on vic-anim's line-191 branch; ~65 s/round).
   It derives the breakpoint from the clone's build, so build the shipped tip first (`docs/crash-debugging.md`).
3. **Stopping rule (user ruling 2026-11-07):** stop at the first hit; N is the user's (last runs: 5). A miss is inconclusive.
   A vic-engine SIGSEGV / fault 800 during a gdb round is gdb-induced — not this bug.
4. **Read the hit:**
   - table ≠ file values → **runtime memory corruption** of `.data` → find the writer (watchpoint on the table
     with the same gdb tooling).
   - table intact but floor/d8 out of range or NaN → **register/VFP-state corruption**, or the caller passing an
     unchecked floor (re-read the caller).
   - otherwise → re-derive from the values.
   On a hit: record the reading here, commit, then the fix is a user round (no patch before it).
5. **Possible fixes** (se_lib can't be rebuilt): link vic-anim with `-Wl,--wrap=DoNrGainLookup_flt32` and supply a
   safe wrapper (clamp/NaN-guard), as the next firmware patch in `robot/firmware/series`; or fix the corrupting writer if it is in our or
   Anki's source. Fallback: option A (smoke tolerates this exact signature once).

## Files

- `robot/seassert/catch.sh`: gdb capture loop. `robot/seassert/md.py`: ARM minidump reader. Runbook: `docs/crash-debugging.md`.
- `robot/work/seassert/`: crash dumps, `.dmp.log`s, the 11-07 crash-window journal (gitignored).
- Memory note: `vic-anim-seassert-startup-flake`.
