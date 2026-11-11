package search

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Query is what a provider searches: Text (keywords), and News for a news/markets ask (both providers: news
// category, past week — search-news-quality N3, Q2).
type Query struct {
	Text string
	News bool
}

// maxKeywords caps Keywords (search-news-quality N1).
const maxKeywords = 6

// filler is always dropped by Keywords, beside every language's self and deictic tokens.
var filler = []string{
	// et
	"mis", "kas", "on", "ja", "et", "ka", "ning", "või", "palun", "saad", "saaksid", "võiksid", "võid", "oskad",
	"mida", "kus", "kes", "kuidas", "seoses", "teemal", "teemalisi", "teemalised", "teemaline", "kohta", "just",
	"rääkisid", "räägitud", "praegu", "hetkel", "midagi", "infot", "informatsiooni", "otsin", "otsiks", "otsida",
	"leia", "leida", "vaata", "järele", "uuri", "välja", "netist", "internetist", "veebist", "ütle", "räägi",
	// en
	"the", "a", "an", "of", "for", "about", "in", "to", "and", "or", "is", "are", "was", "what", "what's", "which",
	"please", "can", "could", "would", "some", "any", "related", "regarding", "up", "out", "tell", "said", "say",
}

// polite may precede a command ("can you search", "palun otsi", "kas sa saad otsida") beside self tokens.
var polite = []string{"can", "could", "would", "will", "please", "palun", "kas", "saad", "saaksid", "võiksid",
	"võid", "oskad"}

// stop is always dropped; command only in the leading run of lead and command tokens (Keywords).
var stop, command, lead = map[string]bool{}, map[string]bool{}, map[string]bool{}

func init() {
	add := func(m map[string]bool, ws []string) {
		for _, w := range ws {
			m[w] = true
		}
	}
	add(stop, filler)
	add(command, []string{"search", "google", "look", "find"})
	add(lead, polite)
	for _, c := range tables {
		add(stop, c.self)
		add(stop, c.deictic)
		add(command, c.command.exact)
		add(lead, c.self)
	}
}

// rawTokens splits s like tokens, keeping case.
func rawTokens(s string) []string {
	return strings.FieldsFunc(strings.ReplaceAll(s, "’", "'"), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

func tokens(s string) []string {
	return rawTokens(strings.ToLower(s))
}

// Keywords is the sentence's content words in order, at most maxKeywords; the sentence itself when none are left.
// A command word ("search", "otsi") is dropped only in the sentence's leading run of polite, self and command
// tokens ("can you google …" drops it, "what is the google stock price" keeps it); "US" is a word, "us" is self.
func Keywords(sentence string) string {
	out := content(sentence)
	if len(out) == 0 {
		return strings.TrimSpace(sentence)
	}
	return strings.Join(capped(out), " ")
}

func capped(ws []string) []string {
	if len(ws) > maxKeywords {
		return ws[:maxKeywords]
	}
	return ws
}

// content is Keywords' words, uncapped and without its whole-sentence fallback.
func content(sentence string) []string {
	var out []string
	leading := true
	for _, raw := range rawTokens(sentence) {
		w := strings.ToLower(raw)
		country := raw == "US"
		leading = leading && !country && (lead[w] || command[w])
		if !country && (stop[w] || (leading && command[w])) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// pointer words refer a search ask back to an earlier one (search-trigger-fixes R6) when no content word follows
// them ("otsi seda", but not "this week", "viimase kuu"); "number N" with a numeral N anywhere; "the last" like a pointer.
var pointer = map[string]bool{"seda": true, "sellest": true, "selle": true, "viimase": true, "eelmise": true,
	"it": true, "that": true, "this": true, "more": true}

var numeral = map[string]bool{"üks": true, "kaks": true, "kolm": true, "neli": true, "viis": true, "kuus": true,
	"seitse": true, "kaheksa": true, "üheksa": true, "kümme": true, "esimene": true, "teine": true, "kolmas": true,
	"one": true, "two": true, "three": true, "four": true, "five": true, "six": true, "seven": true, "eight": true,
	"nine": true, "ten": true, "first": true, "second": true, "third": true}

func isNumeral(w string) bool {
	return numeral[w] || strings.Trim(w, "0123456789") == ""
}

// numberAt: toks[i] starts "number N" / "nr N".
func numberAt(toks []string, i int) bool {
	return (toks[i] == "number" || toks[i] == "nr") && i+1 < len(toks) && isNumeral(toks[i+1])
}

// refersBack: fewer than 2 content words, "number N", or a pointer ("the last" too) with no content word after it (R6).
func refersBack(sentence string) bool {
	if len(content(sentence)) < 2 {
		return true
	}
	toks := tokens(sentence)
	for i, w := range toks {
		if numberAt(toks, i) {
			return true
		}
		if !pointer[w] && !(w == "last" && i > 0 && toks[i-1] == "the") {
			continue
		}
		after := false
		for _, n := range toks[i+1:] {
			after = after || !(stop[n] || pointer[n] || isNumeral(n) || n == "one")
		}
		if !after {
			return true
		}
	}
	return false
}

// rest is content without pointers and "number N".
func rest(sentence string) []string {
	var out []string
	ws := content(sentence)
	for i := 0; i < len(ws); i++ {
		switch {
		case numberAt(ws, i):
			i++
		case !pointer[ws[i]]:
			out = append(out, ws[i])
		}
	}
	return out
}

// maxOwn caps the asking sentence's own words in a carried query; the topic fills the rest of maxKeywords.
const maxOwn = 3

// followQuery is sentence's Query; when it refers back, prefixed with topic: the rest of the newest of at most two
// prev asks (newest first) that Route sends to search for l (R7). topic is "" when nothing is carried.
func followQuery(sentence string, prev []string, l string) (q Query, topic string) {
	q = Query{Text: Keywords(sentence), News: IsNews(sentence)}
	if !refersBack(sentence) {
		return q, ""
	}
	for i, p := range prev {
		if i == 2 {
			break
		}
		t := rest(p)
		if len(t) == 0 || Route(p, l) == None {
			continue
		}
		var own []string
		for _, w := range rest(sentence) {
			if !slices.Contains(t, w) && len(own) < maxOwn {
				own = append(own, w)
			}
		}
		if k := maxKeywords - len(own); len(t) > k {
			t = t[:k]
		}
		q.Text = strings.Join(append(slices.Clone(t), own...), " ")
		q.News = q.News || IsNews(p)
		return q, strings.Join(t, " ")
	}
	return q, ""
}

// NewsAsk: Fill takes sentence (after prev) for l as a news web lookup (Live / Recent with its query's News).
func NewsAsk(sentence string, prev []string, l string) bool {
	if c := Route(sentence, l); c != Live && c != Recent {
		return false
	}
	q, _ := followQuery(sentence, prev, l)
	return q.News
}

// lexicon maps Estonian stems to English (search-news-quality Q1); English matches the longest stem first. Every
// stem is over maxStemRunes: a shorter one misfires as a prefix ("täna" → "tänava", "hind" → "hinnang"), so it
// goes in forms as its inflected forms.
var lexicon = map[string]string{
	"finantsuudis": "financial news", "majandusuudis": "economy news", "pealkir": "headlines",
	"aktsiatur": "stock market", "aktsia": "stocks", "semikond": "semiconductor", "semikont": "semiconductor",
	"pooljuht": "semiconductor", "kiibitoot": "chipmakers", "tehisintellekt": "AI", "tehnoloog": "technology",
	"finants": "finance", "majandus": "economy", "intress": "interest rates", "inflatsioon": "inflation",
	"valuuta": "currency", "krüpto": "crypto", "elektri": "electricity", "ettevõt": "company", "tootja": "makers",
	"mõjuta": "affecting", "ameerika": "US", "viimas": "latest", "värske": "latest", "valimis": "election",
}

const maxStemRunes = 5

// forms maps exact Estonian word forms (short stems' inflections) to English.
var forms = func() map[string]string {
	m := map[string]string{}
	for en, ws := range map[string][]string{
		"news": {"uudis", "uudise", "uudist", "uudised", "uudiseid", "uudisi", "uudiste", "uudises", "uudisest",
			"uudisele", "uudisega", "uudistes", "uudistest", "uudistele", "uudistega"},
		"stock market": {"börs", "börsi", "börsil", "börsile", "börsilt", "börsid", "börse", "börside", "börsidel",
			"börsidele", "börsidelt", "börsis", "börsist"},
		"market": {"turg", "turu", "turgu", "turul", "turule", "turult", "turud", "turge", "turgude", "turgudel",
			"turgudele", "turgudelt", "turus", "turust"},
		"chips":         {"kiip", "kiibi", "kiipi", "kiibid", "kiipe", "kiipide", "kiibist", "kiipidest", "kiipidele"},
		"exchange rate": {"kurss", "kursi", "kurssi", "kursid", "kursse", "kursside", "kursil", "kursile"},
		"oil":           {"nafta", "naftat", "naftas", "naftast", "naftale", "naftaga", "naftahind", "naftahinna"},
		"gold":          {"kuld", "kulla", "kulda", "kullas", "kullast", "kullale", "kullaga"},
		"price": {"hind", "hinna", "hinda", "hinnad", "hindu", "hindade", "hinnas", "hinnast", "hinnale",
			"hinnaga"},
		"Estonia":   {"eesti", "eestis", "eestist", "eestisse", "eestil", "eestile", "eestit"},
		"Finland":   {"soome", "soomes", "soomest", "soomesse", "soomel", "soomele"},
		"China":     {"hiina", "hiinas", "hiinast", "hiinasse", "hiinal", "hiinale"},
		"today":     {"täna", "tänane", "tänase", "tänast", "tänased", "tänaseid", "tänasel", "tänasest", "tänaseks"},
		"yesterday": {"eile", "eilne", "eilse", "eilset", "eilsed", "eilseid", "eilsel", "eilsest"},
		"week":      {"nädal", "nädala", "nädalat", "nädalas", "nädalal", "nädalad", "nädalaid", "nädalate", "nädalast"},
		"war":       {"sõda", "sõja", "sõdu", "sõjad", "sõdade", "sõjas", "sõjast", "sõjale", "sõjaga"},
	} {
		for _, w := range ws {
			m[w] = en
		}
	}
	return m
}()

var stems = func() []string {
	s := make([]string, 0, len(lexicon))
	for k := range lexicon {
		s = append(s, k)
	}
	slices.SortFunc(s, func(a, b string) int { return utf8.RuneCountInString(b) - utf8.RuneCountInString(a) })
	return s
}()

// English maps each word through forms, then lexicon (words without either pass through), dropping repeats.
func English(q string) string {
	var out []string
	for _, w := range strings.Fields(q) {
		lw := strings.ToLower(w)
		if en, ok := forms[lw]; ok {
			w = en
		} else {
			for _, st := range stems {
				if strings.HasPrefix(lw, st) {
					w = lexicon[st]
					break
				}
			}
		}
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// matchRunes: a query word of at least this many runes matches a result word holding its first matchRunes runes
// anywhere ("kurss" in "vahetuskurss") or starting with its first matchRunes-1 ("lätis" → "Läti"); a shorter one
// (from minMatchRunes) must start a result word, and a still shorter one ("ai", "us") never counts (N6).
const (
	matchRunes    = 5
	minMatchRunes = 3
)

// cueWord: a news, time or ask word with no topic in it ("tänased uudised", "news today"); it never counts as a
// match (search-news-quality Q13).
func cueWord(w string) bool {
	for _, p := range []string{"uudis", "headline", "pealkir", "värsk", "viima", "täna", "eil", "nädal", "uusim",
		"ülevaa", "kokkuvõt", "today", "tonight", "week", "recent", "breaking", "update", "latest"} {
		if strings.HasPrefix(w, p) {
			return true
		}
	}
	return slices.Contains([]string{"news", "current", "top", "new", "uued", "uusi", "uut", "anna", "räägi", "ütle"}, w)
}

// Relevant keeps the results whose title or snippet matches a topic word of qs (search-news-quality N6, Q13/Q14);
// with no topic word left (only cue words, or none countable) every result is kept.
func Relevant(rs []Result, qs ...string) []Result {
	var ws []string
	for _, q := range qs {
		for _, w := range tokens(q) {
			w = strings.TrimSuffix(w, "'s")
			if utf8.RuneCountInString(w) >= minMatchRunes && !cueWord(w) && !slices.Contains(ws, w) {
				ws = append(ws, w)
			}
		}
	}
	if len(ws) == 0 {
		return rs
	}
	var out []Result
	for _, r := range rs {
		if matches(ws, tokens(r.Title+" "+r.Snippet)) {
			out = append(out, r)
		}
	}
	return out
}

func matches(ws, rws []string) bool {
	for _, w := range ws {
		long := utf8.RuneCountInString(w) >= matchRunes
		stem, head := w, w
		if long {
			stem, head = string([]rune(w)[:matchRunes]), string([]rune(w)[:matchRunes-1])
		}
		for _, rw := range rws {
			if (long && strings.Contains(rw, stem)) || strings.HasPrefix(rw, head) {
				return true
			}
		}
	}
	return false
}

// IsNews: the sentence asks for news or markets (search-news-quality N3). Bare "market"/"stock" are not cues
// ("christmas market", "in stock"); the pair "stock market(s)" is.
func IsNews(sentence string) bool {
	toks := tokens(sentence)
	for i, w := range toks {
		if strings.Contains(w, "uudis") || slices.Contains([]string{"news", "headlines", "stocks"}, w) {
			return true
		}
		if w == "stock" && i+1 < len(toks) && (toks[i+1] == "market" || toks[i+1] == "markets") {
			return true
		}
		for _, p := range []string{"börs", "aktsia", "pealkir"} {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
	}
	return false
}
