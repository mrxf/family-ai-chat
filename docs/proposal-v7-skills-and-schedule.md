# v7 方案：Skills 级按需工具引擎与家庭日程提醒管理

> 2026-11-20 定稿并落地。借鉴 `xiuxian-server`（`@innerlife/agent`）的双层工具（`common` vs `skill`）架构，并针对小暖的**上游前缀缓存（Prefix Cache）保护、家庭适老化交互与无谷歌推送环境下的手机准点提醒**做了深度适配。

## 1. 背景与核心问题

随着小暖能力从「画图、联网搜索、跨对话记忆」扩展到「日程提醒、日历、地图出行、家庭账本」等更多垂直生活领域，如果把几十个工具的完整 JSON Schema 与使用规范全部常驻塞进每轮请求，会产生三个问题：

1. **Token 成本与延迟膨胀**：95% 的日常闲聊并不涉及低频领域工具，常驻几十个 JSON Schema 每轮白白消耗数千输入 token；
2. **注意力稀释与误调**：工具过多会导致中轻量模型在相似工具间混淆，或漏看复杂领域的业务边界（如周期提醒打卡与永久删除的区别、相对日期换算规则）；
3. **传统动态 System Prompt 击穿前缀缓存**：如果在激活技能或倒计时租约时改写 `Messages[0]`（例如写入递减的 `剩余有效轮次: 3 -> 2 -> 1`），会直接击穿 v6 建立的 100% 上游前缀缓存。

---

## 2. 核心架构设计

### 2.1 双层工具分级（`common` vs `skill`）与「双驱动」目录披露

- **常驻公共层（`ScopeCommon`）**：高频通用能力（`generate_image`、`web_search`、`web_fetch`、`recall_memory`、`read_past_conversation` 以及内置的 `read_skill`）默认挂载在 `req.Tools` 中。
- **按需技能层（`ScopeSkill`）**：低频垂直能力按业务域聚合为 `skills.Definition`（内置注册于 `server/internal/skills`），平时**不进入** `req.Tools`。
- **目录摘要 + 工具线索有意披露（`ToolHints`）**：
  - 在 `systemPromptWithTools` 中注入静态不变的 `# 技能与扩展工具目录`，列出每个技能的 `key`、`Title`、`WhenToUse`，**并显式披露 `read_skill` 后可解锁的工具名与一句话能力摘要**。
  - **两条触发路径双保险**：
    1. **意图驱动**：用户聊到未来安排或要求定提醒，模型根据 `适用场景` 调用 `read_skill({"skill_key": "schedule"})`；
    2. **工具驱动（顺便读规范）**：模型想调用 `create_schedule` 等工具时，从目录披露中得知它归属 `schedule` 技能，于是调用 `read_skill` 解锁工具，**顺带把完整业务指南、14 天日历星期对照表与近 7 天已有日程快照一并读入上下文**。
- **防幻觉盲调拦截**：若个别模型未调 `read_skill` 就直接盲调未解锁的 `skill` 工具，`runTools` 会拦截并返回明确指引，要求其先调用 `read_skill` 阅读指南后再执行。
- **不挤占业务工具轮次**：`read_skill` 作为 0ms 本地元数据读取，自动补偿最多 2 轮 `skillBonus` 预算，保证 `read_skill -> 查询/校验 -> 创建/更新 -> 自然回复` 完整闭环。

### 2.2 100% 保住上游前缀缓存与多轮会话租约（Skill Lease）

1. **`Messages[0]` 绝对静态**：`# 技能与扩展工具目录` 不含任何时间戳或递减计数器；同轮 `read_skill` 激活技能时，完整指南通过末尾 `role: "tool"` 消息返回，**绝不中途改写 `Messages[0]`**。
2. **会话级 3 轮轻量租约（`defaultSkillLeaseTurns = 3`）**：
   - 激活某项技能（或调用其工具）后，该会话接下来 3 轮对话内自动预挂载该技能的 `Tools`，并在最后一条用户消息的 `系统附注` 中附带静态 `LeaseSummary`（无递减数字）。用户下一句直接追问「帮我改到下午三点吧」时无需二次 `read_skill`，连续 3 轮未再使用后自动卸载回归精简态。
3. **动态快照省去多余查询轮次（`Dynamic` 生成器）**：
   - `read_skill("schedule")` 返回时实时拼入 **「当前精准时间 + 近 14 天日期星期对照表 + 未来 7 天已有日程快照（含 `schedule_id`）」**。90% 的近期增删改操作无需再多跑一轮 `list_schedules` 即可防重、防冲突并精准定位 ID，首字延迟减少约 1.5～3 秒。

### 2.3 家庭日程管理与端到端本地提醒闭环

