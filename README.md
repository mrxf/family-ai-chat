# 小暖 · 家用 AI 对话

给家里人（父母、爱人）用的 AI 对话应用，对标「开源版豆包」：打开就能聊、字大、好看、流畅。

```
ai-chat-mobile/
├── server/   Go 后端（单个二进制 + SQLite 或 Postgres，Docker 镜像 13MB）—— 详见 server/README.md
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

第一次用管理员账号登录后，在「设置 → 家庭管理 → 家庭成员」里给家人建账号，App 会生成一段 `#xn...xn#` 加密免输口令，发给异地父母复制后打开小暖即可一键自动登录。

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
| 对话 | 流式输出（逐字渐显）、Markdown / 表格 / 代码 / 公式、对话分享长图保存相册、停止生成、重新回答、换个模型重新回答、修改问题后重发、自动生成标题 |
| 深度思考 | 开关打开后显示可折叠的「思考过程」；默认关闭以换取首字 ~2 秒的速度 |
| 拍照问 | 拍照或相册多选（最多 9 张），压缩到长边 2048 后上传；不支持看图的模型会提示换模型 |
| 助手 | 内置 6 个：健康顾问、家常菜师傅、写作帮手、翻译官、防骗卫士、学习辅导（服务端 `assistants` 表，即未来「角色卡」的雏形） |
| 语音 | 按住说话（需服务端配置 ASR）；系统 TTS 朗读回答，语速可调 |
| 隐私对话 | 对标 ChatGPT / Gemini 临时聊天：不进聊天记录与搜索、不读取也不触发记忆、关闭即销毁消息与图片，服务端定时兜底清理 |
| 适老化 | 字号 标准 / 大 / 特大（叠加系统字号）、所有按钮 ≥44dp、全中文、深色模式 |
| 稳 | 生成在服务端独立运行：切后台、断网、锁屏都不影响，回来自动续上；冷启动先显示本地缓存 |
| 家庭管理 | 管理员建账号 / 重置密码 / 设管理员 / 删除、全家默认模型、30 天用量统计 |
| 自更新 | 服务端放新 APK + latest.json，App 启动时提示更新（家人不用去应用商店） |

## 关键技术决策（v1）

决策过程中和 agy（Gemini）做过一轮架构评审，以下是最终拍板：

