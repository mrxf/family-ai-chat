package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

func TestAppFontSteps(t *testing.T) {
	for i, label := range appFontLabels {
		v, _, err := fontScaleTarget(1, label)
		if err != nil || v != appFontScales[i] {
			t.Fatalf("%s: %v %v", label, v, err)
		}
	}
	for _, tc := range []struct {
		current float64
		request string
		want    float64
		note    string
	}{
		{1, "step_up", 1.15, "大"}, {1, "step_down", .92, "较小"}, {1.3, "step_up", 1.3, "已经是最大"}, {.85, "step_down", .85, "已经是最小"}, {1.02, "step_up", 1.15, "大"},
	} {
		v, note, err := fontScaleTarget(tc.current, tc.request)
		if err != nil || v != tc.want || !strings.Contains(note, tc.note) {
			t.Fatalf("%+v: %v %s %v", tc, v, note, err)
		}
	}
	if _, _, err := fontScaleTarget(1, "huge"); err == nil {
		t.Fatal("invalid step accepted")
	}
}

func TestAppControlContextVisibilityAndActions(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()
	if _, ok := s.Skills.Get("app_control", skills.Context{UserID: u.ID, Private: true}); ok {
		t.Fatal("private skill exposed")
	}
	asst := &store.Message{Role: "assistant", Status: store.StatusStreaming}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{asst}}); err != nil {
		t.Fatal(err)
	}
	turn := &toolTurn{user: u, convID: c.ID, asstID: asst.ID, run: &Run{MessageID: asst.ID}, activeSkills: map[string]bool{"app_control": true}}
	for _, name := range []string{"set_app_settings", "toggle_suggestions_card"} {
		out, _ := s.execAppControlTool(ctx, turn, name, `{"theme":"dark"}`)
		if !strings.Contains(out, "请升级或手动") {
			t.Fatal(out)
		}
	}
	turn.clientCtx = &ClientContext{DeviceID: "dev_1", DeviceKind: "desktop"}
	turn.clientCtx.Settings.FontScale = 1
	guide, tools := s.execReadSkillCall(turn, `{"skill_key":"app_control"}`)
	if !strings.Contains(guide, "dev_1") || !strings.Contains(guide, "先调用 ask_user_question") || !hasToolNamed(tools, "set_app_settings") {
		t.Fatal(guide)
	}
	out, _ := s.execAppControlTool(ctx, turn, "set_app_settings", `{"font_scale":"step_up","theme":"dark"}`)
	if !strings.Contains(out, "等待本机") {
		t.Fatal(out)
	}
	saved, err := s.Store.GetMessageForUser(ctx, asst.ID, u.ID)
	if err != nil || len(saved.Components) != 1 {
		t.Fatalf("%+v %v", saved, err)
	}
	comp := saved.Components[0]
	var data map[string]any
	_ = json.Unmarshal(comp.Data, &data)
	if comp.Kind != "app_action" || data["target_device_id"] != "dev_1" || !strings.Contains(componentNote(saved.Components), "等待本机应用") {
		t.Fatalf("%+v", comp)
	}
	if _, err := s.Store.PinComponent(ctx, u.ID, asst.ID, comp.ID); !errors.Is(err, store.ErrComponentNotPinnable) {
		t.Fatal(err)
	}
	before := len(saved.Components)
	for _, bad := range []string{`{"tts_rate":0.5}`, `{"font_scale":1.3}`, `{"theme":"sepia"}`, `{"logout":true}`} {
		s.execAppControlTool(ctx, turn, "set_app_settings", bad)
	}
	saved, _ = s.Store.GetMessageForUser(ctx, asst.ID, u.ID)
	if len(saved.Components) != before {
		t.Fatal("invalid patches persisted")
	}
	turn.automation = true
	_, tools = s.execReadSkillCall(turn, `{"skill_key":"app_control"}`)
	for _, tool := range tools {
		if isSettingsTool(tool.Name) {
			t.Fatal("automation settings tool mounted")
		}
	}
}

