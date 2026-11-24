# v10 方案：对话操控应用（`app_control`）——首页看板管理与本机设置调节

> 2026-11-24 定稿（决策见第 8 节）。在 v7 **Skills 按需工具引擎**与 v8 **对话内联组件**之上，让小暖能通过对话直接操作应用的大部分能力：
> 管理首页 PIN（固定 / 取消固定 / 排序 / "猜你想问"开关）、调节本机基础设置（字体大小、主题、朗读语速、触感），以及跳转到指定页面。

## 1. 背景与目标

家里长辈最常见的诉求是"字太小了""把这个放到首页""那张天气卡不要了"。现在这些都要用户自己翻设置、长按卡片才能完成，而小暖只会口头指路。

**现状盘点：**

| 能力 | 现状 | 小暖能否操作 |
|---|---|---|
| 首页 PIN（固定 / 取消 / 删除 / 查询） | `ui_components` 技能已有 `pin_component`、`unpin_component`、`delete_component`、`list_components` | ✅ 但有缺口（见下） |
| 首页卡片排序 | `store.ReorderPinnedComponents` 已有，未暴露为工具 | ❌ |
| 首页"猜你想问"卡片 | 纯客户端本地开关（缓存 key `home_suggestions_pinned`） | ❌ |
| 字体、主题、语速、触感 | `AppSettings` 存在本机 `SharedPreferences`，服务端不可见 | ❌ |
| 页面跳转（日程、定时任务、记忆、设置） | 无 | ❌ |
| 日程、定时任务 | v7 `schedule` / automation 工具 | ✅ |

**PIN 现有缺口：**

1. `pin_component` 不传 ID 时只认"本轮刚创建"的卡片（`t.lastCreatedMsgID/CompID`）。用户说"把这个固定到首页"时，往往指的是**上一条回复里的卡片**或**引用过来的卡片**（`SendInput.QuotedComponent`），模型只能先 `list_components` 再固定，多耗一轮，偶尔还会固定错。
2. `unpin_component` 必须同时给出 `message_id` 和 `component_id`。说"把天气卡拿掉"时，至少要先查一轮。
3. PIN 管理工具与 6 个展示类工具（天气、待办、资讯、图表、测验、指标）绑在同一个技能里。只想管理首页时，也要把 11 个工具全部加载。

**目标：**

- 用户一句话就能完成首页管理和设置调节，小暖执行后在聊天里给出可撤销的结果卡片。
- 不破坏 v7 的前缀缓存（`Messages[0]` 保持静态）。
- 手机端和电脑端都可用，能力不同的地方按能力降级（遵守 AGENTS.md 第 1 条硬规则）。
- 有明确的安全白名单，敏感操作永远不开放。

---

## 2. 核心架构设计

### 2.1 设置归属：每台设备自己的设置，不迁移到服务端

字体、主题、语速、触感是**设备级偏好**：手机调大字，电脑不应该跟着变大。**已确认：设置改动只对发出消息的那台设备生效，不做同账号多设备同步。** 因此**不把这些设置挪到服务端存储**，而是采用"服务端定义并校验工具、客户端执行"的模式。

### 2.2 客户端动作通道（Client Action Channel）

```
用户："字太小了，看不清"
  → 小暖调用 set_app_settings({"font_scale": "step_up"})
  → 服务端按白名单与档位校验参数，换算成目标值（标准 → 大）
  → 追加一张 kind="app_action" 的消息组件（复用 v8 的 appendAndPersistComponent）
  → SSE event: components 推送给客户端
  → 发出这条消息的设备执行 settingsProvider.update(...)
  → 聊天里显示「已把字体调到「大」  [撤销]」
```

**为什么把动作做成消息组件：**

- **可追溯、可撤销**：动作和它的结果都落库，翻聊天记录能看到每次改了什么；卡片上的"撤销"会把设置恢复到动作执行前的值（`state.previous`）。
- **断线不丢**：复用 v8 的组件持久化与 SSE 推送，客户端重连后照样能收到。
- **零文本污染**：`Message.content` 仍是纯文本，复制、朗读、检索都不受影响。

**组件数据结构（`app_action`）：**

```json
{
  "id": "c_xxx",
  "kind": "app_action",
  "pos": 12,
  "data": {
    "action": "set_settings",              // set_settings | open_page | toggle_suggestions
    "target_device_id": "dev_abc",         // 发起消息的设备
    "patch": { "font_scale": 1.15 },
    "summary": "字体调到「大」"
  },
  "state": {
    "status": "pending",                   // pending | applied | undone | unsupported
    "applied_device_id": "dev_abc",
    "previous": { "font_scale": 1.0 },
    "applied_at": 1764000000000
  }
}
```

