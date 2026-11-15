# Dock 插件开发指南

给"要给 ClawBench 加一个功能"的人看的手册。读完应该能照着做：新建哪些文件、`plugin.ts` 每个字段填什么、什么时候要动后端、跑哪些测试。

> 这里的"插件"是**内部功能模块**，不是第三方可安装的扩展：功能随应用发布，不支持远程加载代码，也没有沙箱。我们要的只是"加一个功能的边际成本接近零"。

---

## 0. 最短路径（四步）

假设要加的功能叫 `foo`（id 全小写，见下方"id 的规矩"）。

**第一步：新建 `web/src/plugins/foo/plugin.ts`**

```ts
import { Rocket } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

export default {
  id: 'foo',
  titleKey: 'nav.foo',
  descriptionKey: 'settings.dockPlugins.desc.foo',
  group: 'project',
  closable: true,
  // 下面两行只有"这个功能有自己的设置页"时才要，且分类必须先在
  // settingsFieldMap.ts 的 categoryItems 里存在（守卫会检查）。
  settingsCategory: 'foo',
  icon: Rocket,
  settingsPage: { group: 'dockModules', icon: Rocket, kind: 'flat' },
  panel: { load: () => import('@/components/foo/FooPanel.vue') },
} satisfies DockPlugin
```

**第二步：面板组件** 放 `web/src/components/foo/FooPanel.vue`。它只接收一个必有 prop `active`（是否当前可见），其余 props / 事件由 `plugin.ts` 的 `panel.props` / `panel.on` 决定。首屏是"空、只等数据"的，用 `<PanelSkeleton>`，不要居中转圈。

**第三步：在 `web/src/composables/dockTabs.ts` 的 `DOCK_TAB_ORDER` 加一行 id**

```ts
export const DOCK_TAB_ORDER = [
  'browse',
  ...
  'foo',        // ← 加这一行，位置就是默认位置
  'settings',
] as const
```

这是整个流程里**唯一要改的既有文件**（除了 i18n 文案）。

**第四步：补文案**（见 §7）。少一个键，页签标题会显示成原始 key。

不需要改：`dockPluginRuntime.ts`（`import.meta.glob` 自动收）、`App.vue`（一个 `DockPanelHost` 渲染所有面板）、`SettingsIndex.vue` / `SettingsCategory.vue`（设置页从注册表派生）、预设（从 `DOCK_TAB_ORDER` 派生）。

### 为什么顺序必须手写

`DOCK_TAB_ORDER` 是**产品决策**，不是文件系统的副产品：文件管理排第一（很多跳转的兜底落点），设置在最后（回到其他功能的唯一入口）。用 `glob` 的字母序决定新页签落在哪，等于让文件名排序替产品做决定。

另外 `dockTabs.ts` 必须**保持零 import**（无 Vue、无 lucide、无插件模块）。它被 `useWideScreenLayout` 依赖，而后者被 BottomSheet / AppHeader / FileViewer 等一大堆组件依赖——那些组件的测试只用窄 mock 替换 `lucide-vue-next`（只给几个图标）。这里一旦 import 图标，这些测试会在模块加载时就抛 `No "FolderOpen" export is defined on the "lucide-vue-next" mock`。守卫：`dockPluginManifest.test.ts` 的 "dockTabs.ts stays import-free"。

### id 的规矩

`id` 是稳定身份，同时是：前端页签 id、`/api/features` 的键、i18n 错误键前缀（`forge` → `ForgeDisabled`）、`plugin.ts` 所在目录名、深链目标。**全小写，定了就不要改**（改了等于换了一个功能）。守卫会同时检查两件事：目录名必须等于 `id`，`DOCK_TAB_ORDER` 与"有 `plugin.ts` 的目录"必须一一对应（双向）。

---

## 1. `plugin.ts` 的每个字段

类型定义在 `web/src/composables/dockPluginTypes.ts`（纯类型文件，所以插件模块 import 它不会与聚合层成环）。

### 1.1 清单字段（`DockTabPluginManifest`）

