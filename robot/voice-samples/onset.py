"""Seconds from capture start to the first 10 ms window above -40 dBFS, per 16-bit mono WAV ('-' if silent)."""

import math
import struct
import sys
import wave

for f in sys.argv[1:]:
    w = wave.open(f)
    sr = w.getframerate()
    d = w.readframes(w.getnframes())
    s = struct.unpack("<%dh" % (len(d) // 2), d)
    win = sr // 100
    on = "-"
    for i in range(0, len(s) - win, win):
        if math.sqrt(sum(x * x for x in s[i : i + win]) / win) / 32768 > 10 ** (
            -40 / 20
        ):
            on = "%.3f" % (i / sr)
            break
    print(f.rsplit("/", 1)[-1], on)
