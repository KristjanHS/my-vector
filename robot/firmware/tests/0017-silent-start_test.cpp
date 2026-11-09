// Host test of patch 0017's my-vector silent start block (silent-start.sh cuts it from the patch into silent-start.inc,
// its flag path rewritten to MV_TEST_SILENT_FILE, and builds this).
#include <cstdio>
#include <unistd.h>
#include "silent-start.inc"

static int fails = 0, checks = 0;
#define CHECK(c) do { ++checks; if (!(c)) { printf("FAIL %s:%d %s\n", __FILE__, __LINE__, #c); ++fails; } } while (0)

int main()
{
  remove(MV_TEST_SILENT_FILE);
  CHECK(!MyVectorSilentStart(true));  // flag absent: InitialWakeUp plays

  FILE* f = fopen(MV_TEST_SILENT_FILE, "w");
  CHECK(f != nullptr);
  if (f) fclose(f);
  CHECK(MyVectorSilentStart(true));   // flag present: InitialWakeUp skipped
  CHECK(!MyVectorSilentStart(false)); // flag present, other anim: still plays
  remove(MV_TEST_SILENT_FILE);
  printf("silent-start host test: %d/%d checks pass\n", checks - fails, checks);
  return fails ? 1 : 0;
}
