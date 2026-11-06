import contextlib
import gc
import http.client
import inspect
import json
import socket
import threading
import time
import types
import weakref
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


def test_split_sentences_estonian_abbreviations_and_ordinals():
    text = "Tule nt. homme. 2026. aastal oli soe. See on 5. Järgmine."
    assert server.split_sentences(text, "et") == [
        "Tule nt. homme.",
        "2026. aastal oli soe.",
        "See on 5.",
        "Järgmine.",
    ]
    assert server.split_sentences("It was 2026. then rain.") == ["It was 2026.", "then rain."]


def test_stream_normalises_estonian_voice_only():
    piper = FakeEngine(22050)
    synth = server.Synth({"piper": piper, "kokoro": FakeEngine(24000)}, {})
    for voice in ("mari", "cori"):
        list(synth.stream(voice, "Kell on 14.05.", 1.0))
    assert [c[:2] for c in piper.calls] == [
        ("et_EE-news-medium@Mari", "Kell on 14 null 5."),
        ("en_GB-cori-high", "Kell on 14.05."),
    ]


def test_stream_translates_robot_lines_on_et_ee_only():
    piper = FakeEngine(22050)
    synth = server.Synth({"piper": piper, "kokoro": FakeEngine(24000)}, {})
    for robot_lang in (None, "en-US", "et-EE"):
        list(synth.stream("mari", "09:05", 1.0, 0.0, robot_lang))
    assert [c[1] for c in piper.calls] == ["9 null 5", "9 null 5", "Kell on 9 null 5"]


def test_piper_engine_speaker_from_voice_id_one_load_per_model():
    loads, configs = [], []

    class Voice:
        config = types.SimpleNamespace(speaker_id_map={"Mari": 2}, sample_rate=22050)

        def synthesize(self, text, syn_config):
            configs.append(syn_config)
            yield types.SimpleNamespace(audio_float_array=np.zeros(4, np.float32), sample_rate=22050)

    eng = server.PiperEngine.__new__(server.PiperEngine)
    eng._voices, eng._dir, eng._cfg = {}, server.Path("/m"), dict
    eng._load = lambda path: loads.append(path) or Voice()
    eng.synth("et_EE-news-medium@Mari", "Tere.", 1.2)
    eng.synth("et_EE-news-medium", "Tere.", 1.0)
    assert loads == ["/m/et_EE-news-medium.onnx"]
    assert configs == [{"speaker_id": 2, "length_scale": 1.2}, {"speaker_id": None, "length_scale": 1.0}]


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


def test_trim_silence_keeps_the_head_and_tail_pads_around_speech():
    rate = 24000
    x = np.zeros(int(1.5 * rate), np.float32)
    x[int(0.45 * rate) : int(0.70 * rate)] = 0.1  # -20 dBFS speech, Kokoro-like 0.45 s lead / 0.8 s tail
    x[: int(0.1 * rate)] = 0.001  # -60 dBFS hiss stays under the floor
    y = server.trim_silence(x, rate)
    assert y.size == int(0.55 * rate)  # 0.05 head + 0.25 speech + 0.25 tail
    assert y[int(0.05 * rate) - 1] == 0 and y[int(0.05 * rate)] == np.float32(0.1)
    assert y[int(0.30 * rate) - 1] == np.float32(0.1) and not y[int(0.30 * rate) :].any()


def test_trim_silence_leaves_all_quiet_and_tiny_audio_alone():
    quiet = np.full(2400, 0.001, np.float32)
    assert server.trim_silence(quiet, 24000) is quiet
    tiny = np.ones(100, np.float32)
    assert server.trim_silence(tiny, 24000) is tiny


def test_kokoro_synth_trims_its_silence():
    lead, tail = np.zeros(int(0.45 * 24000), np.float32), np.zeros(int(0.82 * 24000), np.float32)
    speech = np.full(2400, 0.1, np.float32)
    part = types.SimpleNamespace(audio=types.SimpleNamespace(cpu=lambda: types.SimpleNamespace(numpy=lambda: np.r_[lead, speech, tail])))
    eng = object.__new__(server.KokoroEngine)
    eng._pipes = {"a": lambda text, voice, speed: [part]}
    eng._torch = types.SimpleNamespace(inference_mode=contextlib.nullcontext)
    audio, rate = eng.synth("af_bella", "Hi.", 1.0)
    assert rate == 24000 and audio.size == int(0.30 * 24000) + speech.size


