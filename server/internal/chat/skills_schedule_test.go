package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

func TestSkillScopeCatalogLeaseAndBlindCallGuard(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()
	m := &store.Model{ID: "m1", UpstreamModel: "gpt-test", Enabled: true, Tools: true, ContextWindow: 128000, MaxOutput: 4096}

	// 1. Fresh conversation: read_skill is mounted, schedule tools are NOT mounted.
	ts := s.toolsFor(ctx, u, c, m)
	if !hasToolNamed(ts.tools, skills.ReadSkillToolName) {
		t.Fatal("expected common tool read_skill to be mounted")
	}
	for _, stName := range []string{"list_schedules", "create_schedule", "update_schedule", "complete_schedule", "delete_schedule"} {
		if hasToolNamed(ts.tools, stName) {
			t.Fatalf("skill tool %s should NOT be mounted before read_skill", stName)
		}
	}
	if !strings.Contains(ts.skillCatalog, "`schedule`（日程与提醒管理）") || !strings.Contains(ts.skillCatalog, "`create_schedule`") {
		t.Fatalf("skillCatalog should disclose schedule and tool hints:\n%s", ts.skillCatalog)
	}

	// 2. Blind call before read_skill is intercepted and redirects model to read_skill.
	run := &Run{MessageID: "a1", ConversationID: c.ID, UserID: u.ID, subs: map[chan struct{}]struct{}{}}
	turn := &toolTurn{user: u, convID: c.ID, asstID: "a1", run: run}
	res, stop := s.runTools(ctx, turn, []llm.ToolCall{{
		ID:        "c1",
		Name:      "create_schedule",
		Arguments: `{"title":"买菜","start_time":"2026-11-21 09:00"}`,
	}})
	if stop || len(res) != 1 || !strings.Contains(res[0], "read_skill") {
		t.Fatalf("expected blind-call redirect to read_skill, got stop=%v res=%v", stop, res)
	}
	items, _ := s.Store.ListSchedules(ctx, u.ID, true)
	if len(items) != 0 {
		t.Fatalf("blind call must not create schedule, got %d", len(items))
	}

	// 3. Private conversation completely isolates skills, schedule tools, and note injection.
	privConv := &store.Conversation{UserID: u.ID, Private: true}
	if err := s.Store.CreateConversation(ctx, privConv); err != nil {
		t.Fatal(err)
	}
	pts := s.toolsFor(ctx, u, privConv, m)
	if hasToolNamed(pts.tools, skills.ReadSkillToolName) || pts.skillCatalog != "" {
		t.Fatalf("private chat must not expose skills: tools=%+v catalog=%q", pts.tools, pts.skillCatalog)
	}
}

