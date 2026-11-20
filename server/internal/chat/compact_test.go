package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type recordedCall struct {
	Model    string
	Stream   bool
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	RawBytes int
}

type fakeUpstream struct {
	mu      sync.Mutex
	calls   []recordedCall
	handler func(call recordedCall) (streamBody string, completeText string)
}

func (u *fakeUpstream) client(kind string) *llm.Client {
	c := llm.NewClient(kind, "https://mock.local/v1", "sk-test")
	c.HTTP = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload struct {
				Model    string `json:"model"`
				Stream   bool   `json:"stream"`
				Messages []struct {
					Role    string `json:"role"`
					Content any    `json:"content"`
				} `json:"messages"`
				Tools []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tools"`
			}
			_ = json.Unmarshal(raw, &payload)
			rec := recordedCall{
				Model:    payload.Model,
				Stream:   payload.Stream,
				Messages: payload.Messages,
				Tools:    payload.Tools,
				RawBytes: len(raw),
			}
			u.mu.Lock()
			u.calls = append(u.calls, rec)
			h := u.handler
			u.mu.Unlock()

			streamSSE, completeTxt := "", "【话题与背景】整理后的前情提要。\n【关键事实与数字】氨氯地平 5mg。"
			if h != nil {
				streamSSE, completeTxt = h(rec)
			}
			if payload.Stream {
				if streamSSE == "" {
					streamSSE = "data: {\"choices\":[{\"delta\":{\"content\":\"好的，没问题。\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n"
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(streamSSE)),
				}, nil
			}
			respJSON, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": completeTxt}},
				},
				"usage": map[string]any{"prompt_tokens": 80, "completion_tokens": 40},
			})
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(respJSON)),
			}, nil
		}),
	}
	return c
}

func (u *fakeUpstream) snapshot() []recordedCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedCall(nil), u.calls...)
}

func injectClient(s *Service, p *store.Provider, c *llm.Client) {
	s.clientsMu.Lock()
	s.clients[fmt.Sprintf("%s@%d", p.ID, p.UpdatedAt)] = c
	s.clientsMu.Unlock()
}

