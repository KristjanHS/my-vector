package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Provider is one web search backend (plan W4); q comes from Keywords (search-news-quality N1).
type Provider interface {
	Name() string
	Search(ctx context.Context, q Query, lang string, n int) ([]Result, error)
}

// Endpoints; vars so tests point them at httptest.
var (
	TavilyURL = "https://api.tavily.com/search"
)

// Tavily searches api.tavily.com with a bearer key; it has no language parameter.
type Tavily struct{ Key string }

// Searxng searches a self-hosted SearXNG at URL (format=json must be enabled).
type Searxng struct{ URL string }

func (Tavily) Name() string  { return "tavily" }
func (Searxng) Name() string { return "searxng" }

type hit struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Content   string `json:"content"`
	Published string `json:"published_date"`
	PubSearx  string `json:"publishedDate"`
}

func results(hs []hit, n int) []Result {
	var rs []Result
	for _, h := range hs {
		if len(rs) == n {
			break
		}
		t, s := strings.TrimSpace(h.Title), strings.TrimSpace(h.Content)
		if t == "" && s == "" {
			continue
		}
		d := h.Published
		if d == "" {
			d = h.PubSearx
		}
		rs = append(rs, Result{Title: t, URL: h.URL, Snippet: s, Date: d})
	}
	return rs
}

// do sends req and decodes a 200 reply (≤ 1 MiB) into out; the error names the host, never the key.
func do(req *http.Request, out any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ue, ok := err.(*url.Error); ok {
			return fmt.Errorf("%s: %w", req.URL.Host, ue.Err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func postBearer(ctx context.Context, endpoint, key string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	return do(req, out)
}

func (p Tavily) Search(ctx context.Context, q Query, _ string, n int) ([]Result, error) {
	var r struct{ Results []hit }
	body := map[string]any{"query": q.Text, "max_results": n, "search_depth": "basic"}
	if q.News {
		body["topic"], body["time_range"] = "news", "week"
	}
	if err := postBearer(ctx, TavilyURL, p.Key, body, &r); err != nil {
		return nil, err
	}
	return results(r.Results, n), nil
}

// Search asks SearXNG in English for news and on an English turn, in Estonian otherwise (search-news-quality Q3).
func (p Searxng) Search(ctx context.Context, q Query, l string, n int) ([]Result, error) {
	var r struct{ Results []hit }
	ql := "en"
	if !q.News && l == lang.Estonian {
		ql = "et"
	}
	v := url.Values{"q": {q.Text}, "format": {"json"}, "language": {ql}}
	if q.News {
		v.Set("categories", "news")
		v.Set("time_range", "week")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.URL, "/")+"/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := do(req, &r); err != nil {
		return nil, err
	}
	return results(r.Results, n), nil
}
