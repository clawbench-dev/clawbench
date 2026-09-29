# 桌面端登录加载屏（对齐 Android splash）设计

日期：2026-09-26
状态：待实施

## 目标

桌面端（Electron）在「点击连接」与「冷启动连已保存服务器」时，出现一段**无任何反馈的白屏**：
登录页被 `loadURL` 换掉后窗口先空白（远程页面加载中），随后继续空白（`App.vue` 在
`isAuthenticated === null` 时只渲染一个 `display:none` 的空 div），直到 `/api/me` 与
`initializeApp()` 全部完成才出现主界面。

Android 没有这个问题：它有原生 splash 浮层（圆形 logo + 旋转光圈 + 分阶段文案 +
取消连接按钮），从点连接一直盖到 JS 挂载完成。

本设计为桌面端补上**行为等价**的加载屏。

## 现状

| | Android | 桌面端（改前） |
|---|---|---|
| 浮层 | `activity_main.xml` 的 `splashScreen`（原生 View 浮层） | 无 |
| 显示时机 | `connectToServer()` / 冷启动有 savedUrl | — |
| 阶段文案 | 连接中 → 加载页面 → 渲染 → 初始化应用 | — |
| 收起 | JS `dismissSplash()` | `dismissSplash` 是空壳（`preload/index.ts:61`） |
| 兜底 | 15s 强制收起 + 90s 连接超时 | — |
| 取消 | 取消连接按钮 → 回登录页 | — |

**关键发现：前端一行都不用改。** `App.vue` 已经在**所有**初始化出口调用了
`dismissSplash()`（`useStartupGuard` 的 `finally`、服务不可达、401、非 2xx 各分支），
桌面端只是把那个调用接到了空实现。因此本设计**纯桌面端**改动。

## 已实测验证的前提

在真实 Electron 44.4.3 运行时上（Xvfb + scrot 截图）验证了本方案的三个前提：

1. `win.contentView.addChildView(view)` 的 `WebContentsView` **渲染在窗口自身页面之上**
   （蓝色主页面 + 红色浮层 → 截图确认浮层覆盖）。
2. `view.setVisible(false)` 能**露出**下层主页面。
3. `addChildView` + `setVisible(true)` 可重新置顶显示（截图与首次逐字节一致）。

API 依据：`View.setVisible` / `View.setBounds` / `View.setBackgroundColor` /
`WebContentsView` / `BaseWindow.getContentBounds` 均存在于 `electron.d.ts`。

## 设计

### 架构

新增一个**原生浮层视图**，挂在主窗口的 `contentView` 上，独立于主页面存在，因此能跨越
「登录页 → 服务器页面」的导航存活：

```
BrowserWindow
└── contentView (View)
    ├── [主页面 webContents]      ← 登录页 / 服务器页面，导航会切换
    └── splashView (WebContentsView)  ← 加载 login.html?splash=1，浮在最上层
```

浮层内容**复用 `desktop/assets/login.html`**（加 `?splash=1` 模式），理由：

- 该文件已内联全部 37 套主题调色板、i18n、主题解析与 `ClawBenchNative` preload。
- 新建独立 `splash.html` 会让 37 套主题色**第三次复制**——现有
  `desktop/src/shared/theme.test.ts` 的注释明确记录过这类漂移事故（六个主题的
  `--accent-rgb` 抄错邻居、dracula 整条缺失）。
- 复用则**零新增资源文件**，`electron-builder.yml` 的 `extraResources`、
  `scripts/stage-payload.mjs` 的 `REQUIRED` 清单、release.yml 的两处校验
  **全部无需改动**。

### 状态机（对齐 Android）

| 触发 | 阶段文案 | 备注 |
|---|---|---|
| 显示浮层（点连接 / 冷启动有 savedUrl） | 正在连接… | 同时 arm 90s 连接超时 |
| `did-start-loading` | 正在加载页面… | |
| `dom-ready` | 正在渲染… | |
| `did-finish-load` | 正在初始化应用… | 同时 arm 15s 兜底 |
| JS `dismissSplash()` | — | 淡出隐藏，取消两个定时器 |
| 取消连接按钮 | — | 停载 → 隐藏 → 回登录页 |
| 90s 连接超时 | — | 隐藏 → 回登录页 + 错误提示 |
| `did-fail-load` / `showLoginPage()` | — | 隐藏（复用现有回退链） |

