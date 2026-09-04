# Android 集成

Android 集成让 ClawBench 在手机上像一个原生 App 一样运行——WebView 承载前端界面，原生层提供后台服务、SSH 端口映射、JS Bridge 和统一日志通道。App 进入后台时通过 BackgroundService 维持 WebSocket 连接和 SSH 隧道活跃，必要时回退到 WorkManager 拉取错过的事件。

## 流程图

### Android App 架构

```mermaid
flowchart TD
    A[MainActivity WebView] --> B[前端 Vue App]
    B <--> C[JS Bridge<br/>AndroidNative AndroidBridge]
    C <--> D[原生层]
    D --> E[BackgroundService<br/>WS + SSH 维持]
    D --> F[PendingEventsWorker<br/>WS 不可达时回退]
    D --> G[BootCompletedReceiver<br/>开机自启]
    D --> I[硬件返回键 useBackHandler]
    D --> J[AppLog 统一日志]
    E --> K[FloatingStatusController<br/>悬浮窗 + 会话面板]
    E --> L[LiveUpdateManager<br/>Android 16 实时更新]
```

### 后台策略

```mermaid
sequenceDiagram
    participant App as App 生命周期
    participant BG as BackgroundService
    participant WM as WorkManager
    participant Server
    App->>BG: onCreate 进入后台
    BG->>Server: WebSocket 维持 (ping 30s)
    Server-->>BG: WS 事件
    BG-->>App: pending notification
    Note over App: WS 断线时
    BG->>WM: 调度 PendingEventsWorker
    WM->>Server: GET /api/ai/events/pending?after=evt_xxx
    Server-->>WM: 漏发事件列表
    WM-->>App: 通知补发
```

### 悬浮窗与 Live Updates 共享 overview

```mermaid
sequenceDiagram
    participant Server
    participant BG as BackgroundService
    participant FC as FloatingStatusController
    participant LM as LiveUpdateManager
    Server-->>BG: WS 事件 / overview 快照
    BG->>FC: 喂 overview 数据
    BG->>LM: 喂同一份 overview 快照
    FC->>FC: computeStats 解析
    LM->>FC: computeStats 委托（复用解析器）
    LM->>LM: 节流合并事件突发
    LM-->>系统: notify 状态胶囊 + 展开卡片
```

悬浮窗胶囊和 Live Updates 状态栏胶囊由同一份 `/api/ai/sessions/overview` 数据驱动，统计数字永远一致；Live Updates 的刷新有合并窗口，事件突发只触发一次通知。

### APK 嵌入流程

```mermaid
flowchart LR
    A[build.sh --android] --> B[cd android && gradle assembleRelease]
    B --> C[APK 产物<br/>android/app/build/outputs/apk/release/clawbench-android.apk]
    C --> D[cp 到 internal/frontend/dist/assets/]
    D --> E[go:embed all:dist<br/>internal/frontend/embed.go]
    E --> F[单二进制部署<br/>运行时 GET /api/apk]
```

## 功能与设计要点