- `app_action` 组件**不可 PIN**（在 `store.PinComponent` 里和 `ask_user` 一样返回 `ErrComponentNotPinnable`）。
- 客户端执行后通过已有的 `PATCH /api/messages/{id}/components/{compId}` 回写 `state`，后续轮次的 `componentNote` 能看到"已应用 / 已撤销"。
- **幂等**：客户端在本地记录已执行的组件 ID（`storage` 中的 `applied_app_actions`，保留最近 200 条）。重放历史消息、重新进入对话时不会重复执行。只有 `status=pending` 且 `target_device_id` 等于本机的动作才会自动执行。

### 2.3 客户端上下文（`client_context`）

`POST /api/chat` 的请求体新增可选字段，`SendInput` / `toolTurn` 同步加 `ClientContext`：

```json
"client_context": {
  "device_id": "dev_abc",                 // 首次启动生成，存 prefs
  "device_kind": "mobile",                // mobile | desktop（取自 PlatformCapabilities）
  "settings": { "font_scale": 1.0, "theme": "system", "tts_rate": 0.5, "haptics": true },
  "capabilities": { "haptics": true, "tts": true },
  "suggestions_pinned": true
}
```

- **不进 `Messages[0]`**：当前设置只出现在 `read_skill("app_control")` 的 `Dynamic` 内容里，以及 `get_app_settings` 的返回中，不影响前缀缓存。
- 老版本客户端不带这个字段：`set_app_settings` / `open_app_page` 返回"当前客户端版本不支持，请让用户升级或手动在设置里调整"，PIN 类工具不受影响。
- 定时任务（automation worker）等没有客户端上下文的场景：设置类工具不挂载。

### 2.4 多设备行为

| 场景 | 行为 |
|---|---|
| 发出消息的设备 | 收到 `pending` 动作后**自动执行**，卡片显示"已调整 + 撤销" |
| 同一账号的其他设备打开这条对话 | **不自动执行**，卡片显示"在本设备应用"按钮（受本机能力约束，比如电脑端不显示触感项） |
| 动作在本机不支持 | 卡片显示"这台设备不支持"，`state.status=unsupported` |

### 2.5 共享字号档位

手机设置页（`_FontScaleSlider`）和桌面设置页（`DesktopSettingsPanes._fontSteps`）目前各写了一份五档常量。本次抽成共享定义，并与服务端保持一致：

| 档位 | 小 | 较小 | 标准 | 大 | 特大 |
|---|---|---|---|---|---|
| `font_scale` | 0.85 | 0.92 | 1.0 | 1.15 | 1.3 |

- 客户端：`core/models/app_settings_steps.dart`（纯常量 + `nearestFontStep()`），两个设置页与动作执行器共用。
- 服务端：`chat/tools_app_control.go` 内同名常量，支持 `step_up` / `step_down`（"再大一点"），到头时提示"已经是最大 / 最小档"。
- 主题：`system` / `light` / `dark`；语速：0.3–0.7 按 0.1 步进，同时支持 `faster` / `slower`。

---

## 3. 新技能 `app_control`（应用设置与首页管理）

### 3.1 技能定义

- **Key**：`app_control`
- **Title**：应用设置与首页管理
- **WhenToUse**：用户想调字体大小、深浅色、朗读快慢、震动反馈；想把卡片固定到首页、从首页拿掉、调整首页顺序、显示或隐藏"猜你想问"；想打开日程、定时任务、记忆、设置等页面时。
- **LeaseSummary**：「应用设置与首页管理 (app_control)」已解锁，可直接调用 set_app_settings / pin_component / unpin_component / reorder_home_cards 等工具，用户说"再大一点""那张也拿掉"时无需再次 read_skill。
- **Dynamic**：本机当前设置、设备类型与能力、首页已固定卡片清单（标题 + 短 ID，最多 20 张）。大多数操作无需再查一轮。

### 3.2 工具清单

| 工具 | 参数 | 作用 | 执行方 |
|---|---|---|---|
| `get_app_settings` | 无 | 返回本机当前设置与能力 | 服务端（读 client_context） |
| `set_app_settings` | `font_scale`（档位名 / `step_up` / `step_down`）、`theme`、`tts_rate`（数值 / `faster` / `slower`）、`haptics` | 修改本机设置，一次可改多项 | 客户端动作 |
| `open_app_page` | `page`：`schedules` / `scheduled_tasks` / `memories` / `settings` / `home` | 打开指定页面（走 `appRoute`，宽屏下自动适配侧栏） | 客户端动作 |
| `list_home_cards` | `keyword`（可选） | 列出首页卡片（含"猜你想问"），按标题关键字过滤 | 服务端 |
| `pin_component`（增强） | `message_id`、`component_id`（均可选） | 固定卡片到首页 | 服务端 |
| `unpin_component`（增强） | `message_id` + `component_id`，或 `keyword` | 从首页取消固定 | 服务端 |
| `reorder_home_cards` | `component_ref` + `position`（`first` / `last` / 序号） | 调整首页顺序（"把天气放最前面"） | 服务端（`ReorderPinnedComponents`） |
| `toggle_suggestions_card` | `visible` | 显示或隐藏"猜你想问" | 客户端动作 |

