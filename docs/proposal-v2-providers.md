# v2 方案：多供应商管理、备用模型、花费统计、App 内首次设置

> 2026-11-17 定稿。Claude 出方案，agy（Gemini）独立做了架构评审，结论一致的部分直接采用，分歧处已由作者拍板。

## 1. 目标

1. 管理员在手机上管理多个模型供应商（OpenAI、OpenRouter、Google Gemini、DeepSeek、阿里云百炼、硅基流动、自定义中转站），添加时只填 Key，模型从上游勾选，参数自动推荐。
2. 主模型出错时自动换「备用模型」回答，家人不看到报错。
3. 用量页显示花费：OpenRouter 返回的实际花费，其他供应商按管理员填写的单价估算。
4. 新服务器不用改配置文件：App 连上空服务器后，用服务端日志里的一次性设置码创建管理员，再引导添加第一个供应商。

## 2. 不做什么（已拍板）

- **不做公开注册 / 多家庭托管。** 原因：国内向公众提供生成式 AI 服务有备案、实名、内容审核要求；平台出钱会被刷、自带 Key 的用户自己就能部署；全局唯一用户名会让登录多一个「家庭号」。替代方案：一键部署脚本（后续）、给朋友单独开容器 + 子域名。数据库不预留 `family_id`，以后需要时一次迁移即可。
- **不做网关式路由**（一个模型挂多个上游、权重、熔断）。只有一个全局「备用模型」。
- **作者自己的中转站不写进开源预设**，只出现在作者服务器的 `.env` 里，首次启动导入为默认供应商；管理员可以删除，删除后不会被重新导入。
- 不暴露 temperature、top_p、自定义 Header 等参数；ASR 暂不并入供应商管理；Key 落盘加密以后再做（可选）。

## 3. 数据模型（迁移 `002_providers_v2.sql`）

| 表 | 新字段 | 说明 |
| --- | --- | --- |
| `providers` | `kind TEXT NOT NULL DEFAULT 'custom'` | 预设类型：`openai` / `openrouter` / `gemini` / `deepseek` / `dashscope` / `siliconflow` / `custom`。不加 CHECK，方便以后加预设 |
| `providers` | `sort INTEGER NOT NULL DEFAULT 0` | 列表顺序 |
| `models` | `input_price REAL NOT NULL DEFAULT 0`、`output_price REAL NOT NULL DEFAULT 0` | 美元 / 百万 token，用于估算花费；OpenRouter 自动填 |
| `messages` | `cost REAL NOT NULL DEFAULT 0` | 本次回答花费（美元）。上游返回 `usage.cost` 就用它，否则按单价估算 |
| `settings` | 键 `fallback_model_id` | 备用模型，空 = 不启用 |
| `settings` | 键 `providers_seeded` | `.env` 默认供应商已导入过，之后不再导入 |

迁移时如果已有供应商，直接写入 `providers_seeded=1`。

## 4. 服务端行为

### 4.1 地址拼接

统一成 `llm.Endpoint(baseURL, path)`：地址**没有路径**（`https://api.openai.com`、`https://cc.miaojiai.com/`）时补 `/v1`；**有路径**（`/v1`、`/api/v1`、`/v1beta/openai`、`/api/v3`、`/compatible-mode/v1`）时原样使用。聊天、拉模型列表、语音转写三处共用。

### 4.2 预设

`GET /api/admin/provider-presets` 返回预设列表（名称、默认地址、一句话介绍、获取 Key 的网址、Key 前缀提示）。客户端不写死预设。

### 4.3 模型推荐参数（`internal/llm/catalog.go`）

`InferModel(kind, upstreamID, meta)` 返回推荐的显示名、简介、能否看图、思考档位、上下文、单价、是否对话模型、是否常用推荐。

- 匹配前去掉 `openai/`、`google/`、`models/` 这类前缀；Gemini 列表里的 `models/` 前缀在入库时去掉。
- OpenRouter 用上游返回的 `context_length`、`architecture.input_modalities`、`supported_parameters`、`pricing`，不靠猜。
- 思考档位：`gpt-4*`、`gpt-3.5`、`deepseek-chat` 等非推理模型**不发** `reasoning_effort`；o 系列、`*-pro`、`*sol*` 日常用 `low`（不接受 `none`）；能关掉思考的（`gpt-5.1+`、`gpt-6*`、Gemini Flash）日常用 `none`，深度思考用 `high`。
- 非对话模型（embedding、tts、whisper、图像、视频、审核、rerank 等）标记 `chat=false`，客户端折叠显示。
- 常用推荐：对话模型，且不是带日期的快照、preview/exp 版本。
- 推荐不一定准，所以每个模型都有「测试」按钮。

### 4.4 OpenRouter

- 请求带 `usage: {include: true}`，从最后一个 usage 块读 `cost`。
- 思考参数用 OpenRouter 的 `reasoning: {effort}` 格式。
- 带 `HTTP-Referer` / `X-Title` 标识来源。

### 4.5 备用模型

主模型（已重试一次）失败、**还没输出任何字**、不是用户取消或服务关闭时，换 `fallback_model_id` 重新组装请求（看图能力、思考档位、上下文都按备用模型重算）再试一次。消息的 `model_id` 记成实际回答的模型，用量也算在它头上。备用模型和主模型相同或不可用时跳过。

