# LLM bench (plan 2026-11-05-estonian-language-mode.md Stage 1.3): answers + speed + VRAM fit, per language.
# Usage: bench.py [--lang et|en] gen MODEL... | bench.py --lang et ladder | bench.py [--lang et|en] summary   (stdlib; python3 -I)
import json
import re
import statistics
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OLLAMA = "http://localhost:11434"
TTS = "http://localhost:8091"
KOKORO_MIB = 666  # Kokoro af_bella 0.65 GiB (R20): resident in every base reading, unloaded in ET mode
# lang -> (questions file, prompt language line, GPU MiB the rest of the stack needs, GPU MiB that mode frees)
LANGS = {
    "et": (
        "questions.txt",
        "\n\nAnswer in Estonian.",
        1341 + 420,
        KOKORO_MIB,
    ),  # TalTech STT int8_float16 + Neurokõne max
    "en": ("questions-en.txt", "", 0, 0),  # vosk STT is CPU; Kokoro already in base
}
EST = "hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-"
LADDER = (
    "Q4_K_M",
    ["Q5_K_S", "Q5_K_M"],
    ["Q4_K_S", "IQ4_XS"],
)  # start, up while fit, down until fit (E11)
SENTENCE_END = re.compile(
    r"[.!?]"
)  # wire-pod StreamingKGSim speaks each chunk at a punctuation mark
# wire-pod CreateAIReq default prompt + CreatePrompt's speech-to-text line (commands block left out)
BASE_PROMPT = (
    "You are a helpful, animated robot called Vector. Keep the response concise yet informative."
    "\n\nKeep in mind, user input comes from speech-to-text software, so respond accordingly. "
    "No special characters, especially these: & ^ * # @ - . No lists. No formatting."
)


def use(lang):
    global W, ANSWERS, MODELS, SCORES, QUESTIONS, PROMPT, NEED_MIB, FREES_MIB
    qfile, line, NEED_MIB, FREES_MIB = LANGS[lang]
    W = ROOT / "robot/work/et-llm" / lang
    ANSWERS, MODELS, SCORES = W / "answers.jsonl", W / "models.jsonl", W / "scores.json"
    QUESTIONS, PROMPT = ROOT / "robot/et-llm" / qfile, BASE_PROMPT + line


def post(path, body, timeout=600):
    req = urllib.request.Request(
        OLLAMA + path, json.dumps(body).encode(), {"Content-Type": "application/json"}
    )
    return urllib.request.urlopen(req, timeout=timeout)


def get(path, base=OLLAMA):
    with urllib.request.urlopen(base + path, timeout=30) as r:
        return json.load(r)


def unload_all():
    for m in get("/api/ps")["models"]:
        post("/api/generate", {"model": m["name"], "keep_alive": 0}).read()
    for _ in range(30):
        if not get("/api/ps")["models"]:
            return
        time.sleep(1)
    raise RuntimeError("Ollama models still loaded after 30 s")