func hasToolNamed(tools []llm.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func waitCompactionIdle(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.compactMu.Lock()
		n := len(s.compactRunning)
		s.compactMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out waiting for background compaction")
}

// TestColdResume900KConversationPreventsCostExplosion verifies that waking up a massive
// legacy conversation (simulating 900K+ historical tokens on a 1M context model) after >= 6h
// sends only a tiny cold-resume tail + provisional episode summary, keeps prefix stable on
// follow-up turns, and backfills the rolling summary in O(1) bounded tokens.
func TestColdResume900KConversationPreventsCostExplosion(t *testing.T) {
	s, u, c := contextFixture(t)
	defer s.Shutdown(context.Background())

	prov := &store.Provider{ID: "p-main", Name: "Main", BaseURL: "https://mock.local/v1", APIKey: "k", Enabled: true}
	if err := s.Store.UpsertProvider(t.Context(), prov); err != nil {
		t.Fatal(err)
	}
	// Admin configured a 1M context window flagship model!
	flagship := &store.Model{
		ID: "m-1m", ProviderID: prov.ID, Kind: store.KindChat,
		UpstreamModel: "gemini-4-carbon", DisplayName: "Carbon 1M",
		Tools: true, Vision: true, ContextWindow: 1_000_000, MaxOutput: 8192, Enabled: true,
	}
	cheap := &store.Model{
		ID: "m-cheap", ProviderID: prov.ID, Kind: store.KindChat,
		UpstreamModel: "deepseek-flash", DisplayName: "Flash",
		Tools: true, ContextWindow: 128_000, Enabled: true,
	}
	if err := s.Store.CreateModels(t.Context(), []*store.Model{flagship, cheap}); err != nil {
		t.Fatal(err)
	}
	_ = s.Store.SetSetting(t.Context(), "default_model_id", flagship.ID)
	_ = s.Store.SetSetting(t.Context(), "memory_model_id", cheap.ID)
	_ = s.Store.SetSetting(t.Context(), "title_model_id", cheap.ID)

	up := &fakeUpstream{}
	injectClient(s, prov, up.client(llm.KindCustom))

	// Seed an existing episode memory for this conversation (as v3 memory would have created).
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{{
		UserID:       u.ID,
		Category:     "episode",
		Content:      "2026-10-01 用户咨询父亲高血压早餐吃氨氯地平 5mg 的注意事项与低盐饮食方案。",
		Keywords:     "高血压 氨氯地平 5mg 父亲 低盐",
		SourceConvID: &c.ID,
	}}); err != nil {
		t.Fatal(err)
	}

	// Build ~900K tokens of old messages (300 messages * ~3000 CJK chars = 900,000 tokens) from 5 days ago.
	oldBase := time.Now().Add(-5 * 24 * time.Hour).UnixMilli()
	chunk := strings.Repeat("这是一段非常长的历史健康与家常菜讨论记录，包含大量详细解释。", 100) // ~3000 CJK runes
	var batch []*store.Message
	for i := 0; i < 150; i++ {
		ts := oldBase + int64(i*60_000)
		uText := fmt.Sprintf("第%d轮提问：%s", i+1, chunk)
		if i == 3 {
			uText = "第四轮关键原话：医生特意叮嘱父亲每天早上七点半空腹吃苯磺酸氨氯地平片 5mg，千万别配西柚汁。" + chunk
		}
		batch = append(batch,
			&store.Message{Role: "user", Content: uText, Status: store.StatusDone, CreatedAt: ts},
			&store.Message{Role: "assistant", Content: fmt.Sprintf("第%d轮回答：%s", i+1, chunk), Status: store.StatusDone, CreatedAt: ts + 5_000},
		)
	}
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{ConversationID: c.ID, Append: batch}); err != nil {
		t.Fatal(err)
	}

	// Now (5 days later, >= 6h cold gap!), user wakes up the 900K conversation with a casual message.
	res, err := s.Send(t.Context(), u, SendInput{
		ConversationID: c.ID,
		ModelID:        flagship.ID,
		Content:        "早上好，今天早餐喝无糖豆浆可以吗？",
	})
	if err != nil {
		t.Fatal(err)
	}
	run := s.Hub.Get(res.Assistant.ID)
	if run != nil {
		<-run.Done()
	}
	waitCompactionIdle(t, s)

	calls := up.snapshot()
	var chatCall *recordedCall
	var compactCall *recordedCall
	for i := range calls {
		if calls[i].Stream && calls[i].Model == "gemini-4-carbon" {
			chatCall = &calls[i]
		}
		if !calls[i].Stream && calls[i].Model == "deepseek-flash" {
			compactCall = &calls[i]
		}
	}
	if chatCall == nil {
		t.Fatalf("expected streaming chat call on gemini-4-carbon, calls=%d", len(calls))
	}
	// Verify prompt size: instead of ~900K tokens (~2.7MB UTF-8), cold resume sent < 12K tokens (< 45KB)!
	if chatCall.RawBytes > 50_000 {
		t.Fatalf("cold resume leaked huge history! payload bytes=%d (want < 50KB)", chatCall.RawBytes)
	}
	sysPrompt, _ := chatCall.Messages[0].Content.(string)
	if !strings.Contains(sysPrompt, "# 本次对话的前情提要") || !strings.Contains(sysPrompt, "氨氯地平 5mg") {
		t.Fatalf("expected provisional episode summary in system prompt, got:\n%s", sysPrompt)
	}
	hasHistoryTool := false
	for _, tl := range chatCall.Tools {
		if tl.Function.Name == readEarlierTool.Name {
			hasHistoryTool = true
		}
	}
	if !hasHistoryTool {
		t.Fatalf("expected %s tool enabled on cold-resumed conversation", readEarlierTool.Name)
	}

	// Verify background compaction ran on cheap model with strictly bounded O(1) input (< 110KB, NOT 2.7MB)
	// and persisted rolling summary + cursor.
	if compactCall == nil {
		t.Fatalf("expected background compaction on cheap model deepseek-flash")
	}
	if compactCall.RawBytes > 120_000 {
		t.Fatalf("compaction input exceeded bounded cap: bytes=%d", compactCall.RawBytes)
	}
	savedSum, savedCur, err := s.Store.GetConversationSummary(t.Context(), c.ID)
	if err != nil || savedCur == "" || savedSum == "" {
		t.Fatalf("expected persisted summary & cursor, got sum=%q cur=%q err=%v", savedSum, savedCur, err)
	}

	// Verify Prefix Cache stability across subsequent active turns after compaction!
	ts := s.toolsFor(t.Context(), u, c, flagship)
	id2 := contextHistory(t, s, c, &store.Message{Role: "user", Content: "那配一个水煮蛋呢？", Status: store.StatusDone})
	reqTurn2, err := s.buildRequestWithTools(t.Context(), u, c, id2, flagship, ts)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Store.FinishMessage(t.Context(), &store.Message{ID: id2, ConversationID: c.ID, Role: "assistant", Content: "水煮蛋很合适。", Status: store.StatusDone})

	id3 := contextHistory(t, s, c, &store.Message{Role: "user", Content: "好的谢谢！", Status: store.StatusDone})
	reqTurn3, err := s.buildRequestWithTools(t.Context(), u, c, id3, flagship, ts)
	if err != nil {
		t.Fatal(err)
	}
	// System prompt and the first historical message after summary_cursor MUST be identical
	// so upstream KV / Prefix Cache hits 100%!
	if reqTurn2.Messages[0].Content != reqTurn3.Messages[0].Content {
		t.Fatalf("system prompt changed between uncompacted turns, breaking prefix cache")
	}
	if reqTurn2.Messages[1].Content != reqTurn3.Messages[1].Content {
		t.Fatalf("leading history anchor shifted between turns (%q vs %q), breaking prefix cache!",
			reqTurn2.Messages[1].Content, reqTurn3.Messages[1].Content)
	}
}

