# 前后端完整插件化设计

日期：2026-11-15
状态：已实现（2026-11-15）
前置文档：[2026-11-14 Dock 插件化重构设计](2026-11-14-dock-plugin-architecture-design.md)（已完成的部分）、[Dock 插件开发指南](../plugin-dev/README.md)

> 落地后的操作手册以代码为准：[`docs/plugin-dev/README.md`](../plugin-dev/README.md)（前端）、[`docs/spec/features/plugin-registry.md`](../spec/features/plugin-registry.md)（后端）。本篇保留设计推理与取舍。

---

## 0. 摘要

一句话：**把"加一个功能"从"改十几处"降到"加两个文件 + 一处登记"。**

- **前台**：现在加一个页签要动 7 个文件；做完只要加 `plugin.ts` + 面板组件，**不再改任何既有文件**（清单里加一行 id 除外，那是刻意的）。
- **后台**：现在加一个"功能总开关"要动 12 处；做完只要**一个 `Register(...)` 调用 + 一个 config 字段**，路由拦截、worker 启停、热更新、`/api/features`、文档守卫全部自动派生。
- **两端对齐**：功能开关的**唯一权威在服务端**（Go 注册表），前端不再维护 `FEATURE_FLAG_DEFAULTS` 这份镜像默认值，改从 `GET /api/features` 读。

### 实现差异（落地时改了设计的地方）

- **深链不是模板，是函数**。设计里写的是 `{ followsProject, validate, apply }`；实现改成 `{ event, handler(detail, ctx) }`，`ctx` 提供 `switchTab` / `hotSwitchProject` / `tabBlocked` / `recordNotificationOrigin` / `currentProject` 五个原语。原因是 Forge 那个跳转的**顺序是承重的**（先拒绝并清目标 → 发布目标 → 同项目跳转或跨项目切换 → 切换没落地再清目标），模板表达不了，硬套会静默改行为。
- **打开设置分类时"从哪来"由插件自己记**。宿主只提供通用的 `openSettingsCategory(id)`；会议中继设置要能返回创建页，那一步（`setMeetingCreateReturn()`）留在插件里，宿主不参与。
- **终端状态端点必须豁免**。设计没提。整段门控会让 `/api/terminal/status` 返回 503，前端拿不到 `platform_supported` 就把平台能力一起置 false，重新打开终端后开关卡在"已开但置灰"、页签不出现，只能刷新。
- **启动顺序是第一版实现的真 bug**。`registerPlugins` 必须早于 `startPlugins` 与 `RegisterRoutes`：晚了注册表还是空的，于是没有任何 worker 启动、没有任何路由被门控，而启动日志干干净净。补了一条读 `main.go` 源码比位置的守卫。
- **预设不是"按 group 派生"**。设计里写了 `hideGroups`，实现是纯规则 + 显式例外（`hideTabs` / `keepOnly: 'permanent'` / `parkHiddenBefore` / `disableFeatures` / `disableAllFeatures`），从 `DOCK_TAB_ORDER` 与 `DOCK_FEATURES` 派生 order 与开关计划。

### 术语纠正（按你的习惯提醒）

你说"插件化"，严格讲这套东西叫 **可插拔的模块化架构**（pluggable modular architecture）。

- **插件化 / plugin architecture** 通常指"第三方能安装扩展、远程加载代码、有沙箱和权限模型"——那是另一个量级的工程（要处理权限、版本兼容、代码签名、崩溃隔离）。
- 我们要做的是**内部模块可插拔**：功能是自带的，但"增加/移除一个功能"的边际成本接近零。

这个区别决定了后面所有设计取舍：**不做运行时热加载、不做沙箱、不做第三方市场**。

---

## 1. 现状盘点（带证据）

### 1.1 已经做好的（不要重做）

| 能力 | 落点 | 状态 |
|---|---|---|
| 页签身份清单 | `web/src/composables/dockTabs.ts`（`DOCK_TABS`，零 import） | ✅ |
| 面板统一挂载 | `components/dock/DockPanelHost.vue` + 各功能 `dock.ts` | ✅ |
| 角标 / 可用性 / onEnter | `composables/dockPluginTypes.ts` 的 `DockPluginRuntime` | ✅ |
| 显示开关 / 拖动排序 / 三个预设 | `useDockPlugins.ts` + `DockPluginSetting.vue` | ✅ |
| 三种"关闭"语义分开 | 能力 `capability` / 功能 `feature` / 偏好 `hidden` | ✅ 红线，见 §3 |
| 后台热更新框架 | `hotReloadFields` + `hotReloadReconfigure` | ⚠️ 有框架无注册表 |
| 六个服务端总开关 | `forge/mail/terminal/kb/tasks/share` | ⚠️ 每个写法不同 |

### 1.2 前台还差什么

| # | 问题 | 证据 | 后果 |
|---|---|---|---|
| F1 | 聚合层手写 | `dockPluginRuntime.ts` 顶部 14 行 import + `RUNTIME` 表 14 行 | 每加插件改这个文件 |
| F2 | 图标在第二个文件 | `dockTabMeta.ts` 的 `DOCK_TAB_ICONS` 是 `Record<DockTabId, Component>`，类型强制覆盖全部 id | 每加插件改这个文件 |
| F3 | 三份 Dock 按钮模板 | 宽屏竖 Dock（App.vue ~70 行）、窄屏内联（~30 行）、溢出菜单（~15 行）各写一遍按钮+角标+动画 | 加一种角标外观要改三处 |
| F4 | 宿主硬编码具体插件 | `tab === 'tasks' && taskUnreadCount / taskJustCompleted / taskRunning`（4 处）；`if (tab === 'terminal') handleDockTerminal()`（2 处）；`liveDotVisible` 里写死 `tab === 'live'` | 新插件想要"进行中"外观只能改 App.vue |
| F5 | 可用性是三套散装 | `unavailableDockTabs`（App.vue:2099）、`featureDisabledDockTabs`（:2118）、`resolvePanelTab(…, forgeHidden, mailHidden)`（useProjectPanel.ts:94-108） | 第三个需要门控的插件要加第三个布尔参数 |
| F6 | `onLeave` 是死代码 | `useDockLifecycle` 暴露 `onLeave`，全仓**无任何调用** | 契约不存在 |
| F7 | 深链逐插件手写 | App.vue 里 `clawbench-open-mail` / `-forge` / `-meeting` 各一个 handler + 注册 + 反注册，各自处理跨项目重派发 | 新插件要能被通知点进去就再写一个 |
| F8 | 宿主能力是封闭接口 | `DockPanelHost` 10 个具名成员，其中 `openMailSettings()` 是单插件专用 | 新插件想开自己的设置页没有通用口子 |
| F9 | 预设手写三份 | `DOCK_PRESET_PLANS` 里 3 个预设 × 3 个数组（hidden / order / features），共 14 个 id 手抄 3 遍 | 新插件漏登记不报错，只是位置怪 |
| F10 | 设置页双登记 | `settingsFieldMap.ts` 的 `categoryItems` + `SettingsIndex.vue` 的 `groupDefs`（含图标 import） | 加设置页改两个文件 + 两处 i18n |
| F11 | 缺守卫 | 无"预设覆盖全部页签"测试、无"`descriptionKey` 中英都存在"测试、无"`tab.feature` ∈ 默认值"测试、`e2e/specs` 无 dock 管理器 spec | 漏登记静默通过 |

