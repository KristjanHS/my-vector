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
  printf("nap host test: %d/%d checks pass\n", checks - fails, checks);
  return fails ? 1 : 0;
}
