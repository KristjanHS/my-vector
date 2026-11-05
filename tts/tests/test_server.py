import http.client
import json
import threading
from http.server import ThreadingHTTPServer

import numpy as np
import pytest
import server


def test_split_sentences_ends_newlines_and_abbreviations():
    text = "Hi there! Dr. Smith says hello.\nIs it 12.5 degrees? Yes…  Done"
    assert server.split_sentences(text) == [
        "Hi there!",
        "Dr. Smith says hello.",
        "Is it 12.5 degrees?",
        "Yes…",
        "Done",
    ]


def test_split_sentences_long_sentence_split_under_cap():
    long = ", ".join(["word " * 20] * 6) + "."
    parts = server.split_sentences(long)
    assert len(parts) > 1
    assert all(len(p) <= server.MAX_SENTENCE for p in parts)
    assert " ".join(parts).split() == long.split()


def test_split_sentences_no_break_points_hard_cut():
    parts = server.split_sentences("a" * 700)
    assert [len(p) for p in parts] == [300, 300, 100]


def test_to_pcm16_resamples_to_16k():
    pcm = server.to_pcm16(np.zeros(24000, np.float32), 24000)
    assert len(pcm) == 16000 * 2


def test_to_pcm16_gain_and_peak_limit():
    t = np.arange(16000) / 16000
    quiet = 0.1 * np.sin(2 * np.pi * 440 * t)
    out = np.frombuffer(server.to_pcm16(quiet, 16000, gain=2.0), "<i2")
    assert abs(out.max() / 32767 - 0.2) < 0.01
    loud = np.frombuffer(server.to_pcm16(quiet, 16000, gain=20.0), "<i2")
    assert abs(loud.max() / 32767 - server.PEAK) < 0.01


def test_parse_request_defaults_and_clamp():
    assert server.parse_request(b'{"text": "hi"}') == ("hi", "cori", 1.0)
    assert server.parse_request(b'{"text": "hi", "voice": "af_bella", "slow": 9}')[
        1:
    ] == ("af_bella", server.SLOW_MAX)
    assert server.parse_request(b'{"text": "hi", "slow": 0.1}')[2] == server.SLOW_MIN


@pytest.mark.parametrize(
    "body",
    [
        b"not json",
        b"[]",
        b'{"text": "  "}',
        b'{"text": "hi", "voice": "x"}',
        b'{"text": "hi", "slow": "fast"}',
        b'{"text": "hi", "slow": NaN}',
    ],
)
def test_parse_request_rejects(body):
    with pytest.raises(ValueError):
        server.parse_request(body)


def test_load_gains_missing_file_is_unity(tmp_path):
    assert server.load_gains(tmp_path / "none.json") == {v: 1.0 for v in server.VOICES}


def test_load_gains_db_to_linear(tmp_path):
    f = tmp_path / "g.json"
    f.write_text(json.dumps({"cori": {"gain_db": 6.0206}}))
    g = server.load_gains(f)
    assert abs(g["cori"] - 2.0) < 1e-3 and g["af_bella"] == 1.0


class FakeEngine:
    """One 0.1 s tone per call at `rate`; records (voice_id, text, slow)."""

    def __init__(self, rate, fail=False):
        self.rate, self.fail, self.calls = rate, fail, []

    def synth(self, voice_id, text, slow):
        if self.fail:
            raise RuntimeError("engine down")
        self.calls.append((voice_id, text, slow))
        return 0.1 * np.ones(self.rate // 10, np.float32), self.rate


@pytest.fixture
def serve():
    servers = []

    def start(engines):
        synth = server.Synth(engines, {v: 1.0 for v in server.VOICES})
        srv = ThreadingHTTPServer(
            ("127.0.0.1", 0),
            server.make_handler(synth, {"voices": sorted(server.VOICES)}),
        )
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        servers.append(srv)
        return srv.server_address[1]

    yield start
    for s in servers:
        s.shutdown()
        s.server_close()


def post(port, body):
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request(
        "POST", "/tts", body=json.dumps(body) if not isinstance(body, bytes) else body
    )
    r = c.getresponse()
    return r, r.read()


def test_tts_streams_one_chunk_per_sentence(serve):
    piper, kokoro = FakeEngine(22050), FakeEngine(24000)
    port = serve({"piper": piper, "kokoro": kokoro})
    r, data = post(port, {"text": "One. Two! Three?", "voice": "af_bella", "slow": 1.2})
    assert r.status == 200 and r.getheader("Transfer-Encoding") == "chunked"
    assert r.getheader("X-Audio-Format") == "s16le;rate=16000;channels=1"
    assert [c[1] for c in kokoro.calls] == [
        "One.",
        "Two!",
        "Three?",
    ] and not piper.calls
    assert kokoro.calls[0][0] == "af_bella" and kokoro.calls[0][2] == 1.2
    assert len(data) == 3 * 1600 * 2  # three 0.1 s sentences at 16 kHz s16le


def test_tts_bad_request_and_unknown_path(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    r, data = post(port, {"text": "hi", "voice": "nobody"})
    assert r.status == 400 and "voice" in json.loads(data)["error"]
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request("GET", "/nope")
    assert c.getresponse().status == 404


def test_tts_engine_failure_is_500(serve):
    port = serve({"piper": FakeEngine(22050, fail=True), "kokoro": FakeEngine(24000)})
    r, data = post(port, {"text": "Hello.", "voice": "cori"})
    assert r.status == 500 and json.loads(data)["error"] == "synthesis failed"


def test_health(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request("GET", "/health")
    assert json.loads(c.getresponse().read())["voices"] == ["af_bella", "cori"]
