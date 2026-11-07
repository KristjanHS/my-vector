# vic-anim SeAssert nrgainv.c:191 — root-cause hunt (option C)

Status: **in progress** — capture tool written, not yet run. Next action: step 1 below.

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
- **Rate:** 2 crashes in ~25+ startups.
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

1. ✔ gdb-multiarch 12.1 installed on the host (2026-11-07).
2. Run `robot/seassert/catch.sh 20`. Each round restarts anki-robot.target like a deploy, attaches gdbserver
   (stdio over ssh) to the new vic-anim, and breaks at vic-anim+0x157804 (line-191 failure branch, before fprintf).
   On a hit it dumps s16 (result), s18 (ratio), s4 (gain), s2 (1−floor), the table at r4, caller d8 (floor) at sp+8,
   inputs [r6]/[r5], and bt. Output goes to `robot/work/seassert/catch-*/`. ~40 s/round, ≤ 40 min.
   - Not yet run even once: watch round 1 (attach works? `handle all` too noisy? does a timeout INT detach
     cleanly?) before leaving it to loop.
   - Code review of `catch.sh` is owed after the first working run.
3. **Stopping rule (user ruling 2026-11-07):** stop at the first hit. Restarts: 5 attached, no hit (round 6 = fault 800,
   cut). Next `catch.sh -d 5`: ship deploy (deploy-v.sh + settings.sh), round 1 watched; a miss is inconclusive.
4. **Read the hit:**
   - table ≠ file values → **runtime memory corruption** of `.data` → find the writer (watchpoint on the table
     with the same gdb tooling).
   - table intact but floor/d8 out of range or NaN → **register/VFP-state corruption**, or the caller passing an
     unchecked floor (re-read the caller).
   - otherwise → re-derive from the values.
   On a hit: record the reading here, commit, then the fix is a user round (no patch before it).
5. **Possible fixes** (se_lib can't be rebuilt): link vic-anim with `-Wl,--wrap=DoNrGainLookup_flt32` and supply a
   safe wrapper (clamp/NaN-guard), as a new firmware patch `0012`; or fix the corrupting writer if it is in our or
   Anki's source. Fallback: option A (smoke tolerates this exact signature once).

## Files

- `robot/seassert/catch.sh`: gdb capture loop. `robot/seassert/md.py`: ARM minidump reader.
- `robot/work/seassert/`: crash dumps, `.dmp.log`s, the 11-07 crash-window journal (gitignored).
- Memory note: `vic-anim-seassert-startup-flake`.
