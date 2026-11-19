package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"nuan/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(status int, body string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestDeepSeekEndpointNormalization(t *testing.T) {
	cases := map[string]string{
		"":                                 "https://api.deepseek.com/v1/tools/search",
		"https://api.deepseek.com":         "https://api.deepseek.com/v1/tools/search",
		"https://api.deepseek.com/":        "https://api.deepseek.com/v1/tools/search",
		"https://api.deepseek.com/v1":      "https://api.deepseek.com/v1/tools/search",
		"https://api.deepseek.com/v1/":     "https://api.deepseek.com/v1/tools/search",
		"https://relay.example.com/api/v1": "https://relay.example.com/api/v1/tools/search",
	}
	for in, want := range cases {
		if got := DeepSeekEndpoint(in, "search"); got != want {
			t.Errorf("DeepSeekEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeQueryClipsTo128Runes(t *testing.T) {
	if got := SanitizeQuery("  \n\t  "); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	long := strings.Repeat("今", 150)
	got := SanitizeQuery("  " + long + "\n测试 ")
	if n := utf8.RuneCountInString(got); n != 128 {
		t.Fatalf("rune count = %d, want 128", n)
	}
}

func TestValidateFetchURL(t *testing.T) {
	good := []string{
		"https://example.com/article",
		"http://news.cn/2026/11/18/c_123.htm?from=rss",
	}
	for _, u := range good {
		if _, err := ValidateFetchURL(u); err != nil {
			t.Errorf("expected valid %q, got %v", u, err)
		}
	}
	bad := []string{
		"",
		"ftp://example.com/file",
		"https://user:pass@example.com/secret",
		"http://localhost:8080/healthz",
		"http://127.0.0.1:8080/api",
		"http://192.168.1.1/admin",
		"https://example.com/" + strings.Repeat("a", 8200),
	}
	for _, u := range bad {
		if _, err := ValidateFetchURL(u); err == nil {
			t.Errorf("expected invalid URL %q", u[:min(len(u), 50)])
		}
	}
}

func TestDeepSeekSearchFetchAndRetry(t *testing.T) {
	st := testStore(t)
	svc := NewService(st)

	var searchCalls atomic.Int32
	var authCalls atomic.Int32
	svc.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer test-key-123" {
				authCalls.Add(1)
				return jsonResp(401, `{"error":{"message":"Authentication Fails","type":"authentication_error","code":"invalid_request_error"}}`), nil
			}
			switch r.URL.Path {
			case "/v1/tools/search":
				n := searchCalls.Add(1)
				if n == 1 {
					// Transient 429 should be retried once automatically.
					return jsonResp(429, `{"error":{"message":"rate limit exceeded","code":"rate_limit_error"}}`), nil
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if utf8.RuneCountInString(body["query"].(string)) > 128 {
					t.Errorf("query exceeded 128 runes")
				}
				return jsonResp(200, `{
					"results": [{
						"rerank_score": 2.4,
						"title": "国内金价动态",
						"url": "https://example.com/gold",
						"snippet": "短摘要",
						"summary": "详细摘要内容",
						"content": "",
						"site_name": "澎湃新闻",
						"site_icon": null,
						"date": "2026-11-18T16:00:00+00:00",
						"authority": 3
					}]
				}`), nil
			case "/v1/tools/fetch":
				return jsonResp(200, `{"title":"国内金价动态","content":"# 国内金价动态\n\n正文内容"}`), nil
			default:
				return jsonResp(404, `{}`), nil
			}
		}),
	}

	// Non-retryable 401 should fail immediately on first attempt without retry.
	_, _, err := svc.Test(t.Context(), &store.SearchConfig{Kind: KindDeepSeek, APIKey: "wrong", BaseURL: "https://api.deepseek.com"})
	if err == nil || authCalls.Load() != 1 {
		t.Fatalf("expected single 401 call, err=%v calls=%d", err, authCalls.Load())
	}

	if err := st.UpsertSearchConfig(t.Context(), &store.SearchConfig{
		Kind:    KindDeepSeek,
		APIKey:  "test-key-123",
		BaseURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(t.Context(), SettingActiveProvider, KindDeepSeek); err != nil {
		t.Fatal(err)
	}

	en, canFetch, kind := svc.ActiveStatus(t.Context())
	if !en || !canFetch || kind != KindDeepSeek {
		t.Fatalf("ActiveStatus = (%v, %v, %q)", en, canFetch, kind)
	}

	sres, err := svc.Search(t.Context(), SearchRequest{
		Query:            strings.Repeat("黄金价格 ", 40),
		Count:            5,
		OriginalQuestion: "今天国内黄金价格多少？",
	})
	if err != nil {
		t.Fatal(err)
	}
	if searchCalls.Load() != 2 || len(sres.Results) != 1 || sres.Results[0].SiteName != "澎湃新闻" || sres.CostUSD <= 0 {
		t.Fatalf("unexpected search response: calls=%d res=%+v", searchCalls.Load(), sres)
	}

	fres, err := svc.Fetch(t.Context(), "https://example.com/gold")
	if err != nil {
		t.Fatal(err)
	}
	if fres.Title != "国内金价动态" || !strings.Contains(fres.Content, "正文内容") || fres.CostUSD <= 0 {
		t.Fatalf("unexpected fetch response: %+v", fres)
	}
}

type mockSerpDriver struct{}

func (mockSerpDriver) Kind() string { return KindSerp }
func (mockSerpDriver) Search(_ context.Context, _ SearchRequest) (*SearchResponse, error) {
	return &SearchResponse{Results: []SearchResult{{Title: "Serp Hit", URL: "https://serp.example.com"}}}, nil
}
func (mockSerpDriver) Test(_ context.Context) (int, error) { return 1, nil }

func TestPluggableDriverRegistration(t *testing.T) {
	st := testStore(t)
	svc := NewService(st)
	svc.RegisterDriver(KindSerp, func(_ *Service, _ *store.SearchConfig) (Driver, error) {
		return mockSerpDriver{}, nil
	})
	_ = st.UpsertSearchConfig(t.Context(), &store.SearchConfig{Kind: KindSerp, APIKey: "serp-key"})
	_ = st.SetSetting(t.Context(), SettingActiveProvider, KindSerp)

	en, canFetch, kind := svc.ActiveStatus(t.Context())
	if !en || canFetch || kind != KindSerp {
		t.Fatalf("expected serp enabled without fetch, got (%v, %v, %q)", en, canFetch, kind)
	}
	res, err := svc.Search(t.Context(), SearchRequest{Query: "hello"})
	if err != nil || len(res.Results) != 1 || res.Results[0].Title != "Serp Hit" {
		t.Fatalf("serp search: %+v %v", res, err)
	}
	if _, err := svc.Fetch(t.Context(), "https://serp.example.com"); err != ErrFetchUnsupported {
		t.Fatalf("expected ErrFetchUnsupported, got %v", err)
	}
}
