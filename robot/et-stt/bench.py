# Estonian STT bench (plan 2026-11-05 Stage 1.1): one candidate per process -> WER, end-of-speech->text latency, VRAM delta.
# Driven by run.sh; `bench.py summary` prints the table from robot/work/et-stt/results/*.json.
import argparse
import base64
import csv
import io
import json
import os
import re
import statistics
import subprocess
import threading
import time
import unicodedata
from pathlib import Path

WORK = Path(__file__).resolve().parents[1] / "work" / "et-stt"
RES = WORK / "results"
N = 50


def norm(t):
    t = unicodedata.normalize("NFC", t).lower()
    t = "".join(" " if unicodedata.category(c)[0] in "PS" else c for c in t)
    return re.sub(r"\s+", " ", t).strip()


def clips(dataset, n=N):
    """n shortest clips (one recording per sentence) -> [(path, ref, seconds)]."""
    if dataset.startswith("fleurs-"):
        lang = {"fleurs-et": "et_ee", "fleurs-en": "en_us"}[dataset]
        d = WORK / "fleurs" / "data" / lang
        rows = list(
            csv.reader(
                open(d / "test.tsv", encoding="utf-8"),
                delimiter="\t",
                quoting=csv.QUOTE_NONE,
            )
        )
        rows.sort(key=lambda r: int(r[5]))
        out, seen = [], set()
        for r in rows:
            if r[0] in seen:
                continue
            seen.add(r[0])
            out.append((str(d / "audio" / "test" / r[1]), r[3], int(r[5]) / 16000))
            if len(out) == n:
                break
        return out
    if dataset == "cv-et":
        d = WORK / "cv"
        tsv = next(d.rglob("test.tsv"))
        base = tsv.parent
        rows = list(
            csv.DictReader(
                open(tsv, encoding="utf-8"), delimiter="\t", quoting=csv.QUOTE_NONE
            )
        )
        durs = {}
        dur_tsv = base / "clip_durations.tsv"
        if dur_tsv.exists():
            for r in csv.DictReader(open(dur_tsv, encoding="utf-8"), delimiter="\t"):
                durs[r["clip"]] = float(r["duration[ms]"]) / 1000
        else:
            from faster_whisper.audio import decode_audio

            for r in rows:
                durs[r["path"]] = (
                    len(decode_audio(str(base / "clips" / r["path"]))) / 16000
                )
        rows = [r for r in rows if r["path"] in durs]
        rows.sort(key=lambda r: durs[r["path"]])
        return [
            (str(base / "clips" / r["path"]), r["sentence"], durs[r["path"]])
            for r in rows[:n]
        ]
    if dataset == "mic-et":  # Stage 2.2: Vector mic captures (miccap), in mic-commands.txt order
        refs = (Path(__file__).parent / "mic-commands.txt").read_text(encoding="utf-8").splitlines()
        audio = sorted(p for p in (WORK / "mic").iterdir() if p.suffix in (".ogg", ".pcm"))
        if len(audio) != len(refs):
            raise SystemExit(f"mic-et: {len(audio)} captures for {len(refs)} commands — prune retries first")
        return [(str(a), r, len(load16k(str(a))) / 16000) for a, r in zip(audio, refs)]
    raise SystemExit(f"unknown dataset {dataset}")


def load16k(path):
    from faster_whisper.audio import (
        decode_audio,
    )  # PyAV: any format -> mono float32 16 kHz

    if path.endswith(".pcm"):  # miccap raw: 16 kHz s16le mono
        import numpy as np

        return np.frombuffer(open(path, "rb").read(), dtype=np.int16).astype(np.float32) / 32768
    return decode_audio(path, sampling_rate=16000)


def gpu_used():
    try:
        return int(
            subprocess.check_output(
                [
                    "nvidia-smi",
                    "--query-gpu=memory.used",
                    "--format=csv,noheader,nounits",
                ],
                text=True,
            ).split()[0]
        )
    except Exception:
        return None


