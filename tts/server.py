"""my-vector TTS service: POST /tts {"text", "voice", "slow", "boost_db"} -> chunked 16 kHz s16le mono PCM, one chunk per sentence.

GET /health -> {"voices": [...], "device": ..., "kokoro_loaded": bool}. Piper (+ Kokoro if selected) warm before the port opens.
Kokoro holds the GPU only while a kokoro voice is wire-pod's T1/T2 (myvectorConfig.json); unselected it loads for one request only.
Env: TTS_BIND (0.0.0.0), TTS_PORT (8091), TTS_PIPER_DIR (Piper .onnx dir), TTS_DEVICE (cuda|cpu), HF_HOME / HF_HUB_CACHE (the one shared HF cache),
TTS_VOICE_CONFIG (wire-pod's voice settings, polled every 5 s; default chipper/myvectorConfig.json in this repo).
"""

import contextlib
import gc
import itertools
import json
import logging
import os
import re
import sys
import threading
import time
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import numpy as np

RATE = 16000
SLOW_MIN, SLOW_MAX = 0.5, 2.0
MAX_TEXT = 5000
MAX_SENTENCE = 300  # longer sentences split again at , ; :
PEAK = 0.97  # limiter ceiling (float full scale)
BOOST_MAX = 18.0  # dB; boost_db is clamped to 0..BOOST_MAX
LIMIT_BLOCK = 32  # samples (2 ms) per limiter gain step
LIMIT_RELEASE_S = 0.06  # limiter gain recovery time constant
GAINS_FILE = Path(__file__).with_name("gains.json")  # tts/calibrate.py writes it

# voice name -> (engine, engine voice id)
VOICES = {"cori": ("piper", "en_GB-cori-high"), "af_bella": ("kokoro", "af_bella")}

log = logging.getLogger("myvector-tts")

# wire-pod's chipper/pkg/myvector/voice/settings.go: the voice choices beyond VOICES, defaults, slow range
STOCK, DEFAULT_VOICE, DEFAULT_SLOW = "stock", "af_bella", 1.2
VOICE_CONFIG = Path(__file__).resolve().parents[1] / "chipper/myvectorConfig.json"
CONFIG_POLL_S = 5

_SENT_END = re.compile(r"(?<=[.!?…])[\"')\]]*\s+|\n+")
_CLAUSE_END = re.compile(r"(?<=[,;:])\s+")
_ABBREV = re.compile(r"\b(?:Mr|Mrs|Ms|Dr|St|vs|etc|e\.g|i\.e|No)\.$", re.IGNORECASE)


def split_sentences(text):
    """Split text into speakable pieces: sentence ends, newlines, then clauses for pieces over MAX_SENTENCE."""
    parts, buf = [], ""
    for piece in _SENT_END.split(text.strip()):
        piece = piece.strip()
        if not piece:
            continue
        buf = f"{buf} {piece}" if buf else piece
        if not _ABBREV.search(buf):
            parts.append(buf)
            buf = ""
    if buf:
        parts.append(buf)
    out = []
    for p in parts:
        out.extend(_split_long(p) if len(p) > MAX_SENTENCE else [p])
    return out


def _split_long(p):
    out, cur = [], ""
    for c in _CLAUSE_END.split(p):
        if cur and len(cur) + 1 + len(c) > MAX_SENTENCE:
            out.extend(_hard_cut(cur))
            cur = c
        else:
            cur = f"{cur} {c}" if cur else c
    if cur:
        out.extend(_hard_cut(cur))
    return out


def _hard_cut(s):
    """No clause breaks left: cut at the last space before MAX_SENTENCE (or at it)."""
    out = []
    while len(s) > MAX_SENTENCE:
        cut = s.rfind(" ", 0, MAX_SENTENCE)
        cut = cut if cut > 0 else MAX_SENTENCE
        out.append(s[:cut])
        s = s[cut:].lstrip()
    if s:
        out.append(s)
    return out


