# 功能插件（服务端）

功能插件就是 `internal/plugin/<id>/` 这个目录。它拥有这个功能的**全部服务端事实**：接口、开关、后台资源、可改配置。

删掉这个目录 = 删掉这个功能（接口、后台任务、开关一并消失，编译期就能发现漏删的引用）。

---

## 1. 目录里放什么

```
internal/plugin/<id>/
├── plugin.go        必需。Register()：我是谁、管哪些前缀、开关怎么读、后台跑什么
├── routes.go        有 HTTP 接口就要。MountXxxRoutes(m plugins.Mount)
├── feature_switch.go 有"关掉要做不可逆动作"才要。OnDisable 或 SwitchEffect
├── jsonkeys.go      可选。本包用到的 API JSON key 常量
├── <实现文件>.go     业务逻辑（原来在 handler 里的那些）
└── *_test.go        自己的测试（不是可选的，见 04-testing.md）
```

`internal/plugin/all/all.go` 里加一行 `xxx.Register()`，这是"加一个功能"唯一要碰的既有文件。

---

## 2. `plugin.go`

```go
package foo

func Register() {
	plugins.Register(&plugins.Plugin{
		ID:          "foo",
		Section:     "foo",                 // config.yaml 里的段名；没有总开关就不填
		Enabled:     func() bool { return model.ConfigInstance.Foo.Enabled },
		DefaultOn:   true,
		APIPrefixes: []string{"/api/foo/"},
		MountRoutes: MountFooRoutes,
		ConfigFields: map[string]bool{
			"foo.enabled": true,
			"foo.interval": true,
		},
		Start: func(cfg model.Config) { startWorker(cfg) },
		Stop:  stopWorker,
		OnDisable: clearEverything,
	})
}
```

### 字段

| 字段 | 说明 |
|---|---|
| `ID` | 稳定标识。前端页签 id、`/api/features` 的键、i18n 错误键前缀都靠它 |
| `Section` | `config.yaml` 的段名。空 = 没有服务端总开关（纯偏好型） |
| `Enabled` | 读**实时**开关。`nil` = 恒开。必须读包级全局或 `model.ConfigInstance`，**不能读启动快照**——热更新会在运行期改它 |
| `DefaultOn` | 只用于文档和"缺键当开"的声明。真正的默认值在 `model.ApplyDefaults` |
| `APIPrefixes` | "这些路由归我"。命中前缀的接口自动被门控（见 §4）。有 `Section` 就必须非空 |
| `MountRoutes` | 挂接口。函数住在 `routes.go`。声明了前缀就必须有它，否则接口全 404 |
| `Start(cfg)` | 启动后台资源。**必须幂等** |
| `Stop()` | 停止。必须幂等；关机时对所有插件都调一次（含没启动成功的） |
| `Reconfigure(cfg)` | 开关没变、只是参数变了（间隔、批量大小） |
| `OnDisable() []string` | 关闭**且配置写盘成功之后**的不可逆动作（清空绑定、撤销链接）。返回 warnings |
| `SwitchEffect(enabled bool) []string` | 双向开关效果（任务：关掉取消执行、打开重排 cron）。与 `OnDisable` **互斥** |
| `DisabledMsgKey` | 拒绝时的 i18n 键。空则按 `<Id>Disabled` 推导 |
| `DisabledStatus` | 拒绝时的状态码。`0` → 409；终端用 503（"运行时没起来"） |
| `Available()` | **运行时**可用性（不是配置）。`nil` = 恒可用 |
| `GateExempt` | 关着也必须能访问的路径，见 §4 |
| `ConfigFields` | 这个功能允许被 PATCH 的配置字段（声明归属），见 §6 |

`Register` 对以下情况**直接 panic**（都是编程错误，静默覆盖最难查）：

- `nil`、空 `ID`、重复 `ID`；
- 前缀不以 `/` 开头、`GateExempt` 有空条目；
- 两个插件声明同一个配置字段。

---

## 3. 装配顺序

```go
// cmd/server/main.go
registerPlugins(port)         // ① 先注册（内部就是 pluginall.RegisterAll）
startPlugins(cfg)             // ② 再启后台资源（只启 IsEnabled() 的）
defer stopPlugins()           // ③ 反向停
...
handler.RegisterRoutes(mux)   // ④ 最后挂路由：此时才能按前缀套门控、调 MountRoutes
```

顺序错了**不会报错**，只会安静地少挂接口 / 少起 worker。所以有两条读源码的守卫盯着（`main_wiring_guard_test.go`）。

`RegisterAll(port)` 里之所以是显式调用而不是各插件 `init()` 自注册：终端的 Manager 需要服务端口，`init()` 拿不到构造参数；显式调用也让"加载了哪些插件"在编译期一目了然。

---

## 4. 路由与门控

```go
// routes.go
func MountFooRoutes(m plugins.Mount) {
	m.Auth("/api/foo/items", ServeFooItems)     // 要登录
	m.Public("/api/foo/health", ServeFooHealth) // 故意免登录（OpenAPI 里要写 security: []）
	m.Raw("/api/foo/entry", ServeFooEntry)      // 套中间件但不套鉴权，handler 自己判
}
```

三种挂载方式：

| 方法 | 鉴权 | 门控 | 路由表标记 | 什么时候用 |
|---|---|---|---|---|
| `Auth` | ✅ | ✅ | authenticated | 绝大多数接口 |
| `Public` | ❌ | ❌ | public | 公开分享页、健康探测这类"靠 token 或本来就该公开"的 |
| `Raw` | ❌（handler 自己判） | ❌ | authenticated | 未登录时必须给**重定向**而不是 JSON 401 的入口（`/share-target`） |

