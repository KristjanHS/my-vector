package localization

import (
	"path/filepath"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

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

func TestForkGetText(t *testing.T) {
	oldLang, oldTexts := vars.APIConfig.STT.Language, forkTexts
	t.Cleanup(func() { vars.APIConfig.STT.Language, forkTexts = oldLang, oldTexts })
	forkTexts = map[string]string{STR_MINUTE: "minut"}
	vars.APIConfig.STT.Language = "en-US"

	forkSwitchTo(t, lang.English)
	if got := GetText(STR_MINUTE); got != "minute" {
		t.Errorf("en-US: %q, want upstream %q", got, "minute")
	}
	vars.APIConfig.STT.Language = "fr-FR"
	if got := GetText(STR_ONE_HOUR); got != "une heure" {
		t.Errorf("en-US switch, fr-FR STT: %q", got)
	}
	vars.APIConfig.STT.Language = "en-US"

	forkSwitchTo(t, lang.Estonian)
	if got := GetText(STR_MINUTE); got != "minut" {
		t.Errorf("et-EE: %q, want fork %q", got, "minut")
	}
	if got := GetText(STR_SECOND); got != "second" {
		t.Errorf("et-EE missing key: %q, want upstream %q", got, "second")
	}
}