// TestGiantSinglePasteWarmGraceAndDeepClauseLookup verifies:
// 1. Pasting a huge 55K-token contract keeps the full contract on Turn 1 AND Turn 2 (immediate follow-up).
// 2. By Turn 4, compaction folds the 55K contract into the rolling summary so subsequent turns are cheap.
// 3. Calling read_earlier_messages retrieves an exact clause buried in the middle of the 55K contract
//    and emits a "history" RunStep ("已翻看前面的聊天记录").
func TestGiantSinglePasteWarmGraceAndDeepClauseLookup(t *testing.T) {
	s, u, c := contextFixture(t)
	defer s.Shutdown(context.Background())

	prov := &store.Provider{ID: "p1", Name: "P1", BaseURL: "https://mock.local/v1", APIKey: "k", Enabled: true}
	_ = s.Store.UpsertProvider(t.Context(), prov)
	m := &store.Model{
		ID: "m-200k", ProviderID: prov.ID, Kind: store.KindChat,
		UpstreamModel: "claude-sonnet-4.6", DisplayName: "Sonnet",
		Tools: true, ContextWindow: 200_000, MaxOutput: 8192, Enabled: true,
	}
	_ = s.Store.CreateModels(t.Context(), []*store.Model{m})
	_ = s.Store.SetSetting(t.Context(), "default_model_id", m.ID)

	up := &fakeUpstream{}
	injectClient(s, prov, up.client(llm.KindCustom))

	now := time.Now().UnixMilli()
	prefix := strings.Repeat("甲方与乙方就房屋装修工程达成如下常规施工质量标准与验收流程约定。", 900) // ~28,800 runes
	hiddenClause := "【特别约定第十二条：若乙方逾期竣工超过十五日，违约金按合同总额的 18.5% 计算并一次性赔付】"
	suffix := strings.Repeat("附则部分包含材料环保等级清单、保修年限说明及双方签章页。", 900) // ~25,200 runes
	giantContract := prefix + hiddenClause + suffix                                     // ~54,000+ CJK tokens

	m1 := &store.Message{Role: "user", Content: giantContract, Status: store.StatusDone, CreatedAt: now}
	a1 := &store.Message{Role: "assistant", Content: "我已经看完了整份装修合同，整体条款完整。", Status: store.StatusDone, CreatedAt: now + 2_000}
	m2 := &store.Message{Role: "user", Content: "帮我看看整体有没有明显大坑？", Status: store.StatusDone, CreatedAt: now + 20_000}

	// Turn 2: immediate follow-up (userTurnsPicked == 2 <= minProtectedUserTurns) -> full 54K contract stays in prompt!
	ts := s.toolsFor(t.Context(), u, c, m)
	asst2ID := contextHistory(t, s, c, m1, a1, m2)
	req2, err := s.buildRequestWithTools(t.Context(), u, c, asst2ID, m, ts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req2.Messages[1].Content, hiddenClause) {
		t.Fatalf("expected immediate follow-up Turn 2 to retain full pasted contract in warm cache")
	}
	_ = s.Store.FinishMessage(t.Context(), &store.Message{ID: asst2ID, ConversationID: c.ID, Role: "assistant", Content: "主要留意工期与增项上限。", Status: store.StatusDone, CreatedAt: now + 25_000})

	// Turn 3 finishes -> background compaction now folds (m1, a1) into summary!
	m3 := &store.Message{Role: "user", Content: "好的，那油漆选什么牌子环保？", Status: store.StatusDone, CreatedAt: now + 60_000}
	a3 := &store.Message{Role: "assistant", Content: "选国标十环认证的乳胶漆即可。", Status: store.StatusDone, CreatedAt: now + 65_000}
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{ConversationID: c.ID, Append: []*store.Message{m3, a3}}); err != nil {
		t.Fatal(err)
	}
	if err := s.compactConversation(t.Context(), compactHint{
		conv: c, user: u, turnModel: m, turnProvider: prov, lowWaterTokens: 18_000, coldTailTokens: 8_000,
	}); err != nil {
		t.Fatal(err)
	}
	_, cur, _ := s.Store.GetConversationSummary(t.Context(), c.ID)
	if cur != a1.ID {
		t.Fatalf("expected giant Turn 1 compacted up to a1 (%s), got cursor=%s", a1.ID, cur)
	}

	// Turn 4: user asks for the exact penalty percentage buried at char ~29,000 of Turn 1!
	// Upstream model calls read_earlier_messages -> receives "18.5%" -> answers accurately!
	round := 0
	up.mu.Lock()
	up.handler = func(call recordedCall) (string, string) {
		if !call.Stream {
			return "", "摘要"
		}
		round++
		if round == 1 {
			return "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_hist_1\",\"function\":{\"name\":\"read_earlier_messages\",\"arguments\":\"{\\\"queries\\\":[\\\"违约金 逾期 百分比\\\"]}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", ""
		}
		// Verify the tool output handed back to round 2 actually contains the hidden clause!
		lastToolMsg, _ := call.Messages[len(call.Messages)-1].Content.(string)
		if !strings.Contains(lastToolMsg, "18.5%") {
			return "data: {\"choices\":[{\"delta\":{\"content\":\"ERROR: missing 18.5% in tool result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", ""
		}
		return "data: {\"choices\":[{\"delta\":{\"content\":\"合同第十二条写明：逾期超过15日，违约金按合同总额的 18.5% 计算。\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", ""
	}
	up.mu.Unlock()

	sendRes, err := s.Send(t.Context(), u, SendInput{
		ConversationID: c.ID,
		ModelID:        m.ID,
		Content:        "刚才合同里写逾期违约金具体是按百分之多少算的？",
	})
	if err != nil {
		t.Fatal(err)
	}
	run := s.Hub.Get(sendRes.Assistant.ID)
	var snap Snapshot
	if run != nil {
		<-run.Done()
		snap = run.Snapshot()
	}
	finalMsg, err := s.Store.GetMessage(t.Context(), sendRes.Assistant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(finalMsg.Content, "18.5%") {
		t.Fatalf("expected final answer with 18.5%%, got %q", finalMsg.Content)
	}
	foundHistoryStep := false
	for _, st := range snap.Steps {
		if st.Kind == "history" && st.Status == "done" && st.Title == "已翻看前面的聊天记录" {
			foundHistoryStep = true
		}
	}
	if !foundHistoryStep {
		t.Fatalf("expected done RunStep{Kind:'history', Title:'已翻看前面的聊天记录'}, got steps=%+v", snap.Steps)
	}
}

