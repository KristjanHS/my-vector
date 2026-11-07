package main

import "testing"

func TestParseScore(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    score
		wantErr bool
	}{
		{`{"relevance": 3, "et": 1}`, score{3, 1}, false},
		{"```json\n{\"relevance\":0,\"et\":0}\n```", score{0, 0}, false},
		{`not json`, score{}, true},
		{`{"relevance": 2`, score{}, true},
		{`{"relevance": 4, "et": 1}`, score{}, true},
		{`{"relevance": -1, "et": 0}`, score{}, true},
		{`{"relevance": 2, "et": 2}`, score{}, true},
		{`{"relevance": 2}`, score{}, true},
	} {
		got, err := parseScore(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("parseScore(%q) = %v, %v; want %v, err %v", c.in, got, err, c.want, c.wantErr)
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
