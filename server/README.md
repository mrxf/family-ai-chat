# 小暖 (Nuan) 服务端

「小暖」是一款面向家庭成员（父母、伴侣、孩子）使用的私有化 AI 对话应用后端（Go Module: `nuan`）。服务端设计目标是**极简部署、稳定省心、弱网友好**：编译后仅为单个无外部依赖的静态二进制文件，所有数据集中在一个目录下。

## 核心特性

- **单静态二进制，零外部依赖**：基于纯 Go 实现的 SQLite（`modernc.org/sqlite`，无 CGO），支持 `CGO_ENABLED=0` 一键交叉编译至 `linux/amd64`、`linux/arm64`。
- **生成任务与连接解耦**：`POST /api/chat` 落库后在后台独立 Goroutine 请求上游大模型，立即返回消息 ID；客户端通过 `GET /api/messages/{id}/stream` 订阅 SSE 流。手机切后台、Wi-Fi/蜂窝切换断线重连后，首帧自动下发 `snapshot` 全量快照并无缝续接后续增量。
- **优雅停机保护**：收到 `SIGINT`/`SIGTERM` 时先通知所有进行中的生成任务停止，在 15 秒宽限期内将已生成的部分回答持久化到数据库并向客户端推送 `done` 事件；进程异常退出残留的 `streaming` 消息会在下次启动时自动标记为 `interrupted`。
- **多模态与语音输入**：支持 JPG/PNG/WebP/GIF 图片上传问答（自动拼接最近多轮图片 Base64）、OpenAI 兼容语音转写代理（如硅基流动 SenseVoice）。
- **家庭场景开箱即用**：首次启动自动注入 6 位实用内置助手（健康顾问、家常菜师傅、写作帮手、翻译官、防骗卫士、学习辅导），支持 Android 客户端 APK 自托管检查更新与下载。

---

## 目录结构

### 源码目录 (`server/`)

```text
server/
├── cmd/server/
│   ├── main.go              # 程序入口：HTTP 服务、信号处理、优雅停机、过期会话清理
│   └── seed.go              # 首次启动初始化：管理员、默认中转站、模型智能推断、6 个内置助手
├── internal/
│   ├── api/
│   │   ├── server.go        # chi 路由表、Bearer/Query 鉴权、统一错误响应、日志与 panic 恢复中间件
│   │   ├── chat.go          # 登录登出、Bootstrap、个人资料、助手、会话、对话发送与 SSE 流
│   │   ├── files.go         # 图片上传读取、语音转写代理、Android APK 自更新接口
│   │   └── admin.go         # 管理员接口：用户管理、中转站管理、模型管理、全局设置与用量统计
│   ├── auth/
│   │   └── auth.go          # Argon2id 密码哈希、不透明 Session Token（SHA-256 落库）、登录防爆破限流
│   ├── chat/
│   │   ├── hub.go           # 内存运行中心：管理生成中的任务、多订阅者唤醒与快照读取
│   │   └── service.go       # 对话编排：模型选择、上下文 Token 预算裁剪、上游重试、自动标题生成
│   ├── config/
│   │   └── config.go        # 环境变量与可选 .env 文件加载
│   ├── llm/
│   │   └── client.go        # OpenAI 兼容 Chat Completions 客户端（流式解析、思考内容提取、空闲看门狗）
│   └── store/
│       ├── store.go         # SQLite 读写分离连接池（WAL 模式：1 写 + 多读）与内嵌 SQL 自动迁移
│       ├── migrations/      # 001_init.sql 数据库表结构定义
│       ├── users.go         # 用户与会话持久化
│       ├── catalog.go       # 中转站、模型、全局设置、助手持久化
│       └── conversations.go # 会话、线性消息历史、文件记录、Token 用量聚合
├── deploy/                  # 部署配置示例（docker-compose.yml、systemd service、nginx.conf）
├── Dockerfile               # 多阶段构建（golang:1.26-alpine -> distroless/static-debian12:nonroot）
├── .dockerignore            # Docker 构建上下文排除规则
└── .env.example             # 环境变量模板
```

