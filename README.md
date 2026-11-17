# 小暖 · 家用 AI 对话

给家里人（父母、爱人）用的 AI 对话应用，对标「开源版豆包」：打开就能聊、字大、好看、流畅。

```
ai-chat-mobile/
├── server/   Go 后端（单个二进制 + SQLite，Docker 镜像 13MB）—— 详见 server/README.md
├── app/      Flutter 客户端（Android 优先）—— 详见 app/README.md
└── docs/     api.md（接口参考）、proposal-v1.md（最初的方案草案，已被下面的决策取代的部分以本文为准）
```

## 快速开始

```bash
# 1. 启动服务端
cd server
cp .env.example .env        # 填 NUAN_ADMIN_PASSWORD、NUAN_UPSTREAM_URL、NUAN_UPSTREAM_KEY
go run ./cmd/server         # 默认 :8080；或 docker compose -f deploy/docker-compose.yml up -d

# 2. 打包客户端
cd ../app
flutter build apk --release --split-per-abi
# 把 build/app/outputs/flutter-apk/app-arm64-v8a-release.apk 发给家人安装
```

第一次用管理员账号登录后，在「设置 → 家庭管理 → 家庭成员」里给家人建账号，App 会生成一段登录信息，复制发给他们即可。

## 在线打包与发版（GitHub Actions）

| 流水线 | 什么时候跑 | 产出 |
|---|---|---|
| `Android`（android.yml） | `app/` 有改动推送到 main、手动运行 | 「Android 测试版」预发布页 [android-nightly](https://github.com/mrxf/family-ai-chat/releases/tag/android-nightly)，手机浏览器打开直接下载 |
| `Server`（server.yml） | `server/` 有改动推送到 main / PR | go vet + test + 各平台编译检查；main 上推送 `ghcr.io/mrxf/family-ai-chat-server:edge` |
| `Build & Release`（release.yml） | 推送 `v*` tag | 正式 Release：APK（arm64 / armv7）、服务端二进制（Linux / macOS / Windows）、Docker 镜像 `:x.y.z` + `:latest`、App 自更新用的 `latest.json` |

发版只需要一条命令（版本号唯一来源是 `app/pubspec.yaml`，脚本自动改版本号、提交、打 tag、推送）：

```bash
scripts/release.sh -m "修复了语音按钮偶尔没反应的问题"          # 1.0.0 → 1.0.1
scripts/release.sh minor -m "新增联网搜索"                       # 1.0.x → 1.1.0
scripts/release.sh current                                        # 不改版本号，给当前版本打 tag（第一次发布）
```

`-m` 的更新说明会写进 Release 页面和 `latest.json`，也就是家人手机上弹出的更新提示。Release 页面附带了把新 APK 推送到自家服务端的命令。

Android 正式签名放在仓库 Secrets（`ANDROID_KEYSTORE_BASE64` 等 4 项），用 `bash app/tool/setup-signing.sh` 一键配置；没配置时 CI 用临时签名，装得上但不能覆盖升级。

## v1 功能

| | |
|---|---|
| 对话 | 流式输出（逐字渐显）、Markdown / 表格 / 代码 / 公式、停止生成、重新回答、换个模型重新回答、修改问题后重发、自动生成标题 |
| 深度思考 | 开关打开后显示可折叠的「思考过程」；默认关闭以换取首字 ~2 秒的速度 |
| 拍照问 | 拍照或相册多选（最多 9 张），压缩到长边 2048 后上传；不支持看图的模型会提示换模型 |
| 助手 | 内置 6 个：健康顾问、家常菜师傅、写作帮手、翻译官、防骗卫士、学习辅导（服务端 `assistants` 表，即未来「角色卡」的雏形） |
| 语音 | 按住说话（需服务端配置 ASR）；系统 TTS 朗读回答，语速可调 |
| 适老化 | 字号 标准 / 大 / 特大（叠加系统字号）、所有按钮 ≥44dp、全中文、深色模式 |
| 稳 | 生成在服务端独立运行：切后台、断网、锁屏都不影响，回来自动续上；冷启动先显示本地缓存 |
| 家庭管理 | 管理员建账号 / 重置密码 / 设管理员 / 删除、全家默认模型、30 天用量统计 |
| 自更新 | 服务端放新 APK + latest.json，App 启动时提示更新（家人不用去应用商店） |

## 关键技术决策（v1）

决策过程中和 agy（Gemini）做过一轮架构评审，以下是最终拍板：

1. **服务端用 Go，不用 Rust / Node。** 瓶颈在上游 LLM，Go 的性能绰绰有余；语法少、读得懂，对熟悉 Node 的维护者最友好；单个静态二进制、无运行时，家里 NAS / 小云主机长期跑不操心。路由用 chi（与标准库完全兼容），SQL 手写（表少，不引入 sqlc 工具链）。
2. **SQLite（纯 Go 驱动，WAL，单写连接 + 读连接池）。** 家庭规模性能远超需要，备份就是复制一个目录。未来「记忆」的向量检索在这个规模用 SQLite 存 BLOB + 内存余弦即可，不上 Postgres。
3. **生成与 HTTP 请求解耦。** `POST /api/chat` 立即返回消息 ID；生成在服务端 goroutine 里跑；客户端 `GET /api/messages/{id}/stream`（SSE）订阅，**每次连接先收到全量 snapshot 再收增量**，首连和断线重连是同一条路径，没有序号对齐问题。服务端把 token 合并成 ~30fps 推送。
4. **线性历史，不做消息树。** 家人理解不了分支切换；「重新回答」替换最后一条，「修改问题」截断其后的消息。
5. **不透明 session token（库里只存 SHA-256），180 天滑动续期，续期写库每小时最多一次。** 可随时吊销；登录按 IP + 用户名限流。
6. **模型的推理档位按模型配置**：`default_effort`（日常，gpt-6.1-terra 用 `none`）和 `think_effort`（深度思考，`high`）。实测 gpt-6.1-terra 默认档首字 16 秒，`none` 首字约 2 秒。
7. **客户端滚动用「中心锚点」方案。** 发送后新问题滑到屏幕顶部、回答在下方向下生长，读长回答时文字不会被新内容推走；向上翻历史时加载更早消息也不会跳动。
8. **语音输入**：中转站不支持音频，所以服务端做了可插拔的 OpenAI 兼容转写代理（`NUAN_ASR_*`，例如硅基流动 SenseVoice）。配置后 App 输入框右侧出现麦克风：按住说话、松开发送、上滑取消（和微信一样）。没配置时按钮不出现，家人可以用输入法自带的语音键。链路已用本地假转写服务端到端验证，**还没有接真实 ASR 服务测过识别效果**。

## 未来路线

- 联网搜索：中转站已验证支持 tool calling，在 `chat.Service` 里加工具循环即可
- 记忆：新增 `memories` 表 + 向量列，生成前检索注入系统提示词
- 角色卡：`assistants` 表已就位，补上用户自建 / 编辑界面
- 生图：中转站有 gpt-image 系列模型
- 服务端 TTS（国产机系统中文语音质量参差）
