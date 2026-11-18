# v4 方案：隐私对话（无痕 / 临时聊天）

> 2026-11-17 定稿。对标 ChatGPT Temporary Chat 与 Gemini 临时对话，结合「小暖」家庭共用设备与跨模型记忆（v3）的实际场景设计。

## 1. 背景与核心决策

v3 上线「跨模型共享记忆」后，小暖默认在每次对话前注入常驻记忆与往事检索，并在回复完成后自动抽取长期事实与对话概括（`episode`）。但在家庭场景下，家人经常有明确不想留痕的提问需求：

- 给爱人、父母挑惊喜礼物（不能进历史记录，更不能被写进「近期在做」后在下次聊天时说漏嘴）；
- 替亲友问敏感病情、私密健康困扰（不希望混入本人的健康记忆，也不希望小暖拿本人的旧病史往上套）；
- 临时查一件一次性琐事、测试提示词，不想弄乱左侧聊天记录。

已拍板的工程决策：

1. **客户端命名统一为「隐私对话」。** 对长辈而言，「隐私对话」比「无痕模式」「临时会话」更直白；欢迎页用三行大白话写清「不进聊天记录 · 不读取也不写入记忆 · 关闭后消息与图片自动清理」。
2. **复用现有流式会话管线（数据库短期承载 + 主动销毁 + 服务端定时兜底），不另起一套客户端自带全量历史的纯无状态接口。**
   - 原因：小暖的核心体验建立在「生成与 HTTP 解耦（SSE 快照重连）」「多轮画图与按图编号改图（`generate_image`）」「修改重发 / 换模型重答」之上。如果把隐私对话做成客户端拼全量消息发给服务端的一套平行接口，切后台断流续传、画图工具保存附件、按 `图1/图2` 改图全部要重写一套。
   - 做法：`conversations` 与 `files` 表增加布尔列 `private`（0/1）。对话存活期间走同一套生成、SSE、看图、画图链路；**一旦客户端关闭/离开隐私对话立即调 `DELETE` 物理销毁会话与关联图片**；若客户端直接划掉进程、断网或掉电没来得及发 `DELETE`，**服务端后台每 10 分钟清理超过 2 小时未活动的隐私对话及孤儿隐私图片**。
3. **记忆彻底「双盲」（既不写，也不读）。**
   - **不写入**：不触发实时关键词 `Kick`、不进入后台每分钟扫描候选集、事务提交层硬拦截，连 `episode` 对话概括也绝不生成。
   - **不读取**：生成前不注入常驻记忆块，也**不调用向量模型做检索**（既避免把用户过往记忆带进隐私提问，也避免把隐私问题发往外部 embedding 接口）。
   - **诚实告知模型**：系统提示词明确告知小暖当前处于「隐私对话」，若家人说「记住这件事」，小暖会温和说明隐私对话关闭后不留存，而不会虚假答应「我已经记下了」。
4. **`private` 属性创建时锁定，不可逆转。** 普通对话不能改成隐私对话，隐私对话也不能转正成普通对话，也不能置顶；向普通对话传入 `private: true` 直接返回 `400 private_mismatch`，杜绝状态错位导致隐私消息落入普通历史。
5. **每日画图额度照常累计。** `image_log` 只记 `(user_id, model_id, cost, created_at)`，不含提示词、消息或文件 ID，保留额度记录可防止通过反复开关隐私对话绕过家庭每日画图上限，同时不泄露任何聊天内容。

---

## 2. 四重隐私保障（不变量）

