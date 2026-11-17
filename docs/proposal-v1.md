# 家用 AI 对话客户端 —— v1 技术方案草案（待评审）

## 背景

- 用户：开发者本人 + 家人（父母、爱人），总共 3–10 人。对标「开源版豆包」：打开就能聊、字大、好看、流畅。
- 上游：开发者自己的 OpenAI 兼容中转站（`/v1/chat/completions`，已实测流式、usage、图片输入、tool calling 都可用；模型有 gpt-6.1-terra、gemini-4-carbon、claude-*、gpt-image-* 等）。
- 未来能力：长期记忆、角色卡（智能体）、联网搜索（tool calling）、生图。v1 只做标准大模型对话，但数据模型和模块边界要给这些留好位置。
- 开发者熟悉 Node.js，不熟 Go / Rust。要求「越高性能越好维护越好」。
- 客户端：Flutter（Android 优先，iOS 以后）。

## 服务端选型：Go（倾向）

| | Go | Rust (axum) | Node (Fastify) |
|---|---|---|---|
| 性能 | 足够（瓶颈在上游 LLM，单机万级并发流） | 最高 | 够用 |
| 维护难度（对 Node 开发者） | 低：语法少、读得懂、无生命周期/借用 | 高：改一行可能要跟编译器斗 | 最低 |
| 部署 | 单个静态二进制 + 一个 SQLite 文件，交叉编译无 cgo | 单二进制 | 要 Node 运行时 |
| 长连接/流式 | goroutine + context 取消，天然适合 | async，可以但复杂 | 可以 |
| AI 写代码质量 | 高，惯用法统一 | 高 | 高 |

结论倾向 Go 1.26：标准库 `net/http`（1.22+ 路由已支持方法和路径参数），不引入 Web 框架。

### 关键依赖

- 数据库：SQLite（`modernc.org/sqlite`，纯 Go，免 cgo），WAL 模式，单写连接 + 多读连接池。家庭规模下性能远超需要；备份就是复制一个文件。存储层封装成 `store` 包接口，将来要换 Postgres 只改一层。
- 迁移：内嵌 SQL 文件，启动时按版本号顺序执行（自写 50 行，不引入 goose 等）。
- 密码：argon2id（`golang.org/x/crypto`）。
- 日志：`log/slog`。
- 配置：环境变量 + 可选 `.env`。

### 认证

- 不开放注册。首次启动用环境变量创建管理员；管理员在 App 里给家人建账号（用户名 + 密码），也可以生成一次性邀请码。
- 登录后发不透明 session token（随机 32 字节，库里存 SHA-256），默认 180 天、滑动续期。不用 JWT：家用场景要能随时吊销，token 校验走内存 LRU 缓存，性能不是问题。

### 核心：生成任务与客户端解耦（体验关键）

手机网络会断、App 会被切到后台。所以：

1. `POST /api/conversations/{id}/messages` 写入用户消息 + 一条 `status=streaming` 的助手占位消息，**在服务端起一个独立 goroutine 调上游**，和 HTTP 请求的生命周期解耦。
2. 该请求随即以 SSE 返回这条生成的事件流（`delta`、`reasoning`、`title`、`done`、`error`），每个事件带递增序号。
3. 内存里的 `RunHub` 保存每个进行中生成的已累计文本与订阅者；客户端断开后可 `GET /api/messages/{id}/stream?after=<seq>` 重新接上，先补齐再继续推。
4. 生成过程中每 ~1s 或每 N 字落盘一次；结束时写最终内容、usage、耗时。服务重启时把残留的 `streaming` 标记为 `interrupted`。
5. 停止生成：`POST /api/messages/{id}/cancel`，取消 context。

### 数据模型（v1）