### 端点与日志通道（关键）

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/apk` | GET | APK 下载（从 `go:embed` 读取，路径 `assets/clawbench-android.apk`） |
| `/api/client-log` | POST | Web 端统一日志（200 条/请求上限，源为 `js`） |
| `/api/android-log` | POST | Android 端日志（legacy alias，写入 `android.log`） |
| `/api/ssh/info` | GET | SSH 隧道状态轮询（无需鉴权） |
| `/api/ai/events/pending` | GET | 离线期间漏发事件 |

> Web 前端使用 `/api/client-log`；当前 Android `AppLog` 仍 POST 到 `/api/android-log`。服务端将两条路由都交给 `ServeClientLog`，因此旧 APK 与新 Web 客户端可以同时工作。
>
> 服务端日志落盘带 50MB 轮转——每次 append 前检查文件大小，超限即轮转为 `.1` 并重开新文件，`js.log`/`android.log` 不再无限增长。

### 功能清单

- **WebView 容器**：Android WebView 承载前端 Vue App，通过 `AndroidNative` JS Bridge 暴露原生能力。Web 和原生之间通过 Bridge 双向通信
- **统一日志 AppLog**：所有 Android Java/Kotlin 代码**必须**使用 `AppLog.d/i/w/e()` 替代原始 `android.util.Log`（仅 `AppLog.java` 自身和测试代码允许裸 `android.util.Log`）。`AppLog` 同时写入 logcat 并 POST `/api/android-log`，服务端按 `android` 来源持久化日志；Web 前端则使用 `/api/client-log`
- **BackgroundService（后台服务）**：管理 SSH 端口映射和原生 WebSocket 事件通道，App 在后台时仍能接收通知
  - 关键 API：`setNativePushEnabled(boolean)`（总开关）、`getTrustAllSSLContext()`（给 PendingEventsWorker 共享 TLS）、`postEventNotificationFromWorker(ctx, eventType, data)`（跨进程触发通知）
- **PendingEventsWorker**：WS 不可达时由 WorkManager 周期调度，通过 HTTP `GET /api/ai/events/pending?after=...` 拉取漏发事件，作为离线通知回退
- **BootCompletedReceiver**：设备开机后恢复 BackgroundService + 调度 PendingEventsWorker
- **OemUtils**：厂商 ROM 适配（自启动白名单 / 后台保活 / 电池优化白名单）
- **SharedCacheUtils**：跨进程共享缓存
- **ClawBenchApp**：Application 类，初始化全局状态
- **BrowserActivity**：运行在独立进程中的浏览器 WebView，提供 URL 栏浏览能力，与承载 ClawBench 主界面的 `MainActivity` 分离
- **SSH 端口映射**：原生层建立 SSH 连接并维持端口映射，前端通过 `usePortForward` composable 控制
- **硬件返回键代理**：Android `onBackPressed` 委托给 JS 层 `clawbench-back-press` 事件，JS 注册了处理器则拦截（不注册则退出 App）。处理器按显式优先级排序（overlay 级 1000 > page 级 100）
- **自动登录**：Android 通过 `AndroidNative.getPassword()` Bridge 获取密码自动登录，配合 `setSSHPassword(savedPwd)` 设置 SSH 密码
- **桌面悬浮状态窗**：`FloatingStatusView`（原生 `FrameLayout` 胶囊 + `TYPE_APPLICATION_OVERLAY`）在 App 进入后台时于系统桌面实时展示会话状态。数据源复用 `BackgroundService` 的原生 WebSocket 通道（`session_update` / `chat_stream` 事件），无需额外连接。胶囊展示实时统计（执行中/待审批/未读计数）；**无任务且无未读时显示"空闲"状态胶囊而非隐藏**——让用户知道悬浮窗依然在守护，点空闲胶囊可打开 App。执行中状态用旋转加载指示器而非呼吸绿点。**点击胶囊展开为分组会话列表面板**（`FloatingStatusPanelView`，280dp 宽），面板标题栏复用胶囊统计内容，正文从 `GET /api/ai/sessions/overview` 拉取按项目分组的会话列表——每行显示状态点（黄色待审批 > 绿色运行中 > 蓝色未读的固定优先级）、省略标题和红色未读徽章，点击行通过深链（session id + project path）跳转到对应会话。项目分组头显示项目名+路径。面板高度跟随内容自适应并限幅屏幕。`FloatingStatusController` 负责事件→UI 状态映射、自动显隐状态机（前台隐藏、后台有任务出现、完成淡出）、展开/收起切换、拖动贴边与位置持久化。需 Manifest 声明 `SYSTEM_ALERT_WINDOW` 权限，Settings 提供开关和权限申请流程
- **Live Updates 实时状态（灵动岛）**：`LiveUpdateManager` 把会话状态作为 Android 16 的实时更新通知（Live Updates）——状态栏显示单行状态胶囊（iOS 灵动岛的 Android 对应物），锁屏和通知抽屉展示默认展开、不可折叠的卡片。状态栏胶囊只显示一组互斥摘要（待审批 > 未读完成会话 > 运行中，按紧急度取最高者，全空时移除通知保持状态栏干净）；展开卡片始终显示三组完整计数（执行中/待审批/未读），标签走 string 资源支持 i18n。数据源与悬浮窗共享同一份 `/api/ai/sessions/overview` 快照（WS 连接和事件时由 service 喂给两个消费者），`computeStats` 委托给 `FloatingStatusController` 复用三个纯 overview 解析器，保证胶囊与悬浮窗数字永远一致。事件驱动刷新有合并窗口（`THROTTLE_MS`）避免 session_update 突发触发多次 notify。Live Updates 是独立于悬浮窗的开关（Settings 提供"灵动岛/实时状态"开关，默认开），Bridge 通过 `setLiveUpdateEnabled` / `isLiveUpdateEnabled` 控制并持久化；开启时会维持原生 WS 保活。需要系统「实时更新」通知权限，Bridge 提供 `canPostPromoted` 检测和 `openLiveUpdateSettings` 跳转授权，系统不支持时回退为普通常驻通知
- **全量国际化**：Android 原生 UI 全面 i18n——`strings.xml` 英文默认 + `values-zh` 中文镜像（108 key 对齐），`MainActivity` 21 处硬编码中文（登录/连接错误、SSL 对话框、文件选择器、splash）抽到 `R.string`，`BackgroundService` 16 处通知文字同样抽离。语言跟随三层保障：App 内 `setLanguage` bridge 持久化到 prefs > cookie 读取 > 系统 locale；悬浮窗 locale 即时刷新（`onConfigurationChanged` → `controller.onLocaleChanged()`）
- **APK 单二进制部署**：`build.sh --android` → Gradle assembleRelease → APK 复制到 `internal/frontend/dist/assets/clawbench-android.apk`（`build.sh`）→ Go `//go:embed all:dist` 打包进二进制（`internal/frontend/embed.go`）→ 运行时 `GET /api/apk` 端点读取

