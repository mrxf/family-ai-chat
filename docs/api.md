# 小暖 (Nuan) 服务端 API 参考文档

本文档基于 `server/` 源码整理，描述「小暖」Go 后端暴露的全部 HTTP 与 SSE 接口。

---

## 1. 通用约定

### 1.1 基础信息与数据格式

- **编码与 Content-Type**：除文件上传（`multipart/form-data`）、文件/安装包下载及 SSE 流（`text/event-stream; charset=utf-8`）外，其余接口均使用 `application/json; charset=utf-8`。JSON 请求体最大允许 **2 MB**。
- **时间戳**：所有时间字段（`created_at`、`updated_at` 及会话分页游标 `before`）均为 **Unix 毫秒时间戳（`int64`）**。
- **主键 ID**：运行时生成的实体 ID 均为按时间有序的 **UUIDv7** 字符串（首次启动自动注入的记录使用固定可读 ID，如 `default`、`m-gpt-6.1-terra`、`builtin-health` 等）。

### 1.2 身份鉴权

不开放公开注册，采用服务端可随时吊销的不透明 Session Token（随机 32 字节 Base64URL 字符串，数据库仅存 SHA-256 摘要）：

- **有效期**：默认 **180 天**，客户端活跃访问时自动滑动续期（最多每小时写库刷新一次）。
- **传递方式**（二选一）：
  1. HTTP 请求头（推荐）：`Authorization: Bearer <token>`（`Bearer` 不区分大小写）
  2. URL 查询参数：`?access_token=<token>`（便于浏览器 `EventSource` 订阅 SSE 或图片组件直接加载 `/api/files/{id}`）
- **权限级别**：
  - `无需鉴权`：公开可访问
  - `需登录`：任意有效登录用户（`member` 或 `admin`）
  - `需管理员`：已登录且 `user.role == "admin"`

### 1.3 统一错误格式

所有 `/api/*` 接口（以及未匹配的 404 路由）在出错时均返回统一 JSON 结构：

```json
{
  "error": {
    "code": "unauthorized",
    "message": "登录已过期，请重新登录"
  }
}
```

> 例外：`GET /app/download/{name}` 为浏览器直接下载路由，文件名非法或文件不存在时由标准库返回纯文本 `404 page not found`。

#### 通用错误码速查

| HTTP 状态码 | `error.code` | 默认 `message` | 触发场景 |
| :---: | :--- | :--- | :--- |
| `400` | `bad_request` | 请求格式不正确 | JSON 解析失败或请求体超过 2MB |
| `401` | `unauthorized` | 登录已过期，请重新登录 | 未传 Token、Token 无效/过期，或所属账号已被停用 |
| `403` | `forbidden` | 需要管理员权限 / 只有管理员可以修改共享助手 | 普通成员访问 `/api/admin/*` 或修改共享助手 |
| `404` | `not_found` | 接口不存在 / 内容不存在或已被删除 | 路由未匹配、目标资源不存在或不属于当前用户 |
| `409` | `conflict` | 已存在同名项目 | 唯一约束冲突（如用户名重复，不区分大小写） |
| `409` | `conversation_busy` | 上一条回答还没结束，请稍等或先停止 | 同一会话已有正在生成的回答 |
| `429` | `busy` | 同时进行的对话太多了，请等当前回答完成 | 当前用户并发生成数已达上限（单用户最多 **3** 个） |
| `503` | `no_model` | 还没有可用的模型，请联系管理员配置 | 没有任何处于启用状态（模型与所属供应商均启用）的模型 |
| `500` | `internal` | 服务器出了点问题，请稍后再试 | 未预期服务端内部错误 |

---

## 2. 核心数据对象结构

### 2.1 `User`（用户）

```json
{
  "id": "01933c8a-7f00-7000-8000-000000000001",
  "username": "mama",
  "display_name": "妈妈",
  "avatar": "🌸",
  "role": "member",
  "settings": {},
  "disabled": false,
  "created_at": 1763280000000,
  "updated_at": 1763280000000
}
```

- `role`：`"admin"` 或 `"member"`。
- `settings`：客户端自由读写的 JSON 对象（默认 `{}`，上限 16KB，可用于存字号等偏好）。

### 2.2 `PublicModel`（面向普通用户的模型信息）

```json
{
  "id": "m-gpt-6.1-terra",
  "display_name": "GPT-6.1 Terra",
  "description": "聪明均衡，日常首选",
  "vision": true,
  "think": true
}
```

- `vision`：是否支持图片输入。
- `think`：是否支持开启「深度思考」（当后台模型配置了非空 `think_effort` 时为 `true`）。

### 2.3 `Assistant`（助手 / 角色卡）

```json
{
  "id": "builtin-health",
  "owner_id": null,
  "name": "健康顾问",
  "avatar": "🩺",
  "description": "看懂体检报告、用药说明，日常健康问题",
  "system_prompt": "你是一位耐心细致的家庭健康顾问……",
  "greeting": "你好，我是健康顾问。体检报告、药品说明书看不懂？拍张照片发给我就行。",
  "suggestions": [
    "帮我看看这份体检报告",
    "血压 145/90 需要注意什么？"
  ],
  "model_id": null,
  "temperature": null,
  "sort": 0,
  "created_at": 1763280000000,
  "updated_at": 1763280000000
}
```

- `owner_id`：为 `null` 表示全局共享助手（内置或管理员创建），非 `null` 表示该用户私有助手。
- `model_id` / `temperature`：可选覆盖项，未指定时为 `null`。

### 2.4 `Conversation`（会话）

```json
{
  "id": "01933c90-1200-7000-8000-000000000010",
  "assistant_id": "builtin-health",
  "model_id": "m-gpt-6.1-terra",
  "title": "体检报告指标解读",
  "pinned": false,
  "private": false,
  "created_at": 1763280100000,
  "updated_at": 1763280150000
}
```

- `private`：是否为「隐私对话」（无痕 / 临时聊天，默认 `false`；创建后不可更改，详见 `docs/proposal-v4-private-chat.md`）。

### 2.5 `Message`（消息）与 `Attachment`（图片附件）

```json
{
  "id": "01933c90-1210-7000-8000-000000000012",
  "conversation_id": "01933c90-1200-7000-8000-000000000010",
  "role": "assistant",
  "content": "从这份化验单来看，各项主要指标都在正常范围内……",
  "reasoning": "用户上传了一张血常规化验单，先核对白细胞、血红蛋白和血小板……",
  "attachments": [],
  "status": "done",
  "error": "",
  "model_id": "m-gpt-6.1-terra",
  "prompt_tokens": 1420,
  "completion_tokens": 386,
  "first_token_ms": 1150,
  "duration_ms": 6420,
  "created_at": 1763280100050,
  "updated_at": 1763280106470
}
```

- `role`：`"user"` 或 `"assistant"`。
- `status` 取值：
  - `"streaming"`：正在生成中
  - `"done"`：正常完成（用户消息固定为 `"done"`）
  - `"cancelled"`：用户主动点击停止（保留已生成的部分文本）
  - `"interrupted"`：服务端停机或重启导致中断（保留停机前已生成的部分文本）
  - `"error"`：上游请求失败（此时 `error` 字段保存面向用户的中文错误提示，如 `"模型服务暂时不可用，请稍后再试"`）
