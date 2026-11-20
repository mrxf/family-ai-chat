# v6 方案：长对话分块压缩、冷唤醒防爆与会话内翻旧话

> 2026-11-19 定稿。针对家庭长辈「像用微信一样同一个对话聊几个月」与「超长老对话（如 900K）隔夜再聊一次成本爆炸 / 逐条滑窗破坏前缀缓存」的痛点设计。

## 1. 背景与核心问题

v1 采用「按 `context_window` 倒序装满后直接硬裁剪」的简单滑窗策略，在实际家用场景中暴露了三个成本与体验缺陷：

1. **逐条滑窗击穿上游前缀缓存（Prefix Cache）**：一旦对话涨满窗口，每多聊一轮都会从 `system` 后挤掉最早的 1～2 条消息，导致后续所有 token 位置整体前移，上游 KV/前缀缓存每轮 0% 命中，轮轮支付满额无缓存输入费。
2. **存量超长老对话（如 900K）冷唤醒成本爆炸**：停聊半天或几天后上游缓存早已过期；若模型配置了 `200K / 1M` 大窗口，冷启动发一句日常问候就会把几十万冷历史全价重发一遍；若配合 Auto 动态升档到旗舰模型（Opus / Carbon / Sol），单句成本极高。
3. **同会话早期历史检索盲区**：`recall_memory` 的原文检索此前排除当前 `conversation_id`，被滑窗裁掉的早期本会话细节反而无法查回。

---

## 2. 已拍板的产品与工程决策

1. **家人无感接着聊 + 按需翻旧话**：
   - 客户端界面保持完整历史可上滑，不弹任何「压缩上下文」提示打扰长辈。
   - 存在已折叠早期历史时，自动向支持工具的模型挂载 `read_earlier_messages` 工具；仅当模型真正调用该工具翻阅本对话更早原话时，回答顶部步骤条展示 **「已翻看前面的聊天记录」**（`kind: "history"`）。
2. **高低水位分块滚动摘要（保住 100% 前缀缓存）**：
   - 拆分「单次超长文档物理上限（`hardBudget = ContextWindow - MaxOutput - sys`）」与「多轮活跃历史软上限（`activeCap`，128K～1M 模型封顶 `48K`，小窗口模型按比例缩放）」。
   - 当未归档活跃历史跨过触发水位（`compactTrigger = 36K`）时，在回复落盘后异步将较早部分合并整理为 300～850 字的 `conversations.summary`，并将 `conversations.summary_cursor` 推进至保留近期原文（`lowWater = 18K`）的分界点。
   - 两次归档之间的几十轮对话中，`system（含前情提要）+ summary_cursor 之后的消息序列` 头部完全固定不变，100% 命中上游前缀缓存。
   - **单次粘贴超长文档保护**：保护最近至少 2 轮用户提问（`minProtectedUserTurns = 2`）在活跃期内受 `hardBudget` 兜底而不受 `activeCap` 截断，确保用户刚贴入几万字合同/报告并紧接着追问时，全文留在热缓存中可读；聊过数轮后再自动归档入提要，且支持按关键词深挖长文中间任意条款窗口。
3. **老对话隔半天/过夜（`ColdResumeIdle = 6h`）自动轻载防爆**：
   - 倒序组装历史时一旦跨越相邻消息 `>= 6 小时` 的静默断层，断层之前的逐字历史预算立即收紧为 `coldTail`（`8K` tokens / 最多 12 条消息，约 6 轮对话）。
   - 若存量 900K 老对话尚未生成 `summary`，首句直接零延迟复用该会话已有的 `episode` 记忆概括作为临时前情提要立即作答（不阻塞首字），并在本轮回复完成后后台以严格有界输入（`<= 26K` tokens，绝不把 900K 全喂给摘要模型）补齐 `summary` 与 `summary_cursor`。
4. **Auto 跨模升档保护**：
   - Auto 动态路由中途从日常/轻量模型升级到 T4/T5 强力或旗舰模型时，自动将前序闲聊原文收紧至 `lowWater`（`18K`）以内，避免旗舰模型替前序几十轮家常闲聊冷启动买单。
5. **管理员零配置 & 严格的隐私隔离**：
   - 普通对话后台整理直接复用 `memory_model_id -> title_model_id -> default_model_id`；单模型编辑页的「上下文长度」明确为单次发送超长资料的容量上限。
   - **隐私对话自整理与零外泄**：隐私对话超长时，**仅调用用户在该隐私对话里当前所选的同一个模型与供应商**生成会话内临时提要（绝不发往外部 `memory_model_id` / `embedding` 供应商），不更新 `updated_at`（不影响 2 小时超时销毁计时），关闭隐私对话或超时回收时随 `conversations` 行级联物理销毁。
   - **修改问题原子失效**：`Store.ApplyTurn` 若发生 `TruncateFrom <= summary_cursor` 的前文回滚编辑，在同一数据库事务内原子清空过期的 `summary` 与 `summary_cursor`；若仅「重新回答」最新消息（`TruncateFrom > summary_cursor`），前情提要完整保留。

---

## 3. 数据模型与关键代码落点

### 3.1 数据库迁移（`012_context_compaction.sql`，SQLite & Postgres 对齐）

```sql
ALTER TABLE conversations ADD COLUMN summary TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN summary_cursor TEXT NOT NULL DEFAULT '';
```

- `ListConversations` 不加载 `summary` 字段，侧边栏列表零额外带宽开销；
- 生成请求通过 `ListMessagesAfter(convID, summaryCursor, asstID, 200)` 走 `messages_conversation (conversation_id, id)` 复合索引范围扫描，900K 老对话每轮仅读取游标后的少量近期消息；
- 画图与改图的全局 `图1..图N` 编号改走 `ListConversationImageRefs`（仅扫描 `attachments <> '[]'` 的消息），既避免加载历史大文本，又保证被折叠早期轮次中的图片编号与未折叠时期严格一致。

### 3.2 代码落点

| 模块 | 职责 | 文件 |
|---|---|---|
| 数据库迁移 | 新增 `conversations.summary` 与 `summary_cursor` | `server/internal/store/migrations/{sqlite,postgres}/012_context_compaction.sql` |
| 存储层 | 游标范围查询、原子提交摘要、回滚失效、同会话全文检索、跨会话补齐已归档本会话命中 | `server/internal/store/conversations.go` |
| 压缩与翻旧话 | 水位预算、冷唤醒切分、有界后台摘要器、`read_earlier_messages` 多窗口检索 | `server/internal/chat/compact.go` |
| 对话编排 | 上下文组装、Auto 升档收紧、工具挂载与步骤推送、优雅停机等待 | `server/internal/chat/service.go`、`server/internal/chat/tools.go` |
| 客户端 UI | `kind: "history"` 步骤展示（「正在翻看前面的聊天记录…」→「已翻看前面的聊天记录」）、流结束后平滑保留步骤胶囊、模型页说明更新 | `app/lib/ui/messages.dart`、`app/lib/ui/admin/model_edit_page.dart` |
