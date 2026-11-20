package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

func TestComponentsAndAskUserSkillCatalogAndBlindGuard(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()
	m := &store.Model{ID: "m1", UpstreamModel: "gpt-test", Enabled: true, Tools: true, ContextWindow: 128000, MaxOutput: 4096}

	ts := s.toolsFor(ctx, u, c, m)
	if !hasToolNamed(ts.tools, skills.ReadSkillToolName) {
		t.Fatal("expected read_skill mounted")
	}
	for _, hidden := range []string{"show_weather", "show_todo", "show_chart", "show_quiz", "show_metrics", "ask_user_question"} {
		if hasToolNamed(ts.tools, hidden) {
			t.Fatalf("skill-scoped tool %s must not be mounted before read_skill", hidden)
		}
		if !strings.Contains(ts.skillCatalog, "`"+hidden+"`") {
			t.Fatalf("skillCatalog must disclose tool hint for %s:\n%s", hidden, ts.skillCatalog)
		}
	}

	// Blind calls before read_skill must redirect to the owning skill.
	run := &Run{MessageID: "a1", ConversationID: c.ID, UserID: u.ID, subs: map[chan struct{}]struct{}{}}
	turn := &toolTurn{user: u, convID: c.ID, asstID: "a1", run: run}
	res, _ := s.runTools(ctx, turn, []llm.ToolCall{
		{ID: "c1", Name: "show_weather", Arguments: `{"location":"北京","condition":"晴","low":2,"high":14}`},
		{ID: "c2", Name: "ask_user_question", Arguments: `{"questions":[{"question":"去几天？","options":["2天","3天"]}]}`},
	})
	if !strings.Contains(res[0], "ui_components") || !strings.Contains(res[1], "ask_user") {
		t.Fatalf("expected blind-call redirects to ui_components and ask_user, got %v", res)
	}
	if len(run.Snapshot().Components) != 0 {
		t.Fatal("blind calls must not emit components")
	}
}