### 3.3 `pin_component` 默认目标解析顺序

不传 ID 时按以下顺序寻找目标，找到即停：

1. 本轮刚创建的卡片（现有逻辑 `t.lastCreatedMsgID/CompID`）；
2. 用户本条消息引用的卡片（`SendInput.QuotedComponent`）；
3. 本对话最近一条带可固定卡片的助手消息中的**最后一张**卡片；
4. 都没有：返回"没找到可以固定的卡片，请先让用户指明是哪一张"。

已固定的卡片再次固定时直接返回"已经在首页了"，不报错。

### 3.4 `unpin_component` 关键字匹配

- 传 `keyword` 时，在首页已固定卡片的标题与摘要（复用 `summarizeComponentForQuote`）中匹配；
- 唯一命中：直接取消固定；
- 多张命中：不执行，返回候选列表，SOP 要求模型调用 `ask_user_question` 让用户点选；
- 零命中：返回首页现有卡片标题，供模型向用户确认。

### 3.5 与 `ui_components` 的关系

- `pin_component`、`unpin_component`、`list_components` **同时挂在** `ui_components` 和 `app_control` 两个技能下，工具实现不变。
- `skills.Registry.byTool` 现在是"一个工具对应一个技能"，需要改为一对多：`OwnerSkill` 返回首个可用的技能，防盲调拦截在任一技能已激活时都放行。
- 用户只说"把 XX 固定到首页"时，只需加载 `app_control`（约 8 个工具），不用加载 11 个组件工具。
- `ask_user_question` 也同时挂到 `app_control` 下：模型在"先问再改"、删除确认、多张卡片候选点选时可以直接调用，不必再加载 `ask_user` 技能。

### 3.6 SOP 要点（写入 `Content`）

- 用户**明确要求**调整（"把字调大""换成深色"）时直接执行，并在回复里说明改了什么、怎么撤销。
- 用户只是表达**感受**（"看不清""字太小""太亮/太暗""说太快了"）而没有明确要求时，**先问一句再改**：调用 `ask_user_question` 给出选项（如「调大一档 / 调到特大 / 先不用」），用户确认后再执行。
- 相对说法（"再大一点"）用 `step_up` / `step_down`，不要自己猜具体数值。
- 改完设置后用一两句话自然回应，不要输出参数或 JSON。
- 删除卡片属于不可逆操作，必须先调用 `ask_user_question` 确认；取消固定不需要确认。
- 不支持的请求（退出登录、改密码等）温和说明需要用户自己在设置里操作，并可调用 `open_app_page("settings")` 帮他打开。

---

## 4. 安全边界

**白名单原则**：只有第 3.2 节列出的动作可以执行，客户端执行器对 `action` 和 `patch` 的键再做一次白名单校验，未知字段一律忽略。

| 级别 | 操作 |
|---|---|
| 直接执行 | 字体、主题、语速、触感、页面跳转、固定 / 取消固定 / 排序、"猜你想问"开关 |
| 先确认再执行 | 删除卡片、清空已完成日程等不可逆操作 |
| 永远不开放 | 退出登录、修改密码与个人资料、切换服务器、邀请码、所有管理员功能（模型、供应商、用户管理等） |

**频率限制**：每轮最多 6 次 `app_control` 工具调用，沿用 `checkComponentManageGuard` 的计数方式。

**隐私对话**：✅ 已确认——`app_control` 技能在隐私对话中**整体不可用**（`Available` 返回 `!ctx.Private`，与现有技能一致），设置类和 PIN 类工具都不开放。用户在隐私对话里提出调整时，小暖温和说明需要退出隐私对话或自己到设置里调整。因此不需要按工具粒度过滤可见性。

---

## 5. 两端交互与降级

| 能力 | 手机端（Android / iOS） | 电脑端（macOS / Windows） |
|---|---|---|
| 字体、主题、语速 | ✅ | ✅ |
| 触感 | ✅ | ❌ 工具参数不暴露，卡片隐藏该项（`caps.haptics=false`） |
| 页面跳转 | 推入新页面（`appRoute`） | 切换右侧面板 / 设置侧栏对应分区（经 `desktop_nav` 状态） |
| 动作卡片"撤销" | 点按 | 点击 + Tooltip |
| 其他设备"在本设备应用" | ✅ | ✅ |