| 字段 | 必填 | 什么时候需要 / 不写会怎样 |
|---|---|---|
| `id` | 是 | 见上。`chat` 是特例：它不占左栏，所以只要求 `id` + `icon` + `titleKey`。 |
| `titleKey` | 是 | 页签标签 / 悬停名称的 i18n 键。缺了会显示原始 key（如 `nav.foo`）。 |
| `descriptionKey` | 是 | 功能编排里那一行的一句话说明。缺了同上。 |
| `group` | 是 | 只决定**设置管理器里的分组**（`workspace` / `project` / `personal` / `system`），**不影响 Dock 顺序**。 |
| `closable` | 是 | 用户能否隐藏这个页签。`false` 只给常驻三件套（`browse` / `view` / `settings`）；这个集合在 `dockTabs.ts` 的 `DOCK_PERMANENT_TAB_IDS` 里另有一份零 import 副本，两边不一致守卫会红。 |
| `feature` | 否 | 关联的服务端总开关（`forge.enabled` / `mail.enabled` / `terminal.enabled` / `kb.enabled` / `tasks.enabled` / `share.enabled`）。写了 ⇒ 管理器里的开关写服务端配置、关闭时页签消失；不写 ⇒ 开关只在本机隐藏入口。**必须**是 `DOCK_FEATURES` 里的值（写错一个字母会永远读成"开"，页签永不消失）。 |
| `capability` | 否 | 宿主/环境能力（目前只有 `terminal`、`portForward`）。用于管理器把开关置灰并给原因。**能力不是功能**：能跑但被关掉，与开着但跑不了，是两件事。 |
| `settingsCategory` | 否 | 这个插件自己的配置属于哪个设置分类。写了 ⇒ 管理器行上出现齿轮，跳到该分类。 |
| `settingsPage` | 否 | 往设置首页贡献一行。**有 `settingsCategory` 就必须有它**，否则齿轮指向的分类在首页没有入口。字段：`categoryId`（默认取 `settingsCategory`，再默认取 `id`）、`group`（**必须是 `dockModules`**）、`icon`、`kind`（`flat` / `group` / `custom`）、`component`（仅 `custom`，必须是 `() => import()`）。 |
| `icon` | 是 | 静态图标。随状态变的图标用运行时的 `dynamicIcon`（Forge 在 GitHub / GitLab 之间切）。 |

### 1.2 运行时字段（`DockPluginRuntime`，全部可选）

| 字段 | 什么时候需要 / 不写会怎样 |
|---|---|
| `panel` | 面板。`load: () => import('...')` 是正常写法（宿主会套上共享的异步组件重试/报错处理）；`component` 只给必须同步的核心面板。`props(host, extras)` / `on(host, extras)` 按需补 props 与事件。不写 ⇒ 这个 id 有页签但打开是空白。 |
| `available()` | 宿主/环境**此刻**能不能用。不写 = 恒可用。返回 false 时页签从 Dock 消失、管理器给原因。**不要**用它表达"服务端功能关了"——那是 `feature` 的事。 |
| `leaveWhenUnavailable()` | 不可用时是否把用户从该页签带走。默认 `true`。终端返回 `false`：平台不支持时留在页面上显示"这台机器跑不了终端"的空状态，而不是被导航走。 |
| `unavailableReasonKey` | 不可用的原因文案键。只在 `available()` 会返回 false 时有意义。 |
| `badge()` | 角标。`{ count }` 是数字，`{ count: 0, live: true }` 是"进行中"的小圆点（会议）。不写 = 无角标。 |
| `appearance` | 按钮外观的**状态谓词**：`unread` / `running` / `justCompleted` / `liveDot`。插件只说"什么时候成立"，长什么样在 `web/src/assets/dock.css`。宿主不再为任何具体插件写特例。 |
| `dynamicIcon()` / `title()` | 图标或标题依赖运行时状态时用（Forge 是唯一例子）。`title()` 优先于 `titleKey`。 |
| `onEnter()` / `onLeave()` | 页签被激活 / 离开时。`onEnter` 幂等（同一页签重复进入只跑一次）；`onLeave` 现在真的会被调用（离开时暂停轮询就靠它）。 |
| `startBackground()` | 页签**关着也要跑**的后台资源（角标轮询），返回清理函数。宿主只在"入口可见且可用"时启动，隐藏 / 关闭 / 不可用就停。目前只有邮件用它。 |
| `onClick(host, extras)` | 点击 Dock 入口时、切换页签**之前**做的额外动作。终端用它清掉"打开到某个目录"的残留请求。不写 = 只切页签。 |
| `deeplinks` | 这个插件接收哪些 `window` 事件（通知点击、引用跳转）。见 §1.3。 |
| `settingsPage` | 见上表。 |

