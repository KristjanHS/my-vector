import http.client
import json
import socket
import threading
import time
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


@pytest.mark.parametrize("text", ["a" * 400 + ", short.", "x, " + "b" * 400 + ", y."])
def test_split_sentences_long_clause_mid_sentence_is_cut(text):
    parts = server.split_sentences(text)
    assert all(len(p) <= server.MAX_SENTENCE for p in parts)
    assert "".join(parts).replace(" ", "") == text.replace(" ", "")


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
    """One 0.1 s tone per call at `rate`; records (voice_id, text, slow). Call number `fail_at` (1-based) raises."""

    def __init__(self, rate, fail_at=0):
        self.rate, self.fail_at, self.calls = rate, fail_at, []

    def synth(self, voice_id, text, slow):
        if len(self.calls) + 1 == self.fail_at:
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
            server.make_handler(synth, lambda: {"voices": sorted(server.VOICES)}),
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
    port = serve({"piper": FakeEngine(22050, fail_at=1), "kokoro": FakeEngine(24000)})
    r, data = post(port, {"text": "Hello.", "voice": "cori"})
    assert r.status == 500 and json.loads(data)["error"] == "synthesis failed"


def test_health(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request("GET", "/health")
    assert json.loads(c.getresponse().read())["voices"] == ["af_bella", "cori"]


def raw_chunks(port, body):
    """POST /tts over a raw socket -> list of HTTP chunk sizes (http.client hides the framing)."""
    data = json.dumps(body).encode()
    with socket.create_connection(("127.0.0.1", port), timeout=10) as s:
        s.sendall(
            b"POST /tts HTTP/1.1\r\nHost: x\r\nConnection: close\r\nContent-Length: %d\r\n\r\n%s"
            % (len(data), data)
        )
        raw = b""
        while chunk := s.recv(65536):
            raw += chunk
    rest = raw.split(b"\r\n\r\n", 1)[1]
    sizes = []
    while True:
        line, rest = rest.split(b"\r\n", 1)
        n = int(line, 16)
        sizes.append(n)
        if n == 0:
            return sizes
        rest = rest[n + 2 :]


def test_tts_http_chunk_per_sentence(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    assert raw_chunks(port, {"text": "One. Two! Three?", "voice": "cori"}) == [
        3200,
        3200,
        3200,
        0,
    ]


def test_tts_mid_stream_failure_leaves_stream_unterminated(serve):
    port = serve({"piper": FakeEngine(22050, fail_at=2), "kokoro": FakeEngine(24000)})
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request("POST", "/tts", body=json.dumps({"text": "One. Two.", "voice": "cori"}))
    r = c.getresponse()
    assert r.status == 200
    with pytest.raises(http.client.IncompleteRead):
        r.read()


def test_tts_keep_alive_after_400_and_200(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    statuses = []
    for body in ({"text": ""}, {"text": "Hi."}, {"text": "Hi."}):
        c.request("POST", "/tts", body=json.dumps(body))
        r = c.getresponse()
        r.read()
        statuses.append(r.status)
    assert statuses == [400, 200, 200]
    assert (
        c.sock is not None
    )  # same socket throughout: http.client drops it when the server closes


def test_tts_chunked_request_body_is_411(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request("POST", "/tts", body=iter([b'{"text": "hi"}']), encode_chunked=True)
    assert c.getresponse().status == 411



class FakeKokoro(FakeEngine):
    """FakeEngine with KokoroEngine's load/close surface; `gate` (an Event) holds each synth until set; `events` logs order."""

    def __init__(self, device, events, gate=None):
        super().__init__(24000)
        self.device, self.events, self.gate, self.closed = device, events, gate, False

    def synth(self, voice_id, text, slow):
        self.events.append("synth start")
        if self.gate:
            self.gate.wait(10)
        out = super().synth(voice_id, text, slow)
        self.events.append("synth end")
        return out

    def close(self):
        self.events.append("close")
        self.closed = True


@pytest.fixture
def lazy():
    """-> (LazyKokoro, Synth, made, events): every factory call appends a FakeKokoro to made, all log into events."""
    made, events, lock = [], [], threading.Lock()

    def factory(device):
        events.append("load")
        made.append(FakeKokoro(device, events))
        return made[-1]

    k = server.LazyKokoro(factory, "cuda", lock)
    return k, server.Synth({"piper": FakeEngine(22050), "kokoro": k}, {}, lock), made, events


def test_unselected_request_loads_once_and_frees_at_request_end(lazy):
    k, synth, made, events = lazy
    assert not k.loaded
    assert len(list(synth.stream("af_bella", "One. Two.", 1.0))) == 2
    assert events == ["load", "synth start", "synth end", "synth start", "synth end", "close"]
    assert not k.loaded
    list(synth.stream("cori", "Hi.", 1.0))  # a Piper request never touches Kokoro
    assert len(made) == 1


def test_selected_loads_warm_now_stays_resident_and_frees_on_deselect(lazy):
    k, synth, made, events = lazy
    k.set_pinned(True)
    assert k.loaded and made[0].calls == [("af_bella", "Hello there.", 1.0)]
    list(synth.stream("af_bella", "Hi.", 1.0))
    assert k.loaded and len(made) == 1 and not made[0].closed
    k.set_pinned(False)
    assert not k.loaded and made[0].closed


def test_deselect_mid_synth_frees_after_the_request(lazy):
    k, synth, made, events = lazy
    k.set_pinned(True)
    made[0].gate = gate = threading.Event()
    t = threading.Thread(target=lambda: list(synth.stream("af_bella", "One. Two.", 1.0)))
    t.start()
    while events[-1] != "synth start":
        time.sleep(0.001)
    unpin = threading.Thread(target=k.set_pinned, args=(False,))
    unpin.start()
    unpin.join(0.2)  # blocked on the lock behind the sentence
    gate.set()
    t.join(5)
    unpin.join(5)
    warm = ["load", "synth start", "synth end"]
    assert events == warm + ["synth start", "synth end"] * 2 + ["close"]  # freed after the 2nd sentence, not between
    assert not k.loaded


def test_handler_frees_unselected_kokoro_after_the_response(lazy):
    k, synth, made, events = lazy
    srv = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(synth, lambda: {}))
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        r, data = post(srv.server_address[1], {"text": "One. Two.", "voice": "af_bella"})
        assert r.status == 200 and len(data) == 2 * 1600 * 2
        assert events[-1] == "close" and not k.loaded
    finally:
        srv.shutdown()
        srv.server_close()


@pytest.mark.parametrize(
    "text, pinned, bad",
    [
        ('{"t1": "af_bella", "slow": 1.2}', True, False),
        ('{"t1": "cori", "t2": "af_bella"}', True, False),
        ('{"t1": "stock", "t2": "cori"}', False, False),
        ("{}", False, False),
        ('{"t1": "af_bella", "t2": "nobody"}', False, True),  # settings.go: any invalid field -> all defaults
        ('{"t1": "af_bella", "slow": 9}', False, True),
        ('{"t1": "af_bella"', False, True),
        ('["af_bella"]', False, True),
    ],
)
def test_kokoro_selected_reads_t1_t2_and_invalid_is_defaults(tmp_path, text, pinned, bad):
    f = tmp_path / "myvectorConfig.json"
    f.write_text(text)
    got, err = server.kokoro_selected(f)
    assert got is pinned and bool(err) is bad


def test_kokoro_selected_missing_file_is_defaults(tmp_path):
    assert server.kokoro_selected(tmp_path / "none.json") == (False, None)


def test_watcher_pins_on_select_and_frees_on_garbled(lazy, tmp_path, caplog):
    k, synth, made, events = lazy
    f = tmp_path / "myvectorConfig.json"
    f.write_text('{"t1": "cori"}')
    w = server.ConfigWatcher(f, k)
    w.poll()
    assert not k.pinned and not k.loaded
    f.write_text('{"t2": "af_bella"}')
    w.poll()
    assert k.pinned and k.loaded
    f.write_text("garbled{")
    w.poll()
    w.poll()  # unchanged file: no second warning
    assert not k.pinned and not k.loaded
    assert sum("using defaults" in r.message for r in caplog.records) == 1
