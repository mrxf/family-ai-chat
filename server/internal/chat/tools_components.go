package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

const (
	maxComponentsPerTurn    = 8
	maxComponentManageCalls = 12
	skillKeyComponents      = "ui_components"
	skillKeyAskUser         = "ask_user"
)

// ---------------- Tool Schemas: ui_components ----------------

var showWeatherTool = llm.Tool{
	Name: "show_weather",
	Description: "Render a visual weather card inline in the reply (supports a lightweight compact mode with minimal fields, or a detailed mode with multi-day forecast). " +
		"IMPORTANT: Pure display tool (no built-in weather API) — call web_search FIRST for real-time/future weather.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location":   map[string]any{"type": "string", "description": "City or district name, e.g. \"北京\", \"上海\"."},
			"condition":  map[string]any{"type": "string", "description": "Weather condition in Chinese, e.g. \"晴\", \"多云\", \"阴\", \"小雨\", \"雷阵雨\", \"小雪\"."},
			"compact":    map[string]any{"type": "boolean", "description": "true = sleek compact single-banner card; false = detailed card."},
			"date_label": map[string]any{"type": "string", "description": "Optional date or period label, e.g. \"明天 周六\" or \"今天实时\"."},
			"temp":       map[string]any{"type": "number", "description": "Optional current or representative temperature in °C."},
			"high":       map[string]any{"type": "number", "description": "Optional daily high temperature in °C."},
			"low":        map[string]any{"type": "number", "description": "Optional daily low temperature in °C."},
			"feels_like": map[string]any{"type": "number", "description": "Optional feels-like temperature in °C (omit in compact mode)."},
			"humidity":   map[string]any{"type": "string", "description": "Optional relative humidity, e.g. \"42%\" (omit in compact mode)."},
			"wind":       map[string]any{"type": "string", "description": "Optional wind info, e.g. \"北风 3级\"."},
			"aqi":        map[string]any{"type": "string", "description": "Optional air quality badge, e.g. \"优 32\" (omit in compact mode)."},
			"tip":        map[string]any{"type": "string", "description": "Optional brief clothing/umbrella note (<= 40 chars)."},
			"forecast": map[string]any{
				"type":        "array",
				"description": "Optional 2-7 day forecast rows (only pass when the user asks for multi-day or detailed weather).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"day":       map[string]any{"type": "string"},
						"condition": map[string]any{"type": "string"},
						"low":       map[string]any{"type": "number"},
						"high":      map[string]any{"type": "number"},
					},
					"required": []string{"day", "condition", "low", "high"},
				},
			},
		},
		"required": []string{"location", "condition"},
	},
}

var showTodoTool = llm.Tool{
	Name: "show_todo",
	Description: "Render an interactive checklist / todo card inline in the reply. " +
		"The user can tap items to check or uncheck them, and progress is persisted automatically.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":    map[string]any{"type": "string", "description": "Checklist title, e.g. \"住院准备物品清单\", \"周末采购清单\"."},
			"subtitle": map[string]any{"type": "string", "description": "Optional short subtitle or hint."},
			"items": map[string]any{
				"type":        "array",
				"description": "1 to 25 checklist items.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string", "description": "Actionable item title."},
						"note": map[string]any{"type": "string", "description": "Optional brief detail or quantity, e.g. \"2份复印件\"."},
						"tag":  map[string]any{"type": "string", "description": "Optional short category pill, e.g. \"证件\", \"必带\"."},
						"done": map[string]any{"type": "boolean", "description": "Initial checked state (false by default)."},
					},
					"required": []string{"text"},
				},
			},
		},
		"required": []string{"title", "items"},
	},
}