### 1.3 后台还差什么

| # | 问题 | 证据 | 后果 |
|---|---|---|---|
| B1 | 没有功能注册表 | "功能是否启用"有 4 种问法：`ConfigInstance.Forge.Enabled` / `model.TasksEnabled` 包级全局 / `model.FeaturePolicy.*` / `kb.Available()` | 新功能只能照着抄一遍 |
| B2 | 一个开关 12 处 | 见 §1.4 表格 | 漏一处就是静默 bug |
| B3 | 接口门控各写各的 | 任务/分享：`rejectIfTasksDisabled` → 409；终端：`terminalMgr == nil` → 503；Forge：2 个接口手写 409（读接口不拦）；知识库：`kb.Available()`；**邮件：`internal/handler/mail.go` 里 `Mail.Enabled` 出现 0 次** | 「关掉邮件」只停了后台收信，`/api/mail/send` 照样能发 |
| B4 | worker 生命周期手排 | `main.go` 里 20 行 `service.StartXxxWorker()` + `defer service.StopXxx()`，热更新再各一个 `hotReloadXxx`（6 个函数，19 处引用） | 加 worker 改 main.go 三处 |
| B5 | 没有 `GET /api/features` | 前端各自去 `/api/config` 里按形状掏 `serverConfig.forge.enabled` | 每个 `dock.ts` 复写一遍掏取 + "缺键当开"判断 |
| B6 | 没有"为什么不可用"的统一回答 | 服务端不区分能力/功能/环境 | 前端靠 `unavailableReasonKey` 硬编码文案 |
| B7 | 没有接口归属概念 | `apiPrefix` 只在 `useCrudList` 里当状态桶 key，与"谁管这个前缀"无关 | 没有任何机制要求新接口加门控 → B3 的邮件洞 |
| B8 | i18n 错误键靠人记 | `i18n_test.go` 里一个手写的 pinned keys 列表 | 新功能的 `XxxDisabled` 漏了只在一个语言里裸奔 |

### 1.4 「一个服务端开关」现在要动的 12 处

以 `mail.enabled` 为例：

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `internal/model/config.go` | `MailConfig` 加 `Enabled bool` |
| 2 | `internal/model/defaults.go` | `if !presence["mail.enabled"] { cfg.Mail.Enabled = true }` |
| 3 | `internal/handler/settings.go` | `configResponse` 加 `Mail configMail` 字段 |
| 4 | 同上 | `type configMail struct` 定义 |
| 5 | 同上 | `serveConfigGet` 里赋值 |
| 6 | 同上 | `PatchableConfigPaths["mail.enabled"] = true` |
| 7 | 同上 | `hotReloadFields["mail.enabled"] = true` |
| 8 | 同上 | `applyConfigPatch` 里手写 `if mailMap, ok := patch["mail"]...` 分支 |
| 9 | 同上 | 关闭副作用（`applyFeatureDisableEffects` 或 `clearForgeBindingsOnDisable`） |
| 10 | `cmd/server/main.go` | 起 worker + `defer Stop` + `hotReloadReconfigure` 里接一个 `hotReloadXxx` |
| 11 | `internal/api/openapi.yaml` | `/api/config` 的 description + schema |
| 12 | `docs/user-guide/configuration.md` | 补一行（有 `TestConfigReferenceMatchesConfigFile` 卡着） |

**外加**：`internal/i18n/locales/active.{zh,en}.yaml` 的 `XxxDisabled` 键 + `i18n_test.go` 的 pinned 列表。

---

## 2. 目标与非目标

### 2.1 目标

1. **加一个前台页签**：新增 `plugin.ts` + 面板组件两个文件；既有文件里只允许出现**一行**（`dockTabs.ts` 的 id）。
2. **加一个服务端功能开关**：一个 `plugins.Register(...)` + 一个 config 字段 + 一个 i18n 键；路由拦截、worker、热更新、`/api/features`、OpenAPI 检查全部自动。
3. **前端不再维护功能开关的默认值镜像**：`FEATURE_FLAG_DEFAULTS` 删除，改由服务端下发。
4. **三种关闭语义保持分开**（能力 / 功能 / 偏好），这是上一轮定的红线，不能合并成一个布尔。
5. **迁移期间行为不变**：所有功能都开着时，界面与现在完全一致。

### 2.2 非目标

| 不做 | 原因 |
|---|---|
| 第三方安装 / 远程加载插件代码 | 需要沙箱、权限、版本兼容、崩溃隔离，另一个量级的工程 |
| 运行时 `registerPlugin()` 动态注册（前台） | 静态 glob 已足够；动态注册会让打包、类型、顺序全部失控 |
| 每个插件独立进程 / 独立二进制 | 没有隔离需求，只会让调试和部署变复杂 |
| 按项目开关插件 | 首版全局；Forge 项目绑定、任务项目归属维持现状 |
| 重写面板内部业务 | 只搬外壳与资源 |
| 把聊天 / 文件管理 / 文件预览搬出 App.vue | 它们与 App.vue 状态耦合极深，收益为负（见 §7 风险 1） |

---

## 3. 核心模型：三种"关闭"必须继续分开

这是上一轮最重要的结论，这一轮**不能因为"统一"而合并**。

| 类型 | 谁决定 | 关掉后真实效果 | 例子 | 界面控件 |
|---|---|---|---|---|
| **能力** capability | 环境 / 宿主 | 本来就没有 | 终端（Windows 无 PTY）、端口映射（无可用传输） | 灰显 + 原因，无开关 |
| **功能** feature | 服务端配置 | 服务端真停（worker / 轮询 / 接口拒绝） | Forge、邮件、任务、分享、知识库、终端 | 真开关，文案写清后果 |
| **偏好** preference | 这台设备 | 只是不显示入口，后台照常 | 历史、统计、会议、通知中心 | 眼睛图标 |