### 1.3 深链：`{ event, handler(detail, ctx) }`

宿主（`usePluginDeeplinks.ts`）负责注册 / 反注册 / 事件名去重，并把五个导航原语作为 `ctx` 交给插件：

```ts
ctx.switchTab(id)                    // 切页签（带守卫）
ctx.hotSwitchProject(path)           // 换项目重建；被拒绝也会 resolve
ctx.tabBlocked(id)                   // 此刻这个页签不可达（隐藏/关闭/不可用）
ctx.recordNotificationOrigin(detail) // 记下"从通知点进来的"，供返回用
ctx.currentProject()                 // 当前项目根
```

为什么不是"宿主统一处理跨项目"的模板（比如 `{ followsProject, apply }`）：Forge 那个跳转**顺序是承重的**——先判断不可达并清掉待处理目标，再发布目标，再同项目跳转或跨项目切换，切换没落地还要把目标清掉。模板表达不了这个顺序，硬套会悄悄改行为。所以宿主只提供原语，插件保留自己的顺序。

写 handler 的三条经验：

- **先拒绝再写状态**。`ctx.tabBlocked(id)` 要在 `setPendingXxx()` 之前判断，否则功能重新打开后，面板会跳到一个陈旧的条目，返回也指向一次没发生的跳转。
- **同项目 / 跨项目分开处理**。跨项目时 `hotSwitchProject().then(...)` 之后再 `switchTab`；有些跳转还需要在切换后**重新派发一次**事件（会议就是这么做的，因为面板已经按旧项目处理过第一遍）。
- 事件名全局唯一。两个插件声明同一个 `event` 时，先声明的生效、后一个记一条 warning 并跳过。

---

## 2. 三种"关闭"不能合并

这是这个架构最重要的一条红线。三件事看起来都是"页签不见了"，但归属、来源、用户看到的东西完全不同：

| | 能力 `capability` / `available()` | 服务端功能 `feature` | 本机偏好 `hidden` |
|---|---|---|---|
| 谁说了算 | 宿主/环境（没有 PTY、没有隧道传输、索引器没起来） | 服务端 `config.yaml` + `GET /api/features` | 这台设备的 `localStorage` |
| 怎么关 | 用户关不掉 | 用户在功能编排里关，写服务端配置 | 用户在本机隐藏入口 |
| 关掉之后 | 服务端照常工作 | 服务端**真的停**（停 worker、接口 409/503、撤销已发出的东西） | 服务端完全不受影响 |
| 用户看到 | 开关灰显 + 原因（"这台机器跑不了"） | 页签消失；其他入口也被拦（点了会提示"功能已关闭"） | 只是入口没了 |
| 读的地方 | `available()` / `capability` | `isFeatureEnabled()`（`useFeatureFlags.ts`） | `useDockPlugins().hidden` |

三者在前端汇总成 `useDockAvailability()` 的三个集合（`featureDisabled` / `unavailable` / `hidden`），导航守卫 `isDockTabBlocked()` 三者取或。**不要**为了让某个地方简单就把它们并成一个布尔：合并之后"这台机器跑不了"和"你把它关了"就没法给出不同的补救办法（一个是去开开关，一个是换台机器）。

`available` 与 `enabled` 在服务端也是两条轴：终端可以 `enabled=true` + `available=false`（Windows 没有 PTY）。`GET /api/features` 两个字段都给，`reason` 里 `disabled` 优先于 `capability`——一个被关掉的功能哪怕宿主也跑不了，对用户来说仍然是"被关掉了"。

---

## 3. 铁律：插件能做什么、不能做什么

1. **不能 import 自己的面板组件**，只能 `load: () => import('@/components/foo/FooPanel.vue')`。
   原因：`plugin.ts` 是**急切**收集的（`import.meta.glob(..., { eager: true })`），静态 import 面板等于把面板和它的全部依赖塞进主包，懒加载彻底失效。