### 运行时数据目录 (`NUAN_DATA_DIR`，默认 `./data`)

```text
data/
├── nuan.db                  # SQLite 主数据库文件
├── nuan.db-wal              # SQLite WAL 预写日志（运行中产生，备份时须关注）
├── nuan.db-shm              # SQLite WAL 共享内存索引文件
├── files/                   # 用户上传的聊天图片，按年月分目录存放（如 2026-11/<uuid>.jpg）
└── app/                     # Android 客户端自更新目录（可选）
    ├── latest.json          # 最新版本描述文件
    └── nuan-1.0.1.apk       # 供下载的 APK 安装包
```

---

## 本地开发

环境要求：**Go 1.26+**（无需安装 GCC / SQLite C 库）。

1. 进入 `server/` 目录并准备环境变量文件：

```bash
cd server
cp .env.example .env
```

2. 编辑 `.env`，填入初始管理员密码及可选的上游中转站配置（程序启动时会自动读取当前工作目录下的 `.env`，且不会覆盖已存在的系统环境变量）：

```dotenv
NUAN_ADDR=:8080
NUAN_DATA_DIR=./data
NUAN_LOG_FORMAT=text

NUAN_ADMIN_USERNAME=admin
NUAN_ADMIN_PASSWORD=replace-with-dev-password

NUAN_UPSTREAM_URL=https://your-openai-proxy.example.com/v1
NUAN_UPSTREAM_KEY=sk-replace-with-your-key
NUAN_MODELS=gpt-6.1-terra,gemini-2.5-pro,claude-4-sonnet
```

3. 启动服务：

```bash
go run ./cmd/server
```

4. 验证健康检查：

```bash
curl http://127.0.0.1:8080/healthz
# {"ok":true,"version":"dev"}
```

---

## 构建

通过 `-ldflags` 去除符号表与调试信息（`-s -w`），并注入版本号（`-X main.version=...`）：

### 本机编译

```bash
cd server
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=1.0.0" -o bin/nuan-server ./cmd/server

# 查看注入的版本号
./bin/nuan-server -version
```

### 交叉编译（部署到 Linux 服务器）

因为完全不依赖 CGO，可在 macOS / Windows / Linux 开发机上直接交叉编译出静态 Linux 二进制：

```bash
# 编译到 Linux x86_64 (amd64) —— 常见云服务器 / NAS / 软路由
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=1.0.0" -o bin/nuan-server-linux-amd64 ./cmd/server

# 编译到 Linux ARM64 —— 树莓派 / ARM 云主机 / Mac 虚拟机
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=1.0.0" -o bin/nuan-server-linux-arm64 ./cmd/server
```

---

## 部署指南

完整 API 契约请参见 [../docs/api.md](../docs/api.md)。服务端内置的 HTTP Server 为支持长耗时 SSE 流**未设置 `WriteTimeout`**，生产环境建议置于 Nginx 或 Caddy 反向代理之后并提供 HTTPS。

### 方式一：直接运行二进制 + systemd（推荐轻量云主机）

参考配置文件：[deploy/nuan.service](deploy/nuan.service) 与 [.env.example](.env.example)。

```bash
# 1. 创建专用系统用户（无登录 shell）
sudo useradd --system --home-dir /var/lib/nuan --shell /usr/sbin/nologin nuan

# 2. 上传编译好的二进制并赋予执行权限
sudo install -m 0755 nuan-server-linux-amd64 /usr/local/bin/nuan-server

# 3. 准备环境变量配置（注意权限 0600/0640，保护密钥）
sudo mkdir -p /etc/nuan
sudo cp .env.example /etc/nuan/nuan.env
sudo chmod 0640 /etc/nuan/nuan.env
sudo chown root:nuan /etc/nuan/nuan.env
sudo nano /etc/nuan/nuan.env   # 填入 NUAN_ADMIN_PASSWORD 等配置

# 4. 安装并启动 systemd 服务（StateDirectory=nuan 会自动创建 /var/lib/nuan 并赋权）
sudo cp deploy/nuan.service /etc/systemd/system/nuan.service
sudo systemctl daemon-reload
sudo systemctl enable --now nuan

# 5. 查看运行状态与日志
sudo systemctl status nuan
sudo journalctl -u nuan -f
```

