# 服务端功能注册表（`internal/plugins`）

一句话：**这个服务器上有哪些功能、它们现在开没开、哪些 HTTP 路由属于它们、它们各自的后台资源怎么起停。**

代码只有两个文件：

| 文件 | 职责 |
|---|---|
| `internal/plugins/plugin.go` | `Plugin` 类型 + 注册表（`Register` / `Lookup` / `All` / `ForRoute` / `Reset`） |
| `internal/plugins/gate.go` | `Gate` 中间件 + `RejectWriter` 注入点 |

它**只依赖 `internal/model`**（与 `internal/ai/backends` 的位置和约束一致）。`Plugin` 里刻意只放"声明"：`Start` / `Stop` / `Reconfigure` / `OnDisable` 是函数值，由 `cmd/server/main.go` 在注册时注入。这样本包不需要 import `service` / `handler` / `kb` / `mail`，也就不会与它们成环（`service` 与 `handler` 都可能在别的方向上需要读到功能状态）。

面向"要加功能的人"的操作步骤在 [插件开发指南](../../plugin-dev/README.md)，本篇讲**为什么这样设计、以及哪些性质被测试钉住了**。

---

## 1. 架构

```
                     ┌──────────────────────────────┐
                     │  cmd/server/main.go          │
                     │  pluginall.RegisterAll(port) │  ← 只调这一行
                     │  startPlugins / stopPlugins  │
                     │  syncPluginLifecycle(cfg)    │
                     └───────────┬──────────────────┘
                                 │ 遍历 internal/plugin/all
                                 ▼
                     ┌──────────────────────────────┐
                     │  internal/plugin/<id>        │
                     │  Register(&plugins.Plugin{…})│
                     │  routes.go / feature_switch  │
                     └───────────┬──────────────────┘
                                 │ Register
                                 ▼
                     ┌──────────────────────────────┐
                     │  internal/plugins            │
                     │  byID / ordered（按 ID 升序） │
                     └───┬──────────────┬───────────┘
             ForRoute    │              │   All
                         ▼              ▼
        ┌────────────────────────┐  ┌────────────────────────┐
        │ handler.RegisterRoutes │  │ GET /api/features      │
        │  register(pattern,h)   │  │ （features.go）         │
        │   → Gate(p, h, reject) │  │  → {id,enabled,        │
        │  mount.MountRoutes(p)  │  │     available,reason,  │
        └────────────────────────┘  │     defaultOn}         │
                                    └────────────────────────┘
```

三条派生关系，都是"注册一次、处处自动"：

1. **路由门控**：`RegisterRoutes` 里的 `register` 闭包对每个 pattern 调 `plugins.ForRoute(pattern)`，命中就 `plugins.Gate(...)` 包一层。新增接口只要落在前缀下就自动被拦，不需要任何人记得写检查。
2. **后台资源**：`startPlugins` 遍历 `plugins.All()`，只启动 `IsEnabled()` 的；`stopPlugins` 反向停。
3. **状态下发**：`ServeFeatures` 遍历 `plugins.All()` 生成 `GET /api/features`。

**接口由插件自己挂**：`RegisterRoutes` 把 `Mount`（三个闭包：Auth / Public / Raw）交给注册表里每个插件的 `MountRoutes`，挂载函数住在 `internal/plugin/<id>/routes.go`。删掉那个目录，接口、开关、后台任务一起消失。

`internal/plugins` 自己不知道任何具体功能的名字——注册表一旦建立就不再变（`Reset()` 明确标注**仅供测试**：生产里改注册表会让已套上 Gate 的路由与 `/api/features` 互相矛盾）。

---

## 2. `Plugin` 字段

