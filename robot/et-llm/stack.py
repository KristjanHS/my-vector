# pyright: reportMissingImports=false
# ET stack fit (plan 2026-11-05-estonian-language-mode.md 1.3, E13/E14): STT + EstLLM + Neurokõne resident, nvidia-smi 100 ms peak.
# Usage: stack.py run [--kokoro-mib N] [QUANT[@CTX]...] | stack.py summary   (stdlib; python3 -I). Workers `_stt`/`_tts` run in their venvs.
import json
import os
import subprocess
import sys
import threading
import time
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import bench  # noqa: E402

HF = "hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:"
LADDER = [
    ("i1-Q4_K_M", None),
    ("i1-Q4_K_S", None),
    ("i1-Q4_K_M", 2048),
    ("i1-IQ4_XS", None),
    ("i1-Q3_K_M", None),
]
W = bench.ROOT / "robot/work"
STT_PY, STT_MODEL = W / "et-stt/venv/bin/python", W / "et-stt/models/taltech-et/ct2"
TTS_PY, TARTU_DIR = (
    W / "voice-samples/venv-tartulocal/bin/python",
    W / "voice-samples/tartu/worker",
)
SERVER_PY, KOKORO_PORT = W / "tts/venv/bin/python", 8092
OUT = W / "et-llm/et/stack.jsonl"
VOICE = "mari"
SENTENCE = "Tallinn on Eesti pealinn ja suurim linn, mis asub Soome lahe ääres."


def clip():
    return str(max((W / "et-stt/mic-et").glob("*.ogg"), key=lambda p: p.stat().st_size))


# ---- workers (own venv; stdout protocol: "ready", then one JSON line per "go" on stdin) ----
def serve(once):
    once()  # warm-up
    print("ready", flush=True)
    for _ in sys.stdin:
        print(json.dumps(once(), ensure_ascii=False), flush=True)


def stt_worker(model, path):
    from faster_whisper import WhisperModel

    m = WhisperModel(model, device="cuda", compute_type="int8_float16")

    def once():
        t = time.monotonic()
        segs, _ = m.transcribe(
            path,
            language="et",
            beam_size=5,
            condition_on_previous_text=False,
            vad_filter=False,
        )
        return {
            "text": " ".join(s.text for s in segs).strip(),
            "s": round(time.monotonic() - t, 2),
        }

    serve(once)


def tts_worker(d):  # same load as synth.py tartulocal
    sys.path.insert(0, d)
    from speechbrain.inference.vocoders import HIFIGAN
    from tts_worker.config import read_model_config
    from tts_worker.synthesizer import Synthesizer

    fh, dec = HIFIGAN.from_hparams, HIFIGAN.decode_batch
    HIFIGAN.from_hparams = lambda **k: fh(**k, run_opts={"device": "cuda"})
    HIFIGAN.decode_batch = lambda s, m: dec(s, m).cpu()
    cfg = read_model_config(f"{d}/config/config.yaml", "multispeaker")
    cfg.model_path = f"{d}/{cfg.model_path}"
    syn = Synthesizer(cfg, max_input_length=400)

    def once():
        t = time.monotonic()
        syn._synthesize(SENTENCE, VOICE, 1.0)
        return {"s": round(time.monotonic() - t, 2)}

    serve(once)


class Worker:
    def __init__(self, py, role, *args, env=None):
        self.role = role
        self.p = subprocess.Popen(
            [str(py), "-I", __file__, role, *map(str, args)],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            env=(os.environ | env) if env else None,
            cwd=W,
        )

    def line(self):
        assert self.p.stdout
        out = self.p.stdout.readline()
        if not out:
            raise RuntimeError(f"worker {self.role} exited ({self.p.wait()})")
        return out.strip()

    def ready(self):  # library prints share stdout: skip to the protocol line
        while self.line() != "ready":
            pass

    def send(self):
        assert self.p.stdin
        self.p.stdin.write("go\n")
        self.p.stdin.flush()

    def recv(self):
        while not (out := self.line()).startswith("{"):
            pass
        return json.loads(out)

    def close(self):
        self.p.kill()
        self.p.wait()


class Peak:
    """Max nvidia-smi memory.used over the with-block, sampled every 100 ms."""

    def __enter__(self):
        self.peak = 0
        self.p = subprocess.Popen(
            [
                "nvidia-smi",
                "--query-gpu=memory.used",
                "--format=csv,noheader,nounits",
                "-lms",
                "100",
            ],
            stdout=subprocess.PIPE,
            text=True,
        )
        self.t = threading.Thread(target=self._read, daemon=True)
        self.t.start()
        return self

    def _read(self):
        assert self.p.stdout
        for line in self.p.stdout:
            if line.strip().isdigit():
                self.peak = max(self.peak, int(line))

    def __exit__(self, *exc):
        time.sleep(0.3)
        self.p.terminate()
        self.p.wait()
        self.t.join(2)


