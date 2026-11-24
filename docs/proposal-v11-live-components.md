# v11 方案：组件唯一实体化与版本溯源（Live Components & Versioning）

> 2026-11-24 定稿。在 v8 **对话内联组件**、首页 PIN 看板、v10 **对话操控应用** 与 AI 定时任务的基础上，将交互卡片升级为**全局唯一、可原地演进、可版本溯源**的独立实体，为跨对话修改（如天气更新、待办增删）和定时任务自动刷新（如每日新闻资讯）打下稳固的数据底座。

## 1. 背景与现状痛点

目前交互组件（`weather` / `todo` / `feed` / `chart` / `metrics` / `quiz`）以 JSON 数组的形式寄生在 `messages.components` 字段内，依靠 `(message_id, component_id)` 复合定位。直接覆盖原消息 JSON 会带来四个问题：

1. **历史对话图文割裂**：11 月 1 日的对话里讨论的是 11/1~11/7 天气，11 月 8 日在另一个对话更新为新一周后，回到原对话时文字与卡片内容互相矛盾，且无历史记录可查。
2. **生命周期强绑定消息**：首页 PIN 与定时任务都直接绑定 `(message_id, component_id)`，删除原对话会影响卡片持续存在。
3. **跨对话更新缺少当场反馈**：在新对话中要求修改已有卡片时，原卡片在后台被改写，新对话里没有直接呈现出更新后的卡片；如果重新生成一张新卡片，又会产生重复组件。
4. **缺少版本与溯源机制**：无法追溯一张卡片经历过哪些对话或定时任务的更新，也缺少并发修改时的版本保护。

## 2. 已确认的产品决策

1. **原对话展示策略**：原对话默认锚定当时的历史版本（如 `v1`），保证历史上下文真实一致；当实体已有更新时，卡片顶部显示轻量提示条（`历史版本 v1 · 已有更新 vN`），支持**一键原地切换查看最新**或切回历史快照。
2. **版本历史一期范围**：一期提供**只读版本历史查看与来源溯源**（展示版本号、更新时间、变更摘要、来源对话/定时任务，支持跳转来源对话与展开预览历史快照）；一键回滚能力留到二期实现。
3. **版本保留策略**：每个组件在版本历史表中严格保留**最近 10 个版本**，写入新版本时同事务自动裁剪更早记录；同时 `messages.components` 保留该条消息生成/更新时刻的快照作为兜底，确保超出 10 个版本后的久远历史对话依然可以正常渲染当时的快照。

## 3. 数据模型设计（`018_live_components.sql`）

### 3.1 `components`（组件唯一实体表）

存储每个可演进组件的唯一最新状态（`ask_user`、`app_action` 等一次性交互/控制卡片不入此表）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `TEXT PK` | 全局唯一实体 ID（UUIDv7） |
| `user_id` | `TEXT NOT NULL` | 所属用户（`ON DELETE CASCADE`） |
| `kind` | `TEXT NOT NULL` | `weather` / `todo` / `feed` / `chart` / `metrics` / `quiz` |
| `title` | `TEXT NOT NULL` | 冗余摘要标题，便于检索与列表展示 |
| `current_version` | `INTEGER NOT NULL` | 当前数据版本号，从 `1` 开始自增 |
| `data` | `TEXT NOT NULL` | 最新规范化内容 JSON |
| `state` | `TEXT NOT NULL` | 最新交互态 JSON（如 TODO 勾选、小测作答） |
| `private` | `INTEGER/BOOLEAN` | 是否诞生于隐私对话（隐私组件不可 PIN 或跨对话修改） |
| `origin_conversation_id` | `TEXT NOT NULL` | 最初诞生对话 ID |
| `origin_message_id` | `TEXT NOT NULL` | 最初诞生消息 ID |
| `origin_component_id` | `TEXT NOT NULL` | 最初消息内局部组件 ID（兼容旧引用） |
| `last_source_type` | `TEXT NOT NULL` | `create` / `chat_update` / `task_update` / `manual_edit` |
| `last_source_conversation_id` | `TEXT NOT NULL` | 最近一次修改来源对话 ID |
| `last_source_message_id` | `TEXT NOT NULL` | 最近一次修改来源消息 ID |
| `last_source_task_id` | `TEXT NOT NULL` | 最近一次修改来源定时任务 ID |
| `created_at` / `updated_at` | `INTEGER/BIGINT` | 创建与最后更新时间（毫秒） |
| `deleted_at` | `INTEGER/BIGINT` | 软删除时间戳，`0` 表示正常 |

### 3.2 `component_versions`（最近 10 个版本快照表）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `TEXT PK` | 版本记录 ID |
| `component_id` | `TEXT NOT NULL` | 关联 `components.id`（`ON DELETE CASCADE`） |
| `version` | `INTEGER NOT NULL` | 版本号，`UNIQUE(component_id, version)` |
| `data` / `state` | `TEXT NOT NULL` | 该版本时刻的内容与状态快照 |
| `change_summary` | `TEXT NOT NULL` | 简短变更摘要（如“更新为本周天气”“新增待办：买牛奶”） |
| `source_type` | `TEXT NOT NULL` | `create` / `chat_update` / `task_update` / `manual_edit` |
| `source_conversation_id` | `TEXT NOT NULL` | 触发修改的对话 ID |
| `source_message_id` | `TEXT NOT NULL` | 触发修改的消息 ID |
| `source_task_id` | `TEXT NOT NULL` | 触发修改的定时任务 ID |
| `created_at` | `INTEGER/BIGINT` | 版本生成时间 |