class Peak(threading.Thread):
    def __init__(self):
        super().__init__(daemon=True)
        self.peak, self.stop = 0, False

    def run(self):
        while not self.stop:
            v = gpu_used()
            if v:
                self.peak = max(self.peak, v)
            time.sleep(0.05)


# ---- engines: setup(args) -> fn(audio float32 16k) -> (text, latency s or None = time the whole call) -------------------------------------------
def eng_fw(a):
    from faster_whisper import WhisperModel

    m = WhisperModel(
        a.model,
        device=a.device,
        compute_type=a.compute,
        cpu_threads=os.cpu_count() or 4,
    )

    def run(x):
        segs, _ = m.transcribe(
            x,
            language=a.lang,
            beam_size=a.beam,
            condition_on_previous_text=False,
            vad_filter=False,
        )
        return " ".join(s.text for s in segs), None

    return run


def eng_vosk(a):
    import numpy as np
    import vosk

    vosk.SetLogLevel(-1)
    m = vosk.Model(a.model)

    def run(x):
        pcm = (np.clip(x, -1, 1) * 32767).astype("<i2").tobytes()
        rec = vosk.KaldiRecognizer(m, 16000)
        step = 3200 * 2  # 0.2 s of int16
        chunks = [pcm[i : i + step] for i in range(0, len(pcm), step)]
        for c in chunks[:-1]:
            rec.AcceptWaveform(c)
        t0 = (
            time.perf_counter()
        )  # end of speech: only the last chunk + FinalResult is timed
        rec.AcceptWaveform(chunks[-1])
        txt = json.loads(rec.FinalResult()).get("text", "")
        return txt, time.perf_counter() - t0

    return run


def wav_bytes(x):
    import soundfile as sf

    b = io.BytesIO()
    sf.write(b, x, 16000, format="WAV", subtype="PCM_16")
    return b.getvalue()


def eng_azure(a):
    import requests

    key, region = os.environ["AZURE_SPEECH_KEY"], os.environ["AZURE_SPEECH_REGION"]
    url = f"https://{region}.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1?language=et-EE&format=simple"
    s = requests.Session()

    def run(x):
        r = s.post(
            url,
            data=wav_bytes(x),
            timeout=30,
            headers={
                "Ocp-Apim-Subscription-Key": key,
                "Content-Type": "audio/wav; codecs=audio/pcm; samplerate=16000",
            },
        )
        r.raise_for_status()
        return r.json().get("DisplayText", ""), None

    return run


def eng_google(a):
    import requests

    key = os.environ["GOOGLE_STT_KEY"]
    s = requests.Session()

    def run(x):
        body = {
            "config": {
                "encoding": "LINEAR16",
                "sampleRateHertz": 16000,
                "languageCode": "et-EE",
            },
            "audio": {"content": base64.b64encode(wav_bytes(x)[44:]).decode()},
        }
        # key in a header, never the URL: requests quotes the URL in HTTPError messages
        r = s.post(
            "https://speech.googleapis.com/v1/speech:recognize",
            headers={"X-Goog-Api-Key": key},
            json=body,
            timeout=30,
        )
        r.raise_for_status()
        return " ".join(
            res["alternatives"][0]["transcript"] for res in r.json().get("results", [])
        ), None

    return run


ENGINES = {"fw": eng_fw, "vosk": eng_vosk, "azure": eng_azure, "google": eng_google}
KEY_VARS = ("AZURE_SPEECH_KEY", "GOOGLE_STT_KEY")


def scrub(e):
    """Exception -> short string with key values and URLs masked (it lands in results JSON and stdout)."""
    msg = str(e)
    for v in KEY_VARS:
        if os.environ.get(v):
            msg = msg.replace(os.environ[v], "<key>")
    msg = re.sub(r"https?://\S+", "<url>", msg)
    return f"{type(e).__name__}: {msg[:120]}"


def pct(xs, p):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, round(p * (len(xs) - 1)))]


def vram(base, loaded, peak):
    if base is None or loaded is None:
        return None
    peak = max(peak, loaded)
    return {
        "baseline": base,
        "loaded": loaded,
        "peak": peak,
        "load_delta": loaded - base,
        "peak_delta": peak - base,
    }


