package api

import (
	"encoding/json"
	"strings"
	"testing"

	"nuan/internal/store"
)

func TestScheduledTasksAndComponentDataAPI(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	conv := &store.Conversation{UserID: f.user.ID, Title: "大模型早报"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	aMsg := &store.Message{
		Role:    "assistant",
		Content: "今日资讯：",
		Components: []store.MessageComponent{
			{
				ID:   "comp_feed_1",
				Kind: "feed",
				Pos:  5,
				Data: json.RawMessage(`{"title":"大模型新闻","items":[{"id":"f1","title":"新闻A"}]}`),
			},
			{
				ID:   "comp_todo_1",
				Kind: "todo",
				Pos:  5,
				Data: json.RawMessage(`{"title":"待办","items":[{"id":"i1","text":"看报告"}]}`),
			},
		},
		Status: store.StatusDone,
	}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{aMsg}}); err != nil {
		t.Fatal(err)
	}

	// Update feed data via PATCH /api/messages/{id}/components/{compId}
	var patchRes struct {
		Component store.MessageComponent `json:"component"`
	}
	f.json("PATCH", "/api/messages/"+aMsg.ID+"/components/comp_feed_1", map[string]any{
		"data": map[string]any{
			"title": "大模型每日速递",
			"items": []map[string]any{
				{"title": "新条目1", "tag": "今日"},
				{"title": "新闻A", "tag": "往期"},
			},
		},
	}, 200, &patchRes)
	if !strings.Contains(string(patchRes.Component.Data), "大模型每日速递") || !strings.Contains(string(patchRes.Component.Data), "新条目1") {
		t.Fatalf("unexpected patched component data: %s", string(patchRes.Component.Data))
	}

	// Pin both and test reorder endpoint
	var pin1, pin2 store.PinnedComponent
	f.json("POST", "/api/pinned-components", map[string]any{"message_id": aMsg.ID, "component_id": "comp_feed_1"}, 200, &pin1)
	f.json("POST", "/api/pinned-components", map[string]any{"message_id": aMsg.ID, "component_id": "comp_todo_1"}, 200, &pin2)
	var reordered struct {
		Items []store.PinnedComponent `json:"items"`
	}
	f.json("PUT", "/api/pinned-components/order", map[string]any{"ids": []string{pin2.ID, pin1.ID}}, 200, &reordered)
	if len(reordered.Items) != 2 || reordered.Items[0].ID != pin2.ID {
		t.Fatalf("unexpected reordered pins: %+v", reordered.Items)
	}

	// Create scheduled task bound to comp_feed_1
	var created store.ScheduledTask
	f.json("POST", "/api/scheduled-tasks", map[string]any{
		"title":               "每天早7点大模型新闻",
		"prompt":              "查询最新大模型新闻，更新前三条，原前三条后移",
		"schedule_kind":       "daily",
		"time_of_day":         "07:00",
		"target_message_id":   aMsg.ID,
		"target_component_id": "comp_feed_1",
	}, 201, &created)
	if created.ID == "" || !created.Enabled || created.TargetComponentID != "comp_feed_1" {
		t.Fatalf("unexpected created task: %+v", created)
	}

	// List scheduled tasks
	var listRes struct {
		Items       []store.ScheduledTask `json:"items"`
		ActiveCount int                   `json:"active_count"`
	}
	f.json("GET", "/api/scheduled-tasks", nil, 200, &listRes)
	if len(listRes.Items) != 1 || listRes.ActiveCount != 1 {
		t.Fatalf("unexpected list response: %+v", listRes)
	}

	// Pause task
	var paused store.ScheduledTask
	f.json("PATCH", "/api/scheduled-tasks/"+created.ID, map[string]any{"enabled": false}, 200, &paused)
	if paused.Enabled || paused.NextRunAt != 0 {
		t.Fatalf("expected paused task with next_run_at=0, got %+v", paused)
	}

	// List runs (initially empty)
	var runsRes struct {
		Items []store.ScheduledTaskRun `json:"items"`
	}
	f.json("GET", "/api/scheduled-tasks/"+created.ID+"/runs", nil, 200, &runsRes)
	if len(runsRes.Items) != 0 {
		t.Fatalf("expected 0 runs, got %+v", runsRes.Items)
	}

	// Delete task & delete component
	f.json("DELETE", "/api/scheduled-tasks/"+created.ID, nil, 204, nil)
	f.json("DELETE", "/api/messages/"+aMsg.ID+"/components/comp_todo_1", nil, 204, nil)
	f.json("GET", "/api/pinned-components", nil, 200, &reordered)
	if len(reordered.Items) != 1 || reordered.Items[0].ComponentID != "comp_feed_1" {
		t.Fatalf("expected comp_todo_1 removed from pins, got %+v", reordered.Items)
	}
}