var showFeedTool = llm.Tool{
	Name: "show_feed",
	Description: "Render a structured dynamic feed / news briefing / curated list card inline in the reply. " +
		"Ideal for daily AI/tech news digests, curated updates, reading lists, or rolling summaries that can be pinned to Home and updated in place over time.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":    map[string]any{"type": "string", "description": "Feed title, e.g. \"每日大模型新闻\", \"科技早报\"."},
			"subtitle": map[string]any{"type": "string", "description": "Optional brief description, e.g. \"每天 07:00 自动滚动更新前 3 条\"."},
			"items": map[string]any{
				"type":        "array",
				"description": "1 to 20 ordered feed entries (newest/most important first).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":        map[string]any{"type": "string", "description": "Concise entry headline."},
						"summary":      map[string]any{"type": "string", "description": "Optional 1-2 sentence summary."},
						"tag":          map[string]any{"type": "string", "description": "Optional short pill tag, e.g. \"今日\", \"往期\", \"OpenAI\"."},
						"source":       map[string]any{"type": "string", "description": "Optional source name, e.g. \"机器之心\", \"TechCrunch\"."},
						"url":          map[string]any{"type": "string", "description": "Optional http(s) link for the user to open."},
						"published_at": map[string]any{"type": "string", "description": "Optional short date/time label, e.g. \"11-23\" or \"今日\"."},
					},
					"required": []string{"title"},
				},
			},
		},
		"required": []string{"title", "items"},
	},
}

var showChartTool = llm.Tool{
	Name:        "show_chart",
	Description: "Render an interactive visual chart card (line trend, bar comparison, or pie/donut breakdown) inline in the reply.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chart_type": map[string]any{"type": "string", "enum": []string{"line", "bar", "pie"}},
			"title":      map[string]any{"type": "string"},
			"subtitle":   map[string]any{"type": "string"},
			"unit":       map[string]any{"type": "string"},
			"labels":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"series": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":   map[string]any{"type": "string"},
						"values": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
					},
					"required": []string{"values"},
				},
			},
		},
		"required": []string{"chart_type", "title", "labels", "series"},
	},
}

var showQuizTool = llm.Tool{
	Name: "show_quiz",
	Description: "Render an interactive single-choice quiz / trivia / self-test card inline in the reply. " +
		"When the user taps an option, it reveals right/wrong feedback and the explanation.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question":     map[string]any{"type": "string"},
			"tag":          map[string]any{"type": "string"},
			"options":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"answer_index": map[string]any{"type": "integer"},
			"explanation":  map[string]any{"type": "string"},
		},
		"required": []string{"question", "options", "answer_index"},
	},
}

var showMetricsTool = llm.Tool{
	Name:        "show_metrics",
	Description: "Render a KPI / key-metrics dashboard card inline in the reply (great for loan/budget calculations, lab report readings, nutrition/calorie breakdowns, or quick comparisons).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":    map[string]any{"type": "string"},
			"subtitle": map[string]any{"type": "string"},
			"metrics": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":  map[string]any{"type": "string"},
						"value":  map[string]any{"type": "string"},
						"unit":   map[string]any{"type": "string"},
						"delta":  map[string]any{"type": "string"},
						"status": map[string]any{"type": "string", "enum": []string{"normal", "good", "warn", "bad"}},
					},
					"required": []string{"label", "value"},
				},
			},
			"summary": map[string]any{"type": "string"},
		},
		"required": []string{"title", "metrics"},
	},
}

var listComponentsTool = llm.Tool{
	Name: "list_components",
	Description: "List the user's pinned home-board components and recent components (with exact message_id, component_id, kind, and full current data/state). " +
		"Call this BEFORE update_component, pin_component, unpin_component, or delete_component whenever you need exact IDs or existing items.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope": map[string]any{
				"type":        "string",
				"enum":        []string{"all", "pinned", "conversation"},
				"description": "\"all\" (default): pinned home cards + current conversation + recent cards; \"pinned\": home board only; \"conversation\": current conversation only.",
			},
			"keyword": map[string]any{
				"type":        "string",
				"description": "Optional keyword filter matching title or content.",
			},
		},
	},
}