可见性公式不变：

```
可见 = 能力可用 && 功能已开 && (不可关 || 用户没有隐藏)
```

**这一轮新增的是**：把"功能"这一轴的**判定来源**从"前端猜"换成"服务端下发"。

---

## 4. 总体架构

```
┌───────────────────────── 服务端（唯一权威） ─────────────────────────┐
│                                                                     │
│  internal/plugins/            ← 新增：功能注册表（只依赖 model）      │
│    plugin.go    类型定义 + Register()/All()/Lookup()                │
│    gate.go      路由门控中间件（按 apiPrefix 自动套）                 │
│                                                                     │
│  cmd/server/main.go           ← 唯一注册点（仿 internal/ai/backends）│
│    plugins.Register(&plugins.Plugin{...})                           │
│                                                                     │
│  派生出去的东西（全部自动，不用手写）：                                │
│    GET /api/features         ← 每个功能的 {id, enabled, available,  │
│                                 reason, defaultOn}                   │
│    PATCH 白名单              ← 由 Section 派生                       │
│    hotReload 分发            ← 由 Plugin.Reconfigure 派生            │
│    worker Start/Stop         ← 由 Plugin.Start/Stop 派生             │
│    路由 409 门控             ← 由 Plugin.APIPrefixes 派生            │
│    OpenAPI / 文档守卫        ← 由注册表枚举                          │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                          GET /api/features
                                   ▼
┌───────────────────────── 前端（纯消费） ────────────────────────────┐
│                                                                     │
│  web/src/plugins/<name>/plugin.ts   ← 每个插件一个自描述文件         │
│    清单 + 图标 + 面板 + 角标 + 生命周期 + 设置页声明                 │
│                                                                     │
│  composables/dockTabs.ts            ← 只剩"身份与默认顺序"（纯数据） │
│  composables/dockPluginRuntime.ts   ← 改成 import.meta.glob 自动聚合 │
│  composables/dockTabMeta.ts         ← 删除                           │
│  composables/useFeatureFlags.ts     ← 改读 /api/features             │
└─────────────────────────────────────────────────────────────────────┘
```

**关键原则：两端只在"功能 id + 开关 key"这一处对齐，且服务端说了算。**

---

## 5. 服务端设计

### 5.1 新包 `internal/plugins`

**只依赖 `internal/model`**（和 `internal/ai/backends/plugin.go` 一样的位置与约束），不 import service / handler / kb / mail，否则成环。

```go
package plugins

// Plugin 是一个功能的完整自描述单元。
//
// 它刻意只放"声明"，不放实现：Start/Stop/Reconfigure 是函数值，由
// cmd/server/main.go 在注册时注入。这样这个包不需要 import service，
// 也就不会和 service 形成环（与 internal/ai/backends 同一手法）。
type Plugin struct {
    // ID 是稳定标识，同时是前端页签 id、/api/features 的键、
    // i18n 错误键前缀。全小写，不允许改（改了等于换了一个功能）。
    ID string

    // Section 是 config.yaml 里的段名（"mail" / "forge" / "kb" / …）。
    // 空表示这个功能没有服务端总开关（纯偏好型，如"统计"）。
    //
    // 它派生三件事：PATCH 白名单、GET /api/config 的段、/api/features 的
    // enabled 值。
    Section string

    // Enabled 读取当前是否开启。nil 表示恒开。
    //
    // 必须读 ConfigInstance 的实时值，不能读启动时的快照：热更新会在
    // 进程生命周期内改它。
    Enabled func() bool

    // DefaultOn 只用于文档与"缺键当开"的语义声明；真正的默认值由
    // model.ApplyDefaults 的 presence 检查决定（那里才是唯一实现）。
    DefaultOn bool

    // APIPrefixes 声明"这些路由属于我"。注册路由时命中的前缀会被自动
    // 套上功能门控（见 §5.4）。这是 B7 的解药：不声明 = 没有门控，且
    // 守卫测试会拒绝"有 Section 但没有 APIPrefixes"的插件。
    APIPrefixes []string

    // Start / Stop / Reconfigure 是后台资源的生命周期。
    //
    // Start 必须幂等（重复调用不叠加 goroutine），Stop 必须幂等。
    // Reconfigure 在热更新后调用，用于"改了间隔 / 改了批次大小"这类
    // 不需要重启的调整。三者都可为 nil。
    Start       func(cfg model.Config)
    Stop        func()
    Reconfigure func(cfg model.Config)

    // DisabledMsgKey 是接口被拒时的 i18n 键（如 "MailDisabled"）。
    // 空则用 "<ID>Disabled" 的约定。
    DisabledMsgKey string

    // DisabledStatus 是拒绝时的 HTTP 状态码。0 表示 409 Conflict
    // （"你有权限，功能关着"，客户端统一处理）。终端用它报 503，
    // 因为那是"运行时没起来"而不是"被关掉了"。
    DisabledStatus int
}
```

注册表本体（与 `backends` 完全同构）：

```go
func Register(p *Plugin)      // 重复 ID panic（编程错误）
func Lookup(id string) *Plugin
func All() []*Plugin           // 按 ID 排序，保证 /api/features 稳定
func ForRoute(path string) *Plugin  // 最长前缀匹配，供门控中间件用
```

### 5.2 注册点：`cmd/server/main.go`

与 `_ "clawbench/internal/ai/backends/claude"` 那批空白 import 同一个思路——**main 是唯一知道所有实现的地方**，注册表本身不认识任何实现。

```go
plugins.Register(&plugins.Plugin{
    ID:      "mail",
    Section: "mail",
    Enabled: func() bool { return model.ConfigInstance.Mail.Enabled },
    DefaultOn: true,
    APIPrefixes: []string{"/api/mail/"},
    Start: func(cfg model.Config) { service.StartMailSyncWorker() },
    Stop:  service.StopMailSyncWorker,
})

plugins.Register(&plugins.Plugin{
    ID:      "forge",
    Section: "forge",
    Enabled: func() bool { return model.ConfigInstance.Forge.Enabled },
    DefaultOn: true,
    APIPrefixes: []string{"/api/forge/"},
    Start:  func(cfg model.Config) { startForgePoller() },   // 现有那段包成函数
    Stop:   service.StopForgePoller,
    // forge 关掉时要清绑定 —— 这是"关闭副作用"，见 §5.6
    OnDisable: clearForgeBindings,
})

plugins.Register(&plugins.Plugin{
    ID:      "terminal",
    Section: "terminal",
    Enabled: func() bool { return model.ConfigInstance.Terminal.Enabled },
    DefaultOn: true,
    APIPrefixes: []string{"/api/terminal/"},
    DisabledStatus: 503,      // 终端报 503 是既有行为，保留
    Start:  startTerminalManager,
    Stop:   stopTerminalManager,
    Reconfigure: reconfigureTerminalManager,
})
```