Android 用 `onProgressChanged` 的百分比映射四个阶段；Electron 的 `webContents` 没有
进度回调，改用上述四个生命周期事件近似映射，阶段语义一致。

### 组件

**新增 `desktop/src/main/splashPolicy.ts`（纯函数，可测）**

- `SplashStage` 枚举 + `stageForEvent(event)` 映射
- `SPLASH_FAILSAFE_MS = 15_000`、`CONNECTION_TIMEOUT_MS = 90_000`
- `splashBackgroundColor(themeId)`：按 dark/light 返回**两个常量之一**，仅用于
  HTML 首帧绘制前的底色（避免深色主题闪白）。**不复制整套调色板**——登录页自身会
  在 head 里同步应用真实主题色，此常量只覆盖首帧前的极短窗口。
- `shouldShowSplash(url, loginUrl)`：仅对**远程 URL** 显示；本地登录页不显示。

**新增 `desktop/src/main/splash.ts`（Electron 生命周期）**

- `createSplashView(win)` / `showSplash(win)` / `dismissSplash(win, opts)` /
  `armFailSafe` / `cancelTimers` / `cancelAndReturnToLogin(win)`
- 负责 `setBounds` 跟随窗口尺寸、`setBackgroundColor`、淡出、两个定时器
- 保持**薄**：所有判断走 `splashPolicy.ts`

**改 `desktop/src/main/window.ts`**

- `createMainWindow()` 里创建浮层视图并挂到 `contentView`
- `resize` → 同步 bounds（`getContentSize()`）
- 冷启动有 savedUrl 时 `showSplash()`
- `did-fail-load` 回退登录页时 `dismissSplash()`
- `showLoginPage()` 里 `dismissSplash()`

**改 `desktop/src/main/bridge.ts`**

- `native:connect-to-server` → `showSplash()` 后 `loadURL`
- 新增 `native:dismiss-splash`（复用前端既有调用）
- 新增 `native:splash-cancel`（取消按钮）

**改 `desktop/src/preload/index.ts`**

- `dismissSplash: () => ipcRenderer.send('native:dismiss-splash')`（替换空壳）
- 新增 `cancelSplash: () => ipcRenderer.send('native:splash-cancel')`

**改 `desktop/assets/login.html`**

- 新增 `?splash=1` 模式：显示 splash 标记（logo + 旋转光圈 + 阶段文案 + 取消按钮），
  隐藏登录表单
- 新增 4 条阶段文案 + 1 条取消按钮文案的 i18n（zh/en）
- 新增 `window.__splashSetStage(stage)` 供主进程经 `executeJavaScript` 推进阶段
- 取消按钮 → `ClawBenchNative.cancelSplash()`
- 光圈复用 Android `splash_sweep.xml` 的视觉（外圈暗环 + 90° 亮弧旋转，1.6s 线性无限）

### 数据流

```
[点连接] LoginView/login.html → ClawBenchNative.connectToServer()
   → bridge: showSplash() → loadURL(server)
   → splashView 加载 login.html?splash=1（本地，立即绘制）
   → 主页面生命周期事件 → executeJavaScript(__splashSetStage)
   → 主页面 did-finish-load → arm 15s 兜底
   → App.vue initializeApp() 完成 → dismissSplash() → IPC → 淡出
```

### 错误处理

- **15s 兜底**：`did-finish-load` 后 JS 若始终不调 `dismissSplash()`（初始化抛错、
  桥不可用），强制隐藏并 `AppLog.w` 记录，避免永久卡在浮层。
- **90s 连接超时**：显示浮层时 arm，任何隐藏路径都取消。超时则隐藏 + 回登录页 +
  经 `onConnectError` 提示（复用现有注入脚本）。