### JS Bridge 关键方法（`AndroidNative`）

通过 `@JavascriptInterface` 暴露：

| 方法 | 用途 |
|------|------|
| `dismissSplash()` | 关闭启动 splash |
| `connectToServer(url)` | 切换连接地址 |
| `showServerDialog()` | 显示服务器选择对话框 |
| `startLogCapture()` / `stopLogCapture()` | 启停日志捕获 |
| `getAppVersion()` | 返回 App 版本号 |
| `setNativePushEnabled(boolean)` | 控制后台策略总开关 |
| `getPassword()` / `setSSHPassword(pwd)` | 自动登录 & SSH 密码 |
| `log(level, tag, msg)` | 三参数统一日志（level=d/i/w/e） |
| `isNativeApp()` | 检测是否在原生环境（与 `window.top` 双保险） |
| `getPendingNavigation()` | 获取挂起导航（启动恢复） |
| `downloadBlob(name, base64)` | 保存 Blob 到本地 |
| `getServerList()` / `saveServer()` / `removeServer()` | 持久化多服务器列表与凭据 |
| `setKeepScreenOn(boolean)` | 配合 Web Wake Lock 控制原生屏幕常亮 |
| `getTheme()` / `setTheme(themeId)` | 读取/设置完整主题 ID（如 `nord`、`github-dark`），持久化到 SharedPreferences；`applyThemeColors()` 将各主题映射到原生状态栏、导航栏和 splash 覆盖层颜色 |

### Java 端关键模块

