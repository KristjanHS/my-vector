package vars

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestForkLangEnglishUnchanged(t *testing.T) {
	oldLang := APIConfig.STT.Language
	t.Cleanup(func() { APIConfig.STT.Language = oldLang })
	lang.UseForTest(t, lang.English)
	for _, l := range []string{"en-US", "it-IT", ""} {
		APIConfig.STT.Language = l
		if got := ForkLanguage(); got != l {
			t.Errorf("switch en-US, STT.Language %q: ForkLanguage() = %q", l, got)
		}
	}
	lang.UseForTest(t, lang.Estonian)
	APIConfig.STT.Language = "en-US"
	if got := ForkLanguage(); got != lang.Estonian {
		t.Errorf("switch et-EE: ForkLanguage() = %q", got)
	}
}

func TestForkLangLoadIntents(t *testing.T) {
	oldLang := APIConfig.STT.Language
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd); APIConfig.STT.Language = oldLang })
	os.Mkdir(filepath.Join(dir, "intent-data"), 0755)
	for file, name := range map[string]string{"et-EE.json": "intent_clock_time", "en-US.json": "intent_weather_extend"} {
		body := `[{"name":"` + name + `","keyphrases":["x"],"requiresexact":false}]`
		if err := os.WriteFile(filepath.Join(dir, "intent-data", file), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	APIConfig.STT.Language = "en-US"
	for _, c := range []struct{ switchTo, want string }{
		{lang.Estonian, "intent_clock_time"},
		{lang.English, "intent_weather_extend"},
	} {
		lang.UseForTest(t, c.switchTo)
		got, err := LoadIntents()
		if err != nil || len(got) != 1 || got[0].Name != c.want {
			t.Errorf("switch %s: LoadIntents() = %v, %v; want [%s]", c.switchTo, got, err, c.want)
		}
	}
}

// Pins the upstream hook lines that only an upstream-file edit could unit-test (!locale sits inline in a loop).
func TestForkLangHookLines(t *testing.T) {
	for file, hook := range map[string]string{
		"vars.go":                           `jsonFile, err := os.ReadFile(path + "intent-data/" + ForkLanguage() + ".json")`,
		"../wirepod/ttr/matchIntentSend.go": `arg = vars.ForkLanguage()`,
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range strings.Split(string(src), "\n") {
			found = found || strings.TrimSpace(line) == hook
		}
		if !found {
			t.Errorf("%s: hook line %q missing", file, hook)
		}
	}
}