- **取消**：停载主页面 → 隐藏浮层 → 回登录页（与 Android 一致）。
- 所有路径都收敛到 `dismissSplash`，它是幂等的。

### 测试

**单元测试 `desktop/src/main/splashPolicy.test.ts`**（纯函数）

- 四个生命周期事件 → 四个阶段的映射，含未知事件
- 超时常量值断言（防止有人顺手改小）
- `shouldShowSplash`：远程 URL 为真、本地登录页为假
- `splashBackgroundColor`：dark/light 主题各返回预期常量、未知主题回落默认

**源码守卫测试 `desktop/src/main/splashWiring.test.ts`**（仓库既有惯例）

- `preload/index.ts` 的 `dismissSplash` 不再是空实现（必须发 IPC）——
  这是本次修复的核心，空壳回归会静默退化成原状
- `window.ts` 里创建浮层并挂到 `contentView`
- `login.html` 含 `?splash=1` 分支、`__splashSetStage`、取消按钮接线
- `login.html` 的 splash i18n 键 zh/en 成对存在

> 说明：`splash.ts` / `window.ts` / `bridge.ts` 都在模块顶层 import `electron`，
> 单测无法加载（与 `loadFailure.ts` 从 `window.ts` 抽出的原因相同），故逻辑下沉到
> `splashPolicy.ts`，接线用源码守卫钉住。

## 改动清单

| 文件 | 改动 |
|---|---|
| `desktop/src/main/splashPolicy.ts` | 新增（纯逻辑） |
| `desktop/src/main/splash.ts` | 新增（视图生命周期） |
| `desktop/src/main/window.ts` | 创建浮层、挂监听、resize 同步、冷启动显示 |
| `desktop/src/main/bridge.ts` | 连接时显示 + 两个 IPC 通道 |
| `desktop/src/preload/index.ts` | `dismissSplash` 接真 + `cancelSplash` |
| `desktop/assets/login.html` | `?splash=1` 模式 + 文案 + 阶段接口 |
| `desktop/src/main/splashPolicy.test.ts` | 新增 |
| `desktop/src/main/splashWiring.test.ts` | 新增 |

**不改**：`electron-builder.yml`、`stage-payload.mjs`、`release.yml`、
`web/src/**`（前端零改动）、`desktop/src/shared/theme.ts`。

## 验证方式

1. `cd desktop && npx tsc --noEmit -p tsconfig.json` — 类型
2. `cd desktop && npx vitest run src/main/splashPolicy.test.ts src/main/splashWiring.test.ts`
3. 隔离跑既有桌面测试，确认无回归（`desktop/src/**` 的 `*.test.ts`）
4. 真机手测（用户执行）：
   - 冷启动连已保存服务器 → 应见 logo + 光圈 + 阶段推进 → 淡出进主界面
   - 登录页点连接 → 同上
   - 服务器不可达 → 浮层消失并回登录页且带错误提示
   - 加载中点取消 → 回登录页
   - 深色 / 浅色主题各验一次无白闪

## 风险与取舍

- **复用 `login.html` 承担双重职责**：登录页多了一个「品牌启动屏」身份。换来的是
  零调色板重复、零打包改动。已与用户确认采纳。
- **首帧底色用 2 常量近似**：`setBackgroundColor` 只覆盖 HTML 首帧前的极短窗口，
  真实主题色由登录页在 head 里同步应用。为两个常量的视觉差异复制整套 37 色调色板
  不划算（Android 的 `ThemePalette.java` 正是那种重复，且需要漂移测试守着）。
- **首启无 savedUrl 不显示浮层**：此时主窗口加载的是**本地**登录页，无网络等待，
  显示浮层只会是一帧无意义的闪烁。浮层只覆盖「有真实加载发生」的远程导航。
- **`desktop/package.json` 当前处于他人中间态**（工作树版本被剥掉 `scripts` /
  `devDependencies`，与 HEAD 不一致）——本次改动不触碰该文件。