2. **不能 import 别的插件。** 插件之间不许有依赖；需要共享的东西抽成 `composables/` 或 `utils/` 里的叶子模块。
3. **不能在模块顶层调 `useSettingsConfig()`。** 要读就在函数体里读。顶层调用会在测试的窄 mock 下撞 TDZ，并让整棵依赖图在启动时就求值。
4. **不放重依赖。** WebRTC、DOMPurify、编辑器这类东西不进 `plugin.ts`。重状态先抽成轻量模块，`plugin.ts` 里只留一个函数体内才求值的引用（会议就是这么做的：`useLiveShare()` 在第一次读角标时才调用）。
5. **`dockTabs.ts` 保持零 import。** 见 §0。
6. **不要在 `App.vue` 里给具体插件加分支。** 需要新外观就加一个 `appearance` 谓词，需要新导航能力就加 `DockPanelHost` 的通用原语，需要单插件能力就加 `DockPanelHostExtras` 的一个键——**永远不要**把 `openFooSettings` 这类单插件成员塞进通用接口。

---

## 4. 后端：这个功能有服务端总开关时

只有"关掉之后服务端要停活、要拒绝接口、要撤销东西"的功能才需要总开关。纯偏好型（统计、通知中心）**不要**加：一个永远为真的开关只会让 `/api/features` 多一行噪音。

### 4.1 一个 `Register(...)`

在 `cmd/server/main.go` 的 `registerPlugins()` 里加一个条目（这是**唯一注册点**：`internal/plugins` 只放类型与注册表，不认识任何实现，函数值都在这里注入）：

```go
plugins.Register(&plugins.Plugin{
    ID:          "foo",
    Section:     "foo",                       // config.yaml 的段名
    Enabled:     func() bool { return model.ConfigInstance.Foo.Enabled },
    DefaultOn:   true,
    APIPrefixes: []string{"/api/foo/"},       // 这些路由自动套门控
    GateExempt:  []string{"GET /api/foo/status"}, // 关着也必须能访问的，见 §4.5
    Start:       func(model.Config) { service.StartFooWorker() },
    Stop:        service.StopFooWorker,
    Reconfigure: func(cfg model.Config) { service.ReconfigureFoo(cfg.Foo) },
    OnDisable:   handler.FooDisableEffect,    // 关闭即撤销时才有
})
```

`Enabled` 必须读**实时值**（包级全局或 `model.ConfigInstance`），不能读启动时的快照——热更新会在进程生命周期内改它。

### 4.2 一个 config 字段（连带六处机械改动）

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `internal/model/config.go` | `FooConfig` 加 `Enabled bool`，并在 `Config` 上挂 `Foo FooConfig \`yaml:"foo"\`` |
| 2 | `internal/model/defaults.go` | 缺键当开：`if !presence["foo.enabled"] { cfg.Foo.Enabled = true }` |
| 3 | `internal/handler/settings.go` | `configResponse` 加 `Foo configFoo \`json:"foo"\`` + 定义 `configFoo` + `serveConfigGet` 里赋值（设置页要能显示它） |
| 4 | 同上 | `PatchableConfigPaths["foo.enabled"] = true`（PATCH 白名单） |
| 5 | 同上 | `applyConfigPatch` 里加分支，把补丁写进 `model.ConfigInstance.Foo.Enabled`（插件读的是它） |
| 6 | `docs/user-guide/configuration.md` | 加一行说明（`TestConfigReferenceMatchesConfigFile` 会卡：每个 yaml 字段都必须在手册里有条目） |

**`hotReloadFields` 不用动**：`<section>.enabled` 由 `plugins.IsFeatureSwitchPath(path)` 自动判为热更新（它只认**已注册插件的 `Section`**，`nosuchsection.enabled` 与 `mail.sync_interval_minutes` 这类子字段都返回 false）。理由：开关在 PATCH 时已经被 `syncPluginLifecycle` 起停过了，再报"需要重启"是错的。只有**非 `.enabled` 的子字段**（例如 `foo.interval`）才需要手写进 `hotReloadFields`。

### 4.3 `PatchableConfigPaths` 为什么必须手写

它决定"哪些键允许被 PATCH 写进 `config.yaml`"，是安全边界。反射派生白名单等于把边界交给反射：加一个结构体字段就自动开放写入。所以白名单手写，**用守卫测试代替反射**：

