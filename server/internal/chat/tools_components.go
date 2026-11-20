package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

const (
	maxComponentsPerTurn = 8

	skillKeyComponents = "ui_components"
	skillKeyAskUser    = "ask_user"
)

// ---------------- Tool Schemas: ui_components ----------------

var showWeatherTool = llm.Tool{
	Name: "show_weather",
	Description: "Render a visual weather card inline in the reply (supports a lightweight compact mode with minimal fields, or a detailed mode with multi-day forecast). " +
		"IMPORTANT: Pure display tool (no built-in weather API) — call web_search FIRST for real-time/future weather.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "City or district name, e.g. \"北京\", \"上海\".",
			},
			"condition": map[string]any{
				"type":        "string",
				"description": "Weather condition in Chinese, e.g. \"晴\", \"多云\", \"阴\", \"小雨\", \"雷阵雨\", \"小雪\".",
			},
			"compact": map[string]any{
				"type":        "boolean",
				"description": "true = render a sleek compact single-banner weather card (recommended when only basic temp/condition is needed without extra metrics); false = detailed card.",
			},
			"date_label": map[string]any{
				"type":        "string",
				"description": "Optional date or period label, e.g. \"明天 周六\" or \"今天实时\".",
			},
			"temp": map[string]any{
				"type":        "number",
				"description": "Optional current or representative temperature in °C.",
			},
			"high": map[string]any{
				"type":        "number",
				"description": "Optional daily high temperature in °C.",
			},
			"low": map[string]any{
				"type":        "number",
				"description": "Optional daily low temperature in °C.",
			},
			"feels_like": map[string]any{
				"type":        "number",
				"description": "Optional feels-like temperature in °C (omit in compact mode).",
			},
			"humidity": map[string]any{
				"type":        "string",
				"description": "Optional relative humidity, e.g. \"42%\" (omit in compact mode).",
			},
			"wind": map[string]any{
				"type":        "string",
				"description": "Optional wind info, e.g. \"北风 3级\".",
			},
			"aqi": map[string]any{
				"type":        "string",
				"description": "Optional air quality badge, e.g. \"优 32\" (omit in compact mode).",
			},
			"tip": map[string]any{
				"type":        "string",
				"description": "Optional brief clothing/umbrella note (<= 40 chars).",
			},
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
			"title": map[string]any{
				"type":        "string",
				"description": "Checklist title, e.g. \"住院准备物品清单\", \"周末采购清单\".",
			},
			"subtitle": map[string]any{
				"type":        "string",
				"description": "Optional short subtitle or hint.",
			},
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

var showChartTool = llm.Tool{
	Name: "show_chart",
	Description: "Render an interactive visual chart card (line trend, bar comparison, or pie/donut breakdown) inline in the reply.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chart_type": map[string]any{
				"type":        "string",
				"enum":        []string{"line", "bar", "pie"},
				"description": "\"line\" for trends over time, \"bar\" for category comparisons, \"pie\" for proportion/share breakdown.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Chart title, e.g. \"近 7 天最高气温走势\", \"家庭月度开销占比\".",
			},
			"subtitle": map[string]any{
				"type":        "string",
				"description": "Optional subtitle or timeframe.",
			},
			"unit": map[string]any{
				"type":        "string",
				"description": "Optional value suffix unit, e.g. \"℃\", \"元\", \"%\", \"kcal\".",
			},
			"labels": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Category or X-axis labels (2 to 14 labels), e.g. [\"周一\", \"周二\", \"周三\"].",
			},
			"series": map[string]any{
				"type":        "array",
				"description": "1 to 3 data series aligned with labels (for pie charts, pass 1 series whose values match labels).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":   map[string]any{"type": "string", "description": "Series legend name, e.g. \"最高温\"."},
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
			"question": map[string]any{
				"type":        "string",
				"description": "The quiz question stem.",
			},
			"tag": map[string]any{
				"type":        "string",
				"description": "Optional badge label, e.g. \"防骗小测\", \"健康常识\", \"趣味诗词\".",
			},
			"options": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "2 to 5 plain option texts (without leading 'A.' / 'B.' prefixes).",
			},
			"answer_index": map[string]any{
				"type":        "integer",
				"description": "0-based index of the correct option in options (0 = first option).",
			},
			"explanation": map[string]any{
				"type":        "string",
				"description": "Warm, clear explanation shown after the user picks an option.",
			},
		},
		"required": []string{"question", "options", "answer_index"},
	},
}