**纯偏好型**（没有服务端开关）的插件也要注册，这样 `/api/features` 能告诉前端"这个功能存在且可用"：

```go
plugins.Register(&plugins.Plugin{ID: "stats", APIPrefixes: []string{"/api/usage/", "/api/git/stats"}})
```

### 5.3 `GET /api/features`

新增端点，返回**全部**已注册功能的状态：

```json
{
  "features": [
    {"id":"forge","enabled":true,"available":true,"reason":"","defaultOn":true},
    {"id":"terminal","enabled":true,"available":false,"reason":"capability","defaultOn":true},
    {"id":"mail","enabled":false,"available":true,"reason":"","defaultOn":true}
  ]
}
```

- `enabled`：`Plugin.Enabled()`，无 Section 时恒 `true`。
- `available`：**运行时**可用性（不是配置）。目前只有终端会 `false`（PTY 没起来 / 平台不支持），由 `Plugin.Available func() bool` 可选提供。
- `reason`：`"disabled"` / `"capability"` / `""`。前端据此决定灰显文案，**不再各自硬编码 `unavailableReasonKey`**。
- `defaultOn`：让前端在请求未落地时能有个合理的首屏判断（见 §6.5）。

鉴权：走 `middleware.Auth`，与 `/api/config` 一致。

OpenAPI：新增 `tag: Features`，operationId `featuresList`，**必须同步**（`openapi_drift_test.go` 会拦）。

### 5.4 路由门控：从"每个接口写 if"到"注册时自动套"

现在是：

```go
// 每个 handler 自己记得写
if rejectIfTasksDisabled(w, r) { return }
```

改成在 `RegisterRoutes` 的 `register` 闭包里自动判断：

```go
register := func(pattern string, handler http.HandlerFunc) {
    h := handler
    if p := plugins.ForRoute(pattern); p != nil {
        h = plugins.Gate(p, h)   // 见下
    }
    registerRoute(mux, pattern, middleware.Auth(h))
    ...
}
```

```go
// Gate 在功能关闭时直接拒绝，不进入 handler。
func Gate(p *Plugin, next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if p.Enabled != nil && !p.Enabled() {
            status := p.DisabledStatus
            if status == 0 { status = http.StatusConflict }
            // 复用 handler 层的本地化错误体
            writeGateError(w, r, status, p.disabledKey())
            return
        }
        next(w, r)
    }
}
```

**为什么这样能修掉邮件的洞**：`/api/mail/` 前缀一注册，`/api/mail/send`、`/api/mail/messages` 全部自动被拦，不需要任何人记得写检查。

**保留的例外**（必须显式声明，不能靠遗漏）：

| 场景 | 处理 |
|---|---|
| 读接口在功能关闭时仍要能读（Forge 的 `GET /api/forge/binding` 返回空） | `Plugin.GateExempt []string` 列出精确路径 |
| 关闭后仍要能"打开"（`PATCH /api/config` 本身） | 不在任何插件前缀下 |
| 分享关闭后旧链接要 404（不是 409） | 走 `share.enabled` 的既有判定，不进 Gate（`/api/share/{token}` 是公开端点，不在 auth 链上） |

**迁移策略**：先让 Gate 只**记录日志不拦截**（`plugins.GateShadow`），跑一轮测试 + 手工点一遍，确认每个会被拦的接口都该拦，再切成真拦。这避免"上线即全站 409"。

### 5.5 worker 生命周期

```go
// 启动：遍历注册表
for _, p := range plugins.All() {
    if p.Start != nil && (p.Enabled == nil || p.Enabled()) {
        p.Start(model.ConfigInstance)
    }
}

// 关闭：defer 里反向遍历
for _, p := range plugins.All() {
    if p.Stop != nil { p.Stop() }
}
```

热更新同理：`hotReloadReconfigure` 末尾加一段遍历，按"开关是否翻转"决定 `Start`/`Stop`，否则调 `Reconfigure`。

这样 `main.go` 里那 20 行 `StartXxx` + 6 个 `hotReloadXxx` 函数**逐步消失**（每个插件迁移一次）。

### 5.6 关闭副作用（destructive disable）

三个功能在关闭时有不可逆动作：

| 功能 | 关闭时做什么 | 现在在哪 |
|---|---|---|
| Forge | 清空所有项目的仓库绑定 | `clearForgeBindingsOnDisable` |
| 分享 | 撤销所有已发链接（旧链接 404） | `applyFeatureDisableEffects` |
| 任务 | 取消运行中执行、丢弃排队事件 | `GlobalScheduler.SetEnabled(false)` |

注册表加一个字段：

```go
// OnDisable 在开关从 true 变 false 且配置已成功落盘后调用。
// 返回 warnings（部分失败时报告给客户端，但不回滚配置）。
OnDisable func() []string
```

**执行时机不变**：必须在 `writeConfigYAML` 成功之后（现有的注释已经解释过为什么——先撤销再写盘失败会毁掉用户的绑定）。

### 5.7 PATCH 白名单与热更新自动派生

现在是两张手写 map（188 + 187 个键）。改成：

```go
// 派生：每个插件的 Section 下所有 yaml 字段自动进白名单。
// 仍保留一张显式的"非插件配置"白名单（chat/tts/stt/rag/…），
// 因为它们不属于任何功能开关。
```

实现上用反射遍历 `model.Config` 里被插件 Section 命中的子结构，把 `section.*` 全部加进 `PatchableConfigPaths`。

**守卫**：`TestPatchablePathsExistInConfig` 已经在跑，派生后仍然通过；再加一条 `TestEveryPluginSectionIsPatchable`。

> ⚠️ 这一步风险最高（反射 + 白名单 = 安全边界）。建议**保留手写白名单，只加一条守卫测试**：`TestEveryPluginSectionFullyWhitelisted` 检查"注册表声明的 Section 下每个 yaml 字段都在 `PatchableConfigPaths` 里"。收益（漏写会红）与风险（反射误放行）之间，这样更划算。

### 5.8 i18n 错误键

约定：`<ID>Disabled`。注册时若 `DisabledMsgKey` 为空则按约定推导。

守卫（替代现在的手写 pinned 列表）：遍历注册表，断言每个 `DisabledMsgKey` 在中英两个 yaml 里都存在。这比现在的 `i18n_test.go` 手抄列表可靠。

---

## 6. 前端设计