- `TestPluginRegistry_EverySectionIsFullyWhitelisted`（`cmd/server/plugin_registry_test.go`）：`Section` 下每个 yaml 字段要么可 PATCH、要么是可 PATCH 字段的容器、要么在 `nonPatchableByDesign` 里登记**并写明原因**。漏登记 = 红。
- `TestPatchablePathsExistInConfig`（`internal/handler/settings_config_paths_test.go`）：白名单里的键必须在 `model.Config` 里真实存在（防改名后写进一个没人读的孤儿键）。
- `TestHotReloadFieldsArePatchable`：标了热更新却不可 PATCH 的路径是死重量。

### 4.4 哪些是自动的

| 能力 | 怎么来的 |
|---|---|
| 路由门控 | `handler.RegisterRoutes` 的 `register` 闭包对每个 pattern 调 `plugins.ForRoute`，命中就套 `plugins.Gate`。**前缀下新增接口自动被拦**，不需要任何人记得写检查（邮件当年就是漏在这里：关掉只停了后台收信，`/api/mail/send` 照发）。 |
| worker 启停 | `startPlugins` 只启动 `IsEnabled()` 的插件；`stopPlugins` 反向停；`syncPluginLifecycle` 在热更新后判断"开关翻转 ⇒ Start/Stop，开关没变 ⇒ Reconfigure"。加 worker 不用再改 `main()`。 |
| 热更新分类 | `<section>.enabled` 由 `plugins.IsFeatureSwitchPath` 判为热更新（不报 `needs_restart`）；子字段仍看 `hotReloadFields` |
| 热更新动作 | `syncPluginLifecycle` 在 `applyHotReloadGlobals` 里把开关翻转映射成 Start/Stop，否则 Reconfigure |
| `GET /api/features` | 从注册表直接生成，不用登记 |
| 关闭副作用 | `applyPluginDisableEffects` 遍历注册表，对"这次 PATCH 里 `section.enabled=false`"的插件调 `OnDisable` |
| 守卫测试 | 见 §4.6 |

### 4.5 `GateExempt` 的语义（开洞要克制）

门控是**整段拒绝**：前缀下所有已鉴权路由，功能关着就按 `DisabledStatus`（默认 409，终端是 503）返回 `msgKey = DisabledKey()`。豁免必须显式声明：

- `"/api/foo/"` —— **子树**，只豁免它下面的路径（不会顺手放过 `/api/foo` 本身）。
- `"/api/foo/binding"` —— **精确**，不做前缀匹配（不会放过 `/api/foo/binding-extra`）。
- `"GET /api/foo/binding"` —— 只豁免该方法。

三条现实用法：

- **Forge**：`GET /api/forge/binding` 必须放行，前端靠它判断"未绑定"，关闭时应返回空对象而不是 409；写仍然被拒。
- **任务**：路由层只拦"新建"（`POST /api/tasks`，method + path 就能认出来）。**`/api/tasks/` 整棵子树豁免**——因为动作和"整体更新"共用同一个 `PUT /api/tasks/{id}`，靠 body 里的 `action` 区分（`cancel` / `resume` / `trigger` / `pause` / `read` / `stop` / `deleteExecution`），而路由层读不到 body。所以由 handler 决定：`resume` / `trigger` / 整体更新调 `rejectIfTasksDisabled` 拒绝，`cancel` / `pause` / `read` / `stop` / 删除放行——关闭开关会取消所有执行，用户必须能看见并收拾残局。豁免清单因此是 `[]string{"GET /api/tasks", "/api/tasks/"}`。
- **终端**：`/api/terminal/status` 是只读能力探测，必须放行。否则前端拿不到 `platform_supported`，会把它和"被关掉"混为一谈，重新打开后开关卡在"已开但置灰"、页签也不出现，只能刷新。

豁免写错也是静默 bug，所以有守卫：`TestGate_ExemptionsNameRealRoutes`（必须命中真实路由，且必须属于自己这个插件）。

### 4.6 改完后端要跑什么

```bash
go test ./internal/plugins/            # 注册表 / ForRoute / 门控单元
go test ./cmd/server/                  # 注册表守卫 + 启动顺序守卫 + 门控表驱动
go test ./internal/handler/            # 白名单漂移、/api/features
go test ./internal/model/              # config 手册漂移
```