| 字段 | 语义 | 关键约束 |
|---|---|---|
| `ID` | 稳定标识，同时是前端页签 id、`/api/features` 的键、i18n 错误键前缀 | 全小写；注册后不允许改 |
| `Section` | `config.yaml` 里的段名（`mail` / `forge` / `kb` / …） | 空 = 这个功能没有服务端总开关（纯偏好型）。非空派生两件事：该段下每个 yaml 字段必须在 `plugin/config` 的 `Fields` 里（或登记为不可改，守卫检查），以及"这次 PATCH 关掉它了吗"的判定依据 |
| `Enabled` | 读**实时**开关 | `nil` = 恒开。必须读包级全局 / `model.ConfigInstance`，不能读启动快照——热更新会在进程生命周期内改它 |
| `DefaultOn` | 只用于文档与"缺键当开"的语义声明 | 真正的默认值在 `model.ApplyDefaults` 的 presence 检查里，那里是唯一实现 |
| `APIPrefixes` | "这些路由属于我" | 有 `Section` 就必须非空（否则接口永不被门控）。精确路径或子树前缀都行，匹配见 §3 |
| `MountRoutes` | 挂载这个功能的 HTTP 接口 | 函数住在 `internal/plugin/<id>/routes.go`。声明了前缀就必须有它（守卫），否则接口全 404 |
| `Start(cfg)` | 启动后台资源 | 必须**幂等**（重复调用不叠加 goroutine / Manager） |
| `Stop()` | 停止后台资源 | 必须幂等；关机会对**所有**插件调一次（含从未启动的，让半途失败的 Start 也能收尾） |
| `Reconfigure(cfg)` | 开关没变、只是参数变了 | 用于"改了间隔 / 批次大小"这类不需要重启的调整 |
| `OnDisable() []string` | 关闭**且配置已成功落盘之后**的不可逆动作（清空仓库绑定、撤销分享链接） | 返回 warnings：部分失败报告给客户端但**不回滚配置**。时机不可改，见 §4 |
| `SwitchEffect(enabled) []string` | **双向**开关效果（任务：关掉取消执行、打开重排 cron） | 与 `OnDisable` 互斥（同时声明会让清理跑两遍，守卫会红） |
| `DisabledMsgKey` | 拒绝时的 i18n 键 | 空则按 `DisabledKey()` 推导 |
| `DisabledStatus` | 拒绝时的 HTTP 状态码 | `0` 归一为 409；终端用 503（"运行时没起来"而非"被关掉了"） |
| `Available()` | **运行时**可用性（能力轴） | `nil` = 恒可用。与 `Enabled` 正交：终端可以 `enabled=true` + `available=false` |
| `GateExempt` | 关着也必须能访问的路径 | 必须显式声明，不能靠漏配前缀。写法见 §3.2 |
| `ConfigFields` | 这个功能允许被 PATCH 的配置字段 | 直接写 `config.Fields("<id>")`；白名单只有这一份，handler 读同一处。重复声明同一路径会让 `Register` panic |

派生方法：`DisabledKey()`（`<ID 首字母大写>Disabled`，如 `forge` → `ForgeDisabled`、`kb` → `KbDisabled`）、`DisabledHTTPStatus()`、`IsEnabled()`、`IsAvailable()`、`IsGateExempt(method, path)`。

包级导出（给 `handler` 与守卫测试用，不是 `Plugin` 的方法）：

| 函数 | 语义 |
|---|---|
| `PrefixMatchesPath(prefix, path)` | `path` 是否落在 `prefix` 上（精确或段边界，两侧尾 `/` 抹平）。这是「前缀覆盖某条路由」的**唯一实现**：`ForRoute` 用它做归属判断，守卫用它**逐个前缀**核对「每个声明都真的命中路由」 |
| `IsFeatureSwitchPath(path)` | `path` 是否正好是某个**已注册插件**的 `<section>.enabled`。`nosuchsection.enabled` 与 `mail.sync_interval_minutes` 都返回 false。用途见 §4.2：功能开关不再需要登记 `hotReloadFields` |

### 注册期的 panic

以下都是编程错误，`Register` 直接 panic（静默覆盖会让两个功能抢同一批路由，那是最难排查的一类 bug）：

- `nil` 插件、空 `ID`、重复 `ID`；
- `APIPrefixes` 里有不以 `/` 开头的项；
- `GateExempt` 里有空条目；
- 两个插件声明同一个 `ConfigFields` 路径（谁也说不清这个字段归谁，关掉其中一个该不该拦住它）。

`ordered` 在每次 `Register` 后按 ID 排序：`/api/features` 的输出顺序与 worker 启停顺序都必须稳定，不能依赖注册调用的先后。

---

## 3. 路由归属与门控

### 3.1 `ForRoute`：最长前缀 + 段边界

两条规则都要，缺一条就会出错：

- **最长前缀优先**：声明了 `/api/git/stats` 的插件压过声明 `/api/git/` 的插件（统计页签就是靠这个把 `/api/git/stats`、`/api/git/cloc` 从 Git 那一大片前缀里单独拎出来）。
- **按路径段边界匹配**：前缀 `/api/mail` 不能匹配 `/api/mailbox`。

