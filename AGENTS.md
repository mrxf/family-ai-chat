# 小暖 · 协作约定（给所有 AI 编码助手和开发者）

家用 AI 对话产品：`server/` 是 Go 服务端，`app/` 是 Flutter 客户端。一套 Flutter 代码同时支持
**手机端（Android、iOS）和电脑端（macOS、Windows）**。

## 三条硬规则

1. **每个新功能都要同时考虑电脑端和移动端。** 动手前先想清楚两端分别怎么交互（触摸/长按 vs 鼠标/右键/键盘）、
   布局怎么适配（抽屉 vs 侧栏）、平台能力有没有（拍照、通知、分享进来）。
   **如果某一端做不了、要降级或体验有取舍，先告诉用户并说明方案，等确认后再写代码**，不要默默只做一端。
2. **单个源码文件不超过 800 行**（Dart、Go 都算，自动生成的文件除外）。改到一个已经超过或接近 800 行的文件时，
   顺手按职责拆开，不要继续往里加。拆分时跨文件用到的私有组件改成有意义的公开名字，不用 `part`。
3. **改完必须验证**：在 `app/` 下 `flutter analyze` 零问题、`flutter test` 全绿；动了桌面相关代码再跑
   `flutter build macos --debug`。服务端改动跑 `go test ./...`。

## 客户端分层（app/lib）

```
lib/
├── main.dart            只做启动：打开存储、预热资源、runApp
├── app/                 组装根：NuanApp（主题/字号/登录态路由）、navigatorKey
├── core/                与平台无关的纯逻辑：api.dart、storage.dart、invite_code.dart、models/（按领域拆，models.dart 为 barrel）
├── state/               Riverpod 状态与业务逻辑（chat、session、conversations、schedules…），不依赖 UI
├── platform/            平台能力层：只在这里碰插件和原生通道
│   ├── platform_capabilities.dart   各平台能力表（唯一允许按平台分支的地方）
│   ├── reminders/  share_intake/    接口 + 原生通道实现 + 不支持平台的空实现，经 Provider 注入
│   ├── media/                       选图/拍照/压缩（image_input）、保存图片（image_saver）
│   └── speech/                      朗读
└── ui/
    ├── theme/           颜色、主题、图标
    ├── widgets/         通用组件（按压、头像、弹窗、Markdown、吉祥物），不含业务
    ├── adaptive/        断点 WindowSize、ReadableWidth、appRoute
    ├── features/<功能>/ 两端共享的页面与组件：chat、conversations、share、auth、settings、memories、schedules、admin、home
    ├── mobile/          窄窗口外壳（抽屉式）
    └── desktop/         宽窗口外壳（常驻侧栏 + 快捷键）
```

依赖方向只能向下：`ui → state → core`，`ui/state → platform → core`。`core` 不 import Flutter UI 和插件；
`state` 不 import `ui`；`features` 之间尽量通过 state 通信，避免页面互相深度引用。

## 跨平台写法

- **按能力分支，不按平台分支。** 功能代码里不要写 `Platform.isMacOS` / `defaultTargetPlatform ==`。需要区分时：
  在 `PlatformCapabilities` 加一个能力字段（把各平台的值填全），功能代码读 `platformCapabilitiesProvider`。
- **用插件或原生通道的功能放进 `platform/`**：先定义接口，再写有实现的平台和“不支持”的空实现，通过 Provider
  按能力选择。不支持的平台要优雅降级（隐藏入口或给出说明），不能抛 `MissingPluginException`。
- **布局按窗口宽度，不按设备**：`context.windowSize`（≥840 为宽屏）。宽屏下正文用 `ReadableWidth` 限宽，
  不要用 `MediaQuery.sizeOf(context).width` 算组件尺寸，用父级约束（`LayoutBuilder`）。
- **整页跳转统一用 `appRoute(...)`**，不要直接 `MaterialPageRoute`，宽屏下会自动居中限宽。
- **交互两端都要有**：长按菜单在桌面靠右键（`Pressable.onLongPress` 已自动支持右键）；悬浮提示用 `Tooltip`；
  桌面常用操作给快捷键（⌘ / Ctrl）；输入框桌面上回车发送、Shift+回车换行，输入法组词时回车不发送。
- 用户可见文案别写死“手机”：用 `caps.deviceNoun`、`caps.savedImagesPlace` 之类的能力字段。
- 新增插件前确认它支持的平台（看 pub.dev 或 pubspec 里的 `platforms`）；不支持某端时按上面的方式降级，并告诉用户。
- macOS 沙盒权限在 `macos/Runner/*.entitlements`，用途说明在 `macos/Runner/Info.plist`；新能力（摄像头、定位、
  文件写入…）要同时补齐。Windows 原生源码保持纯 ASCII（构建开了 `/WX`）。

## 风格

- 保持现有代码风格（行宽约 160）。**不要对整个目录跑 `dart format`**，只格式化自己新建或整体重写的文件。
- import 分三组排序：`dart:` → `package:` → 相对路径；`lib/` 内部用相对路径。
- 注释写“为什么”，不写“做了什么”。公开类和非显而易见的函数要有 `///` 文档注释。
- 测试放 `app/test/`，用 Provider override 注入假 Api / 存储 / 平台能力；需要宽屏时设置 `tester.view.physicalSize`。

## 服务端

Go 代码在 `server/internal/` 按领域分包。800 行规则同样适用；下面这些文件已经超过上限，**下次改到时先拆**：
`chat/service.go`、`chat/tools_components.go`、`store/conversations.go`、`chat/tools_schedule.go`、
`chat/tools.go`、`llm/profiles.go`。同包内拆文件不影响调用方。