var showMetricsTool = llm.Tool{
	Name: "show_metrics",
	Description: "Render a KPI / key-metrics dashboard card inline in the reply (great for loan/budget calculations, lab report readings, nutrition/calorie breakdowns, or quick comparisons).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Card title, e.g. \"等额本息房贷测算\", \"体检关键指标速览\".",
			},
			"subtitle": map[string]any{
				"type":        "string",
				"description": "Optional context line, e.g. \"商贷 100 万 · 30 年 · 利率 3.15%\".",
			},
			"metrics": map[string]any{
				"type":        "array",
				"description": "2 to 6 key metric tiles.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":  map[string]any{"type": "string", "description": "Metric name, e.g. \"每月月供\", \"空腹血糖\"."},
						"value":  map[string]any{"type": "string", "description": "Prominent value string, e.g. \"4,297\", \"6.4\"."},
						"unit":   map[string]any{"type": "string", "description": "Optional unit, e.g. \"元\", \"mmol/L\", \"kcal\"."},
						"delta":  map[string]any{"type": "string", "description": "Optional reference range or change note, e.g. \"参考 3.9-6.1\"."},
						"status": map[string]any{"type": "string", "enum": []string{"normal", "good", "warn", "bad"}, "description": "Visual status badge color."},
					},
					"required": []string{"label", "value"},
				},
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Optional 1-sentence takeaway at the bottom of the card.",
			},
		},
		"required": []string{"title", "metrics"},
	},
}

// ---------------- Tool Schema: ask_user ----------------

var askUserQuestionTool = llm.Tool{
	Name: "ask_user_question",
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
						"question": map[string]any{
							"type":        "string",
							"description": "Clear, concise question text (do NOT append '可多选', the UI shows a badge automatically).",
						},
						"multi_select": map[string]any{
							"type":        "boolean",
							"description": "true if the user may select multiple options; false (default) for single choice.",
						},
						"options": map[string]any{
							"type":        "array",
							"description": "2 to 5 concrete choices (do NOT include an 'Other' option; the UI automatically provides custom input). Put the recommended choice first with recommended=true when applicable.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string", "description": "Short option label (2-18 chars)."},
									"description": map[string]any{"type": "string", "description": "Optional 1-line helper description."},
									"recommended": map[string]any{"type": "boolean", "description": "Set true on the top recommended option if there is a sensible default."},
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

const componentsSkillSOP = `## 对话可视化卡片组件操作规范

你拥有 5 个精美的对话内联可视化组件工具，它们会**严格按你调用工具时的位置穿插渲染在回复正文中**，让信息一目了然、告别大段枯燥纯文字：

### 1. 五大组件适用场景与要点
1. **天气卡片 (` + "`show_weather`" + `)**
   - **重要前提**：` + "`show_weather`" + ` 是纯展示组件，**不自带天气数据源**。当用户询问实时或未来天气且上下文中没有确切气象数据时，**必须先调用 ` + "`web_search`" + ` 查询真实天气**，再将查到的数据填入 ` + "`show_weather`" + `，严禁凭空编造气温！
   - **优先用精简版（` + "`compact: true`" + `），切勿凑数据**：日常顺带查某一天天气、问冷暖或带不带伞时，**只需传 ` + "`location`" + `、` + "`condition`" + `、温度（` + "`temp`" + ` 或 ` + "`low`" + `/` + "`high`" + ` 任意已知项）及可选 ` + "`date_label`" + `/` + "`tip`" + `，并设 ` + "`compact: true`" + `** 即可渲染精致小巧的天气横幅卡，完全不需要去凑湿度、体感、空气质量或多日预报！只有当用户明确想看详细气象或未来多天趋势时才传 ` + "`forecast`" + ` 等完整字段。
2. **互动清单 (` + "`show_todo`" + `)**
   - 适合出门打包行李、超市采购单、办事准备材料、做菜备料、阶段执行清单等。
   - 用户可在手机气泡里直接点击打勾，勾选进度会自动保存，下一轮对话你也能看到用户勾选了哪些项。
   - 注意区分：若用户明确要求**到点响铃提醒**，请使用 ` + "`schedule`" + ` 技能；若是一份供核对打勾的事项清单，用 ` + "`show_todo`" + `。
3. **数据图表 (` + "`show_chart`" + `)**
   - 有 ≥3 个数据点要看变化走势用 ` + "`line`" + `（折线图），对比多项数值高低用 ` + "`bar`" + `（柱状图），展示预算/营养/构成占比用 ` + "`pie`" + `（环形图）。
   - ` + "`labels`" + ` 的长度应与每组 ` + "`series[i].values`" + ` 一一对应，记得填写 ` + "`unit`" + `（如 ` + "`℃`" + `、` + "`元`" + `、` + "`%`" + `）。
4. **互动问答测验 (` + "`show_quiz`" + `)**
   - 适合陪孩子出题练习、老年人防诈骗自测、生活/健康常识问答、趣味猜谜。
   - **防剧透铁律**：调用 ` + "`show_quiz`" + ` 后，正文中**绝对不要写出正确答案或提前剧透解析**！把答案和原理解析填在 ` + "`answer_index`" + ` 和 ` + "`explanation`" + ` 里，让用户亲自点选揭晓。
5. **核心指标看板 (` + "`show_metrics`" + `)**
   - 适合房贷/理财/AA算账结果、体检化验单关键指标解读、食物热量与三大营养素估算、核心参数速览。
   - 用 ` + "`status`" + ` 标注状态色：正常/中性用 ` + "`normal`" + `，达标/利好/健康用 ` + "`good`" + `，偏高偏低需留意用 ` + "`warn`" + `，明显异常/超支风险用 ` + "`bad`" + `。

### 2. 图文穿插与防重复原则（非常关键）
- **自然穿插**：你可以先输出一句简短亲切的引入语，再调用组件工具，工具执行完后再补充一两句贴心建议；或者直接先调用组件工具把卡片置顶，随后做简短解读。
- **严禁文字复述卡片内容**：卡片已经直观展示了数字、清单条目或图表，你的配套文字只负责**给结论、给温暖叮嘱或洞察**，不要把卡片里每一天的温度、每一条清单或每一个数字再用 Markdown 列表或表格抄一遍！
- 单次回复最多插入 1～3 张最契合的卡片即可，克制使用、恰到好处。`

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
		Description:  "在对话回复中内联渲染精美的天气卡、可打勾待办清单、折线/柱状/环形图表、互动答题卡与关键指标看板。",
		WhenToUse:    "当回复包含天气预报、待办/采购/步骤清单(Todo)、多组趋势/对比/占比数据(Chart)、趣味出题/常识自测(Quiz)、或算账/体检/热量等关键指标(Metrics)，适合用可视化卡片代替纯文字时",
		LeaseSummary: "「对话可视化卡片组件 (ui_components)」工具当前已解锁，遇到天气、清单、图表、测验或关键指标场景可直接穿插调用，切勿用文字重复罗列卡片已有数据。",
		ToolHints: []skills.ToolHint{
			{Name: showWeatherTool.Name, Brief: "插入天气预报卡片（需先用 web_search 查真实气象数据再填参展示）"},
			{Name: showTodoTool.Name, Brief: "插入用户可直接点击打勾保存进度的互动待办/物品/步骤清单卡片"},
			{Name: showChartTool.Name, Brief: "插入交互式可视化图表卡片（支持折线图 line、柱状图 bar、环形占比图 pie）"},
			{Name: showQuizTool.Name, Brief: "插入点击作答即揭晓对错与解析的互动问答/自测卡片"},
			{Name: showMetricsTool.Name, Brief: "插入大字号关键指标看板卡片（算账月供、体检指标状态、热量营养速览）"},
		},
		Tools: []llm.Tool{
			showWeatherTool,
			showTodoTool,
			showChartTool,
			showQuizTool,
			showMetricsTool,
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
}

type todoCardItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Note string `json:"note,omitempty"`
	Tag  string `json:"tag,omitempty"`
	Done bool   `json:"done,omitempty"`
}