- `attachments` 元素结构（`width`、`height` 为 `0` 时因 `omitempty` 省略）：
  ```json
  {
    "file_id": "01933c8f-9900-7000-8000-000000000009",
    "mime": "image/jpeg",
    "width": 1280,
    "height": 960
  }
  ```
  助手消息的 `attachments` 是小暖在这条回复里**画的图**（JPEG，属于对话的用户，同样通过 `GET /api/files/{file_id}` 读取），额外带 `prompt` 字段：画这张图用的提示词；在已有图片上修改出来的图还带 `source` 字段：原图的 `file_id`。

  **图片编号与改图**：一段对话里的所有图片（用户发的照片和小暖画的图）按时间顺序统一编号「图1」「图2」…，服务端会把编号写进发给模型的历史。模型调用 `generate_image` 时填 `image: "图N"` 就是在那张图上修改：服务端把原图（长边不超过 2048px 的 JPEG）发给画图模型的 `/v1/images/edits`（OpenRouter 则放进 chat 请求）。画图模型不支持编辑、而原图是小暖画的时，改用「原提示词 + 修改要求」重新生成一张。

  **文件清理**：
  - **隐私对话文件**：删除隐私对话（`DELETE /api/conversations/{id}`）时，其消息关联且无其他引用的图片记录与磁盘文件会**立即同步删除**；服务端每 **10 分钟**巡检一次，自动清理超过 **2 小时**未活动的过期隐私对话及其文件，以及上传时标记了 `private=1` 但超过 **15 分钟**仍未被消息引用的孤儿文件。
  - **普通对话文件**：服务端每小时清理一次没有任何消息引用、且创建超过 **24 小时**的文件（删除的对话、被编辑或重新生成掉的消息、上传后没发出去的图片、管理员的测试图），以及磁盘上已没有对应记录的文件（例如被删除用户的文件）。

---

## 3. SSE 流式协议与客户端重连策略

「小暖」将**发起对话**与**订阅流式输出**拆分为两个独立请求，使生成过程完全独立于手机端单次 HTTP 连接的生命周期：

1. 客户端调用 `POST /api/chat`（或 `POST /api/conversations/{id}/regenerate`），服务端同步创建 `status: "streaming"` 的助手占位消息并在后台 Goroutine 启动生成，**立即返回 HTTP 200 JSON**（内含 `assistant_message.id`）。
2. 客户端随后连接 `GET /api/messages/{assistant_message.id}/stream` 接收实时 SSE 事件流。

### 3.1 SSE 事件帧定义

每个事件遵循标准 SSE 文本格式 `event: <类型>\ndata: <单行 JSON>\n\n`：

#### (1) `snapshot` —— 全量状态快照（每次连接必收到的第一帧）

- **任务仍在生成中时**：
  ```text
  event: snapshot
  data: {"content":"已累积的正文","reasoning":"已累积的思考过程","status":"streaming","title":"","activity":"","activity_at":0,"attachments":[]}
  ```
- **连接时任务已结束（已落盘回放）**：
  ```text
  event: snapshot
  data: {"content":"最终完整正文","reasoning":"最终完整思考过程","status":"done","attachments":[]}
  ```
  > 注意：回放分支下 `snapshot` 不含 `title` 字段，`status` 为数据库中的终态（`"done"` / `"error"` / `"cancelled"` / `"interrupted"`），紧接着服务端会立即发送一帧 `done` 并关闭连接。

#### (2) `delta` —— 增量文本片段

服务端以最小 **35ms** 间隔（约 30fps）合并上游 Token 突发后推送，慢客户端不会阻塞上游生成：

```text
event: delta
data: {"reasoning":"新增思考片段","content":"新增正文片段"}
```

- `data` 中仅包含本次有新增的键（可能只有 `"reasoning"`、只有 `"content"`，或两者同时存在）。
- 客户端收到 `delta` 后应**追加（append）**到本地缓存的 `reasoning` / `content` 字符串末尾。

#### (3) `title` —— 自动生成会话标题通知

当新会话原本 `title` 为空，后台异步标题生成完成且此时流仍处于连接状态时推送：

```text
event: title
data: {"conversation_id":"01933c90-1200-7000-8000-000000000010","title":"体检报告指标解读"}
```

#### (4) `activity` —— 工具正在做什么（画图）

聊天模型调用工具时推送，`text` 为空字符串表示结束；`at` 是开始时间（Unix 毫秒），重连后用它接着显示「已经 N 秒」：

```text
event: activity
data: {"text":"正在画图…","at":1763280103000}
```

#### (5) `attachments` —— 这条回复已画好的图（全量）

每画好一张图推送一次，`attachments` 是到目前为止的**完整列表**（直接替换本地列表）：

```text
event: attachments
data: {"attachments":[{"file_id":"...","mime":"image/jpeg","width":1254,"height":1254,"prompt":"A cute puppy ..."}]}
```

#### (6) `done` —— 生成结束终态帧（最后一帧）

无论正常完成、用户取消、服务停机中断还是上游报错，流最终均以 `done` 事件收尾，携带持久化后的完整 `Message` 对象，随后服务端主动关闭连接：

```text
event: done
data: {"message":{"id":"01933c90-1210-7000-8000-000000000012","conversation_id":"...","role":"assistant","content":"...","reasoning":"...","attachments":[],"status":"done","error":"","model_id":"m-gpt-6.1-terra","prompt_tokens":1420,"completion_tokens":386,"first_token_ms":1150,"duration_ms":6420,"created_at":1763280100050,"updated_at":1763280106470}}
```

> **错误处理注意**：流式过程中若上游大模型报错，服务端**不会**单独发送 `event: error` 事件，而是发送 `event: done`，其中 `data.message.status == "error"` 且 `data.message.error` 为友好错误文案。

#### (5) `: ping` —— 注释心跳

当上游模型静默思考、暂无新文本产出时，服务端每 **15 秒**输出一行标准 SSE 注释：

```text
: ping

```

标准 SSE 解析器会自动忽略 `:` 开头的注释行，但底层 Socket 会收到字节流，可用于客户端死连接检测与中间网关保活。

### 3.2 推荐的客户端重连与恢复策略

1. **以 `snapshot` 覆盖、以 `delta` 追加（天然幂等重连）**  
   重连时**无需**记录或传递任何序号/`Last-Event-ID`。每次（重）连接 `GET /api/messages/{id}/stream`，收到首帧 `snapshot` 时直接用其 `content` 和 `reasoning` **覆盖替换**界面当前流式缓冲，之后的 `delta` 继续追加即可，绝不会出现丢字或重复拼接。
2. **设置客户端读空闲超时（建议 35–45 秒）**  
   既然服务端每 15 秒必发 `: ping` 心跳，若客户端底层 HTTP 流连续 **35–45 秒**未读到任何字节（包括注释行），即可判定 Wi-Fi/蜂窝切换导致半开死连接，主动断开并触发重连。
