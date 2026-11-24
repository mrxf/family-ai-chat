package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

// ClientContext is an ephemeral snapshot of the sending device, never a system prompt or account preference.
type ClientContext struct {
	DeviceID   string `json:"device_id"`
	DeviceKind string `json:"device_kind"`
	Settings   struct {
		FontScale float64 `json:"font_scale"`
		Theme     string  `json:"theme"`
	} `json:"settings"`
	Capabilities      map[string]any `json:"capabilities"`
	SuggestionsPinned bool           `json:"suggestions_pinned"`
}

var appFontScales = []float64{0.85, 0.92, 1, 1.15, 1.3}
var appFontLabels = []string{"小", "较小", "标准", "大", "特大"}

const unsupportedClient = "当前客户端版本不支持此操作，请升级或手动在设置里调整。"

func appTool(name, description string, props map[string]any, required ...string) llm.Tool {
	p := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		p["required"] = required
	}
	return llm.Tool{Name: name, Description: description, Parameters: p}
}

var getAppSettingsTool = appTool("get_app_settings", "读取发消息设备的设置与能力。", map[string]any{})
var setAppSettingsTool = appTool("set_app_settings", "只调整发消息设备的字体和主题；用户只表达感受时必须先询问确认。", map[string]any{
	"font_scale": map[string]any{"type": "string", "enum": []string{"小", "较小", "标准", "大", "特大", "step_up", "step_down"}},
	"theme":      map[string]any{"type": "string", "enum": []string{"system", "light", "dark"}},
})
var toggleSuggestionsTool = appTool("toggle_suggestions_card", "显示或隐藏本机首页猜你想问。", map[string]any{"visible": map[string]any{"type": "boolean"}}, "visible")
var listHomeCardsTool = appTool("list_home_cards", "按首页顺序读取卡片，含猜你想问状态。", map[string]any{"keyword": map[string]any{"type": "string"}})
var reorderHomeCardsTool = appTool("reorder_home_cards", "调整首页卡片顺序；component_ref 使用列表中的短ID或完整ID；数字位置从1开始。", map[string]any{
	"component_ref": map[string]any{"type": "string"}, "position": map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer", "minimum": 1}}},
}, "component_ref", "position")

func homeControlTools() []llm.Tool {
	return []llm.Tool{listHomeCardsTool, reorderHomeCardsTool, pinComponentTool, unpinComponentTool, listComponentsTool, askUserQuestionTool}
}
func isSettingsTool(name string) bool {
	return name == "get_app_settings" || name == "set_app_settings" || name == "toggle_suggestions_card"
}
func isAppControlTool(name string) bool {
	if isSettingsTool(name) {
		return true
	}
	for _, tool := range homeControlTools() {
		if name == tool.Name {
			return true
		}
	}
	return false
}

func (s *Service) buildAppControlSkill() skills.Definition {
	tools := append(homeControlTools(), getAppSettingsTool, setAppSettingsTool, toggleSuggestionsTool)
	return skills.Definition{
		Key: "app_control", Title: "应用设置与首页管理",
		Description:  "管理首页卡片与发消息设备的字体、主题。",
		WhenToUse:    "用户要求调整字体大小、深浅色；固定或取消固定卡片、排序首页、显示或隐藏猜你想问时。",
		LeaseSummary: "「应用设置与首页管理 (app_control)」已解锁，可直接调用 set_app_settings / pin_component / unpin_component / reorder_home_cards 等；再大一点、那张也拿掉无需再次 read_skill。",
		Available:    func(ctx skills.Context) bool { return !ctx.Private },
		Tools:        tools,
		Dynamic: func(ctx skills.Context) string {
			client, _ := ctx.ClientContext.(*ClientContext)
			return s.appControlSnapshot(context.Background(), &toolTurn{user: &store.User{ID: ctx.UserID}, clientCtx: client})
		},
		Content: `只操作发消息的设备，不同步其他设备。第一期仅支持字体、主题与首页管理。
- 明确要求（把字调大、换成深色）直接执行，说明结果及卡片上的撤销。
- 仅表达感受（看不清、字太小、太亮/太暗）没有明确要求，必须先调用 ask_user_question，给出「调大一档 / 调到特大 / 先不用」等选项，确认后才调整。
- 相对字号用 step_up / step_down，不要猜数值。回复简短自然，不输出参数、JSON或内部ID。
- 固定卡片不传ID时依次选本轮新卡片、用户引用卡片、最近助手消息最后一张可固定卡片。已经固定无需重复操作。
- 取消固定不需要确认。关键字多匹配必须使用 ask_user_question 点选，不能擅自选择。
- 删除是不可逆操作，必须先调用 ask_user_question 确认；需要删除时再读取 ui_components 技能。
- 永远不开放退出登录、修改密码和个人资料、切换服务器、邀请码、所有管理员功能（模型、供应商、用户管理等）。温和说明请用户自行设置。
- 语速、触感、页面跳转和其他设备应用属于第二期，当前不可执行。
- 每轮应用管理工具最多6次。`,
	}
}