type todoCardData struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle,omitempty"`
	Items    []todoCardItem `json:"items"`
}

type todoCardState struct {
	DoneIDs []string `json:"done_ids"`
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
}

type quizCardData struct {
	Question    string   `json:"question"`
	Tag         string   `json:"tag,omitempty"`
	Options     []string `json:"options"`
	AnswerIndex int      `json:"answer_index"`
	Explanation string   `json:"explanation,omitempty"`
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
	Title    string       `json:"title"`
	Subtitle string       `json:"subtitle,omitempty"`
	Metrics  []metricTile `json:"metrics"`
	Summary  string       `json:"summary,omitempty"`
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

// ---------------- Tool Execution ----------------

// execComponentOrAskTool handles the 5 ui_components tools and ask_user_question.
// Returns (toolResult, handled, askedUser).
func (s *Service) execComponentOrAskTool(_ context.Context, t *toolTurn, name, rawArgs string) (string, bool, bool) {
	switch name {
	case showWeatherTool.Name:
		return s.execShowWeather(t, rawArgs), true, false
	case showTodoTool.Name:
		return s.execShowTodo(t, rawArgs), true, false
	case showChartTool.Name:
		return s.execShowChart(t, rawArgs), true, false
	case showQuizTool.Name:
		return s.execShowQuiz(t, rawArgs), true, false
	case showMetricsTool.Name:
		return s.execShowMetrics(t, rawArgs), true, false
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

func (s *Service) appendAndPersistComponent(t *toolTurn, kind string, data any, state any) error {
	rawData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var rawState json.RawMessage
	if state != nil {
		rawState, _ = json.Marshal(state)
	}
	t.componentCalls++
	comp := store.MessageComponent{
		ID:    fmt.Sprintf("comp_%d_%s", t.componentCalls, store.NewID()[:8]),
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
	return nil
}

func (s *Service) execShowWeather(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（至少包含 location 与 condition）。"
	}
	loc := clipRunes(strVal(raw["location"], raw["city"]), 40)
	cond := clipRunes(strVal(raw["condition"], raw["weather"]), 24)
	if loc == "" || cond == "" {
		return "参数不完整：show_weather 至少需要提供 location（城市）与 condition（天气现象）。若尚未查询真实天气，请先调用 web_search。"
	}
	high, okH := numVal(raw["high"], raw["max_temp"], raw["temp_high"])
	low, okL := numVal(raw["low"], raw["min_temp"], raw["temp_low"])
	if okH && okL && low > high {
		low, high = high, low
	}
	card := weatherCardData{
		Location:  loc,
		DateLabel: clipRunes(strVal(raw["date_label"], raw["date"]), 36),
		Condition: cond,
		Humidity:  clipRunes(strVal(raw["humidity"]), 20),
		Wind:      clipRunes(strVal(raw["wind"]), 28),
		AQI:       clipRunes(strVal(raw["aqi"], raw["air_quality"]), 24),
		Tip:       clipRunes(strVal(raw["tip"], raw["advice"]), 80),
	}
	if okH {
		rh := round1(high)
		card.High = &rh
	}
	if okL {
		rl := round1(low)
		card.Low = &rl
	}
	if v, ok := numVal(raw["temp"], raw["temperature"], raw["current_temp"]); ok {
		rv := round1(v)
		card.Temp = &rv
	}
	if v, ok := numVal(raw["feels_like"], raw["feelsLike"]); ok {
		rv := round1(v)
		card.FeelsLike = &rv
	}
	if fcRaw, ok := raw["forecast"].([]any); ok {
		for _, item := range fcRaw {
			if len(card.Forecast) >= 7 {
				break
			}
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			day := clipRunes(strVal(m["day"], m["date"]), 16)
			fcCond := clipRunes(strVal(m["condition"], m["weather"]), 16)
			fLow, okFL := numVal(m["low"], m["min"])
			fHigh, okFH := numVal(m["high"], m["max"])
			if day == "" || fcCond == "" || (!okFL && !okFH) {
				continue
			}
			if !okFL {
				fLow = fHigh
			}
			if !okFH {
				fHigh = fLow
			}
			if fLow > fHigh {
				fLow, fHigh = fHigh, fLow
			}
			card.Forecast = append(card.Forecast, weatherForecastDay{
				Day:       day,
				Condition: fcCond,
				Low:       round1(fLow),
				High:      round1(fHigh),
			})
		}
	}
	if explicitCompact, hasExplicit := raw["compact"].(bool); hasExplicit {
		card.Compact = explicitCompact && len(card.Forecast) == 0
	} else {
		extraCount := 0
		if card.FeelsLike != nil {
			extraCount++
		}
		if card.Humidity != "" {
			extraCount++
		}
		if card.Wind != "" {
			extraCount++
		}
		if card.AQI != "" {
			extraCount++
		}
		card.Compact = len(card.Forecast) == 0 && extraCount <= 1
	}
	if err := s.appendAndPersistComponent(t, "weather", card, nil); err != nil {
		return "渲染天气卡片失败：" + err.Error()
	}
	tempBrief := card.Condition
	switch {
	case card.Low != nil && card.High != nil:
		tempBrief = fmt.Sprintf("%s %.0f°~%.0f°", card.Condition, *card.Low, *card.High)
	case card.Temp != nil:
		tempBrief = fmt.Sprintf("%s %.0f°", card.Condition, *card.Temp)
	case card.High != nil:
		tempBrief = fmt.Sprintf("%s %.0f°", card.Condition, *card.High)
	case card.Low != nil:
		tempBrief = fmt.Sprintf("%s %.0f°", card.Condition, *card.Low)
	}
	t.run.addDoneStep("component", "已生成天气卡片："+card.Location, tempBrief)
	t.postActivity = "正在补充出行贴心提示…"
	return fmt.Sprintf("天气卡片已在当前文字位置向用户展示（%s %s）。请用一两句温暖简短的话补充穿衣/带伞或出行提醒即可，不要再用文字重复罗列卡片里已有的温度和预报列表。",
		card.Location, tempBrief)
}

func (s *Service) execShowTodo(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 title 与 items）。"
	}
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "待办清单"
	}
	itemsRaw, _ := raw["items"].([]any)
	var items []todoCardItem
	var doneIDs []string
	for _, elem := range itemsRaw {
		if len(items) >= 30 {
			break
		}
		id := fmt.Sprintf("i%d", len(items)+1)
		switch v := elem.(type) {
		case string:
			txt := clipRunes(strings.TrimSpace(v), 120)
			if txt != "" {
				items = append(items, todoCardItem{ID: id, Text: txt})
			}
		case map[string]any:
			txt := clipRunes(strVal(v["text"], v["title"], v["label"], v["item"]), 120)
			if txt == "" {
				continue
			}
			done, _ := v["done"].(bool)
			if !done {
				done, _ = v["checked"].(bool)
			}
			it := todoCardItem{
				ID:   id,
				Text: txt,
				Note: clipRunes(strVal(v["note"], v["detail"], v["description"]), 80),
				Tag:  clipRunes(strVal(v["tag"], v["category"]), 16),
				Done: done,
			}
			items = append(items, it)
			if done {
				doneIDs = append(doneIDs, id)
			}
		}
	}
	if len(items) == 0 {
		return "参数不完整：show_todo 的 items 至少需要包含 1 条有效待办项。"
	}
	card := todoCardData{
		Title:    title,
		Subtitle: clipRunes(strVal(raw["subtitle"], raw["description"]), 60),
		Items:    items,
	}
	if doneIDs == nil {
		doneIDs = []string{}
	}
	if err := s.appendAndPersistComponent(t, "todo", card, todoCardState{DoneIDs: doneIDs}); err != nil {
		return "渲染互动清单失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成互动清单："+card.Title, fmt.Sprintf("共 %d 项，可点击打勾", len(items)))
	t.postActivity = "正在补充说明…"
	return fmt.Sprintf("互动清单卡片「%s」（共 %d 项）已在当前位置展示给用户，用户可直接点击打勾保存进度。后续文字请只做简短重点提醒，不要再把清单条目逐条复述一遍。", card.Title, len(items))
}

func (s *Service) execShowChart(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 chart_type, title, labels, series）。"
	}
	ct := strings.ToLower(strings.TrimSpace(strVal(raw["chart_type"], raw["type"])))
	switch ct {
	case "line":
		ct = "line"
	case "pie", "donut", "doughnut", "ring":
		ct = "pie"
	default:
		ct = "bar"
	}
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "数据图表"
	}
	var labels []string
	if lRaw, ok := raw["labels"].([]any); ok {
		for _, v := range lRaw {
			if len(labels) >= 16 {
				break
			}
			labels = append(labels, clipRunes(fmt.Sprint(v), 20))
		}
	}
	var series []chartSeries
	if sRaw, ok := raw["series"].([]any); ok {
		for idx, elem := range sRaw {
			if len(series) >= 3 {
				break
			}
			m, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			vals := floatSlice(m["values"], m["data"])
			if len(vals) == 0 {
				continue
			}
			name := clipRunes(strVal(m["name"], m["label"]), 24)
			if name == "" {
				name = fmt.Sprintf("系列 %d", idx+1)
			}
			series = append(series, chartSeries{Name: name, Values: vals})
		}
	}
	// Fallback 1: top-level "values": [1, 2, 3]
	if len(series) == 0 {
		if vals := floatSlice(raw["values"], raw["data"]); len(vals) > 0 {
			series = append(series, chartSeries{Name: title, Values: vals})
		}
	}
	// Fallback 2: "items" / "slices": [{"label": "餐饮", "value": 1200}, ...]
	if len(series) == 0 || len(labels) == 0 {
		for _, k := range []string{"items", "slices", "points"} {
			arr, ok := raw[k].([]any)
			if !ok || len(arr) == 0 {
				continue
			}
			var fbLabels []string
			var fbVals []float64
			for _, elem := range arr {
				if len(fbLabels) >= 16 {
					break
				}
				m, ok := elem.(map[string]any)
				if !ok {
					continue
				}
				lbl := clipRunes(strVal(m["label"], m["name"], m["category"], m["x"]), 20)
				val, okV := numVal(m["value"], m["y"], m["amount"], m["count"])
				if lbl != "" && okV {
					fbLabels = append(fbLabels, lbl)
					fbVals = append(fbVals, round2(val))
				}
			}
			if len(fbLabels) > 0 {
				labels = fbLabels
				series = []chartSeries{{Name: title, Values: fbVals}}
				break
			}
		}
	}
	if len(series) == 0 || len(series[0].Values) == 0 {
		return "参数不完整：show_chart 需要提供至少一组有效数值（labels 与 series）。"
	}
	// Align label count and series lengths safely.
	n := len(labels)
	if n == 0 {
		n = min(len(series[0].Values), 16)
		for i := 0; i < n; i++ {
			labels = append(labels, fmt.Sprintf("#%d", i+1))
		}
	}
	for i := range series {
		if len(series[i].Values) > n {
			series[i].Values = series[i].Values[:n]
		} else {
			for len(series[i].Values) < n {
				series[i].Values = append(series[i].Values, 0)
			}
		}
	}
	if ct == "pie" && len(series) > 1 {
		series = series[:1]
	}
	card := chartCardData{
		ChartType: ct,
		Title:     title,
		Subtitle:  clipRunes(strVal(raw["subtitle"]), 60),
		Unit:      clipRunes(strVal(raw["unit"]), 12),
		Labels:    labels,
		Series:    series,
	}
	if err := s.appendAndPersistComponent(t, "chart", card, nil); err != nil {
		return "渲染图表卡片失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成数据图表："+card.Title, fmt.Sprintf("%s · %d 个数据项", ct, len(labels)))
	t.postActivity = "正在提炼核心洞察…"
	return fmt.Sprintf("数据图表卡片「%s」（类型：%s，共 %d 个维度）已在当前位置渲染给用户，支持点击查看精确数值。请只用一两句话点出关键结论或趋势洞察，不要把图表里的数字逐一抄写一遍。", card.Title, ct, len(labels))
}

