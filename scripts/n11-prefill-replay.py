#!/usr/bin/env python3
"""N11 prefill replay (search-news-quality Q7): apertus-estllm:8b, captured live et system prompt with its {search}
slot holding an et-EE news block; arms base / source ("<first source> andmetel") / generic ("Otsingu järgi") /
allsrc ("<every source, genitive> andmetel", Q9) / sentgen ("Värskete uudiste järgi").
Usage: python3 -I scripts/n11-prefill-replay.py [reps=4] [arms=base,source,generic] [asks=news|web|trim|weather|weather-held]  (local Ollama only; appends to
robot/work/search-trigger/n11/prefill.jsonl)
"""
import json
import pathlib
import re
import statistics
import sys
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
D = ROOT / "robot/work/search-trigger/n11"
REPS = int(sys.argv[1]) if len(sys.argv) > 1 else 4
SYS = (D / "sys.txt").read_text().rstrip("\n")
W = SYS[SYS.index("Live weather in Tallinn") : SYS.index("\n\nKasutaja sõnad")]  # the captured {search}-slot block

# search.Block(prompt.OllamaEt, <fixed results>) output, rendered by go run against chipper/pkg/myvector/search (2026-11-11).
BLOCKS = {
    "nvda": "Värsked veebitulemused (kuupäev, allikas): vasta nende põhjal 2–3 lausega, nimeta allikas ja kuupäev. Kui need ei vasta küsimusele, ütle, et ei leidnud sobivat vastust. Ära ütle, et sa ei saa otsida.\n- Nvidia shares slide 4% as export curbs to China tighten (reuters.com, 10.11): Nvidia stock fell 4.2% to $187 on Tuesday after the US Commerce Department widened restrictions on advanced chip sales to China.\n- Nvidia aktsia langes, investorid ootavad kvartalitulemusi (err.ee, 10.11): Nvidia aktsia hind langes teisipäeval üle nelja protsendi; ettevõte avaldab kolmanda kvartali tulemused 19. novembril.\n- Nvidia stock: analysts keep Buy rating ahead of earnings (cnbc.com, 9.11): Most Wall Street analysts kept their Buy ratings on Nvidia, citing strong data-center demand, with an average price target of $230.",
    "semi": "Värsked veebitulemused (kuupäev, allikas): vasta nende põhjal 2–3 lausega, nimeta allikas ja kuupäev. Kui need ei vasta küsimusele, ütle, et ei leidnud sobivat vastust. Ära ütle, et sa ei saa otsida.\n- Pooljuhiaktsiad kukkusid pärast Nvidia hoiatust, Nasdaq lan… (err.ee, 10.11): Pooljuhtide aktsiad langesid teisipäeval Wall Streetil, kui investorid muretsesid tehisintellekti kiipide nõudluse pärast; Philadelphia pooljuhiindeks kaotas 3…\n- TSMC October revenue jumps 38% on AI chip demand (reuters.com, 10.11): Taiwan Semiconductor Manufacturing Co reported October revenue of T$352 billion, up 38% from a year earlier, as demand for AI servers stayed strong.\n- Kiibitootjate aktsiad tõid Euroopa börsid langusesse (aripaev.ee, 11.11): ASML ja Infineoni aktsiad langesid kolmapäeva hommikul üle kahe protsendi, mis viis Euroopa börsiindeksid miinusesse."
}


def source(block):
    """First result's source as a listener hears it: the rendered host's label before the TLD, <=3 letters upper (err.ee ->
    ERR), else capitalised (reuters.com -> Reuters)."""
    name = hosts(block)[0].split(".")[-2]
    return name.upper() if len(name) <= 3 else name.capitalize()


ASKS = [
    ("semi", "otsin pooljuhtide teemalisi finantsuudiseid, mis mõjutavad aktsiaturge"),
    ("nvda", "otsi uudiseid Nvidia aktsia kohta"),
]
# Non-news web asks (Q10: base vs generic), same OllamaEt wrap; asks=web as the third argument.
WRAP = BLOCKS["nvda"].split("\n")[0]
BLOCKS["rimi"] = WRAP + "\n- Rimi Hyper Ülemiste lahtiolekuajad (rimi.ee, 10.11): Rimi Hyper Ülemiste on avatud iga päev kell 8–23, apteek kell 9–21.\n- Rimi poed Tallinnas (rimi.ee, 9.11): Enamik Rimi Supermarketeid Tallinnas on avatud E–P 8–22; pühade ajal võivad lahtiolekuajad muutuda."
BLOCKS["pm"] = WRAP + "\n- Peaminister (valitsus.ee, 10.11): Vabariigi Valitsust juhib peaminister Kristen Michal, kes asus ametisse 23. juulil 2024.\n- Kristen Michal (et.wikipedia.org, 8.11): Kristen Michal on Eesti poliitik, Reformierakonna esimees ja alates 2024. aastast Eesti peaminister."
if len(sys.argv) > 3 and sys.argv[3] == "web":
    ASKS = [("rimi", "millal Rimi lahti on?"), ("pm", "kes on Eesti peaminister?")]
