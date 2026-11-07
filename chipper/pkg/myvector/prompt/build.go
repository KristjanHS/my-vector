package prompt

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Vars holds the rendered block for each template tag; an empty block drops out of the prompt.
type Vars struct {
	Now, Lang, User, Facts, Diary, Search, Speech, Commands string
}

var squeeze = regexp.MustCompile(`\n{3,}`)

// Render fills the tags in one pass (block text is never rescanned, so {{cmd||param}} survives) and squeezes
// the gaps empty blocks leave.
func Render(tmpl string, v Vars) string {
	t := strings.TrimSpace
	out := strings.NewReplacer(
		"{now}", t(v.Now), "{lang}", t(v.Lang), "{user}", t(v.User), "{facts}", t(v.Facts),
		"{diary}", t(v.Diary), "{search}", t(v.Search), "{speech}", t(v.Speech), "{commands}", t(v.Commands),
	).Replace(tmpl)
	return strings.TrimSpace(squeeze.ReplaceAllString(out, "\n\n"))
}

// Estimate is a rough token count: one token per 3 runes, rounded up.
func Estimate(s string) int {
	return (utf8.RuneCountInString(s) + 2) / 3
}