| 维度 | 保障规则 | 代码落点 |
|---|---|---|
| **1. 对话历史与搜索隔离** | ① `GET /api/conversations`（含 `?q=` 全文搜索会话标题与消息正文）固定过滤 `c.private = 0`；<br>② 首轮回复不启动 `generateTitle`（不额外调标题模型、不写标题）；<br>③ 客户端绝不把隐私对话写入 `conversationsProvider`，绝不落盘 `conversations.json` 与 `conv_<id>.json`。 | `store/conversations.go`<br>`chat/service.go`<br>`app/lib/state/chat.dart` |
| **2. 记忆四道防线** | ① **生成前（注入层）**：`conv.Private` 时跳过 `Memory.Inject`（0 次记忆查库、0 次向量请求）；<br>② **回复后（触发层）**：`FinishMessage` 后跳过 `MemoryWorker.Kick`；<br>③ **扫描层**：`Store.MemoryCandidates` SQL 固定 `c.private = 0`；<br>④ **执行与事务层**：`Worker.Process` 与 `Store.ApplyMemoryExtraction` 双重校验 `private`，即使被误调也直接返回、不调模型不入库。 | `chat/service.go`<br>`store/memories.go`<br>`memory/worker.go` |
| **3. 图片与临时文件即用即清** | ① 隐私对话上传图片带 `?private=1`，AI 画图自动继承 `private = 1`；<br>② `GET /api/files/{id}` 对隐私文件返回 `Cache-Control: private, no-store`，禁止磁盘长缓存；<br>③ 删除隐私对话时，在同一数据库事务内清理未被其他消息引用的 `files` 记录，并在提交后**立即 `os.Remove` 删除磁盘文件**；<br>④ 上传后未发出的孤儿隐私文件，宽限期从常规 24 小时缩短为 **15 分钟**；<br>⑤ 客户端退出隐私对话时，同步清理输入框草稿及相册压缩产生的本地临时图片文件。 | `api/files.go`<br>`chat/tools.go`<br>`chat/cleanup.go`<br>`store/images.go`<br>`app/lib/state/chat.dart`<br>`app/lib/ui/composer.dart` |
| **4. 生命周期双重销毁** | ① **主动即刻销毁**：点「退出」、点「新对话」、打开任意历史对话、切换账号、退出登录前，客户端立即取消生成并调用 `DELETE /api/conversations/{id}`；<br>② **飞行中竞态保护**：消息还在上传或刚发出尚未返回会话 ID 时用户就点了退出，待请求一返回立即补发 `DELETE`，绝不残留；<br>③ **服务端定时兜底回收**：进程启动后及每 **10 分钟**运行 `CleanupPrivate`，自动清除 `updated_at < now - 2h`（且无活跃流）的隐私对话及其磁盘文件。 | `app/lib/state/chat.dart`<br>`app/lib/state/session.dart`<br>`chat/cleanup.go`<br>`cmd/server/main.go` |

---

## 3. 数据模型与迁移（007）

SQLite 与 Postgres 迁移版本号严格对齐（`007_private_chat.sql`），沿用既有整数布尔约定（`0` / `1`），`nuan-server migrate-db` 原生自动覆盖新列。

### 3.1 SQLite (`migrations/sqlite/007_private_chat.sql`)

```sql
ALTER TABLE conversations ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
CREATE INDEX conversations_private_updated ON conversations(updated_at) WHERE private = 1;
ALTER TABLE files ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
CREATE INDEX files_private_created ON files(created_at) WHERE private = 1;
```

### 3.2 Postgres (`migrations/postgres/007_private_chat.sql`)

```sql
ALTER TABLE conversations ADD COLUMN private BIGINT NOT NULL DEFAULT 0;
CREATE INDEX conversations_private_updated ON conversations(updated_at) WHERE private = 1;
ALTER TABLE files ADD COLUMN private BIGINT NOT NULL DEFAULT 0;
CREATE INDEX files_private_created ON files(created_at) WHERE private = 1;
```

部分索引（Partial Index）只索引 `private = 1` 的极少数行，体积趋近于 0，使每 10 分钟的 `CleanupPrivate` 扫描开销可以忽略不计。

---

## 4. 服务端接口与内部流程

### 4.1 接口变更摘要

1. **`Conversation` 对象**：新增 `"private": false`（`bool`）。
2. **`POST /api/chat`**：
   - 请求体新增可选布尔字段 `"private": false`。
   - `conversation_id` 为空时：按 `private` 创建普通或隐私会话。
   - `conversation_id` 非空时：若会话本身 `private == true`，后续轮次无论是否重传 `private` 均保持隐私对话属性；若会话是普通对话但请求传了 `"private": true`，返回 `400 private_mismatch`（`不能把隐私消息发到普通对话中`）。
3. **`POST /api/conversations`**：请求体新增可选 `"private": false`；`private` 为 `true` 时强制 `pinned = false`。
4. **`PATCH /api/conversations/{id}`**：不接受修改 `private`；若目标是隐私对话，忽略/禁止 `pinned = true`。
5. **`DELETE /api/conversations/{id}`**：
   - 先取消该会话正在运行的生成任务并等待 Goroutine 收尾（含正在进行的画图落盘）；
   - 使用独立超时上下文（防止手机端发完请求立刻断连导致清理半途中断）；
   - 若该会话 `private == true`，事务内一并查出其消息附件的 `file_id`，删除会话（级联删除全部 `messages`）及无引用的 `files` 行，提交后立即删除磁盘图片文件。
6. **`POST /api/files` 与 `GET /api/files/{id}`**：
   - `POST /api/files?private=1`（或表单字段 `private=1`）将上传文件标记为 `private = 1`；
   - `GET /api/files/{id}` 对 `private == true` 的文件返回 `Cache-Control: private, no-store`。