`cmd/server` 里那几组守卫分别是：

| 测试 | 钉住什么 |
|---|---|
| `TestPluginRegistry_IDsAreUniqueAndLowercase` | ID 唯一、全小写 |
| `TestPluginRegistry_EverySectionIsFullyWhitelisted` | Section 下每个字段可 PATCH 或有登记的豁免 |
| `TestPluginRegistry_EveryDisabledKeyIsLocalized` | 有 `Enabled` 的插件，`DisabledKey()` 中英都存在（缺键会把原始 id 给用户） |
| `TestPluginRegistry_SectionsDeclareRoutes` | 有 Section 就必须声明 `APIPrefixes`（否则接口永远不被拦） |
| `TestPluginRegistry_DeclaredPrefixesOwnRealRoutes` | **按前缀**逐个核对：每个声明的前缀都真的覆盖至少一条路由（按插件计数会让 `/api/git/clocx` 这种手滑被同插件另一个前缀掩盖） |
| `TestPluginRegistry_FeatureSwitchesAreHot` | 每个已注册 `Section` 的 `<section>.enabled` 都判为热更新；未注册段与子字段判为 false |
| `TestPluginRegistry_DestructiveDisablesDeclareHooks` | forge / share 必须有 `OnDisable` |
| `TestGate_EveryDisabledPluginRejectsItsRoutes` | 表驱动：**每个**可开关插件关闭后，它名下的每条已鉴权路由都返回该插件的状态码与 msgKey |
| `TestGate_TasksKeepsReadAndCleanupRoutes` | 任务关闭后读 / 取消 / 删除仍可达，新建 / 编辑被拒 |
| `TestGate_TerminalStatusIsExempt` | 状态端点放行且 `platform_supported` 描述平台而非开关 |
| `TestPluginLifecycle_*` | 翻转才 Start/Stop、否则 Reconfigure；启动时不启动已关闭的；反向停止 |
| `TestTerminalPluginLifecycleIsIdempotent` | Start / Stop 可以反复调用而不叠加 Manager、不 panic |
| `TestOnDisableRunsOnlyAfterPersist` | 写盘失败时**不**执行撤销钩子（先撤销再写盘失败会毁用户数据，而开关还开着） |
| `TestMainWiring_PluginsRegisteredBeforeStartAndRoutes` | `registerPlugins(port)` 必须在 `startPlugins(cfg)` 与 `handler.RegisterRoutes(mux)` **之前** |

最后那条是文本守卫（读 `main.go` 源码比位置），因为顺序错了不会报错：注册表还是空的，于是**没有任何 worker 启动、没有任何路由被门控**，启动日志干干净净。

### 4.7 已知取舍

- **门控是整段拒绝**，不是"逐接口声明"。好处是新增接口自动被拦；代价是"关闭后仍要能读"的功能必须显式豁免。更麻烦的一种情况是**同一个 method+path 既是"用功能"又是"收拾残局"**（任务的 `PUT /api/tasks/{id}`：整体更新要拒，取消执行要放行），路由层读不到 body，只能整棵子树豁免、交给 handler 按 `action` 判断。
- **终端用 503 而不是 409**：它的语义是"运行时没起来"，与"被关掉了"不同。
- **状态 / 探针类端点一律豁免**：它们回答的是"这台服务器能做什么"，关掉功能之后这个问题依然需要答案。
- **公开路由不在门控内**（`registerPublic` 不经过 Gate）：`/api/share/{token}` 关闭时由它自己返回 404，而不是 409。旧链接应该"打不开"，不该告诉匿名访客"这台服务器关了分享"。

---

## 5. 前端要改的其它地方