### 4.6 选模型的兜底

- 这条消息带图片、而选中的模型不能看图时，改用第一个能看图的可用模型（顺序：备用 → 默认 → 列表顺序）。用户在对话里手动选的模型同样适用，因为不换的话图片会被丢掉。
- 管理员删除 / 停用供应商或模型后，如果默认模型、备用模型、标题模型失效：默认模型自动改成第一个可用模型（优先能看图的），备用和标题模型清空。

### 4.7 Key 安全

- 列表只返回打码的 Key：长度 ≥ 16 显示前 3 位 + 后 4 位，否则只显示 `****`。
- **改供应商地址的域名时必须重新填 Key**，防止有人把地址改到自己的服务器上，套走已存的 Key。测试连接接口同理。

### 4.8 首次设置

- 没有任何账号、也没配 `NUAN_ADMIN_PASSWORD` 时，服务端照常启动，在日志里打印一次性设置码（每次启动重新生成）。
- `GET /api/setup` → `{needs_setup}`；`POST /api/setup` `{code, username, password, display_name}` → 创建管理员并直接返回登录令牌（和登录接口同格式）。设置码错误按 IP 限流。
- 配了 `NUAN_ADMIN_PASSWORD` 的老部署方式不变。

## 5. 管理员接口（新增 / 变更）

| 接口 | 说明 |
| --- | --- |
| `GET /api/admin/provider-presets` | 预设列表 |
| `GET /api/admin/providers` | 增加 `kind`、`sort`、`model_count`、`enabled_model_count`、`endpoint`（实际请求地址） |
| `POST/PATCH /api/admin/providers[/{id}]` | 增加 `kind`、`sort`；改域名不带 Key → `400 key_required` |
| `POST /api/admin/providers/test` | `{id?, kind, base_url, api_key?}`，拉一次 `/models`，返回 `{ok, model_count, latency_ms, message}`，失败也是 200 + `ok:false` |
| `GET /api/admin/providers/{id}/upstream-models` | 改为 `{items:[推荐参数对象]}`，带 `existing_model_id` |
| `POST /api/admin/models/batch` | `{provider_id, models:[...]}` 批量添加；没有默认模型时把第一个设为默认 |
| `POST /api/admin/models/{id}/test` | `{think}`，用真实配置发一句话，返回 `{ok, reply, first_token_ms, duration_ms, message}` |
| `PUT /api/admin/models/order` | `{ids:[...]}`，按顺序重写 `sort` |
| `GET/PUT /api/admin/settings` | 增加 `fallback_model_id`；校验模型存在 |
| `GET /api/admin/usage?days=` | 每人增加 `cost`；新增 `models`（按模型汇总）和 `total` |
| 删除供应商 / 模型、停用 | 返回或触发 4.6 的默认模型修复 |

## 6. App 页面

设置 →「家庭管理」：家庭成员 / **模型与供应商** / **用量与花费**。

1. **模型与供应商**（独立页面）
   - 全家使用：默认模型、备用模型、自动标题模型，点击弹出模型选择。
   - 家人能看到的模型：可拖动排序，开关直接启停，点进去编辑。
   - 供应商卡片：类型图标、名称、域名、Key 打码、模型数、停用标记；「添加供应商」。
   - 没有供应商时显示空状态，引导添加。
2. **添加供应商**（两步）：选预设（卡片网格，附一句话介绍）→ 填 Key（粘贴按钮、显示/隐藏、「去哪里获取 Key」链接；预设的地址收在「高级」里，自定义时实时显示实际请求地址）→「测试并保存」→ 直接进入选模型。
3. **供应商详情**：启停、改名称/地址/Key、测试连接、本供应商的模型列表、「从上游添加模型」「手动添加」、删除（提示会连带删除几个模型、默认模型会变成什么）。
4. **从上游选模型**：搜索框；筛选「常用 / 能看图 / 可深度思考」；分组：常用推荐、其他对话模型、非对话模型（折叠）；已添加的置灰打勾；每行显示看图/思考标签、上下文、单价；底部「添加 N 个模型」。
5. **模型编辑**：显示名、简介、能否看图、思考设置（平时回答：快速 / 稍作思考 / 模型默认；深度思考：关闭 / 低 / 中 / 高；「高级」里可直接填原始值）、上下文长度（常用档位 + 自定义）、单价、「测试」「测试深度思考」、删除。
6. **用量与花费**：7 / 30 / 90 天切换；总花费和回答数；按成员、按模型列出花费和 token。
7. **首次设置**：登录页检测到服务器需要初始化时，引导填设置码和管理员账号，完成后直接进入「添加供应商」。
8. 其他：App 回到前台超过 5 分钟自动刷新模型列表；修复成员停用状态读错字段的问题。

## 7. 实施顺序

1. 服务端：迁移、地址拼接、预设、推荐参数、新接口、Key 安全规则。
2. 服务端：备用模型、看图兜底、默认模型修复、花费记录、首次设置。
3. App：上面 6 中的页面。
4. 文档（`docs/api.md`、README）、测试、模拟器实测、Codex 代码评审。
