package wirepod_ttr

import (
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func TestForkTimerEstonian(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	cases := map[string]string{
		"paneme taimer viieks minutiks":         "300",
		"pane taimer viis minutit":              "300",
		"taimer kahekümne viieks sekundiks":     "25",
		"pane taimer pooleks tunniks":           "1800",
		"taimer pool tundi":                     "1800",
		"pane taimer 5 minutiks":                "300",
		"taimer üheks tunniks":                  "3600",
		"taimer viieteistkümneks minutiks":      "900",
		"taimer üheksakümne üheksaks sekundiks": "99",
		"taimer kakskümmend üks minutit":        "1260",
		"taimer 1,5 tundi":                      "5400",
		"kaks ja pool minutit":                  "150",
		"pane taimer tunniks":                   "3600",
		"pane taimer minutiks":                  "60",
		"poolteist tundi":                       "5400",
		"pooleteist tunniks":                    "5400",
		"poolteist minutit":                     "90",
	}
	oldLang := vars.APIConfig.STT.Language
	t.Cleanup(func() { vars.APIConfig.STT.Language = oldLang })
	for _, base := range []string{"en-US", "it-IT", "ko-KR"} { // STR_SECOND: "second", "secondi", "초"
		vars.APIConfig.STT.Language = base
		for _, stt := range []string{"vosk", "whisper.cpp"} { // whisper.cpp routes digits to whisperSpeechtoNum
			t.Setenv("STT_SERVICE", stt)
			for in, want := range cases {
				if got := words2num(in); got != want {
					t.Errorf("%s/%s: words2num(%q) = %s, want %s", base, stt, in, got, want)
				}
			}
		}
	}
}

func TestForkTimerEnglishUnchanged(t *testing.T) {
	lang.UseForTest(t, lang.English)
	t.Setenv("STT_SERVICE", "vosk")
	in := "set a timer for five minutes and ten seconds"
	if got := forkTimerText(in); got != in {
		t.Errorf("forkTimerText rewrote en-US text: %q", got)
	}
	if got := forkTimerText("taimer 5 minutit"); got != "taimer 5 minutit" {
		t.Errorf("forkTimerText rewrote et-EE text in en-US mode: %q", got)
	}
	if got := words2num(in); got != "310" {
		t.Errorf("words2num(%q) = %s, want 310", in, got)
	}
}