var updateComponentTool = llm.Tool{
	Name: "update_component",
	Description: "Update an existing component card IN PLACE by its message_id and component_id (changes immediately reflect both in the original chat and on the Home pinned board). " +
		"Supports partial/full field patches via `data`, AND safe atomic list operations (`prepend_items`, `append_items`, `remove_item_ids`, `max_items`, `done_ids`) for `feed` and `todo` cards — e.g. to update the top 3 news items while shifting the previous top 3 into positions 4-6 without retyping old items, pass `prepend_items` with the 3 new items and `max_items: 6`!",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message_id":   map[string]any{"type": "string", "description": "Exact message_id from list_components or quoted/task context."},
			"component_id": map[string]any{"type": "string", "description": "Exact component_id of the target card."},
			"data": map[string]any{
				"type":        "object",
				"description": "Optional partial or full replacement fields matching the component's kind (e.g. title, subtitle, items, metrics, location/condition/temp, labels/series).",
			},
			"prepend_items": map[string]any{
				"type":        "array",
				"description": "For `feed` or `todo`: new items to insert at the FRONT of the existing list, automatically shifting existing items downward.",
				"items":       map[string]any{"type": "object"},
			},
			"append_items": map[string]any{
				"type":        "array",
				"description": "For `feed` or `todo`: new items to append to the END of the existing list.",
				"items":       map[string]any{"type": "object"},
			},
			"remove_item_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "For `feed` or `todo`: item IDs to remove before/after updates.",
			},
			"max_items": map[string]any{
				"type":        "integer",
				"description": "For `feed` or `todo`: optional cap keeping only the first N items after prepend/append (e.g. 6 keeps 3 new + 3 previous).",
			},
			"done_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "For `todo`: optional explicit list of checked item IDs.",
			},
			"previous_tag": map[string]any{
				"type":        "string",
				"description": "For `feed`: optional tag to assign to shifted existing items when prepending new items (e.g. \"往期\").",
			},
		},
		"required": []string{"message_id", "component_id"},
	},
}

var pinComponentTool = llm.Tool{
	Name: "pin_component",
	Description: "Pin an existing component card to the user's Home board. " +
		"Omit both IDs to use this turn's new card, the user's quoted card, or the last pinnable card in the latest assistant message.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message_id":   map[string]any{"type": "string", "description": "Optional message_id (defaults to the latest component created in this turn)."},
			"component_id": map[string]any{"type": "string", "description": "Optional component_id (defaults to the latest component created in this turn)."},
		},
	},
}

var unpinComponentTool = llm.Tool{
	Name:        "unpin_component",
	Description: "Unpin a Home card by message_id and component_id, or by a title/summary keyword. Multiple matches require asking the user.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"keyword":      map[string]any{"type": "string", "description": "标题或摘要关键字；多匹配时先询问用户"},
			"message_id":   map[string]any{"type": "string"},
			"component_id": map[string]any{"type": "string"},
		},
	},
}

var deleteComponentTool = llm.Tool{
	Name:        "delete_component",
	Description: "Permanently delete a component card (and unpin it if pinned). Only call when the user explicitly asks to delete a card.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message_id":   map[string]any{"type": "string"},
			"component_id": map[string]any{"type": "string"},
		},
		"required": []string{"message_id", "component_id"},
	},
}

// ---------------- Tool Schema: ask_user ----------------

var askUserQuestionTool = llm.Tool{
	Name:        "ask_user_question",
	Description: "Display a native interactive multiple-choice question card in the chat so the user can tap options (or type 'Other') to clarify their preferences, constraints, or goals.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Optional short card header, e.g. \"先了解几个小偏好\" (defaults to \"请帮小暖确认一下\").",
			},
			"questions": map[string]any{
				"type":        "array",
				"description": "1 to 4 concise questions to ask the user.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question":     map[string]any{"type": "string"},
						"multi_select": map[string]any{"type": "boolean"},
						"options": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string"},
									"description": map[string]any{"type": "string"},
									"recommended": map[string]any{"type": "boolean"},
								},
								"required": []string{"label"},
							},
						},
					},
					"required": []string{"question", "options"},
				},
			},
		},
		"required": []string{"questions"},
	},
}

// ---------------- Skill Definitions ----------------

