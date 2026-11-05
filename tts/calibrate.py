"""Measure each voice's loudness on a reference paragraph (ffmpeg ebur128) and write gains.json for -14 LUFS (R23; the limiter catches the peaks).

Run with the service venv + the env setup.sh uses: TTS_PIPER_DIR=... venv/bin/python -I tts/calibrate.py
"""

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import server  # noqa: E402

# R23: 6 dB over gen.sh's -20 loudnorm (the listen-test level), which played too quiet on the robot
TARGET_LUFS = -14.0
REFERENCE = (
    "Good morning! It's a little cloudy outside, with a high of twelve degrees. "
    "Would you like me to set a timer, or tell you something interesting about octopuses? "
    "They have three hearts, and their blood is blue."
)


def lufs(pcm):
    ffmpeg = shutil.which("ffmpeg") or "/opt/ffmpeg/ffmpeg"
    with tempfile.NamedTemporaryFile(suffix=".pcm") as f:
        f.write(pcm)
        f.flush()
        cmd = [
            ffmpeg,
            "-nostdin",
            "-hide_banner",
            "-f",
            "s16le",
            "-ar",
            str(server.RATE),
            "-ac",
            "1",
            "-i",
            f.name,
        ]
        out = subprocess.run(
            [*cmd, "-af", "ebur128", "-f", "null", "-"],
            capture_output=True,
            text=True,
            check=True,
        )
    m = re.findall(r"I:\s+(-?[\d.]+) LUFS", out.stderr)
    if not m:
        raise RuntimeError("no integrated loudness in ffmpeg output")
    return float(m[-1])  # the last I: line is the summary


def main():
    piper_dir = os.environ["TTS_PIPER_DIR"]
    kokoro = server.KokoroEngine(os.environ.get("TTS_DEVICE", "cuda"))
    synth = server.Synth(
        {"piper": server.PiperEngine(piper_dir), "kokoro": kokoro},
        {v: 1.0 for v in server.VOICES},
    )
    gains = {}
    for voice in server.VOICES:
        level = lufs(b"".join(synth.stream(voice, REFERENCE, 1.0)))
        gains[voice] = {
            "gain_db": round(TARGET_LUFS - level, 1),
            "measured_lufs": round(level, 1),
        }
        print(f"{voice}: {level:.1f} LUFS -> {gains[voice]['gain_db']:+.1f} dB")
    server.GAINS_FILE.write_text(json.dumps(gains, indent=2) + "\n")


if __name__ == "__main__":
    main()
