package localization

// forkTexts are GetText's et-EE words while the fork switch is Estonian; a missing key falls through to upstream.
// Timer units stay upstream English: the ET timer normaliser (ttr/fork_timer.go) emits "<n> second".
var forkTexts = map[string]string{
	STR_NAME_IS:                        "nimi on ",
	STR_NAME_IS2:                       "nimi on ",
	STR_NAME_IS3:                       "nimi on ",
	STR_WEATHER_FORECAST:               "prognoos",
	STR_WEATHER_TOMORROW:               "homme",
	STR_WEATHER_THE_DAY_AFTER_TOMORROW: "ülehomme", // weatherParser checks this before "homme", its substring
	STR_WEATHER_TONIGHT:                "täna õhtul",
	STR_WEATHER_THIS_AFTERNOON:         "täna pärastlõunal",
}