func (s *Service) execShowQuiz(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 question, options, answer_index）。"
	}
	q := clipRunes(strVal(raw["question"], raw["title"]), 240)
	var opts []string
	if oRaw, ok := raw["options"].([]any); ok {
		for _, elem := range oRaw {
			if len(opts) >= 6 {
				break
			}
			var txt string
			switch v := elem.(type) {
			case string:
				txt = v
			case map[string]any:
				txt = strVal(v["text"], v["label"], v["option"])
			}
			txt = stripOptionPrefix(strings.TrimSpace(txt))
			if txt != "" {
				opts = append(opts, clipRunes(txt, 100))
			}
		}
	}
	if q == "" || len(opts) < 2 {
		return "参数不完整：show_quiz 需要 question 以及至少 2 个有效 options。"
	}
	ansIdx := 0
	if v, ok := numVal(raw["answer_index"], raw["answerIndex"], raw["correct_index"]); ok {
		ansIdx = int(math.Round(v))
	} else if sAns := strings.ToUpper(strings.TrimSpace(strVal(raw["answer"]))); len(sAns) == 1 && sAns[0] >= 'A' && sAns[0] <= 'F' {
		ansIdx = int(sAns[0] - 'A')
	}
	if ansIdx == len(opts) && len(opts) >= 2 {
		// Tolerate 1-based answer_index when equal to len(opts).
		ansIdx = len(opts) - 1
	}
	if ansIdx < 0 || ansIdx >= len(opts) {
		ansIdx = 0
	}
	card := quizCardData{
		Question:    q,
		Tag:         clipRunes(strVal(raw["tag"], raw["category"]), 20),
		Options:     opts,
		AnswerIndex: ansIdx,
		Explanation: clipRunes(strVal(raw["explanation"], raw["reason"], raw["analysis"]), 400),
	}
	if err := s.appendAndPersistComponent(t, "quiz", card, quizCardState{SelectedIndex: -1}); err != nil {
		return "渲染互动测验卡片失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成互动问答卡片", clipRunes(q, 24))
	t.postActivity = "正在邀请用户作答…"
	return "互动问答测验卡片已在当前位置展示给用户。切记：正文中绝对不要写出正确答案或提前剧透解析！只需简短鼓励用户点选卡片试试看即可。"
}

