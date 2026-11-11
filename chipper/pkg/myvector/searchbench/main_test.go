package main

import "testing"

func TestParseScore(t *testing.T) {
	for _, c := range []struct {
		in      string
		news    bool
		want    score
		wantErr bool
	}{
		{`{"relevance": 3, "et": 1}`, false, score{Relevance: 3, ET: 1}, false},
		{"```json\n{\"relevance\":0,\"et\":0}\n```", false, score{}, false},
		{`not json`, false, score{}, true},
		{`{"relevance": 2`, false, score{}, true},
		{`{"relevance": 4, "et": 1}`, false, score{}, true},
		{`{"relevance": -1, "et": 0}`, false, score{}, true},
		{`{"relevance": 2, "et": 2}`, false, score{}, true},
		{`{"relevance": 2}`, false, score{}, true},
		{`{"relevance": 2, "dated": 1}`, false, score{}, true},
		{`{"relevance": 2, "dated": 1}`, true, score{Relevance: 2, Dated: 1}, false},
		{`{"relevance": 2, "et": 1}`, true, score{}, true},
		{`{"relevance": 3, "dated": 2}`, true, score{}, true},
	} {
		got, err := parseScore(c.in, c.news)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("parseScore(%q, news=%v) = %v, %v; want %v, err %v", c.in, c.news, got, err, c.want, c.wantErr)
		}
	}
}

func TestPick(t *testing.T) {
	rows := []row{
		{name: "slow", relevance: 3, et: 1, judged: true, medianMS: 2600},
		{name: "a", relevance: 2.5, et: 0.5, judged: true, medianMS: 900},
		{name: "b", relevance: 2.5, et: 0.8, judged: true, medianMS: 1500},
		{name: "c", relevance: 2.5, et: 0.8, judged: true, medianMS: 1200},
	}
	if got, _ := pick(rows); got != "c" {
		t.Fatalf("pick = %q, want c (slow disqualified; tie on relevance → et → median ms)", got)
	}
	if _, ok := pick([]row{{name: "x", medianMS: 100}}); ok {
		t.Fatal("unjudged row picked")
	}
}
