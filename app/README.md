# 小暖 · Flutter 客户端

Android 优先（iOS 工程已生成但未调试）。Flutter 3.44 / Dart 3.12。

## 开发

```bash
flutter pub get
flutter run                 # 模拟器访问本机服务端填 10.0.2.2:8080
flutter analyze && flutter test
```

网络慢时在命令前加代理，例如 `https_proxy=http://127.0.0.1:7892 flutter pub get`。

## 打包与签名

```bash
flutter build apk --release --split-per-abi
# 家人手机几乎都是 arm64：build/app/outputs/flutter-apk/app-arm64-v8a-release.apk
```

release 包用 `android/nuan-release.jks` 签名，密码在 `android/key.properties`（两者都在 .gitignore 里）。
**这两个文件一定要备份**：丢了以后，家人手机上的 App 就不能覆盖升级，只能卸载重装。
没有 key.properties 时会退回 debug 签名。换电脑或给 GitHub Actions 配置签名：`bash tool/setup-signing.sh`。

## 发布更新（App 内自更新）

推荐用流水线：在仓库根目录执行 `scripts/release.sh -m "更新说明"`，GitHub Actions 会打好 APK 和 `latest.json` 放进 Release，
把它们下载到服务端 `data/app/` 即可（Release 页面有现成命令）。版本号和 versionCode 由脚本自动维护（1.2.3 → 10203）。

手动发布：

1. 改 `pubspec.yaml` 的 `version: 1.0.1+10001`（`+` 后面的构建号必须递增）
2. 打包，把 APK 复制到服务端 `data/app/nuan-1.0.1.apk`
3. 写 `data/app/latest.json`：`{"version":"1.0.1","build":10001,"notes":"更新内容","apk":"nuan-1.0.1.apk","force":false}`

家人下次打开 App 会弹出更新提示。

## 代码结构

```
lib/
├── main.dart               入口：主题、字号缩放、登录态路由
├── core/
│   ├── api.dart            REST + SSE 客户端（dio）
│   ├── models.dart         数据类
│   ├── storage.dart        token（安全存储）、偏好、JSON 文件缓存
│   └── tts.dart            朗读（去掉 Markdown 符号）
├── state/                  Riverpod
│   ├── session.dart        登录态、bootstrap、App 设置
│   ├── conversations.dart  历史列表
│   └── chat.dart           当前对话：乐观更新、上传、SSE 订阅与重连、停止/重答/编辑
└── ui/
    ├── home_page.dart      主界面（顶栏选模型、欢迎页/消息列表、输入框）
    ├── chat_view.dart      消息列表（中心锚点滚动）
    ├── messages.dart       用户气泡、助手回答、思考中动画、深度思考面板、操作栏
    ├── markdown.dart       gpt_markdown 封装（流式渐显）
    ├── composer.dart       输入框（图片、深度思考、发送/停止）
    ├── welcome.dart        空对话欢迎页（建议问题、助手）
    ├── drawer.dart         历史抽屉（分组、搜索、置顶/重命名/删除）
    ├── settings_page.dart  设置
    ├── admin_page.dart     家庭成员管理、用量统计
    ├── sheets.dart         底部弹窗、模型选择、编辑框
    ├── update.dart         检查更新
    ├── widgets.dart        吉祥物、按压缩放、头像等通用组件
    └── theme.dart          颜色与主题
```

设计约定：正在生成的那条回答只通过 `ChatController.live`（ValueNotifier）刷新，token 不会触发整个列表重建。