func TestSkillEndToEndActivationLeaseAndUpcomingBrief(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()

	tomorrow := time.Now().In(shanghai).AddDate(0, 0, 1).Format("2006-01-02")
	var streamSeq atomic.Int32

	up := &fakeUpstream{
		handler: func(call recordedCall) (string, string) {
			if !call.Stream {
				return "", "体检提醒"
			}
			n := streamSeq.Add(1)
			switch n {
			case 1:
				// Turn 1 Round 0: model calls read_skill("schedule")
				return `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_skill","type":"function","function":{"name":"read_skill","arguments":"{\"skill_key\":\"schedule\"}"}}]}}]}` + "\n\ndata: [DONE]\n\n", ""
			case 2:
				// Turn 1 Round 1: schedule tools are unlocked; model creates tomorrow's reminder
				argsJSON, _ := json.Marshal(map[string]any{
					"title":             "协和医院空腹体检",
					"start_time":        tomorrow + " 08:30",
					"remind_before_min": 30,
					"location":          "体检中心二楼",
					"note":              "前一天晚上十点后禁食",
				})
				chunk, _ := json.Marshal(map[string]any{
					"choices": []any{map[string]any{
						"delta": map[string]any{
							"tool_calls": []any{map[string]any{
								"index": 0, "id": "call_create", "type": "function",
								"function": map[string]any{"name": "create_schedule", "arguments": string(argsJSON)},
							}},
						},
					}},
				})
				return "data: " + string(chunk) + "\n\ndata: [DONE]\n\n", ""
			case 3:
				return `data: {"choices":[{"delta":{"content":"已经帮您记好明天早上 8:30 的体检提醒啦～"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n", ""
			default:
				return "", ""
			}
		},
	}

	prov := &store.Provider{Name: "mock", Kind: "openai", BaseURL: "https://mock.local/v1", APIKey: "k", Enabled: true}
	if err := s.Store.UpsertProvider(ctx, prov); err != nil {
		t.Fatal(err)
	}
	s.clients[fmt.Sprintf("%s@%d", prov.ID, prov.UpdatedAt)] = up.client("openai")

	model := &store.Model{ProviderID: prov.ID, DisplayName: "TestModel", UpstreamModel: "gpt-test", Kind: store.KindChat, Enabled: true, Tools: true, ContextWindow: 128000, MaxOutput: 2048}
	if err := s.Store.UpsertModel(ctx, model); err != nil {
		t.Fatal(err)
	}
	_ = s.Store.SetSetting(ctx, "default_model_id", model.ID)

	res1, err := s.Send(ctx, u, SendInput{ConversationID: c.ID, Content: "明天早上八点半要去协和医院空腹体检，帮我定个提醒"})
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Hub.Get(res1.Assistant.ID); r != nil {
		<-r.Done()
	}

	// Verify DB schedule created
	scheds, err := s.Store.ListSchedules(ctx, u.ID, false)
	if err != nil || len(scheds) != 1 {
		t.Fatalf("expected 1 schedule saved, got %d (%v)", len(scheds), err)
	}
	if scheds[0].Title != "协和医院空腹体检" || scheds[0].RemindBeforeMin != 30 {
		t.Fatalf("unexpected saved schedule: %+v", scheds[0])
	}

	// Verify Round 0 vs Round 1 requests:
	// - Round 0 had read_skill, NOT create_schedule;
	// - Round 1 unlocked create_schedule, AND Messages[0] (system prompt) stayed 100% identical!
	up.mu.Lock()
	if len(up.calls) != 3 {
		t.Fatalf("expected 3 upstream calls in Turn 1, got %d", len(up.calls))
	}
	r0Tools, _ := json.Marshal(up.calls[0].Tools)
	r1Tools, _ := json.Marshal(up.calls[1].Tools)
	r0Sys := fmt.Sprint(up.calls[0].Messages[0].Content)
	r1Sys := fmt.Sprint(up.calls[1].Messages[0].Content)
	up.mu.Unlock()

	if !strings.Contains(string(r0Tools), "read_skill") || strings.Contains(string(r0Tools), "create_schedule") {
		t.Fatalf("Round 0 tools mismatch: %s", string(r0Tools))
	}
	if !strings.Contains(string(r1Tools), "create_schedule") || !strings.Contains(string(r1Tools), "list_schedules") {
		t.Fatalf("Round 1 should dynamically mount schedule tools: %s", string(r1Tools))
	}
	if r0Sys != r1Sys {
		t.Fatal("system prompt Messages[0] must NOT mutate mid-turn (preserves upstream prefix cache)")
	}

	// Verify Turn 2..4: lease keeps schedule tools mounted AND per-turn note carries tomorrow's schedule brief!
	for turnNo := 0; turnNo < 3; turnNo++ {
		resNext, err := s.Send(ctx, u, SendInput{ConversationID: c.ID, Content: fmt.Sprintf("顺便聊聊天 %d", turnNo)})
		if err != nil {
			t.Fatal(err)
		}
		if r := s.Hub.Get(resNext.Assistant.ID); r != nil {
			<-r.Done()
		}
	}

	up.mu.Lock()
	turn2Req := up.calls[3]
	turn2Tools, _ := json.Marshal(turn2Req.Tools)
	lastUserText := fmt.Sprint(turn2Req.Messages[len(turn2Req.Messages)-1].Content)
	up.mu.Unlock()

	if !strings.Contains(string(turn2Tools), "create_schedule") {
		t.Fatalf("Turn 2 should keep schedule tools mounted via active lease: %s", string(turn2Tools))
	}
	if !strings.Contains(lastUserText, "明天 08:30 协和医院空腹体检") {
		t.Fatalf("Turn 2 last user note should carry tomorrow's schedule brief, got: %s", lastUserText)
	}

	// After 3 idle turns without invoking schedule tools, lease expires cleanly!
	tsAfter := s.toolsFor(ctx, u, c, model)
	if hasToolNamed(tsAfter.tools, "create_schedule") {
		t.Fatal("expected schedule lease to expire after 3 idle turns")
	}

	// Conflict detection test: creating an overlapping schedule at 08:50 tomorrow warns about 08:30!
	run2 := &Run{MessageID: "a2", ConversationID: c.ID, UserID: u.ID, subs: map[chan struct{}]struct{}{}}
	turn2 := &toolTurn{user: u, convID: c.ID, asstID: "a2", run: run2, activeSkills: map[string]bool{"schedule": true}}
	conflictOut := s.execCreateSchedule(ctx, turn2, fmt.Sprintf(`{"title":"去菜市场买排骨","start_time":"%s 08:50"}`, tomorrow))
	if !strings.Contains(conflictOut, "时间相近提醒") || !strings.Contains(conflictOut, "协和医院空腹体检") {
		t.Fatalf("expected nearby conflict warning, got: %s", conflictOut)
	}
}
