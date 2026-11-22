package api

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/store"
)

func TestHomeSuggestionsStaleWhileRevalidateAndPrivacy(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	var calls atomic.Int32
	var lastPrompt atomic.Value
	f.up.mu.Lock()
	f.up.suggestionsFunc = func(req upstreamRequest) string {
		calls.Add(1)
		if len(req.Messages) > 1 {
			var s string
			_ = json.Unmarshal(req.Messages[1].Content, &s)
			lastPrompt.Store(s)
		}
		return "```json\n" + `[
			{"emoji":"🏃","text":"新手跑完5公里膝盖微酸，拉伸应该怎么做？"},
			{"emoji":"🥗","text":"减脂期晚上饿了有哪些低卡又顶饱的加餐？"},
			{"emoji":"☕","text":"手冲咖啡粉水比和研磨度怎么搭配更顺口？"},
			{"emoji":"📚","text":"推荐几本周末能一口气读完的轻松推理小说"},
			{"emoji":"🧘","text":"久坐肩颈僵硬，睡前5分钟怎么放松最有效？"}
		]` + "\n```"
	}
	f.up.mu.Unlock()

	type sugResp struct {
		Items       []store.HomeSuggestion `json:"items"`
		GeneratedAt int64                  `json:"generated_at"`
		Refreshing  bool                   `json:"refreshing"`
	}

	// 1. Cold start with no conversations/memories: empty list, no LLM call.
	var res sugResp
	f.json("GET", "/api/suggestions", nil, 200, &res)
	if len(res.Items) != 0 || res.Refreshing {
		t.Fatalf("cold start expected empty & not refreshing, got %+v", res)
	}

	// 2. Private conversation alone must NOT trigger generation or leak into prompts.
	privConv := &store.Conversation{UserID: f.user.ID, Title: "隐私秘密话题", Private: true}
	if err := f.st.CreateConversation(ctx, privConv); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{
		ConversationID: privConv.ID,
		Append: []*store.Message{
			{Role: "user", Content: "这是绝对不能泄露的隐私内容1", Status: store.StatusDone},
			{Role: "user", Content: "这是绝对不能泄露的隐私内容2", Status: store.StatusDone},
		},
	}); err != nil {
		t.Fatal(err)
	}
	f.json("GET", "/api/suggestions", nil, 200, &res)
	if res.Refreshing || calls.Load() != 0 {
		t.Fatalf("private activity must not trigger suggestions, refreshing=%v calls=%d", res.Refreshing, calls.Load())
	}

	// 3. Add non-private conversation messages and a memory -> triggers async generation.
	conv := &store.Conversation{UserID: f.user.ID, Title: "跑步与饮食"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{
		ConversationID: conv.ID,
		Append: []*store.Message{
			{Role: "user", Content: "最近刚开始晨跑 5 公里", Status: store.StatusDone},
			{Role: "assistant", Content: "太棒了！", Status: store.StatusDone},
			{Role: "user", Content: "跑完步早餐怎么搭配蛋白质？", Status: store.StatusDone},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.CreateMemories(ctx, []*store.Memory{{
		UserID:   f.user.ID,
		Category: "preference",
		Content:  "喜欢喝手冲咖啡，周末常看推理小说",
	}}); err != nil {
		t.Fatal(err)
	}

	f.json("GET", "/api/suggestions", nil, 200, &res)
	if !res.Refreshing {
		t.Fatalf("expected async refresh to start, got %+v", res)
	}

	eventually(t, "suggestions persisted", func() bool {
		var r sugResp
		f.json("GET", "/api/suggestions", nil, 200, &r)
		res = r
		return len(r.Items) == 5 && r.GeneratedAt > 0
	})

	// Verify prompt included recent non-private conversation + memory, and excluded private chat.
	prompt, _ := lastPrompt.Load().(string)
	if !strings.Contains(prompt, "跑步与饮食") || !strings.Contains(prompt, "手冲咖啡") {
		t.Fatalf("expected prompt to include recent digest & memory, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "隐私秘密话题") || strings.Contains(prompt, "绝对不能泄露") {
		t.Fatalf("private conversation leaked into suggestions prompt:\n%s", prompt)
	}

	// 4. Within 6h cooldown, calling again does not trigger another LLM call.
	beforeCalls := calls.Load()
	f.json("GET", "/api/suggestions", nil, 200, &res)
	if res.Refreshing || calls.Load() != beforeCalls {
		t.Fatalf("within cooldown should not refresh: refreshing=%v calls=%d->%d", res.Refreshing, beforeCalls, calls.Load())
	}

	// 5. After 6h cooldown with NO new activity, still does not trigger.
	oldTime := time.Now().Add(-7 * time.Hour).UnixMilli()
	cached, err := f.st.GetUserSuggestions(ctx, f.user.ID)
	if err != nil || cached == nil {
		t.Fatalf("expected cached suggestions: %v", err)
	}
	cached.GeneratedAt = oldTime
	if err := f.st.SaveUserSuggestions(ctx, cached); err != nil {
		t.Fatal(err)
	}
	f.json("GET", "/api/suggestions", nil, 200, &res)
	if res.Refreshing || calls.Load() != beforeCalls {
		t.Fatalf("past cooldown without new activity should not refresh: refreshing=%v calls=%d->%d", res.Refreshing, beforeCalls, calls.Load())
	}

	// 6. Past 6h cooldown WITH 2 new non-private conversations -> triggers refresh.
	for _, title := range []string{"京都自由行", "阳台种番茄"} {
		c := &store.Conversation{UserID: f.user.ID, Title: title}
		if err := f.st.CreateConversation(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := f.st.ApplyTurn(ctx, store.Turn{
			ConversationID: c.ID,
			Append:         []*store.Message{{Role: "user", Content: "聊聊" + title, Status: store.StatusDone}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.json("GET", "/api/suggestions", nil, 200, &res)
	if !res.Refreshing {
		t.Fatalf("expected refresh after cooldown + new conversations")
	}
	eventually(t, "second suggestions generation", func() bool {
		return calls.Load() > beforeCalls
	})
}