### 4.2 定时清理参数

| 常量 | 值 | 含义 |
|---|---|---|
| `PrivateConvTTL` | `2h` | 隐私对话静默超过 2 小时（且当前无流式生成）视为客户端已脱离，服务端自动物理销毁会话与附件 |
| `PrivateFileGrace` | `15m` | 标记 `private = 1` 但未被任何消息引用的孤儿上传文件，超过 15 分钟即清理 |
| `FileGrace` | `24h` | 普通对话删除或改写后遗留的无引用文件，保持 24 小时清理周期不变 |
| 扫描周期 | 启动 15 秒后 + 每 10 分钟 | `CleanupPrivate` 后台巡检频率 |

> **为什么切后台不立刻删，而是静默 2 小时兜底？**
> 家人在手机上提问时，经常需要切去微信复制一段聊天、去相册找化验单照片（打开系统相机/相册也会触发 App 进入 `paused` 状态）。只要 App 进程还活着且用户没点退出，切回小暖应当能继续看回答；一旦点退出/切换对话立即秒删，直接划掉 App 进程则本机 0 缓存残留、服务端最多 2 小时彻底清空。

---

## 5. 客户端交互与适老化设计

家庭应用最大的交互风险是：**长辈不小心点进了隐私模式却以为是普通聊天（回头找不到记录），或者以为在隐私模式其实在普通模式。** 因此界面必须在第一眼到最后一刻都给出清晰但温和的区分：

1. **新建对话页底部虚线框直达入口**：
   - 新建对话欢迎页底部提供带圆角虚线框的 **「开启隐私对话」** 卡片（写明「不留聊天记录 · 关闭后自动清除，小暖不会记住」），对话内容页顶栏右侧让位给「分享对话」与「新对话」，左侧历史抽屉顶部仅保留全宽「新对话」按钮，职责清晰不拥挤；
2. **模式感知（沉静石板蓝配色 vs 日常暖橙色）**：
   - 进入隐私对话后，顶栏标题变为带无痕图标的 **「隐私对话」**（选了角色则为「隐私对话 · 健康顾问」），模型选择器照常可用；
   - 顶栏右侧变为醒目的 **「× 退出」胶囊按钮**，长辈无需理解图标含义，点「退出」即可结束并回到日常小暖，弹出轻提示「隐私对话已清除」；
   - **专属欢迎页**：展示无痕徽标与三项清晰说明（不进聊天记录 / 不读取也不写入记忆 / 关闭即清理），底部仍可挑选「健康顾问」「防骗卫士」等助手且**保持在隐私模式内**；
   - **对话中常驻提示条**：一旦开始收发消息，顶栏下方保留一行轻量横幅「隐私对话中 · 离开后记录与图片自动清除，小暖不会记住」；
   - **输入框联动**：提示词改为「在隐私对话中提问…」，发送按钮切换为石板蓝渐变；退出隐私模式时自动清空未发出的文字草稿与已选临时图片，防止草稿串回普通对话；
   - **画图等待卡片文案适配**：隐私模式下不再显示「离开这个页面也没关系，画好会留在这里」，改为「隐私对话关闭后会清除，画好后可保存到手机相册」。

---

## 6. 验收清单

- [x] SQLite 与 Postgres 迁移版本一致，`migrate-db` 双向迁移测试通过。
- [x] 隐私对话不出现在 `GET /api/conversations` 列表与 `?q=` 搜索结果中，不触发自动标题生成。
- [x] 用户已有常驻记忆与 `episode` 时，隐私对话请求上游不含任何记忆内容，也不发起 embedding 查询。
- [x] 在隐私对话中发送「记住我对芒果过敏」等强触发词：不调用 `MemoryWorker.Kick`、`MemoryCandidates` 不返回、`Worker.Process` 与 `ApplyMemoryExtraction` 均不产出记忆或概括。
- [x] 在隐私对话中上传图片或让 AI 画图：`GET /api/files/{id}` 返回 `no-store`；调用 `DELETE /api/conversations/{id}` 后，消息、数据库文件记录与磁盘文件全部立即清除，`GET /api/files/{id}` 立即返回 `404`。
- [x] 未主动 `DELETE` 的过期隐私对话与孤儿隐私上传文件，可被 `CleanupPrivate` 自动彻底清理，活跃流式中的隐私对话不会被误删。
- [x] 向已有普通对话发送 `private: true` 请求被 `400 private_mismatch` 拒绝。
- [x] 客户端冷启动缓存、会话列表、登出前清理、飞行中退出竞态、特大字号（`1.3x`）布局与深浅色主题全部验证通过。
