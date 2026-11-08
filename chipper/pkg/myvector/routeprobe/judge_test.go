package main

import "testing"

func TestParseJudge(t *testing.T) {
	for _, c := range []struct {
		name, reply string
		want        int
		ok          bool
	}{
		{"bare", `{"score": 72}`, 72, true},
		{"fence", "```json\n{\"score\": 100}\n```", 100, true},
		{"prose", "Here is my grade: {\"score\": 0} — short and correct.", 0, true},
		{"missing", `{"grade": 80}`, 0, false},
		{"no json", "score 80", 0, false},
		{"over", `{"score": 101}`, 0, false},
		{"under", `{"score": -1}`, 0, false},
		{"string", `{"score": "high"}`, 0, false},
		{"fraction", `{"score": 80.5}`, 0, false},
	} {
		got, err := parseJudge(c.reply)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("%s: parseJudge(%q) = %d, %v; want %d, ok %v", c.name, c.reply, got, err, c.want, c.ok)
		}
	}
}