1. **服务端用 Go，不用 Rust / Node。** 瓶颈在上游 LLM，Go 的性能绰绰有余；语法少、读得懂，对熟悉 Node 的维护者最友好；单个静态二进制、无运行时，家里 NAS / 小云主机长期跑不操心。路由用 chi（与标准库完全兼容），SQL 手写（表少，不引入 sqlc 工具链）。
2. **数据库二选一：默认 SQLite（纯 Go 驱动，WAL，单写连接 + 读连接池），配置 `NUAN_DB_URL=postgres://…` 时整个 App 用 Postgres。** 小内存机器用 SQLite，备份就是复制一个目录；要扩展时换 Postgres，`nuan-server migrate-db` 在两者之间搬数据。SQL 只写一份（`?` 占位符，Postgres 下由 store 包装层改写），迁移文件两种方言各一份、版本号一一对应，同一套测试在两种数据库上都跑（`NUAN_TEST_PG_URL`）。
3. **生成与 HTTP 请求解耦。** `POST /api/chat` 立即返回消息 ID；生成在服务端 goroutine 里跑；客户端 `GET /api/messages/{id}/stream`（SSE）订阅，**每次连接先收到全量 snapshot 再收增量**，首连和断线重连是同一条路径，没有序号对齐问题。服务端把 token 合并成 ~30fps 推送。
4. **线性历史，不做消息树。** 家人理解不了分支切换；「重新回答」替换最后一条，「修改问题」截断其后的消息。
5. **不透明 session token（库里只存 SHA-256），180 天滑动续期，续期写库每小时最多一次。** 可随时吊销；登录按 IP + 用户名限流。
6. **模型的推理档位按模型配置**：`default_effort`（日常，gpt-6.1-terra 用 `none`）和 `think_effort`（深度思考，`high`）。实测 gpt-6.1-terra 默认档首字 16 秒，`none` 首字约 2 秒。
7. **客户端滚动用「中心锚点」方案。** 发送后新问题滑到屏幕顶部、回答在下方向下生长，读长回答时文字不会被新内容推走；向上翻历史时加载更早消息也不会跳动。
8. **对话里画图（工具调用）**：家人不用切换「画图模式」，说「帮我画…」时由聊天模型自己调用 `generate_image` 工具，服务端用管理员指定的画图模型出图（中转站 `gpt-image-*` 走 `/v1/images/generations`，OpenRouter 走 chat + `modalities`），转成 JPEG 存为这条回复的附件，再把结果交还聊天模型说一两句话。没有引入 Agent 框架：服务端只有一个最多两轮的小循环（第一轮可同时画多张、并行出图，第二轮不带工具只出文字），以后的联网搜索也挂在这个循环上。方案和 agy 评审过一轮，详见 `server/internal/chat/tools.go`。
9. **跨模型共享记忆（v3，方案见 `docs/proposal-v3-memory.md`）**：换哪个模型，小暖都记得你。不接第三方记忆服务；记忆是一条条短句（不是 PARA 文档），按「健康与用药 / 家人 / 关于我 / 喜好习惯 / 近期在做」常驻进系统提示词，「聊过的事」按向量（OpenRouter `baai/bge-m3`）+ 中文二字组关键词检索后放进最后一条用户消息的附注。写入不靠聊天模型调工具，而是回复完成后由后台用便宜模型抽取（新增 / 替换 / 归档，宁可漏记不要记错，代别人问的不记），已有对话按时间从早到晚回填。家人在「设置 → 小暖记住的事」里看、改、删、导入其他 AI 的记忆，管理员看不到别人的记忆。系统提示词去掉了分钟级时间，保住上游前缀缓存。方案由 agy 独立分析并对照代码评审过一轮。
10. **语音输入**：中转站不支持音频，所以服务端做了可插拔的 OpenAI 兼容转写代理（`NUAN_ASR_*`，例如硅基流动 SenseVoice）。配置后 App 输入框右侧出现麦克风：按住说话、松开发送、上滑取消（和微信一样）。没配置时按钮不出现，家人可以用输入法自带的语音键。链路已用本地假转写服务端到端验证，**还没有接真实 ASR 服务测过识别效果**。
11. **隐私对话（v4，方案见 `docs/proposal-v4-private-chat.md`）**：给爱人挑惊喜礼物、替亲友问病情或查一次性琐事时，点新建对话页底部的「开启隐私对话」虚线框卡片即可开启。存活期间复用同一套 SSE 流式、看图、画图改图链路；不进历史列表与搜索、不自动生成标题、客户端不落盘任何缓存；**记忆彻底双盲**（生成前不注入常驻记忆也不调向量检索，回复后四道防线阻断抽取与 `episode` 概括）；退出或切换对话时立即物理删除会话及关联磁盘图片，App 被直接划掉时由服务端后台每 10 分钟兜底回收超时隐私对话与孤儿隐私图片。
12. **零配置默认登录、自托管模式与 `#xn...xn#` 免输口令（v5，方案见 `docs/proposal-v5-login-and-self-host.md`）**：客户端内置默认服务器 `https://xiaonuan.miaojiai.com`，首屏彻底隐藏服务器地址框；私有部署用户可在「自托管服务器」独立界面切换并测速，启用后在登录页与聊天首页清晰展示自托管状态与能力。支持把服务器与账号经 **RFC 8439 ChaCha20 + HMAC-SHA256** 认证加密封装为 `#xn...xn#` 口令，异地父母复制微信消息后打开小暖即自动弹窗一键登录，登录成功自动清空剪贴板且无需服务端改表。
13. **长对话分块压缩、冷唤醒防爆与会话内翻旧话（v6，方案见 `docs/proposal-v6-context-compaction.md`）**：拆分「单次长文档物理上限」与「多轮活跃历史软上限（48K）」，跨过 36K 触发水位时后台异步生成会话内滚动「前情提要」并推进 `summary_cursor` 至 18K，使日常多轮对话前缀固定不变、100% 命中上游前缀缓存；停聊 ≥ 6 小时再唤醒老对话（哪怕历史攒了 900K）自动轻载只带「前情提要 + 最近约 6 轮」，不卡首字并在后台有界补齐提要；家人追问早期细节时由 `read_earlier_messages` 本地毫秒级检索本会话早期原话与长文条款（展示「已翻看前面的聊天记录」）；隐私对话仅用所选同模型自整理并在关闭时彻底物理销毁。
14. **Skills 级按需工具引擎与家庭日程提醒管理（v7，方案见 `docs/proposal-v7-skills-and-schedule.md`）**：将工具拆为常驻 `common`（画图、联网、记忆、`read_skill`）与按需挂载的 `skill` 两层；系统提示词仅保留静态 `# 技能与扩展工具目录` 并有意披露各技能可解锁的工具线索，模型按意图或工具需求调用 `read_skill` 时一次性拿到完整业务规范、14 天日历对照与近 7 天快照，并在当前轮及后续 3 轮会话租约内动态解锁对应工具，既避免低频工具污染日常上下文，又 100% 保住上游前缀缓存。首个落地的 `schedule`（日程与提醒管理）技能支持单次/全天/每天/工作日/每周/每月/每年提醒、冲突提示、日常聊天附注联动（如空腹体检提醒）、App「我的日程」页面，以及零第三方推送依赖的 Android `AlarmManager`（含开机重注册）/ iOS `UNUserNotificationCenter` 原生手机准点响铃提醒。
15. **对话内联可视化组件与原生询问用户技能（v8，方案见 `docs/proposal-v8-components-and-ask-user.md`）**：基于 Skills 引擎新增两个高频交互技能——`ui_components` 解锁五大精美对话卡片（**天气预报卡** `show_weather` 配合联网查询渲染天色渐变与多日温差条、**互动待办清单** `show_todo` 支持气泡内直接打勾落库、**数据图表卡** `show_chart` 原生绘制折线/柱状/环形占比图并支持点选查数、**互动问答测验卡** `show_quiz` 点击揭晓对错与原理解析、**核心指标看板** `show_metrics` 呈现算账/体检/热量大字格），`ask_user` 解锁原生**询问用户偏好卡片** `ask_user_question`（支持 1～4 题单选/多选、推荐标记与自由补充输入，点选确认后自动锁定摘要并发起下一轮回复）。组件通过 Unicode 偏移量 `pos` 与正文流式精确穿插，不污染纯文本复制、TTS 朗读与历史上下文，同时通过 `componentNote` 让小暖在后续轮次实时感知用户的打勾与作答进度。

## 未来路线

- 联网搜索：工具循环已就位（见上面第 8 条），新增一个 `web_search` 工具即可
- 画图：按住图片「在这张上改」的快捷入口；每人单独设置张数上限
- 记忆增强：夜间整理（合并重复、生成「人物画像」）、支持工具的模型加只读 `recall` 工具、自建角色卡的关系记忆；可选把记忆库作为 MCP 服务开放给官方 App
- 角色卡：`assistants` 表已就位，补上用户自建 / 编辑界面
- 服务端 TTS（国产机系统中文语音质量参差）