### 方式二：Docker 单容器部署

参考文件：[Dockerfile](Dockerfile)。运行镜像基于 `gcr.io/distroless/static-debian12:nonroot`（内置 `tzdata` 与 `ca-certificates`，默认以 `65532:65532` 非 root 用户运行）。

```bash
cd server

# 1. 构建镜像（可通过 --build-arg 覆盖 GOPROXY 和 VERSION）
docker build \
  --build-arg VERSION=1.0.0 \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  -t nuan-server:1.0.0 .

# 2a. 使用 Docker 命名卷运行（推荐，自动继承 /data 的 65532:65532 权限）
docker run -d \
  --name nuan \
  --restart unless-stopped \
  --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  --env-file .env \
  -e NUAN_DATA_DIR=/data \
  -e NUAN_LOG_FORMAT=json \
  -v nuan-data:/data \
  nuan-server:1.0.0

# 2b. 若需挂载宿主机目录（如 /opt/nuan/data），须先将宿主机目录属主设为 65532:65532：
sudo mkdir -p /opt/nuan/data
sudo chown -R 65532:65532 /opt/nuan/data
docker run -d \
  --name nuan \
  --restart unless-stopped \
  --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  --env-file .env \
  -e NUAN_DATA_DIR=/data \
  -e NUAN_LOG_FORMAT=json \
  -v /opt/nuan/data:/data \
  nuan-server:1.0.0
```

### 方式三：Docker Compose 部署

参考文件：[deploy/docker-compose.yml](deploy/docker-compose.yml)。

```bash
cd server
cp .env.example .env
# 编辑 .env，至少填写 NUAN_ADMIN_PASSWORD

# 启动（自动构建镜像并创建命名卷）
docker compose -f deploy/docker-compose.yml up -d --build

# 查看日志
docker compose -f deploy/docker-compose.yml logs -f

# 停止服务
docker compose -f deploy/docker-compose.yml down
```

---

## 反向代理与 HTTPS 配置（SSE 关键注意事项）

「小暖」的流式回答依赖标准 SSE（`GET /api/messages/{id}/stream`）。虽然服务端响应已主动附带 `X-Accel-Buffering: no` 与 `Cache-Control: no-cache, no-transform`，且每 **15 秒**发送一次 `: ping\n\n` 心跳注释，但配置反向代理时仍需确认以下要点：

1. **关闭代理响应缓冲（`proxy_buffering off`）与压缩（`gzip off`）**：否则代理会攒够缓冲区才下发，前端表现为“卡住很久后一次性蹦出整段文字”。
2. **使用 HTTP/1.1 回源并清空 `Connection` 头**：避免 HTTP/1.0 默认短连接行为干扰长连接及上游连接池复用。
3. **调大读超时（`proxy_read_timeout`）**：服务端本身不设 `WriteTimeout`，且每 15 秒发送 `: ping` 保活；建议将 SSE 路由的读超时设为 `3600s`（普通路由设为 `120s` 以兼顾最长 60s 的语音转写请求）。
4. **放宽请求体大小限制**：图片上传单张上限为 `20MB`、录音上传上限为 `10MB`，代理层建议设为 `25MB`。
5. **正确传递真实客户端 IP**：登录接口基于 `X-Forwarded-For` 首个 IP（回退 `X-Real-IP`）做防爆破限流，面向公网的最外层代理应使用客户端真实连接 IP 覆盖该头，避免伪造绕过或全部落到 `127.0.0.1` 导致全家误锁。

