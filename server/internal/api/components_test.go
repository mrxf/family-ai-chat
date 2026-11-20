package api

import (
	"encoding/json"
	"testing"

	"nuan/internal/store"
)

func TestUpdateMessageComponentEndpoint(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	conv := &store.Conversation{UserID: f.user.ID, Title: "组件测试"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	uMsg := &store.Message{Role: "user", Content: "列个采购清单", Status: store.StatusDone}
	aMsg := &store.Message{
		Role:    "assistant",
		Content: "好呀：",
		Components: []store.MessageComponent{{
			ID:    "comp_todo_1",
			Kind:  "todo",
			Pos:   3,
			Data:  json.RawMessage(`{"title":"超市采购","items":[{"id":"i1","text":"牛奶"},{"id":"i2","text":"鸡蛋"}]}`),
			State: json.RawMessage(`{"done_ids":[]}`),
		}},
		Status: store.StatusDone,
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{uMsg, aMsg}}); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Component store.MessageComponent `json:"component"`
		Message   store.Message          `json:"message"`
	}
	f.json("PATCH", "/api/messages/"+aMsg.ID+"/components/comp_todo_1", map[string]any{
		"state": map[string]any{"done_ids": []string{"i1", "i2"}},
	}, 200, &out)
	if out.Component.ID != "comp_todo_1" || len(out.Message.Components) != 1 {
		t.Fatalf("unexpected response: %+v", out)
	}
	var st struct {
		DoneIDs []string `json:"done_ids"`
	}
	if err := json.Unmarshal(out.Component.State, &st); err != nil || len(st.DoneIDs) != 2 {
		t.Fatalf("expected 2 done_ids, got %+v err=%v", st, err)
	}

	// Non-existent component returns 404.
	f.json("PATCH", "/api/messages/"+aMsg.ID+"/components/missing", map[string]any{
		"state": map[string]any{"done_ids": []string{}},
	}, 404, nil)
}