### 6.1 一个插件一个 `plugin.ts`

目录结构从：

```
components/mail/
  dock.ts          ← 运行时
  MailPanel.vue
  …10 个组件
composables/dockTabMeta.ts     ← 图标（另一个文件）
composables/dockPluginRuntime.ts ← 聚合（又一个文件）
```

改成：

```
web/src/plugins/mail/
  plugin.ts        ← 自描述：清单 + 图标 + 面板 + 角标 + 生命周期
  MailPanel.vue
  …10 个组件
```

`plugin.ts` 的形状：

```ts
import { Mail } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

export default {
  // ── 清单（原来是 dockTabs.ts 的一行）──
  id: 'mail',
  titleKey: 'nav.mail',
  descriptionKey: 'settings.dockPlugins.desc.mail',
  group: 'personal',
  closable: true,
  // 功能开关的 key 现在只是一个字符串：默认值由 /api/features 下发，
  // 前端不再有镜像表。
  feature: 'mail.enabled',
  settingsCategory: 'mail',

  // ── 图标（原来是 dockTabMeta.ts 的一行）──
  icon: Mail,

  // ── 运行时（原来是 dock.ts）──
  badge: () => ({ count: total.value }),
  panel: {
    load: () => import('./MailPanel.vue'),
    on: (host) => ({ 'open-settings': host.openSettingsCategory }),
  },
  startBackground: () => {
    startMailUnreadPolling()
    return stopMailUnreadPolling
  },
} satisfies DockPlugin
```

**约束不变**（上一轮定下的，继续守）：

1. `plugin.ts` **不能 import 自己的面板组件**（会破坏懒加载）——只用 `() => import(...)`。
2. `plugin.ts` 不能 import 别的插件。
3. `plugin.ts` 里不放重依赖（WebRTC、DOMPurify、编辑器）；重状态先抽轻量模块（会议就是这样做的）。
4. 不能在模块顶层调 `useSettingsConfig()`（测试 mock 会撞 TDZ）；要读就在函数体里读。

### 6.2 聚合改成 glob

```ts
// composables/dockPluginRuntime.ts
const modules = import.meta.glob<{ default: DockPlugin }>(
  '@/plugins/*/plugin.ts',
  { eager: true },
)

const RUNTIME: Record<string, DockPlugin> = {}
for (const mod of Object.values(modules)) {
  const p = mod.default
  if (!p?.id) continue
  RUNTIME[p.id] = p
}
```

**这个文件从此不再需要修改。** 加插件 = 加目录。

> `import.meta.glob` 是 Vite 编译期特性，构建时静态分析；vitest 也支持（已有先例：`SettingsIndex.test.ts` 用它读 locale 文件）。

### 6.3 `dockTabs.ts` 怎么办？

它有**零 import** 的硬约束（文件头写了原因：一批测试对 `lucide-vue-next` 做了窄 mock，加载期引入图标会集体炸）。

三个选项：

| 方案 | 做法 | 代价 |
|---|---|---|
| A（推荐） | `DOCK_TABS` 只留 `{ id, order }` 一份 id 列表，其余字段从 glob 结果派生 | 加插件仍需在 id 列表加一行（**这是好事**：默认顺序必须显式，不能让 glob 的字典序决定） |
| B | `dockTabs.ts` 从 `plugin.ts` 派生全部字段 | 破坏零 import 约束，窄 mock 测试集体红 |
| C | 保留现在的完整 `DOCK_TABS` | 清单字段写两遍（一处 manifest、一处 plugin.ts），必然漂移 |

**选 A。** 理由：默认顺序是产品决策，应该显式写在代码里，不能被文件系统的字母序决定（`browse` 必须第一、`settings` 必须在末尾附近）。

于是：

```ts
// dockTabs.ts（零 import，只剩顺序与身份）
export const DOCK_TAB_ORDER = [
  'browse', 'view', 'history', 'forge', 'tasks', 'terminal', 'stats',
  'mail', 'kb', 'live', 'share', 'proxy', 'notifications', 'settings',
] as const

export type DockTabId = (typeof DOCK_TAB_ORDER)[number]
export type TabId = 'chat' | DockTabId
```

其余（titleKey / group / closable / feature / capability / settingsCategory / icon）**全部从 plugin.ts 派生**，由 `dockPluginRuntime.ts` 组装成完整的 `DockTabDescriptor`。

`DockTabId` 类型从数组派生 → 新插件不加进数组就**类型报错**（`plugin.ts` 里 `id: 'snippets'` 不满足联合类型）。这比"运行时静默不渲染"好得多。

### 6.4 统一 Dock 按钮（上一轮 Phase 2，这次做掉）

新增 `components/dock/DockTabButton.vue`：

```vue
<script setup lang="ts">
defineProps<{
  tab: string
  active: boolean
  variant: 'wide' | 'inline' | 'popup'
}>()
</script>
```

内部职责：
- 图标（`resolveDockIcon(id) ?? plugin.icon`）
- 标题（`resolveDockTitle(id) ?? t(plugin.titleKey)`）
- 角标数字 / 进行中圆点 / 数字变化动画
- **状态类名**（`has-unread` / `has-running` / `just-completed`）——**从插件声明取，不再在 App.vue 里写 `tab === 'tasks'`**

为此 `DockPlugin` 增加一个声明：

```ts
/**
 * 按钮外观的状态。宿主不再知道任何具体插件。
 *
 * 每个状态返回 true 时，按钮挂上对应 class；样式仍在 App.vue 的 CSS 里
 * （那是"长什么样"），插件只声明"什么时候算"。
 */
appearance?: {
  unread?: () => boolean
  running?: () => boolean
  justCompleted?: () => boolean
  /** 进行中圆点（会议那种，没有数字） */
  liveDot?: () => boolean
}
```

任务插件：

```ts
appearance: {
  unread: () => store.state.taskUnreadCount > 0,
  running: () => store.state.taskRunning,
  justCompleted: () => store.state.taskJustCompleted,
}
```

会议插件：

```ts
appearance: { liveDot: () => liveState().value !== 'idle' },
```

**App.vue 里 6 处硬编码 + `liveDotVisible` 特判全部删除。**

同时删掉 8 个动画 ref 和 `badgeAnims` 映射表——动画状态收进按钮内部，按自己的 `badge.count` 变化触发。

### 6.5 可用性收敛成一个对象

```ts
// composables/useDockAvailability.ts
export interface DockAvailability {
  /** 服务端说这个功能关了 */
  featureDisabled: ReadonlySet<DockTabId>
  /** 宿主跑不了（PTY / 传输 / 原生能力） */
  unavailable: ReadonlySet<DockTabId>
  /** 用户在这台设备隐藏了入口 */
  hidden: ReadonlySet<DockTabId>
}
```