**版本号递增规则：**
- **`data` 发生语义变更**（对话更新、手动编辑、定时任务刷新）→ `current_version + 1`，写入 `component_versions`，并在同事务内执行 `DELETE FROM component_versions WHERE component_id = ? AND version <= current_version - 10`。
- **`state` 发生纯交互态变更**（勾选 TODO 复选框、点选小测选项）→ 原地更新 `components.state`，**不递增版本号**，避免产生无意义碎片版本。

### 3.3 关联表与消息结构扩展

- `pinned_components` 新增 `entity_id TEXT NOT NULL DEFAULT ''`：列表读取时优先按 `entity_id` 从 `components` 读取最新 `data` / `state` / `current_version`。
- `scheduled_tasks` 新增 `target_entity_id TEXT NOT NULL DEFAULT ''`：定时任务优先按 `target_entity_id` 定位目标卡片。
- `MessageComponent` 扩展字段（向后兼容）：
  - `entity_id`（落盘）：关联的全局唯一实体 ID。
  - `version`（落盘）：本条消息锚定的版本号（如创建时为 `1`，新对话更新时为 `2`）。
  - `latest_version`（读取时动态注入）：该实体当前最新版本号。
  - `deleted`（读取时动态注入）：该实体是否已被删除。
  - `data`（落盘）：本条消息对应版本的快照，确保即使旧版本被 10 版本上限裁剪，历史对话仍可完整渲染。
  - `state`（读取时同步）：当 `version == latest_version` 时自动同步实体最新 `state`，保证最新版本与首页勾选状态实时一致；处于历史版本时保持快照只读。

### 3.4 老数据平滑兼容（懒回填）

历史消息中尚未拥有 `entity_id` 的组件无需停机全表扫描迁移：在首次被 PIN、被更新、被定时任务绑定时自动创建对应的 `components(v1)` 与 `component_versions(v1)`，并回写 `entity_id` 到原消息和关联表。

## 4. 三处核心场景展示方案

| 场景 | 默认展示 | 版本提示与交互 |
|---|---|---|
| **首页 PIN 看板** | 永远展示最新 `vN` | 底栏展示 `vN` 胶囊（桌面带 `Tooltip`），点击打开只读版本历史；右侧/菜单支持引用对话、编辑、定时更新 |
| **新对话（触发更新的回复）** | 自动挂载同一 `entity_id` 的最新 `vN` 卡片 | 顶部/操作区标注 `已更新 · vN`，当场即可看到最新结果，不产生重复卡片；若后续又被改成 `v(N+1)`，自动转为历史提示态 |
| **原对话（当年生成 `v1` 的消息）** | 默认锚定 `v1` 快照 | 顶部展示 `历史版本 v1 · 已有更新 vN [查看最新]`；点击原地切换预览最新 `vN`（并可切回 `v1`）；历史态下交互控件只读，切到最新后可直接交互 |

## 5. 服务端接口与 LLM 工具变更

### 5.1 新增 HTTP API
- `GET /api/components/{entityId}`：获取组件实体最新内容、版本号与最近更新来源。
- `GET /api/components/{entityId}/versions`：获取该组件最近 10 个版本的只读时间线（含来源对话标题 / 定时任务标题）。
- `PATCH /api/components/{entityId}`：按 `entity_id` 更新 `data`（生成新版本）或 `state`（原地更新交互态）。
- 原有 `PATCH /api/messages/{id}/components/{compId}` 保持兼容，内部统一路由到实体版本更新逻辑。

### 5.2 LLM 工具演进
- `show_*` 创建卡片时同步创建 `components(v1)` + `component_versions(v1)`，并把 `entity_id` 和 `version=1` 写入消息组件。
- `list_components` 与 `quotedComponentPromptNote` 注入 `entity_id` 与当前版本号，明确提示模型：修改/刷新/增删已有卡片必须调用 `update_component`，禁止再次调用 `show_*` 重复创建。
- `update_component` 接受 `entity_id`（优先）或 `message_id + component_id`（兼容），新增可选 `change_summary` 参数；更新成功后**自动将指向同一 `entity_id`、锚定 `newVersion` 的卡片挂载到当前回复消息**并通过 SSE 推送。

## 6. 跨端体验（移动端 + 桌面端）

- **全局组件状态池**：Flutter 侧新增 `componentEntitiesProvider`（按 `entityId` 索引最新 `ComponentEntity`），首页看板、原对话切换最新态、新对话更新卡共享同一份最新状态，修改后全端联动刷新。
- **只读版本历史面板**：使用 `showAdaptiveModal` 实现（移动端底部抽屉 Sheet，桌面端居中弹窗），展示版本列表（版本号、相对时间、变更摘要、来源徽标）、支持点击跳转来源对话，并支持在面板内直接展开预览任一历史版本的卡片渲染效果。
- **桌面端增强**：版本胶囊配 `Tooltip` 悬浮提示，右键菜单新增“查看版本历史”。

## 7. 分期计划

- **一期（本次实现）**：数据库迁移、实体与 10 版本存储层、API 与 LLM 工具联动、新对话自动挂载更新卡、原对话历史锚定 + 一键切最新、双端只读版本历史面板与测试验证。
- **二期（后续演进）**：版本一键回滚（恢复历史版本为 `v(N+1)`）、两版本 Diff 对比视图。