实现上是先抹平两端尾部 `/`，再判断"路径相等，或路径以 `前缀 + "/"` 开头"。抹平是必需的：路由表里 `/api/tasks` 与 `/api/tasks/` 是两个 pattern，而插件只声明一次，两种写法都必须命中。这段判断抽成了导出的 `PrefixMatchesPath(prefix, path)`——`ForRoute` 与守卫测试共用同一份实现，守卫因此能**逐个前缀**核对覆盖情况（按插件计数会让一个写错的前缀被同插件另一个前缀掩盖）。

`ForRoute` 对 `/` 与空前缀返回 nil（不能让某个插件接管全站）。

### 3.2 `Gate` 与 `GateExempt`

`Gate(p, next, reject)` 的判定顺序：

1. `p == nil` 或 `p.IsEnabled()` → 放行；
2. `p.IsGateExempt(method, path)` → 放行；
3. 否则按 `DisabledHTTPStatus()` 写出本地化错误，`msgKey = DisabledKey()`。

`reject` 由 handler 注入（它拥有 i18n 与统一错误体）。`reject == nil` 时写一个最小 JSON 体（`{"error","code","msgKey"}`），便于单测直接调 `Gate`。

`GateExempt` 比 `ForRoute` **严格**，因为豁免是在开一个洞：

| 写法 | 语义 | 不会发生什么 |
|---|---|---|
| `"/api/foo/"` | 子树，只豁免它**下面**的路径 | 不会顺手放过 `/api/foo` 本身 |
| `"/api/foo/binding"` | 精确路径，不做前缀匹配 | 不会放过 `/api/foo/binding-extra` |
| `"GET /api/foo/binding"` | 只豁免该方法 | POST / DELETE 仍被拒 |
| `"/api/foo/read/"` | 子树，所有方法 | 不会覆盖 `/api/foo/read` |

现有的三处豁免与理由：

| 插件 | 豁免 | 为什么 |
|---|---|---|
| `forge` | `GET /api/forge/binding` | 前端靠它判断"未绑定"，关闭时要返回空对象而不是 409；写仍然被拒 |
| `tasks` | `GET /api/tasks`、`/api/tasks/` | 只有「新建」（`POST /api/tasks`）能被 method + path 识别并拦住；**整棵 `/api/tasks/` 子树豁免**，因为动作与「整体更新」共用同一个 `PUT /api/tasks/{id}`，靠 body 里的 `action` 区分，而路由层读不到 body。由 handler 决定：`resume` / `trigger` / 整体更新 `rejectIfTasksDisabled`，`cancel` / `pause` / `read` / `stop` / 删除放行（关闭开关会取消所有执行，用户必须能收拾残局） |
| `terminal` | `/api/terminal/status` | 只读能力探测。门控它会让前端把"关了"和"跑不了"混为一谈：`platform_supported` 拿不到 ⇒ 前端把平台能力一起置 false ⇒ 重新打开后开关卡在"已开但置灰"、页签不出现，只能刷新 |

**公开路由不在门控内**：`registerPublic` 不套 `Gate`。`/api/share/{token}` 关闭时由它自己返回 404，而不是 409——旧链接应该"打不开"，不该告诉匿名访客"这台服务器关了分享"。

---

## 4. 生命周期时序

### 4.1 启动（顺序是承重的）

```
registerPlugins(port)                 ← 必须最先（内部 pluginall.RegisterAll，遍历 internal/plugin/all）
startPlugins(cfg)                     ← 只对 IsEnabled() 的插件调 Start，并 markPluginRunning(id, true)
defer stopPlugins()                   ← 紧挨着 start，反向遍历，Stop 幂等
handler.RegisterRoutes(mux)           ← 之后才能按 ForRoute 套门控、调 MountRoutes
```

注册晚于任一步的后果都是**静默的**：注册表还是空的 ⇒ 没有任何 worker 启动（邮件不同步、Forge 不轮询、终端 Manager 从不创建）、没有任何路由被门控，而启动日志干干净净。这在本重构第一版里真实发生过，且没有单元测试能抓到——测试自己按"先注册再启动"的顺序调用，恰好就是 bug 违反的那个顺序。所以补了一条读源码比位置的文本守卫（`TestMainWiring_PluginsRegisteredBeforeStartAndRoutes`、`TestMainWiring_StopIsDeferredRightAfterStart`）。