// TestPrivateChatSelfCompactionNeverLeavesSelectedProvider verifies that a long
// private conversation compacts ONLY with the user's chosen private chat model
// (never calling memory_model_id / title_model_id on another provider), supports
// read_earlier_messages locally, and wipes everything upon DeleteConversation.
func TestPrivateChatSelfCompactionNeverLeavesSelectedProvider(t *testing.T) {
	s, u, _ := contextFixture(t)
	defer s.Shutdown(context.Background())

	privProv := &store.Provider{ID: "prov-private", Name: "Trusted", BaseURL: "https://trusted.local/v1", APIKey: "k1", Enabled: true}
	otherProv := &store.Provider{ID: "prov-cheap", Name: "ThirdPartyCheap", BaseURL: "https://cheap.local/v1", APIKey: "k2", Enabled: true}
	_ = s.Store.UpsertProvider(t.Context(), privProv)
	_ = s.Store.UpsertProvider(t.Context(), otherProv)

	privModel := &store.Model{
		ID: "m-priv", ProviderID: privProv.ID, Kind: store.KindChat,
		UpstreamModel: "private-sonnet", DisplayName: "PrivateModel",
		Tools: true, ContextWindow: 32_000, MaxOutput: 4096, Enabled: true,
	}
	bgModel := &store.Model{
		ID: "m-bg-cheap", ProviderID: otherProv.ID, Kind: store.KindChat,
		UpstreamModel: "cheap-summarizer", DisplayName: "CheapModel",
		Tools: true, ContextWindow: 128_000, Enabled: true,
	}
	_ = s.Store.CreateModels(t.Context(), []*store.Model{privModel, bgModel})
	_ = s.Store.SetSetting(t.Context(), "memory_model_id", bgModel.ID)
	_ = s.Store.SetSetting(t.Context(), "title_model_id", bgModel.ID)

	trustedUp := &fakeUpstream{
		handler: func(call recordedCall) (string, string) {
			return "", "【私密临时提要】给爱人挑结婚纪念日蓝宝石项链，预算 8000 元。"
		},
	}
	thirdPartyUp := &fakeUpstream{}
	injectClient(s, privProv, trustedUp.client(llm.KindCustom))
	injectClient(s, otherProv, thirdPartyUp.client(llm.KindCustom))

	privConv := &store.Conversation{UserID: u.ID, Private: true, ModelID: &privModel.ID}
	if err := s.Store.CreateConversation(t.Context(), privConv); err != nil {
		t.Fatal(err)
	}

	// Append enough private turns to cross 32K model's compactTrigger (~12K tokens).
	pad := strings.Repeat("挑礼物的详细对比记录与款式参数讨论。", 160) // ~2880 CJK tokens
	var turns []*store.Message
	for i := 0; i < 5; i++ {
		turns = append(turns,
			&store.Message{Role: "user", Content: fmt.Sprintf("私密问题%d（预算8000元买蓝宝石项链）：%s", i+1, pad), Status: store.StatusDone},
			&store.Message{Role: "assistant", Content: fmt.Sprintf("私密建议%d：%s", i+1, pad), Status: store.StatusDone},
		)
	}
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{ConversationID: privConv.ID, Append: turns}); err != nil {
		t.Fatal(err)
	}

	res, err := s.Send(t.Context(), u, SendInput{
		ConversationID: privConv.ID,
		ModelID:        privModel.ID,
		Private:        true,
		Content:        "那证书选 GIA 还是 GRS？",
	})
	if err != nil {
		t.Fatal(err)
	}
	if run := s.Hub.Get(res.Assistant.ID); run != nil {
		<-run.Done()
	}
	waitCompactionIdle(t, s)

	// CRITICAL PRIVACY CHECK: thirdPartyUp (memory_model_id / title_model_id) MUST have 0 calls!
	if leaked := len(thirdPartyUp.snapshot()); leaked != 0 {
		t.Fatalf("PRIVACY BREACH: private chat sent %d requests to external memory/title provider!", leaked)
	}
	// Self-compaction ran on trustedUp ("private-sonnet").
	sum, cur, err := s.Store.GetConversationSummary(t.Context(), privConv.ID)
	if err != nil || cur == "" || !strings.Contains(sum, "蓝宝石项链") {
		t.Fatalf("expected private self-summary on conversation row, got sum=%q cur=%q err=%v", sum, cur, err)
	}
	// Global memories table remains completely empty.
	if mems, _ := s.Store.ListMemories(t.Context(), u.ID); len(mems) != 0 {
		t.Fatalf("private chat must never write to memories table, got=%+v", mems)
	}

	// Closing private chat physically destroys the conversation and its temporary summary.
	if err := s.DeleteConversation(t.Context(), privConv.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Store.GetConversationSummary(t.Context(), privConv.ID); err == nil {
		t.Fatalf("deleted private conversation summary still exists!")
	}
}

