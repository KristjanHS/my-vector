"""STT misses of "Kell on" (and hour-minute fillers) in mari's clock line per Piper noise level → server.PIPER_NOISE (needs myvector-stt, et-EE):
robot/work/tts/venv/bin/python -I tts/noise_sweep.py [takes=20]"""

import json
import os
import re
import sys
import urllib.request
from pathlib import Path

import numpy as np
import soxr

sys.path.insert(0, str(Path(__file__).parent))
import server  # noqa: E402
from et_text import normalize  # noqa: E402

VOICE = server.VOICES["mari"][1]
LEVELS = [(0.667, 0.8), (0.4, 0.6), (0.2, 0.4)]  # the model default first
TIMES = ["20:47", "12:00", "08:05", "09:26"]  # 09:26: the "kuni" filler case (bdea4c0)
FILLER = re.compile(r"\b(kuni|või)\b", re.I)
STT = os.environ.get("STT_URL", "http://127.0.0.1:8092")


def heard(pcm):
    req = urllib.request.Request(
        f"{STT}/stt?lang=et-EE",
        data=pcm,
        headers={"Content-Type": "application/octet-stream"},
    )
    return json.load(urllib.request.urlopen(req, timeout=120)).get("text", "")


def main():
    takes = int(sys.argv[1]) if len(sys.argv) > 1 else 20
    root = Path(__file__).resolve().parents[1]
    eng = server.PiperEngine(
        os.environ.get("TTS_PIPER_DIR", root / "robot/work/voice-samples/piper-voices")
    )
    for level in LEVELS:
        server.PIPER_NOISE[VOICE] = level
        for t in TIMES:
            text, misses, fillers = normalize(f"Kell on {t}."), [], []
            for _ in range(takes):
                audio, rate = eng.synth(VOICE, text, 1.0)
                audio = soxr.resample(audio, rate, server.RATE, quality="HQ")
                got = heard((np.clip(audio, -1, 1) * 32767).astype("<i2").tobytes())
                if not got.startswith("Kell on "):
                    misses.append(got)
                if FILLER.search(got):  # the hour-minute seam filler that et_text._time's words removed
                    fillers.append(got)
            print(
                f"noise {level[0]}/{level[1]}  {t}  miss {len(misses)}/{takes}  filler {len(fillers)}/{takes}  {[*fillers[:1], *(m for m in misses if m not in fillers)][:2]}",
                flush=True,
            )


if __name__ == "__main__":
    main()