# ---- fit ----
def verdict(row):
    """(headroom MiB, fit): spill fails; else total - (peak - base + worst base - Kokoro share + margin)."""
    if row["size_vram_b"] < row["size_b"]:
        return None, "spill"
    need = (
        row["peak_mib"]
        - row["gpu_base_mib"]
        + bench.WORST_BASE_MIB
        - row["kokoro_mib"]
        + bench.MARGIN_MIB
    )
    headroom = row["gpu_total_mib"] - need
    return headroom, "yes" if headroom >= 0 else "no"


def ctx_estimate(rows, quant, ctx):
    """Estimated headroom of quant@ctx = its default-ctx headroom + Q4_K_M's measured ctx saving; None if unmeasured."""
    by = {(r["quant"], r["num_ctx"]): r for r in rows if "error" not in r}
    m0, mc, q0 = (
        by.get(("i1-Q4_K_M", None)),
        by.get(("i1-Q4_K_M", ctx)),
        by.get((quant, None)),
    )
    if not (m0 and mc and q0) or None in (
        m0["headroom_mib"],
        mc["headroom_mib"],
        q0["headroom_mib"],
    ):
        return None
    return q0["headroom_mib"] + mc["headroom_mib"] - m0["headroom_mib"]


# ---- orchestration ----
def chat(model, text, opts):
    body = {
        "model": model,
        "stream": True,
        "keep_alive": "10m",
        "options": opts | {"temperature": 1, "top_p": 1},
        "messages": [
            {"role": "system", "content": bench.PROMPT},
            {"role": "user", "content": text},
        ],
    }
    t0, sentence, parts = time.monotonic(), None, []
    with bench.post("/api/chat", body) as r:
        for line in r:
            obj = json.loads(line)
            if "error" in obj:
                raise RuntimeError(f"{model}: {obj['error']}")
            delta = (obj.get("message") or {}).get("content") or ""
            if delta and sentence is None and bench.SENTENCE_END.search(delta):
                sentence = time.monotonic() - t0
            parts.append(delta)
    total = time.monotonic() - t0
    return (
        "".join(parts).strip(),
        bench.r2(sentence if sentence is not None else total),
        round(total, 2),
    )


def health(port):
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/health", timeout=2) as r:
            return json.load(r)
    except (OSError, ValueError):
        return None