| 场景 | 改什么 |
|---|---|
| 新服务端开关 | `dockTabs.ts` 的 `DockFeature` 联合类型 + `DOCK_FEATURES` 数组各加一项（`dockPluginManifest.test.ts` 断言这个数组等于各 manifest 实际声明的集合）。`isFeatureEnabled('foo.enabled')` 自动可用；`useFeatureFlags()` 那个便捷对象是手写的，想在那里取简写名再加一行。 |
| 别的入口也要拦 | 按钮 / 菜单项 / 快捷键的处理器里用 `useFeatureFlag('foo.enabled')` 判断，被点时调 `notifyFeatureDisabled()` 给统一提示。**只隐藏 Dock 页签不够**——入口还在，用户点了只会收到服务端 409。 |
| 新设置分类 | `settingsFieldMap.ts` 的 `categoryItems` 加分类（`flat` / `group` 页面要有条目；纯自定义页给空数组），再在 `plugin.ts` 里声明 `settingsPage`。**不要**去改 `SettingsIndex.vue` / `SettingsCategory.vue`。 |
| 新面板事件 | 在 `plugin.ts` 的 `panel.on` 里给；事件名用组件 `defineEmits` 的**原始拼写**（模板里的 `@relay-settings` 会被编译器归一化，对象键不会）。 |

---

## 6. 后端拒绝时前端看到什么

功能关着时接口返回（生产路径由 `writeLocalizedErrorf` 写）：

```json
{ "error": "邮件功能已关闭", "code": 409, "msgKey": "MailDisabled" }
```

- `msgKey` 由 `Plugin.DisabledKey()` 推导：`<ID 首字母大写>Disabled`（`kb` → `KbDisabled`）。要不一样的键就写 `DisabledMsgKey`。
- `error` 是**服务端已本地化**的文案（按 `X-Locale` / Cookie / `Accept-Language` 选语言），界面直接显示它；`msgKey` 是原始键，客户端需要自己翻译时用。
- 状态码由 `DisabledStatus` 决定，`0` 归一为 409。
- 所以 `XxxDisabled` 必须加进**服务端** YAML 语言包（中英各一份，`TestPluginRegistry_EveryDisabledKeyIsLocalized` 会检查）；纯前端的提示文案才放在前端 TS 语言包里。

---

## 7. i18n 键清单

两套语言包，别混：

| 键 | 文件 | 说明 |
|---|---|---|
| `titleKey` | `web/src/i18n/locales/zh.ts` + `en.ts` | 页签标签 / 悬停名 |
| `descriptionKey` | 同上 | 功能编排里的一句话说明 |
| `unavailableReasonKey` | 同上 | 不可用原因（只在 `available()` 会返回 false 时需要） |
| `DisabledMsgKey` / 推导出的 `XxxDisabled` | `internal/i18n/locales/active.zh.yaml` + `active.en.yaml` | 接口被拒时的服务端文案 |
| 设置页字段文案 | `web/src/i18n/locales/{zh,en}.ts` + `settingsFieldMap.ts` 里的 `labelKey` / `descriptionKey` | 新分类的每个字段都要 |

`nav.*` 是页签名、`settings.dockPlugins.desc.*` 是编排说明、`settings.dockPlugins.unavailable.*` 是不可用原因——跟现有插件保持一致即可。

---

## 8. 验证清单

### 单测 / 守卫（必跑）

```bash
npx vitest run web/src/composables/__tests__/dockPluginManifest.test.ts
npx vitest run web/src/composables/__tests__/useDockAvailability.test.ts \
              web/src/composables/__tests__/usePluginDeeplinks.test.ts \
              web/src/composables/__tests__/dockPluginRuntime.test.ts \
              web/src/composables/__tests__/useDockLifecycle.test.ts
npx vitest run web/src/__tests__/dockButtonWiring.test.ts
```

`dockPluginManifest.test.ts` 覆盖的都是"漏了就静默失效"的步骤：

- 目录名 = `id`；`DOCK_TAB_ORDER` 与插件目录双向一一对应；
- 组装出来的顺序等于 `DOCK_TAB_ORDER`，每个页签都有图标；
- `closable: false` 的集合等于 `DOCK_PERMANENT_TAB_IDS`；
- `feature` 必须是 `DOCK_FEATURES` 里的值，且 `DOCK_FEATURES` 等于实际声明的集合；
- 每个 `titleKey` / `descriptionKey` / `unavailableReasonKey` 在**中英两边**都能解析且非空；
- 有 `settingsCategory` 就必须有 `settingsPage`，`group === 'dockModules'`，且 `categoryId` 是真实的设置分类；
- `kind: 'custom'` 必须给 `component`；
- 三个预设展开后的 order 都是全部 id 的排列；
- `dockTabs.ts` 零 import、无 `import.meta`。

