# pyright: reportMissingImports=false, reportRedeclaration=false
"""Synthesize one text with one engine: synth.py <engine> <outdir> <slow> <voice>... (text in $TXT).

engine piper: voice = model path (.onnx), @Speaker for multi-speaker · edge: en-US-AriaNeural · kokoro: af_heart
· tartu (TartuNLP Neurokõne API): mari · tartulocal (same, local worker in $TARTU_DIR): mari
· xtts (TartuNLP XTTS-v2-est in $XTTS_DIR): reference clip .wav · azure ($AZURE_SPEECH_KEY/_REGION): et-EE-AnuNeural
· espeak (espeak-ng unpacked in $ESPEAK_ROOT): et.
slow > 1 speaks slower (Piper length_scale, edge/azure rate, Kokoro/TartuNLP/XTTS 1/speed, espeak wpm).
$DEVICE=cpu|cuda forces the torch device (kokoro, xtts). $BENCH=1: one warm-up per voice, then stderr
"bench <name> first=<s> total=<s> torch_gb=<peak>" (first = first audio bytes for streaming engines, else = total),
also appended to $BENCH_LOG (if set) as "<UTC time> <engine>:<voice> device=<$DEVICE|auto> first=.. total=.. torch_gb=..".
"""

import asyncio
import json
import os
import re
import subprocess
import sys
import time
import urllib.request

engine, outdir, slow, voices = (
    sys.argv[1],
    sys.argv[2],
    float(sys.argv[3]),
    sys.argv[4:],
)
text = os.environ["TXT"].strip() or sys.exit("empty $TXT")
bench = os.environ.get("BENCH") == "1"


def name(v):
    base = re.sub(r"\.(onnx|wav)", "", os.path.basename(v))
    return f"{engine}-{base.replace('@', '-')}"


def device():
    import torch

    return os.environ.get("DEVICE") or ("cuda" if torch.cuda.is_available() else "cpu")


def fetch(
    req, out
):  # stream an HTTP response to out; returns seconds to its first bytes
    t = time.perf_counter()
    with urllib.request.urlopen(req, timeout=60) as r, open(out, "wb") as f:
        chunk = r.read(4096)
        first = time.perf_counter() - t
        while chunk:
            f.write(chunk)
            chunk = r.read(65536)
    return first


# say(voice, out_base) writes out_base.<ext>; returns seconds to first audio when streaming, else None.
if engine == "piper":
    import wave
    from piper import PiperVoice, SynthesisConfig

    loaded = {}

    def say(v, out):
        path, _, spk = v.partition("@")
        pv = loaded.get(path) or loaded.setdefault(path, PiperVoice.load(path))
        sid = pv.config.speaker_id_map[spk] if spk else None
        with wave.open(f"{out}.wav", "wb") as w:
            pv.synthesize_wav(
                text, w, syn_config=SynthesisConfig(speaker_id=sid, length_scale=slow)
            )
elif engine == "edge":
    import edge_tts

    rate = f"{round(100 / slow - 100):+d}%"

    async def stream(v, out):
        t, first = time.perf_counter(), None
        with open(f"{out}.mp3", "wb") as f:
            async for c in edge_tts.Communicate(text, v, rate=rate).stream():
                if c["type"] == "audio":
                    first = first or time.perf_counter() - t
                    f.write(c["data"])
        return first

    def say(v, out):
        return asyncio.run(stream(v, out))
elif engine == "kokoro":
    import numpy as np
    import soundfile as sf
    from kokoro import KModel, KPipeline

    model = KModel(repo_id="hexgrad/Kokoro-82M").to(device()).eval()

    def say(v, out):
        pipe = KPipeline(lang_code=v[0], repo_id="hexgrad/Kokoro-82M", model=model)
        audio = np.concatenate(
            [r.audio.cpu().numpy() for r in pipe(text, voice=v, speed=1 / slow)]
        )
        sf.write(f"{out}.wav", audio, 24000)
elif engine == "tartu":

    def say(v, out):
        body = json.dumps({"text": text, "speaker": v, "speed": 1 / slow}).encode()
        req = urllib.request.Request(
            "https://api.tartunlp.ai/text-to-speech/v2",
            body,
            {"Content-Type": "application/json"},
        )
        return fetch(req, f"{out}.wav")