func TestHomeDefaultPinKeywordAndMultiSkillGuard(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()
	first := &store.Message{Role: "assistant", Status: store.StatusDone, Components: []store.MessageComponent{
		{ID: "weather1", Kind: "weather", Data: json.RawMessage(`{"location":"上海天气"}`)},
		{ID: "weather2", Kind: "weather", Data: json.RawMessage(`{"location":"北京天气"}`)},
		{ID: "question", Kind: "ask_user", Data: json.RawMessage(`{}`)},
	}}
	userMsg := &store.Message{Role: "user", Content: "固定"}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{first, userMsg}}); err != nil {
		t.Fatal(err)
	}
	turn := &toolTurn{user: u, convID: c.ID, run: &Run{}, activeSkills: map[string]bool{"app_control": true}}
	mid, cid := s.defaultPinTarget(ctx, turn)
	if mid != first.ID || cid != "weather2" {
		t.Fatalf("%s %s", mid, cid)
	}
	turn.quotedComponent = &ComponentQuoteRef{MessageID: first.ID, ComponentID: "weather1"}
	_, cid = s.defaultPinTarget(ctx, turn)
	if cid != "weather1" {
		t.Fatal(cid)
	}
	turn.lastCreatedMsgID = first.ID
	turn.lastCreatedCompID = "weather2"
	_, cid = s.defaultPinTarget(ctx, turn)
	if cid != "weather2" {
		t.Fatal(cid)
	}
	out, _ := s.runTools(ctx, turn, []llm.ToolCall{{Name: "pin_component", Arguments: `{}`}})
	if !strings.Contains(out[0], "固定到首页") {
		t.Fatal(out)
	}
	out, _ = s.runTools(ctx, turn, []llm.ToolCall{{Name: "pin_component", Arguments: `{}`}})
	if !strings.Contains(out[0], "已经在首页了") {
		t.Fatal(out)
	}
	if _, err := s.Store.PinComponent(ctx, u.ID, first.ID, "weather1"); err != nil {
		t.Fatal(err)
	}
	text := s.execUnpinComponent(ctx, turn, `{"keyword":"天气"}`)
	if !strings.Contains(text, "ask_user_question") {
		t.Fatal(text)
	}
	pins, _ := s.Store.ListPinnedComponents(ctx, u.ID)
	if len(pins) != 2 {
		t.Fatal("ambiguous unpin changed pins")
	}
	text = s.execUnpinComponent(ctx, turn, `{"keyword":"上海"}`)
	if !strings.Contains(text, "取消固定") {
		t.Fatal(text)
	}
	text = s.execUnpinComponent(ctx, turn, `{"keyword":"不存在"}`)
	if !strings.Contains(text, "北京天气") {
		t.Fatal(text)
	}
	turn.componentManageCalls = 0
	turn.appControlCalls = 6
	out, _ = s.runTools(ctx, turn, []llm.ToolCall{{Name: "list_home_cards", Arguments: `{}`}})
	if !strings.Contains(out[0], "6 次上限") {
		t.Fatal(out)
	}
}

func TestHomeListAndReorder(t *testing.T) {
	s, u, c := contextFixture(t)
	ctx := t.Context()
	m := &store.Message{Role: "assistant", Status: store.StatusDone, Components: []store.MessageComponent{
		{ID: "w", Kind: "weather", Data: json.RawMessage(`{"location":"天气"}`)},
		{ID: "t", Kind: "todo", Data: json.RawMessage(`{"title":"购物清单"}`)},
	}}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{m}}); err != nil {
		t.Fatal(err)
	}
	for _, comp := range m.Components {
		if _, err := s.Store.PinComponent(ctx, u.ID, m.ID, comp.ID); err != nil {
			t.Fatal(err)
		}
	}
	turn := &toolTurn{user: u, convID: c.ID, run: &Run{}, clientCtx: &ClientContext{DeviceID: "dev", SuggestionsPinned: true}}
	for _, position := range []any{"first", "last", 1, 2} {
		raw, _ := json.Marshal(map[string]any{"component_ref": "w", "position": position})
		out := s.execReorderHomeCards(ctx, turn, string(raw))
		if !strings.Contains(out, "已调整") {
			t.Fatal(out)
		}
		pins, _ := s.Store.ListPinnedComponents(ctx, u.ID)
		index := 0
		if position == "last" || position == 2 {
			index = 1
		}
		if pins[index].ComponentID != "w" {
			t.Fatalf("position %v: %+v", position, pins)
		}
	}
	out := s.execListHomeCards(ctx, turn, `{"keyword":"购物"}`)
	if !strings.Contains(out, "猜你想问：显示=true") || !strings.Contains(out, "购物清单") || strings.Contains(out, "天气") {
		t.Fatal(out)
	}
}