文案不写死"手机"，统一用 `caps.deviceNoun`（如"已把这台{deviceNoun}的字体调大"）。

---

## 6. 代码落点

> ⚠️ 按 AGENTS.md 第 2 条：`chat/service.go`、`chat/tools.go`、`chat/tools_components.go` 已超过 800 行，本次改动前先按职责拆分；`app/lib/state/chat.dart`（781 行）只加一行转发，执行逻辑放到新文件。

| 模块 | 职责 | 文件 |
|---|---|---|
| 技能与工具 | `app_control` 技能定义、SOP、Dynamic 快照、参数校验与档位换算、`app_action` 组件生成 | 新建 `server/internal/chat/tools_app_control.go`、`tools_app_control_test.go` |
| 首页卡片工具 | `list_home_cards`、`reorder_home_cards`、`pin/unpin` 默认目标与关键字匹配 | `server/internal/chat/tools_components_manage.go`（拆出 `tools_home_cards.go`） |
| 工具分发 | `runTools` 增加新工具 case；拆分超长文件 | `server/internal/chat/tools.go` → 拆出 `tools_dispatch.go` |
| 客户端上下文 | `SendInput.ClientContext`、`toolTurn.clientCtx`、API 请求解析 | `server/internal/chat/service.go`、`server/internal/api/chat.go` |
| 技能注册表 | 工具一对多技能 | `server/internal/skills/skills.go`、`skills_test.go` |
| 存储 | `app_action` 不可 PIN | `server/internal/store/pinned_components.go` |
| 客户端设备与上下文 | `device_id` 生成与持久化、发消息附带 `client_context` | `app/lib/state/session.dart`、`app/lib/core/api.dart` |
| 共享档位 | 字号 / 语速档位常量与就近匹配 | 新建 `app/lib/core/models/app_settings_steps.dart` |
| 动作执行器 | 收到 `app_action` 后执行、幂等去重、撤销、回写状态 | 新建 `app/lib/state/app_actions.dart` |
| 对话状态 | SSE `components` 事件中转发 `app_action` 给执行器 | `app/lib/state/chat.dart`（只加一行） |
| 动作卡片 UI | `AppActionCard`：已调整 / 撤销 / 在本设备应用 / 不支持 | 新建 `app/lib/ui/features/chat/app_action_card.dart` |
| 设置页 | 改用共享档位常量 | `app/lib/ui/features/settings/settings_page.dart`、`app/lib/ui/desktop/desktop_settings_panes.dart` |
| 页面跳转 | 页面枚举到路由 / 桌面侧栏分区的映射 | `app/lib/state/desktop_nav.dart`、`app/lib/app/navigation.dart` |
| 测试 | 执行器幂等 / 撤销 / 跨设备、卡片渲染、宽窄屏 | 新建 `app/test/app_actions_test.dart`、`app/test/app_action_card_test.dart` |
| 文档 | 本方案；`docs/api.md` 补充 `client_context` 与 `app_action` | `docs/proposal-v10-app-control.md`、`docs/api.md` |

**验证**（AGENTS.md 第 3 条）：`go test ./...`；`flutter analyze` 零问题、`flutter test` 全绿；涉及桌面导航改动再跑 `flutter build macos --debug`。

---

## 7. 分期计划

### 第一期：首页管理补强 + 字体 / 主题

- `pin_component` 默认目标解析、`unpin_component` 关键字匹配、`list_home_cards`、`reorder_home_cards`
- `toggle_suggestions_card`
- 客户端动作通道（`client_context`、`app_action` 组件、执行器、幂等）
- `set_app_settings` 仅支持 `font_scale`、`theme`
- `AppActionCard`（已调整 + 撤销）
- 共享字号档位常量

### 第二期：其余设置 + 页面跳转 + 多设备

- `set_app_settings` 增加 `tts_rate`、`haptics`
- `open_app_page`（含桌面侧栏分区映射）
- 其他设备"在本设备应用"

---

## 8. 已确认决策（2026-11-24）

1. ~~**设置作用范围**~~ ✅ 已确认（2026-11-24）：设置改动**只对发出消息的那台设备生效**，不做同账号多设备同步。其他设备打开该对话时只显示"在本设备应用"按钮（第 2.4 节，第二期实现），不会自动执行。
2. ~~**隐私对话**~~ ✅ 已确认：隐私对话里**不能**调字体、主题等设置，`app_control` 技能整体不可用（第 4 节）。
3. ~~**分期范围**~~ ✅ 已确认：按第 7 节划分，第一期 = 首页管理补强 + 字体 / 主题。
4. ~~**主动调整的尺度**~~ ✅ 已确认：用户只表达感受、没有明确要求时**先问再改**（`ask_user_question` 给选项）；明确要求时直接执行（第 3.6 节）。
