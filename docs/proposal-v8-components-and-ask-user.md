# v8 方案：Skills 级对话内联可视化组件 (`ui_components`) 与原生询问用户 (`ask_user`)

## 1. 背景与目标

纯文字长段落或窄屏 Markdown 大表格在手机阅读时容易让人视觉疲劳；同时当用户提出开放或模糊需求时，传统 AI 要么盲目猜一大篇套话，要么抛出一串问题逼用户费力打字回复。

v8 在 v7 **Skills 级按需工具引擎**之上，新增两个相互独立、按需挂载的领域技能：
1. **`ui_components`（对话可视化卡片组件）**：提供 5 个预设精美组件工具（天气卡、互动待办清单、折线/柱状/环形图表、互动问答测验卡、核心指标看板），支持在流式回复正文中按调用位置自然穿插，展示完组件后模型可继续输出温暖简短的叮嘱或洞察。
2. **`ask_user`（交互式询问用户偏好）**：对标 Coding Agent 的原生 Question 交互，提供独立的 `ask_user_question` 技能工具，支持单选/多选、推荐标记与「其他」自由输入，点选确认后自动持久化状态并作为下一条用户消息发回给小暖。

---

## 2. 核心架构设计

### 2.1 零文本污染的正文流式穿插（Rune Offset Interleaving）
- `messages` 表新增 `components TEXT NOT NULL DEFAULT '[]'`（迁移 `014_message_components.sql`，SQLite / Postgres 双库对齐）。
- 每个 `MessageComponent` 包含 `{ id, kind, pos, data, state }`：
  - 当模型在流式生成中途调用组件工具时，`Run.addComponent` 在互斥锁内原子记录当前 `content` 的 Unicode 字符数（`pos = utf8.RuneCountInString(content)`），并立即通过 `SetComponents` 落库、通过 SSE `event: components` 推送给客户端。
  - 客户端 `InterleavedAssistantBody` 按 `pos` 将正文切分为多段 `Md` 与 `ChatComponentCard` 交替渲染。
  - **优势**：`Message.content` 始终保持 100% 纯净文本——「复制全文」不带乱码占位符、系统 TTS 朗读不会念出标签、全文检索与摘要压缩不受 JSON 噪声干扰。

### 2.2 交互状态持久化与多轮上下文感知
- 用户在气泡中勾选 `TodoCard`、作答 `QuizCard` 或提交 `AskUserCard` 时，客户端乐观更新本地状态并调用 `PATCH /api/messages/{id}/components/{compId}` 同步写回服务端（若该消息仍在流式输出中，同步更新内存 `Run` 快照防止收尾覆盖）。
- 后续轮次构建历史上下文时，`service.go` 通过 `componentNote(m.Components)` 将卡片摘要及**最新交互状态**（如 `已完成 2/3，未勾选：充电宝`、`用户已选 B，回答正确`）以轻量系统附注告知模型，让小暖真正“看得见”用户在卡片上的每一次点按。

### 2.3 五大基础组件 (`ui_components`) 与原生询问 (`ask_user`)
| 技能 Key | 工具名 | 核心能力与交互体验 |
|---|---|---|
| `ui_components` | `show_weather` | 纯展示渲染工具：SOP 引导模型先调 `web_search` 查真实气象数据再填参；自适应晴/多云/雨/雪/雷/雾霾天色渐变、实时或温差大字、湿度风力胶囊、出行贴士与多日温差区间条 |
| `ui_components` | `show_todo` | 标题 + 动态完成进度条 + 可打勾条目（支持备注与分类标签），点击打勾即时落库保存 |
| `ui_components` | `show_chart` | 零第三方图表库膨胀，Flutter `CustomPaint` 原生绘制平滑折线图 (`line`)、分组柱状图 (`bar`)、环形占比图 (`pie`)，支持手势点击任意数据点/扇区查看精确数值 |
| `ui_components` | `show_quiz` | 支持趣味答题与防骗/健康自测，SOP 严禁正文剧透；点击选项即时揭晓绿/红判分动效并展开原理解析，支持「重做」 |
| `ui_components` | `show_metrics` | 自适应双列大字号指标格 + `normal / good / warn / bad` 状态色圆点，适合房贷理财算账、体检指标解读、热量营养速览 |
| `ask_user` | `ask_user_question` | 支持 1～4 道澄清问题、单选/多选切换、`推荐` 徽章与「其他（自己补充说明…）」输入框；单轮调用后自动收尾等待用户作答，用户点「确认并发送」后锁定为已确认摘要卡并自动发出回复 |