### Nginx 示例

完整配置见 [deploy/nginx.conf](deploy/nginx.conf)，核心片段如下：

```nginx
location ~ ^/api/messages/[^/]+/stream$ {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;

    proxy_buffering off;
    proxy_cache off;
    gzip off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

### Caddy 示例 (`Caddyfile`)

Caddy 默认自动申请并续期 Let's Encrypt HTTPS 证书，且对 `text/event-stream` 自动禁用缓冲（`flush_interval` 自适应实时刷新），配置非常简洁：

```caddyfile
chat.example.com {
    request_body {
        max_size 25MB
    }

    reverse_proxy 127.0.0.1:8080 {
        # 确保覆盖客户端伪造的 X-Forwarded-For，使服务端限流拿到真实 IP
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}

        # 显式开启立即刷新（Caddy 对 text/event-stream 默认已实时刷新，写明更稳妥）
        flush_interval -1
    }
}
```

---

## 环境变量说明

服务端启动时会先尝试读取工作目录下的 `.env` 文件（若存在，不会覆盖系统已有的同名环境变量），随后读取环境变量：

| 变量名 | 默认值 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| `NUAN_ADDR` | `:8080` | 否 | HTTP 监听地址，如 `:8080` 或 `127.0.0.1:8080` |
| `NUAN_DATA_DIR` | `./data` | 否 | 数据持久化目录（存放 `nuan.db`、`files/`、`app/`） |
| `NUAN_LOG_FORMAT` | `text` | 否 | 标准输出日志格式：`text`（适合本地阅读）或 `json`（适合容器/采集） |
| `NUAN_ADMIN_USERNAME` | `admin` | 否 | **仅首次启动**（`users` 表为空时）创建的管理员登录用户名 |
| `NUAN_ADMIN_PASSWORD` | 无（空） | 否 | **仅首次启动**（`users` 表为空时）创建的管理员密码；为空则进入首次设置模式：日志打印一次性设置码 `setup_code`，在 App 里初始化管理员 |
| `NUAN_UPSTREAM_URL` | 无（空） | 否 | **只导入一次**（`providers` 表为空且同时配置了 Key 时）的默认供应商地址；只填域名自动加 `/v1`，带路径的原样使用。管理员删掉它后重启不会再导入 |
| `NUAN_UPSTREAM_KEY` | 无（空） | 否 | **仅首次启动**默认中转站的 API Key；留空可启动后用管理员接口配置 |
| `NUAN_MODELS` | `gpt-6.1-terra` | 否 | **仅首次启动**创建默认中转站时同步创建的模型列表（英文逗号分隔上游模型 ID，第 1 个自动设为全局 `default_model_id`） |
| `NUAN_ASR_URL` | 无（空） | 否 | 语音输入转写服务地址（OpenAI 兼容，实际请求 `<URL>/v1/audio/transcriptions`）；须与 `NUAN_ASR_KEY` 同时非空才开启 |
| `NUAN_ASR_KEY` | 无（空） | 否 | 语音输入转写服务的 API Key |
| `NUAN_ASR_MODEL` | `FunAudioLLM/SenseVoiceSmall` | 否 | 调用语音转写接口时携带的 `model` 字段名 |

---

## 首次启动初始化逻辑

首次启动（详见 `cmd/server/seed.go`）按表独立检查，**绝不会修改或覆盖已有数据**：

1. **管理员账号**（当 `users` 表行数为 `0` 时）：
   - 配置了 `NUAN_ADMIN_PASSWORD` 时，以 Argon2id 哈希创建角色为 `admin`、昵称为「管理员」的账号（用户名取 `NUAN_ADMIN_USERNAME`）。
   - 没配置时进入**首次设置模式**：日志打印 8 位一次性设置码（`setup_code`，每次启动重新生成）。在 App 登录页填入服务器地址，会出现「这台服务器还没有管理员」，按提示输入设置码创建管理员（`GET/POST /api/setup`）。
   - 账号建好后，后续重启即使删掉环境变量中的 `NUAN_ADMIN_PASSWORD` 也不受影响（安全起见建议首次启动成功后从 `.env` 移除明文密码）。
2. **默认中转站与模型**（只导入一次：`settings.providers_seeded` 未标记、`providers` 表为空，且 `NUAN_UPSTREAM_URL` 与 `NUAN_UPSTREAM_KEY` 均非空时）：
   - 创建 ID 为 `default`、名称为「默认中转站」的供应商记录。
   - 按逗号拆分 `NUAN_MODELS`，生成 ID 形如 `m-<模型名>`（斜杠替换为 `-`）的模型记录，并用 `internal/llm/catalog.go` 的规则推断友好显示名、简介、视觉能力（`vision`）与深度思考档位（`default_effort` / `think_effort`），上下文默认 128K。之后的供应商和模型都在 App「设置 → 模型与供应商」里管理。
   - 将列表中的第一个模型写入 `settings` 表的 `default_model_id`。
3. **内置家庭助手**（当 `assistants` 表行数为 `0` 时）：
   - 自动创建 6 个全局共享助手（`owner_id = null`）：`🩺 健康顾问`、`🍳 家常菜师傅`、`✍️ 写作帮手`、`🌐 翻译官`、`🛡️ 防骗卫士`、`📚 学习辅导`。

---

## 管理员日常运维操作（curl 示例）

假设服务地址为 `https://chat.example.com`：

