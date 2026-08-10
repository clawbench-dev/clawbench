# ClawBench Electron 桌面客户端 — 设计文档

- 日期：2026-08-10
- 状态：已批准
- 范围：Electron 桌面客户端（远程客户端形态）+ 平台无关桥层 + npm 分发/自动更新

## 1. 背景与目标

ClawBench 目前有三端：Go 后端（本机服务）、Vue 3 移动优先 Web（PWA）、Android 原生 WebView App（远程客户端）。

目标：新增 **Electron 桌面客户端**，采用**远程客户端形态**（仿 Android），连接远程 ClawBench 服务器（含 SSH 隧道端口转发）。前端已有一条原生桥缝隙 `window.AndroidNative`（约 40 个 JS 桥方法），本次将其抽象为**平台无关桥层 `window.ClawBenchNative`**，Android 与 Electron 共用同一契约。

关键决策（已与需求方确认）：

- 部署形态：**远程客户端**（非自包含本地服务）
- 功能范围：**核心必备 🟢 + 值得做 🟡**（不含 OEM 类、桌面无意义项）
- 桥层：**平台无关 `ClawBenchNative`，不做 APK 向后兼容**（Android 同步改注入名）
- 分发/更新：**复用 npm 自升级模式**，载荷为**便携目录**（镜像 Go 替换二进制）

## 2. 功能范围

### 2.1 核心必备（🟢）

| 功能 | ClawBenchNative 方法 | 桌面实现 |
|---|---|---|
| 原生模式标记 | `isNativeApp()` | preload 注入 |
| 应用版本/语言 | `getAppVersion()` / `getLanguage()` | 读 package.json / locale |
| 服务器列表 | `getServerList()` `saveServer()` `removeServer()` | electron-store |
| 连接服务器 | `connectToServer()` `getSavedServerConfig()` `getServerUrl()` | 本地保存 + 登录页预填 |
| SSH 隧道 + 端口转发 | `addForwardedPort()` `removeForwardedPort()` `getForwardedPorts()` `testPortReachable()` `reconnectTunnel()` `reconnectTunnelAsync()` `isTunnelConnected()` `getTunnelError()` `getTunnelErrorType()` | 主进程 Node `ssh2`（替代 BackgroundService） |
| SSH/Web 密码 | `getPassword()` `setSSHPassword()` | Electron 内置 `safeStorage` 加密 |
| 文件下载 | `downloadFile()` `downloadUrl()` `downloadBlob()` | 保存对话框 / Downloads / showItemInFolder |
| 打开外部链接 | `openInBrowser()` | `shell.openExternal` |
| 日志中继 | `log()` | 复用后端 appLog 通道 |

### 2.2 值得做（🟡）

| 功能 | 方法 | 桌面实现 |
|---|---|---|
| 系统通知 + 会话深链 | `openSession()` `getPendingNavigation()` | 原生 Notification，点击聚焦会话 |
| 通知开关 | `setNativePushEnabled()` `updateLastSeenEventId()` | 复用 |
| 沙盒浏览器窗口 | `openInSandbox()` | 独立 BrowserWindow（partitioned session 隔离 cookie） |
| 防休眠 | `setKeepScreenOn()` | `powerSaveBlocker`（AI 流式/TTS 时） |
| 分享 | `shareText()` `shareFile()` `shareFiles()` | 简化：系统默认应用打开 / 复制剪贴板 |
| 日志采集 | `startLogCapture()` `stopLogCapture()` | 复用 |

### 2.3 明确不做（桌面侧不实现，但**保留在共享契约**）

> **2026-08-10 修订**：以下方法原本被列为"不做"并从共享契约移除，但审查发现会破坏 Android 既有流程（桥为 Android+Electron 共用，移除即 Android 回归）。现**保留在 `ClawBenchNative` 契约**，Electron preload 实现为 no-op，Android 侧真实行为不变。

- `dismissSplash`（桌面无 splash；**契约保留**，Electron no-op —— Android 远程页 splash 依赖此调用，不可移除）
- `stopBackgroundService`（桌面无前台服务；**契约保留**，Electron no-op —— Android 无端口时停服务防耗电，不可移除）
- `setVolumeKeyMode` / `setTerminalSessionCount`（桌面无硬件音量键/状态栏角标；**契约保留**，Electron no-op —— Android 终端音量键转发与通知角标）
- 所有 OEM 相关（`isChineseOem`/`getOemName`/`isOemAutoStartPrompted`/`setOemAutoStartPrompted`/`openOemAutoStartSettings`/`openOemBatterySettings`）（桌面无；**契约保留**，Electron no-op —— Android 国产机型引导；注：当前前端无活跃调用点，保留契约以对齐 Java）
- 桌面独有加分项（托盘/应用菜单/单实例锁/协议深链/自启动）——**列入后续**，本次不含

## 3. 仓库结构（新增顶层 `desktop/`）