# Q12 (N14): the "semi" block with its cut-off "…" snippet tail cut back (semi_s), and its title tail too (semi_st); asks=trim.
CUT = "; Philadelphia pooljuhiindeks kaotas 3…"
BLOCKS["semi_s"] = BLOCKS["semi"].replace(CUT, ".")
BLOCKS["semi_st"] = BLOCKS["semi_s"].replace("hoiatust, Nasdaq lan…", "hoiatust")
if len(sys.argv) > 3 and sys.argv[3] == "trim":
    ASKS = [(k, ASKS[0][1]) for k in ("semi", "semi_s", "semi_st")]


def hosts(block):
    """Distinct rendered hosts in block order."""
    return list(dict.fromkeys(re.findall(r"^- .*? \(([^,)]+)", block, re.M)))


# Hand-written genitives for the fixed hosts (Q9 allsrc arm).
GEN = {"err.ee": "ERRi", "reuters.com": "Reutersi", "aripaev.ee": "Äripäeva", "cnbc.com": "CNBC"}


def auto_gen(host):
    """Simple automatic rule: label before the TLD, <=4 letters upper else capitalised, +i after a consonant."""
    name = host.split(".")[-2]
    name = name.upper() if len(name) <= 4 else name.capitalize()
    return name + ("i" if name[-1].lower() not in "aeiouõäöü" else "")


def joined(names):
    return names[0] if len(names) == 1 else ", ".join(names[:-1]) + " ja " + names[-1]


def prefill(arm, key):
    if arm in ("source", "allsrc") and not hosts(BLOCKS[key]):
        return ""
    v = {
        "base": "",
        "source": lambda: source(BLOCKS[key]) + " andmetel",
        "generic": "Otsingu järgi",
        "allsrc": lambda: joined([GEN.get(h, auto_gen(h)) for h in hosts(BLOCKS[key])]) + " andmetel",
        "sentgen": "Värskete uudiste järgi",
        "wx": "Ilmateate järgi",
        "wxfresh": "Värske ilmateate järgi",
        **{a: "Ilmateate järgi" for a in WX_ARMS},
        **{a: "Värskete uudiste järgi" for a in TRIM_ARMS},
    }[arm]
    return v() if callable(v) else v


# N13: n11's two weather asks with the live Open-Meteo block (wx-block.txt, fetched via search.Fill); asks=weather.
if len(sys.argv) > 3 and sys.argv[3] == "weather":
    BLOCKS["wx"] = (D / "wx-block.txt").read_text().strip()
    ASKS = [("wx", "kas homme sajab lund Tallinnas?"), ("wx", "kui külm homme Tallinnas on?")]
# N15 held-out weather asks (asks=weather-held, arm etshort, reps=1): 8 asks beyond n11's two; rows carry ask key "wxheld".
if len(sys.argv) > 3 and sys.argv[3] == "weather-held":
    ASKS = [("wxheld", q) for q in ("kas ülehomme sajab?", "kui tugev tuul praegu on?", "mis ilm reedel on?", "kas täna on vihmavarju vaja?",
            "kas homme on külmem kui täna?", "kas reedel sajab rohkem kui homme?", "kas täna õhtul sajab lund?", "mis ilm nädalavahetusel tuleb?")]