| 类 | 角色 |
|----|------|
| `MainActivity` | WebView 容器 + `@JavascriptInterface` 全部 Bridge 方法注册 |
| `BrowserActivity` | 独立进程浏览器 WebView + URL 栏 |
| `BackgroundService` | 后台保活 + SSH 隧道 + WS 心跳 + 调度 PendingEventsWorker |
| `PendingEventsWorker` | WorkManager fallback，HTTP 拉 `/api/ai/events/pending` |
| `BootCompletedReceiver` | 开机自启恢复 |
| `AppLog` | `d/i/w/e()` 统一日志（logcat + `/api/android-log`） |
| `OemUtils` | 厂商 ROM 适配 |
| `SharedCacheUtils` | 跨进程缓存 |
| `ClawBenchApp` | Application 初始化 |
| `FloatingStatusView` | 桌面悬浮胶囊 View（状态映射 + 空闲状态 + 旋转加载指示器 + 拖动贴边 + 收缩正圆动画） |
| `FloatingStatusContentView` | 胶囊/面板标题栏共享的统计内容行（logo + 执行中/待审批/未读计数，计数为 0 时整组隐藏） |
| `FloatingStatusPanelView` | 点击胶囊展开的分组会话列表面板：解析 overview JSON、状态点优先级、未读徽章、点击行回调 |
| `FloatingStatusController` | 悬浮窗状态机：事件映射、自动显隐、展开/收起、位置持久化、overview 拉取调度、locale 变更即时刷新 |
| `LiveUpdateManager` | Android 16 实时更新通知：状态栏状态胶囊 + 默认展开卡片、三组计数、事件节流合并、无内容自动移除 |

### 设计要点

- **后台服务是端口映射的前提**：没有 BackgroundService，Android 杀进程后 SSH 端口映射断开，已映射的端口全部不可达。后台服务保持 SSH 心跳，维持隧道活跃
- **悬浮窗与 Live Updates 共享事件通道与解析器**：悬浮窗不建立新连接，直接消费 BackgroundService 原生 WS 的 `session_update` / `chat_stream` 事件；Live Update 同样复用同一份 overview 快照，并委托给同一个 `computeStats` 解析器——省电、与 App 内状态天然一致，且两处展示永不出现数字打架。胶囊本身保持轻量（只做展示 + 展开面板），交互集中在展开后的会话面板上：按项目分组浏览各会话状态、一眼看到未读、点击行直达目标会话。overview 拉取有最小间隔节流（2s），避免展开时高频刷新
- **空闲状态常驻而非隐藏**：悬浮窗无任务、无未读时显示"空闲"胶囊而不是消失——隐藏会让用户以为悬浮窗失效，常驻空闲状态明确告知"后台守护中"，点击可回 App。Live Updates 则相反，无会话时移除状态栏通知保持系统通知栏干净（状态栏不常驻，与锁屏卡片体验一致）
- **Live Updates 是独立开关但共享数据**：Live Updates 不依赖悬浮窗开关——任一消费者存活就拉取 overview，各自的开关控制各自的通知生命周期。设置里独立开关（默认开），Bridge 提供权限检测与跳转，系统不支持实时更新时自动回退为普通常驻通知
- **WS 优先 + Worker 回退**：常驻 WS 链路是主路径（实时通知），PendingEventsWorker 是 WS 不可达时的兜底（轮询拉取）。两条路径相互独立，BackgroundService 监控 WS 健康度触发 Worker
- **AppLog 双写 + Anti-Recursion**：`AppLog` 写入 logcat，同时 POST 到 `/api/android-log` 实现集中持久化。`AppLog.java` 自身是允许调用裸 `android.util.Log` 的唯一生产代码位置，以避免日志封装递归；通过 `OemUtils` 和 `SharedCacheUtils` 共享多进程状态
- **单二进制包含 APK**：`//go:embed all:dist` 把 APK 嵌入 Go 二进制，无需外部 APK 文件即可部署。`internal/frontend/embed.go::GetFS()` 优先读磁盘 `public/`（热替换），否则从 embed 读取
- **日志处理器统一、客户端端点兼容**：Web 的 `appLog.ts` 使用 `/api/client-log`，Android 的 `AppLog.java` 使用兼容路由 `/api/android-log`；两者都由服务端 `ServeClientLog` 处理，源字段区分 `js` 和 `android`
- **屏幕常亮双通道**：`useWakeLock` 优先申请标准 Web Wake Lock，并同时调用 Android `setKeepScreenOn`。页面隐藏时浏览器可能释放锁，重新可见且业务仍需要常亮时自动申请；显式释放会同时关闭两条通道
- **服务器列表由客户端持有**：Android Bridge 保存多个实例地址和密码，使当前服务器不可达时仍可切换到其他实例，完整流程见[多服务器管理](multi-server.md)