func fontScaleTarget(current float64, request string) (float64, string, error) {
	idx := 0
	for i, v := range appFontScales {
		if math.Abs(v-current) < math.Abs(appFontScales[idx]-current) {
			idx = i
		}
	}
	switch request {
	case "step_up":
		if idx == len(appFontScales)-1 {
			return current, "已经是最大档", nil
		}
		idx++
	case "step_down":
		if idx == 0 {
			return current, "已经是最小档", nil
		}
		idx--
	default:
		idx = -1
		for i, label := range appFontLabels {
			if request == label {
				idx = i
				break
			}
		}
		if idx < 0 {
			return 0, "", fmt.Errorf("字号请使用小/较小/标准/大/特大或step_up/step_down")
		}
	}
	return appFontScales[idx], "字体调到「" + appFontLabels[idx] + "」", nil
}

func (s *Service) appControlSnapshot(ctx context.Context, t *toolTurn) string {
	var b strings.Builder
	if t.clientCtx == nil {
		b.WriteString(unsupportedClient)
	} else {
		raw, _ := json.Marshal(t.clientCtx)
		b.WriteString("当前设备快照：" + string(raw))
	}
	b.WriteString("\n首页卡片（标题与短ID，仅供工具使用）：\n")
	pins, err := s.Store.ListPinnedComponents(ctx, t.user.ID)
	if err != nil {
		return b.String() + "读取失败"
	}
	for i, p := range pins {
		if i >= 20 {
			break
		}
		title, _ := summarizeComponentForQuote(p.Component)
		fmt.Fprintf(&b, "%d. %s [%s]\n", i+1, title, shortHomeRef(p.ID))
	}
	return b.String()
}

func (s *Service) execAppControlTool(ctx context.Context, t *toolTurn, name, raw string) (string, bool) {
	switch name {
	case "list_home_cards":
		return s.execListHomeCards(ctx, t, raw), true
	case "reorder_home_cards":
		return s.execReorderHomeCards(ctx, t, raw), true
	}
	if !isSettingsTool(name) {
		return "", false
	}
	if t.private {
		return "隐私对话不支持应用管理，请退出隐私对话或手动调整设置。", true
	}
	if t.automation || t.clientCtx == nil || strings.TrimSpace(t.clientCtx.DeviceID) == "" {
		return unsupportedClient, true
	}
	if name == "get_app_settings" {
		data, _ := json.Marshal(t.clientCtx)
		return string(data), true
	}
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args) != nil {
		return "参数错误：需要合法JSON对象。", true
	}
	patch := map[string]any{}
	summary := []string{}
	action := "set_settings"
	if name == "toggle_suggestions_card" {
		visible, ok := args["visible"].(bool)
		if !ok || len(args) != 1 {
			return "请提供visible布尔值。", true
		}
		action = "toggle_suggestions"
		patch["visible"] = visible
		label := "隐藏"
		if visible {
			label = "显示"
		}
		summary = append(summary, label+"猜你想问")
	} else {
		for key, val := range args {
			switch key {
			case "font_scale":
				request, ok := val.(string)
				if !ok {
					return "字号必须使用档位名称。", true
				}
				target, label, err := fontScaleTarget(t.clientCtx.Settings.FontScale, request)
				if err != nil {
					return err.Error(), true
				}
				summary = append(summary, label)
				if !strings.HasPrefix(label, "已经") {
					patch[key] = target
				}
			case "theme":
				theme, ok := val.(string)
				if !ok || (theme != "system" && theme != "light" && theme != "dark") {
					return "主题只支持system/light/dark。", true
				}
				patch[key] = theme
				summary = append(summary, "主题改为"+map[string]string{"system": "跟随系统", "light": "浅色", "dark": "深色"}[theme])
			default:
				return "第一期仅支持font_scale和theme。", true
			}
		}
	}
	if len(patch) == 0 {
		if len(summary) > 0 {
			return strings.Join(summary, "；"), true
		}
		return "请指定要调整的字体或主题。", true
	}
	data := map[string]any{"action": action, "target_device_id": t.clientCtx.DeviceID, "patch": patch, "summary": strings.Join(summary, "；")}
	if _, err := s.appendAndPersistComponent(t, "app_action", data, map[string]any{"status": "pending"}); err != nil {
		return "保存操作失败：" + err.Error(), true
	}
	// Relative operations in the same turn build on the preceding validated action.
	if v, ok := patch["font_scale"].(float64); ok {
		t.clientCtx.Settings.FontScale = v
	}
	if v, ok := patch["theme"].(string); ok {
		t.clientCtx.Settings.Theme = v
	}
	if v, ok := patch["visible"].(bool); ok {
		t.clientCtx.SuggestionsPinned = v
	}
	t.run.addDoneStep("app", "已发送本机操作："+strings.Join(summary, "；"), "")
	return "操作已发送给发消息设备，等待本机应用；应用后可在卡片上撤销。", true
}