### 1. 管理员登录获取 Token

```bash
TOKEN=$(curl -sS -X POST https://chat.example.com/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "replace-with-your-admin-password",
    "device": "admin-cli"
  }' | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')
```

### 2. 为家人创建账号

用户名支持 2–32 位字母、数字、`_`、`.`、`-` 及**中文汉字**（不区分大小写），密码至少 6 位：

```bash
curl -sS -X POST https://chat.example.com/api/admin/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "mama",
    "password": "replace-with-family-password",
    "display_name": "妈妈",
    "avatar": "🌸",
    "role": "member"
  }'
```

> 提示：对话系统提示词中会自动注入用户的 `display_name`（如 `正在和你对话的用户称呼是「妈妈」。`），让 AI 称呼更亲切。

### 3. 添加中转站（供应商）并查看上游模型列表

```bash
# 创建中转站（记录返回的 id）
curl -sS -X POST https://chat.example.com/api/admin/providers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "主力中转站",
    "base_url": "https://your-openai-proxy.example.com/v1",
    "api_key": "sk-replace-with-your-key",
    "enabled": true
  }'

# 拉取该中转站上游暴露的所有模型 ID（替换 <PROVIDER_ID>，首次自动创建的为 default）
curl -sS https://chat.example.com/api/admin/providers/default/upstream-models \
  -H "Authorization: Bearer $TOKEN"
```

### 4. 添加模型并配置深度思考与默认模型

```bash
# 添加一个支持识图与深度思考的模型
curl -sS -X POST https://chat.example.com/api/admin/models \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "provider_id": "default",
    "upstream_model": "gpt-6.1-terra",
    "display_name": "GPT-6.1 日常版",
    "description": "聪明均衡，支持看图与深度思考",
    "vision": true,
    "default_effort": "none",
    "think_effort": "high",
    "context_window": 200000,
    "max_output": 8192,
    "enabled": true,
    "sort": 1
  }'

# 设置全局默认对话模型与自动生成标题用的轻量模型（替换对应模型 id）
curl -sS -X PUT https://chat.example.com/api/admin/settings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "default_model_id": "m-gpt-6.1-terra",
    "title_model_id": "m-gemini-2.5-flash"
  }'
```

---

## 开启语音输入（以硅基流动 SenseVoice 为例）

