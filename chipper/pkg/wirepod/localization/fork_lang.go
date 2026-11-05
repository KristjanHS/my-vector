package localization

import "github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"

func forkGetText(key string) (string, bool) {
	if lang.Current() != lang.Estonian {
		return "", false
	}
	s, ok := forkTexts[key]
	return s, ok
}
