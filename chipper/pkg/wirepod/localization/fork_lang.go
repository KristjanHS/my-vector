package localization

import "github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"

// forkTexts are GetText's et-EE words while the fork switch is Estonian (Estonian plan 6.2 fills them); a missing key falls through to upstream.
var forkTexts = map[string]string{}

func forkGetText(key string) (string, bool) {
	if lang.Current() != lang.Estonian {
		return "", false
	}
	s, ok := forkTexts[key]
	return s, ok
}
