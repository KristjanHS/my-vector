# Blind scorer for the Estonian LLM bench: shuffled answers, model hidden, one 1-5 score each; resumable.
# Usage: python3 -I robot/et-llm/score.py [[--lang] et|en]   (q = quit, s = skip; scores -> robot/work/et-llm/<lang>/scores.json)
import json
import random
import sys
from pathlib import Path

LANGS = {"et", "en"}
USAGE = "usage: score.py [[--lang] et|en]"


def parse_lang(args):
    if args[:1] == ["--lang"]:
        args = args[1:]
        if not args:
            sys.exit(USAGE)
    if len(args) > 1 or (args and args[0] not in LANGS):
        sys.exit(USAGE)
    return args[0] if args else "et"


def main():
    W = (
        Path(__file__).resolve().parents[2]
        / "robot/work/et-llm"
        / parse_lang(sys.argv[1:])
    )
    ANSWERS, SCORES = W / "answers.jsonl", W / "scores.json"
    if not ANSWERS.exists():
        sys.exit(f"no answers to score: {ANSWERS} missing (run bench.py gen first)")
    rows = [json.loads(line) for line in ANSWERS.read_text().splitlines()]
    scores = json.loads(SCORES.read_text()) if SCORES.exists() else {}
    random.Random(1918).shuffle(rows)
    todo = [r for r in rows if f"{r['model']}|{r['qid']}" not in scores]
    for i, r in enumerate(todo, len(rows) - len(todo) + 1):
        print(
            f"\n[{i}/{len(rows)}] K: {r['question']}\n{'-' * 60}\n{r['answer']}\n{'-' * 60}"
        )
        while True:
            s = input("score 1-5 (s skip, q quit)> ").strip().lower()
            if s in {"1", "2", "3", "4", "5", "s", "q"}:
                break
        if s == "q":
            break
        if s == "s":
            continue
        scores[f"{r['model']}|{r['qid']}"] = int(s)
        tmp = SCORES.with_suffix(".tmp")
        tmp.write_text(json.dumps(scores, indent=1))
        tmp.replace(SCORES)
    print(f"\n{len(scores)}/{len(rows)} scored -> {SCORES}")


if __name__ == "__main__":
    main()