门控规则：

1. `RegisterRoutes` 对每条 `Auth` 路由调 `plugins.ForRoute(pattern)`，命中前缀就包一层 `Gate`。
2. `Gate` 的顺序：功能开着 → 放行；命中 `GateExempt` → 放行；否则按 `DisabledStatus` 写错误，key 是 `DisabledKey()`。
3. **公开路由不套门控**：`/api/share/{token}` 关掉时由它自己 404，而不是告诉匿名访客"这台服务器关了分享"。

`GateExempt` 是显式开洞，写法有四种语义：

| 写法 | 豁免范围 |
|---|---|
| `"/api/foo/"` | 这棵子树（不含 `/api/foo` 本身） |
| `"/api/foo/binding"` | 精确这一条 |
| `"GET /api/foo/binding"` | 只豁免 GET |
| `"/api/foo/read/"` | 子树，所有方法 |

现有三处豁免与理由：

| 插件 | 豁免 | 为什么 |
|---|---|---|
| `forge` | `GET /api/forge/binding` | 前端靠它判断"未绑定"，关闭时要返回空对象而不是 409；写仍然被拒 |
| `tasks` | `GET /api/tasks`、`/api/tasks/` | 只有"新建"能被 method+path 识别；动作与"整体更新"共用 `PUT /api/tasks/{id}`，靠 body 里的 `action` 区分，路由层读不到 body |
| `terminal` | `/api/terminal/status` | 只读能力探测。门控它会让前端把"关了"和"跑不了"混为一谈 |

---

## 5. 生命周期

### 进程级：`Start` / `Stop` / `Reconfigure`

启动时：`startPlugins` 只对 `IsEnabled()` 的插件调 `Start`。

热更新时（`syncPluginLifecycle`，在**写盘之前**跑）：

| 情况 | 动作 |
|---|---|
| 开着但没跑过 | `Start(cfg)` |
| 关着但还在跑 | `Stop()` |
| 开关没变 | `Reconfigure(cfg)` |

所以三个钩子都必须**幂等**，而且 `Start` 里不要再判断"我是不是该开"——那是调用方的事。

### 开关效果：`OnDisable` / `SwitchEffect`

在**配置写盘成功之后**跑（`applyPluginDisableEffects`），只对"这次 PATCH 真的碰了 `<section>.enabled`"的插件触发。改子配置项不会触发。

| 钩子 | 什么时候用 |
|---|---|
| `OnDisable` | 关闭即撤销：清空仓库绑定、撤销分享链接 |
| `SwitchEffect(enabled)` | 两个方向都要动状态：关掉取消执行、打开重排 cron |

两者只能选一个（守卫会红），否则关闭时清理会跑两遍。

时机不能改：先撤销再写盘失败，会毁掉用户数据而开关其实还开着。

失败**只报 warning，不返回 500**：配置已经落盘且生效是权威事实；回 500 会让客户端把开关回滚成"开"，而服务端是关的。

---

## 6. 配置字段：三处，各有分工

| 位置 | 放什么 | 谁读 |
|---|---|---|
| `handler.PatchableConfigPaths` | 不属于任何功能的字段（`user.nickname`、`chat.*`、`upload.*`） | PATCH 校验 |
| `handler.builtinConfigFields` | 属于某个功能的字段（实际校验） | PATCH 校验 |
| `plugins.Plugin.ConfigFields` | 同样的功能字段，声明"归谁" | `/api/features`、守卫 |

**为什么校验不直接读插件清单**：字段是编译期事实，注册表是运行时状态。若校验建立在注册表上，handler 自己的单测（注册表为空）会把每个功能字段都判成非法，几十个测试同时红，而生产上恰好正常。所以保留两份，用守卫 `TestPluginRegistry_ConfigFieldsMatchHandler` 双向钉住。

加一个配置字段要动两处：`builtinConfigFields` 加一行、插件 `ConfigFields` 加一行。漏一处守卫会红。

`Plugin.Section` 下的每个 yaml 字段都必须在白名单里，或者登记为"故意不可改"（`nonPatchableByDesign`，要写原因）。

---

## 7. 依赖方向

```
plugin/<id>  →  httpx / filex / cfgwrite / sharecore / stt / ws / service / store / model
     ↑
handler.RegisterRoutes（只注入 Mount，不认识任何具体插件）
```

**插件包不能 import `internal/handler`**：那会让"删掉一个插件目录"变成"handler 编译失败"。需要 handler 里的能力时，把它抽到上表那些公共层——它们就是因为这个原因存在的。

反过来，handler 也不该 import 具体插件包（测试里可以，为了验证跨功能行为）。

唯一允许的单向功能依赖：`share → live`（分享页要把 `/notes`、`/signal` 这些子路径转给会议纪要）。删 `live` 会连带破坏 `share`，删 `share` 不影响 `live`。

---

## 8. 一个完整的例子：任务（tasks）

它同时用了三种钩子，是最全的样本：

- `plugin.go`：`Section: "tasks"`、`Enabled` 读 `model.TasksEnabled`、`SwitchEffect: TasksSwitchEffect`、`GateExempt` 放行读操作。
- `routes.go`：`/api/tasks` 与 `/api/tasks/` 两条。
- `feature_switch.go`：`TasksSwitchEffect(enabled)` 调 `service.GlobalScheduler.SetEnabled`。
- `scheduler.go`：接口实现，内部用 `rejectIfTasksDisabled` 补"路由层读不到 body"的判断。

照着它的形状建新插件，四类文件各看一眼就知道该写什么。
