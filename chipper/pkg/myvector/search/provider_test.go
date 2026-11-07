package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seen struct {
	auth, method string
	query        map[string][]string
	body         map[string]any
}

// fakeProvider answers every request with status + body and records what it saw.
func fakeProvider(t *testing.T, status int, body string) (*httptest.Server, *seen) {
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.auth, s.method, s.query = r.Header.Get("Authorization"), r.Method, r.URL.Query()
		json.NewDecoder(r.Body).Decode(&s.body)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

const providerFixture = `{"results":[
 {"title":"Tartu ilm","url":"https://a.ee/1","content":"Homme sajab.","published_date":"2026-11-07","publishedDate":""},
 {"title":"  ","url":"https://a.ee/empty","content":" "},
 {"title":"Teine","url":"https://a.ee/2","content":"Päike.","publishedDate":"2026-11-06T10:00:00"},
 {"title":"Kolmas","url":"https://a.ee/3","content":"Pilves."}]}`

func TestProviders(t *testing.T) {
	cases := []struct {
		name string
		mk   func(url string) Provider
		set  func(url string) func()
	}{
		{"tavily", func(string) Provider { return Tavily{Key: "K1"} }, func(u string) func() { o := TavilyURL; TavilyURL = u; return func() { TavilyURL = o } }},
		{"ollama", func(string) Provider { return Ollama{Key: "K2"} }, func(u string) func() { o := OllamaURL; OllamaURL = u; return func() { OllamaURL = o } }},
		{"searxng", func(u string) Provider { return Searxng{URL: u + "/"} }, func(string) func() { return func() {} }},
	}
	for _, c := range cases {
		srv, s := fakeProvider(t, 200, providerFixture)
		restore := c.set(srv.URL)
		p := c.mk(srv.URL)
		rs, err := p.Search(context.Background(), "kas homme sajab?", "et-EE", 2)
		restore()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var got []string
		for _, r := range rs {
			got = append(got, r.Title+"|"+r.URL+"|"+r.Snippet+"|"+r.Date)
		}
		want := "Tartu ilm|https://a.ee/1|Homme sajab.|2026-11-07,Teine|https://a.ee/2|Päike.|2026-11-06T10:00:00"
		if p.Name() != c.name || strings.Join(got, ",") != want {
			t.Errorf("%s: %q results %q, want %q", c.name, p.Name(), got, want)
		}
		switch c.name {
		case "searxng":
			if s.method != "GET" || s.auth != "" || s.query["language"][0] != "et" || s.query["format"][0] != "json" || s.query["q"][0] != "kas homme sajab?" {
				t.Errorf("searxng request %s auth %q query %v", s.method, s.auth, s.query)
			}
		default:
			key := map[string]string{"tavily": "K1", "ollama": "K2"}[c.name]
			if s.method != "POST" || s.auth != "Bearer "+key || s.body["query"] != "kas homme sajab?" || s.body["max_results"] != float64(2) {
				t.Errorf("%s request %s auth %q body %v", c.name, s.method, s.auth, s.body)
			}
		}
	}
}

func TestProvidersErrorStatus(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		srv, _ := fakeProvider(t, status, `{"error":"no"}`)
		oldT, oldO := TavilyURL, OllamaURL
		TavilyURL, OllamaURL = srv.URL, srv.URL
		for _, p := range []Provider{Tavily{Key: "SECRET"}, Ollama{Key: "SECRET"}, Searxng{URL: srv.URL}} {
			rs, err := p.Search(context.Background(), "q", "et-EE", 3)
			if err == nil || rs != nil || strings.Contains(err.Error(), "SECRET") {
				t.Errorf("%s status %d: results %v err %v; want an error without the key", p.Name(), status, rs, err)
			}
		}
		TavilyURL, OllamaURL = oldT, oldO
	}
}

func TestOllamaMaxResultsCap(t *testing.T) {
	srv, s := fakeProvider(t, 200, `{"results":[]}`)
	old := OllamaURL
	OllamaURL = srv.URL
	defer func() { OllamaURL = old }()
	if _, err := (Ollama{Key: "K"}).Search(context.Background(), "q", "en-US", 25); err != nil || s.body["max_results"] != float64(10) {
		t.Errorf("max_results %v err %v; want the API ceiling 10", s.body["max_results"], err)
	}
}
