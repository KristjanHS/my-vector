package vars

// Built-in intents that still match before the LLM (intent-allowlist.md, docs/plans/).
var forkAllowedIntents = []string{
	"intent_clock_time",
	"intent_clock_settimer_extend",
	"intent_clock_checktimer",
	"intent_weather_extend",
	"intent_photo_take_extend",
	"intent_names_username_extend",
	"intent_names_ask",
	"intent_knowledge_promptquestion",
}

// Keyphrases that hijack ordinary questions via substring match ("how deep is the water" → weather).
var forkDroppedKeyphrases = map[string][]string{
	"intent_weather_extend":           {"whether", "the other", "the water", "no other"},
	"intent_names_username_extend":    {"names"},
	"intent_photo_take_extend":        {"capture"},
	"intent_knowledge_promptquestion": {"conversation"},
	"intent_clock_settimer_extend":    {"time for", "time or", "time of", "time of for"},
}

// forkFilterIntents keeps only allowlisted intents, in file order, minus their dropped keyphrases.
func forkFilterIntents(in []JsonIntent) []JsonIntent {
	allowed := map[string]bool{}
	for _, n := range forkAllowedIntents {
		allowed[n] = true
	}
	var out []JsonIntent
	for _, it := range in {
		if !allowed[it.Name] {
			continue
		}
		drop := map[string]bool{}
		for _, k := range forkDroppedKeyphrases[it.Name] {
			drop[k] = true
		}
		var kps []string
		for _, k := range it.Keyphrases {
			if !drop[k] {
				kps = append(kps, k)
			}
		}
		it.Keyphrases = kps
		out = append(out, it)
	}
	return out
}
