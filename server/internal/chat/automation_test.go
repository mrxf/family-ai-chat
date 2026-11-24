package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"nuan/internal/store"
)

func TestFeedRollingUpdateAndScheduledTaskExecution(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()

	// Seed an initial feed card with 3 LLM news items and pin it to Home.
	uMsg := &store.Message{Role: "user", Content: "先帮我整理今天的大模型新闻", Status: store.StatusDone}
	aMsg := &store.Message{Role: "assistant", Content: "今日大模型新闻：", Status: store.StatusDone}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{uMsg, aMsg}}); err != nil {
		t.Fatal(err)
	}
	run := &Run{MessageID: aMsg.ID, ConversationID: c.ID, UserID: u.ID, subs: map[chan struct{}]struct{}{}}
	turn := &toolTurn{
		user:         u,
		convID:       c.ID,
		asstID:       aMsg.ID,
		run:          run,
		activeSkills: map[string]bool{skillKeyComponents: true, skillKeyAutomation: true},
	}
	out := s.execShowFeed(turn, `{"title":"每日大模型新闻","subtitle":"每天 07:00 更新","items":[{"title":"旧闻1：模型A发布","tag":"今日"},{"title":"旧闻2：模型B降价","tag":"今日"},{"title":"旧闻3：开源框架更新","tag":"今日"}]}`)
	if !strings.Contains(out, "每日大模型新闻") {
		t.Fatalf("unexpected show_feed output: %s", out)
	}
	compID := turn.lastCreatedCompID
	if compID == "" {
		t.Fatal("expected lastCreatedCompID to be recorded")
	}
	pinOut := s.execPinComponent(ctx, turn, `{}`)
	if !strings.Contains(pinOut, "固定到首页") {
		t.Fatalf("unexpected pin output: %s", pinOut)
	}

	// Verify list_components returns the pinned feed card with its IDs and items.
	listOut := s.execListComponents(ctx, turn, `{"scope":"pinned"}`)
	if !strings.Contains(listOut, aMsg.ID) || !strings.Contains(listOut, compID) || !strings.Contains(listOut, "旧闻1：模型A发布") {
		t.Fatalf("list_components missing expected card details:\n%s", listOut)
	}

	// Create a daily 07:00 scheduled task bound to this feed card.
	createArgs, _ := json.Marshal(map[string]any{
		"title":               "早7点大模型新闻更新",
		"prompt":              "查询最新3条大模型新闻，更新前三条，把原来的前三条放到后面三条",
		"schedule_kind":       "daily",
		"time_of_day":         "07:00",
		"target_message_id":   aMsg.ID,
		"target_component_id": compID,
	})
	createOut := s.execCreateScheduledTask(ctx, turn, string(createArgs))
	if !strings.Contains(createOut, "创建成功") {
		t.Fatalf("unexpected create_scheduled_task output: %s", createOut)
	}

	tasks, err := s.Store.ListScheduledTasks(ctx, u.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected 1 scheduled task, got %+v err=%v", tasks, err)
	}
	task := tasks[0]

	// Mock LLM executing the scheduled task: calls update_component with prepend_items + previous_tag + max_items=6.
	var step atomic.Int32
	var receivedSystemPrompt string
	up := &fakeUpstream{
		handler: func(call recordedCall) (string, string) {
			n := step.Add(1)
			if n == 1 {
				if len(call.Messages) > 0 {
					receivedSystemPrompt, _ = call.Messages[0].Content.(string)
				}
				updArgs, _ := json.Marshal(map[string]any{
					"message_id":   aMsg.ID,
					"component_id": compID,
					"previous_tag": "往期",
					"max_items":    6,
					"prepend_items": []map[string]any{
						{"title": "新闻1：新旗舰模型发布", "summary": "推理速度翻倍", "tag": "今日", "source": "机器之心"},
						{"title": "新闻2：多模态基准刷新", "tag": "今日"},
						{"title": "新闻3：端侧小模型开源", "tag": "今日"},
					},
				})
				chunk, _ := json.Marshal(map[string]any{
					"choices": []any{map[string]any{
						"delta": map[string]any{
							"tool_calls": []any{map[string]any{
								"index": 0, "id": "call_upd", "type": "function",
								"function": map[string]any{"name": "update_component", "arguments": string(updArgs)},
							}},
						},
					}},
				})
				return "data: " + string(chunk) + "\n\ndata: [DONE]\n\n", ""
			}
			return `data: {"choices":[{"delta":{"content":"已为您更新今日最新 3 条大模型新闻，原有 3 条已顺延至第 4-6 条。"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n", ""
		},
	}

	prov := &store.Provider{Name: "mock", Kind: "openai", BaseURL: "https://mock.local/v1", APIKey: "k", Enabled: true}
	_ = s.Store.UpsertProvider(ctx, prov)
	s.clients[fmt.Sprintf("%s@%d", prov.ID, prov.UpdatedAt)] = up.client("openai")
	model := &store.Model{ProviderID: prov.ID, DisplayName: "TestModel", UpstreamModel: "gpt-test", Kind: store.KindChat, Enabled: true, Tools: true, ContextWindow: 128000, MaxOutput: 2048}
	_ = s.Store.UpsertModel(ctx, model)
	_ = s.Store.SetSetting(ctx, "default_model_id", model.ID)

	runRec := s.ExecuteScheduledTask(ctx, task, "schedule")
	if runRec.Status != store.TaskRunStatusSuccess {
		t.Fatalf("expected task run success, got status=%s err=%s", runRec.Status, runRec.Error)
	}
	if runRec.UpdatedComponents != 1 {
		t.Fatalf("expected UpdatedComponents=1, got %d", runRec.UpdatedComponents)
	}
	if !strings.Contains(receivedSystemPrompt, "旧闻1：模型A发布") {
		t.Fatalf("expected bound target component snapshot in system prompt, got:\n%s", receivedSystemPrompt)
	}

	// Verify pinned component on Home now has 6 items: 3 new followed by 3 previous (tagged 往期).
	pins, err := s.Store.ListPinnedComponents(ctx, u.ID)
	if err != nil || len(pins) != 1 {
		t.Fatalf("expected 1 pinned card, got %+v err=%v", pins, err)
	}
	var updatedFeed feedCardData
	if err := json.Unmarshal(pins[0].Component.Data, &updatedFeed); err != nil {
		t.Fatal(err)
	}
	if len(updatedFeed.Items) != 6 {
		t.Fatalf("expected 6 feed items, got %d: %+v", len(updatedFeed.Items), updatedFeed.Items)
	}
	wantTitles := []string{
		"新闻1：新旗舰模型发布",
		"新闻2：多模态基准刷新",
		"新闻3：端侧小模型开源",
		"旧闻1：模型A发布",
		"旧闻2：模型B降价",
		"旧闻3：开源框架更新",
	}
	for i, want := range wantTitles {
		if updatedFeed.Items[i].Title != want {
			t.Fatalf("item[%d] title=%q, want %q", i, updatedFeed.Items[i].Title, want)
		}
	}
	if updatedFeed.Items[3].Tag != "往期" || updatedFeed.Items[0].Tag != "今日" {
		t.Fatalf("unexpected tags after rolling update: item0=%q item3=%q", updatedFeed.Items[0].Tag, updatedFeed.Items[3].Tag)
	}
}