三个来源合成一个：

| 轴 | 来源 | 变化 |
|---|---|---|
| `featureDisabled` | `GET /api/features` 的 `enabled === false` | **改**：原来读 `/api/config` + 本地默认值表 |
| `unavailable` | `GET /api/features` 的 `available === false` + 前端本机探测（原生能力位） | **改**：原来每个 `dock.ts` 自己写 `available()` |
| `hidden` | `panelState` 的 `dockHidden` | 不变 |

**`resolvePanelTab` 去掉两个专用参数**：

```ts
// 现在
resolvePanelTab(remembered, isWideScreen, hasOpenFile, forgeHidden, mailHidden)

// 之后
resolvePanelTab(remembered, isWideScreen, hasOpenFile, availability)
```

内部逻辑从"两个 if"变成"查一遍 `dockTabs(availability)` 里有没有它"。第三个门控插件不再改签名。

**首屏判断**：`/api/features` 请求未落地时，按"功能开着"处理（与现在的"缺键 = 开"一致），避免入口闪一下消失。

### 6.6 生命周期：`onLeave` 接上，`onEnter` 去重

现状：`onEnter` 在两处被调用（App.vue:1382 的 `switchTab`、:3711 的宽屏 `sideEffects`），`onLeave` **零调用**。

改成统一在一个地方管：

```ts
// useDockLifecycle.ts
let current: DockTabId | null = null

function enter(id: string) {
  if (current === id) return           // 幂等
  if (current) runDockOnLeave(current)
  current = id
  runDockOnEnter(id)
}
```

两处调用点都改成 `dockLifecycle.enter(tab)`。这样：

- `onEnter` 不会因为窄屏/宽屏两条路径各调一次而重复执行；
- `onLeave` 真的会被调用（新插件可以"离开时暂停轮询"）。

### 6.7 深链统一

现在每个可被通知点进去的插件都在 App.vue 手写一个 handler。改成插件自己声明：

```ts
// plugin.ts
/**
 * 这个插件接受的窗口事件名，以及收到后要做什么。
 *
 * 宿主只负责：注册监听、必要时先切项目、把 detail 交给插件。
 * 跨项目的判断（"这条通知属于另一个项目"）由宿主统一处理，
 * 插件只关心"拿到 detail 之后做什么"。
 */
deeplinks?: {
  event: string                        // 'clawbench-open-mail'
  /** 需要切到 detail 里带的项目吗（会议、Forge 是 true，邮件是 false） */
  followsProject?: boolean
  /** 校验 detail，返回 false 表示忽略（宿主记一条 warning） */
  validate?: (detail: unknown) => boolean
  /** 宿主已经把页签切过来了，做插件自己的事 */
  apply: (detail: unknown) => void
}
```

宿主 `usePluginDeeplinks()` 遍历注册表，统一 `addEventListener` / `removeEventListener`、统一跨项目重派发、统一"功能关着就拒绝且不留残留状态"。

App.vue 里那 4 个 handler（`handleOpenForge` / `handleOpenMail` / `handleComposeMail` / `handleOpenMeeting`）搬进各自的 `plugin.ts`。

### 6.8 宿主能力改成通用口子

现在 `DockPanelHost` 是 10 个具名成员，`openMailSettings()` 是单插件专用。改成两层：

**通用层**（任何插件都能用）：

```ts
export interface DockPanelHost {
  projectPath: string
  openFile(path: string): void
  /** 打开任意设置分类（替代 openMailSettings / openMeetingRelaySettings） */
  openSettingsCategory(id: string): void
  /** 跳到任意页签（替代 requestProject） */
  openTab(id: string): void
  /** 当前是否宽屏（面板按宽度自适应时用） */
  isWideScreen: boolean
}
```

**专用层**（少数插件确实需要的，仍具名，但按插件归组）：

```ts
export interface DockPanelHostExtras {
  terminal: { requestedCwd: string | null; clearCwd(): void; platformUnsupported: boolean }
  task: { openTaskFile(path: string, lineStart?: number): void }
  meeting: { quote(token: string): void }
}
```

面板通过 `panel.props` 取自己那一组。这样加一个新插件**不需要往接口里加成员**。

### 6.9 预设从"手抄三份"改成"按 group 派生 + 显式例外"

现在 `DOCK_PRESET_PLANS` 把 14 个 id 手抄 3 遍。改成：

```ts
export const DOCK_PRESETS = {
  developer: { /* 全开、默认顺序 */ },
  standard: {
    // 派生规则：隐藏"开发相关"分组里的项
    hideGroups: [],
    hideTabs: ['forge', 'terminal', 'proxy'],   // 显式例外
    parkHiddenBefore: 'settings',               // 隐藏项挪到设置之前
    disableFeatures: ['forge.enabled', 'terminal.enabled'],
  },
  minimal: {
    keepOnly: 'permanent',      // 只留常驻
    disableAllFeatures: true,
  },
} as const
```

`order` 不再手写：从 `DOCK_TAB_ORDER` 派生，把隐藏项挪到 `parkHiddenBefore` 之前。新插件自动被三个预设正确对待，**不需要登记**。

守卫测试：`TestPresetsCoverEveryTab` —— 三个预设展开后的 id 集合必须都等于 `DOCK_TAB_ORDER`。

### 6.10 设置页由插件自己贡献

现在加一个设置页要改 `settingsFieldMap.ts` 的 `categoryItems` + `SettingsIndex.vue` 的 `groupDefs`（含图标 import）+ 两处 i18n。

改成插件声明：

```ts
// plugin.ts
settingsPage: {
  /** 分类 id，默认 = plugin.id */
  categoryId: 'mail',
  /** 挂在设置首页哪个分组下 */
  group: 'dockModules',
  icon: Mail,
  /** 三种形态之一：扁平列表 / 分组面板 / 自定义组件 */
  kind: 'custom',
  component: () => import('./MailSetting.vue'),
}
```

`SettingsIndex.vue` 的 `groupDefs` 改成"静态核心分类 + 从注册表派生"，`SettingsCategory.vue` 的 `v-else-if="categoryId === 'xxx'"` 长链改成一个查表分派。

**顺序**：管理器（`dockPlugins`）打头，其余按 `DOCK_TAB_ORDER` 排——与现在的人工顺序一致，但不用手维护。

---

## 7. 迁移阶段

每个阶段可独立合并、独立回滚，行为不变。

### Phase 0：止血 + 守卫（半天）

**不改架构，只补漏。**