def test_limiter_only_dips_around_the_peak():
    """A loud burst is held to PEAK; quiet audio 0.5 s later keeps its full gain (not a whole-sentence scale-down)."""
    t = np.arange(16000) / 16000
    x = 0.1 * np.sin(2 * np.pi * 440 * t)
    x[:800] *= 30  # 3.0 peak burst, first 50 ms
    out = np.frombuffer(server.to_pcm16(x, 16000), "<i2") / 32767
    assert np.abs(out).max() <= server.PEAK + 1e-4
    assert abs(np.abs(out[8000:]).max() - 0.1) < 0.002


def test_limit_holds_the_ceiling_itself_not_via_the_final_clip():
    """limit() alone stays under PEAK (to_pcm16's clip would hide a no-op or overshooting limiter = hard clipping)."""
    rng = np.random.default_rng(0)
    x = (0.2 * rng.standard_normal(4000)).astype(np.float32)
    for i in (0, 15, 16, 31, 32, 33, 63, 64, 99, 3999):  # block edges and the padded tail
        x[i] = 3.0
    y = server.limit(x)
    assert np.abs(y).max() <= server.PEAK + 1e-6
    assert not np.allclose(y, np.clip(x, -server.PEAK, server.PEAK), atol=1e-3)  # gain dips, not clipping


def test_parse_request_defaults_and_clamp():
    assert server.parse_request(b'{"text": "hi"}') == ("hi", "af_bella", 1.0, 0.0, None)
    assert server.parse_request(b'{"text": "hi", "lang": "et-EE"}')[4] == "et-EE"
    assert server.parse_request(b'{"text": "hi", "lang": "en-US"}')[4] == "en-US"
    assert server.parse_request(b'{"text": "hi", "voice": "cori", "slow": 9}')[
        1:3
    ] == ("cori", server.SLOW_MAX)
    assert server.parse_request(b'{"text": "hi", "slow": 0.1}')[2] == server.SLOW_MIN
    assert server.parse_request(b'{"text": "hi", "boost_db": 99}')[3] == server.BOOST_MAX
    assert server.parse_request(b'{"text": "hi", "boost_db": -3}')[3] == 0.0


