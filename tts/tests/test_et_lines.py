import et_lines
import et_text
import pytest


@pytest.mark.parametrize(
    "en, et",
    [
        ("12 degrees and cloudy", "12 kraadi ja pilves"),
        ("3 degrees and cold", "3 kraadi ja külm"),
        ("8 degrees and raining", "8 kraadi ja sajab vihma"),
        ("0 degrees and snowing", "0 kraadi ja sajab lund"),
        ("14 degrees and starry night", "14 kraadi ja tähine öö"),
        ("25 degrees and sunny", "25 kraadi ja päikseline"),
        ("10 degrees and windy", "10 kraadi ja tuuline"),
        ("21 degrees with thunderstorms", "21 kraadi ja äike"),
        ("1 degrees and sunny", "1 kraad ja päikseline"),
        ("-1 degrees and cold", "miinus 1 kraad ja külm"),
        ("-5 degrees and snowing", "miinus 5 kraadi ja sajab lund"),
        ("-12 degrees with thunderstorms", "miinus 12 kraadi ja äike"),
        ("Have we met before, Kristjan?", "Kas me oleme varem kohtunud, Kristjan?"),
        ("I already know an Anna", "Mul on juba tuttav nimega Anna"),
        ("I already know a Mari", "Mul on juba tuttav nimega Mari"),
        ("I already know you!", "Ma tunnen sind juba!"),
        ("You're Mari, not Anna", "Sa oled Mari, mitte Anna"),
        ("You’re Mari, not Anna", "Sa oled Mari, mitte Anna"),
        ("I don't know", "Ma ei tea"),
        ("Ready", "Valmis"),
        ("Ready \n", "Valmis"),
        ("09:05", "Kell on 9:05"),
        ("9:05", "Kell on 9:05"),
        ("15:15", "Kell on 15:15"),
        ("00:00", "Kell on 0:00"),
    ],
)
def test_translate_template_lines(en, et):
    assert et_lines.translate(en) == et


@pytest.mark.parametrize(
    "text",
    [
        "Hello there.",
        "It is 09:05 now",
        "25:00",
        "12 degrees and foggy",
        "Ready now",
        "I already know",
    ],
)
def test_translate_unmatched_is_unchanged(text):
    assert et_lines.translate(text) == text


def test_clock_line_then_normalize():
    assert [et_text.normalize(et_lines.translate(t)) for t in ("09:05", "15:15", "09:00", "00:00")] == [
        "Kell on üheksa null viis",
        "Kell on viisteist viisteist",
        "Kell on üheksa",
        "Kell on null",
    ]
