#!/usr/bin/env python3
# Minimal ARM minidump reader: exception thread registers (core + VFP), module bases, and memory at given addresses.
# Usage: md.py <file.dmp> [module-relative-hex-addr:len ...]   (dumps: robot/work/seassert/)
import struct
import sys

d = open(sys.argv[1], "rb").read()
assert d[:4] == b"MDMP"
nstreams, dir_rva = struct.unpack_from("<II", d, 8)
streams = {}
for i in range(nstreams):
    t, size, rva = struct.unpack_from("<III", d, dir_rva + 12 * i)
    streams.setdefault(t, []).append((size, rva))


def mdstr(rva):
    n = struct.unpack_from("<I", d, rva)[0]
    return d[rva + 4 : rva + 4 + n].decode("utf-16-le")


mods = []
size, rva = streams[4][0]
n = struct.unpack_from("<I", d, rva)[0]
for i in range(n):
    o = rva + 4 + 108 * i
    base, msize = struct.unpack_from("<QI", d, o)
    name = mdstr(struct.unpack_from("<I", d, o + 20)[0])
    mods.append((base, msize, name))
anim = [m for m in mods if m[2].endswith("vic-anim")]
abase = anim[0][0] if anim else 0
print(f"vic-anim base 0x{abase:x}")

mem = []
for t in (5,):
    for size, rva in streams.get(t, []):
        n = struct.unpack_from("<I", d, rva)[0]
        for i in range(n):
            start, msz, mrva = struct.unpack_from("<QII", d, rva + 4 + 16 * i)
            mem.append((start, msz, mrva))


def rd(addr, ln):
    for s, z, r in mem:
        if s <= addr and addr + ln <= s + z:
            return d[r + addr - s : r + addr - s + ln]
    return None


def where(a):
    for b, z, nm in mods:
        if b <= a < b + z:
            return f"{nm.split('/')[-1]}+0x{a - b:x}"
    return ""


size, rva = streams[6][0]
tid = struct.unpack_from("<I", d, rva)[0]
code, flags = struct.unpack_from("<II", d, rva + 8)
addr = struct.unpack_from("<Q", d, rva + 24)[0]
csize, crva = struct.unpack_from("<II", d, rva + 8 + 152)
print(f"exception thread {tid} code {code} addr 0x{addr:x} {where(addr)} ctx {csize}B")
cf = struct.unpack_from("<I", d, crva)[0]
r = struct.unpack_from("<16I", d, crva + 4)
cpsr, fpscr = struct.unpack_from("<IQ", d, crva + 68)
fr = struct.unpack_from("<32Q", d, crva + 80)
for i in range(16):
    print(f"  r{i:<2} = 0x{r[i]:08x} {where(r[i])}")
print(f"  cpsr 0x{cpsr:x} fpscr 0x{fpscr:x}")
s = []
for q in fr[:16]:
    lo, hi = q & 0xFFFFFFFF, q >> 32
    s += [
        struct.unpack("<f", struct.pack("<I", lo))[0],
        struct.unpack("<f", struct.pack("<I", hi))[0],
    ]
for i in range(0, 32, 4):
    print("  " + "  ".join(f"s{j:<2}={s[j]:<14.7g}" for j in range(i, i + 4)))
print(f"memory regions captured: {len(mem)}")
sp = r[13]
st = rd(sp, 0x80)
if st:
    for k in range(0, 0x80, 4):
        u = struct.unpack_from("<I", st, k)[0]
        f = struct.unpack_from("<f", st, k)[0]
        print(f"  sp+0x{k:02x} [0x{sp + k:08x}] 0x{u:08x} f={f:<12.6g} {where(u)}")
for spec in sys.argv[2:]:
    a, ln = spec.split(":")
    a = int(a, 16)
    ln = int(ln)
    for label, va in (("abs", a), ("anim+", abase + a)):
        b = rd(va, ln)
        if b:
            fl = struct.unpack(f"<{ln // 4}f", b)
            print(f"{label} 0x{va:x}: " + " ".join(f"{x:.4g}" for x in fl))
            break
    else:
        print(f"0x{a:x}: not captured")