- `users`（id, username, display_name, avatar, role[admin|member], password_hash, settings_json, created_at）
- `sessions`（token_hash, user_id, device, created_at, last_seen_at, expires_at）
- `providers`（id, name, base_url, api_key, kind=openai_compat）—— 只有管理员可见，key 永不下发客户端
- `models`（id, provider_id, upstream_model, display_name, description, capabilities_json{vision,tools,reasoning}, enabled, sort, is_default）
- `assistants`（id, owner_id|null(系统内置), name, avatar, system_prompt, model_id, greeting, suggestions_json, ...）—— v1 内置几个（通用助手、翻译、写作…），就是未来「角色卡」的雏形
- `conversations`（id, user_id, assistant_id, title, model_id, pinned, archived, created_at, updated_at, last_message_at）
- `messages`（id, conversation_id, role, content, reasoning, attachments_json, status[done|streaming|error|interrupted|cancelled], model_id, usage_json, error, created_at）—— 线性历史；「重新生成」替换最后一条助手消息，「编辑」截断其后的消息。
- `files`（id, user_id, sha256, mime, size, path, width, height, created_at）—— 图片问答用，图片压缩后上传，发给上游时转 base64 data URL。
- ID 用 UUIDv7（时间有序，客户端可离线生成）。

未来：`memories`（用户记忆条目 + 向量）、`tools`、assistant 增加 tools/knowledge 字段——都是加表/加列，不改现有结构。

### API 一览（REST + SSE，JSON）

```
POST /api/auth/login            GET  /api/me     PATCH /api/me     POST /api/auth/logout
GET  /api/models                GET  /api/assistants
GET  /api/conversations?cursor= POST /api/conversations   PATCH/DELETE /api/conversations/{id}
GET  /api/conversations/{id}/messages?before=
POST /api/conversations/{id}/messages        → SSE
POST /api/conversations/{id}/regenerate      → SSE
GET  /api/messages/{id}/stream?after=        → SSE（断线续传）
POST /api/messages/{id}/cancel
POST /api/files (multipart)     GET /api/files/{id}
admin: /api/admin/users, /api/admin/providers, /api/admin/models
GET /healthz
```

### 其他

- 自动标题：首轮回答结束后用便宜模型异步生成，通过 SSE `title` 事件推给客户端。
- 上下文管理：按模型上下文窗口粗略估算 token，超长时从最早的消息裁剪（v1 不做摘要）。
- 并发限制：每个用户同时最多 2 个生成；上游 429/5xx 自动重试一次（只在首字节前）。
- 部署：Dockerfile（distroless）+ 直接跑二进制两种方式；数据目录 `./data`。
- 测试：store 层和 RunHub 单测；用假上游（httptest）跑端到端流式测试。

## 客户端（Flutter）要点

- 首页即对话（像豆包）：大输入框、建议问题、图片按钮；左侧抽屉是历史会话（按今天/昨天/7 天内分组，可搜索）。
- 流式 Markdown 渲染（代码块、表格、公式），逐字出现但不抖动；自动滚动，用户上滑时停止跟随。
- 消息操作：复制、重新生成、朗读（系统 TTS）、编辑重发、停止生成。
- 适老化：字号设置（标准 / 大 / 特大），默认就偏大；按钮大、对比度高；中文全部本地化。
- 本地缓存会话列表与最近消息，冷启动先显示缓存再刷新。
- 状态管理：Riverpod 或简单的 ChangeNotifier（待定）。

## 想请评审的问题

1. Go vs Rust：在「开发者只熟 Node、但大部分代码由 AI 写」的前提下，Go 是否是最佳平衡？有没有低估 Rust 或 Node 的地方？
2. SQLite（modernc 纯 Go）够不够？未来「记忆」需要向量检索，家庭规模下是用 Go 内存暴力余弦，还是现在就该选 Postgres + pgvector？
3. 生成与请求解耦 + RunHub 断线续传的设计有没有坑？SSE vs WebSocket？
4. 线性历史 vs 消息树（分支）：对家用场景 v1 是否够？
5. 认证用不透明 token 是否合适？
6. 还缺什么家用场景必需的东西（比如语音输入：中转站没有 ASR 模型，安卓国产机系统语音识别不一定可用）？