def limit(x, ceiling=PEAK):
    """Peak limiter: gain dips instantly around any block whose peak tops `ceiling`, recovers over LIMIT_RELEASE_S."""
    if not x.size or float(np.max(np.abs(x))) <= ceiling:
        return x
    n = -(-x.size // LIMIT_BLOCK)
    a = np.zeros(n * LIMIT_BLOCK, np.float32)
    a[: x.size] = np.abs(x)
    pk = a.reshape(n, LIMIT_BLOCK).max(axis=1)
    pk = np.maximum(pk, np.maximum(np.r_[0, pk[:-1]], np.r_[pk[1:], 0]))  # each step also covers its neighbours
    want = np.minimum(1.0, ceiling / np.maximum(pk, 1e-9))
    rel = 1.0 - np.exp(-LIMIT_BLOCK / (RATE * LIMIT_RELEASE_S))
    g, cur = np.empty(n), 1.0
    for i, w in enumerate(want):
        cur = w if w < cur else cur + (1.0 - cur) * rel
        g[i] = min(cur, w)  # cur may recover past a pinned w; g never tops w, so the ceiling holds
    # linear between block centres: both ends are under the bound for the samples between them
    gs = np.interp(np.arange(x.size), np.arange(n) * LIMIT_BLOCK + (LIMIT_BLOCK - 1) / 2, g)
    return x * gs.astype(np.float32)


def to_pcm16(audio, rate, gain=1.0):
    """float audio at `rate` -> 16 kHz s16le bytes, times `gain`, peak-limited to PEAK."""
    x = np.asarray(audio, dtype=np.float32).reshape(-1)
    if rate != RATE and x.size:
        import soxr

        x = soxr.resample(x, rate, RATE, quality="HQ").astype(np.float32)
    x = limit(x * np.float32(gain))
    return (np.clip(x, -PEAK, PEAK) * 32767.0).astype("<i2").tobytes()


def load_gains(path=GAINS_FILE):
    """Per-voice linear gains from gains.json ({"cori": {"gain_db": -1.5}, ...}); missing voice = 0 dB."""
    try:
        raw = json.loads(path.read_text())
    except FileNotFoundError:
        log.warning("%s missing: all voices at 0 dB (run tts/calibrate.py)", path.name)
        raw = {}
    return {v: 10 ** (float(raw.get(v, {}).get("gain_db", 0.0)) / 20) for v in VOICES}


def parse_request(body):
    """Validate a /tts JSON body -> (text, voice, slow, boost_db). Raises ValueError with a client-facing message."""
    try:
        req = json.loads(body or b"{}")
    except (json.JSONDecodeError, UnicodeDecodeError) as e:
        raise ValueError(f"bad JSON: {e}") from None
    if not isinstance(req, dict):
        raise ValueError("body must be a JSON object")
    text = req.get("text")
    if not isinstance(text, str) or not text.strip():
        raise ValueError("text: non-empty string required")
    if len(text) > MAX_TEXT:
        raise ValueError(f"text: over {MAX_TEXT} chars")
    voice = req.get("voice", DEFAULT_VOICE)
    if voice not in VOICES:
        raise ValueError(f"voice: one of {sorted(VOICES)}")
    try:
        slow = float(req.get("slow", 1.0))
    except (TypeError, ValueError):
        raise ValueError("slow: number required") from None
    if slow != slow:  # NaN
        raise ValueError("slow: number required")
    try:
        boost = float(req.get("boost_db", 0.0))
    except (TypeError, ValueError):
        raise ValueError("boost_db: number required") from None
    if boost != boost:
        raise ValueError("boost_db: number required")
    return text, voice, min(max(slow, SLOW_MIN), SLOW_MAX), min(max(boost, 0.0), BOOST_MAX)


class PiperEngine:
    def __init__(self, model_dir):
        from piper import PiperVoice, SynthesisConfig

        self._cfg = SynthesisConfig
        self._voices = {}
        self._dir = Path(model_dir)
        self._load = PiperVoice.load

    def synth(self, voice_id, text, slow):
        if voice_id not in self._voices:
            self._voices[voice_id] = self._load(str(self._dir / f"{voice_id}.onnx"))
        v = self._voices[voice_id]
        chunks = list(v.synthesize(text, syn_config=self._cfg(length_scale=slow)))
        if not chunks:
            return np.zeros(0, np.float32), v.config.sample_rate
        return np.concatenate([c.audio_float_array for c in chunks]), chunks[
            0
        ].sample_rate


class KokoroEngine:
    REPO = "hexgrad/Kokoro-82M"

    def __init__(self, device):
        import torch
        from kokoro import KModel, KPipeline

        if device == "cuda" and not torch.cuda.is_available():
            log.warning("CUDA unavailable: Kokoro on CPU")
            device = "cpu"
        try:
            self._model = KModel(repo_id=self.REPO).to(device).eval()
        except RuntimeError as e:  # e.g. CUDA out of memory
            if device == "cpu":
                raise
            log.warning("Kokoro on %s failed (%s): CPU", device, e)
            device = "cpu"
            self._model = KModel(repo_id=self.REPO).to(device).eval()
        self.device = device
        self._torch = torch
        self._pipes = {}
        self._pipeline = KPipeline

    def close(self):
        """Drop model + pipelines; reclaim_memory() then hands the VRAM back."""
        self._pipes.clear()
        self._model = None

    def synth(self, voice_id, text, slow):
        lang = voice_id[0]
        if lang not in self._pipes:
            self._pipes[lang] = self._pipeline(
                lang_code=lang, repo_id=self.REPO, model=self._model
            )
        with self._torch.inference_mode():
            parts = [
                r.audio.cpu().numpy()
                for r in self._pipes[lang](text, voice=voice_id, speed=1 / slow)
                if r.audio is not None
            ]
        return (np.concatenate(parts) if parts else np.zeros(0, np.float32)), 24000


def reclaim_memory():
    """Collect dropped models and return torch's cached CUDA blocks to the driver; never under the Synth lock."""
    gc.collect()
    torch = sys.modules.get("torch")
    if torch is not None and torch.cuda.is_initialized():
        torch.cuda.empty_cache()


class LazyKokoro:
    """Kokoro resident iff pinned (a kokoro voice selected in wire-pod); unpinned it loads for a request and frees at its end.

    Every load/free runs under `lock` (Synth's), so a free never lands mid-synthesis; reclaim runs after the lock is released.
    """

    def __init__(self, factory, device, lock, reclaim=reclaim_memory):
        self._factory, self._want, self.device = factory, device, device  # _want: every load retries it (one OOM != CPU forever)
        self._lock, self._reclaim = lock, reclaim
        self._engine, self.pinned, self._requests = None, False, 0

    @property
    def loaded(self):
        return self._engine is not None

    @staticmethod
    def _call(what, fn, *args):
        """fn(*args), but a failure re-raises as a fresh error: the original traceback's frames would keep the model alive."""
        try:
            return fn(*args)
        except Exception as e:
            log.error("kokoro %s failed:\n%s", what, traceback.format_exc())  # text, not exc_info: a kept LogRecord pins frames too
            msg = f"kokoro {what} failed: {e!r}"
        raise RuntimeError(msg)

    def _load(self):  # caller holds the lock
        t = time.monotonic()
        self._engine = self._call("load", self._factory, self._want)
        self.device = self._engine.device
        log.info("kokoro loaded on %s in %.1f s", self.device, time.monotonic() - t)

    def _drop_if_unused(self):  # caller holds the lock; True -> caller runs self._reclaim() after releasing it
        if self._engine is None or self.pinned or self._requests:
            return False
        self._engine.close()
        self._engine = None
        log.info("kokoro freed")
        return True

    def synth(self, voice_id, text, slow):  # Synth calls this under the lock
        if self._engine is None:
            self._load()
        return self._call("synthesis", self._engine.synth, voice_id, text, slow)

    @contextlib.contextmanager
    def request(self):
        """Spans one /tts request: an unpinned engine survives its sentences, then frees once no request holds it."""
        with self._lock:
            self._requests += 1
        try:
            yield
        finally:
            with self._lock:
                self._requests -= 1
                dropped = self._drop_if_unused()
            if dropped:
                self._reclaim()

    def set_pinned(self, pinned):
        """Selected -> load + warm now (the robot's first line is fast); unselected -> free now (or at the last request's end).

        A failed load/warm leaves it unpinned and raises, so the caller's next attempt retries.
        """
        with self._lock:
            if pinned == self.pinned:
                return
            log.info("kokoro %s", "selected: loading, stays resident" if pinned else "unselected: freeing")
            try:
                if pinned and self._engine is None:
                    self._load()
                    self._call("warm", self._engine.synth, next(v for e, v in VOICES.values() if e == "kokoro"), "Hello there.", 1.0)
                self.pinned = pinned
            finally:
                dropped = self._drop_if_unused()
        if dropped:
            self._reclaim()


def kokoro_selected(path):
    """(is a kokoro voice wire-pod's T1 or T2, why-defaults or None). Unreadable/invalid -> defaults, as settings.go Current."""
    try:
        cfg = json.loads(Path(path).read_text())
        if not isinstance(cfg, dict):
            raise ValueError("not a JSON object")
        t1, t2 = cfg.get("t1", DEFAULT_VOICE), cfg.get("t2", DEFAULT_VOICE)
        slow = cfg.get("slow", DEFAULT_SLOW)
        for v in (t1, t2):
            if not isinstance(v, str) or (v != STOCK and v not in VOICES):  # a dict/list would TypeError on `in`
                raise ValueError(f"voice {v!r}")
        if isinstance(slow, bool) or not isinstance(slow, (int, float)) or not SLOW_MIN <= slow <= SLOW_MAX:
            raise ValueError(f"slow {slow!r}")
    except FileNotFoundError:
        return VOICES[DEFAULT_VOICE][0] == "kokoro", None
    except (OSError, ValueError) as e:  # JSONDecodeError and UnicodeDecodeError are ValueErrors
        return VOICES[DEFAULT_VOICE][0] == "kokoro", f"{path}: {e}"
    return any(VOICES.get(v, ("",))[0] == "kokoro" for v in (t1, t2)), None


class ConfigWatcher:
    """Polls wire-pod's voice config (stat key: mtime, inode, size) and pins/unpins Kokoro on a change."""

    def __init__(self, path, kokoro):
        self.path, self.kokoro, self._key = Path(path), kokoro, ()

    def poll(self):
        try:
            st = self.path.stat()
            key = (st.st_mtime_ns, st.st_ino, st.st_size)
        except OSError:
            key = None
        if key != self._key:  # one log line per change, not per poll
            pinned, err = kokoro_selected(self.path)
            if err:
                log.warning("voice config unreadable, using defaults: %s", err)
            self.kokoro.set_pinned(pinned)
            self._key = key  # only after success: a failed load retries next poll

    def poll_logged(self):
        """poll(), but a failure only logs: cori still serves and a kokoro request still loads on demand."""
        try:
            self.poll()
        except Exception:
            log.exception("kokoro pin/free failed; retrying next poll")

    def run(self, interval=CONFIG_POLL_S):
        while True:
            time.sleep(interval)
            self.poll_logged()


class Synth:
    """Voice name -> engine; one synthesis at a time (engines are not thread-safe)."""

    def __init__(self, engines, gains, lock=None):
        self.engines = engines  # {"piper": engine, "kokoro": engine}
        self.gains = gains
        self._lock = lock or threading.Lock()

    def sentence_pcm(self, voice, text, slow, boost_db=0.0):
        engine, voice_id = VOICES[voice]
        with self._lock:
            audio, rate = self.engines[engine].synth(voice_id, text, slow)
        return to_pcm16(audio, rate, self.gains.get(voice, 1.0) * 10 ** (boost_db / 20))

    def stream(self, voice, text, slow, boost_db=0.0):
        engine = self.engines[VOICES[voice][0]]
        with getattr(engine, "request", contextlib.nullcontext)():
            for s in split_sentences(text):
                pcm = self.sentence_pcm(voice, s, slow, boost_db)
                if pcm:
                    yield pcm

    def warm(self, engines=None):
        for voice, (engine, _) in VOICES.items():
            if engines is None or engine in engines:
                self.sentence_pcm(voice, "Hello there.", 1.0)


def make_handler(synth, info):
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        timeout = 30  # idle keep-alive / half-sent request frees its thread

        def log_message(self, format, *args):  # noqa: A002 - base class name
            log.info("%s %s", self.address_string(), format % args)

        def _json(self, code, obj):
            body = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/health":
                self._json(200, info())
            else:
                self._json(404, {"error": "not found"})

        def do_POST(self):
            client_close = self.close_connection  # the client's Connection: close
            self.close_connection = True  # until the body is read: an unread body would parse as the next request
            if self.path != "/tts":
                self._json(404, {"error": "not found"})
                return
            if "Transfer-Encoding" in self.headers:
                self._json(411, {"error": "send Content-Length, not a chunked body"})
                return
            try:
                n = int(self.headers.get("Content-Length", 0))
            except ValueError:
                n = -1
            if not 0 <= n <= 6 * MAX_TEXT + 1024:  # 6 = a \uXXXX-escaped char
                self._json(413 if n > 0 else 400, {"error": "bad Content-Length"})
                return
            body = self.rfile.read(n)
            self.close_connection = client_close
            try:
                text, voice, slow, boost = parse_request(body)
            except ValueError as e:
                self._json(400, {"error": str(e)})
                return
            # Headers wait for the first sentence: time-to-first-byte = time-to-first-audio, and a failing voice is a 500.
            chunks = synth.stream(voice, text, slow, boost)
            try:
                self._send_stream(chunks)
            finally:
                chunks.close()  # ends the stream's Kokoro request span now, even if the client left mid-stream

        def _send_stream(self, chunks):
            try:
                first = next(chunks, b"")
            except Exception:
                log.exception("synthesis failed")
                self._json(500, {"error": "synthesis failed"})
                return
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("X-Audio-Format", f"s16le;rate={RATE};channels=1")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            try:
                for pcm in itertools.chain([first] if first else [], chunks):
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(pcm), pcm))
                    self.wfile.flush()
                self.wfile.write(b"0\r\n\r\n")
            except (BrokenPipeError, ConnectionResetError):
                log.info("client left mid-stream")
                self.close_connection = True
            except Exception:  # headers are sent: drop the stream unterminated so the client sees a failure
                log.exception("synthesis failed")
                self.close_connection = True

    return Handler


