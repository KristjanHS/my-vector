# vic-anim SeAssert nrgainv.c:191 — root-cause hunt (option C)

Status: **ROOT CAUSE FOUND 2026-11-07 19:30** (3rd hit, ship 44a8b57) — fix awaits the user's ruling on §Fix.

## Background

`make ship` b70e53a (2026-11-07 11:53) failed its smoke test: vic-anim died ~13 s after the deploy restart with
`SeAssert: ../../../se_lib/nrgainv.c:191`, then SIGILL (exit status 246). The robot's fault-code-handler restarted
anki-robot.target ~15 s later and it recovered. 3rd hit: ship 44a8b57 (19:20), not pushed.

The user suspected same-day changes. User's options round: A = smoke tolerates this one signature, B = do nothing and
re-ship, **C = dig deeper with robot restarts (chosen; the power-on sound on each restart is accepted).**

## Evidence so far

- **Not a same-day change.** The robot crash dumps (`/data/data/com.anki.victor/cache/crashDumps/`, they survive
  reboots; the journal does not) show the identical assert on **2026-11-05 09:22**, also during a ship-deploy restart,
  before patch 0011, the Estonian/lang work and the WiFi page existed.
- **Same binary passed and failed.** The robot's vic-anim (md5 `74a2435a…`, built 2026-11-06 21:03) passed 5 ships
  earlier on 11-07. The deploys copy only `lib/libc++.so.1` every time: same content (md5 `d315fd6c…`), the build
  only refreshes its mtime. Every ship log since 11-05 shows it.
- **Rate:** 3 crashes, all on ship deploys (~23); 0 in 5 gdb deploys, 5 gdb restarts, 5 plain restarts (11-07).
- **Timing:** all three asserts fire in the same instant vic-engine reaches `engine_ready` and plays `anim_power_offon`,
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
  ⇒ Wrong (19:30): the `isinf` passes the ratio in r0, libc reads s0 = numerator; +inf runs to entry 18 =
  relocated pointers (NaN) → NaN result. Bin index 0 (11-05, 19:20) and 3 (11:54).
- **Minidumps** (`robot/work/seassert/*.dmp`, read with `robot/seassert/md.py`): `r8` = caller's table pointer =
  base + 0x37F12C (the valid 12dB table) in both. The VFP registers are not captured (all 0) and breakpad scrubs
  non-pointer stack words to `0x0defaced`, so the operand values are lost. Caller loop count left: 254 (11-07),
  257 (11-05).

## Root cause

`libsignalessence_softfp.a(nrgainv.o)` is soft-float ABI and calls hard-float libc `__isinff` unbridged (no
`__se_isinff` in `math_abi_bridge.o`), so it tests the numerator; ratio +inf overruns the table. Inferred, unseen
(dump VFP regs = 0): the trigger is den == 0 at bin 0/3, with the `.data` table intact.

## Fix (user round — no patch before the ruling)

| id | ruling | edit site |
|---|---|---|
| F1 | **rename inside the archive** (user 2026-11-07): `objcopy --redefine-sym __isinff=__se_isinff` on `nrgainv.o` | `libsignalessence_softfp.a` + `math_abi_bridge.c` in the clone |
| F1b | add `int pcs("aapcs") __se_isinff(float)` to `math_abi_bridge.c`, rebuild its `.o` into the archive | commit on clone branch `my-vector` → `robot/firmware/apply.sh --export` = `0012-*.patch` |
| F1c | `robot/firmware/series` + `markers.tsv` src row (`math_abi_bridge.c`) + bin row | `robot/firmware/` |
| F2 | _owed_ — `policy_actions.o` calls unbridged `sin` (double): same rename as F1 (`sin=__se_sin`) recommended | same patch |

Falsifier: `robot/seassert/isinff-abi.c` built unfixed exits 139 (SIGSEGV) on args `1 0` on the robot, `-DFIX` exits 0.
Ship gate: `nm vic-anim.full | grep -E ' U (__isinff|sin)$'` empty; `isinff-abi` against the patched archive (no `-DFIX`) exits 0 on `1 0`.

## Files

- `robot/seassert/isinff-abi.c`: on-robot repro (build flags in its header). `robot/seassert/catch.sh`: gdb capture loop. `robot/seassert/md.py`: ARM minidump reader. Runbook: `docs/crash-debugging.md`.
- `robot/work/seassert/`: 11-05 + 11:54 dumps, `.dmp.log`s, crash-window journal; 19:20 dump: `robot/work/crash-review/dumps/` (gitignored).
- Memory note: `vic-anim-seassert-startup-flake`.
