import http.client
import json
import os
import threading
import types
from http.server import ThreadingHTTPServer

import numpy as np
import pytest
import server


class StubModel:
    def __init__(self, calls):
        self.calls = calls

    def transcribe(self, audio, language, beam_size):
        self.calls.append((audio, language, beam_size))
        return iter(
            [
                types.SimpleNamespace(text=" Mis kell"),
                types.SimpleNamespace(text=" on?"),
            ]
        ), None


@pytest.fixture
def stub():
    st = types.SimpleNamespace(loads=0, calls=[], fail=False)

    def factory(model, device, compute):
        st.loads += 1
        if st.fail:
            raise OSError("no model.bin")
        return StubModel(st.calls)

    st.rec = server.Recognizer(factory, "m", "cuda", "int8_float16")
    return st


def write_lang(path, lang):
    path.write_text(json.dumps({"t1": "af_bella", "lang": lang}))
    st = path.stat()
    os.utime(
        path, ns=(st.st_atime_ns, st.st_mtime_ns + 1_000_000)
    )  # same-size rewrite still changes the stat key


@pytest.mark.parametrize(
    "body, want",
    [
        (None, ("en-US", None)),
        ('{"t1": "cori"}', ("en-US", None)),
        ('{"lang": "et-EE"}', ("et-EE", None)),
        ('{"lang": "fi-FI"}', ("en-US", "lang 'fi-FI'")),
        ("[1]", ("en-US", "not a JSON object")),
        ("{", ("en-US", "Expecting")),
    ],
)
def test_config_lang_matches_lang_go(tmp_path, body, want):
    p = tmp_path / "myvectorConfig.json"
    if body is not None:
        p.write_text(body)
    lang, err = server.config_lang(p)
    assert lang == want[0]
    assert (err is None) if want[1] is None else (want[1] in err)


def test_transcribe_estonian_scales_pcm_and_joins_segments(stub):
    stub.rec.set_active(True)
    pcm = np.array([16384, -32768], "<i2").tobytes()
    assert stub.rec.transcribe(pcm) == "Mis kell on?"
    audio, language, beam = stub.calls[0]
    assert audio.dtype == np.float32 and audio.tolist() == [0.5, -1.0]
    assert (language, beam) == ("et", server.BEAM)


def test_transcribe_inactive_raises_without_loading(stub):
    with pytest.raises(server.Inactive):
        stub.rec.transcribe(b"\0\0")
    assert stub.loads == 0


def test_watcher_loads_on_estonian_and_frees_on_english(tmp_path, stub):
    p = tmp_path / "myvectorConfig.json"
    w = server.ConfigWatcher(p, stub.rec)
    w.poll()
    assert not stub.rec.loaded  # no file -> en-US
    write_lang(p, "et-EE")
    w.poll()
    w.poll()
    assert stub.rec.loaded and stub.loads == 1
    write_lang(p, "en-US")
    w.poll()
    assert not stub.rec.loaded and not stub.rec.active


def test_failed_load_retries_next_poll(tmp_path, stub):
    p = tmp_path / "myvectorConfig.json"
    write_lang(p, "et-EE")
    w = server.ConfigWatcher(p, stub.rec)
    stub.fail = True
    with pytest.raises(RuntimeError, match="no model.bin"):
        w.poll()
    stub.fail = False
    w.poll()
    assert stub.rec.loaded and stub.loads == 2


@pytest.fixture
def serve(tmp_path, stub):
    cfg = tmp_path / "myvectorConfig.json"
    w = server.ConfigWatcher(cfg, stub.rec)
    srv = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(stub.rec, w))
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    def post(body, path="/stt"):
        c = http.client.HTTPConnection("127.0.0.1", srv.server_port, timeout=5)
        c.request("POST", path, body)
        r = c.getresponse()
        return r.status, json.loads(r.read())

    yield types.SimpleNamespace(post=post, cfg=cfg, stub=stub, port=srv.server_port)
    srv.shutdown()
    srv.server_close()


def test_http_english_is_409(serve):
    assert serve.post(b"\0\0")[0] == 409
    assert serve.stub.loads == 0


def test_http_request_right_after_switch_loads_and_answers(serve):
    write_lang(
        serve.cfg, "et-EE"
    )  # no background poll in the test: the request's own poll must see it
    assert serve.post(b"\0\0") == (200, {"text": "Mis kell on?"})


def test_http_load_failure_is_500(serve):
    serve.stub.fail = True
    write_lang(serve.cfg, "et-EE")
    status, body = serve.post(b"\0\0")
    assert status == 500 and "no model.bin" in body["error"]


@pytest.mark.parametrize(
    "n, code",
    [(1, 400), (2 * server.RATE * server.MAX_SECONDS + 2, 413)],
    ids=["odd", "over-max"],
)
def test_http_bad_length(serve, n, code):
    c = http.client.HTTPConnection("127.0.0.1", serve.port, timeout=5)
    c.putrequest("POST", "/stt")
    c.putheader("Content-Length", str(n))
    c.endheaders()  # no body: the server must answer from the header alone
    assert c.getresponse().status == code