`pluginRunning` 表（`map[string]bool`，加锁）记录"实际起过的插件"。它是"开关翻转"判定的唯一依据：`Enabled()` 读的是**新**配置，插件自己回答不了"上一刻我是不是开着"；表只在真正 Start/Stop 时更新，与实际的 goroutine 同源，所以不会出现"以为在跑其实没跑"。

### 4.2 热更新

```
PATCH /api/config
  ├─ applyConfigPatch(patch)          ← 写内存（功能字段转交 plugin/config）
  │    └─ applyHotReloadGlobals()     ← 末尾调用
  │         └─ reconfigureOnHotReload ← main 注册的回调
  │              └─ syncPluginLifecycle(cfg)
  │                   ├─ enabled && !running  → Start
  │                   ├─ !enabled && running  → Stop
  │                   └─ 其余（有 Reconfigure）→ Reconfigure
  ├─ writeConfigYAML(patch)           ← 写盘；失败则回滚内存并 500，**跳过**下一步
  └─ applyPluginDisableEffects(patch) ← 只对"这次 PATCH 碰了 section.enabled"的插件调
                                        OnDisable（仅关）或 SwitchEffect（两个方向）
```

五个要点：

- **翻转才起停，不翻转只 Reconfigure**。判错要么每次 PATCH 泄漏一个 goroutine，要么重新打开的功能永远死着。
- **功能开关的「热更新分类」是派生的**。PATCH 响应里的 `needs_restart` / `changed_cold_fields` 由 `!hotReloadFields[f] && !plugins.IsFeatureSwitchPath(f)` 判定：`<section>.enabled` 一律算热（它已经由 `syncPluginLifecycle` 起停过，报「需要重启」是错的）。所以新增功能开关**不用**往 `hotReloadFields` 补行；那个白名单只保留非 `.enabled` 的子字段（它同时是「哪些字段真的需要重启」的文档）。
- **`syncPluginLifecycle` 跑在写盘之前**，所以它只能做可逆的事（起停 goroutine、重建 Manager）。不可逆的撤销必须在写盘成功之后。
- **`OnDisable` / `SwitchEffect` 的时机不能改**：先撤销再写盘失败会毁掉用户的数据，而开关其实还开着——两种状态里最坏的组合。守卫 `TestOnDisableRunsOnlyAfterPersist` 用一个"config 是个普通文件"的数据目录让写盘必失败，断言钩子没被调用、内存也回滚了。
- **撤销失败是 warning，不是 500**。配置已经落盘且生效是权威事实；回 500 会让客户端把开关回滚成"开"，而服务端是关的，用户被告知"保存失败"但改动其实生效了。

### 4.3 关闭

`stopPlugins` 反向遍历（后启动的先停，消费者先于依赖下线），对**所有**声明了 `Stop` 的插件调用一次，包括从未启动的——那是安全的方向：半途失败的 Start 也能拿到清理。所以 `Stop` 必须幂等。

---

## 5. `GET /api/features` 契约

需要鉴权（与 `/api/config` 同级：答案是服务端配置，不是公开能力）。

```json
{
  "features": [
    { "id": "forge", "enabled": true, "available": true, "reason": "", "defaultOn": true }
  ]
}
```

| 字段 | 含义 |
|---|---|
| `id` | 插件 ID |
| `enabled` | 服务端功能开关（功能轴） |
| `available` | 运行时可用性（能力轴），与 `enabled` 正交 |
| `reason` | `""` / `"disabled"` / `"capability"` |
| `defaultOn` | 该功能未写入配置时的默认状态，供前端首屏判断 |

规则：

- **`disabled` 压过 `capability`**。一个被关掉的功能哪怕宿主也跑不了，对用户来说仍然是"被关掉了"（他去设置里打开即可）。
- **空注册表也要序列化成 `[]`，不能是 `null`**（前端按数组处理）。
- 只读：非 GET 返回 405。

前端语义（`useFeatureFlags.ts`）：

- `/api/features` 是**唯一权威**，前端不再维护一份默认值镜像，也不各自去 `/api/config` 里按形状掏 `serverConfig.forge.enabled`。
- **未加载时读作"全开"**：快照为空 ⇒ `isFeatureEnabled()` 落到配置快照，再落到"缺键当开"。这与服务端对"开关出现之前写下的配置"的处理一致，所以首屏不会有页签闪一下再消失。
- 请求失败保留上一次的快照，**绝不因为失败把功能判成关**。
- `loadConfig()` / `patchConfig()` 之后都会刷新这份快照，所以设置里拨完开关，Dock 与各入口不用刷新页面就跟着变。

