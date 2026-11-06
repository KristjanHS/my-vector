"""Estonian for the robot's built-in English lines (plan E33/E34): the en-US BehaviorStrings templates, filled.

translate("09:05") -> "Kell on 9:05" (et_text.normalize reads the time); unmatched text comes back unchanged.
"""

import re

_EXACT = {
    "I already know you!": "Ma tunnen sind juba!",
    "I don't know": "Ma ei tea",
    "Ready": "Valmis",
}
_WEATHER = {
    "cloudy": "pilves",
    "cold": "külm",
    "raining": "sajab vihma",
    "snowing": "sajab lund",
    "starry night": "tähine öö",
    "sunny": "päikseline",
    "windy": "tuuline",
}
_CLOCK = re.compile(r"([01]?\d|2[0-3]):([0-5]\d)")
_WEATHER_RE = re.compile(
    rf"(-?\d+) degrees (?:and ({'|'.join(_WEATHER)})|with (thunderstorms))"
)
_RULES = [
    (re.compile(r"Have we met before, (.+)\?"), "Kas me oleme varem kohtunud, {0}?"),
    (re.compile(r"I already know an? (.+)"), "Mul on juba tuttav nimega {0}"),
    (re.compile(r"You're (.+), not (.+)"), "Sa oled {0}, mitte {1}"),
]


def _degrees(n):
    word = "kraad" if abs(n) == 1 else "kraadi"
    return f"miinus {-n} {word}" if n < 0 else f"{n} {word}"


def translate(text):
    line = text.strip().replace("’", "'")
    if line in _EXACT:
        return _EXACT[line]
    if m := _CLOCK.fullmatch(line):
        return f"Kell on {int(m[1])}:{m[2]}"
    if m := _WEATHER_RE.fullmatch(line):
        return f"{_degrees(int(m[1]))} ja {_WEATHER[m[2]] if m[2] else 'äike'}"
    for rx, et in _RULES:
        if m := rx.fullmatch(line):
            return et.format(*m.groups())
    return text
