# 安卓界面移植与兼容框架评估

核查日期：2026-11-06。当前版本：`0.2.0`。上游基线：`252b5289424fcf86cf935b722e09fa48d8ecee32`。

## 结论与实施

本项目采用 ArkTS / ArkUI 原生壳 + ArkWeb + 共享 Vue 前端。安卓的工作台界面不是 Java 原生控件页面：`MainActivity.java` 使用 WebView 加载服务端，Vue 负责 Dock、聊天、文件、终端、Git、设置、任务、群聊和会议界面。因此复用同一前端是保留界面结构与交互最直接的路线，不需要先把 Vue 重写成 ArkUI、Flutter 或 React Native。

完整复刻仍包含原生能力重建，不能把“网页打开成功”作为完成标准。

| 层次 | 现有实现 | 鸿蒙处理 |
| --- | --- | --- |
| 工作台界面 | Vue 3、CSS token、主题、Lucide 图标、自适应布局 | 直接加载同一服务端前端，保留组件和样式 |
| 系统容器 | Android WebView、Activity、返回键 | ArkWeb、UIAbility、页面返回入口；已经实现首版 |
| 宿主身份 | Android / Electron，旧版把非桌面原生宿主当安卓 | 新增独立 HarmonyOS 身份，不应用安卓 CSS 补偿和 APK 更新入口 |
| 凭据与服务器 | Android SharedPreferences 和原生认证 | Preferences 保存非敏感信息，AssetStore 按地址保存密码，原生认证后配置 Web Cookie |
| 文件与媒体 | 安卓选择器、下载、分享、MediaProjection | 已实现原生保存、ShareKit 分享 / 导入、选择器、剪贴板和媒体授权；录屏授权后挂起，仍待修复与验收 |
| 后台与隧道 | Android Service / WorkManager、SSH、HTTP2 | 已实现原生通知与 SSH / HTTP2 正反向隧道；后台仅限进程获准运行，悬浮窗与 Live View 资格受限 |
| 端口浏览器 | Android 内嵌 BrowserActivity / sandbox | 当前打开系统浏览器，Cookie / 存储独立且无应用桥，不宣称会话与安卓内嵌 sandbox 等价 |

0.1.2 直接复用安卓 `assets/login.html`、`url-utils.js` 和 logo，替换先前自制的 ArkUI 连接表单。页面、主题、双语文案、服务器列表和确认弹窗保留安卓实现；存储和认证仍由鸿蒙原生层完成。登录页随 HAP 打包，服务端不可达时仍可管理服务器。来源和适配范围见 [安卓登录页资源](ANDROID_LOGIN_ASSETS.md)。

额外的原生工具栏已完全移除，无论服务器是否部署兼容补丁，都不再显示齿轮、ClawBench 标题与刷新那一行。已在 HarmonyOS 7 / API 26 手机模拟器中确认旧服务端的工作台直接位于系统状态栏下方。没有进行同版本安卓 App 与鸿蒙真机的逐像素对照。

## 框架对比

| 方案 | 适合复用什么 | 对本项目的影响 | 选择 |
| --- | --- | --- | --- |
| ArkTS / ArkUI + ArkWeb | 现有 HTML / CSS / Vue，原生层提供系统能力 | 最大限度复用安卓正在使用的业务 UI；桥和系统能力仍需移植 | 采用 |
| Flutter 的 OpenHarmony 适配版 | 已有 Dart / Flutter 页面与部分插件 | 本项目的 Vue 和 Java 壳都需要重新接入或重写，不能自动转换 Java API | 本项目不采用 |
| React Native 的 OpenHarmony 适配版 | 已有 React Native / JS 业务和适配组件 | 上游使用 Vue，不能直接复用为 RN 组件，原生依赖也要核对鸿蒙适配 | 本项目不采用 |
| ArkUI-X | 希望以 ArkUI 技术建立多平台 UI 的项目 | 属于另一种跨平台 UI 路线，不能直接把现有 Android Java 壳无损变成鸿蒙应用 | 本项目不采用 |

