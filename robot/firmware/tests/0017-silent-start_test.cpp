// Host test of patch 0017's my-vector silent start block (silent-start.sh cuts it from the patch into silent-start.inc,
// its flag path rewritten to MV_TEST_SILENT_FILE, and builds this). now is injected: the flag holds an expiry epoch.
#include <cctype>
#include <cerrno>
#include <cstdio>
#include <cstdlib>
#include "silent-start.inc"

static int fails = 0, checks = 0;
#define CHECK(c) do { ++checks; if (!(c)) { printf("FAIL %s:%d %s\n", __FILE__, __LINE__, #c); ++fails; } } while (0)

static const long long kNow = 1800000000;

// put("...") writes the flag with exactly that content; skip(now) = the decision for an InitialWakeUp sequence.
static void put(const char* content)
{
  FILE* f = fopen(MV_TEST_SILENT_FILE, "w");
  CHECK(f != nullptr);
  if (f) { fputs(content, f); fclose(f); }
}
static bool skip(long long now_s, bool hasInitialWakeUp = true)
{
  long long until_s = 0;
  return MyVectorSilentStart(hasInitialWakeUp, now_s, until_s);
}

int main()
{
  char line[64];
  remove(MV_TEST_SILENT_FILE);
  CHECK(!skip(kNow));                    // flag absent: InitialWakeUp plays

  snprintf(line, sizeof line, "%lld\n", kNow + 300); // what restarts.sh / catch.sh / ship.sh write (echo: trailing newline)
  put(line);
  CHECK(skip(kNow));                     // valid expiry ahead: skipped
  CHECK(!skip(kNow, false));             // valid, other anim: still plays
  CHECK(!skip(kNow + 300));              // now == expiry: expired, plays
  CHECK(!skip(kNow + 301));              // past the expiry: plays

  snprintf(line, sizeof line, "%lld", kNow + kMyVectorSilentStartMax_s);
  put(line);
  CHECK(skip(kNow));                     // exactly the cap ahead: still honoured
  snprintf(line, sizeof line, "%lld\n", kNow + kMyVectorSilentStartMax_s + 1);
  put(line);
  CHECK(!skip(kNow));                    // beyond the cap: plays
  put("1800000300000\n");                // a ms epoch: far beyond the cap, plays
  CHECK(!skip(kNow));

  put("");                               // empty (the old touch flag): plays
  CHECK(!skip(kNow));
  put("garbage\n");                      // not a number: plays
  CHECK(!skip(kNow));
  snprintf(line, sizeof line, "%lldx\n", kNow + 300); // number then junk: plays
  put(line);
  CHECK(!skip(kNow));
  put("99999999999999999999999\n");      // overflows long long: plays
  CHECK(!skip(kNow));

  remove(MV_TEST_SILENT_FILE);
  printf("silent-start host test: %d/%d checks pass\n", checks - fails, checks);
  return fails ? 1 : 0;
}