- [ ] 修「关掉邮件还能发信」：`/api/mail/*` 加 `rejectIfMailDisabled`（先手写，Phase 2 再被 Gate 取代）
- [ ] 加守卫测试：三个预设覆盖全部页签
- [ ] 加守卫测试：`descriptionKey` 中英都存在且非空
- [ ] 加守卫测试：每个 `tab.feature` 都在 `FEATURE_FLAG_DEFAULTS` 里
- [ ] 加守卫测试：每个插件的 `unavailableReasonKey` 中英都存在
- [ ] 补 `e2e/specs/dock-plugins.spec.ts`（隐藏 → 刷新 → 仍隐藏 → 显示 → 回来）

**退出标准**：新测试全绿；邮件关闭后 `POST /api/mail/send` 返回 409。

### Phase 1：服务端注册表骨架（只读，不接管行为）

- [ ] 新增 `internal/plugins` 包（类型 + 注册表 + 排序）
- [ ] `main.go` 注册现有 6 个功能开关 + 3 个纯偏好插件
- [ ] 新增 `GET /api/features`（+ OpenAPI + tag）
- [ ] 守卫：每个 `Section` 的 yaml 字段都在 `PatchableConfigPaths` 里
- [ ] 守卫：每个 `DisabledMsgKey` 中英都存在

**退出标准**：`/api/features` 返回正确；**没有一行行为改变**（Gate 尚未接入）。

### Phase 2：路由门控接管（影子模式 → 真拦）

- [ ] `plugins.Gate` + `ForRoute` 最长前缀匹配
- [ ] `RegisterRoutes` 的 `register` 闭包接入 Gate，先 `shadow: true`（只记日志）
- [ ] 跑全量 e2e + 手工点一遍所有功能，检查日志里"本该被拦"的路径是否合理
- [ ] 关掉 shadow，删掉手写的 `rejectIfTasksDisabled` / `rejectIfShareDisabled` / `rejectIfMailDisabled` / Forge 的两处 if

**退出标准**：功能关闭时，该前缀下所有接口 409（终端 503），且**新增一个接口不需要任何人记得写检查**。

### Phase 3：worker 生命周期统一

- [ ] `main.go` 的 20 行 `StartXxx` + `defer Stop` 改成遍历注册表
- [ ] 6 个 `hotReloadXxx` 函数搬成插件的 `Reconfigure` / `Start` / `Stop`
- [ ] `applyFeatureDisableEffects` / `clearForgeBindingsOnDisable` 搬成 `OnDisable`

**退出标准**：开关切换后 worker 行为与现在完全一致（含"重新打开不补跑"）。

### Phase 4：前端 `plugin.ts` + glob（无行为变化）

- [ ] `DockPlugin` 类型定义（合并 `DockTabDescriptor` + `DockPluginRuntime` + icon）
- [ ] 14 个 `components/*/dock.ts` → `plugins/*/plugin.ts`（补上清单字段与图标）
- [ ] `dockPluginRuntime.ts` 改成 glob
- [ ] `dockTabs.ts` 瘦成 `DOCK_TAB_ORDER` + 派生类型
- [ ] 删除 `dockTabMeta.ts`
- [ ] `DockPanelHost` / `DockPluginSetting` 改从 runtime 取图标与清单

**退出标准**：界面零变化；`dockTabs.ts` 仍零 import；加一个测试插件只需两个文件。

### Phase 5：统一 Dock 按钮 + 去硬编码

- [ ] `DockTabButton.vue` 三处接入
- [ ] `appearance` 声明，删掉 App.vue 里 6 处 `tab === 'tasks'` 和 `liveDotVisible` 特判
- [ ] 删 8 个动画 ref + `badgeAnims`
- [ ] `handleDockTerminal` / `handleInlineOverflowClick` 的 `if (tab === 'terminal')` 改成插件的 `onClick` 钩子

**退出标准**：三种布局的角标、圆点、动画与现在一致；App.vue 里不再出现任何具体插件 id（`chat` 除外）。

### Phase 6：可用性收敛 + 深链 + onLeave

- [ ] `useDockAvailability` 合成三轴，读 `/api/features`
- [ ] 删除 `FEATURE_FLAG_DEFAULTS`（前端不再有默认值镜像）
- [ ] `resolvePanelTab` 去掉 `forgeHidden` / `mailHidden`
- [ ] `deeplinks` 声明 + `usePluginDeeplinks`，搬走 4 个 handler
- [ ] `useDockLifecycle.enter()` 幂等 + `onLeave` 接上
- [ ] `DockPanelHost` 拆通用层 / 专用层

**退出标准**：第三个门控插件不需要改 App.vue / `useProjectPanel.ts`。

### Phase 7：预设派生 + 设置页自贡献

- [ ] `DOCK_PRESETS` 从"手抄三份"改成规则 + 例外
- [ ] `settingsPage` 声明 + `SettingsIndex` / `SettingsCategory` 查表分派

**退出标准**：加插件不再需要登记预设；加设置页不再改 `SettingsIndex.vue`。

### Phase 8：文档与开发者指南

- [ ] `docs/plugin-dev/` 全部重写（现在描述的是 Phase 4 之前的形态）
- [ ] `docs/spec/` 加 `features/plugin-registry.md`
- [ ] 更新 `AGENTS.md` 的包表
- [ ] 更新 `docs/user-guide/configuration.md`（若白名单派生影响文档）

---

## 8. 测试与守卫

### 8.1 服务端

| 测试 | 断言什么 |
|---|---|
| `TestPluginIDsUnique` | 注册表无重复 ID（Register 已 panic，这里是双保险） |
| `TestEveryPluginSectionFullyWhitelisted` | 插件声明的 Section 下每个 yaml 字段都在 `PatchableConfigPaths` |
| `TestEveryPluginDisabledKeyLocalized` | `<ID>Disabled` 在中英 yaml 都存在 |
| `TestEveryPluginWithSectionDeclaresRoutes` | 有 `Section` 但 `APIPrefixes` 为空 → 失败（防止 B7 复发） |
| `TestGateRejectsWhenDisabled` | 表驱动：每个插件 × 每个前缀 → 关闭时返回预期状态码与 msgKey |
| `TestGateExemptionsAreReal` | `GateExempt` 里的路径必须真实注册过 |
| `TestFeaturesEndpointShape` | `/api/features` 的字段齐全、`enabled` 与 `ConfigInstance` 一致 |
| `TestWorkerLifecycle` | `Start` 幂等、`Stop` 幂等、开关翻转后 goroutine 不泄漏（用 `goleak` 或计数器） |
| `TestOnDisableRunsAfterPersist` | 写盘失败时 `OnDisable` 不被调用 |