3. **未收到 `done` 前断线采用指数退避重连**  
   若连接在收到 `event: done` 之前异常断开（且非用户主动离开页面），按 `300ms -> 1s -> 2s -> 5s`（上限 5s）退避重连同一路径；一旦成功收到 `snapshot` 即重置退避计数。遇到以下情况立即停止重连：
   - 已收到 `event: done`（用 `data.message` 替换本地消息状态即可）；
   - HTTP 状态码返回 `401`（跳转重新登录）或 `404`（消息或会话已被删除/截断）。
4. **切回前台与重新进入会话时的恢复**  
   - 若正在观看某条流式回答时 App 被系统挂起，回到前台后直接重连 `GET /api/messages/{id}/stream`：若后台已生成完毕，服务端会立刻回放 `snapshot` + `done` 收口；若仍在生成，则无缝接着播。
   - 打开任意历史会话调用 `GET /api/conversations/{id}` 时，检查响应中的 `active_message_id`：若非空字符串，说明该会话仍有生成任务在跑，立即订阅 `GET /api/messages/{active_message_id}/stream`。
   - 首轮对话若回答极快结束，`done` 可能早于异步标题生成完成；客户端可在 `done` 后若发现会话标题仍为空，稍后刷新一次会话详情或列表以同步自动标题。

---

## 4. 公开与系统接口（无需鉴权）

### 4.1 健康检查

- **方法与路径**：`GET /healthz`
- **鉴权要求**：无需鉴权（该接口不会输出访问日志，适合高频探针）
- **成功响应** (`200 OK`)：
  ```json
  {
    "ok": true,
    "version": "1.0.0"
  }
  ```

### 4.2 检查 Android 客户端更新

- **方法与路径**：`GET /api/app/version`
- **鉴权要求**：无需鉴权
- **成功响应** (`200 OK`)：
  - 当 `<NUAN_DATA_DIR>/app/latest.json` 不存在或 JSON 非法时：
    ```json
    {
      "available": false
    }
    ```
  - 当存在有效 `latest.json` 时（原样透传其中的键值，自动注入 `"available": true`，且当 `"apk"` 匹配 `^[A-Za-z0-9._-]+\.apk$` 时自动注入 `"url"`）：
    ```json
    {
      "available": true,
      "version": "1.0.1",
      "build": 2,
      "notes": "修复若干问题",
      "apk": "nuan-1.0.1.apk",
      "force": false,
      "url": "/app/download/nuan-1.0.1.apk"
    }
    ```

### 4.3 下载 Android APK 安装包

- **方法与路径**：`GET /app/download/{name}`
- **鉴权要求**：无需鉴权
- **路径参数**：`name` 必须匹配 `^[A-Za-z0-9._-]+\.apk$`
- **成功响应** (`200 OK` / `206 Partial Content`)：
  - 响应头：`Content-Type: application/vnd.android.package-archive`、`Content-Disposition: attachment; filename="<name>"`
  - 响应体：APK 二进制文件流（支持 `Range` 断点续传）
- **错误响应**：`404 Not Found`（纯文本 `404 page not found`）

---

## 5. 认证与个人资料接口

### 5.1 登录

- **方法与路径**：`POST /api/auth/login`
- **鉴权要求**：无需鉴权
- **请求体**：
  ```json
  {
    "username": "mama",
    "password": "replace-with-password",
    "device": "Xiaomi 14 (Android 15)"
  }
  ```
  - `device`：可选，设备标识描述，超过 100 个字符会被自动截断。
- **成功响应** (`200 OK`)：
  ```json
  {
    "token": "8fK3xZ9pL2mN7qR4vW1yB6cD0eF5gH8jK2lM4nP6qR8",
    "user": {
      "id": "01933c8a-7f00-7000-8000-000000000001",
      "username": "mama",
      "display_name": "妈妈",
      "avatar": "🌸",
      "role": "member",
      "settings": {},
      "disabled": false,
      "created_at": 1763280000000,
      "updated_at": 1763280000000
    }
  }
  ```
- **错误码**：
  - `400 bad_request`：JSON 格式错误
  - `401 bad_credentials`：`用户名或密码不正确`（用户不存在、密码错误或账号已停用）
  - `429 too_many_attempts`：`尝试次数太多，请 15 分钟后再试`（同一 IP 或同一用户名 15 分钟内连续失败达 8 次）

### 5.2 退出登录

- **方法与路径**：`POST /api/auth/logout`
- **鉴权要求**：需登录
- **请求体**：无
- **成功响应** (`200 OK`)：
  ```json
  {
    "ok": true
  }
  ```

### 5.3 客户端冷启动聚合初始化 (Bootstrap)

一次往返拉取当前用户资料、可用模型列表、可见助手列表、全局默认模型 ID 及服务端特性开关。

- **方法与路径**：`GET /api/bootstrap`
- **鉴权要求**：需登录
- **成功响应** (`200 OK`)：
  ```json
  {
    "user": {
      "id": "01933c8a-7f00-7000-8000-000000000001",
      "username": "mama",
      "display_name": "妈妈",
      "avatar": "🌸",
      "role": "member",
      "settings": {},
      "disabled": false,
      "created_at": 1763280000000,
      "updated_at": 1763280000000
    },
    "models": [
      {
        "id": "m-gpt-6.1-terra",
        "display_name": "GPT-6.1 Terra",
        "description": "聪明均衡，日常首选",
        "vision": true,
        "think": true
      }
    ],
    "assistants": [
      {
        "id": "builtin-health",
        "owner_id": null,
        "name": "健康顾问",
        "avatar": "🩺",
        "description": "看懂体检报告、用药说明，日常健康问题",
        "system_prompt": "你是一位耐心细致的家庭健康顾问……",
        "greeting": "你好，我是健康顾问。体检报告、药品说明书看不懂？拍张照片发给我就行。",
        "suggestions": ["帮我看看这份体检报告"],
        "model_id": null,
        "temperature": null,
        "sort": 0,
        "created_at": 1763280000000,
        "updated_at": 1763280000000
      }
    ],
    "default_model_id": "m-gpt-6.1-terra",
    "features": {
      "voice_input": true,
      "image_generation": true,
      "images_left": -1
    },
    "server_version": "1.0.0"
  }
  ```
  - `features.image_generation`：管理员是否开通了画图（对话里说「帮我画…」即可）。
  - `features.images_left`：当前用户今天还能画几张；`-1` 表示不限（没设上限，或者是管理员）。

### 5.4 获取当前用户信息