const componentsSkillSOP = `## 对话可视化卡片组件与管理规范

你拥有 6 个可视化卡片渲染工具与 5 个卡片管理工具。卡片既可穿插在回复中展示，也可固定到用户首页看板，并支持后续**原地修改更新**。

### 1. 六大卡片组件适用场景
1. **天气卡片 (` + "`show_weather`" + `)**：纯展示组件，实时/未来天气须先调 ` + "`web_search`" + ` 查证；日常简单查询优先设 ` + "`compact: true`" + `。
2. **互动清单 (` + "`show_todo`" + `)**：待办事项、采购单、打包清单、执行步骤，用户可勾选并持久化。
3. **动态资讯/列表卡片 (` + "`show_feed`" + `)**：每日新闻简报、大模型动态、关注列表、滚动资讯汇总（支持序号展示、标签、来源、可点击原链接与更新时间，最适合固定到首页并由定时任务持续更新）。
4. **数据图表 (` + "`show_chart`" + `)**：折线图 ` + "`line`" + `、柱状图 ` + "`bar`" + `、环形图 ` + "`pie`" + `。
5. **互动问答测验 (` + "`show_quiz`" + `)**：单选小测；**防剧透铁律**：正文不要写出答案或提前剧透解析。
6. **核心指标看板 (` + "`show_metrics`" + `)**：算账结果、体检关键指标、热量营养、核心参数速览。

### 2. 组件读写与原地更新基础操作
- **先查后改**：不知道目标卡片的 ` + "`message_id`" + ` / ` + "`component_id`" + ` 或现有内容时，先调用 ` + "`list_components`" + ` 获取真实 ID 与完整条目，**严禁编造 ID**。
- **原地更新 (` + "`update_component`" + `)**：当用户说“更新首页那张卡片”“在清单里加两项”“更新前三条并把原来的前三条放后三条”时，优先原地更新已有卡片，而不是重复生成一堆新卡片：
  - **滚动更新列表最佳实践（零遗漏）**：对 ` + "`feed`" + ` 或 ` + "`todo`" + ` 卡片，使用 ` + "`prepend_items`" + ` 传入新条目 + ` + "`max_items`" + ` 保留总条数（如新插 3 条并设 ` + "`max_items: 6`" + `，原前 3 条会自动顺延到第 4~6 条；` + "`feed`" + ` 还可传 ` + "`previous_tag: \"往期\"`" + ` 区分新旧），完全不需要手动重抄旧条目！
  - 修改天气、指标、图表、标题或整体替换条目时，传入 ` + "`data`" + ` 字段即可局部/整体更新。
- **固定与管理**：调用 ` + "`pin_component`" + ` 可把刚创建的或已有卡片固定到首页看板；` + "`unpin_component`" + ` 取消固定；` + "`delete_component`" + ` 仅在用户明确要求删除卡片时调用。

### 3. 图文穿插与防重复原则
- 卡片已经直观展示的数据，配套文字只给结论、变化摘要或贴心提示，不要把卡片条目逐条再抄一遍。
- 不要向用户输出内部 ` + "`message_id`" + ` 或 ` + "`component_id`" + `。`