func TestComponentsInterleavedPosAndMultiTurnStateContext(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()

	intro := "我刚查好了北京明天的天气，先看卡片："
	introRunes := utf8.RuneCountInString(intro)

	var streamSeq atomic.Int32
	var turn2Prompt string

	up := &fakeUpstream{
		handler: func(call recordedCall) (string, string) {
			if !call.Stream {
				return "", "周末出行天气与清单"
			}
			n := streamSeq.Add(1)
			switch n {
			case 1:
				// Turn 1 Round 0: activate ui_components
				return `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_sk","type":"function","function":{"name":"read_skill","arguments":"{\"skill_key\":\"ui_components\"}"}}]}}]}` + "\n\ndata: [DONE]\n\n", ""
			case 2:
				// Turn 1 Round 1: stream intro text, then call show_weather + show_todo + show_chart + show_quiz + show_metrics
				calls := []map[string]any{
					{
						"index": 0, "id": "call_w", "type": "function",
						"function": map[string]any{
							"name":      "show_weather",
							"arguments": `{"location":"北京","date_label":"明天 周六","condition":"晴转多云","temp":9,"low":1,"high":13,"wind":"北风 3级","tip":"早晚温差大，带件防风外套","forecast":[{"day":"周六","condition":"晴","low":1,"high":13},{"day":"周日","condition":"多云","low":0,"high":11}]}`,
						},
					},
					{
						"index": 1, "id": "call_t", "type": "function",
						"function": map[string]any{
							"name":      "show_todo",
							"arguments": `{"title":"出门准备","items":[{"text":"保温杯","tag":"必带"},{"text":"充电宝"},{"text":"身份证","done":true}]}`,
						},
					},
					{
						"index": 2, "id": "call_ch", "type": "function",
						"function": map[string]any{
							"name":      "show_chart",
							"arguments": `{"chart_type":"line","title":"周末白天气温","unit":"℃","labels":["周五","周六","周日"],"series":[{"name":"最高温","values":[12,13,11]}]}`,
						},
					},
					{
						"index": 3, "id": "call_qz", "type": "function",
						"function": map[string]any{
							"name":      "show_quiz",
							"arguments": `{"question":"北方冬季洋葱式穿衣最内层适合穿什么？","tag":"生活小测","options":["A. 纯棉厚卫衣","B. 透气排汗贴身层","C. 防风冲锋衣"],"answer_index":1,"explanation":"排汗透气层能避免出汗后着凉。"}`,
						},
					},
					{
						"index": 4, "id": "call_mt", "type": "function",
						"function": map[string]any{
							"name":      "show_metrics",
							"arguments": `{"title":"周末预算估算","metrics":[{"label":"交通","value":"180","unit":"元","status":"good"},{"label":"餐饮门票","value":"420","unit":"元","status":"normal"}],"summary":"合计约 600 元"}`,
						},
					},
				}
				c1, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": intro}}}})
				c2, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": calls}}}})
				return "data: " + string(c1) + "\n\ndata: " + string(c2) + "\n\ndata: [DONE]\n\n", ""
			case 3:
				// Turn 1 Round 2: closing text after components
				return `data: {"choices":[{"delta":{"content":"祝你们周末玩得开心，记得按清单核对一下哦～"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n", ""
			case 4:
				// Turn 2: inspect history message fed back to LLM after user checks off "保温杯" and answers quiz!
				for _, m := range call.Messages {
					if sContent, _ := m.Content.(string); m.Role == "assistant" && strings.Contains(sContent, "交互卡片") {
						turn2Prompt = sContent
					}
				}
				return `data: {"choices":[{"delta":{"content":"看到你只剩充电宝没勾啦，出门前别忘装包里！"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n", ""
			default:
				return "", ""
			}
		},
	}

	prov := &store.Provider{Name: "mock", Kind: "openai", BaseURL: "https://mock.local/v1", APIKey: "k", Enabled: true}
	_ = s.Store.UpsertProvider(ctx, prov)
	s.clients[fmt.Sprintf("%s@%d", prov.ID, prov.UpdatedAt)] = up.client("openai")
	model := &store.Model{ProviderID: prov.ID, DisplayName: "TestModel", UpstreamModel: "gpt-test", Kind: store.KindChat, Enabled: true, Tools: true, ContextWindow: 128000, MaxOutput: 2048}
	_ = s.Store.UpsertModel(ctx, model)
	_ = s.Store.SetSetting(ctx, "default_model_id", model.ID)

	res1, err := s.Send(ctx, u, SendInput{ConversationID: c.ID, Content: "帮我看看明天北京天气并列个周末出行清单、气温图、穿衣小测和预算"})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Hub.Get(res1.Assistant.ID).Done()

	persisted, err := s.Store.GetMessageForUser(ctx, res1.Assistant.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Components) != 5 {
		t.Fatalf("expected 5 persisted components, got %d: %+v", len(persisted.Components), persisted.Components)
	}
	for _, comp := range persisted.Components {
		if comp.Pos != introRunes {
			t.Fatalf("expected component %s pos=%d (after intro), got %d", comp.Kind, introRunes, comp.Pos)
		}
	}

	// Simulate user checking off i1 ("保温杯") in addition to i3 ("身份证") and picking option 1 on quiz!
	todoID := persisted.Components[1].ID
	quizID := persisted.Components[3].ID
	if _, _, err := s.Store.UpdateMessageComponentState(ctx, persisted.ID, u.ID, todoID, json.RawMessage(`{"done_ids":["i1","i3"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Store.UpdateMessageComponentState(ctx, persisted.ID, u.ID, quizID, json.RawMessage(`{"selected_index":1}`)); err != nil {
		t.Fatal(err)
	}

	res2, err := s.Send(ctx, u, SendInput{ConversationID: c.ID, Content: "看看我还差啥没准备好？"})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Hub.Get(res2.Assistant.ID).Done()

	if !strings.Contains(turn2Prompt, "已完成 2/3") || !strings.Contains(turn2Prompt, "未勾选：充电宝") || !strings.Contains(turn2Prompt, "回答正确") {
		t.Fatalf("expected updated component state in Turn 2 LLM context, got:\n%s", turn2Prompt)
	}
}

func TestAskUserQuestionSkillStopsToolsForWrapUp(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()

	var streamSeq atomic.Int32
	var wrapUpToolsCount int

	up := &fakeUpstream{
		handler: func(call recordedCall) (string, string) {
			if !call.Stream {
				return "", "周末出游规划"
			}
			n := streamSeq.Add(1)
			switch n {
			case 1:
				return `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c_sk","type":"function","function":{"name":"read_skill","arguments":"{\"skill_key\":\"ask_user\"}"}}]}}]}` + "\n\ndata: [DONE]\n\n", ""
			case 2:
				args := `{"title":"定制周末行程","questions":[{"question":"这次想玩几天？","options":[{"label":"(Recommended) 2天1晚","description":"轻松不赶路"},{"label":"当天往返"}]},{"question":"和谁一起去？","multi_select":true,"options":["父母长辈","孩子","朋友"]}]}`
				chunk, _ := json.Marshal(map[string]any{
					"choices": []any{map[string]any{
						"delta": map[string]any{
							"tool_calls": []any{map[string]any{
								"index": 0, "id": "c_ask", "type": "function",
								"function": map[string]any{"name": "ask_user_question", "arguments": args},
							}},
						},
					}},
				})
				return "data: " + string(chunk) + "\n\ndata: [DONE]\n\n", ""
			case 3:
				wrapUpToolsCount = len(call.Tools)
				return `data: {"choices":[{"delta":{"content":"先点选一下上面的偏好，我马上为你定制路线～"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n", ""
			default:
				return "", ""
			}
		},
	}

	prov := &store.Provider{Name: "mock", Kind: "openai", BaseURL: "https://mock.local/v1", APIKey: "k", Enabled: true}
	_ = s.Store.UpsertProvider(ctx, prov)
	s.clients[fmt.Sprintf("%s@%d", prov.ID, prov.UpdatedAt)] = up.client("openai")
	model := &store.Model{ProviderID: prov.ID, DisplayName: "TestModel", UpstreamModel: "gpt-test", Kind: store.KindChat, Enabled: true, Tools: true, ContextWindow: 128000, MaxOutput: 2048}
	_ = s.Store.UpsertModel(ctx, model)
	_ = s.Store.SetSetting(ctx, "default_model_id", model.ID)

	res, err := s.Send(ctx, u, SendInput{ConversationID: c.ID, Content: "帮我规划个周末周边游"})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Hub.Get(res.Assistant.ID).Done()

	if wrapUpToolsCount != 0 {
		t.Fatalf("expected wrap-up round after ask_user_question to disable tools (0), got %d", wrapUpToolsCount)
	}
	got, err := s.Store.GetMessageForUser(ctx, res.Assistant.ID, u.ID)
	if err != nil || len(got.Components) != 1 || got.Components[0].Kind != "ask_user" {
		t.Fatalf("expected 1 ask_user component persisted, got %+v err=%v", got, err)
	}
	var parsed askUserCardData
	if err := json.Unmarshal(got.Components[0].Data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Questions) != 2 || !parsed.Questions[0].Options[0].Recommended || parsed.Questions[0].Options[0].Label != "2天1晚" {
		t.Fatalf("unexpected normalized ask_user data: %+v", parsed)
	}
}

func TestCompactWeatherMinimalArgsAndAskUserDismissNote(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()

	uMsg := &store.Message{Role: "user", Content: "杭州冷吗", Status: store.StatusDone}
	aMsg := &store.Message{Role: "assistant", Status: store.StatusStreaming}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{uMsg, aMsg}}); err != nil {
		t.Fatal(err)
	}

	run := &Run{MessageID: aMsg.ID, ConversationID: c.ID, UserID: u.ID, subs: map[chan struct{}]struct{}{}}
	turn := &toolTurn{
		user:         u,
		convID:       c.ID,
		asstID:       aMsg.ID,
		run:          run,
		activeSkills: map[string]bool{skillKeyComponents: true, skillKeyAskUser: true},
	}

	// Minimal weather call without low/high/forecast/humidity -> succeeds and defaults to Compact=true.
	out := s.execShowWeather(turn, `{"location":"杭州","condition":"多云","temp":18}`)
	if !strings.Contains(out, "杭州 多云 18°") {
		t.Fatalf("unexpected minimal weather output: %s", out)
	}
	comps := run.Snapshot().Components
	if len(comps) != 1 {
		t.Fatalf("expected 1 component, got %d", len(comps))
	}
	var wd weatherCardData
	if err := json.Unmarshal(comps[0].Data, &wd); err != nil || !wd.Compact || wd.Temp == nil || *wd.Temp != 18 {
		t.Fatalf("expected compact weather data with temp=18, got %+v err=%v", wd, err)
	}

	// Dismissed ask_user state generates non-nagging note for follow-up turns.
	note := componentNote([]store.MessageComponent{{
		ID:    "ask_1",
		Kind:  "ask_user",
		Data:  json.RawMessage(`{"questions":[{"id":"q1","question":"去哪玩？","options":[{"label":"西湖"},{"label":"灵隐寺"}]}]}`),
		State: json.RawMessage(`{"submitted":false,"dismissed":true}`),
	}})
	if !strings.Contains(note, "用户已主动跳过关闭该询问卡") {
		t.Fatalf("expected dismissed guidance in componentNote, got: %s", note)
	}
}