@pytest.mark.parametrize(
    "body",
    [
        b"not json",
        b"[]",
        b'{"text": "  "}',
        b'{"text": "hi", "voice": "x"}',
        b'{"text": "hi", "slow": "fast"}',
        b'{"text": "hi", "slow": NaN}',
        b'{"text": "hi", "boost_db": "max"}',
        b'{"text": "hi", "boost_db": NaN}',
        b'{"text": "hi", "lang": "fi-FI"}',
        b'{"text": "hi", "lang": ["et-EE"]}',
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


def test_tts_lang_reaches_the_stream(serve):
    piper = FakeEngine(22050)
    port = serve({"piper": piper, "kokoro": FakeEngine(24000)})
    assert post(port, {"text": "Ready", "voice": "mari", "lang": "et-EE"})[0].status == 200
    assert post(port, {"text": "Ready", "voice": "mari"})[0].status == 200
    r, data = post(port, {"text": "Ready", "voice": "mari", "lang": "xx"})
    assert r.status == 400 and "lang" in json.loads(data)["error"]
    assert [c[1] for c in piper.calls] == ["Valmis", "Ready"]


def test_tts_engine_failure_is_500(serve):
    port = serve({"piper": FakeEngine(22050, fail_at=1), "kokoro": FakeEngine(24000)})
    r, data = post(port, {"text": "Hello.", "voice": "cori"})
    assert r.status == 500 and json.loads(data)["error"] == "synthesis failed"


def test_health(serve):
    port = serve({"piper": FakeEngine(22050), "kokoro": FakeEngine(24000)})
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    c.request("GET", "/health")
    assert json.loads(c.getresponse().read())["voices"] == ["af_bella", "cori", "mari"]


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




class Model:
    """Stands in for the GPU model: a test checks it is garbage once freed."""


class FakeKokoro(FakeEngine):
    """FakeEngine with KokoroEngine's load/close surface; `gate` (an Event) holds each synth until set; `events` logs order."""

    def __init__(self, device, events, fail_at=0):
        super().__init__(24000, fail_at)
        self.device, self.events, self.gate, self.closed = device, events, None, False
        self.model = Model()
        self.ref = weakref.ref(self.model)

    def synth(self, voice_id, text, slow):
        model = self.model  # a frame local, as Kokoro's pipeline frames hold the model  # noqa: F841
        self.events.append("synth start")
        if self.gate:
            self.gate.wait(10)
        out = super().synth(voice_id, text, slow)
        self.events.append("synth end")
        return out

    def close(self):
        self.events.append("close")
        self.closed, self.model = True, None


def test_kokoro_engine_has_the_surface_the_fakes_stand_in_for():
    # B1 smoke: a module function inserted mid-class orphaned KokoroEngine.synth while every faked test stayed green
    for name in ("synth", "close"):
        assert inspect.signature(getattr(server.KokoroEngine, name)) == inspect.signature(getattr(FakeKokoro, name))


@pytest.fixture
def lazy():
    """LazyKokoro + Synth over FakeKokoros. f.made: engines; f.events: order log; f.devices: devices asked of the factory;
    f.reclaims: (Synth lock held?, live models of closed engines) per reclaim; f.fail_loads / f.fail_at / f.fallback steer loads."""
    f = types.SimpleNamespace(made=[], events=[], devices=[], reclaims=[], fail_loads=0, fail_at=0, fallback=False)
    lock = threading.Lock()

    def factory(device):
        f.devices.append(device)
        if f.fail_loads:
            f.fail_loads -= 1
            raise RuntimeError("load down")
        f.events.append("load")
        f.made.append(FakeKokoro("cpu" if f.fallback else device, f.events, f.fail_at))
        return f.made[-1]

    def reclaim():
        gc.collect()
        f.reclaims.append((lock.locked(), sum(1 for e in f.made if e.closed and e.ref() is not None)))

    f.k = server.LazyKokoro(factory, "cuda", lock, reclaim)
    f.synth = server.Synth({"piper": FakeEngine(22050), "kokoro": f.k}, {}, lock)
    return f


def wait_until(cond, timeout=5):
    end = time.monotonic() + timeout
    while not cond():
        assert time.monotonic() < end, "timed out"
        time.sleep(0.001)


def test_unselected_request_loads_once_and_frees_at_request_end(lazy):
    assert not lazy.k.loaded
    assert len(list(lazy.synth.stream("af_bella", "One. Two.", 1.0))) == 2
    assert lazy.events == ["load", "synth start", "synth end", "synth start", "synth end", "close"]
    assert not lazy.k.loaded and lazy.reclaims == [(False, 0)]  # reclaimed outside the lock, model gone
    list(lazy.synth.stream("cori", "Hi.", 1.0))  # a Piper request never touches Kokoro
    assert len(lazy.made) == 1


def test_overlapping_unselected_requests_share_one_load(lazy):
    a, b = lazy.synth.stream("af_bella", "One. Two.", 1.0), lazy.synth.stream("af_bella", "Three.", 1.0)
    next(a)
    next(b)
    list(a)
    assert lazy.k.loaded  # b still holds it
    list(b)
    assert not lazy.k.loaded and lazy.events.count("load") == 1 and lazy.events.count("close") == 1


def test_selected_loads_warm_now_stays_resident_and_frees_on_deselect(lazy):
    k, made = lazy.k, lazy.made
    k.set_pinned(True)
    assert k.loaded and made[0].calls == [("af_bella", "Hello there.", 1.0)]
    list(lazy.synth.stream("af_bella", "Hi.", 1.0))
    assert k.loaded and len(made) == 1 and not made[0].closed
    k.set_pinned(False)
    assert not k.loaded and made[0].closed and lazy.reclaims == [(False, 0)]


def test_deselect_mid_synth_frees_after_the_request(lazy):
    k, events = lazy.k, lazy.events
    k.set_pinned(True)
    lazy.made[0].gate = gate = threading.Event()
    t = threading.Thread(target=lambda: list(lazy.synth.stream("af_bella", "One. Two.", 1.0)))
    t.start()
    wait_until(lambda: events[-1] == "synth start")
    unpin = threading.Thread(target=k.set_pinned, args=(False,))
    unpin.start()
    unpin.join(0.2)  # blocked on the lock behind the sentence
    gate.set()
    t.join(5)
    unpin.join(5)
    warm = ["load", "synth start", "synth end"]
    assert events == warm + ["synth start", "synth end"] * 2 + ["close"]  # freed after the 2nd sentence, not between
    assert not k.loaded


def test_failed_synth_still_frees_the_model(lazy):
    lazy.fail_at = 1
    with pytest.raises(RuntimeError, match="engine down"):
        list(lazy.synth.stream("af_bella", "One.", 1.0))
    assert not lazy.k.loaded and lazy.k._requests == 0
    assert lazy.reclaims == [(False, 0)]  # no traceback frame kept the model alive through the reclaim


def test_failed_pin_load_is_retried_by_the_watcher(lazy, tmp_path):
    f = tmp_path / "myvectorConfig.json"
    f.write_text('{"t1": "af_bella"}')
    w = server.ConfigWatcher(f, lazy.k)
    lazy.fail_loads = 1
    w.poll_logged()  # logs, does not raise
    assert not lazy.k.pinned and not lazy.k.loaded
    w.poll_logged()  # file unchanged, still retried
    assert lazy.k.pinned and lazy.k.loaded


def test_cpu_fallback_does_not_stick(lazy):
    lazy.fallback = True
    list(lazy.synth.stream("af_bella", "Hi.", 1.0))
    assert lazy.k.device == "cpu"
    lazy.fallback = False
    list(lazy.synth.stream("af_bella", "Hi.", 1.0))
    assert lazy.devices == ["cuda", "cuda"] and lazy.k.device == "cuda"


def test_handler_frees_unselected_kokoro_after_the_response(lazy):
    srv = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(lazy.synth, lambda: {}))
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        r, data = post(srv.server_address[1], {"text": "One. Two.", "voice": "af_bella"})
        assert r.status == 200 and len(data) == 2 * 1600 * 2
        assert lazy.events[-1] == "close" and not lazy.k.loaded
    finally:
        srv.shutdown()
        srv.server_close()


@pytest.mark.parametrize(
    "text, pinned, bad",
    [
        ('{"t1": "af_bella", "slow": 1.2}', True, False),
        ('{"t1": "cori", "t2": "af_bella"}', True, False),
        ('{"t1": "stock", "t2": "cori"}', False, False),
        ("{}", True, False),  # default voice af_bella
        ('{"t1": "cori", "t2": "nobody"}', True, True),  # settings.go: any invalid field -> all defaults
        ('{"t1": "cori", "t2": "cori", "slow": 9}', True, True),
        ('{"t1": "cori"', True, True),
        ('["cori"]', True, True),
        ('{"t1": {}, "t2": "cori"}', True, True),
        ('{"t1": "cori", "t2": ["cori"]}', True, True),
        ('{"t1": "mari", "t2": "cori"}', True, True),  # mari is Estonian-only
        ('{"lang": "en-US", "t1": "af_bella", "t1_et": "mari"}', True, False),
        ('{"lang": "et-EE", "t1": "af_bella", "t2": "af_bella"}', False, False),  # E7: the ET stack never pins Kokoro
        ('{"lang": "fi-FI", "t1": "cori", "t2": "af_bella"}', True, False),  # unsupported lang -> en-US
        ('{"lang": "et-EE", "t1_et": "af_bella"}', False, True),  # E30: et pair is mari only -> defaults (mari)
        ('{"lang": "et-EE", "t2_et": "stock"}', False, True),
        ('{"lang": "en-US", "t1": "cori", "t2": "cori", "t1_et": "af_bella"}', True, True),  # any invalid field -> defaults
    ],
)
def test_kokoro_selected_reads_t1_t2_and_invalid_is_defaults(tmp_path, text, pinned, bad):
    f = tmp_path / "myvectorConfig.json"
    f.write_text(text)
    got, err = server.kokoro_selected(f)
    assert got is pinned and bool(err) is bad


def test_kokoro_selected_missing_file_is_defaults(tmp_path):
    assert server.kokoro_selected(tmp_path / "none.json") == (True, None)


def test_watcher_pins_on_select_frees_on_unselect_and_garbled_is_defaults(lazy, tmp_path, caplog):
    k = lazy.k
    f = tmp_path / "myvectorConfig.json"
    f.write_text('{"t1": "cori", "t2": "cori"}')
    w = server.ConfigWatcher(f, k)
    w.poll()
    assert not k.pinned and not k.loaded
    f.write_text('{"t1": "cori", "t2": "af_bella"}')
    w.poll()
    assert k.pinned and k.loaded
    f.write_text('{"t1": "stock", "t2": "cori"}')
    w.poll()
    assert not k.pinned and not k.loaded
    f.write_text("garbled{")
    w.poll()
    w.poll()  # unchanged file: no second warning
    assert k.pinned and k.loaded  # defaults = af_bella
    assert sum("using defaults" in r.message for r in caplog.records) == 1