def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
        stream=sys.stdout,
    )
    warm_only = "--warm-only" in sys.argv[1:]
    piper_dir = os.environ.get(
        "TTS_PIPER_DIR",
        str(
            Path(__file__).resolve().parents[1]
            / "robot/work/voice-samples/piper-voices"
        ),
    )
    lock = threading.Lock()
    kokoro = LazyKokoro(KokoroEngine, os.environ.get("TTS_DEVICE", "cuda"), lock)
    synth = Synth({"piper": PiperEngine(piper_dir), "kokoro": kokoro}, load_gains(), lock)
    if warm_only:  # tts/setup.sh: prove every model loads
        synth.warm()
        return
    synth.warm({"piper"})
    watcher = ConfigWatcher(os.environ.get("TTS_VOICE_CONFIG", VOICE_CONFIG), kokoro)
    watcher.poll_logged()  # a selected Kokoro loads + warms before the port opens
    threading.Thread(target=watcher.run, daemon=True).start()
    log.info("warm: piper%s, kokoro on %s", " + kokoro" if kokoro.loaded else "", kokoro.device)

    def info():
        return {"voices": sorted(VOICES), "device": kokoro.device, "kokoro_loaded": kokoro.loaded, "rate": RATE}

    addr = (
        os.environ.get("TTS_BIND", "0.0.0.0"),
        int(os.environ.get("TTS_PORT", "8091")),
    )
    srv = ThreadingHTTPServer(addr, make_handler(synth, info))
    log.info("listening on %s:%d", *addr)
    srv.serve_forever()


if __name__ == "__main__":
    main()