### e2e（改动了 Dock 行为时）

```bash
go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock
npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/dock-plugins.spec.ts
```

`dock-plugins.spec.ts` 三组用例：管理器隐藏 → 刷新仍隐藏 → 显示后角标一起回来；服务端关掉某功能后页签离开、打开后回来；插件声明的深链能跳转、被拦时不跳且不留残留状态。

### 手验

1. 页签出现在预期位置，图标 / 名称 / 提示都正常（不是原始 key）。
2. 功能编排里那一行：开关、说明、齿轮（有配置的话）都对；点齿轮落到正确的设置分类。
3. 隐藏 → 刷新 → 仍然隐藏 → 显示 → 角标与状态点都回来。
4. 有服务端开关的：关掉后页签消失，并且**手动打该前缀下的接口**返回 409/503 + `XxxDisabled`；重新打开后页签回来、后台资源重新跑（看日志）。
5. 有深链的：从通知 / 引用点进去能落到正确条目；功能关着时点了什么都不发生，也不会在重新打开后跳到旧条目。

---

## 9. 文件地图

| 文件 | 作用 |
|---|---|
| `web/src/plugins/<id>/plugin.ts` | **这个插件的全部自描述**：清单 + 运行时资源 |
| `web/src/composables/dockPluginTypes.ts` | 契约类型（纯类型，无运行时代码） |
| `web/src/composables/dockPluginRuntime.ts` | 唯一聚合点：`import.meta.glob` 收全部 `plugin.ts`，按 `DOCK_TAB_ORDER` 组装；解析角标 / 标题 / 图标 / 可用性；`dockTabs()` 是唯一的渲染列表 |
| `web/src/composables/dockTabs.ts` | id 与顺序（`DOCK_TAB_ORDER`）、分组 / 能力 / 功能联合类型、常驻集合、`mergeDockOrder`。**零 import** |
| `web/src/composables/useDockPlugins.ts` | 隐藏集合、顺序、三个预设、`usePluginProfile`（给插件读预设） |
| `web/src/composables/useDockAvailability.ts` | 三条轴合成一个对象（`featureDisabled` / `unavailable` / `hidden`）+ 导航守卫 |
| `web/src/composables/useDockLifecycle.ts` | 后台资源启停、`onEnter` / `onLeave` |
| `web/src/composables/usePluginDeeplinks.ts` | 装 / 卸插件声明的窗口事件，事件名去重 |
| `web/src/composables/useFeatureFlags.ts` | 服务端功能开关的唯一读取入口（`/api/features`） |
| `web/src/components/dock/DockTabButton.vue` | 唯一一种 Dock 按钮（宽屏竖排 / 窄屏内联 / 溢出菜单行） |
| `web/src/assets/dock.css` | 按钮 / 角标 / 溢出行的样式（**全局**：scoped 规则到不了子组件内部） |
| `web/src/components/dock/DockPanelHost.vue` | 通用面板宿主：按 id 取 `panel`，绑 props / 事件 |
| `web/src/utils/panelState.ts` | 持久化：`dockHidden` / `dockOrder` / `dockPreset` |
| `web/src/components/settings/DockPluginSetting.vue` | 功能编排 UI |
| `internal/plugins/` | 服务端注册表（`plugin.go`）+ 门控（`gate.go`），只依赖 `internal/model` |
| `cmd/server/main.go` | 唯一注册点 `registerPlugins` + 生命周期 `startPlugins` / `stopPlugins` / `syncPluginLifecycle` |
| `internal/handler/features.go` | `GET /api/features` |

服务端侧的设计细节（`ForRoute` 匹配规则、门控语义、生命周期时序、不变量与守卫对照表）见 [plugin-registry 规格](../spec/features/plugin-registry.md)。

---

## 10. 旧文档

`01-model.md` … `09-settings.md` 是 2026-11 上一轮重构时写的，描述的是"每个功能自己一份 `components/<feature>/dock.ts`"的形态——那些文件已经删了，字段名和接线位置都不再适用。其中**仍然成立**的只有概念部分：三种"关闭"必须分开、常驻页签不能隐藏、预设是粗粒度选择而管理器是细粒度。具体照本文件做。
