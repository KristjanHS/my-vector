// Host test of patch 0011's my-vector nap block (nap.sh cuts it from the patch into nap.inc and builds this).
#include <cstdio>
#include "nap.inc"

static int fails = 0, checks = 0;
#define CHECK(c) do { ++checks; if (!(c)) { printf("FAIL %s:%d %s\n", __FILE__, __LINE__, #c); ++fails; } } while (0)

int main()
{
  const long long now = 1793000000; // 2026-10, epoch seconds
  CHECK(!MyVectorNapActive(0, now));                   // 0 = off
  CHECK(MyVectorNapActive(now + 60 * 60, now));        // 1 h nap
  CHECK(MyVectorNapActive(now + 4 * 60 * 60, now));    // NapMax itself is allowed
  CHECK(!MyVectorNapActive(now, now));                 // ends at its epoch
  CHECK(!MyVectorNapActive(now - 1, now));             // past
  CHECK(!MyVectorNapActive(now + 5 * 60 * 60, now));   // over NapMax: refused
  CHECK(!MyVectorNapActive(now * 1000 + 3600000, now)); // ms epoch: refused

  // MyVectorNapStep: the latch over the var's changes, and the log line each change owes.
  MyVectorNapLatch latch;
  MyVectorNapLog log = kMyVectorNapUntil;
  CHECK(!MyVectorNapStep(latch, 0, now, log) && log == kMyVectorNapQuiet); // boot: var 0, nothing to log
  const long long far = now + 5 * 60 * 60;
  CHECK(!MyVectorNapStep(latch, far, now, log) && log == kMyVectorNapRefused);
  CHECK(!MyVectorNapStep(latch, far, now + 1, log) && log == kMyVectorNapQuiet); // same value: logged once
  CHECK(!MyVectorNapStep(latch, far, now + 2 * 60 * 60, log));               // refused, then within NapMax: stays refused
  const long long soon = now + 2 * 60 * 60 + 60 * 60;
  CHECK(MyVectorNapStep(latch, soon, now + 2 * 60 * 60, log) && log == kMyVectorNapUntil); // a new value is judged anew
  CHECK(MyVectorNapStep(latch, soon, now + 2 * 60 * 60 + 1, log) && log == kMyVectorNapQuiet);
  CHECK(!MyVectorNapStep(latch, soon, soon, log) && log == kMyVectorNapQuiet); // ends at its epoch, no line
  CHECK(!MyVectorNapStep(latch, 0, soon + 1, log) && log == kMyVectorNapOff);   // set to 0 (make unquiet): nap off
  CHECK(MyVectorNapStep(latch, soon + 60, soon + 1, log) && log == kMyVectorNapUntil);
  CHECK(!MyVectorNapStep(latch, 0, soon + 2, log) && log == kMyVectorNapOff);   // ended early: nap off
  CHECK(!MyVectorNapStep(latch, now - 1, soon + 3, log) && log == kMyVectorNapQuiet); // a past epoch: no line
  printf("nap host test: %d/%d checks pass\n", checks - fails, checks);
  return fails ? 1 : 0;
}