func TestAutoEscalationCapsCasualHistoryForFlagshipModel(t *testing.T) {
	s, u, c := contextFixture(t)
	defer s.Shutdown(context.Background())

	now := time.Now().UnixMilli()
	pad := strings.Repeat("日常菜谱与家常闲聊内容记录。", 200) // ~2800 tokens per msg
	var msgs []*store.Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs,
			&store.Message{Role: "user", Content: fmt.Sprintf("闲聊%d：%s", i+1, pad), Status: store.StatusDone, CreatedAt: now + int64(i*10_000)},
			&store.Message{Role: "assistant", Content: fmt.Sprintf("回复%d：%s", i+1, pad), Status: store.StatusDone, CreatedAt: now + int64(i*10_000+2_000)},
		)
	}
	msgs = append(msgs, &store.Message{Role: "user", Content: "我刚测血压 160/100 头晕怎么办？", Status: store.StatusDone, CreatedAt: now + 90_000})
	asstID := contextHistory(t, s, c, msgs...)

	flagship := &store.Model{UpstreamModel: "claude-opus-5.5", Tools: true, ContextWindow: 200_000, MaxOutput: 8192}
	ts := toolSet{allowTools: true}

	normalReq, err := s.buildRequestWithOpts(t.Context(), u, c, asstID, flagship, ts, buildOpts{autoEscalated: false})
	if err != nil {
		t.Fatal(err)
	}
	escalatedReq, err := s.buildRequestWithOpts(t.Context(), u, c, asstID, flagship, ts, buildOpts{autoEscalated: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(escalatedReq.Messages) >= len(normalReq.Messages) {
		t.Fatalf("expected autoEscalated request to shed old casual turns: escalated=%d normal=%d",
			len(escalatedReq.Messages), len(normalReq.Messages))
	}
	if !hasToolNamed(escalatedReq.tools, readEarlierTool.Name) {
		t.Fatalf("expected read_earlier_messages enabled when autoEscalated folds casual history")
	}
}
