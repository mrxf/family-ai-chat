package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/chat"
	"nuan/internal/llm"
	"nuan/internal/store"
)

type searchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f searchRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockJSONResp(status int, body string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestWebSearchAdminConfigAndFamilyChatFlow(t *testing.T) {
	f := newAPIFixture(t)

	var upstreamToolsSeen atomic.Int32
	var chatRound atomic.Int32

	srv := newHTTPTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"content":"今日金价查询"}}]}`)
			return
		}
		tools, _ := body["tools"].([]any)
		hasSearchTool := false
		for _, tl := range tools {
			if strings.Contains(fmt.Sprint(tl), "web_search") {
				hasSearchTool = true
			}
		}
		if hasSearchTool {
			upstreamToolsSeen.Add(1)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if !hasSearchTool {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"目前未开启联网搜索。\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}

		n := chatRound.Add(1)
		switch n {
		case 1:
			// Round 0: model invokes web_search.
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_s1","type":"function","function":{"name":"web_search","arguments":"{\"queries\":[\"今日国内金价\"]}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
		case 2:
			// Round 1: model invokes web_fetch on the top result.
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_f1","type":"function","function":{"name":"web_fetch","arguments":"{\"url\":\"https://m.thepaper.cn/newsDetail_forward_1\",\"focus\":\"工商银行\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
		default:
			msgsRaw, _ := json.Marshal(body["messages"])
			if !strings.Contains(string(msgsRaw), "澎湃新闻") || !strings.Contains(string(msgsRaw), "工商银行积存金") {
				t.Errorf("expected search & fetch results in LLM messages, got %s", string(msgsRaw))
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"据澎湃新闻今日报道，国内金价现报约 796 元/克。\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	if l, ok := srv.Listener.(*memoryListener); ok {
		transport := llm.NewClient("", srv.URL, "").HTTP.Transport.(*http.Transport)
		transport.CloseIdleConnections()
		transport.DialContext, transport.Proxy = l.dial, nil
	}

	ctx := t.Context()
	p := &store.Provider{ID: "search-llm", Kind: "deepseek", Name: "DeepSeek", BaseURL: srv.URL, APIKey: "provider-deepseek-key-9999", Enabled: true}
	if err := f.st.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	m := &store.Model{ID: "search-model", ProviderID: p.ID, Kind: store.KindChat, Tools: true, UpstreamModel: "deepseek-chat", DisplayName: "DeepSeek", Enabled: true}
	if err := f.st.UpsertModel(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSetting(ctx, "default_model_id", m.ID); err != nil {
		t.Fatal(err)
	}

	// Mock DeepSeek Search & Fetch API transport.
	f.api.Search.HTTPClient = &http.Client{
		Transport: searchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer test-search-secret-key-12345" && authHeader != "Bearer provider-deepseek-key-9999" {
				return mockJSONResp(401, `{"error":{"message":"invalid api key"}}`), nil
			}
			switch r.URL.Path {
			case "/v1/tools/search":
				time.Sleep(50 * time.Millisecond)
				return mockJSONResp(200, `{
					"results": [{
						"rerank_score": 2.3,
						"title": "国内金价最新行情",
						"url": "https://m.thepaper.cn/newsDetail_forward_1",
						"snippet": "上海黄金交易所现货黄金最新报价",
						"summary": "今日国际金价波动，国内金价同步调整。",
						"content": "",
						"site_name": "澎湃新闻",
						"date": "2026-11-18T16:00:00+00:00",
						"authority": 3
					}]
				}`), nil
			case "/v1/tools/fetch":
				return mockJSONResp(200, `{
					"title": "国内金价最新行情",
					"content": "# 国内金价最新行情\n\n工商银行积存金报 796.23 元/克。"
				}`), nil
			default:
				return mockJSONResp(404, `{}`), nil
			}
		}),
	}

	// 1. Not pre-configured by default: web_search is false for family members, and /api/admin/search rejects non-admins.
	var boot struct {
		Features struct {
			WebSearch      bool   `json:"web_search"`
			SearchProvider string `json:"search_provider"`
		} `json:"features"`
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features.WebSearch || boot.Features.SearchProvider != "" {
		t.Fatalf("expected web_search disabled by default, got %+v", boot.Features)
	}
	f.json("GET", "/api/admin/search", nil, 403, nil)

	// 2. Admin configures & enables DeepSeek Web Search.
	f.asAdmin()
	// Cannot enable before key is configured.
	f.json("PUT", "/api/admin/search", map[string]any{"active": "deepseek"}, 400, nil)
	// Reserved SerpAPI cannot be enabled yet.
	f.json("PUT", "/api/admin/search", map[string]any{"active": "serp"}, 400, nil)

	// Verify reusable_provider_key_hint is detected from existing DeepSeek provider.
	var initialAdminState struct {
		Providers []struct {
			Kind                    string `json:"kind"`
			ReusableProviderKeyHint string `json:"reusable_provider_key_hint"`
		} `json:"providers"`
	}
	f.json("GET", "/api/admin/search", nil, 200, &initialAdminState)
	if initialAdminState.Providers[0].ReusableProviderKeyHint == "" {
		t.Fatalf("expected reusable provider key hint, got %+v", initialAdminState)
	}

	var testRes struct {
		OK          bool   `json:"ok"`
		ResultCount int    `json:"result_count"`
		Message     string `json:"message"`
	}
	f.json("POST", "/api/admin/search/test", map[string]any{
		"kind":     "deepseek",
		"api_key":  "test-search-secret-key-12345",
		"base_url": "https://api.deepseek.com",
	}, 200, &testRes)
	if !testRes.OK || testRes.ResultCount != 1 {
		t.Fatalf("expected test ok with 1 result, got %+v", testRes)
	}

	var adminState struct {
		Active        string `json:"active"`
		Enabled       bool   `json:"enabled"`
		SupportsFetch bool   `json:"supports_fetch"`
		Providers     []struct {
			Kind       string `json:"kind"`
			HasKey     bool   `json:"has_key"`
			APIKeyHint string `json:"api_key_hint"`
			Ready      bool   `json:"ready"`
		} `json:"providers"`
	}
	f.json("PUT", "/api/admin/search", map[string]any{
		"active": "deepseek",
		"config": map[string]any{
			"kind":     "deepseek",
			"api_key":  "test-search-secret-key-12345",
			"base_url": "https://api.deepseek.com",
		},
	}, 200, &adminState)
	if !adminState.Enabled || !adminState.SupportsFetch || adminState.Active != "deepseek" {
		t.Fatalf("unexpected admin search state: %+v", adminState)
	}
	if !adminState.Providers[0].HasKey || strings.Contains(adminState.Providers[0].APIKeyHint, "secret") {
		t.Fatalf("raw key leaked in hint: %+v", adminState.Providers[0])
	}

	// Changing BaseURL origin without re-supplying API key must be rejected to protect stored credentials.
	f.json("POST", "/api/admin/search/test", map[string]any{
		"kind":     "deepseek",
		"base_url": "https://evil.example.com",
	}, 400, nil)

	// 3. Switch back to normal family member: web_search is now available and works end-to-end!
	memberRole := "member"
	if _, err := f.st.UpdateUser(ctx, f.user.ID, store.UserPatch{Role: &memberRole}); err != nil {
		t.Fatal(err)
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if !boot.Features.WebSearch || boot.Features.SearchProvider != "deepseek" {
		t.Fatalf("expected web_search enabled for family member, got %+v", boot.Features)
	}

	var sent chat.SendResult
	f.json("POST", "/api/chat", map[string]any{"content": "今天国内黄金多少钱一克？"}, 200, &sent)
	sr := f.stream(sent.Assistant.ID)
	sawSteps := false
	sawSources := false
	var finalMsg *store.Message
	for {
		ev := sr.next()
		switch ev.name {
		case "snapshot":
			var snap map[string]any
			_ = json.Unmarshal(ev.data, &snap)
			if steps, ok := snap["steps"].([]any); ok && len(steps) > 0 {
				sawSteps = true
			}
			if srcs, ok := snap["sources"].([]any); ok && len(srcs) > 0 {
				sawSources = true
			}
		case "steps":
			sawSteps = true
		case "sources":
			sawSources = true
		case "done":
			finalMsg = decodeEvent[chat.FinalEvent](t, ev).Message
		}
		if finalMsg != nil {
			break
		}
	}
	if !sawSteps || !sawSources || !strings.Contains(finalMsg.Content, "796") {
		t.Fatalf("expected steps, sources and final answer: steps=%v sources=%v msg=%+v", sawSteps, sawSources, finalMsg)
	}

	// Reload conversation and verify sources were persisted on the assistant message.
	var detail struct {
		Messages []struct {
			Role    string            `json:"role"`
			Content string            `json:"content"`
			Sources []store.WebSource `json:"sources"`
		} `json:"messages"`
	}
	f.json("GET", "/api/conversations/"+sent.Conversation.ID, nil, 200, &detail)
	if len(detail.Messages) != 2 || len(detail.Messages[1].Sources) != 1 || detail.Messages[1].Sources[0].SiteName != "澎湃新闻" {
		t.Fatalf("persisted messages missing sources: %+v", detail.Messages)
	}

	// 4. Admin turns off web search -> family chat stops sending web_search tools.
	f.asAdmin()
	f.json("PUT", "/api/admin/search", map[string]any{"active": ""}, 200, &adminState)
	if adminState.Enabled {
		t.Fatal("expected search disabled")
	}
	if _, err := f.st.UpdateUser(ctx, f.user.ID, store.UserPatch{Role: &memberRole}); err != nil {
		t.Fatal(err)
	}
	prevToolsSeen := upstreamToolsSeen.Load()
	sent2 := f.send(map[string]any{"content": "你好"})
	_, _, _ = f.stream(sent2.Assistant.ID).finish()
	if upstreamToolsSeen.Load() != prevToolsSeen {
		t.Fatal("expected web_search tool not sent after admin disabled it")
	}
}

func TestLiveDeepSeekSearchAndJudgment(t *testing.T) {
	liveKey := strings.TrimSpace(os.Getenv("NUAN_LIVE_DEEPSEEK_KEY"))
	if liveKey == "" {
		t.Skip("NUAN_LIVE_DEEPSEEK_KEY not set")
	}
	f := newAPIFixture(t)
	f.http.Client().Timeout = 60 * time.Second
	ctx := t.Context()

	p := &store.Provider{
		ID:      "live-ds",
		Kind:    "deepseek",
		Name:    "DeepSeek Official",
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  liveKey,
		Enabled: true,
	}
	if err := f.st.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	m := &store.Model{
		ID:            "live-ds-chat",
		ProviderID:    p.ID,
		Kind:          store.KindChat,
		Tools:         true,
		UpstreamModel: "deepseek-chat",
		DisplayName:   "DeepSeek Chat",
		ContextWindow: 64000,
		MaxOutput:     2048,
		Enabled:       true,
	}
	if err := f.st.UpsertModel(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSetting(ctx, "default_model_id", m.ID); err != nil {
		t.Fatal(err)
	}

	// Admin enables DeepSeek web search by reusing the configured DeepSeek provider key.
	f.asAdmin()
	f.json("PUT", "/api/admin/search", map[string]any{
		"active": "deepseek",
		"config": map[string]any{
			"kind":               "deepseek",
			"reuse_provider_key": true,
		},
	}, 200, nil)

	// Member 1: Real-time question MUST trigger web_search and attach real web sources.
	memberRole := "member"
	_, _ = f.st.UpdateUser(ctx, f.user.ID, store.UserPatch{Role: &memberRole})

	readLiveSSE := func(msgID string) (*store.Message, []string) {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", f.http.URL+"/api/messages/"+msgID+"/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+f.token)
		resp, err := f.http.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 4096), 2<<20)
		var evName string
		var evData []byte
		var stepLogs []string
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if evName == "steps" {
					stepLogs = append(stepLogs, string(evData))
				} else if evName == "done" {
					var fin chat.FinalEvent
					if err := json.Unmarshal(evData, &fin); err != nil {
						t.Fatal(err)
					}
					return fin.Message, stepLogs
				}
				evName, evData = "", nil
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				evName = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				evData = append(evData, strings.TrimPrefix(line, "data: ")...)
			}
		}
		t.Fatalf("SSE ended unexpectedly: %v", sc.Err())
		return nil, nil
	}

	var sent1 chat.SendResult
	f.json("POST", "/api/chat", map[string]any{"content": "今天国内黄金多少钱一克？"}, 200, &sent1)
	final1, liveSteps := readLiveSSE(sent1.Assistant.ID)
	t.Logf("Live real-time reply: %s (sources=%d, stepEvents=%d)", final1.Content, len(final1.Sources), len(liveSteps))
	if len(final1.Sources) == 0 {
		t.Fatalf("expected live web_search to be triggered for today's gold price, got 0 sources: %s", final1.Content)
	}

	// Member 2: Timeless math/common-sense question MUST NOT trigger web_search.
	var sent2 chat.SendResult
	f.json("POST", "/api/chat", map[string]any{"content": "125 乘以 8 等于多少？直接简短回答"}, 200, &sent2)
	final2, liveSteps2 := readLiveSSE(sent2.Assistant.ID)
	t.Logf("Live non-realtime reply: %s (sources=%d, stepEvents=%d)", final2.Content, len(final2.Sources), len(liveSteps2))
	if len(final2.Sources) != 0 {
		t.Fatalf("expected NO web_search on basic math question, got %d sources", len(final2.Sources))
	}
}
