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
    def __init__(self, st):
        self.st = st

    def transcribe(self, audio, language, beam_size):
        self.st.calls.append((audio, language, beam_size))
        if self.st.crash:
            raise RuntimeError("whisper child exited (-11)")
        return "Mis kell on?"

    def close(self):
        self.st.closes += 1


@pytest.fixture
def stub():
    st = types.SimpleNamespace(loads=0, closes=0, calls=[], models=[], fail=False, crash=False)

    def factory(model, device, compute):
        st.loads += 1
        st.models.append(model)
        if st.fail:
            raise OSError("no model.bin")
        return StubModel(st)

    st.rec = server.Recognizer(
        factory, {server.ENGLISH: "m-en", server.ESTONIAN: "m-et"}, "cuda", "int8_float16"
    )
    return st


def echo_child(conn, model, device, compute):
    conn.send(("ok", None))
    while True:
        try:
            audio, language, beam = conn.recv()
        except EOFError:
            return
        conn.send(("ok", f"{model} {language} {beam} {len(audio)}"))


def failing_child(conn, model, device, compute):
    conn.send(("err", "model load failed: OSError('no model.bin')"))


def dying_child(conn, model, device, compute):
    conn.send(("ok", None))
    conn.recv()
    os._exit(3)


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


@pytest.mark.parametrize("lang, code", [(server.ESTONIAN, "et"), (server.ENGLISH, "en")])
def test_transcribe_scales_pcm_in_lang(stub, lang, code):
    stub.rec.set_lang(lang)
    pcm = np.array([16384, -32768], "<i2").tobytes()
    assert stub.rec.transcribe(pcm, lang) == "Mis kell on?"
    audio, language, beam = stub.calls[0]
    assert audio.dtype == np.float32 and audio.tolist() == [0.5, -1.0]
    assert (language, beam) == (code, server.BEAM)


def test_transcribe_other_lang_raises_without_loading(stub):
    with pytest.raises(server.Inactive):
        stub.rec.transcribe(b"\0\0", server.ESTONIAN)
    assert stub.loads == 0
    stub.rec.set_lang(server.ENGLISH)
    with pytest.raises(server.Inactive):
        stub.rec.transcribe(b"\0\0", server.ESTONIAN)
    assert stub.calls == []


def test_watcher_swaps_model_with_lang(tmp_path, stub):
    p = tmp_path / "myvectorConfig.json"
    w = server.ConfigWatcher(p, stub.rec)
    w.poll()
    assert stub.models == ["m-en"]  # no file -> en-US
    write_lang(p, "et-EE")
    w.poll()
    w.poll()
    assert stub.models == ["m-en", "m-et"] and stub.closes == 1  # one model resident
    write_lang(p, "en-US")
    w.poll()
    assert stub.models == ["m-en", "m-et", "m-en"] and stub.closes == 2
    assert stub.rec.lang == server.ENGLISH


def test_crashed_model_is_freed_and_next_poll_reloads(tmp_path, stub):
    p = tmp_path / "myvectorConfig.json"
    write_lang(p, "et-EE")
    w = server.ConfigWatcher(p, stub.rec)
    w.poll()
    stub.crash = True
    with pytest.raises(RuntimeError, match="exited"):
        stub.rec.transcribe(b"\0\0", server.ESTONIAN)
    assert not stub.rec.loaded and stub.closes == 1
    stub.crash = False
    w.poll()  # config unchanged: the unloaded check alone must reload
    assert stub.rec.loaded and stub.loads == 2


def test_child_whisper_round_trip_and_close_exits():
    m = server.ChildWhisper("m", "cpu", "int8", target=echo_child)
    assert m.transcribe(np.zeros(3, np.float32), "et", 5) == "m et 5 3"
    m.close()
    assert m._proc.exitcode == 0


def test_child_whisper_load_error_raises_and_reaps():
    with pytest.raises(RuntimeError, match="no model.bin"):
        server.ChildWhisper("m", "cpu", "int8", target=failing_child)


def test_child_whisper_death_mid_request_raises():
    m = server.ChildWhisper("m", "cpu", "int8", target=dying_child)
    with pytest.raises(RuntimeError, match=r"exited \(3\)"):
        m.transcribe(np.zeros(1, np.float32), "et", 5)
    m.close()


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


def test_http_lang_mismatch_is_409(serve):
    assert serve.post(b"\0\0", "/stt?lang=et-EE")[0] == 409  # no file -> en-US resident
    assert serve.stub.calls == []


def test_http_english_answers(serve):
    assert serve.post(b"\0\0", "/stt?lang=en-US") == (200, {"text": "Mis kell on?"})
    assert serve.stub.calls[0][1] == "en"


def test_http_request_right_after_switch_loads_and_answers(serve):
    write_lang(
        serve.cfg, "et-EE"
    )  # no background poll in the test: the request's own poll must see it
    assert serve.post(b"\0\0") == (200, {"text": "Mis kell on?"})


def test_http_load_failure_is_500(serve):
    serve.stub.fail = True
    write_lang(serve.cfg, "et-EE")
    status, body = serve.post(b"\0\0")
    assert status == 500 and "not loaded" in body["error"]
    assert serve.stub.loads == 1  # one load attempt per request


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
