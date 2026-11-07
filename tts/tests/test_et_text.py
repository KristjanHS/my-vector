import pytest

import et_text


@pytest.mark.parametrize(
    "text, want",
    [
        ("Kell on 14.05.", "Kell on neliteist null viis."),
        ("Kell on 10:30.", "Kell on kümme kolmkümmend."),
        ("kell on 09.26,", "kell on üheksa kakskümmend kuus,"),
        ("kell 7.00", "kell seitse"),
        ("Kell on 14.00.", "Kell on neliteist."),
        ("Täna on 5. november.", "Täna on viies november."),
        ("6. novembril", "kuuendal novembril"),
        ("1. mail", "esimesel mail"),
        ("21. märtsini", "kahekümne esimeseni märtsini"),
        ("15. juuni", "viieteistkümnes juuni"),
        ("12. detsembri hommikul", "kaheteistkümnenda detsembri hommikul"),
        ("30. augustist", "kolmekümnendast augustist"),
        ("Kell on 14.25. novembris", "Kell on neliteist kakskümmend viis. novembris"),
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


def test_cardinal_forms_and_range():
    assert [et_text.cardinal(n) for n in (0, 10, 11, 17, 20, 26, 59)] == [
        "null",
        "kümme",
        "üksteist",
        "seitseteist",
        "kakskümmend",
        "kakskümmend kuus",
        "viiskümmend üheksa",
    ]
    for bad in (-1, 60):
        with pytest.raises(ValueError):
            et_text.cardinal(bad)


def test_ordinal_forms_and_range():
    assert et_text.ordinal(11) == ("üheteistkümnes", "üheteistkümnenda")
    assert et_text.ordinal(20) == ("kahekümnes", "kahekümnenda")
    assert et_text.ordinal(31) == ("kolmekümne esimene", "kolmekümne esimese")
    for bad in (0, 32):
        with pytest.raises(ValueError):
            et_text.ordinal(bad)