这些是跨平台开发和平台适配项目，不是已经替本项目验证过的 Android API 兼容层。没有选用把 APK 套壳为 HAP 的路线。

## 前端兼容补丁

补丁和完整基线记录位于 `patches/upstream/`，只发布必要差异，不提交整个原始源码目录。

- 增加 `getHostPlatform()` 和能力表；`getNative()` 显式识别 `window.ClawBenchHarmony`。
- HarmonyOS 与 Android、Electron、浏览器分别判断；触摸和宽屏轴继续独立。
- 鸿蒙不启用安卓字体 / 合成补偿，不显示 Android APK 更新；终端音量键按独立能力开关接入，不再仅按安卓身份判断。
- 文件保存、ShareKit、通知、剪贴板、选区菜单和隧道已有原生桥；悬浮状态与 Live Updates 仍报告不支持。录屏入口存在不等于捕获已通过验收。
- 通知设置分别显示系统授权和实际接收状态，不借修改服务器推送模式掩盖系统拒绝。剪贴板使用真实原生读写结果，分享导入核对确认的项目归属。
- 网页不获取已保存密码。原生认证在选定服务器完成，Web 只使用 Cookie。
- 原生桥不向所有 iframe 注入可调用代理。先向可信应用顶层文档提供外观快照，再通过面向精确 Origin 的消息端口传递白名单命令；每次连接使用随机通道标识，离页关闭端口并拒绝待处理请求。连接、文档与登录代次防止旧异步响应覆盖新页面。
- 未部署补丁的旧服务端保持网页模式，也不添加原生工具栏。工作台使用上游返回事件；事件未消费时，连续两次返回打开本地服务器列表。适配前端还可通过服务器管理桥打开该列表；进程冷启动始终可进入本地登录页。

本地登录页使用独立 ArkWeb 控制器，仅允许导航到打包的登录文档。CSP 禁止联网、frame、表单导航和外部对象；只读 `ClawBenchLoginInit.getConfig()` 提供语言、主题、版本、服务器名称与临时密码引用，动作通过随机通道的消息端口执行。`javaScriptOnDocumentStart` 不用于本地资源页，避免初始化未执行；该 API 仍用于 HTTP/HTTPS 工作台的宿主注入。远程控制器不注册登录初始化代理。

从锁定上游归档恢复参考目录后，在根目录运行：

```powershell
./scripts/apply-upstream-patch.ps1
Set-Location 'clawbench-source code'
npm ci
npx vitest run web/src/composables/__tests__/harmonyHost.test.ts web/src/composables/__tests__/useAppMode.test.ts web/src/composables/__tests__/usePlatformDetect.test.ts web/src/utils/__tests__/portForwardUtils.test.ts --maxWorkers=1
npm run typecheck
npm run build
```

Office 预览依赖的 postinstall 会准备 Vue 3 入口，正常 `npm ci` 应保留这些初始化步骤。若为了审查依赖使用 `--ignore-scripts`，之后需要明确执行 `npm rebuild @vue-office/docx @vue-office/excel @vue-office/pptx vue-demi`。本次已用该流程成功构建。

本次只通过本机代理验证适配后的前端，没有替换、重启或升级共享测试服务器。实际部署时须按上游构建流程处理 Web、Excalidraw 资源和 Go 嵌入内容，并独立安排服务器变更。

## 0.2.0 原生验证范围

双架构 arm64-v8a / x86_64 NAPI 和未签名 HAP 已构建。SSH 使用 libssh2 + mbedTLS，HTTP2 使用 nghttp2 并保留 TLS 主机名 / CA 校验；没有通过忽略证书错误获取兼容性。