国产 Android 手机自带语音输入体验不一，「小暖」内置了 OpenAI `/v1/audio/transcriptions` 兼容的语音转写代理：

1. 在硅基流动（SiliconFlow）控制台创建 API Key（`FunAudioLLM/SenseVoiceSmall` 识别中文/方言速度快、免费或极低成本）。
2. 在服务端 `.env` 或系统环境中配置以下三项并重启服务：

```dotenv
NUAN_ASR_URL=https://api.siliconflow.cn/v1
NUAN_ASR_KEY=sk-replace-with-siliconflow-key
NUAN_ASR_MODEL=FunAudioLLM/SenseVoiceSmall
```

3. 重启后日志将打印 `voice=true`，且客户端调用 `GET /api/bootstrap` 时会收到 `"features": {"voice_input": true}` 并自动展示按住说话按钮；录音文件经 `POST /api/audio/transcriptions` 由服务端转发，客户端不会接触 `NUAN_ASR_KEY`。

---

## App 自更新：发布新版本 APK

服务端无需依赖第三方应用商店即可向家人手机推送更新。只需将编译好的 `.apk` 与 `latest.json` 放入 `<NUAN_DATA_DIR>/app/` 目录（实时读取，**无需重启服务**）：

1. **文件名要求**：APK 文件名必须匹配正则 `^[A-Za-z0-9._-]+\.apk$`（仅含字母、数字、`.`、`_`、`-`，且以 `.apk` 结尾）。
2. 将 APK 放入 `<NUAN_DATA_DIR>/app/nuan-1.0.1.apk`。
3. 编写 `<NUAN_DATA_DIR>/app/latest.json`：

```json
{
  "version": "1.0.1",
  "build": 2,
  "notes": "1. 支持大图双指缩放\n2. 优化弱网自动重连体验",
  "apk": "nuan-1.0.1.apk",
  "force": false
}
```

字段说明：
- `version`（字符串）：展示给用户的语义化版本号。
- `build`（整数）：客户端用于比对是否有新版本的构建号（对应 Flutter `pubspec.yaml` 中 `+` 后的数字）。
- `notes`（字符串）：更新说明文案。
- `apk`（字符串）：同目录下的 APK 文件名；服务端会在响应中自动拼接 `"url": "/app/download/nuan-1.0.1.apk"`。
- `force`（布尔值）：是否强制更新。

4. 验证更新接口（无需鉴权）：

```bash
curl -sS https://chat.example.com/api/app/version
# {"apk":"nuan-1.0.1.apk","available":true,"build":2,"force":false,"notes":"...","url":"/app/download/nuan-1.0.1.apk","version":"1.0.1"}
```

> 若使用 Docker 命名卷 `nuan-data`，可通过 `docker cp nuan-1.0.1.apk nuan:/data/app/` 与 `docker cp latest.json nuan:/data/app/` 拷贝进去（若 `/data/app` 尚未创建，可在宿主机建好 `app/` 目录一并 `docker cp app nuan:/data/`）。

---

## 备份与恢复

所有状态均位于 `NUAN_DATA_DIR`（SQLite 数据库 `nuan.db`、聊天图片 `files/`、安装包 `app/`）。

### 方案 A：停服冷备（最简单稳妥）

停止服务时 SQLite 会完成检查点关闭，直接打包整个数据目录即可：

```bash
# systemd 部署示例
sudo systemctl stop nuan
sudo tar -czf nuan-backup-$(date +%F).tar.gz -C /var/lib/nuan .
sudo systemctl start nuan
```

### 方案 B：不停服在线热备

运行期间数据库开启了 WAL 模式，**切勿在不停服时直接 `cp nuan.db`**（会导致漏掉 `-wal` 数据或拷到不一致页面）。请使用 `sqlite3` 的 `.backup` 命令在线快照数据库，再同步静态文件目录：