---

## 6. 为什么配置白名单保持手写

白名单现在分两张：

| 表 | 放什么 |
|---|---|
| `handler.PatchableConfigPaths` | 不属于任何功能的字段（`user.nickname`、`chat.*`、`upload.*`） |
| `internal/plugin/config/<id>.go` | 属于某个功能的字段（`Fields`），同时声明默认值、校验与写入 |

`Plugin.Section` 声明了"这个功能由哪个配置段控制"，守卫据此检查该段下每个 yaml 字段都在白名单里——但白名单本身**不派生**。

反射派生白名单等于把安全边界交给反射：结构体里加一个字段（哪怕只是内部状态、迁移用的旧键）就自动开放了写入。所以：

- 白名单手写；
- 功能字段的**归属与校验**读同一份 `internal/plugin/config`（静态包，不是运行时注册表）：handler 单测注册表为空也能工作，因此不存在两份清单漂移；`Plugin.ConfigFields` 只是 `config.Fields("<id>")` 的引用；
- 该段里确实不该被设置页修改的字段，在 `nonPatchableByDesign` 里登记**并写明原因**（`forge.credentials`、`forge.schemes`、`forge.platforms`、`terminal.max_line_bytes`、`terminal.max_buffer_mb`）；
- 豁免本身也有守卫（`TestPluginRegistry_NonPatchableExemptionsAreReal`）：指向不存在字段的豁免 = 红（它会静默预批一个未来可能被复用的名字），既豁免又可 PATCH = 红。

配套的两条漂移守卫在 `internal/handler/settings_config_paths_test.go`：白名单里的键必须在 `model.Config` 里真实存在（否则写进一个没人读的孤儿键，开关看起来保存成功却什么也不做）；标了热更新却不可 PATCH 的路径 = 死重量。

---

## 7. 不变量 ↔ 守卫测试