def sec(v):
    return "-" if v is None else f"{v:.2f}"


def gb(v):
    return (
        "-"
        if not v
        else f"{v['load_delta'] / 1024:.2f}/{v['peak_delta'] / 1024:.2f} GB"
    )


def bench(a):
    import jiwer

    data = clips(a.dataset, a.n)
    audio = [load16k(p) for p, _, _ in data]
    online = a.engine in ("azure", "google")
    base = None if online or a.device == "cpu" else gpu_used()
    t = time.perf_counter()
    fn = ENGINES[a.engine](a)
    load_s = time.perf_counter() - t
    loaded = None if base is None else gpu_used()
    pk = Peak()
    if base is not None:
        pk.start()
    warm_err = None
    try:
        fn(audio[0])  # warm-up, excluded
    except Exception as e:
        if not online:
            raise
        warm_err = scrub(e)  # online: the traceback would quote the request
        print(f"{a.name}: warm-up failed: {warm_err}")
    rows = []
    for (p, ref, dur), x in zip(data, audio):
        t0 = time.perf_counter()
        try:
            hyp, lat = fn(x)
            err = None
        except Exception as e:  # online engines: record, keep going
            hyp, lat, err = "", None, scrub(e)
        lat = time.perf_counter() - t0 if lat is None else lat
        rows.append(
            {
                "file": Path(p).name,
                "dur": round(dur, 2),
                "lat": round(lat, 3),
                "ref": norm(ref),
                "hyp": norm(hyp),
                "raw": hyp.strip(),
                "err": err,
            }
        )
        if online:
            time.sleep(0.3)  # sequential and polite
    pk.stop = True
    refs = [r["ref"] for r in rows]
    hyps = [r["hyp"] for r in rows]
    lats = [
        r["lat"] for r in rows if not r["err"]
    ]  # a failed call's time is not a latency
    out = {
        "name": a.name,
        "engine": a.engine,
        "model": a.model,
        "device": a.device,
        "compute": a.compute,
        "beam": a.beam,
        "dataset": a.dataset,
        "n": len(rows),
        "median_clip_s": statistics.median(r["dur"] for r in rows),
        "wer": jiwer.wer(refs, hyps),
        "lat_med": statistics.median(lats) if lats else None,
        "lat_p90": pct(lats, 0.9) if lats else None,
        "load_s": round(load_s, 1),
        "errors": sum(1 for r in rows if r["err"]),
        "warmup_err": warm_err,
        "vram_mib": vram(base, loaded, pk.peak),
        "clips": rows,
    }
    RES.mkdir(parents=True, exist_ok=True)
    (RES / f"{a.name}.json").write_text(json.dumps(out, ensure_ascii=False, indent=1))
    print(
        f"{a.name}: WER {out['wer']:.1%}  lat med {sec(out['lat_med'])}s p90 {sec(out['lat_p90'])}s  "
        f"VRAM {gb(out['vram_mib'])}  errors {out['errors']}"
    )


def summary(_):
    for f in sorted(RES.glob("*.json")):
        r = json.loads(f.read_text())
        print(
            f"{r['name']:<28} {r['dataset']:<10} WER {r['wer']:6.1%}  lat {sec(r['lat_med'])}/{sec(r['lat_p90'])}s  "
            f"VRAM {gb(r['vram_mib'])}  clip med {r['median_clip_s']:.2f}s  err {r['errors']}"
        )


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["run", "summary"])
    ap.add_argument("--name")
    ap.add_argument("--engine", choices=ENGINES)
    ap.add_argument("--model", default="")
    ap.add_argument("--device", default="cuda")
    ap.add_argument("--compute", default="float16")
    ap.add_argument("--beam", type=int, default=5)
    ap.add_argument("--lang", default="et")
    ap.add_argument("--dataset", default="fleurs-et")
    ap.add_argument("--n", type=int, default=N)
    a = ap.parse_args()
    (bench if a.cmd == "run" else summary)(a)