### 8.2 前端

| 测试 | 断言什么 |
|---|---|
| `TestEveryPluginHasPanel` | 现有 `dockPanelCoverage.test.ts` 保留，改成读 glob |
| `TestEveryPluginInDockOrder` | glob 出来的 id 集合 == `DOCK_TAB_ORDER`（防"加了 plugin.ts 忘了加顺序"） |
| `TestDockOrderHasNoUnknownIds` | 反过来（防"删了插件忘了删顺序"） |
| `TestPresetsCoverEveryTab` | 三个预设展开后都等于全集 |
| `TestEveryTitleAndDescriptionLocalized` | 两个 locale 都有 `titleKey` / `descriptionKey` |
| `TestNoHardcodedPluginIdsInApp` | 源码守卫：App.vue 里除 `chat` 外不出现具体页签 id（白名单：注释与 CSS 类名） |
| `TestDockTabsZeroImport` | `dockTabs.ts` 仍无 runtime import |
| `TestFeatureFlagsReadFromServer` | `/api/features` 未落地时按开处理；落地后按服务端 |
| `TestPluginDeeplinksRegistered` | 每个 `deeplinks` 都被宿主注册且卸载时反注册 |
| `TestLifecycleEnterIdempotent` | 重复 `enter(same)` 不重复触发 `onEnter`；切走触发 `onLeave` |

### 8.3 E2E

`e2e/specs/dock-plugins.spec.ts`：

1. 设置 → 功能编排：隐藏邮件 → 左栏无入口 → 刷新后仍无 → 重新显示 → 入口与角标回来
2. 关闭任务 → 通知深链跳任务被拒绝 → 当前页签不变
3. 应用"极简"预设 → 确认框列出会被停掉的东西 → 只剩常驻
4. 拖动排序 → 刷新后顺序保持
5. 关闭邮件 → `POST /api/mail/send` 返回 409（接口层，不是 UI）

---

## 9. 风险与坑

1. **App.vue 是 5715 行的巨型单文件**。Phase 5 要动它的模板。**必须小步合并**：先加 `DockTabButton` 只在宽屏接入、跑绿、再换窄屏、再换溢出。不要一次重排模板。
2. **`dockTabs.ts` 零 import 是硬约束**。Phase 4 瘦身时，如果为了"少写一行"把它改成从 glob 派生，会让一批窄 mock 测试在模块加载期集体炸。方案 A 刻意保留手写顺序。
3. **Gate 接入是行为变更**。用 shadow 模式先观察一轮。特别注意：Forge 的 `GET /api/forge/binding` 在关闭时**要返回空而不是 409**（前端靠它判断"未绑定"），必须进 `GateExempt`。
4. **反射派生白名单风险高于收益**。建议保留手写 `PatchableConfigPaths`，只加守卫测试（§5.7 的结论）。
5. **`OnDisable` 的执行时机不能变**。必须在配置写盘成功之后。现在的注释已经解释过：先撤销再写盘失败 = 毁掉用户的绑定。
6. **三种关闭语义不能合并**。这是上一轮的红线。Gate 只管"功能"这一轴，能力与偏好在各自的位置判断。
7. **glob 的构建影响**。`import.meta.glob(..., { eager: true })` 会把所有 `plugin.ts` 打进主包——这与现在 `dockPluginRuntime.ts` 静态 import 全部 `dock.ts` 等价，**不增加体积**。但要注意 `plugin.ts` 里不能有重依赖（约束 3）。
8. **`DockTabId` 从数组派生后，新插件漏加顺序会类型报错**。这是好事，但要在开发文档里写清："类型报错是设计意图，不是要你加 `as any`"。
9. **通知/深链的跨项目逻辑**是最容易漏的：`handleOpenForge` 的注释已经写明"必须在写任何状态之前拒绝，否则会留下一个永远不消费的 pending target"。搬进插件时要原样保留这段顺序。
10. **`/api/features` 的缓存**。`useSettingsConfig.patchConfig` 每次写完都 `loadConfig()`；`/api/features` 也要在 PATCH 后刷新，否则开关拨完入口不变。
11. **终端的能力不可用不等于功能关闭**。它 `enabled: true, available: false`，前端要灰显而不是隐藏（现在的行为：面板保留、显示"不支持"空状态）。

---

## 10. 验收标准

全部满足才算完成：

1. **加一个前台页签**：新增 `plugins/<x>/plugin.ts` + `<X>Panel.vue` 两个文件，`dockTabs.ts` 加一行 id；**其余既有文件零修改**（含 App.vue / dockTabMeta / dockPluginRuntime / SettingsIndex / settingsFieldMap / 预设）。
2. **加一个服务端功能开关**：`plugins.Register(...)` 一处 + `MailConfig` 一个字段 + 一个 i18n 键；**路由拦截、worker 启停、热更新、`/api/features`、OpenAPI 检查全部自动**。
3. **新增一个接口**：只要它在前缀下，功能关闭时自动被拒，**不需要任何人记得写检查**。
4. **前端不再有功能开关的默认值镜像**（`FEATURE_FLAG_DEFAULTS` 已删除）。
5. **App.vue 里不出现任何具体插件 id**（`chat` 除外）。
6. **三种关闭语义保持分开**：能力不可用灰显 + 原因、功能关闭真停服务端、偏好只隐藏入口。
7. 所有守卫测试通过；`e2e/specs/dock-plugins.spec.ts` 三浏览器全绿。
8. 所有功能都开着时，界面与现在**像素级一致**。

---

## 11. 附：新增一个插件的新旧对比

| 步骤 | 现在 | 之后 |
|---|---|---|
| 清单 | `dockTabs.ts`（类型 + 数组两处） | `dockTabs.ts` 加一行 id |
| 图标 | `dockTabMeta.ts` | `plugin.ts` 里写 |
| 运行时 | `components/<x>/dock.ts` 新建 | `plugin.ts` 里写 |
| 聚合 | `dockPluginRuntime.ts` import + 表 | **不动** |
| 面板 | 新建 `.vue` | 新建 `.vue` |
| i18n | zh + en 两处 `nav.x` + `desc.x` | 同左（无法避免） |
| 预设 | `useDockPlugins.ts` 三处数组 | **不动**（自动派生） |
| 设置页 | `settingsFieldMap` + `SettingsIndex` + 可能 `SettingsCategory` | `plugin.ts` 里声明 |
| 服务端开关 | 12 处 | 2 处 |
| **合计** | **前台 7 文件 / 后台 12 处** | **前台 3 文件（1 行改动）/ 后台 2 处** |
