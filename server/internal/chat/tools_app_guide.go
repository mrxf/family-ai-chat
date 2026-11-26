package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"nuan/internal/chat/guide"
	"nuan/internal/llm"
	"nuan/internal/skills"
)

const skillKeyAppGuide = "app_guide"

var readAppGuideTool = llm.Tool{
	Name:        "read_app_guide",
	Description: "按主题按需读取小暖 App 的官方使用指南文档（支持一次读取 1–3 个主题）。回答小暖使用方法、入口路径、两端差异或功能限制前先调用本工具。",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"topics": map[string]any{
				"type":        "array",
				"description": "要查阅的指南主题 key 列表（1 到 3 个），如 [\"memories\"] 或 [\"chat\", \"settings_and_shortcuts\"]。",
				"minItems":    1,
				"maxItems":    guide.MaxTopicsPerRead,
				"items": map[string]any{
					"type": "string",
					"enum": guide.TopicKeys(),
				},
			},
		},
		"required":             []string{"topics"},
		"additionalProperties": false,
	},
}

func (s *Service) buildAppGuideSkill() skills.Definition {
	content := strings.TrimSpace(`你是小暖的内置产品向导，负责根据真实文档准确回答“小暖怎么用、功能在哪里、能不能做到、手机/电脑端怎么操作”。

使用规则：
1. **先读文档再回答**：不要凭记忆或猜想编造不存在的菜单名、按钮或功能（例如不要编造小暖支持直接上传 Word/TXT 导入记忆）。先调用 ` + "`read_app_guide`" + ` 读取最相关的 1–3 个主题文档。
2. **贴合当前设备**：若已知用户当前在手机端或电脑端，优先给出对应端的操作路径；如果另一端有明显差异（如电脑端无本地通知、iOS/电脑端不支持外部 App 系统分享），顺带用一句话说明即可。
3. **直白、准确、给替代方案**：直接写出准确按钮/页面名称；遇到小暖当前不支持的能力或字数/张数限制，如实说明，并给出文档里写明的可行替代办法。
4. **不要暴露内部概念**：回答里不要提 skill、tool、read_app_guide、主题 key 或内部字段名。

` + guide.CatalogMarkdown())

	return skills.Definition{
		Key:          skillKeyAppGuide,
		Title:        "小暖使用指南",
		Description:  "查询小暖 App 的真实功能、操作入口、手机/电脑端差异与已知限制，按主题按需读取对应操作文档。",
		WhenToUse:    "当用户询问小暖怎么用、有哪些功能、某个入口在哪、为什么导入/操作受限、怎么导入记忆、怎么发图/画图/语音、怎么共享对话、怎么定日程或定时任务、怎么管理首页卡片、电脑端快捷键或设置项在哪等问题时。",
		LeaseSummary: "「小暖使用指南 (app_guide)」已解锁，可直接调用 read_app_guide 按主题查阅其他功能的准确操作文档。",
		ToolHints: []skills.ToolHint{
			{Name: readAppGuideTool.Name, Brief: "按主题读取小暖对应功能的操作文档（每次 1–3 个主题）"},
		},
		Tools:     []llm.Tool{readAppGuideTool},
		Content:   content,
		Available: func(ctx skills.Context) bool { return !ctx.Private },
		Dynamic: func(ctx skills.Context) string {
			client, _ := ctx.ClientContext.(*ClientContext)
			if client == nil {
				return ""
			}
			kind := strings.ToLower(strings.TrimSpace(client.DeviceKind))
			switch {
			case kind == "desktop" || kind == "macos" || kind == "windows" || kind == "linux":
				return "当前发消息设备：电脑端（" + client.DeviceKind + "），请优先给出电脑端的操作路径与快捷键，必要时再补充手机端差异。"
			case kind == "mobile" || kind == "android" || kind == "ios":
				return "当前发消息设备：手机端（" + client.DeviceKind + "），请优先给出手机端的操作路径，必要时再补充电脑端差异。"
			default:
				return ""
			}
		},
	}
}

func parseAppGuideTopics(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var args struct {
		Topics []string `json:"topics"`
		Topic  string   `json:"topic"`
		Key    string   `json:"key"`
	}
	if json.Unmarshal([]byte(raw), &args) != nil {
		return nil
	}
	out := make([]string, 0, len(args.Topics)+1)
	for _, t := range args.Topics {
		if v := strings.TrimSpace(t); v != "" {
			out = append(out, v)
		}
	}
	for _, single := range []string{args.Topic, args.Key} {
		if v := strings.TrimSpace(single); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (s *Service) execAppGuideTool(t *toolTurn, name, rawArgs string) (string, bool) {
	if name != readAppGuideTool.Name {
		return "", false
	}
	topics := parseAppGuideTopics(rawArgs)
	if len(topics) == 0 {
		return fmt.Sprintf("参数错误：请提供要查阅的 topics（可选：%s）。", strings.Join(guide.TopicKeys(), ", ")), true
	}
	content, titles, unknown := guide.Read(topics)
	if content == "" {
		return fmt.Sprintf("未找到对应的使用指南主题：%s。可用主题：%s。", strings.Join(unknown, ", "), strings.Join(guide.TopicKeys(), ", ")), true
	}
	if t != nil && t.run != nil && len(titles) > 0 {
		t.run.addDoneStep("skill", "已查阅使用指南："+strings.Join(titles, "、"), "")
		t.postActivity = "正在整理操作步骤…"
	}
	if len(unknown) > 0 {
		content += fmt.Sprintf("\n\n（注：未识别的主题 %s 已忽略；可用主题：%s）", strings.Join(unknown, ", "), strings.Join(guide.TopicKeys(), ", "))
	}
	return content, true
}
