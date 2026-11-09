"""Score clips like the service does: nospeech.py [-l en-US|et-EE] <clip.ogg|clip.pcm (16 kHz s16le)>... -> no_speech<TAB>text<TAB>clip.

CPU by default (STT_DEVICE=cuda to share the GPU with the running service); exit 1 when any clip scores >= --max (default: none).
"""

import argparse
import os
import sys
from pathlib import Path

import numpy as np
import server


def load(path):
    if path.suffix == ".pcm":
        return (
            np.frombuffer(path.read_bytes(), dtype="<i2").astype(np.float32) / 32768.0
        )
    from faster_whisper import decode_audio

    return decode_audio(str(path), sampling_rate=server.RATE)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "-l", "--lang", default=server.ESTONIAN, choices=sorted(server.WHISPER_LANG)
    )
    ap.add_argument(
        "--max", type=float, help="fail when a clip's no_speech reaches this"
    )
    ap.add_argument("clips", nargs="+", type=Path)
    a = ap.parse_args()
    from faster_whisper import WhisperModel

    model = {
        server.ESTONIAN: os.environ.get("STT_MODEL", server.MODEL),
        server.ENGLISH: os.environ.get("STT_MODEL_EN", server.MODEL_EN),
    }[a.lang]
    device = os.environ.get("STT_DEVICE", "cpu")
    m = WhisperModel(
        str(model),
        device=device,
        compute_type="int8" if device == "cpu" else "int8_float16",
    )
    bad = 0
    for clip in a.clips:
        segments, _ = m.transcribe(
            load(clip), language=server.WHISPER_LANG[a.lang], beam_size=server.BEAM
        )
        text, p = server.heard(segments)
        print(f"{p:.3f}\t{text}\t{clip}")
        bad += a.max is not None and p >= a.max
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