func (s *Service) execShowMetrics(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 title 与 metrics）。"
	}
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "核心指标速览"
	}
	mRaw, _ := raw["metrics"].([]any)
	if len(mRaw) == 0 {
		mRaw, _ = raw["items"].([]any)
	}
	var tiles []metricTile
	for _, elem := range mRaw {
		if len(tiles) >= 8 {
			break
		}
		m, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		lbl := clipRunes(strVal(m["label"], m["name"], m["title"]), 28)
		val := clipRunes(anyToString(m["value"]), 32)
		if lbl == "" || val == "" {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(strVal(m["status"], m["level"])))
		switch st {
		case "good", "ok", "healthy", "positive", "low_risk":
			st = "good"
		case "warn", "warning", "caution", "high", "low":
			st = "warn"
		case "bad", "danger", "error", "critical", "negative":
			st = "bad"
		default:
			st = "normal"
		}
		tiles = append(tiles, metricTile{
			Label:  lbl,
			Value:  val,
			Unit:   clipRunes(strVal(m["unit"]), 14),
			Delta:  clipRunes(strVal(m["delta"], m["note"], m["reference"], m["range"]), 36),
			Status: st,
		})
	}
	if len(tiles) == 0 {
		return "参数不完整：show_metrics 的 metrics 至少需要 1 项包含 label 和 value 的指标。"
	}
	card := metricsCardData{
		Title:    title,
		Subtitle: clipRunes(strVal(raw["subtitle"]), 60),
		Metrics:  tiles,
		Summary:  clipRunes(strVal(raw["summary"], raw["conclusion"]), 120),
	}
	if err := s.appendAndPersistComponent(t, "metrics", card, nil); err != nil {
		return "渲染指标看板失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成指标看板："+card.Title, fmt.Sprintf("共 %d 项核心指标", len(tiles)))
	t.postActivity = "正在补充解读…"
	return fmt.Sprintf("核心指标看板「%s」（共 %d 项）已在当前位置展示给用户。请只补充简短的综合建议或注意事项，不要把看板中的数字逐项重复罗列。", card.Title, len(tiles))
}

