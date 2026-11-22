package api

import (
	"encoding/json"
	"strings"
	"testing"

	"nuan/internal/store"
)

func TestPinnedComponentsLifecycleAndSharedState(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	conv := &store.Conversation{UserID: f.user.ID, Title: "周末采购"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	uMsg := &store.Message{Role: "user", Content: "列个采购清单", Status: store.StatusDone}
	aMsg := &store.Message{
		Role:    "assistant",
		Content: "清单如下：",
		Components: []store.MessageComponent{
			{
				ID:    "comp_todo_1",
				Kind:  "todo",
				Pos:   5,
				Data:  json.RawMessage(`{"title":"超市采购","items":[{"id":"i1","text":"牛奶"},{"id":"i2","text":"鸡蛋"}]}`),
				State: json.RawMessage(`{"done_ids":[]}`),
			},
			{
				ID:   "comp_ask_1",
				Kind: "ask_user",
				Pos:  5,
				Data: json.RawMessage(`{"questions":[{"id":"q1","question":"要哪种？","options":["全脂","脱脂"]}]}`),
			},
		},
		Status: store.StatusDone,
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{uMsg, aMsg}}); err != nil {
		t.Fatal(err)
	}

	// Pinning ask_user is rejected.
	f.json("POST", "/api/pinned-components", map[string]any{
		"message_id":   aMsg.ID,
		"component_id": "comp_ask_1",
	}, 400, nil)

	// Pin todo component.
	var pin store.PinnedComponent
	f.json("POST", "/api/pinned-components", map[string]any{
		"message_id":   aMsg.ID,
		"component_id": "comp_todo_1",
	}, 200, &pin)
	if pin.ConversationID != conv.ID || pin.ConversationTitle != "周末采购" || pin.Component.Kind != "todo" {
		t.Fatalf("unexpected pin: %+v", pin)
	}

	// Idempotent re-pin returns the same pin id.
	var pinAgain store.PinnedComponent
	f.json("POST", "/api/pinned-components", map[string]any{
		"message_id":   aMsg.ID,
		"component_id": "comp_todo_1",
	}, 200, &pinAgain)
	if pinAgain.ID != pin.ID {
		t.Fatalf("expected idempotent pin id %s, got %s", pin.ID, pinAgain.ID)
	}

	// Updating component state via message endpoint immediately reflects in GET /api/pinned-components.
	f.json("PATCH", "/api/messages/"+aMsg.ID+"/components/comp_todo_1", map[string]any{
		"state": map[string]any{"done_ids": []string{"i1"}},
	}, 200, nil)

	var listOut struct {
		Items []store.PinnedComponent `json:"items"`
	}
	f.json("GET", "/api/pinned-components", nil, 200, &listOut)
	if len(listOut.Items) != 1 {
		t.Fatalf("expected 1 pinned item, got %d", len(listOut.Items))
	}
	if !strings.Contains(string(listOut.Items[0].Component.State), `"i1"`) {
		t.Fatalf("expected shared state to include i1, got %s", string(listOut.Items[0].Component.State))
	}

	// Unpin removes it.
	f.json("DELETE", "/api/pinned-components/"+aMsg.ID+"/comp_todo_1", nil, 200, nil)
	f.json("GET", "/api/pinned-components", nil, 200, &listOut)
	if len(listOut.Items) != 0 {
		t.Fatalf("expected 0 pinned items after unpin, got %d", len(listOut.Items))
	}

	// Re-pin and delete conversation -> cascade deletes pinned component.
	f.json("POST", "/api/pinned-components", map[string]any{
		"message_id":   aMsg.ID,
		"component_id": "comp_todo_1",
	}, 200, nil)
	f.json("DELETE", "/api/conversations/"+conv.ID, nil, 200, nil)
	f.json("GET", "/api/pinned-components", nil, 200, &listOut)
	if len(listOut.Items) != 0 {
		t.Fatalf("expected cascade delete to clear pinned items, got %d", len(listOut.Items))
	}
}

func TestPinnedComponentsRejectsPrivateConversation(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	conv := &store.Conversation{UserID: f.user.ID, Title: "隐私", Private: true}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	aMsg := &store.Message{
		Role:    "assistant",
		Content: "天气：",
		Components: []store.MessageComponent{{
			ID:   "comp_w_1",
			Kind: "weather",
			Data: json.RawMessage(`{"location":"北京","condition":"晴","temp":22}`),
		}},
		Status: store.StatusDone,
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{aMsg}}); err != nil {
		t.Fatal(err)
	}
	f.json("POST", "/api/pinned-components", map[string]any{
		"message_id":   aMsg.ID,
		"component_id": "comp_w_1",
	}, 400, nil)
}

func TestChatWithQuotedComponent(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	srcConv := &store.Conversation{UserID: f.user.ID, Title: "超市采购"}
	if err := f.st.CreateConversation(ctx, srcConv); err != nil {
		t.Fatal(err)
	}
	srcMsg := &store.Message{
		Role:    "assistant",
		Content: "清单：",
		Components: []store.MessageComponent{{
			ID:    "c_todo",
			Kind:  "todo",
			Data:  json.RawMessage(`{"title":"采购清单","items":[{"id":"i1","text":"牛奶"},{"id":"i2","text":"鸡蛋"}]}`),
			State: json.RawMessage(`{"done_ids":["i1"]}`),
		}},
		Status: store.StatusDone,
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: srcConv.ID, Append: []*store.Message{srcMsg}}); err != nil {
		t.Fatal(err)
	}

	res := f.send(map[string]any{
		"content": "还差什么没买？",
		"quoted_component": map[string]any{
			"message_id":   srcMsg.ID,
			"component_id": "c_todo",
		},
	})
	f.waitRun(res.Assistant.ID)

	if res.UserMessage.QuotedComponent == nil {
		t.Fatalf("expected quoted_component snapshot on user message")
	}
	if res.UserMessage.QuotedComponent.Title != "采购清单" || res.UserMessage.QuotedComponent.Summary != "已完成 1/2 项" {
		t.Fatalf("unexpected quoted_component: %+v", res.UserMessage.QuotedComponent)
	}

	f.up.mu.Lock()
	var reqs []upstreamRequest
	for _, list := range f.up.requests {
		reqs = append(reqs, list...)
	}
	f.up.mu.Unlock()
	found := false
	for _, r := range reqs {
		for _, m := range r.Messages {
			if m.Role != "user" {
				continue
			}
			var s string
			if json.Unmarshal(m.Content, &s) == nil && strings.Contains(s, "已完成（1 项）：牛奶") && strings.Contains(s, "未完成（1 项）：鸡蛋") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected structured quote context in LLM prompt, requests=%+v", reqs)
	}
}
