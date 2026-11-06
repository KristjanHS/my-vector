import pytest

import et_text


@pytest.mark.parametrize(
    "text, want",
    [
        ("Kell on 14.05.", "Kell on 14 null 5."),
        ("Kell on 10:30.", "Kell on 10 30."),
        ("kell 7.00", "kell 7"),
        ("Kell on 14.00.", "Kell on 14."),
        ("Täna on 5. november.", "Täna on viies november."),
        ("6. novembril", "kuuendal novembril"),
        ("1. mail", "esimesel mail"),
        ("21. märtsini", "kahekümne esimeseni märtsini"),
        ("15. juuni", "viieteistkümnes juuni"),
        ("12. detsembri hommikul", "kaheteistkümnenda detsembri hommikul"),
        ("30. augustist", "kolmekümnendast augustist"),
        ("Kell on 14.25. novembris", "Kell on 14 25. novembris"),
        ("Neid oli 5. Mai algul", "Neid oli 5. Mai algul"),
    ],
)
def test_normalize_times_and_day_month(text, want):
    assert et_text.normalize(text) == want


@pytest.mark.parametrize(
    "text",
    [
        "5.11.2026",
        "3,5 eurot",
        "12.5 kraadi",
        "2026. aastal",
        "3. klassis",
        "25.30",
        "32. mail",
        "14.5",
    ],
)
def test_normalize_leaves_other_numbers(text):
    assert et_text.normalize(text) == text


def test_ordinal_forms_and_range():
    assert et_text.ordinal(11) == ("üheteistkümnes", "üheteistkümnenda")
    assert et_text.ordinal(20) == ("kahekümnes", "kahekümnenda")
    assert et_text.ordinal(31) == ("kolmekümne esimene", "kolmekümne esimese")
    for bad in (0, 32):
        with pytest.raises(ValueError):
            et_text.ordinal(bad)