func (s *Service) execAskUserQuestion(t *toolTurn, rawArgs string) (string, bool) {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 questions 数组）。", false
	}
	qRaw, _ := raw["questions"].([]any)
	// Shorthand tolerance: top-level "question" + "options"
	if len(qRaw) == 0 && strVal(raw["question"]) != "" {
		qRaw = []any{raw}
	}
	var questions []askUserQuestionItem
	for _, elem := range qRaw {
		if len(questions) >= 4 {
			break
		}
		m, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		qText := clipRunes(strVal(m["question"], m["title"], m["prompt"]), 140)
		if qText == "" {
			continue
		}
		multi, _ := m["multi_select"].(bool)
		if !multi {
			multi, _ = m["is_multi_select"].(bool)
		}
		oRaw, _ := m["options"].([]any)
		var opts []askUserOption
		for _, oe := range oRaw {
			if len(opts) >= 6 {
				break
			}
			var opt askUserOption
			switch ov := oe.(type) {
			case string:
				opt.Label = ov
			case map[string]any:
				opt.Label = strVal(ov["label"], ov["text"], ov["title"])
				opt.Description = clipRunes(strVal(ov["description"], ov["detail"], ov["hint"]), 70)
				opt.Recommended, _ = ov["recommended"].(bool)
			}
			lbl := strings.TrimSpace(opt.Label)
			for _, pfx := range []string{"(Recommended)", "（推荐）", "(推荐)", "[推荐]"} {
				if strings.HasPrefix(strings.ToLower(lbl), strings.ToLower(pfx)) {
					opt.Recommended = true
					lbl = strings.TrimSpace(lbl[len(pfx):])
				}
			}
			lbl = stripOptionPrefix(lbl)
			if lbl == "" || lbl == "其他" || strings.EqualFold(lbl, "other") {
				continue
			}
			opt.Label = clipRunes(lbl, 36)
			opts = append(opts, opt)
		}
		if len(opts) < 2 {
			continue
		}
		questions = append(questions, askUserQuestionItem{
			ID:          fmt.Sprintf("q%d", len(questions)+1),
			Question:    qText,
			MultiSelect: multi,
			Options:     opts,
		})
	}
	if len(questions) == 0 {
		return "参数不完整：ask_user_question 需要 1~4 个包含至少 2 个选项的有效问题。", false
	}
	card := askUserCardData{
		Title:     clipRunes(strVal(raw["title"]), 36),
		Questions: questions,
	}
	initState := askUserCardState{
		Submitted: false,
		Answers:   map[string]askUserAnswerEntry{},
	}
	if err := s.appendAndPersistComponent(t, "ask_user", card, initState); err != nil {
		return "展示询问卡片失败：" + err.Error(), false
	}
	t.run.addDoneStep("ask_user", fmt.Sprintf("向您确认 %d 个偏好选项", len(questions)), questions[0].Question)
	t.postActivity = "等待您点选确认…"
	return fmt.Sprintf("已向用户展示包含 %d 个问题的原生点选卡片（含自动附带的「其他」自定义输入）。请只用简短温暖的一句话（不超过 25 字）引导用户点选卡片，绝对不要在正文中把题目或选项再复述一遍，也不要提前输出长篇方案，停下来等待用户选择。", len(questions)), true
}