```bash
BACKUP_DIR="/var/backups/nuan-$(date +%F-%H%M)"
mkdir -p "$BACKUP_DIR"

# 1. 在线安全备份 SQLite 数据库
sqlite3 /var/lib/nuan/nuan.db ".backup '$BACKUP_DIR/nuan.db'"

# 2. 拷贝上传的图片与 APK 目录
cp -a /var/lib/nuan/files "$BACKUP_DIR/" 2>/dev/null || true
cp -a /var/lib/nuan/app   "$BACKUP_DIR/" 2>/dev/null || true
```

### 恢复数据

1. 停止 `nuan` 服务。
2. 清空目标 `NUAN_DATA_DIR` 下旧的 `nuan.db`、`nuan.db-wal`、`nuan.db-shm`（**注意务必删掉旧的 `-wal` 和 `-shm` 文件**，防止损坏恢复后的库）。
3. 将备份中的 `nuan.db`、`files/`、`app/` 拷回 `NUAN_DATA_DIR`，并确认目录属主正确（systemd 为 `nuan:nuan`，Docker `:nonroot` 镜像为 `65532:65532`）。
4. 启动 `nuan` 服务。

---

## 常见问题 (FAQ)

### 1. 首次启动后怎么创建管理员？
两种方式任选：在 `.env` 里配置 `NUAN_ADMIN_PASSWORD`，首次启动自动创建；或者留空，用 `docker logs nuan 2>&1 | grep setup_code` 查到设置码，在 App 登录页填入服务器地址后按提示初始化。

### 2. AI 回复不是逐字流式出现，而是等十几秒后一次性整段显示？
几乎都是反向代理缓冲引起：
- 检查 Nginx 是否对 `/api/messages/{id}/stream` 配置了 `proxy_buffering off;`、`gzip off;` 与 `proxy_http_version 1.1;`。
- 若前置了 Cloudflare 等 CDN，请确认 CDN 未缓存或缓冲 `text/event-stream` 响应。
- 若使用 `gpt-*` 推理模型且未关闭默认思考，确认模型 `default_effort` 是否设为了 `"none"`（否则上游模型会先隐藏思考较长时间才吐出首字）。

### 3. Docker 挂载宿主机目录 `./data:/data` 启动即报 `open database: ... permission denied`？
`Dockerfile` 使用了 `gcr.io/distroless/static-debian12:nonroot`，容器内进程 UID/GID 为 `65532:65532`。改用 Docker 命名卷（如 `-v nuan-data:/data`）或先在宿主机执行 `sudo chown -R 65532:65532 ./data` 即可解决。

### 4. 登录返回 `429 too_many_attempts`（尝试次数太多，请 15 分钟后再试）？
服务端内置内存级防爆破器：同一来源 IP 或同一用户名在 15 分钟内连续失败满 **8 次**会锁定 15 分钟。
- 若全家人在同一宽带下频繁触发，请检查 Nginx 是否正确传递了 `X-Forwarded-For`（否则若外网经过多层 NAT 或内网代理把所有请求源 IP 都变成了同一个内网网关 IP，错误次数会叠加）。
- 紧急解锁可直接重启一次 `nuan` 服务（限流计数器保存在内存中）。

### 5. 忘记管理员密码怎么办？
如果还有其他管理员账号，可由另一位管理员调用 `PATCH /api/admin/users/{id}` 重置密码；若只有一个管理员，停服后在 SQLite 中清空该用户或在本地用 Go 调用 `auth.HashPassword` 生成新的 `$argon2id$...` 哈希更新 `users.password_hash` 列即可。

### 6. 国内服务器如何通过 HTTP 代理访问海外上游大模型？
`internal/llm/client.go` 已配置 `http.ProxyFromEnvironment`，只需为进程设置标准环境变量 `HTTPS_PROXY=http://127.0.0.1:7890`（以及 `NO_PROXY=127.0.0.1,localhost,.siliconflow.cn` 绕过国内语音识别域名）即可生效。
