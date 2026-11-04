"""dBFS summary of 16-bit mono capture WAVs: peak, loudest 50 ms, rms of windows above -40 dBFS.
--ticks: count of seconds whose peak exceeds -45 dBFS, plus the median per-second peak."""
import math, struct, sys, wave

def db(v):
    return 20 * math.log10(v) if v > 0 else -99.0

def samples(path):
    w = wave.open(path)
    d = w.readframes(w.getnframes())
    return w.getframerate(), [x / 32768 for x in struct.unpack('<%dh' % (len(d) // 2), d)]

ticks = sys.argv[1:2] == ['--ticks']
for f in sys.argv[1 + ticks:]:
    sr, s = samples(f)
    name = f.rsplit('/', 1)[-1]
    if ticks:
        pk = sorted(max(map(abs, s[i * sr:(i + 1) * sr])) for i in range(len(s) // sr)) or [0]
        print('%-40s %3ds  ticks>-45 %3d  max %6.1f  median %6.1f' % (
            name, len(s) // sr, sum(p > 10 ** (-45 / 20) for p in pk), db(pk[-1]), db(pk[len(pk) // 2])))
        continue
    win = int(sr * 0.05)
    wins = [math.sqrt(sum(x * x for x in s[i:i + win]) / win) for i in range(0, len(s) - win, win)] or [0]
    act = [v for v in wins if v > 10 ** (-40 / 20)]
    rms = math.sqrt(sum(v * v for v in act) / len(act)) if act else 0
    print('%-55s peak %6.1f  loud50ms %6.1f  active %6.1f' % (name, db(max(map(abs, s), default=0)), db(max(wins)), db(rms)))