def gpu_mib():
    out = subprocess.run(
        [
            "nvidia-smi",
            "--query-gpu=memory.used,memory.total",
            "--format=csv,noheader,nounits",
        ],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.split(",")
    return int(out[0]), int(out[1])


def settled_gpu(tries=30, tol=16):
    """nvidia-smi used/total once two readings 1 s apart differ by <= tol MiB."""
    prev, total = gpu_mib()
    cur = prev
    for _ in range(tries):
        time.sleep(1)
        cur, total = gpu_mib()
        if abs(cur - prev) <= tol:
            return cur, total
        prev = cur
    raise RuntimeError(f"GPU memory not settled after {tries} s (last {cur} MiB)")


def kokoro_resident():
    try:
        return get("/health", TTS).get("kokoro_loaded") is True
    except (OSError, ValueError):
        return False


def ensure_pulled(model):
    if model in {m["name"] for m in get("/api/tags")["models"]}:
        return
    print(f"   pulling {model}")
    post("/api/pull", {"model": model, "stream": False}, timeout=7200).read()


def fit(row):
    """(headroom MiB, verdict): ET mode frees Kokoro, then needs STT + Neurokõne; any CPU spill fails."""
    if "llm_mib" not in row:
        return None, "unknown"
    if (
        row.get("size_mib")
        and row.get("size_vram_mib") is not None
        and row["size_vram_mib"] < row["size_mib"]
    ):
        return None, "spill"
    headroom = row["gpu_total_mib"] - row["gpu_used_mib"] + FREES_MIB - NEED_MIB
    return headroom, "yes" if headroom >= 0 else "no"


def ask(model, question):
    body = {
        "model": model,
        "stream": True,
        "temperature": 1,
        "top_p": 1,
        "max_tokens": 2048,
        "messages": [
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": question},
        ],
    }
    t0 = time.monotonic()
    first = sentence = None
    parts = []
    with post("/v1/chat/completions", body) as r:
        for line in r:
            line = line.decode().strip()
            if not line.startswith("data: ") or line == "data: [DONE]":
                continue
            obj = json.loads(line[6:])
            if "error" in obj:
                raise RuntimeError(f"{model}: stream error: {obj['error']}")
            delta = ((obj.get("choices") or [{}])[0].get("delta") or {}).get(
                "content"
            ) or ""
            if delta and first is None:
                first = time.monotonic() - t0
            if delta and sentence is None and SENTENCE_END.search(delta):
                sentence = time.monotonic() - t0
            parts.append(delta)
    total = time.monotonic() - t0
    text = "".join(parts).strip()
    if sentence is None and text:
        sentence = total  # no punctuation: wire-pod speaks the tail at stream end
    return text, first, sentence, total


def load_rows(path):
    return (
        [json.loads(line) for line in path.read_text().splitlines()]
        if path.exists()
        else []
    )


def append(path, row):
    with path.open("a") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")


def r2(x):
    return round(x, 2) if x is not None else None


def run_model(model, qs, done):
    """Measure one model's fit, then answer every question; returns the fit verdict or 'error'."""
    print(f"== {model}")
    unload_all()
    try:
        ensure_pulled(model)
    except urllib.error.HTTPError as e:
        msg = e.read().decode()[:200]
        print(f"!! {model} pull failed: {msg}")
        append(MODELS, {"model": model, "error": msg})
        return "error"
    base, total = settled_gpu()
    t0 = time.monotonic()
    try:
        post("/api/generate", {"model": model, "keep_alive": "10m"}).read()
    except urllib.error.HTTPError as e:
        msg = e.read().decode()[:200]
        print(f"!! {model} skipped: {msg}")
        append(MODELS, {"model": model, "error": msg})
        return "error"
    load_s = time.monotonic() - t0
    used, _ = settled_gpu()
    ps = [m for m in get("/api/ps")["models"] if m["name"] == model]
    row = {
        "model": model,
        "ollama": get("/api/version")["version"],
        "load_s": round(load_s, 1),
        "size_mib": ps[0]["size"] >> 20 if ps else None,
        "size_vram_mib": ps[0]["size_vram"] >> 20 if ps else None,
        "gpu_base_mib": base,
        "gpu_used_mib": used,
        "gpu_total_mib": total,
        "llm_mib": used - base,
    }
    headroom, verdict = fit(row)
    row["headroom_mib"], row["fit"] = headroom, verdict
    append(MODELS, row)
    print(
        f"   loaded {load_s:.1f}s, base {base} -> {used}/{total} MiB, LLM {used - base}, headroom {headroom}: {verdict}"
    )
    for qid, q in enumerate(qs, 1):
        if (model, qid) in done:
            continue
        text, first, sentence, total_s = ask(model, q)
        append(
            ANSWERS,
            {
                "model": model,
                "qid": qid,
                "question": q,
                "answer": text,
                "first_token_s": r2(first),
                "first_sentence_s": r2(sentence),
                "total_s": round(total_s, 2),
            },
        )
        print(
            f"   q{qid} first {first or 0:.2f}s sentence {sentence or 0:.2f}s total {total_s:.1f}s"
        )
    return verdict


def start():
    if not kokoro_resident():
        sys.exit(
            f"Kokoro not loaded ({TTS}/health kokoro_loaded != true): the base reading must include it"
        )
    W.mkdir(parents=True, exist_ok=True)
    qs = [q for q in QUESTIONS.read_text().splitlines() if q.strip()]
    return qs, {(r["model"], r["qid"]) for r in load_rows(ANSWERS)}


def gen(models):
    qs, done = start()
    for model in models:
        run_model(model, qs, done)


def ladder():
    qs, done = start()
    first, ups, downs = LADDER
    ok = run_model(EST + first, qs, done) == "yes"
    pick = first if ok else None
    for q in ups if ok else downs:
        v = run_model(EST + q, qs, done)
        if ok and v != "yes":
            break
        if v == "yes":
            pick = q
            if not ok:
                break
    print(f"== ladder pick: {pick or 'none fits'}")
    return pick


def med(xs):
    return f"{statistics.median(xs):.2f}" if xs else "–"


def summary():
    answers = load_rows(ANSWERS)
    scores = json.loads(SCORES.read_text()) if SCORES.exists() else {}
    loads = {}
    for r in load_rows(MODELS):  # last good load wins; an error row only if no good one
        if "error" not in r or "error" in loads.get(r["model"], {"error": ""}):
            loads[r["model"]] = r
    print(
        "| model | answers | empty | scored | mean score | first token med s | first sentence med/max s | total med s | LLM GiB | headroom MiB | fit |"
    )
    print("|---|---|---|---|---|---|---|---|---|---|---|")
    for model, ld in loads.items():
        if "error" in ld:
            print(
                f"| {model} | – | – | – | – | – | – | – | – | – | error: {ld['error'][:60]} |"
            )
            continue
        rows = [a for a in answers if a["model"] == model]
        sc = [
            scores[f"{model}|{a['qid']}"]
            for a in rows
            if f"{model}|{a['qid']}" in scores
        ]
        ft = [a["first_token_s"] for a in rows if a["first_token_s"] is not None]
        fs = [
            a["first_sentence_s"] for a in rows if a.get("first_sentence_s") is not None
        ]
        tt = [a["total_s"] for a in rows if a["first_token_s"] is not None]
        empty = sum(1 for a in rows if a["answer"] == "" or a["first_token_s"] is None)
        mean = f"{statistics.mean(sc):.2f}" if sc else "–"
        fsx = f"{med(fs)} / {max(fs):.2f}" if fs else "–"
        headroom, verdict = fit(ld)
        llm = f"{ld['llm_mib'] / 1024:.2f}" if "llm_mib" in ld else "–"
        print(
            f"| {model} | {len(rows)} | {empty} | {len(sc)} | {mean} | {med(ft)} | {fsx} | {med(tt)} | {llm} | {headroom if headroom is not None else '–'} | {verdict} |"
        )


if __name__ == "__main__":
    args = sys.argv[1:]
    lang = "et"
    if args[:1] == ["--lang"] and len(args) > 1 and args[1] in LANGS:
        lang, args = args[1], args[2:]
    use(lang)
    if (
        len(args) >= 2
        and args[0] == "gen"
        and not any(m.startswith("-") for m in args[1:])
    ):
        gen(args[1:])
    elif args == ["ladder"] and lang == "et":
        ladder()
    elif args == ["summary"]:
        summary()
    else:
        sys.exit(
            "usage: bench.py [--lang et|en] gen MODEL... | bench.py --lang et ladder | bench.py [--lang et|en] summary"
        )
