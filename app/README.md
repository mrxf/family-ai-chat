# 小暖 · Flutter 客户端

一套代码支持 Android、iOS、macOS、Windows：Android 是主力平台；iOS 工程已生成但还没调试过；
macOS 能编译运行；Windows 工程已生成，需要在 Windows 机器上编译验证。Flutter 3.44 / Dart 3.12。

开发约定（跨端要求、分层、单文件 ≤ 800 行）见仓库根目录 [AGENTS.md](../AGENTS.md)。

## 开发

```bash
flutter pub get
flutter run                 # 模拟器访问本机服务端填 10.0.2.2:8080
flutter analyze && flutter test
flutter run -d macos        # 电脑端（窗口拉宽到 ≥ 840 是侧栏布局，拉窄回到手机布局）
```

电脑端打包：

```bash
flutter build macos --release   # build/macos/Build/Products/Release/XiaoNuan.app，最低 macOS 11
flutter build windows --release # 需在 Windows 上执行，产物在 build/windows/x64/runner/Release/
```

没有公证的 macOS 包拷到别的 Mac 上，第一次打开会被拦下：到「系统设置 → 隐私与安全性」点「仍要打开」（macOS 14 及更早也可以在 Finder 里右键 →「打开」）。桌面图标由 `scripts/make_desktop_icons.py` 生成。
电脑端暂不支持：拍照（直接打开文件选择）、日程本地响铃（由手机端提醒）、从其他应用分享进来、App 内自动更新。

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
3. 写 `data/app/latest.json`，`build` 要填 **APK 里的实际 versionCode**：`--split-per-abi` 会给 arm64 包加 2000（10001 → 12001），可用 `aapt2 dump badging xxx.apk | head -1` 查看：`{"version":"1.0.1","build":12001,"notes":"更新内容","apk":"nuan-1.0.1.apk","force":false}`

家人下次打开 App 会弹出更新提示。

## 代码结构

```
lib/
├── main.dart        启动
├── app/             NuanApp（主题、字号、登录态路由）、navigatorKey
├── core/            API 客户端、存储、数据模型（与平台无关）
├── state/           Riverpod：会话、当前对话（乐观更新/SSE 重连）、历史、日程…
├── platform/        平台能力表 + 提醒、分享进来、选图/存图、朗读等平台服务
└── ui/
    ├── theme/  widgets/  adaptive/     主题、通用组件、断点与自适应路由
    ├── features/<功能>/               两端共享的页面：chat、conversations、share、auth、settings…
    ├── mobile/                        窄窗口外壳（抽屉）
    └── desktop/                       宽窗口外壳（常驻侧栏 + 快捷键）
```

各层职责和依赖方向见 [AGENTS.md](../AGENTS.md)。

设计约定：正在生成的那条回答只通过 `ChatController.live`（ValueNotifier）刷新，token 不会触发整个列表重建。