HarmonyOS 7 / API 26 模拟器已验证认证文件下载、35 B 原生保存和正确的进度结束时机、35 B ShareKit 面板与取消、剪贴板授权读写、选区 Quote、后台发通知及点击定位会话、摄像头 / 麦克风音视频轨道 live。实际分享接收方、外部分享导入和完整会议仍需业务验收，不能从面板打开或轨道 live 推断通过。

SSH 与 HTTP2 的正向和反向分别完成各 6 轮模拟器数据测试，包含 2 MiB、慢读与并发，SHA-256 一致。SSH 指纹 UI 与隔离测试服务公开密钥哈希核对一致后信任，实际传输为 SSH、连接成功，已有正反向映射自动恢复通过。半关闭测试中 hdc 会提前关闭双向连接，当前环境无法据此验收半关闭，不能计作通过。

仅视频与请求音频的两种录屏场景均在授权后挂起，尚未取得可用捕获流。后台通知和隧道只在进程获准运行时工作；跨应用悬浮状态与 Live View 保持受限，不承诺常驻或厂商推送。

## 完整对齐的后续工作

| 顺序 | 工作与出口 |
| --- | --- |
| 1 | 对照同版本安卓客户端：Dock、页面、弹窗、键盘、横屏 / 平板、主题、字体和安全区域 |
| 2 | 补齐已实现文件能力的端到端验收：大文件、取消、中文名、重名、接收方实际分享、拍摄文件与外部分享导入 |
| 3 | 组合验证通知补拉、去重、已读撤销、不同事件定位与切服隔离，覆盖 Home / 锁屏 / 进程重建 |
| 4 | 完成 SSH / HTTP2 真机、错误恢复、TLS、网络切换与长连接测试；用不提前关闭双向的测试链路验收半关闭 |
| 5 | 排查录屏授权后挂起，完成摄像头 / 麦克风的实际会议流程、系统浏览器打开 / 返回验收；特殊状态展示需先取得平台资格 |
| 6 | 鸿蒙真机签名安装与完整回归，单独记录 30 / 60 分钟后台和长会话结果 |

最低系统当前配置为 HarmonyOS 6.1 / API 23，原因之一是原生 HTTP 使用 API 23 的 `maxRedirects=0` 禁止登录请求自动跳向其他服务器。编译 API 为 26；尚未在 API 23 真机上验证。自签名 HTTPS 暂未提供证书信任流程，保持系统校验，不能通过忽略证书错误冒充兼容。

## 核查来源

- 本地上游：`android/app/src/main/java/com/clawbench/app/MainActivity.java`、`web/src/utils/clawbenchNative.ts`、`web/src/composables/useAppMode.ts`、`usePlatformDetect.ts`、`docs/spec/client/design-guide.md`。
- Huawei ArkWeb 开发指南：`https://developer.huawei.com/consumer/cn/doc/harmonyos-guides-V5/arkweb-V5`。旧 `web-app-development` 入口本次访问返回 404，不能继续作为有效正文引用。
- 配套 SDK：`@ohos.web.webview.d.ts` 中的消息端口 API、`component/web.d.ts`、`@ohos.security.asset.d.ts`、`@ohos.net.http.d.ts`；本次按 SDK 实际类型编译。
- OpenHarmony JS 桥接口说明：`https://github.com/openharmony/docs/blob/master/zh-cn/application-dev/reference/apis-arkweb/arkts-apis-webview-WebviewController.md`。其代理接口明确会注入所有 frame，故本项目使用较窄的消息端口。
- Flutter 适配项目：`https://gitcode.com/CPF-Flutter/flutter_flutter`，从旧 openharmony-tpc 地址访问后显示该维护入口。
- React Native 适配项目：`https://gitcode.com/CPF-RN/ohos_react_native`，从旧 openharmony-sig 地址访问后显示该维护入口；项目主页受访问频次限制，未对全部依赖和版本兼容性背书。
- ArkUI-X 文档仓库：`https://github.com/arkui-x/docs`。
