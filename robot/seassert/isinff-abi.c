// Repro for vic-anim "SeAssert: se_lib/nrgainv.c:191": calls the prebuilt se_lib's DoNrGainLookup_flt32 on one bin.
// Usage (on the robot): isinff-abi <numerator> <denominator>   e.g. `1 1` passes, `1 0` (ratio +inf) asserts.
// se_lib is soft-float ABI (float args in core registers); everything else here is hard-float, so its entry points
// are declared pcs("aapcs"). -DFIX -Wl,--wrap=__isinff supplies the soft-float __isinff bridge the archive lacks.
// Build: $SDK/prebuilt/bin/arm-oe-linux-gnueabi-clang --sysroot=$SDK/sysroot -O1 -mcpu=cortex-a7 -mfloat-abi=hard
//   -mfpu=neon-vfpv4 -Wl,--allow-multiple-definition,--no-warn-mismatch,-z,now isinff-abi.c <softfp.a> -lm
// (SDK = robot/work/wire-os-victor/anki-deps/vicos-sdk/dist/6.0.0-r08; flags = vic-anim's link line.)
#include <math.h>
#include <stdio.h>
#include <stdlib.h>

#define SOFTFP __attribute__((pcs("aapcs")))
SOFTFP void NrGainLookupInit(void *lookup, int table);
SOFTFP void DoNrGainLookup_flt32(void *lookup, float *num, float *den, int n, float floor, float *out);

#ifdef FIX
SOFTFP int __wrap___isinff(float x) { return isinf(x) ? (x > 0 ? 1 : -1) : 0; }
#endif

int main(int argc, char **argv) {
  if (argc != 3) { fprintf(stderr, "usage: %s <numerator> <denominator>\n", argv[0]); return 2; }
  static long lookup[16];  // NrGainLookup_t; Init stores only the table pointer in word 0
  float num = strtof(argv[1], NULL), den = strtof(argv[2], NULL), out = -1;
  NrGainLookupInit(lookup, 2);  // same table the crash used (GainLookupTable_Range12dB)
  DoNrGainLookup_flt32(lookup, &num, &den, 1, 0.1f, &out);
  printf("ok: ratio %g -> gain %g\n", num / den, out);
  return 0;
}