// ---------------- Multi-Turn History Summary Note ----------------

// componentNote builds a concise system note summarizing components rendered in an earlier
// assistant message (including live user interactions like checked Todo items, Quiz choice,
// or submitted AskUser preferences) so follow-up turns stay context-aware without polluting m.Content.
func componentNote(comps []store.MessageComponent) string {
	if len(comps) == 0 {
		return ""
	}
	var parts []string
	for _, c := range comps {
		switch c.Kind {
		case "weather":
			var d weatherCardData
			if json.Unmarshal(c.Data, &d) == nil && d.Location != "" {
				switch {
				case d.Low != nil && d.High != nil:
					parts = append(parts, fmt.Sprintf("天气卡「%s %s %.0f~%.0f℃」", d.Location, d.Condition, *d.Low, *d.High))
				case d.Temp != nil:
					parts = append(parts, fmt.Sprintf("天气卡「%s %s %.0f℃」", d.Location, d.Condition, *d.Temp))
				default:
					parts = append(parts, fmt.Sprintf("天气卡「%s %s」", d.Location, d.Condition))
				}
			}
		case "todo":
			var d todoCardData
			var st todoCardState
			_ = json.Unmarshal(c.Data, &d)
			_ = json.Unmarshal(c.State, &st)
			doneSet := map[string]bool{}
			for _, id := range st.DoneIDs {
				doneSet[id] = true
			}
			var doneNames, todoNames []string
			for _, it := range d.Items {
				if doneSet[it.ID] {
					doneNames = append(doneNames, it.Text)
				} else {
					todoNames = append(todoNames, it.Text)
				}
			}
			summary := fmt.Sprintf("互动清单「%s」（已完成 %d/%d", d.Title, len(doneNames), len(d.Items))
			if len(doneNames) > 0 {
				summary += "，已勾选：" + clipRunes(strings.Join(doneNames, "、"), 80)
			}
			if len(todoNames) > 0 {
				summary += "；未勾选：" + clipRunes(strings.Join(todoNames, "、"), 80)
			}
			summary += "）"
			parts = append(parts, summary)
		case "chart":
			var d chartCardData
			if json.Unmarshal(c.Data, &d) == nil {
				parts = append(parts, fmt.Sprintf("数据图表「%s」(%s, %d项)", d.Title, d.ChartType, len(d.Labels)))
			}
		case "quiz":
			var d quizCardData
			var st quizCardState
			st.SelectedIndex = -1
			_ = json.Unmarshal(c.Data, &d)
			_ = json.Unmarshal(c.State, &st)
			status := "用户尚未点选"
			if st.SelectedIndex >= 0 && st.SelectedIndex < len(d.Options) {
				verdict := "回答错误"
				if st.SelectedIndex == d.AnswerIndex {
					verdict = "回答正确"
				}
				status = fmt.Sprintf("用户已选「%s」，%s", d.Options[st.SelectedIndex], verdict)
			}
			parts = append(parts, fmt.Sprintf("问答测验卡「%s」（%s）", clipRunes(d.Question, 36), status))
		case "metrics":
			var d metricsCardData
			if json.Unmarshal(c.Data, &d) == nil {
				var kvs []string
				for i, m := range d.Metrics {
					if i >= 4 {
						break
					}
					kvs = append(kvs, m.Label+":"+m.Value+m.Unit)
				}
				parts = append(parts, fmt.Sprintf("指标看板「%s」（%s）", d.Title, strings.Join(kvs, "，")))
			}
		case "ask_user":
			var d askUserCardData
			var st askUserCardState
			_ = json.Unmarshal(c.Data, &d)
			_ = json.Unmarshal(c.State, &st)
			var qs []string
			for _, q := range d.Questions {
				qs = append(qs, clipRunes(q.Question, 24))
			}
			status := "用户未点选此卡片（若用户已聊起新话题，直接顺应新话题回复，切勿强行追问）"
			if st.Submitted {
				status = "用户已在卡片确认提交"
			} else if st.Dismissed {
				status = "用户已主动跳过关闭该询问卡（请尊重用户自己的话题，不要追问未选问题）"
			}
			parts = append(parts, fmt.Sprintf("交互询问卡（问题：%s；%s）", strings.Join(qs, " / "), status))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "（系统附注，不是对话内容，回复时不要写这种附注：这条回复里你向用户展示了以下交互卡片——" + strings.Join(parts, "；") + "）\n"
}

// ---------------- Parsing Helpers ----------------

func strVal(vals ...any) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(round2(x), 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func numVal(vals ...any) (float64, bool) {
	for _, v := range vals {
		switch x := v.(type) {
		case float64:
			if !math.IsNaN(x) && !math.IsInf(x, 0) {
				return x, true
			}
		case int:
			return float64(x), true
		case string:
			clean := strings.TrimSpace(x)
			for _, suf := range []string{"℃", "°C", "°", "%"} {
				clean = strings.TrimSuffix(clean, suf)
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(clean), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				return f, true
			}
		}
	}
	return 0, false
}

func floatSlice(vals ...any) []float64 {
	for _, v := range vals {
		arr, ok := v.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		out := make([]float64, 0, min(len(arr), 16))
		for _, elem := range arr {
			if len(out) >= 16 {
				break
			}
			if n, ok := numVal(elem); ok {
				out = append(out, round2(n))
			} else {
				out = append(out, 0)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

func stripOptionPrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		c := s[0]
		if (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') || (c >= '1' && c <= '6') {
			rest := s[1:]
			for _, sep := range []string{". ", "、", "．", ") ", "）", ".", ":"} {
				if strings.HasPrefix(rest, sep) {
					return strings.TrimSpace(rest[len(sep):])
				}
			}
		}
	}
	return s
}
