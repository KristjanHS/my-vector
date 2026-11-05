# pyright: reportMissingImports=false
"""Synthesize one text with one engine: synth.py <engine> <outdir> <slow> <voice>... (text in $TXT).

engine piper: voice = model path (.onnx) · edge: en-US-AriaNeural · kokoro: af_heart.
slow > 1 speaks slower (Piper length_scale, edge rate, Kokoro 1/speed).
"""

import asyncio
import os
import sys

engine, outdir, slow, voices = (
    sys.argv[1],
    sys.argv[2],
    float(sys.argv[3]),
    sys.argv[4:],
)
text = os.environ["TXT"]


def name(v):
    return f"{engine}-{os.path.basename(v).removesuffix('.onnx')}"


if engine == "piper":
    import wave
    from piper import PiperVoice, SynthesisConfig

    for v in voices:
        with wave.open(f"{outdir}/{name(v)}.wav", "wb") as w:
            PiperVoice.load(v).synthesize_wav(
                text, w, syn_config=SynthesisConfig(length_scale=slow)
            )
elif engine == "edge":
    import edge_tts

    rate = f"{round(100 / slow - 100):+d}%"
    for v in voices:
        asyncio.run(
            edge_tts.Communicate(text, v, rate=rate).save(f"{outdir}/{name(v)}.mp3")
        )
elif engine == "kokoro":
    import numpy as np
    import soundfile as sf
    import torch
    from kokoro import KModel, KPipeline

    model = (
        KModel(repo_id="hexgrad/Kokoro-82M")
        .to("cuda" if torch.cuda.is_available() else "cpu")
        .eval()
    )
    for v in voices:
        pipe = KPipeline(lang_code=v[0], repo_id="hexgrad/Kokoro-82M", model=model)
        audio = np.concatenate(
            [r.audio.cpu().numpy() for r in pipe(text, voice=v, speed=1 / slow)]
        )
        sf.write(f"{outdir}/{name(v)}.wav", audio, 24000)
else:
    sys.exit(f"unknown engine {engine}")
