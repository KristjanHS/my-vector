# Estonian LLM bench (plan 2026-11-05-estonian-language-mode.md Stage 1.3): answers + first-token s + VRAM fit.
# Usage: bench.py gen MODEL... | bench.py summary   (stdlib only; run with python3 -I)
import json
import statistics
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
W = ROOT / "robot/work/et-llm"
ANSWERS = W / "answers.jsonl"
MODELS = W / "models.jsonl"
SCORES = W / "scores.json"
QUESTIONS = ROOT / "robot/et-llm/questions.txt"
OLLAMA = "http://localhost:11434"
STT_MIB = 1341  # TalTech int8_float16 VRAM peak 1.31 GiB (plan Deliverable §STT)
# wire-pod CreateAIReq default prompt + CreatePrompt's speech-to-text line (commands block left out) + plan R-c language line
PROMPT = (
    "You are a helpful, animated robot called Vector. Keep the response concise yet informative."
    "\n\nKeep in mind, user input comes from speech-to-text software, so respond accordingly. "
    "No special characters, especially these: & ^ * # @ - . No lists. No formatting."
    "\n\nAnswer in Estonian."
)


def post(path, body, timeout=600):
    req = urllib.request.Request(
        OLLAMA + path, json.dumps(body).encode(), {"Content-Type": "application/json"}
    )
    return urllib.request.urlopen(req, timeout=timeout)


def get(path):
    with urllib.request.urlopen(OLLAMA + path, timeout=30) as r:
        return json.load(r)


def unload_all():
    for m in get("/api/ps")["models"]:
        post("/api/generate", {"model": m["name"], "keep_alive": 0}).read()
    for _ in range(30):
        if not get("/api/ps")["models"]:
            return
        time.sleep(1)


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
    first, parts = None, []
    with post("/v1/chat/completions", body) as r:
        for line in r:
            line = line.decode().strip()
            if not line.startswith("data: ") or line == "data: [DONE]":
                continue
            delta = json.loads(line[6:])["choices"][0]["delta"].get("content") or ""
            if delta and first is None:
                first = time.monotonic() - t0
            parts.append(delta)
    return "".join(parts).strip(), first, time.monotonic() - t0


def load_rows(path):
    return (
        [json.loads(line) for line in path.read_text().splitlines()]
        if path.exists()
        else []
    )


def append(path, row):
    with path.open("a") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")


def gen(models):
    W.mkdir(parents=True, exist_ok=True)
    qs = [q for q in QUESTIONS.read_text().splitlines() if q.strip()]
    done = {(r["model"], r["qid"]) for r in load_rows(ANSWERS)}
    for model in models:
        print(f"== {model}")
        unload_all()
        base, total = gpu_mib()
        t0 = time.monotonic()
        try:
            post("/api/generate", {"model": model, "keep_alive": "10m"}).read()
        except urllib.error.HTTPError as e:
            msg = e.read().decode()[:200]
            print(f"!! {model} skipped: {msg}")
            append(MODELS, {"model": model, "error": msg})
            continue
        load_s = time.monotonic() - t0
        ps = [m for m in get("/api/ps")["models"] if m["name"] == model]
        used, _ = gpu_mib()
        row = {
            "model": model,
            "load_s": round(load_s, 1),
            "size_vram_mib": ps[0]["size_vram"] // 2**20 if ps else None,
            "gpu_used_mib": used,
            "gpu_total_mib": total,
            "gpu_base_mib": base,
        }
        append(MODELS, row)
        print(f"   loaded {load_s:.1f}s, GPU {used}/{total} MiB")
        for qid, q in enumerate(qs, 1):
            if (model, qid) in done:
                continue
            text, first, total_s = ask(model, q)
            append(
                ANSWERS,
                {
                    "model": model,
                    "qid": qid,
                    "question": q,
                    "answer": text,
                    "first_token_s": round(first, 2) if first is not None else None,
                    "total_s": round(total_s, 2),
                },
            )
            print(f"   q{qid} first {first or 0:.2f}s total {total_s:.1f}s")


def summary():
    answers = load_rows(ANSWERS)
    scores = json.loads(SCORES.read_text()) if SCORES.exists() else {}
    loads = {}
    for r in load_rows(MODELS):
        loads[r["model"]] = r  # last load wins
    print(
        "| model | answers | scored | mean score | first token med/max s | VRAM GiB | free for STT | fit |"
    )
    print("|---|---|---|---|---|---|---|---|")
    for model, ld in loads.items():
        if "error" in ld:
            print(f"| {model} | – | – | – | – | – | – | skipped: {ld['error'][:60]} |")
            continue
        rows = [a for a in answers if a["model"] == model]
        sc = [
            scores[f"{model}|{a['qid']}"]
            for a in rows
            if f"{model}|{a['qid']}" in scores
        ]
        ft = [a["first_token_s"] for a in rows if a["first_token_s"] is not None]
        free = ld["gpu_total_mib"] - ld["gpu_used_mib"]
        mean = f"{statistics.mean(sc):.2f}" if sc else "–"
        ftx = f"{statistics.median(ft):.2f} / {max(ft):.2f}" if ft else "–"
        vram = f"{ld['size_vram_mib'] / 1024:.2f}" if ld["size_vram_mib"] else "–"
        fit = "yes" if free >= STT_MIB else "no"
        print(
            f"| {model} | {len(rows)} | {len(sc)} | {mean} | {ftx} | {vram} | {free / 1024:.2f} | {fit} |"
        )


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "gen":
        gen(sys.argv[2:])
    elif sys.argv[1:] == ["summary"]:
        summary()
    else:
        sys.exit(__doc__ or "usage: bench.py gen MODEL... | bench.py summary")