def chat(s, u, pre, temp=0.4, max_tokens=300, shots=()):
    msgs = [{"role": "system", "content": s}]
    for q, a in shots:
        msgs += [{"role": "user", "content": q}, {"role": "assistant", "content": a}]
    msgs.append({"role": "user", "content": u})
    if pre:
        msgs.append({"role": "assistant", "content": pre})
    body = json.dumps({"model": "apertus-estllm:8b", "messages": msgs, "temperature": temp, "top_p": 0.9, "max_tokens": max_tokens}).encode()
    req = urllib.request.Request("http://localhost:11434/v1/chat/completions", body, {"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req, timeout=180))["choices"][0]["message"]["content"].strip()


def spoken(pre, cont):
    """Prefill + continuation, joined the way a listener hears it (no doubled opener if the model repeats it)."""
    if not pre:
        return cont
    if cont.lower().startswith(pre.lower()):
        return cont
    sep = "" if cont[:1] in ",.:;" else " "
    return pre + sep + cont


STRICT = re.compile(r"ei saa\w* .{0,40}otsi|ei (ole|oma) .{0,30}(otsi|internet|veeb|reaalajas)|ei saa sind aidata", re.I)
BROAD = re.compile(r"ei saa|ei oma|puudu|ei ole võimalik|pole võimalik|reaalajas|ei tea|kahjuks", re.I)  # n11/replay.py REF
SRC = re.compile(r"\b(err|reuters|cnbc|äripäev|aripaev)\b", re.I)
DATE = re.compile(r"\b(9|10|11)\.\s?11\b|\b(9|10|11)\.\s?novemb|\b(üheksa|kümne|üheteist)\w* novemb|novemb\w* (9|10|11)\b|teisipäev|kolmapäev|esmaspäev|eile|täna", re.I)

# N15 (asks=weather): the live wx block re-written in Estonian (same numbers as wx-block.txt; 14 km/h = 4 m/s; 11.11.2026 =
# Wednesday) — etsent alone, etshort (+one-sentence ask, temp 0, 60 tokens), etdemo (+two few-shot turns).
WX_ET = (
    "Open-Meteo ilmateade Tallinnale ütleb (vasta ilmaküsimusele ainult selle järgi; kui küsitud nähtust, nt lund, siin pole, ütle, et seda ei tule):\n"
    "Praegu: 3 kraadi sooja, pilves, tuul 4 m/s.\n"
    "Täna, kolmapäeval 11. novembril: 2 kuni 4 kraadi sooja, uduvihm, sademeid 0,3 mm (tõenäosus 39%). Lund ei tule.\n"
    "Homme, neljapäeval 12. novembril: 2 kuni 6 kraadi sooja, uduvihm, sademeid 0,5 mm (tõenäosus 70%). Lund ei tule.\n"
    "Reedel, 13. novembril: 2 kuni 4 kraadi sooja, nõrk vihm, sademeid 5,3 mm (tõenäosus 85%). Lund ei tule."
)
WX_ET_SHORT = WX_ET.replace("seda ei tule):", "seda ei tule. Vasta ühe lausega.):", 1)
WX_SHOTS = (
    ("kas täna on külm?", "Ilmateate järgi on täna kaks kuni neli kraadi sooja ja sajab uduvihma, külmakraade ei tule."),
    ("kas reedel sajab lund?", "Ilmateate järgi reedel lund ei tule, sajab nõrka vihma."),
)
WX_ARMS = {"etsent": ("WX_ET", {}), "etshort": ("WX_ET_SHORT", {"temp": 0, "max_tokens": 60}), "etdemo": ("WX_ET", {"shots": WX_SHOTS})}

# N16 (asks=trim, run on the "semi" ask only): xq (quote-then-answer, two calls), facts (numbered fact lines from the semi_s
# snippets, no titles), shot0 (untrimmed semi block, one worked example in the wrap, temp 0).
OLD_ASK = "vasta nende põhjal 2–3 lausega, nimeta allikas ja kuupäev."
assert OLD_ASK in BLOCKS["semi"]
FACT_LINES = re.findall(r"^- .*? \(([^,)]+), ([\d.]+)\): (.*)$", BLOCKS["semi_s"], re.M)
TRIM_FACTS = (
    "Värsked uudised (allikas, kuupäev). Iga rida on üks teada fakt; räägi ainult nendest faktidest, kahe lausega. Ära lisa "
    "põhjusi, arve ega järeldusi, mida ridadel pole, ja ära seo ridu sõnadega „kuna“ või „sest“, kui rida ise seda ei ütle. "
    "Ära ütle, et sa ei saa otsida.\n" + "\n".join(f"{i}. ({h}, {d}) {t}" for i, (h, d, t) in enumerate(FACT_LINES, 1))
)
TRIM_SHOT0 = BLOCKS["semi"].replace(
    OLD_ASK,
    "vasta nende põhjal kahe lausega. Näide: tulemus „Elektri hind tõusis teisipäeval, kui tuul vaibus; Nord Pooli hind jõudis 2…“ "
    "→ vastus „Värskete uudiste järgi tõusis elektri hind teisipäeval, kui tuul vaibus.“ Poolikut arvu, põhjust ega hinnangut, "
    "mida tulemuses pole, ära lisa.",
)
XQ_ASK = "\n\nKopeeri tulemustest sõna-sõnalt kuni kaks lauset, mis sellele küsimusele vastavad, kumbki eraldi reale. Ära lisa midagi muud. Kui ükski ei vasta, kirjuta ainult: EI LEIDNUD"
XQ_HEAD = "Värsked uudised (ainult need laused on teada):\n"
XQ_TAIL = "\nVasta ainult nende lausete põhjal kahe lausega. Ära lisa põhjusi, arve ega järeldusi, mida lausetes pole."
TRIM_ARMS = {"xq": ("semi", {}), "facts": ("TRIM_FACTS", {}), "shot0": ("TRIM_SHOT0", {"temp": 0})}
NEW_BLOCKS = {"WX_ET": WX_ET, "WX_ET_SHORT": WX_ET_SHORT, "TRIM_FACTS": TRIM_FACTS, "TRIM_SHOT0": TRIM_SHOT0, "semi": BLOCKS["semi"]}
LIST = re.compile(r"^\s*([-*•]|\d+[.)])\s", re.M)


def xq_lines(raw, block):
    """Call-1 lines kept: bullet/number/quote marks stripped, ≥ 4 words, verbatim in the block, no "…" at or after the copy."""
    kept = []
    for ln in raw.splitlines():
        ln = re.sub(r"^\s*([-*•]|\d+[.)])\s*", "", ln).strip().strip("\"„“”«»").strip().rstrip("…").rstrip()
        i = block.find(ln) if len(ln.split()) >= 4 else -1
        if i >= 0 and not block[i + len(ln):].lstrip().startswith("…"):
            kept.append(ln)
    return kept


out = open(D / "prefill.jsonl", "a")
ARMS = sys.argv[2].split(",") if len(sys.argv) > 2 else ["base", "source", "generic"]
for arm in ARMS:
    n = {"strict": 0, "broad": 0, "src": 0, "date": 0}
    new = arm in WX_ARMS or arm in TRIM_ARMS
    asks = [a for a in ASKS if a[0] == "semi"] if arm in TRIM_ARMS else ASKS
    lists, spokens, walls = 0, [], []
    for rep in range(REPS):
        for key, ask in asks:
            pre = prefill(arm, key)
            extra = {}
            if not new:
                s = SYS.replace(W, BLOCKS[key])
                cont = chat(s, ask, pre)
            else:
                bname, kw = (WX_ARMS | TRIM_ARMS)[arm]
                block = NEW_BLOCKS[bname]
                s = SYS.replace(W, block)
                if arm == "xq":
                    t0 = time.monotonic()
                    raw = chat(s, ask + XQ_ASK, "", temp=0, max_tokens=120)
                    t1 = time.monotonic()
                    kept = [] if "EI LEIDNUD" in raw else xq_lines(raw, block)
                    extra = {"xq_call1": raw, "xq_kept": kept, "t1": round(t1 - t0, 2)}
                    if kept:
                        block = XQ_HEAD + "\n".join(kept) + XQ_TAIL
                        cont = chat(SYS.replace(W, block), ask, pre)
                        extra["t2"] = round(time.monotonic() - t1, 2)
                    else:
                        cont, extra["t2"] = "", 0.0
                    walls.append(extra["t1"] + extra["t2"])
                else:
                    cont = chat(s, ask, pre, **kw)
                extra |= {"q": ask, "block": bname, "blocktext": block}
            t = spoken(pre, cont)
            r = {"strict": bool(STRICT.search(t)), "broad": bool(BROAD.search(t)), "src": bool(SRC.search(t)), "date": bool(DATE.search(t))}
            if arm == "xq" and not cont:
                t, r["strict"], r["broad"] = "", True, True
            if new:
                extra["list"] = bool(LIST.search(t))
                lists += extra["list"]
                spokens.append(t)
            for k, v in r.items():
                n[k] += v
            out.write(json.dumps({"arm": arm, "ask": key, "rep": rep, "prefill": pre, "cont": cont, "spoken": t, **r, **extra}, ensure_ascii=False) + "\n")
            flags = "".join(k[0].upper() if v else "." for k, v in r.items())
            print(f"{arm:7} {key} {flags} | {t[:160]}", flush=True)
    tot = REPS * len(asks)
    more = f" lists {lists}/{tot} distinct {len(set(spokens))}/{tot}" if new else ""
    if walls:
        more += f" wall-median {statistics.median(walls):.2f}s"
    print(f"== {arm}: strict {n['strict']}/{tot} broad {n['broad']}/{tot} source {n['src']}/{tot} date {n['date']}/{tot}{more}", flush=True)
