"""my-vector TTS service: POST /tts {"text", "voice", "slow"} -> chunked 16 kHz s16le mono PCM, one chunk per sentence.

GET /health -> {"voices": [...], "device": ...}. Models load + warm before the port opens (refused = still starting).
Env: TTS_BIND (0.0.0.0), TTS_PORT (8091), TTS_PIPER_DIR (Piper .onnx dir), TTS_DEVICE (cuda|cpu), HF_HOME.
"""

import itertools
import json
import logging
import os
import re
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import numpy as np

RATE = 16000
SLOW_MIN, SLOW_MAX = 0.5, 2.0
MAX_TEXT = 5000
MAX_SENTENCE = 300  # longer sentences split again at , ; :
PEAK = 0.97  # per-sentence limiter ceiling (float full scale)
GAINS_FILE = Path(__file__).with_name("gains.json")  # tts/calibrate.py writes it

# voice name -> (engine, engine voice id)
VOICES = {"cori": ("piper", "en_GB-cori-high"), "af_bella": ("kokoro", "af_bella")}

log = logging.getLogger("myvector-tts")

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


def to_pcm16(audio, rate, gain=1.0):
    """float audio at `rate` -> 16 kHz s16le bytes, times `gain`, scaled down to PEAK if it would clip."""
    x = np.asarray(audio, dtype=np.float32).reshape(-1)
    if rate != RATE and x.size:
        import soxr

        x = soxr.resample(x, rate, RATE, quality="HQ").astype(np.float32)
    x = x * np.float32(gain)
    peak = float(np.max(np.abs(x))) if x.size else 0.0
    if peak > PEAK:
        x *= np.float32(PEAK / peak)
    return (np.clip(x, -1.0, 1.0) * 32767.0).astype("<i2").tobytes()


def load_gains(path=GAINS_FILE):
    """Per-voice linear gains from gains.json ({"cori": {"gain_db": -1.5}, ...}); missing voice = 0 dB."""
    try:
        raw = json.loads(path.read_text())
    except FileNotFoundError:
        log.warning("%s missing: all voices at 0 dB (run tts/calibrate.py)", path.name)
        raw = {}
    return {v: 10 ** (float(raw.get(v, {}).get("gain_db", 0.0)) / 20) for v in VOICES}


def parse_request(body):
    """Validate a /tts JSON body -> (text, voice, slow). Raises ValueError with a client-facing message."""
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
    voice = req.get("voice", "cori")
    if voice not in VOICES:
        raise ValueError(f"voice: one of {sorted(VOICES)}")
    try:
        slow = float(req.get("slow", 1.0))
    except (TypeError, ValueError):
        raise ValueError("slow: number required") from None
    if slow != slow:  # NaN
        raise ValueError("slow: number required")
    return text, voice, min(max(slow, SLOW_MIN), SLOW_MAX)


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


class Synth:
    """Voice name -> engine; one synthesis at a time (engines are not thread-safe)."""

    def __init__(self, engines, gains):
        self.engines = engines  # {"piper": engine, "kokoro": engine}
        self.gains = gains
        self._lock = threading.Lock()

    def sentence_pcm(self, voice, text, slow):
        engine, voice_id = VOICES[voice]
        with self._lock:
            audio, rate = self.engines[engine].synth(voice_id, text, slow)
        return to_pcm16(audio, rate, self.gains.get(voice, 1.0))

    def stream(self, voice, text, slow):
        for s in split_sentences(text):
            pcm = self.sentence_pcm(voice, s, slow)
            if pcm:
                yield pcm

    def warm(self):
        for voice in VOICES:
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
                self._json(200, info)
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
                text, voice, slow = parse_request(body)
            except ValueError as e:
                self._json(400, {"error": str(e)})
                return
            # Headers wait for the first sentence: time-to-first-byte = time-to-first-audio, and a failing voice is a 500.
            chunks = synth.stream(voice, text, slow)
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
    kokoro = KokoroEngine(os.environ.get("TTS_DEVICE", "cuda"))
    synth = Synth({"piper": PiperEngine(piper_dir), "kokoro": kokoro}, load_gains())
    synth.warm()
    log.info("warm: voices %s, kokoro on %s", sorted(VOICES), kokoro.device)
    if warm_only:
        return
    info = {"voices": sorted(VOICES), "device": kokoro.device, "rate": RATE}
    addr = (
        os.environ.get("TTS_BIND", "0.0.0.0"),
        int(os.environ.get("TTS_PORT", "8091")),
    )
    srv = ThreadingHTTPServer(addr, make_handler(synth, info))
    log.info("listening on %s:%d", *addr)
    srv.serve_forever()


if __name__ == "__main__":
    main()
