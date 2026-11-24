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

func TestComponentEntityVersioningAndTenVersionRetention(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	conv := &store.Conversation{UserID: f.user.ID, Title: "天气查询"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	origData := json.RawMessage(`{"location":"北京","date_label":"11/1-11/7","condition":"晴","temp":15}`)
	ent := &store.ComponentEntity{
		ID:                   store.NewID(),
		UserID:               f.user.ID,
		Kind:                 "weather",
		Title:                "北京 · 11/1-11/7",
		CurrentVersion:       1,
		Data:                 origData,
		OriginConversationID: conv.ID,
		OriginComponentID:    "comp_w1",
	}
	aMsg := &store.Message{
		Role:    "assistant",
		Content: "11月1日到7日天气如下：",
		Components: []store.MessageComponent{{
			ID:       "comp_w1",
			EntityID: ent.ID,
			Kind:     "weather",
			Version:  1,
			Data:     origData,
		}},
		Status: store.StatusDone,
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{aMsg}}); err != nil {
		t.Fatal(err)
	}
	ent.OriginMessageID = aMsg.ID
	ent.LastSourceMessageID = aMsg.ID
	if err := f.st.CreateComponentEntity(ctx, ent, "创建初始天气卡"); err != nil {
		t.Fatal(err)
	}

	// Pin to home board.
	var pin store.PinnedComponent
	f.json("POST", "/api/pinned-components", map[string]any{
		"message_id":   aMsg.ID,
		"component_id": "comp_w1",
	}, 200, &pin)
	if pin.EntityID != ent.ID {
		t.Fatalf("expected pin entity_id=%s, got %q", ent.ID, pin.EntityID)
	}

	// Update 12 times via entity endpoint -> should reach v13 and retain only latest 10 versions (v4..v13).
	for i := 2; i <= 13; i++ {
		f.json("PATCH", "/api/components/"+ent.ID, map[string]any{
			"data": map[string]any{
				"location":   "北京",
				"date_label": "第" + string(rune('A'+i)) + "周",
				"condition":  "多云",
				"temp":       10 + i,
			},
			"change_summary": "更新天气版本",
		}, 200, nil)
	}

	// State-only update should NOT increment version.
	var afterState store.ComponentEntity
	f.json("PATCH", "/api/components/"+ent.ID, map[string]any{
		"state": map[string]any{"unit": "C"},
	}, 200, &afterState)
	if afterState.CurrentVersion != 13 {
		t.Fatalf("expected version to stay 13 after state-only update, got %d", afterState.CurrentVersion)
	}

	// GET /api/components/{entityId}/versions returns exactly 10 versions (v13 down to v4).
	var verOut struct {
		Entity   store.ComponentEntity    `json:"entity"`
		Versions []store.ComponentVersion `json:"versions"`
	}
	f.json("GET", "/api/components/"+ent.ID+"/versions", nil, 200, &verOut)
	if len(verOut.Versions) != store.MaxComponentVersions {
		t.Fatalf("expected %d versions retained, got %d", store.MaxComponentVersions, len(verOut.Versions))
	}
	if verOut.Versions[0].Version != 13 || verOut.Versions[len(verOut.Versions)-1].Version != 4 {
		t.Fatalf("expected versions v13..v4, got v%d..v%d", verOut.Versions[0].Version, verOut.Versions[len(verOut.Versions)-1].Version)
	}

	// Original message still preserves v1 snapshot while hydrating latest_version=13.
	origMsg, err := f.st.GetMessageForUser(ctx, aMsg.ID, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(origMsg.Components) != 1 || origMsg.Components[0].Version != 1 || origMsg.Components[0].LatestVersion != 13 {
		t.Fatalf("expected original message anchored at v1 with latest_version=13, got %+v", origMsg.Components)
	}
	if !json.Valid(origMsg.Components[0].Data) || string(origMsg.Components[0].Data) != string(origData) {
		t.Fatalf("expected original message v1 snapshot preserved, got %s", string(origMsg.Components[0].Data))
	}

	// Home pinned card reflects live latest version v13.
	var listOut struct {
		Items []store.PinnedComponent `json:"items"`
	}
	f.json("GET", "/api/pinned-components", nil, 200, &listOut)
	if len(listOut.Items) != 1 || listOut.Items[0].Component.Version != 13 {
		t.Fatalf("expected pinned card at live v13, got %+v", listOut.Items)
	}
}