const askUserSkillSOP = `## 原生交互式询问用户（ask_user_question）操作规范

当用户的需求存在关键分叉或缺少必要条件时，调用 ` + "`ask_user_question`" + ` 会在聊天气泡中渲染一张**原生可点选的问答卡片**，用户无需费力打字，点几下即可把偏好发回给你。

### 1. 何时应该使用
- 用户提出开放式请求，且不同偏好的方案差异极大（例如：定制旅游行程、选购家电/手机/礼物、制定食谱或运动计划、润色信件/文案的语气场合、多套技术或生活方案取舍）。
- 明确 1～3 个关键问题后，你能给出精准十倍的定制回答，而不是先盲猜一大篇泛泛而谈的套话。

### 2. 何时不要使用
- 问题本身已经足够明确（如“明天北京天气如何”“西红柿炒蛋怎么做”“氨氯地平有什么副作用”），请直接回答，不要反问。
- 用户的常驻记忆画像或上文已经告诉过你的信息（如已知家里有老人、已知忌口），不要重复询问。
- 简单的是/否确认且上下文已很清楚时，直接自然对话即可。

### 3. 提问设计最佳实践
- **少而精**：每次控制在 **1～3 个问题**（最多不超过 4 个），只问最影响决策的核心分叉。
- **好点好懂**：每题给 **2～5 个具体选项**，` + "`label`" + ` 简短清晰（2～15 字），必要时用 ` + "`description`" + ` 补充半句说明。
- **推荐项置顶**：如果有最稳妥省心的默认推荐，把它放在第一个并设置 ` + "`recommended: true`" + `。
- **不要手写“其他”**：UI 原生自带「其他（自己补充）」输入框，无需在 ` + "`options`" + ` 里再造一个“其他”选项。
- **优雅收尾等待作答**：调用 ` + "`ask_user_question`" + ` 后，**不要在文字里把题目和选项列表再复述一遍**，也不要还没等用户选就提前输出长篇大论的方案；只需配一句简短温和的引导（如“先点选一下上面的偏好，我马上为你定制～”）并停下来等待用户选择。
- **尊重跳过与换话题（极其重要）**：卡片支持用户点击「暂不选择 / ×」直接关闭，用户也完全可以不理会卡片、直接在输入框自己聊别的。一旦用户在后续消息里跳过了卡片或聊起别的话题，**必须立即顺着用户的新意思自然回应，严禁追问或强迫用户补填刚才卡片里的选项！**`

func (s *Service) buildComponentsSkill() skills.Definition {
	return skills.Definition{
		Key:          skillKeyComponents,
		Title:        "对话可视化卡片组件",
		Description:  "创建与原地管理可视化卡片：天气卡、互动待办清单、动态资讯/新闻列表卡、折线/柱状/环形图表、互动答题卡与指标看板，支持查询、原地更新内容、滚动更新前几条、固定到首页及取消固定/删除。",
		WhenToUse:    "当需要展示天气、待办清单(Todo)、新闻/资讯/动态列表(Feed)、图表(Chart)、测验(Quiz)、指标(Metrics)卡片，或者需要查看、原地修改/更新、固定/取消固定/删除已有组件卡片时",
		LeaseSummary: "「对话可视化卡片组件 (ui_components)」已解锁，支持展示卡片及调用 list_components / update_component / pin_component / unpin_component / delete_component 管理已有卡片。",
		ToolHints: []skills.ToolHint{
			{Name: showWeatherTool.Name, Brief: "插入天气预报卡片（需先用 web_search 查真实气象数据再填参展示）"},
			{Name: showTodoTool.Name, Brief: "插入用户可直接点击打勾保存进度的互动待办/物品/步骤清单卡片"},
			{Name: showFeedTool.Name, Brief: "插入新闻简报/动态资讯/有序列表卡片（适合固定到首页并定期滚动更新）"},
			{Name: showChartTool.Name, Brief: "插入交互式可视化图表卡片（支持折线图 line、柱状图 bar、环形占比图 pie）"},
			{Name: showQuizTool.Name, Brief: "插入点击作答即揭晓对错与解析的互动问答/自测卡片"},
			{Name: showMetricsTool.Name, Brief: "插入大字号关键指标看板卡片（算账月供、体检指标状态、热量营养速览）"},
			{Name: listComponentsTool.Name, Brief: "查询首页已固定或近期卡片及其精确 message_id / component_id 与现有条目"},
			{Name: updateComponentTool.Name, Brief: "原地更新已有卡片内容（支持局部补丁及列表 prepend_items + max_items 滚动更新）"},
			{Name: pinComponentTool.Name, Brief: "将刚创建或指定的卡片固定到首页看板"},
			{Name: unpinComponentTool.Name, Brief: "将指定卡片从首页看板取消固定"},
			{Name: deleteComponentTool.Name, Brief: "删除指定卡片（仅在用户明确要求删除时使用）"},
		},
		Tools: []llm.Tool{
			showWeatherTool,
			showTodoTool,
			showFeedTool,
			showChartTool,
			showQuizTool,
			showMetricsTool,
			listComponentsTool,
			updateComponentTool,
			pinComponentTool,
			unpinComponentTool,
			deleteComponentTool,
		},
		Content: componentsSkillSOP,
		Available: func(ctx skills.Context) bool {
			return !ctx.Private
		},
	}
}