elif (
    engine == "tartulocal"
):  # acoustic model: TF 2.13 (no CUDA 11.8 here → CPU); HiFiGAN vocoder: $DEVICE
    d = os.environ["TARTU_DIR"]
    sys.path.insert(0, d)
    from speechbrain.inference.vocoders import HIFIGAN
    from tts_worker.config import read_model_config
    from tts_worker.synthesizer import Synthesizer

    dev, fh, dec = device(), HIFIGAN.from_hparams, HIFIGAN.decode_batch
    HIFIGAN.from_hparams = lambda **k: fh(**k, run_opts={"device": dev})
    HIFIGAN.decode_batch = lambda s, m: dec(s, m).cpu()  # worker concatenates numpy
    cfg = read_model_config(f"{d}/config/config.yaml", "multispeaker")
    cfg.model_path = f"{d}/{cfg.model_path}"
    syn = Synthesizer(
        cfg, max_input_length=400
    )  # skips the 1998-char probe (3.5 GB VRAM)

    def say(v, out):
        with open(f"{out}.wav", "wb") as f:
            f.write(syn._synthesize(text, v, 1 / slow)[0].content.audio)
elif engine == "xtts":
    import numpy as np
    import soundfile as sf
    from TTS.tts.configs.xtts_config import XttsConfig
    from TTS.tts.layers.xtts.tokenizer import VoiceBpeTokenizer
    from TTS.tts.models.xtts import Xtts
    from tts_preprocess_et.convert import convert_sentence

    pre = (
        VoiceBpeTokenizer.preprocess_text
    )  # upstream coqui-tts lacks TartuNLP's "et" branch
    VoiceBpeTokenizer.preprocess_text = lambda s, t, lang: (
        convert_sentence(t) if lang == "et" else pre(s, t, lang)
    )
    d = os.environ["XTTS_DIR"]
    cfg = XttsConfig()
    cfg.load_json(f"{d}/config.json")
    model = Xtts.init_from_config(cfg)
    model.load_checkpoint(cfg, checkpoint_dir=d, vocab_path=f"{d}/vocab.json")
    model.to(device()).eval()
    model.tokenizer.char_limits["et"] = 400
    latents = {}

    def say(v, out):
        if v not in latents:
            latents[v] = model.get_conditioning_latents(audio_path=v)
        t, first, chunks = time.perf_counter(), None, []
        for c in model.inference_stream(
            text,
            "et",
            *latents[v],
            speed=1 / slow,
            temperature=0.1,
            top_k=10,
            top_p=0.3,
            repetition_penalty=10.0,
        ):
            first = first or time.perf_counter() - t
            chunks.append(c.cpu().numpy())
        sf.write(f"{out}.wav", np.concatenate(chunks), 24000)
        return first
elif engine == "azure":
    from xml.sax.saxutils import escape

    def say(v, out):
        ssml = (
            f"<speak version='1.0' xml:lang='{v[:5]}'><voice name='{v}'>"
            f"<prosody rate='{round(100 / slow - 100):+d}%'>{escape(text)}</prosody></voice></speak>"
        )
        req = urllib.request.Request(
            f"https://{os.environ['AZURE_SPEECH_REGION']}.tts.speech.microsoft.com/cognitiveservices/v1",
            ssml.encode(),
            {
                "Ocp-Apim-Subscription-Key": os.environ["AZURE_SPEECH_KEY"],
                "Content-Type": "application/ssml+xml",
                "X-Microsoft-OutputFormat": "riff-24khz-16bit-mono-pcm",
                "User-Agent": "my-vector-voice-samples",
            },
        )
        return fetch(req, f"{out}.wav")
elif engine == "espeak":
    r = os.environ["ESPEAK_ROOT"]
    lib = f"{r}/usr/lib/x86_64-linux-gnu"

    def say(v, out):
        subprocess.run(
            [
                f"{r}/usr/bin/espeak-ng",
                f"--path={lib}",
                "-v",
                v,
                "-s",
                str(round(175 / slow)),
                "-w",
                f"{out}.wav",
                "--",  # text starting with "-" is not an option
                text,
            ],
            env={**os.environ, "LD_LIBRARY_PATH": lib},
            check=True,
        )
else:
    sys.exit(f"unknown engine {engine}")

if bench:
    print("bench loaded", file=sys.stderr, flush=True)
for v in voices:
    out = f"{outdir}/{name(v)}"
    if bench:
        say(v, out)
    t = time.perf_counter()
    first = say(v, out)
    total = time.perf_counter() - t
    if bench:
        tp = sys.modules.get(
            "torch"
        )  # torch-side peak; nvidia-smi adds the CUDA context
        gb = (
            tp.cuda.max_memory_reserved() / 2**30
            if tp and tp.cuda.is_initialized()
            else 0
        )
        m = f"first={first or total:.3f} total={total:.3f} torch_gb={gb:.2f}"
        print(f"bench {name(v)} {m}", file=sys.stderr, flush=True)
        if os.environ.get("BENCH_LOG"):
            voice = re.sub(r"\.(onnx|wav)", "", os.path.basename(v))
            stamp = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
            dev = os.environ.get("DEVICE") or "auto"
            with open(os.environ["BENCH_LOG"], "a") as f:
                f.write(f"{stamp} {engine}:{voice} device={dev} {m}\n")
