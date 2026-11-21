# 两边的契约

界面插件和功能插件是两套独立的东西，只在四个地方对上：`id`、开关、`/api/features`、错误文案。

---

## 1. 同一个 id 的四个落点

```
web/src/plugins/foo/plugin.ts        id: 'foo'
        │
        ├─ 左栏页签 id ─────────────────────────┐
        ├─ feature: 'foo.enabled' ──┐            │
        │                           │            │
        ▼                           ▼            ▼
GET /api/features  ←──  internal/plugin/foo/plugin.go  ID: "foo"
  { id: "foo",            Section: "foo"
    enabled,              Enabled: func() { model.ConfigInstance.Foo.Enabled }
    available,            APIPrefixes: ["/api/foo/"]
    reason }              DisabledMsgKey 推导为 "FooDisabled"
```

| 东西 | 前端 | 后端 |
|---|---|---|
| 目录名 | `web/src/plugins/foo/` | `internal/plugin/foo/` |
| id | `plugin.ts` 的 `id` | `Plugin.ID` |
| 开关 | `feature: 'foo.enabled'` | `Section: "foo"` + `Enabled` |
| 接口前缀 | —（不声明） | `APIPrefixes: ["/api/foo/"]` |
| 错误文案 | 不需要写 | `FooDisabled` 中英都要有 |

**前端不声明前缀**：它只知道"这个页签依赖哪个开关"，接口路径是后端的事。

---

## 2. 开关的完整链路

```
用户在「功能编排」里拨开关
   │
   ├─ 写服务端 config.yaml（PATCH /api/config）
   │     ├─ 写盘成功
   │     ├─ syncPluginLifecycle：Start / Stop 后台资源
   │     └─ applyPluginDisableEffects：OnDisable / SwitchEffect
   │
   └─ 刷新 GET /api/features → 前端 isFeatureEnabled('foo.enabled') 变 false
         ├─ 左栏页签消失
         ├─ 所有跳到这个功能的入口（按钮、引用、通知）一起失效
         └─ 服务端接口返回 409 / 插件自定义状态码
```

关键点：

- **`/api/features` 是唯一权威**。前端不维护默认值镜像，也不去 `/api/config` 里按形状掏字段。
- **没加载时读作"全开"**，所以首屏不会先闪一下页签再消失。
- **请求失败保留上一次快照**，绝不因为失败把功能判成关。
- **开关是热更新的**：`<section>.enabled` 一律算热字段，不需要登记到 `hotReloadFields`。

---

## 3. `GET /api/features`

需要登录（和 `/api/config` 同级：答案是服务端配置，不是公开能力）。

```json
{
  "features": [
    { "id": "forge", "enabled": true, "available": true, "reason": "", "defaultOn": true }
  ]
}
```

| 字段 | 含义 |
|---|---|
| `enabled` | 服务端开关（功能轴） |
| `available` | 运行时可用性（能力轴），与 `enabled` 正交 |
| `reason` | `""` / `"disabled"` / `"capability"` |
| `defaultOn` | 没写进配置时的默认状态 |

规则：`disabled` 压过 `capability`（被关掉的功能，哪怕宿主也跑不了，对用户来说仍然是"你去打开一下"）；空注册表也序列化成 `[]` 不是 `null`；只读。

---

## 4. 错误文案的约定

功能被门控拦下时，服务端返回：

```json
{ "error": "…", "code": 409, "msgKey": "FooDisabled" }
```

- key 默认是 `<Id 首字母大写>Disabled`（`foo` → `FooDisabled`、`kb` → `KbDisabled`）。
- 特殊文案用 `DisabledMsgKey` 覆盖。
- 中英两份语言表都必须有，否则守卫红。
- 状态码默认 409（"你有权限，功能关着"）；终端用 503（"运行时没起来"）。

前端不需要认识这些 key——它只看 `/api/features` 的 `enabled`/`available` 来决定要不要显示入口。

---

## 5. 加一个带开关的功能：完整清单

以 `foo` 为例。

### 前端（3 个文件）

- [ ] `web/src/plugins/foo/plugin.ts`：`id: 'foo'`、`order`、`titleKey`、`descriptionKey`、`group`、`closable`、`icon`、`feature: 'foo.enabled'`、`settingsCategory: 'foo'`、`settingsPage`、`panel.load`
- [ ] `web/src/plugins/foo/i18n.ts`：`zh/en` 各一句 `desc`（不可用原因可选）
- [ ] `web/src/plugins/foo/settings.ts`：这个功能自己的设置项（没有设置页就不用）
- [ ] 面板组件放 `web/src/components/foo/FooPanel.vue`（接收 `active`，首屏用 `PanelSkeleton`）
- [ ] 标题等导航文案加到 `i18n/locales/{zh,en}.ts`（`nav.foo`）

**不需要**再改 `dockTabs.ts`、`settingsFieldMap.ts`、全局语言表里的 `dockPlugins.desc`。

### 后端（4 个文件 + 一行）

- [ ] `internal/plugin/foo/plugin.go`：`Register()` 字面量
- [ ] `internal/plugin/foo/routes.go`：`MountFooRoutes`
- [ ] `internal/plugin/foo/feature_switch.go`：有"关掉要撤销"才要
- [ ] `internal/plugin/foo/<实现>.go` + 测试
- [ ] `internal/plugin/all/all.go` 加一行 `foo.Register()`
- [ ] 有配置的话：`internal/plugin/config/foo.go` 写 `Fields` / `Defaults` / `Validate` / `Apply`，并在 `plugin.go` 里 `ConfigFields: config.Fields("foo")`
- [ ] `internal/model/config.go` 加结构体字段；`configResponse` 加下发形状；配置手册加一行
- [ ] 语言表加 `FooDisabled`（中英）

### 验证

```bash
go build ./... && go test ./cmd/server/ ./internal/plugin/foo/ ./internal/handler/
cd web && npx vitest run src/composables/__tests__/dockPluginManifest.test.ts
```

守卫会在这些地方红：`id` 与目录名不一致、`order` 重复、`feature` 打错、配置字段只加了一边、声明了前缀却没挂路由、`MountRoutes` 为空、`OnDisable` 与 `SwitchEffect` 同时声明。

---

## 6. 只加界面页签（没有服务端功能）

- 建 `web/src/plugins/foo/{plugin.ts,i18n.ts}` + 面板组件。
- 不写 `feature`，不建后端插件。
- 设置项如果只存在本机，`settings.ts` 里用 `source: 'local'`。

典型：文件浏览、历史会话、端口映射。

---

## 7. 只加后端功能（没有页签）

- 建 `internal/plugin/foo/` + `all.go` 一行。
- 不建 `web/src/plugins/foo/`。
- 如果想让它出现在"功能编排"里（让用户能关掉），前端仍需要一个页签；纯后台功能可以不出现。

典型：统计（`stats` 有页签但接口与页签无关）、通知中心的后台写入。