func (s *Service) buildAskUserSkill() skills.Definition {
	return skills.Definition{
		Key:          skillKeyAskUser,
		Title:        "交互式询问用户偏好",
		Description:  "向用户弹出原生可点选的单选/多选问题卡片以澄清模糊需求或收集偏好。",
		WhenToUse:    "当用户的请求开放模糊、缺少关键前提或有多种路线分叉（如出游定制、礼物选购、饮食忌口、方案取舍），适合弹出可选项卡片让用户点选而非盲目长篇猜测时",
		LeaseSummary: "「交互式询问用户 (ask_user)」工具当前已解锁；若用户刚刚已通过卡片确认了偏好，请直接根据其选择给出定制结果，不要无故反复追问。",
		ToolHints: []skills.ToolHint{
			{Name: askUserQuestionTool.Name, Brief: "展示原生可点选问题卡片（支持 1-4 题、单选/多选、推荐标记与自由补充输入）"},
		},
		Tools:     []llm.Tool{askUserQuestionTool},
		Content:   askUserSkillSOP,
		Available: func(ctx skills.Context) bool { return !ctx.Private },
	}
}

// ---------------- Normalized Payload Structs ----------------

type weatherForecastDay struct {
	Day       string  `json:"day"`
	Condition string  `json:"condition"`
	Low       float64 `json:"low"`
	High      float64 `json:"high"`
}

type weatherCardData struct {
	Location  string               `json:"location"`
	DateLabel string               `json:"date_label,omitempty"`
	Condition string               `json:"condition"`
	Compact   bool                 `json:"compact,omitempty"`
	Temp      *float64             `json:"temp,omitempty"`
	High      *float64             `json:"high,omitempty"`
	Low       *float64             `json:"low,omitempty"`
	FeelsLike *float64             `json:"feels_like,omitempty"`
	Humidity  string               `json:"humidity,omitempty"`
	Wind      string               `json:"wind,omitempty"`
	AQI       string               `json:"aqi,omitempty"`
	Tip       string               `json:"tip,omitempty"`
	Forecast  []weatherForecastDay `json:"forecast,omitempty"`
	UpdatedAt int64                `json:"updated_at,omitempty"`
}

type todoCardItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Note string `json:"note,omitempty"`
	Tag  string `json:"tag,omitempty"`
	Done bool   `json:"done,omitempty"`
}

type todoCardData struct {
	Title     string         `json:"title"`
	Subtitle  string         `json:"subtitle,omitempty"`
	Items     []todoCardItem `json:"items"`
	UpdatedAt int64          `json:"updated_at,omitempty"`
}

type todoCardState struct {
	DoneIDs []string `json:"done_ids"`
}

type feedCardItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Source      string `json:"source,omitempty"`
	URL         string `json:"url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type feedCardData struct {
	Title     string         `json:"title"`
	Subtitle  string         `json:"subtitle,omitempty"`
	Items     []feedCardItem `json:"items"`
	UpdatedAt int64          `json:"updated_at,omitempty"`
}

type chartSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

type chartCardData struct {
	ChartType string        `json:"chart_type"` // "line" | "bar" | "pie"
	Title     string        `json:"title"`
	Subtitle  string        `json:"subtitle,omitempty"`
	Unit      string        `json:"unit,omitempty"`
	Labels    []string      `json:"labels"`
	Series    []chartSeries `json:"series"`
	UpdatedAt int64         `json:"updated_at,omitempty"`
}

type quizCardData struct {
	Question    string   `json:"question"`
	Tag         string   `json:"tag,omitempty"`
	Options     []string `json:"options"`
	AnswerIndex int      `json:"answer_index"`
	Explanation string   `json:"explanation,omitempty"`
	UpdatedAt   int64    `json:"updated_at,omitempty"`
}

type quizCardState struct {
	SelectedIndex int `json:"selected_index"`
}

type metricTile struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Unit   string `json:"unit,omitempty"`
	Delta  string `json:"delta,omitempty"`
	Status string `json:"status,omitempty"` // "normal" | "good" | "warn" | "bad"
}

type metricsCardData struct {
	Title     string       `json:"title"`
	Subtitle  string       `json:"subtitle,omitempty"`
	Metrics   []metricTile `json:"metrics"`
	Summary   string       `json:"summary,omitempty"`
	UpdatedAt int64        `json:"updated_at,omitempty"`
}

type askUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

type askUserQuestionItem struct {
	ID          string          `json:"id"`
	Question    string          `json:"question"`
	MultiSelect bool            `json:"multi_select,omitempty"`
	Options     []askUserOption `json:"options"`
}

type askUserCardData struct {
	Title     string                `json:"title,omitempty"`
	Questions []askUserQuestionItem `json:"questions"`
}

type askUserAnswerEntry struct {
	Selected []string `json:"selected,omitempty"`
	Custom   string   `json:"custom,omitempty"`
}

type askUserCardState struct {
	Submitted bool                          `json:"submitted"`
	Dismissed bool                          `json:"dismissed,omitempty"`
	Answers   map[string]askUserAnswerEntry `json:"answers,omitempty"`
}

// ---------------- Tool Execution Dispatcher ----------------

// execComponentOrAskTool handles ui_components tools and ask_user_question.
// Returns (toolResult, handled, askedUser).
func (s *Service) execComponentOrAskTool(ctx context.Context, t *toolTurn, name, rawArgs string) (string, bool, bool) {
	switch name {
	case showWeatherTool.Name:
		return s.execShowWeather(t, rawArgs), true, false
	case showTodoTool.Name:
		return s.execShowTodo(t, rawArgs), true, false
	case showFeedTool.Name:
		return s.execShowFeed(t, rawArgs), true, false
	case showChartTool.Name:
		return s.execShowChart(t, rawArgs), true, false
	case showQuizTool.Name:
		return s.execShowQuiz(t, rawArgs), true, false
	case showMetricsTool.Name:
		return s.execShowMetrics(t, rawArgs), true, false
	case listComponentsTool.Name:
		return s.execListComponents(ctx, t, rawArgs), true, false
	case updateComponentTool.Name:
		return s.execUpdateComponent(ctx, t, rawArgs), true, false
	case pinComponentTool.Name:
		return s.execPinComponent(ctx, t, rawArgs), true, false
	case unpinComponentTool.Name:
		return s.execUnpinComponent(ctx, t, rawArgs), true, false
	case deleteComponentTool.Name:
		return s.execDeleteComponent(ctx, t, rawArgs), true, false
	case askUserQuestionTool.Name:
		out, ok := s.execAskUserQuestion(t, rawArgs)
		return out, true, ok
	default:
		return "", false, false
	}
}

func (s *Service) checkComponentLimit(t *toolTurn) string {
	if t.private {
		return "当前是隐私对话模式，暂不启用扩展组件工具。"
	}
	if t.componentCalls >= maxComponentsPerTurn {
		return "本轮回复插入的交互组件数量已达上限，请直接用精炼文字完成剩余回复。"
	}
	return ""
}

func (s *Service) appendAndPersistComponent(t *toolTurn, kind string, data any, state any) (string, error) {
	rawData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	var rawState json.RawMessage
	if state != nil {
		rawState, _ = json.Marshal(state)
	}
	t.componentCalls++
	compID := fmt.Sprintf("comp_%d_%s", t.componentCalls, store.NewID()[:8])
	comp := store.MessageComponent{
		ID:    compID,
		Kind:  kind,
		Pos:   -1, // stamped atomically inside Run.addComponent against current content rune length
		Data:  rawData,
		State: rawState,
	}
	all := t.run.addComponent(comp)
	pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Store.SetComponents(pctx, t.asstID, all); err != nil {
		s.Log.Error("save message components", "message_id", t.asstID, "err", err)
	}
	if kind != "ask_user" && kind != "app_action" {
		t.lastCreatedMsgID = t.asstID
		t.lastCreatedCompID = compID
	}
	return compID, nil
}
