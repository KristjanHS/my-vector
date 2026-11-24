# How it was built

Each change here was planned, built and reviewed with a coding agent; these trails follow a few of them from plan to commits to review to archive, with the diff that mattered quoted from the commit.

## Hard debugging: the crash the agent blamed on memory corruption

**Plan:** [vic-anim SeAssert hunt](plans/archive/2026-11-07-vic-anim-seassert.md) ·
**Commits:** [52759a3](https://github.com/KristjanHS/my-vector/commit/52759a3) hunt →
[09da0bb](https://github.com/KristjanHS/my-vector/commit/09da0bb) archived, no repro →
[323f20b](https://github.com/KristjanHS/my-vector/commit/323f20b) root cause, plan reopened →
[97812eb](https://github.com/KristjanHS/my-vector/commit/97812eb) fix ·
**Review fix:** [63bc3aa](https://github.com/KristjanHS/my-vector/commit/63bc3aa) ·
**Archive:** [2026-11-07-vic-anim-seassert.md](plans/archive/2026-11-07-vic-anim-seassert.md)

**What the agent did.** After a firmware ship, the robot's animation process (`vic-anim`) died about 13 seconds
after a restart on an assert inside a closed-source audio library (`se_lib/nrgainv.c:191`), then recovered. I could
have let the smoke test tolerate that one signature; I chose to dig. The agent read the disassembly and the
minidumps and concluded that with a valid gain table and a valid floor the assert could not fire. So either the
table, which sits in writable `.data`, was overwritten at runtime, or a VFP register was corrupted. It wrote a gdb
capture plan around those two branches, with a stopping rule of "stop at the first hit". Ten gdb rounds and five
plain restarts never reproduced the crash, so the plan was archived as "no repro".

**What was wrong.** Both branches. When the crash came back, the agent re-read the call site. The library is built
for the soft-float ABI but called the hard-float libc `__isinff` with no bridge between the two. Soft-float passes
the argument in a core register (`r0`); libc reads a VFP register (`s0`), which held the numerator instead of the
ratio. So the infinity check missed `+inf`, the lookup ran past the 18-entry table into relocated pointers, and the
NaN gain tripped the assert. The agent reopened the plan, struck its own hypothesis and the two capture branches,
and wrote the root cause in their place
([323f20b](https://github.com/KristjanHS/my-vector/commit/323f20b)):

```diff
-  ⇒ With a valid table and floor, line 191 **cannot** fail. So either the table memory was overwritten at runtime,
-  or a VFP register value (floor in d8) got corrupted.
+  ⇒ Wrong (19:30): the `isinf` passes the ratio in r0, libc reads s0 = numerator; +inf runs to entry 18 =
+  relocated pointers (NaN) → NaN result. Bin index 0 (11-05, 19:20) and 3 (11:54).
```

```diff
-   - table ≠ file values → **runtime memory corruption** of `.data` → find the writer (watchpoint on the table
-     with the same gdb tooling).
-   - table intact but floor/d8 out of range or NaN → **register/VFP-state corruption**, or the caller passing an
-     unchecked floor (re-read the caller).
```

```diff
+`libsignalessence_softfp.a(nrgainv.o)` is soft-float ABI and calls hard-float libc `__isinff` unbridged (no
+`__se_isinff` in `math_abi_bridge.o`), so it tests the numerator; ratio +inf (bin den = 0) overruns the table.
```

The dumps had no VFP registers, so this was still an inference. A small on-robot repro settled it: built against
the unpatched library it crashed on a `+inf` ratio, and with the fix it returned normally.

**What I decided.** The fix was my call, not the agent's. Instead of wrapping the function at link time, I chose to
rename the symbol inside the vendored archive and add a matching wrapper with the right calling convention. While
checking, the agent found a second object in the same library calling `sin` the same broken way; I ruled it gets
the same treatment. I also moved the ship gate from the linked binary to the archive, because a linked binary
keeps an undefined `sin` reference whenever anything calls libm's `sin`, the new wrapper included. The firmware
patch adds the two wrappers to the library's bridge file
([97812eb](https://github.com/KristjanHS/my-vector/commit/97812eb), `robot/firmware/0012-se-isinff-bridge.patch`):

```diff
+@@ -22,3 +22,8 @@ double __attribute__((pcs("aapcs"))) __log10_finite(double x)           { return
+ double __attribute__((pcs("aapcs"))) __log_finite(double x)             { return log(x); }
+ double __attribute__((pcs("aapcs"))) __atan2_finite(double y, double x) { return atan2(y, x); }
+ double __attribute__((pcs("aapcs"))) __fmod_finite(double x, double y)  { return fmod(x, y); }
++
++// my-vector: the two libm calls se_lib made without a bridge (objcopy --redefine-sym in the archive).
++// nrgainv.o's isinf test on the gain-table ratio; glibc __isinff reads s0, so +inf ran off the table (nrgainv.c:191).
++int __attribute__((pcs("aapcs"))) __se_isinff(float x)              { return isinf(x) ? (x > 0 ? 1 : -1) : 0; }
++double __attribute__((pcs("aapcs"))) __se_sin(double x)             { return sin(x); }
```

**Review fix.** The fix also added a static check (S6) that fails the build when the archive calls a libm
function without a bridge. The review found that S6 matched function names with a hand-written regex, which missed
the `__*_finite` variants, the `l` suffix, `fmax`/`copysign` and weak references. The check now takes its list
from the SDK's own libm exports plus libc's float classifiers, and it is red on the unpatched archive (`nrgainv.o:
__isinff`, `policy_actions.o: sin`) and green with the patch
([63bc3aa](https://github.com/KristjanHS/my-vector/commit/63bc3aa), `robot/firmware/check-static.sh`):

```diff
@@ -113,16 +113,33 @@ echo "src markers OK ($n)"
 
 # S6: the soft-float se_lib archive calls hard-float libm only through math_abi_bridge.o's pcs("aapcs") wrappers
 # (a direct call passes floats in core registers; libm reads VFP ones: the vic-anim nrgainv.c:191 SeAssert).
+# Float functions = the SDK's libm exports + libc's float classifiers (glibc keeps __isinff & co. in libc).
+# Flagged: an undefined ref (U or w) to one of them, outside the bridge, that no archive member defines itself.
 se_a=3rd/signalEssence/v009/vicos-highres/platform/anki_victor_example/build/libsignalessence_softfp.a
-math_re='(a?sin|a?cos|a?tan|atan2|sinh|cosh|tanh|sincos|exp|exp2|expm1|log|log2|log10|log1p|pow|sqrt|cbrt|hypot|ceil|floor|round|lround|trunc|rint|lrint|fmod|fabs|ldexp|frexp|modf|__isinf|__isnan|__finite|__fpclassify|__signbit)f?'
```

```diff
+  float_fns=$({ dyn "$sysroot/libm.so.6"
+    dyn "$sysroot/libc.so.6" | grep -E '^(__)?(isinf|isnan|finite|frexp|ldexp|modf|scalbn|copysign|signbit|fpclassify)[fl]?$' || true
+  } | sort -u)
+  grep -qx sin <<<"$float_fns" && grep -qx __isinff <<<"$float_fns" || die "S6: cannot read the SDK libm/libc exports (the check would pass vacuously)"
```

The same commit trimmed the plan to its rulings and archived it.

## Design rejected: the agent's winning opener credited the wrong source

**Plan:** [search news quality](plans/archive/2026-11-11-search-news-quality.md) ·
**Commits:** [1de75bd](https://github.com/KristjanHS/my-vector/commit/1de75bd) opener + replay arms →
[65d96a0](https://github.com/KristjanHS/my-vector/commit/65d96a0) news-ask timeout and fallback (ruling Q16) ·
**Review fixes:** [c73ba1b](https://github.com/KristjanHS/my-vector/commit/c73ba1b),
[bafcb6b](https://github.com/KristjanHS/my-vector/commit/bafcb6b) ·
**Archive:** [2026-11-11-search-news-quality.md](plans/archive/2026-11-11-search-news-quality.md)

**What the agent did.** On Estonian news questions, the local model sometimes said it could not search, even
with fresh results in its prompt. The agent's fix was to prefill the first words of the reply. Its first replay
compared a source-naming opener (`<first host> andmetel`, "according to (source)") with a generic one
("Otsingu järgi", "according to the search"), eight runs each. By the decision rule set for that replay, the
source arm won: no refusals and no markdown lists. The agent noted one known cost and moved on to building it: on
a question whose answer drew on several outlets, the replies credited items from other outlets to the
first one.

**What I decided.** A news answer that credits the wrong outlet is not acceptable, however well it does on
refusals. Before anything was built, I asked for a second replay with two new arms: one that credits every source
("ERRi, Reutersi ja Äripäeva andmetel", "according to ERR, Reuters and Äripäev") and a sentence-shaped generic one
("Värskete uudiste järgi", "according to the latest news"). Among the arms that rarely refused, the new rule
ranked by wrong credit plus made-up facts first, then by lists. Wrong credit and made-up facts were judged by hand
against the search results; each cell counts replies out of eight (from the plan):

| arm | strict | lists | wrong credit | made-up | wc+mf | names source |
|---|---|---|---|---|---|---|
| source | 0 | 0 | 5 | 2 | 7 | 8 |
| generic "Otsingu järgi" | 0 | 6 | 1 | 2 | 3 | 5 |
| allsrc "ERRi, Reutersi ja Äripäeva andmetel" | 0 | 1 | 1 | 3 | 4 | 8 |
| sentgen "Värskete uudiste järgi" | 0 | 0 | 0 | 2 | 2 | 0 |

The sentence-shaped opener won: 2, against 3 for the generic and 4 for all sources. The agent flagged that the
margin is fragile (one hand call flips it) and that this opener never names a source. Crediting every source needs
a rule for Estonian genitive forms of host names, and the simple one got half of the hosts wrong, which counted
against the all-sources arm as well. The opener that
shipped ([1de75bd](https://github.com/KristjanHS/my-vector/commit/1de75bd),
`chipper/pkg/wirepod/ttr/fork_prefill.go`):

```diff
+// forkNewsOpener is the OllamaEt reply's prefilled opener on a news ask with web results (search-news-quality Q9/Q10).
+const forkNewsOpener = "Värskete uudiste järgi"
+
+// forkPrefill holds esn's opener for its ask, set by forkAIReq after the search and taken by forkLLMStream.
+var forkPrefill sync.Map // esn -> forkCannedReply{ask, opener}
+
+// forkPrefillSet gives an OllamaEt news ask with a web block (not Unavailable) its opener; any other ask clears esn's.
+func forkPrefillSet(esn, last, l, brain, block string, prev []string) {
+	if brain == prompt.OllamaEt && block != "" && block != search.Unavailable(l) && search.NewsAsk(last, prev, l) {
+		forkPrefill.Store(esn, forkCannedReply{last, forkNewsOpener})
+		return
+	}
+	forkPrefill.Delete(esn)
+}
```

**Review fixes.** The first review
([c73ba1b](https://github.com/KristjanHS/my-vector/commit/c73ba1b)) found that the benchmark's query arms
skipped the relevance gate production runs, so they scored results the robot would have thrown away; the arms now
score what the gate keeps. The second review
([bafcb6b](https://github.com/KristjanHS/my-vector/commit/bafcb6b)), after news asks got a longer search deadline
and a keyword fallback, fixed a data race in a test, restored the check that every news query carries the topic,
and pinned the news provider to a single call in the fallback test. It also found a gap in the opener itself: when
the news provider is late and only undated fallback hits arrive, the reply still opens with "according to the latest news". That went to the
backlog as an open item rather than into a quick patch, and the plan was archived in the same commit.
