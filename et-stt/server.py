"""my-vector Estonian STT service: POST /stt (16 kHz s16le mono PCM body) -> {"text"}; GET /health -> {"lang", "loaded", "device"}.

TalTech turbo-et (faster-whisper) holds the GPU only while myvectorConfig.json "lang" is et-EE; /stt under en-US -> 409.
Env: STT_BIND (127.0.0.1), STT_PORT (8092), STT_MODEL (CT2 dir), STT_DEVICE (cuda), STT_COMPUTE (int8_float16), STT_CONFIG (polled every 5 s).
"""

import gc
import json
import logging
import os
import sys
import threading
import time
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import numpy as np

RATE = 16000
MAX_SECONDS = 60
BEAM = 5
ENGLISH, ESTONIAN = "en-US", "et-EE"  # chipper/pkg/myvector/lang
REPO = Path(__file__).resolve().parents[1]
CONFIG = REPO / "chipper/myvectorConfig.json"
MODEL = REPO / "robot/work/et-stt/models/taltech-et/ct2"
CONFIG_POLL_S = 5

log = logging.getLogger("myvector-stt")


class Inactive(Exception):
    """/stt while the switch is not et-EE."""


def config_lang(path):
    """(lang, why-default or None) as lang.go load: missing file or no "lang" -> en-US; unreadable/invalid -> en-US + why."""
    try:
        cfg = json.loads(Path(path).read_text())
        if not isinstance(cfg, dict):
            raise ValueError("not a JSON object")
        lang = cfg.get("lang", ENGLISH)
        if lang not in (ENGLISH, ESTONIAN):
            raise ValueError(f"lang {lang!r}")
    except FileNotFoundError:
        return ENGLISH, None
    except (OSError, ValueError) as e:
        return ENGLISH, f"{path}: {e}"
    return lang, None


def whisper_factory(model, device, compute):
    from faster_whisper import WhisperModel

    return WhisperModel(str(model), device=device, compute_type=compute)


class Recognizer:
    """The model is resident iff active (lang et-EE); every load/free/transcribe runs under one lock."""

    def __init__(self, factory, model, device, compute):
        self._factory, self._args = factory, (model, device, compute)
        self._lock, self._model, self.active = threading.Lock(), None, False
        self.device = device

    @property
    def loaded(self):
        return self._model is not None

    def _load(self):  # caller holds the lock
        t = time.monotonic()
        try:
            self._model = self._factory(*self._args)
        except Exception as e:
            log.error("model load failed:\n%s", traceback.format_exc())
            msg = f"model load failed: {e!r}"
        else:
            log.info("model loaded on %s in %.1f s", self.device, time.monotonic() - t)
            return
        raise RuntimeError(
            msg
        )  # outside except: the traceback's frames would pin a half-built model

    def set_active(self, active):
        """et-EE -> load now (a failure raises; the next poll or request retries); en-US -> free now."""
        with self._lock:
            self.active = active
            if active and self._model is None:
                self._load()
            elif not active and self._model is not None:
                self._model = None
                gc.collect()
                log.info("model freed")

    def transcribe(self, pcm):
        audio = np.frombuffer(pcm, dtype="<i2").astype(np.float32) / 32768.0
        with self._lock:
            if not self.active:
                raise Inactive
            if self._model is None:
                self._load()
            model = self._model
            assert model is not None  # _load sets it or raises
            if not audio.size:
                return ""
            segments, _ = model.transcribe(audio, language="et", beam_size=BEAM)
            return "".join(
                s.text for s in segments
            ).strip()  # segments is lazy: decode inside the lock


class ConfigWatcher:
    """Polls wire-pod's settings (stat key: mtime, inode, size) and (de)activates the recognizer on a change."""

    def __init__(self, path, recognizer):
        self.path, self.rec, self._key = Path(path), recognizer, ()
        self._lock = threading.Lock()

    def poll(self):
        with self._lock:
            try:
                st = self.path.stat()
                key = (st.st_mtime_ns, st.st_ino, st.st_size)
            except OSError:
                key = None
            if key != self._key:
                lang, err = config_lang(self.path)
                if err:
                    log.warning("settings unreadable, using %s: %s", ENGLISH, err)
                self.rec.set_active(lang == ESTONIAN)
                self._key = key  # only after success: a failed load retries next poll

    def poll_logged(self):
        try:
            self.poll()
        except Exception:
            log.exception("model load/free failed; retrying next poll")

    def run(self, interval=CONFIG_POLL_S):
        while True:
            time.sleep(interval)
            self.poll_logged()


def make_handler(rec, watcher):
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        timeout = 30

        def log_message(self, format, *args):  # noqa: A002 - base class name
            log.info("%s %s", self.address_string(), format % args)

        def _json(self, code, obj):
            body = json.dumps(obj, ensure_ascii=False).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/health":
                self._json(
                    200,
                    {
                        "lang": ESTONIAN if rec.active else ENGLISH,
                        "loaded": rec.loaded,
                        "device": rec.device,
                    },
                )
            else:
                self._json(404, {"error": "not found"})

        def do_POST(self):
            client_close = self.close_connection
            self.close_connection = True  # until the body is read: an unread body would parse as the next request
            if self.path != "/stt":
                self._json(404, {"error": "not found"})
                return
            try:
                n = int(self.headers.get("Content-Length", -1))
            except ValueError:
                n = -1
            if n > 2 * RATE * MAX_SECONDS:
                self._json(413, {"error": f"over {MAX_SECONDS} s of audio"})
                return
            if n < 0 or n % 2:
                self._json(400, {"error": "bad Content-Length (whole s16le samples)"})
                return
            pcm = self.rfile.read(n)
            self.close_connection = client_close
            watcher.poll_logged()  # a request right after the switch sees it now, not at the next poll (plan E21)
            t = time.monotonic()
            try:
                text = rec.transcribe(pcm)
            except Inactive:
                self._json(409, {"error": f"lang is not {ESTONIAN}"})
                return
            except Exception as e:
                log.exception("transcription failed")
                self._json(500, {"error": str(e)})
                return
            log.info(
                "%.2f s audio -> %.2f s: %s", n / 2 / RATE, time.monotonic() - t, text
            )
            self._json(200, {"text": text})

    return Handler


def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
        stream=sys.stdout,
    )
    rec = Recognizer(
        whisper_factory,
        os.environ.get("STT_MODEL", MODEL),
        os.environ.get("STT_DEVICE", "cuda"),
        os.environ.get("STT_COMPUTE", "int8_float16"),
    )
    if "--warm-only" in sys.argv[1:]:  # et-stt/setup.sh: prove the model loads and decodes
        rec.set_active(True)
        print(repr(rec.transcribe(np.zeros(RATE, "<i2").tobytes())))
        return
    watcher = ConfigWatcher(os.environ.get("STT_CONFIG", CONFIG), rec)
    watcher.poll_logged()
    threading.Thread(target=watcher.run, daemon=True).start()
    addr = (
        os.environ.get("STT_BIND", "127.0.0.1"),
        int(os.environ.get("STT_PORT", "8092")),
    )
    srv = ThreadingHTTPServer(addr, make_handler(rec, watcher))
    log.info(
        "listening on %s:%d (model %s)", *addr, "loaded" if rec.loaded else "not loaded"
    )
    srv.serve_forever()


if __name__ == "__main__":
    main()
