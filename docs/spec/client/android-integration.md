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

### 设计要点

- **后台服务是端口映射的前提**：没有 BackgroundService，Android 杀进程后 SSH 端口映射断开，已映射的端口全部不可达。后台服务保持 SSH 心跳，维持隧道活跃
- **WS 优先 + Worker 回退**：常驻 WS 链路是主路径（实时通知），PendingEventsWorker 是 WS 不可达时的兜底（轮询拉取）。两条路径相互独立，BackgroundService 监控 WS 健康度触发 Worker
- **AppLog 双写 + Anti-Recursion**：`AppLog` 写入 logcat，同时 POST 到 `/api/android-log` 实现集中持久化。`AppLog.java` 自身是允许调用裸 `android.util.Log` 的唯一生产代码位置，以避免日志封装递归；通过 `OemUtils` 和 `SharedCacheUtils` 共享多进程状态
- **单二进制包含 APK**：`//go:embed all:dist` 把 APK 嵌入 Go 二进制，无需外部 APK 文件即可部署。`internal/frontend/embed.go::GetFS()` 优先读磁盘 `public/`（热替换），否则从 embed 读取
- **日志处理器统一、客户端端点兼容**：Web 的 `appLog.ts` 使用 `/api/client-log`，Android 的 `AppLog.java` 使用兼容路由 `/api/android-log`；两者都由服务端 `ServeClientLog` 处理，源字段区分 `js` 和 `android`
- **屏幕常亮双通道**：`useWakeLock` 优先申请标准 Web Wake Lock，并同时调用 Android `setKeepScreenOn`。页面隐藏时浏览器可能释放锁，重新可见且业务仍需要常亮时自动申请；显式释放会同时关闭两条通道
- **服务器列表由客户端持有**：Android Bridge 保存多个实例地址和密码，使当前服务器不可达时仍可切换到其他实例，完整流程见[多服务器管理](multi-server.md)