```
desktop/
  package.json                # electron / electron-store / ssh2；版本号来源
  main/
    index.ts                  # 入口：app 生命周期
    bridge.ts                 # 所有 ipcMain.handle 注册（ClawBenchNative 契约）
    window.ts                 # 主窗口 + 沙盒窗口管理
    tunnel.ts                 # SSH 隧道（ssh2 端口转发，替代 BackgroundService）
    notification.ts           # 原生通知 + 会话深链
    download.ts               # 保存对话框 / Downloads / showItemInFolder
    store.ts                  # electron-store：服务器列表等
    secrets.ts                # safeStorage 加密密码
    powersave.ts              # powerSaveBlocker
    updater.ts                # npm 自升级器（镜像 internal/service/upgrade.go）
  preload/
    index.ts                  # contextBridge.exposeInMainWorld('ClawBenchNative', …)
  build/                      # electron-builder 配置（win/mac/linux）
```

## 4. 桥层契约 `window.ClawBenchNative`

`contextBridge` 暴露同名对象，Android 与 Electron 共用同一契约。**读操作全部改 async（`ipcRenderer.invoke`）**——这是与 Android 同步 `@JavascriptInterface` 的本质差异。

### 4.1 同步方法（preload 本地常量）

- `isNativeApp(): boolean` → true
- `getLanguage(): string`（locale，preload 本地读）
- `openSession(sessionId: string): void` — 派发会话深链事件
- `log(level, tag, msg): void`
- `setKeepScreenOn(on: boolean): void`
- `setNativePushEnabled(enabled: boolean): void`
- `updateLastSeenEventId(eventId: string): void`
- `dismissSplash(): void` — Android 收口；Electron no-op
- `stopBackgroundService(): void` — Android 收口；Electron no-op

### 4.2 异步方法（ipcRenderer.invoke → ipcMain.handle）

- 环境：`getAppVersion()` → Promise（主进程 `app.getVersion()`，构建时源为 `desktop/package.json`，避免 preload 在 asar 下读 package.json 的坑）
- 服务器：`getServerUrl()` `getSavedServerConfig()` `getServerList()` `getPassword()` → Promise
- 服务器写：`saveServer(url, pwd)` `removeServer(url)` `setSSHPassword(pwd)` `connectToServer(url, pwd)` → Promise
- 隧道：`addForwardedPort(localPort, targetPort, host)` `removeForwardedPort(port)` `reconnectTunnel()` `reconnectTunnelAsync()` → Promise
- 隧道读：`getForwardedPorts()` `testPortReachable(port)` `isTunnelConnected()` `getTunnelError()` `getTunnelErrorType()` → Promise
- 文件：`downloadFile(path)` `downloadUrl(url, fileName)` `downloadBlob(base64, fileName)` → Promise
- 浏览器：`openInBrowser(port, protocol, host, path)` `openInSandbox(port, protocol, host, path, sessionId)` → Promise
- 通知：`getPendingNavigation()` → Promise
- 日志：`startLogCapture()` `stopLogCapture()` → Promise
- 分享：`shareText(text)` `shareFile(path, mime)` `shareFiles(pathsJson, mimesJson)` → Promise

### 4.3 前端消费点改造

`window.AndroidNative` → `window.ClawBenchNative` 全库改名（约 202 处，含测试）。**需 await 化的调用点**（主风险）：

- `useServerList.ts`：`getServerList()` / `saveServer()` / `removeServer()`（现为同步读）
- `usePortForward.ts`：`getForwardedPorts()` `testPortReachable()` `isTunnelConnected()` `getTunnelError*()`（读值需 await）
- `LoginView.vue` / `App.vue` / `SettingsCategory.vue`：同步读服务器配置/密码
- `download.ts`：`downloadFile/downloadUrl/downloadBlob`（目前 fire-and-forget，低风险）

## 5. Android 侧同步改名

- `MainActivity.java:422`：`addJavascriptInterface(new WebAppInterface(this), "AndroidNative")` → `"ClawBenchNative"`
- `JSErrorInjector.java`：`buildScript("AndroidNative")` → `"ClawBenchNative"`
- 静态 `android/app/src/main/assets/login.html`：`AndroidNative.*` → `ClawBenchNative.*`
- 重打 APK。**无向后兼容**（旧 APK 失去原生模式属预期）
- **上线顺序**：先部署改名的 web 前端 bundle（`ClawBenchNative` 契约生效），再发布新版 APK；避免出现"旧 web + 新 APK"或"新 web + 旧 APK"两者都拿不到桥的中间态。

## 6. Electron 主进程关键实现

### 6.1 隧道 `tunnel.ts`
`ssh2.Client` 建立 SSH 连接，起本地 localhost 监听，转发到服务器 host:port。语义等同 Android `BackgroundService`。隧道状态/错误类型暴露给 `isTunnelConnected` / `getTunnelError` / `getTunnelErrorType`。