1. **数据模型与周期展开引擎（`013_schedules.sql`，SQLite & Postgres 双库对齐）**：
   - 支持单次事项、全天事项（默认当天 `09:00` 提醒）、提前 N 分钟/小时/天提醒与不响铃纯备忘（`remind_before_min = -1`）；
   - 支持 `daily`（每天）、`weekdays`（工作日）、`weekly`（自定义周几，如 `1,3,5`）、`monthly`（含月底自动收敛，如 31 号在 2 月落到 28/29 号）、`yearly`（生日/纪念日）；
   - **单次完成 vs 周期单日打卡隔离**：周期日程通过 `completed_dates` 仅勾选指定日期（如今天吃过药了），次日及后续周期提醒照常生效。
2. **日常聊天主动关怀联动 + 严格隐私隔离**：
   - 普通对话在最后一条用户消息的 `系统附注` 自动注入「今明两天的待办日程摘要」（不影响前缀缓存），当父母问「明天早饭吃啥」时小暖能贴心提醒「明天 08:30 要空腹体检」；
   - **隐私对话双盲隔离**：隐私模式下隐藏技能目录、禁用 `read_skill` 与所有日程工具、不注入近期日程附注。
3. **零第三方插件的原生手机准点提醒（`com.nuan.chat/reminders`）**：
   - 服务端统一计算未来 30 天内最近 48 个待触发提醒时间点（`upcoming_reminders`，规避 iOS 系统 64 条本地通知上限）；
   - **Android 原生**：基于系统 `AlarmManager`（Android 12+ 自动检测 `canScheduleExactAlarms` 并平滑降级 `setAndAllowWhileIdle`，绝不抛权限异常）+ 高优先级通知渠道「小暖日程提醒」+ `SharedPreferences` 持久化 + `BOOT_COMPLETED / MY_PACKAGE_REPLACED` 开机与升级自动重注册；
   - **iOS 原生**：基于 `UNUserNotificationCenter` 日历触发器，支持前台横幅响铃与点击通知直达「我的日程」页；
   - **自动同步时机**：聊天中一旦触发 `kind: "schedule"` 工具步骤，流结束时客户端自动刷新日程、同步手机本地闹钟并在首次需要时温和请求系统通知权限；退出登录时自动清空本机挂起提醒。

---

## 3. 代码落点一览

| 模块 | 职责 | 文件 |
|---|---|---|
| 数据库迁移 | `schedules` 表结构与索引（SQLite / Postgres 对齐）及 `migrate-db` 搬家表清单 | `server/internal/store/migrations/{sqlite,postgres}/013_schedules.sql`、`server/internal/store/copy.go` |
| 日程存储与展开 | CRUD、周期展开 `ExpandOccurrences`、月末收敛、未来提醒计算 `UpcomingReminders` | `server/internal/store/schedules.go`、`server/internal/store/schedules_test.go` |
| Skills 引擎 | `ScopeCommon / ScopeSkill`、`ToolHint` 目录披露、`Provider / Registry`、`read_skill` 定义 | `server/internal/skills/skills.go`、`server/internal/skills/skills_test.go` |
| 对话技能运行时 | 会话技能租约生命周期、`read_skill` 执行、动态挂载工具、防盲调拦截、轮次补偿 | `server/internal/chat/skills_runtime.go`、`server/internal/chat/service.go`、`server/internal/chat/tools.go` |
| 日程技能与工具 | `schedule` 技能指南、14天日历+7天快照、`list/create/update/complete/delete_schedule`、冲突检测、今明附注 | `server/internal/chat/tools_schedule.go`、`server/internal/chat/skills_schedule_test.go` |
| REST API | `GET/POST/PATCH/DELETE /api/schedules` | `server/internal/api/schedules.go`、`server/internal/api/server.go`、`server/internal/api/schedules_test.go` |
| 原生本地通知桥 | Android `AlarmManager` + 开机重注册、iOS `UNUserNotificationCenter`、Dart `ReminderBridge` | `app/android/app/src/main/kotlin/com/nuan/chat/ReminderNotifications.kt`、`MainActivity.kt`、`AndroidManifest.xml`、`app/ios/Runner/AppDelegate.swift`、`app/lib/core/reminders.dart` |
| 客户端状态与界面 | 自动同步与权限引导、对话步骤条、`我的日程` 页、添加/编辑弹窗、侧边栏与设置页入口 | `app/lib/state/schedules.dart`、`app/lib/state/chat.dart`、`app/lib/ui/schedules_page.dart`、`app/lib/ui/schedule_editor.dart`、`app/lib/ui/messages.dart`、`app/lib/ui/drawer.dart`、`app/lib/ui/settings_page.dart` |
