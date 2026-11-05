package vars

import (
	"path/filepath"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// forkSwitchTo saves l as the fork language in a temp settings file for the test's duration.
func forkSwitchTo(t *testing.T, l string) {
	t.Helper()
	old := lang.Path
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	lang.Path = func() string { return path }
	if err := lang.Save(l); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lang.Save(lang.English); lang.Path = old })
}

func TestForkLangEnglishUnchanged(t *testing.T) {
	oldLang := APIConfig.STT.Language
	t.Cleanup(func() { APIConfig.STT.Language = oldLang })
	forkSwitchTo(t, lang.English)
	for _, l := range []string{"en-US", "it-IT", ""} {
		APIConfig.STT.Language = l
		if got := ForkLanguage(); got != l {
			t.Errorf("switch en-US, STT.Language %q: ForkLanguage() = %q", l, got)
		}
	}
	forkSwitchTo(t, lang.Estonian)
	APIConfig.STT.Language = "en-US"
	if got := ForkLanguage(); got != lang.Estonian {
		t.Errorf("switch et-EE: ForkLanguage() = %q", got)
	}
}