### 6.2 密钥 `secrets.ts`
Electron 内置 `safeStorage` 加密 `setSSHPassword` 存的密码（不用已废弃的 keytar）。Linux 无 keyring 时降级明文 + 日志告警。

### 6.3 通知 `notification.ts`
原生 Notification，点击 → 聚焦主窗口 + `clawbench-open-session` 事件深链；复用 `getPendingNavigation` 冷启动兜底。

### 6.4 沙盒 `window.ts`
沙盒窗口用 partitioned session 隔离 cookie，替代 Android 的隔离进程。

## 7. 打包、发布与自动更新（Option A / A2）

### 7.1 构建产物
`desktop/` 用 electron-builder 产出各平台产物，打包为便携目录（asar + 运行时二进制），发布为 **`@xulongzhe/clawbench-desktop-<os>-<arch>`**（沿用 `internal/service/upgrade.go` 的包命名与平台映射风格）。版本号统一取自 `desktop/package.json`。

### 7.2 自升级器 `desktop/main/updater.ts`（镜像 `upgrade.go`）
1. 查 npm registry（国内 `registry.npmmirror.com`，海外 `registry.npmjs.org`）`@xulongzhe/clawbench-desktop-<os>-<arch>/latest`
2. 取 `dist.version / tarball / integrity(sha512)`
3. 与 `app.getVersion()` 比对，有新版才继续
4. 下载 tarball → 校验 sha512 integrity → 解压替换 app 便携目录 → 重启
5. 进度经 IPC 推给渲染进程（复用 `upgrade_update` 事件风格）

> 便携目录替换（A2）：与 Go 后端替换自身二进制 100% 同构，跨平台最简单，无安装器/签名折腾。

### 7.3 前置条件（发布流程）
- macOS 自动更新/安装需代码签名（Gatekeeper）
- Windows 建议签名（SmartScreen）
- 签名作为 CI 发布流程前置条件，写入本文档但不在本次代码范围内实现

## 8. 前端下载入口（PC 浏览器 + 配置页）

### 8.1 后端
新增 **`/api/desktop/latest`**：复用 `internal/service/upgrade.go` 的 registry 查询逻辑，返回：

```json
{
  "version": "0.1.0",
  "downloads": {
    "win32-x64":   "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/-/…",
    "darwin-arm64":"https://registry.npmjs.org/@xulongzhe/clawbench-desktop-darwin-arm64/-/…",
    "darwin-x64":  "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-darwin-x64/-/…",
    "linux-x64":   "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-linux-x64/-/…",
    "linux-arm64": "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-linux-arm64/-/…"
  }
}
```

**OS→arch 解析规则（明确）**：后端按 `os-arch` 键返回全部平台项；前端用 `navigator.userAgentData` / `navigator.platform` 检测当前 OS + arch，按优先级选键（darwin 优先 `darwin-arm64` 若 Apple Silicon，否则 `darwin-x64`）。若检测不到 arch，默认回退到该 OS 的 x64 项。

国内访问自动改写为 `registry.npmmirror.com`。公开端点，无需鉴权。

### 8.2 前端
- **PC 浏览器欢迎界面**（`WelcomeOverlay.vue`）：用 `usePlatformDetect.isPC` + OS 检测（UA/platform），展示"下载桌面版"按钮，按当前 OS 显示对应平台
- **配置页**（`SettingsCategory.vue`）：同样加"下载桌面版"
- 点击 → `downloadByUrl(...)` → Electron 走 `ClawBenchNative.downloadUrl`（保存对话框），Web 走 `<a>`

## 9. 测试

- **前端**：单测改 `ClawBenchNative` + async（Vitest），覆盖 await 化后的读操作
- **Electron 主进程**：抽纯模块单测（`store.ts`、`secrets.ts`、`tunnel.ts` 配置层、`updater.ts` 的 registry 查询/校验），mock `ssh2` / `safeStorage` / npm HTTP。**registry 查询逻辑须抽成纯函数**（不依赖 Electron，可单测），与 §8.1 的 Go 端点 `/api/desktop/latest` 保持一致的包名映射与 npmmirror 改写规则。
- **Android**：改名 + 重打 APK，跑现有测试

## 10. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 同步→异步桥契约不匹配（最大） | 逐调用点 await 化 + 单测覆盖；Electron preload 只暴露 async 契约 |
| 旧 APK 失去原生模式 | 已确认不做向后兼容，属预期 |
| npm 自升级跨平台替换 | 便携目录替换与 Go 同构，Windows/Linux 无签名依赖 |
| Linux 无 keyring 致 safeStorage 失效 | 降级明文 + 日志告警 |

## 11. 后续（不在本次范围）

- 系统托盘、原生应用菜单、单实例锁、`clawbench://` 协议深链、登录自启动
- 原生安装器（A1）分发，若初始安装体验需要