- **方法与路径**：`GET /api/me`
- **鉴权要求**：需登录
- **成功响应** (`200 OK`)：返回 [`User`](#21-user用户) 对象。

### 5.5 修改当前用户资料与密码

- **方法与路径**：`PATCH /api/me`
- **鉴权要求**：需登录
- **请求体**（所有字段均可选，仅传需修改的字段）：
  ```json
  {
    "display_name": "老妈",
    "avatar": "🌺",
    "settings": { "font_scale": 1.25 },
    "old_password": "old-password",
    "new_password": "new-password-123"
  }
  ```
- **成功响应** (`200 OK`)：返回更新后的 [`User`](#21-user用户) 对象。
- **错误码**：
  - `400 bad_name`：`称呼需要 1–20 个字`（去除首尾空白后为空或超过 20 个字符）
  - `400 bad_settings`：`设置格式不正确`（非法 JSON 或超过 16KB）
  - `400 bad_password`：`原密码不正确`（传了 `new_password` 但 `old_password` 校验不通过）
  - `400 weak_password`：`新密码至少 6 位`

---

## 6. 模型与助手（角色卡）接口

### 6.1 获取可用模型列表

仅返回自身启用（`enabled=true`）且所属供应商也启用（`provider.enabled=true`）的模型，按 `sort, created_at` 排序。

- **方法与路径**：`GET /api/models`
- **鉴权要求**：需登录
- **成功响应** (`200 OK`)：`PublicModel[]` 数组（无可用模型时返回 `[]`）
  ```json
  [
    {
      "id": "m-gpt-6.1-terra",
      "display_name": "GPT-6.1 Terra",
      "description": "聪明均衡，日常首选",
      "vision": true,
      "think": true
    }
  ]
  ```

### 6.2 获取可见助手列表

返回所有全局共享助手（`owner_id == null`）以及当前用户自己创建的私有助手，按 `sort, created_at` 排序。

- **方法与路径**：`GET /api/assistants`
- **鉴权要求**：需登录
- **成功响应** (`200 OK`)：[`Assistant[]`](#23-assistant助手--角色卡) 数组。

### 6.3 创建助手

- **方法与路径**：`POST /api/assistants`
- **鉴权要求**：需登录
- **请求体**：
  ```json
  {
    "name": "英语陪练",
    "avatar": "🗣️",
    "description": "日常口语对话纠错",
    "system_prompt": "你是一位温和的英语口语陪练老师……",
    "greeting": "Hello! 今天想聊点什么话题？",
    "suggestions": ["用简单英语聊聊周末计划", "帮我纠正这句话的语法"],
    "model_id": "m-gpt-6.1-terra",
    "temperature": 0.7,
    "sort": 10,
    "shared": false
  }
  ```
  - `name`：**必填**（非空）。
  - `shared`：可选布尔值；仅当调用者是 `admin` 且 `"shared": true` 时会创建全局共享助手（`owner_id: null`），普通用户传 `true` 会被忽略，仍归属自己。
  - `model_id`：传 `""` 或不传表示不绑定特定模型（`null`）。
- **成功响应** (`201 Created`)：返回新建的 [`Assistant`](#23-assistant助手--角色卡) 对象。
- **错误码**：
  - `400 bad_name`：`请填写名称`

### 6.4 修改助手

- **方法与路径**：`PATCH /api/assistants/{id}`
- **鉴权要求**：需登录（私有助手仅本人可改；共享助手 `owner_id == null` 仅管理员可改）
- **请求体**：与 `POST /api/assistants` 字段相同（全部可选，注意更新时不支持通过 `shared` 变更归属关系；传 `"model_id": ""` 可清空绑定模型）。
- **成功响应** (`200 OK`)：返回更新后的 [`Assistant`](#23-assistant助手--角色卡) 对象。
- **错误码**：
  - `403 forbidden`：`只有管理员可以修改共享助手`
  - `404 not_found`：助手不存在或无权查看

### 6.5 删除助手

- **方法与路径**：`DELETE /api/assistants/{id}`
- **鉴权要求**：需登录（普通用户仅能删除自己的私有助手；管理员可删除包括共享助手在内的任意助手）
- **成功响应** (`200 OK`)：
  ```json
  {
    "ok": true
  }
  ```
- **错误码**：`404 not_found`

---

## 7. 会话与对话消息接口

### 7.1 获取会话列表（游标分页 + 全文搜索）

按 `updated_at` 倒序返回当前用户的非隐私会话（`private == false`；隐私对话绝不会出现在会话列表与全文搜索结果中。置顶分组由客户端根据 `pinned` 字段自行归类展示）。

- **方法与路径**：`GET /api/conversations`
- **鉴权要求**：需登录
- **查询参数（Query）**：
  - `before`（可选，`int64`）：上一页最后一条会话的 `updated_at` 毫秒时间戳（返回 `updated_at < before` 的记录；不传或 `0` 表示从最新开始）
  - `limit`（可选，`int`）：每页条数，范围 `1–200`，默认 `50`（超出范围回退为 `50`）
  - `q`（可选，`string`）：搜索关键词，同时模糊匹配会话 `title` 与所属消息 `content`
- **成功响应** (`200 OK`)：
  ```json
  {
    "items": [
      {
        "id": "01933c90-1200-7000-8000-000000000010",
        "assistant_id": "builtin-health",
        "model_id": "m-gpt-6.1-terra",
        "title": "体检报告指标解读",
        "pinned": true,
        "created_at": 1763280100000,
        "updated_at": 1763280150000
      }
    ]
  }
  ```

### 7.2 创建空会话（可选）

> 说明：客户端也可以不提前调此接口，直接调用 `POST /api/chat` 且不传 `conversation_id`，服务端会自动创建新会话。

- **方法与路径**：`POST /api/conversations`
- **鉴权要求**：需登录
- **请求体**（所有字段均可选）：
  ```json
  {
    "assistant_id": "builtin-health",
    "model_id": "m-gpt-6.1-terra",
    "title": "自定义标题",
    "private": false
  }
  ```
  - `title` 超过 60 个字符会自动截断。
  - `private`：可选布尔值，设为 `true` 时创建隐私对话（强制 `pinned: false`）。
- **成功响应** (`201 Created`)：返回 [`Conversation`](#24-conversation会话) 对象。
- **错误码**：`404 not_found`（指定的 `assistant_id` 不存在或不可见）

### 7.3 获取会话详情与最新一页消息

返回会话元数据、最新最多 **60 条**消息（按时间正序 `id ASC` 排列）、是否还有更早历史消息，以及当前正在运行生成的助手消息 ID。

- **方法与路径**：`GET /api/conversations/{id}`
- **鉴权要求**：需登录
- **成功响应** (`200 OK`)：
  ```json
  {
    "conversation": {
      "id": "01933c90-1200-7000-8000-000000000010",
      "assistant_id": "builtin-health",
      "model_id": "m-gpt-6.1-terra",
      "title": "体检报告指标解读",
      "pinned": false,
      "created_at": 1763280100000,
      "updated_at": 1763280150000
    },
    "messages": [
      {
        "id": "01933c90-1205-7000-8000-000000000011",
        "conversation_id": "01933c90-1200-7000-8000-000000000010",
        "role": "user",
        "content": "帮我看看这张化验单",
        "reasoning": "",
        "attachments": [
          {
            "file_id": "01933c8f-9900-7000-8000-000000000009",
            "mime": "image/jpeg",
            "width": 1280,
            "height": 960
          }
        ],
        "status": "done",
        "error": "",
        "model_id": null,
        "prompt_tokens": 0,
        "completion_tokens": 0,
        "first_token_ms": 0,
        "duration_ms": 0,
        "created_at": 1763280100010,
        "updated_at": 1763280100010
      }
    ],
    "has_more": false,
    "active_message_id": ""
  }
  ```
- **错误码**：`404 not_found`

### 7.4 修改会话（重命名 / 置顶 / 切换模型或助手）

- **方法与路径**：`PATCH /api/conversations/{id}`
- **鉴权要求**：需登录
- **请求体**（所有字段可选；`model_id` 或 `assistant_id` 传空字符串 `""` 表示清空为 `null`）：
  ```json
  {
    "title": "父母年度体检解读",
    "pinned": true,
    "model_id": "m-gpt-6.1-terra",
    "assistant_id": "builtin-health"
  }
  ```
- **成功响应** (`200 OK`)：返回更新后的 [`Conversation`](#24-conversation会话) 对象。
- **错误码**：`404 not_found`

### 7.5 删除会话

若该会话当前有正在进行的生成任务，服务端会先取消该任务并等待其收尾，再级联删除会话与旗下全部消息。若该会话为**隐私对话（`private: true`）**，还会同步删除其消息中关联的无引用 `files` 记录并立即移除磁盘上的图片文件。

- **方法与路径**：`DELETE /api/conversations/{id}`
- **鉴权要求**：需登录
- **成功响应** (`200 OK`)：
  ```json
  {
    "ok": true
  }
  ```
- **错误码**：`404 not_found`

### 7.6 向上翻页加载更早的历史消息

- **方法与路径**：`GET /api/conversations/{id}/messages`
- **鉴权要求**：需登录
- **查询参数（Query）**：
  - `before`（可选，`string`）：当前已加载最早一条消息的 `id`（UUIDv7 字符串，返回 `id < before` 的消息）
  - `limit`（可选，`int`）：条数，范围 `1–200`，默认 `60`
- **成功响应** (`200 OK`)（`items` 内按时间正序 `id ASC` 排列）：
  ```json
  {
    "items": [],
    "has_more": false
  }
  ```
- **错误码**：`404 not_found`

### 7.7 发送消息并发起生成

写入用户消息及助手占位消息（`status: "streaming"`），后台异步请求大模型，立即返回 JSON。随后客户端应连接 `GET /api/messages/{assistant_message.id}/stream` 获取流式输出。

- **方法与路径**：`POST /api/chat`
- **鉴权要求**：需登录
- **请求体**：
  ```json
  {
    "conversation_id": "",
    "assistant_id": "builtin-health",
    "model_id": "m-gpt-6.1-terra",
    "content": "帮我看看这份体检报告需要注意什么",
    "attachments": [
      { "file_id": "01933c8f-9900-7000-8000-000000000009" }
    ],
    "edit_of": "",
    "think": false,
    "private": false
  }
  ```
  - `conversation_id`：为空时自动新建会话；非空时追加到已有会话。
  - `assistant_id`：仅在 `conversation_id` 为空（新建会话）时生效。
  - `model_id`：可选；传入后会持久化更新到会话上。实际选用模型的优先级为：**会话 `model_id` → 助手 `model_id` → 全局 `default_model_id` → 第一个可用模型**。
  - `content`：消息正文（上限 `100,000` 个字符；当 `attachments` 非空时允许为空字符串）。
  - `attachments`：可选图片列表，单次最多 **9** 张；请求时只需传 `file_id`（必须先通过 `POST /api/files` 上传且属于当前用户），服务端会自动补全 `mime`、`width`、`height`。
  - `edit_of`：可选，编辑重发功能——传入本会话中某条已有 `user` 消息的 `id`，服务端会在同一事务内删除该消息及其后的所有消息，再追加本次新消息。
  - `think`：是否开启深度思考模式（仅当所选模型配置了 `think_effort` 时生效）。
  - `private`：可选，是否为「隐私对话」。新建会话时传入 `true` 则创建隐私会话（不进历史与搜索、不自动生成标题、不读取也不写入任何跨模型记忆、AI 画图自动标为隐私文件）；若向已有普通会话传入 `"private": true`，服务端返回 `400 private_mismatch`。已有隐私会话后续轮次即使省略 `private` 仍保持隐私模式。
- **成功响应** (`200 OK`)：
  ```json
  {
    "conversation": {
      "id": "01933c90-1200-7000-8000-000000000010",
      "assistant_id": "builtin-health",
      "model_id": "m-gpt-6.1-terra",
      "title": "",
      "pinned": false,
      "created_at": 1763280100000,
      "updated_at": 1763280100050
    },
    "user_message": {
      "id": "01933c90-1205-7000-8000-000000000011",
      "conversation_id": "01933c90-1200-7000-8000-000000000010",
      "role": "user",
      "content": "帮我看看这份体检报告需要注意什么",
      "reasoning": "",
      "attachments": [
        {
          "file_id": "01933c8f-9900-7000-8000-000000000009",
          "mime": "image/jpeg",
          "width": 1280,
          "height": 960
        }
      ],
      "status": "done",
      "error": "",
      "model_id": null,
      "prompt_tokens": 0,
      "completion_tokens": 0,
      "first_token_ms": 0,
      "duration_ms": 0,
      "created_at": 1763280100050,
      "updated_at": 1763280100050
    },
    "assistant_message": {
      "id": "01933c90-1210-7000-8000-000000000012",
      "conversation_id": "01933c90-1200-7000-8000-000000000010",
      "role": "assistant",
      "content": "",
      "reasoning": "",
      "attachments": [],
      "status": "streaming",
      "error": "",
      "model_id": "m-gpt-6.1-terra",
      "prompt_tokens": 0,
      "completion_tokens": 0,
      "first_token_ms": 0,
      "duration_ms": 0,
      "created_at": 1763280100050,
      "updated_at": 1763280100050
    }
  }
  ```
- **错误码**：
  - `400 empty`：`消息不能为空`（文字与图片均为空）
  - `400 too_long`：`消息太长了`（超过 100,000 字）
  - `400 too_many_files`：`一次最多发送 9 张图片`
  - `404 not_found`：会话、助手、图片附件或 `edit_of` 目标消息不存在
  - `409 conversation_busy`：`上一条回答还没结束，请稍等或先停止`
  - `429 busy`：`同时进行的对话太多了，请等当前回答完成`（单用户最多并发 3 个）
  - `503 no_model`：`还没有可用的模型，请联系管理员配置`

### 7.8 重新生成最后一条回答

若会话最后一条消息是 `assistant`，则将其删除并生成新的 `assistant` 回复；若最后一条是 `user`，则直接在其后生成 `assistant` 回复。

- **方法与路径**：`POST /api/conversations/{id}/regenerate`
- **鉴权要求**：需登录
- **请求体**（可选，允许空请求体 `Content-Length: 0`）：
  ```json
  {
    "model_id": "m-gpt-6.1-terra",
    "think": true
  }
  ```
- **成功响应** (`200 OK`)（结构同 `/api/chat`，但不含 `user_message` 字段）：
  ```json
  {
    "conversation": { "...": "..." },
    "assistant_message": { "...": "..." }
  }
  ```
- **错误码**：
  - `400 nothing`：`没有可以重新生成的回答`（会话内没有任何消息）
  - `404 not_found`、`409 conversation_busy`、`429 busy`、`503 no_model`

### 7.9 订阅助手消息 SSE 流

- **方法与路径**：`GET /api/messages/{id}/stream`
- **鉴权要求**：需登录（支持 `Authorization: Bearer <token>` 或 `?access_token=<token>`）
- **路径参数**：`id` 为助手消息 `Message.id`
- **成功响应** (`200 OK`，`Content-Type: text/event-stream; charset=utf-8`)：事件序列详见 [第 3 节 SSE 流式协议](#3-sse-流式协议与客户端重连策略)。
- **错误码**（建流前返回 JSON）：`401 unauthorized`、`404 not_found`、`500 internal`

### 7.10 停止正在生成的回答

取消指定助手消息的后台生成任务；已产出的部分内容会以 `status: "cancelled"` 保存，并向所有订阅该流的客户端推送 `done` 事件。

- **方法与路径**：`POST /api/messages/{id}/cancel`
- **鉴权要求**：需登录
- **请求体**：无
- **成功响应** (`200 OK`)：
  ```json
  {
    "ok": true
  }
  ```
  - 若该消息正在生成且归属当前用户，返回 `"ok": true`；若已结束或不存在，仍返回 HTTP `200` 且 `"ok": false`。

---

## 8. 图片文件与语音识别接口

### 8.1 上传图片

仅支持 **JPG、PNG、WebP、GIF** 图片（服务端通过文件内容魔数 `http.DetectContentType` 校验并解析宽高，不依赖扩展名），单文件上限 **20 MB**。

- **方法与路径**：`POST /api/files`（隐私对话上传可附带查询参数 `?private=1`，标记为短期隐私文件）
- **鉴权要求**：需登录
- **请求格式**：`multipart/form-data`，表单字段名为 `file`（可选字段 `private=1`）
- **成功响应** (`201 Created`)：
  ```json
  {
    "id": "01933c8f-9900-7000-8000-000000000009",
    "mime": "image/jpeg",
    "size": 284910,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "width": 1280,
    "height": 960,
    "created_at": 1763280095000
  }
  ```
- **错误码**：
  - `400 bad_file`：`没有收到文件，或文件超过 20MB`
  - `400 too_large`：`文件超过 20MB`
  - `400 unsupported`：`目前只支持 JPG、PNG、WebP、GIF 图片`
  - `400 bad_image`：`图片无法识别`

### 8.2 读取图片

仅允许上传该图片的用户本人读取；普通图片响应头带 `Cache-Control: private, max-age=31536000, immutable` 供客户端强缓存，隐私图片（`private: true`）返回 `Cache-Control: private, no-store`。

- **方法与路径**：`GET /api/files/{id}`
- **鉴权要求**：需登录（可使用 `?access_token=<token>`）
- **成功响应** (`200 OK`)：对应 `mime` 类型的图片二进制流。
- **错误码**：`401 unauthorized`、`404 not_found`

### 8.3 语音转文字（ASR 代理）

将客户端录制的短音频代理转发至服务端配置的 OpenAI 兼容 `/v1/audio/transcriptions` 接口，请求体上限 **10 MB**。

- **方法与路径**：`POST /api/audio/transcriptions`
- **鉴权要求**：需登录
- **请求格式**：`multipart/form-data`，表单字段名为 `file`（如 `voice.m4a`、`voice.wav`、`voice.webm`）
- **成功响应** (`200 OK`)：
  ```json
  {
    "text": "明天早上八点提醒我量血压"
  }
  ```
- **错误码**：
  - `404 voice_disabled`：`服务器没有开启语音识别`（未同时配置 `NUAN_ASR_URL` 与 `NUAN_ASR_KEY`）
  - `400 bad_file`：`没有收到录音` / `录音读取失败`
  - `502 asr_failed`：`语音识别服务连接失败` / `语音识别失败，请再说一次`

---

## 9. 管理员接口 (`/api/admin/*`)

以下接口均要求已登录且 `user.role == "admin"`，否则统一返回 `403 forbidden`（`需要管理员权限`）。

### 9.1 用户管理

#### (1) 获取全部用户列表

- **方法与路径**：`GET /api/admin/users`
- **成功响应** (`200 OK`)：[`User[]`](#21-user用户) 数组（按 `created_at` 升序）。

#### (2) 创建用户

- **方法与路径**：`POST /api/admin/users`
- **请求体**：
  ```json
  {
    "username": "baba",
    "password": "replace-with-password",
    "display_name": "爸爸",
    "avatar": "🍵",
    "role": "member"
  }
  ```
  - `username`：**必填**，正则 `^[A-Za-z0-9_.\-\p{Han}]{2,32}$`（2–32 位字母、数字、`_`、`.`、`-` 或汉字，唯一性不区分大小写）。
  - `password`：**必填**，至少 6 字节。
  - `display_name`：可选，为空时默认取 `username`。
  - `role`：可选，除 `"admin"` 外一律归为 `"member"`。
- **成功响应** (`201 Created`)：返回新建的 [`User`](#21-user用户) 对象。
- **错误码**：
  - `400 bad_username`：`用户名需要 2–32 位，可以用字母、数字、汉字`
  - `400 weak_password`：`密码至少 6 位`
  - `409 conflict`：`已存在同名项目`

#### (3) 修改用户（重置密码 / 启停账号 / 调整角色）

修改密码或将 `disabled` 设为 `true` 时，会自动吊销该用户的所有已登录 Session。

- **方法与路径**：`PATCH /api/admin/users/{id}`
- **请求体**（所有字段可选）：
  ```json
  {
    "display_name": "老爸",
    "avatar": "🍵",
    "role": "member",
    "password": "new-password-123",
    "disabled": false
  }
  ```
- **成功响应** (`200 OK`)：返回更新后的 [`User`](#21-user用户) 对象。
- **错误码**：
  - `400 self`：`不能取消自己的管理员权限或停用自己`
  - `400 bad_role`：`角色不正确`（非 `"admin"` / `"member"`）
  - `400 weak_password`：`密码至少 6 位`
  - `404 not_found`

#### (4) 删除用户

级联删除该用户的会话、对话消息、私有助手及文件数据库记录。

- **方法与路径**：`DELETE /api/admin/users/{id}`
- **成功响应** (`200 OK`)：`{"ok": true}`
- **错误码**：
  - `400 self`：`不能删除自己`
  - `404 not_found`

---

### 9.2 中转站（供应商）管理

供应商响应绝不返回明文 `api_key`，仅返回脱敏后的 `api_key_hint`（长度 > 8 时形如 `sk-****abcd`，否则为 `****`）。

#### (1) 获取供应商列表

- **方法与路径**：`GET /api/admin/providers`
- **成功响应** (`200 OK`)：
  ```json
  [
    {
      "id": "default",
      "name": "默认中转站",
      "base_url": "https://api.example.com/v1",
      "enabled": true,
      "created_at": 1763280000000,
      "updated_at": 1763280000000,
      "api_key_hint": "sk-****9f2a"
    }
  ]
  ```

#### (2) 新增供应商

- **方法与路径**：`POST /api/admin/providers`
- **请求体**：
  ```json
  {
    "name": "备用中转站",
    "base_url": "https://backup-proxy.example.com/v1",
    "api_key": "sk-replace-with-your-key",
    "enabled": true
  }
  ```
  - `name`、`api_key` 必填非空；`base_url` 必须以 `http` 开头；`enabled` 不传时默认为 `true`。
- **成功响应** (`200 OK`)：返回带 `api_key_hint` 的供应商对象。
- **错误码**：
  - `400 bad_provider`：`请填写名称、以 http 开头的地址和 API Key`

#### (3) 修改供应商

- **方法与路径**：`PATCH /api/admin/providers/{id}`
- **请求体**（所有字段可选；**`api_key` 不传或传空字符串 `""` 表示保留原密钥不变**）：
  ```json
  {
    "name": "主力中转站",
    "base_url": "https://api.example.com/v1",
    "api_key": "",
    "enabled": true
  }
  ```
- **成功响应** (`200 OK`)：返回更新后的供应商对象。
- **错误码**：`400 bad_provider`、`404 not_found`

#### (4) 删除供应商

> 注意：删除供应商会级联删除（`ON DELETE CASCADE`）归属该供应商的所有模型配置。

- **方法与路径**：`DELETE /api/admin/providers/{id}`
- **成功响应** (`200 OK`)：`{"ok": true}`
- **错误码**：`404 not_found`

#### (5) 探测供应商上游可用模型 ID 列表

实时请求供应商 `<base_url>/v1/models` 并返回按字典序排列的模型 ID 数组，辅助管理员挑选 `upstream_model`。

- **方法与路径**：`GET /api/admin/providers/{id}/upstream-models`
- **成功响应** (`200 OK`)：
  ```json
  [
    "claude-4-sonnet",
    "gemini-2.5-pro",
    "gpt-6.1-terra"
  ]
  ```
- **错误码**：
  - `404 not_found`：供应商不存在
  - `502 upstream`：`获取模型列表失败：<具体错误>`

---

### 9.3 模型管理

#### (1) 获取全部模型配置列表

返回所有模型（含已停用模型或归属已停用供应商的模型），按 `sort, created_at` 排序。

- **方法与路径**：`GET /api/admin/models`
- **成功响应** (`200 OK`)：
  ```json
  [
    {
      "id": "m-gpt-6.1-terra",
      "provider_id": "default",
      "kind": "chat",
      "upstream_model": "gpt-6.1-terra",
      "display_name": "GPT-6.1 Terra",
      "description": "聪明均衡，日常首选",
      "vision": true,
      "tools": true,
      "default_effort": "none",
      "think_effort": "high",
      "context_window": 200000,
      "max_output": 0,
      "enabled": true,
      "sort": 0,
      "created_at": 1763280000000,
      "updated_at": 1763280000000
    }
  ]
  ```

#### (2) 新增模型

- **方法与路径**：`POST /api/admin/models`
- **请求体**：
  ```json
  {
    "provider_id": "default",
    "upstream_model": "gemini-2.5-pro",
    "display_name": "Gemini 2.5 Pro",
    "description": "反应快，超长上下文",
    "vision": true,
    "default_effort": "",
    "think_effort": "high",
    "context_window": 500000,
    "max_output": 8192,
    "enabled": true,
    "sort": 2
  }
  ```
  - 字段默认值说明：
    - `provider_id`：**必填**，必须指向已存在的供应商 ID。
    - `upstream_model`：**必填**，请求上游 `/v1/chat/completions` 时的实际 `model` 名。
    - `display_name`：为空时默认等于 `upstream_model`。
    - `default_effort`：普通对话时发送给上游的 `reasoning_effort`（如 `"none"` / `"low"`；为空字符串 `""` 时不发送该字段）。
    - `think_effort`：用户开启「深度思考」时发送给上游的 `reasoning_effort`（如 `"high"`；为空字符串 `""` 表示该模型不支持深度思考开关，前端 `think` 为 `false`）。
    - `context_window`：默认 `128000`（用于裁剪历史消息 Token 预算）。
    - `max_output`：默认 `0`（`0` 表示请求上游时不传 `max_tokens`，上下文预算按预留 `8192` 估算）。
    - `enabled`：默认 `true`。
    - `kind`：创建和修改均有效，支持 `"embedding"`（向量检索，详见第 10 节）、`"chat"`（默认，家人聊天用）或 `"image"`（画图模型：不出现在家人的模型列表，只供聊天模型通过 `generate_image` 工具调用；价格按画图接口返回的 token 用量估算）。
    - `tools`：聊天模型是否会用工具（画图），默认 `true`。上游拒绝 `tools` 参数时服务端会自动去掉工具重试，并记住到该模型下次被修改为止。
  - 新增第一个 `kind: "image"` 模型且尚未设置 `image_model_id` 时，自动把它设为画图模型。
- **成功响应** (`200 OK`)：返回完整 `Model` 对象。
- **错误码**：
  - `400 bad_model`：`请填写上游模型名`
  - `400 bad_provider`：`供应商不存在`

#### (3) 修改模型

- **方法与路径**：`PATCH /api/admin/models/{id}`
- **请求体**：与 `POST /api/admin/models` 相同，所有字段可选。
- **成功响应** (`200 OK`)：返回更新后的完整 `Model` 对象。
- **错误码**：`404 not_found`、`400 bad_model`、`400 bad_provider`

#### (4) 删除模型

- **方法与路径**：`DELETE /api/admin/models/{id}`
- **成功响应** (`200 OK`)：`{"ok": true}`
- **错误码**：`404 not_found`

---

### 9.4 全局设置与用量统计

#### (1) 获取全局设置

- **方法与路径**：`GET /api/admin/settings`
- **成功响应** (`200 OK`)：
  ```json
  {
    "default_model_id": "m-gpt-6.1-terra",
    "fallback_model_id": "",
    "title_model_id": "",
    "image_model_id": "",
    "image_daily_limit": ""
  }
  ```
  - `default_model_id`：未显式指定模型时使用的全局默认模型 ID。
  - `fallback_model_id`：默认模型出错且还没输出任何内容时，自动改用的备用模型（留空不启用）。
  - `title_model_id`：首轮对话自动生成简短标题时使用的模型 ID（留空则复用当前对话所用的模型）。
  - `image_model_id`：画图模型（必须是 `kind: "image"`；留空表示不开通画图）。前三项必须是 `kind: "chat"` 的模型，混用返回 `400 bad_model`。
  - `image_daily_limit`：每位家人每天最多画几张（上海时间零点重置；只算画成功的，删除对话不退回；管理员不受限）。空或 `"0"` 表示不限；更新时须为 0–10000 的整数，否则返回 `400 bad_limit`。

#### (2) 更新全局设置

- **方法与路径**：`PUT /api/admin/settings`
- **请求体**（识别上面的设置键及第 10 节的记忆设置，可只传其中几个，其余键忽略）：
  ```json
  {
    "default_model_id": "m-gpt-6.1-terra",
    "title_model_id": "m-gemini-2.5-flash"
  }
  ```
- **成功响应** (`200 OK`)：返回更新后的完整全局设置对象。

#### (3) 查看用户 Token 用量统计

按用户聚合统计最近 `days` 天内助手回复条数及输入/输出 Token 总和。

- **方法与路径**：`GET /api/admin/usage`
- **查询参数（Query）**：
  - `days`（可选，`int`）：统计天数，范围 `1–366`，默认 `30`
- **成功响应** (`200 OK`)：
  ```json
  {
    "days": 30,
    "items": [
      {
        "user_id": "01933c8a-7f00-7000-8000-000000000001",
        "messages": 85,
        "prompt_tokens": 198420,
        "completion_tokens": 54310
      }
    ]
  }
  ```

### 10. 跨模型共享记忆

以下接口需要登录，只处理当前用户的记忆；管理员也没有读取其他用户记忆的接口。
记忆对象为：

```json
{"id":"…","category":"health","content":"用户有高血压。","source":"auto","source_conv_id":null,"pinned":true,"is_new":true,"created_at":1794930000000,"updated_at":1794930000000}
```

`category` 为 `health | family | profile | preference | ongoing | episode`，`source` 为 `auto | manual | import`。
`is_new` 表示自动记忆创建不足 3 天。仅返回 active 记忆，不暴露向量、关键词、内部状态。

| 方法 | 路径 | 请求体 | 成功响应 |
|---|---|---|---|
| GET | `/api/memories` | — | `200 {"memories":[Memory…],"total":N,"new":M,"enabled":true}` |
| POST | `/api/memories` | `{"category":"health","content":"…"}` | `201 Memory`，source 为 manual |
| PATCH | `/api/memories/{id}` | `{"category"?:"…","content"?:"…"}` | `200 Memory` |
| DELETE | `/api/memories/{id}` | — | `204` |
| DELETE | `/api/memories` | — | `204`，清空本人全部记忆 |
| POST | `/api/memories/import/preview` | `{"text":"…"}` | `202 ImportJob`，后台开始整理，替换本人之前的整理任务 |
| GET | `/api/memories/import/preview` | — | `200 ImportJob`；没有任务时 `404 not_found` |
| DELETE | `/api/memories/import/preview` | — | `204`，取消整理 / 丢弃预览 |
| POST | `/api/memories/import` | `{"items":[ImportItem…]}` | `200 {"memories":[Memory…],"skipped":N}`，source 为 import |

列表按 health、family、profile、preference、ongoing、episode 排序，同分类按创建时间倒序。
手工添加、修改、导入的 content 去首尾空白后须为 1–200 字；预览 text 最多 30000 字；导入最多 300 条，导入不接受 episode 分类。
无效输入返回 `400 bad_memory`；别人的或已删除的记忆返回 `404 not_found`。

导入外部记忆（ChatGPT / Gemini 等导出的文字）分两步：先整理成候选条目给用户勾选，确认后才入库。整理要调用几次模型（长文本按行切成约 4000 字一段，最多 3 段并发，多段时再做一次跨段去重 / 合并），可能超过一分钟，所以是每个用户一个的后台任务，App 每 1.5 秒轮询。任务只存在内存里，结束后保留 1 小时；服务重启后丢失，App 提示重新整理。

```json
// ImportJob
{"id":"…","status":"running","done":1,"total":3,"items":[ImportItem…],"skipped":2,"failed":0,"truncated":false,"error":""}
// ImportItem
{"category":"health","content":"用户的妻子 2025 年 2 月起改吃缬沙坦 80 毫克。","keywords":"降压药 高血压","valid_until":"2027-01-31","replaces_id":"…","replaces":"用户的妻子每天吃氨氯地平 5mg。"}
```

- `status`：`running | done | failed`；`failed` 时 `error` 是给用户看的中文提示。`done / total` 是已整理的段数，`done == total` 而仍在 running 表示在做跨段合并。
- `skipped`：文本里和已有记忆重复、因此没列出来的已有记忆条数。`failed`：没整理好的段数（其余段照常给出）。`truncated`：候选超过 300 条被截断。
- `keywords`、`valid_until`（仅 ongoing，`YYYY-MM-DD`）、`replaces_id` 都是可选的，导入时原样带回；`replaces` 只用于预览展示。
- 导入时在一个事务里：和本人已有记忆（忽略标点、空白和开头的「用户」）重复的跳过并计入 `skipped`；`replaces_id` 指向本人的 active 记忆时把它标为 superseded，指向别人的或已删除的记忆时只新增；随后按常驻字数预算降级。导入后立即补算向量。
编辑会清空旧向量，由后台重算；删除对话只连带删除该对话的 episode，已抽取的事实保留。

`PATCH /api/me` 的 `settings` 现在按顶层键合并，值为 `null` 时删除对应键。例如：

```json
{"settings":{"memory_enabled":false,"obsolete_key":null}}
```

`memory_enabled` 缺省为 true，false 时聊天不注入或检索记忆，后台只推进抽取游标。
角色对象新增 `use_memory`（bool），创建角色默认 true，翻译官内置角色为 false；创建和修改角色均可设置此字段。

`GET/PUT /api/admin/settings` 新增 `memory_model_id`（chat）和 `embedding_model_id`（embedding），空字符串表示未设置。
抽取模型按 memory → title → 默认聊天模型选择，向量未配置或失败时退回关键词检索。
`Model.kind` 支持 `chat | image | embedding`，创建和修改模型均可设置；embedding 不要求上下文窗口至少 1024。
`GET /api/models` 仍只提供可用 chat 模型；上游模型推荐新增 `kind`，向量模型为 embedding。
`POST /api/admin/models/{id}/test` 对 embedding 调用 `/embeddings`，成功沿用测试响应格式，`reply` 为 `向量维度 1024` 等。
不可用或类型不匹配的记忆设置会在设置修复时清空；切换向量模型后后台每批 32 条重算旧向量。

---

## 9. Auto 智能选模型（v5）

面向家庭成员的「服务端自动选模」：默认关闭，仅在管理员开启并配置判断模型后生效。

- **管理员配置（`GET/PUT /api/admin/settings`）**：
  - `auto_enabled`：`"1"` 启用，`""` 关闭（启用时必须指定 `auto_model_id`）。
  - `auto_model_id`：用作路由判断的大语言模型 ID（必须为 `chat` 模型）。
  - `auto_router_kind`：路由引擎类型，当前默认 `"llm"`（大语言模型判断），服务端 `ModelRouter` 接口已预留 `"jev"` 供未来接入专用轻量路由模型。
  - `auto_dynamic`：`"1"` 开启对话中动态能力升降级；`""`（默认）在首轮提问选定模型后锁定不变，以保护上下文前缀缓存命中。
  - `default_model_id`：当 `auto_enabled == "1"` 时支持设为 `"auto"`（全家新对话默认进入 Auto 模式）；关闭 Auto 时自动回退修复为具体可用聊天模型。
- **模型场景说明与服务端兜底（`/api/admin/models*`、`/api/admin/providers/{id}/upstream-models`）**：
  - `Model` 新增 `routing_note`（最长 300 字，管理员填写的适用场景说明，选模时最高优先级）。
  - 后台模型与上游推荐额外返回只读字段 `builtin_routing_note` 与 `auto_tier`（1～5 档），由服务端内置模型库生成（内置御三家 OpenAI / Anthropic / Google 与主流开源/国产模型家族、同系版本高低关系如 `Opus > Sonnet > Haiku`、同系高版本优于低版本等基础兜底认知）。
- **客户端与流式协议**：
  - `GET /api/bootstrap` 的 `features` 新增 `auto_model`（bool）与 `auto_dynamic`（bool）。
  - `Conversation` 新增 `auto_mode`（bool）；`POST /api/chat`、`POST /api/conversations/{id}/regenerate`、`POST/PATCH /api/conversations` 的 `model_id` 支持传 `"auto"`。
  - `GET /api/messages/{id}/stream`：Auto 选定或动态切换模型时，`snapshot` 包含最新 `model_id`，并在运行中推送 `event: model`（`{"conversation_id":"…","model_id":"…"}`）。