def kokoro_share():
    """VRAM MiB a fresh TTS server adds with Kokoro loaded (the live server is left running)."""
    bench.unload_all()
    before, _ = bench.settled_gpu()
    env = os.environ | {
        "TTS_PORT": str(KOKORO_PORT),
        "TTS_BIND": "127.0.0.1",
        "HF_HUB_OFFLINE": "1",
    }
    env.setdefault("HF_HOME", str(Path.home() / ".cache/hf"))
    env.setdefault("HF_HUB_CACHE", env["HF_HOME"] + "/hub")
    p = subprocess.Popen(
        [str(SERVER_PY), "-I", str(bench.ROOT / "tts/server.py")],
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    h = None
    try:
        for _ in range(300):
            h = health(KOKORO_PORT)
            if h or p.poll() is not None:
                break
            time.sleep(1)
        if not (h and h.get("kokoro_loaded")):
            sys.exit(
                f"second TTS server on :{KOKORO_PORT} did not load Kokoro ({h}); pass --kokoro-mib N"
            )
        after, _ = bench.settled_gpu()
    finally:
        p.terminate()
        p.wait(30)
    return after - before


def run_variant(quant, ctx, kokoro_mib, ver, warm_q):
    model, opts = HF + quant, ({"num_ctx": ctx} if ctx else {})
    print(f"== {quant}" + (f" num_ctx {ctx}" if ctx else ""))
    row = {
        "quant": quant,
        "num_ctx": ctx,
        "model": model,
        "ollama": ver,
        "kokoro_mib": kokoro_mib,
    }
    workers = []
    try:
        bench.unload_all()
        if bench.ensure_pulled(model) == "error":
            raise RuntimeError("pull failed")
        base, total = bench.settled_gpu()
        with Peak() as pk:
            workers = [
                Worker(STT_PY, "_stt", STT_MODEL, clip()),
                Worker(TTS_PY, "_tts", TARTU_DIR),
            ]
            for w in workers:
                w.ready()
            stt, tts = workers
            bench.post(
                "/api/generate", {"model": model, "keep_alive": "10m", "options": opts}
            ).read()
            chat(model, warm_q, opts)
            ps = [m for m in bench.get("/api/ps")["models"] if m["name"] == model]
            stt.send()
            heard = stt.recv()
            tts.send()  # Neurokõne speaks while the LLM generates
            answer, sentence, total_s = chat(model, heard["text"], opts)
            spoke = tts.recv()
    except (*bench.ERRORS, OSError, ValueError) as e:
        msg = bench.errmsg(e)
        print(f"!! {quant} failed: {msg}")
        bench.append(OUT, row | {"error": msg})
        return "error"
    finally:
        for w in workers:
            w.close()
    if not ps:
        bench.append(OUT, row | {"error": "model not in /api/ps"})
        return "error"
    row |= {
        "size_b": ps[0]["size"],
        "size_vram_b": ps[0]["size_vram"],
        "gpu_base_mib": base,
        "peak_mib": pk.peak,
        "gpu_total_mib": total,
        "stt_s": heard["s"],
        "heard": heard["text"],
        "tts_s": spoke["s"],
        "first_sentence_s": sentence,
        "total_s": total_s,
        "answer": answer,
    }
    row["headroom_mib"], row["fit"] = verdict(row)
    bench.append(OUT, row)
    print(
        f"   base {base} peak {pk.peak}/{total} MiB, Kokoro {kokoro_mib}, headroom {row['headroom_mib']}: {row['fit']}"
    )
    return row["fit"]


def parse(spec):
    quant, _, ctx = spec.partition("@")
    return quant, int(ctx) if ctx else None


def run(args):
    kokoro_mib = None
    if args[:1] == ["--kokoro-mib"]:
        kokoro_mib, args = int(args[1]), args[2:]
    bench.use("et")
    if not bench.kokoro_resident():
        sys.exit(
            f"Kokoro not loaded ({bench.TTS}/health): the base reading must include it"
        )
    OUT.parent.mkdir(parents=True, exist_ok=True)
    ver = bench.get("/api/version")["version"]
    warm_q = bench.QUESTIONS.read_text().splitlines()[0]
    try:
        if kokoro_mib is None:
            kokoro_mib = kokoro_share()
            print(f"Kokoro share {kokoro_mib} MiB")
        if args:
            for spec in args:
                run_variant(*parse(spec), kokoro_mib, ver, warm_q)
            return
        for quant, ctx in LADDER:
            fit = run_variant(quant, ctx, kokoro_mib, ver, warm_q)
            if fit == "yes":
                return
            if (
                ctx
                and (ctx_estimate(bench.load_rows(OUT), "i1-Q4_K_S", ctx) or -1) >= 0
            ):
                if run_variant("i1-Q4_K_S", ctx, kokoro_mib, ver, warm_q) == "yes":
                    return
    finally:
        subprocess.run(
            [str(bench.ROOT / "scripts/llm.sh"), "warm"], capture_output=True
        )


def summary():
    rows = bench.load_rows(OUT)
    last = {}
    for r in rows:  # last good row per variant; an error row only if no good one
        k = (r["quant"], r["num_ctx"])
        if "error" not in r or "error" in last.get(k, {"error": ""}):
            last[k] = r
    print(
        "| quant | num_ctx | base→peak MiB | Kokoro MiB | headroom MiB | fit | STT s | first sentence s | total s | TTS s |"
    )
    print("|---|---|---|---|---|---|---|---|---|---|")
    for (quant, ctx), r in last.items():
        if "error" in r:
            print(
                f"| {quant} | {ctx or 'default'} | – | – | – | error: {r['error'][:60]} | – | – | – | – |"
            )
            continue
        print(
            f"| {quant} | {ctx or 'default'} | {r['gpu_base_mib']}→{r['peak_mib']} | {r['kokoro_mib']} | {r['headroom_mib']} | {r['fit']}"
            f" | {r['stt_s']} | {r['first_sentence_s']} | {r['total_s']} | {r['tts_s']} |"
        )
    est = ctx_estimate(rows, "i1-Q4_K_S", 2048)
    if est is not None and ("i1-Q4_K_S", 2048) not in last:
        print(
            f"\ni1-Q4_K_S @2048 estimated headroom {est} MiB (Q4_K_S default + Q4_K_M ctx saving)"
        )


if __name__ == "__main__":
    a = sys.argv[1:]
    if a[:1] == ["_stt"]:
        stt_worker(*a[1:3])
    elif a[:1] == ["_tts"]:
        tts_worker(a[1])
    elif a[:1] == ["run"]:
        run(a[1:])
    elif a == ["summary"]:
        summary()
    else:
        sys.exit(
            "usage: stack.py run [--kokoro-mib N] [QUANT[@CTX]...] | stack.py summary"
        )
