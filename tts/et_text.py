"""Estonian text normaliser for Piper et_EE (espeak-ng `et` reads cardinals itself, not times or ordinals).

normalize("Kell on 14.05.") -> "Kell on neliteist null viis."; normalize("5. novembril") -> "viiendal novembril".
"""

import re

# day 1-31 -> (nominative, genitive) ordinal
_UNITS = [
    ("esimene", "esimese"),
    ("teine", "teise"),
    ("kolmas", "kolmanda"),
    ("neljas", "neljanda"),
    ("viies", "viienda"),
    ("kuues", "kuuenda"),
    ("seitsmes", "seitsmenda"),
    ("kaheksas", "kaheksanda"),
    ("üheksas", "üheksanda"),
]
_TEEN_STEMS = [
    "ühe",
    "kahe",
    "kolme",
    "nelja",
    "viie",
    "kuue",
    "seitsme",
    "kaheksa",
    "üheksa",
]


def ordinal(n):
    """1-31 -> (nominative, genitive) Estonian ordinal."""
    if not 1 <= n <= 31:
        raise ValueError(n)
    if n < 10:
        return _UNITS[n - 1]
    if n == 10:
        return "kümnes", "kümnenda"
    if n < 20:
        stem = _TEEN_STEMS[n - 11] + "teistkümne"
        return stem + "s", stem + "nda"
    tens = {2: "kahekümne", 3: "kolmekümne"}[n // 10]
    if n % 10 == 0:
        return tens + "s", tens + "nda"
    nom, gen = _UNITS[n % 10 - 1]
    return f"{tens} {nom}", f"{tens} {gen}"


# month nominative -> genitive stem; a case form = genitive stem + one of _CASE_ENDINGS
_MONTHS = {
    "jaanuar": "jaanuari",
    "veebruar": "veebruari",
    "märts": "märtsi",
    "aprill": "aprilli",
    "mai": "mai",
    "juuni": "juuni",
    "juuli": "juuli",
    "august": "augusti",
    "september": "septembri",
    "oktoober": "oktoobri",
    "november": "novembri",
    "detsember": "detsembri",
}
_CASE_ENDINGS = ("", "l", "s", "st", "ks", "ni", "le", "lt", "ga", "na", "ta")

_TIME = re.compile(r"(?<![\d.,:])([01]?\d|2[0-3])[.:]([0-5]\d)(?![\d]|[.:,]\d)")
# ET months are lowercase: "5. Mai" is a sentence end or a name
_DAY_MONTH = re.compile(r"(?<![\d.,])([1-9]|[12]\d|3[01])\.(\s+)([a-zõäöüšž]+)\b")


_DIGITS = ["null", "üks", "kaks", "kolm", "neli", "viis", "kuus", "seitse", "kaheksa", "üheksa"]


def cardinal(n):
    """0-59 -> Estonian cardinal in words."""
    if not 0 <= n <= 59:
        raise ValueError(n)
    if n < 10:
        return _DIGITS[n]
    if n == 10:
        return "kümme"
    if n < 20:
        return _DIGITS[n - 10] + "teist"
    tens = _DIGITS[n // 10] + "kümmend"
    return f"{tens} {_DIGITS[n % 10]}" if n % 10 else tens


def _time(m):
    # in words: espeak's digit reading of "26" ("kaks | kümmendkuus") gives mari a seam she fills with a
    # "kuni"/"või"-like syllable (STT: 3/8 takes of "kell on 9 26"); spelled out measured 0/40 over 4 times
    hour, mm = int(m.group(1)), m.group(2)
    if mm == "00":
        return cardinal(hour)
    minutes = f"null {_DIGITS[int(mm[1])]}" if mm[0] == "0" else cardinal(int(mm))
    return f"{cardinal(hour)} {minutes}"


def _day_month(m):
    word = m.group(3)
    nom, gen = ordinal(int(m.group(1)))
    if word in _MONTHS:
        return f"{nom}{m.group(2)}{word}"
    for stem in _MONTHS.values():
        if word.startswith(stem) and word[len(stem) :] in _CASE_ENDINGS:
            return f"{gen}{word[len(stem) :]}{m.group(2)}{word}"
    return m.group(0)


def normalize(text):
    # day-month first, so the "25." of a time "14.25." never reads as a day
    return _TIME.sub(_time, _DAY_MONTH.sub(_day_month, text))