| 不变量 | 守卫 |
|---|---|
| ID 唯一、全小写 | `cmd/server/plugin_registry_test.go` `TestPluginRegistry_IDsAreUniqueAndLowercase` |
| 有 `Section` ⇒ 该段每个 yaml 字段可 PATCH / 有登记豁免 / 是容器 | `TestPluginRegistry_EverySectionIsFullyWhitelisted` |
| `nonPatchableByDesign` 的每条豁免都指向真实字段、且确实不可 PATCH | `TestPluginRegistry_NonPatchableExemptionsAreReal` |
| 有 `Enabled` ⇒ `DisabledKey()` 在中英两边都存在 | `TestPluginRegistry_EveryDisabledKeyIsLocalized` |
| 破坏性关闭（forge / share）必须声明 `OnDisable` | `TestPluginRegistry_DestructiveDisablesDeclareHooks` |
| `OnDisable` 与 `SwitchEffect` 不能同时声明 | `TestPluginRegistry_SwitchHooksAreExclusive` |
| 任务开关必须是双向的 `SwitchEffect` | `TestPluginRegistry_TasksUseSwitchEffect` |
| 有 `Section` ⇒ `APIPrefixes` 非空 | `TestPluginRegistry_SectionsDeclareRoutes` |
| 有 `APIPrefixes` ⇒ `MountRoutes` 非空 | `TestPluginRegistry_PrefixedPluginsDeclareMountRoutes` |
| 每个声明的前缀下真的挂上了路由 | `TestPluginRegistry_EveryRoutePrefixHasMountedRoutes` |
| `plugin/config` 的声明与已注册插件/`Section` 对得上（无孤儿功能、字段都在自己的段下） | `TestPluginRegistry_ConfigOwnershipMatchesFeatures` |
| 建了插件目录就必须在 `all.go` 注册 | `TestPluginRegistry_EveryPluginPackageIsRegistered` |
| 每个声明的前缀都命中至少一条真实路由，且该路由归回同一个插件（**按前缀计数**，否则同插件另一个前缀会掩盖手滑） | `TestPluginRegistry_DeclaredPrefixesOwnRealRoutes` |
| 每个已注册 `Section` 的 `<section>.enabled` 都判为热更新；未注册段与子字段判为 false | `TestPluginRegistry_FeatureSwitchesAreHot` |
| 关闭后，插件名下每条已鉴权路由都返回该插件的状态码与 msgKey | `plugin_gate_test.go` `TestGate_EveryDisabledPluginRejectsItsRoutes`（表驱动，遍历真实路由表） |
| 任务关闭后读 / 取消 / 删除仍可达，新建 / 编辑被拒 | `TestGate_TasksKeepsReadAndCleanupRoutes` |
| 终端状态端点放行，且 `platform_supported` 描述平台而非开关 | `TestGate_TerminalStatusIsExempt` |
| 公开分享路由不被门控（404 而非 409） | `TestGate_PublicShareRoutesStayOut` |
| 豁免必须命中真实路由，且属于本插件 | `TestGate_ExemptionsNameRealRoutes` |
| 开关翻转才 Start/Stop，否则 Reconfigure；启动不启动已关闭的；反向停止 | `TestPluginLifecycle_FlipsStartStopElseReconfigure` / `_DoesNotStartDisabledPlugin` / `_StopsInReverseOrder` |
| 每个启动过的插件都被 Stop | `TestStopPluginsStopsEverythingThatStarted` |
| Start / Stop 幂等（终端是唯一真正持有资源的例子） | `TestTerminalPluginLifecycleIsIdempotent` |
| `OnDisable` 只在写盘成功后执行；写盘失败内存回滚 | `TestOnDisableRunsOnlyAfterPersist` |
| `registerPlugins` 早于 `startPlugins` 与 `RegisterRoutes`；`defer stopPlugins()` 紧挨 start | `main_wiring_guard_test.go`（两条） |
| `ForRoute` 最长前缀 + 段边界；`IsGateExempt` 三种写法语义 | `internal/plugins/plugin_test.go` `TestForRoute` / `TestIsGateExempt` |
| `Gate` 的判定顺序、方法级豁免、无 writer 时的兜底体 | `internal/plugins/gate_test.go`（6 条） |
| `/api/features` 三条轴、`disabled` 优先、空表序列化成 `[]`、只读 | `internal/handler/features_test.go`（4 条） |
| 白名单键必须在 `model.Config` 里存在；热更新字段必须可 PATCH | `internal/handler/settings_config_paths_test.go`（2 条） |
| 每个 config 字段都在用户手册里有条目 | `internal/model/config_reference_test.go` `TestConfigReferenceMatchesConfigFile` |

前端的对应守卫（目录名 = 声明的 id、`order` 互不相同、`feature` 在服务端注册表里、`dockTabs.ts` 零 import 等）见 [界面插件 §8](../../plugin-dev/01-frontend.md)。

---

## 8. 已知取舍

- **门控是整段拒绝，不是逐接口声明**。好处：前缀下新增接口自动被拦（邮件当年漏的正是这里——关掉只停了后台收信，`/api/mail/send` 照发）。代价：需要"关闭后仍能读"的功能必须显式写豁免（任务），而不是写"只拦写"。
- **同一个 method+path 可能既该拒又该放行**。任务的 `PUT /api/tasks/{id}` 就是：整体更新要拒，取消运行中的执行必须放行，区别只在 body 里的 `action`，而路由层读不到 body。这类接口只能**整棵子树豁免**、把判断交给 handler（`rejectIfTasksDisabled`），代价是"某个动作忘了判"不会像门控那样自动兜住。
- **豁免靠人工维护，靠守卫兜底**。没法从路由表反推"这条该不该豁免"——那是个产品判断（用户关掉之后还需要做什么）。
- **终端用 503**：既有行为，语义是"运行时没起来"，与"被关掉了"不同。前端据此区分两种灰显。
- **`tasks` 的总开关用 `SwitchEffect` 而不是 `OnDisable`**：它的效果是**双向**的（关：取消运行中的执行、丢弃排队事件；开：重启 cron），而 `OnDisable` 只有一个方向。
- **纯偏好型插件也注册**（`live` / `notifications` / `stats`）：它们恒 `enabled=true`，注册的目的是让 `/api/features` 能回答"这个功能存在且可用"，前端不必按页签 id 硬编码一份清单。它们的可用性（能力）仍在客户端探测——端口映射有没有可用传输、原生能力位这些只有客户端知道。
- **不做运行时注册**：注册表一旦建立就不该再变。动态注册会让已套上 Gate 的路由、`/api/features` 与前端缓存的清单三者互相矛盾。
