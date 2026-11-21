# 测试与覆盖率

这个项目对插件的测试要求不是"最好有"，而是"没有就加不进功能"：每个插件包必须有**自己的**测试，因为覆盖率按包统计。

---

## 1. 前端

```bash
npm test                                              # 全量（带超时与僵尸进程清理）
./scripts/vitest-run.sh run src/composables/__tests__/dockPluginManifest.test.ts
npm run typecheck                                     # vue-tsc
```

### 加插件时相关的那几个

| 文件 | 管什么 |
|---|---|
| `composables/__tests__/dockPluginManifest.test.ts` | 插件清单的全部不变量（见 01-frontend.md §8） |
| `composables/__tests__/dockTabs.test.ts` | 页签 id 的安装、默认顺序、常驻项 |
| `composables/__tests__/useDockPlugins.test.ts` | 预设、显隐、顺序合并 |
| `composables/__tests__/useDockLifecycle.test.ts` | 后台资源随三轴起停 |
| `composables/__tests__/useFeatureFlags.test.ts` | `/api/features` 的读取与回退 |

`dockPluginManifest.test.ts` 是唯一需要你为新插件"补东西"的地方——但它不写死插件清单，而是 `import.meta.glob` 枚举目录。**建好目录、填对字段，它就自动覆盖你**；红了一般说明字段填错了，不是要你去改测试。

---

## 2. 后端

```bash
go build ./...
go test ./internal/plugin/foo/ ./cmd/server/ ./internal/handler/
go test ./...                       # 全量
./scripts/check-go-coverage.sh      # 覆盖率门禁（Tier 1 项目 + Tier 2 改动行）
```

### 每个插件包自带测试

不是风格问题：`go test -coverprofile` **只给被测包自己插桩**。包 A 的测试调用包 B 的函数时，B 的代码不进 A 的 profile；B 自己的 profile 里只有 B 的测试跑过的行。

所以：
- 把代码从 `handler` 搬进 `plugin/foo` 时，测试要一起搬；
- 新包没有 `*_test.go`，覆盖率就是 0%，门禁直接红。

### 公共夹具：`internal/handlertest`

```go
func TestXxx(t *testing.T) {
	env, teardown := setupTestEnv(t)   // 包内薄包装
	t.Cleanup(teardown)
	// env.ProjectDir / env.WatchDir 已就绪，DB 已指向内存库
}
```

- `handlertest.Setup` 只建 `projects` 表——**故意**这么小，用来复现"旧数据目录缺表"的场景。
- 需要更多表时用 `handlertest.FullSchema`（迁移后的生产 schema，24 张表）+ 该功能自己的 DDL 常量（`service.LiveNotesDDL` 这类）。
- **不要手抄生产 DDL**：抄一份就多一份漂移源。已经踩过：夹具里有个生产根本不存在的 `scheduled_tasks.description` 列，测试能建、真库没有。
- 请求构造与断言用 `handlertest.NewRequest` / `Call` / `AssertStatus` / `WithProjectCookie`。

### handler 的测试看不到插件路由

`handler.RegisterRoutes` 在注册表为空时**一条插件路由都不挂**（这是有意的：否则 handler 要 import 插件包，"删目录 = 删功能"就不成立）。

所以 handler 的测试里：

- 只测核心路由（chat / 文件 / 设置 / agent）——默认状态即可；
- 需要完整路由表的（路由表 vs OpenAPI、鉴权标记）先调 `registerAllPluginsForTest(t)`。

插件接口本身的测试写在插件包里，`handler` 不需要重复。

---

## 3. 覆盖率门禁怎么算

`scripts/check-go-coverage.sh` 两层：

| 层 | 规则 |
|---|---|
| Tier 1 | 每个包的覆盖率不低于基线的 1.5% |
| Tier 2 | **本次改动过的行**覆盖率 ≥ 80% |

Tier 2 的"改动行"是 `git diff` 的 hunk 行号，所以：

- 搬家（`git mv` + 改包名 + 加 `httpx.` 前缀）算改动：那些行必须被测到；
- 纯移动的大文件不会要求整文件 80%（git 识别重命名，只算真正变的行），但**每一条被改到的错误分支**都算。

实践上这意味着：搬一个功能时，顺手把它从来没测过的错误分支补上，比事后追着门禁补省事。

### 变异验证

加了守卫之后，**故意把它要抓的东西改坏**，确认它会红。这个项目里已经被骗过几次（守卫看着在检查，其实是死代码）。典型做法：

```bash
cp internal/plugin/foo/plugin.go /tmp/bak
# 改坏一处（删掉 MountRoutes / 把 OnDisable 换成 SwitchEffect）
go test ./cmd/server/ -run TestPluginRegistry 2>&1 | head
cp /tmp/bak internal/plugin/foo/plugin.go
```

---

## 4. 后端守卫清单

`cmd/server/plugin_registry_test.go` 里的都跑在真实注册表上：

| 守卫 | 抓什么 |
|---|---|
| `IDsAreUniqueAndLowercase` | id 重复 / 大写 |
| `EverySectionIsFullyWhitelisted` | 有 `Section` 但该段下有字段没进白名单（也没登记豁免） |
| `NonPatchableExemptionsAreReal` | 豁免指向不存在的字段，或既豁免又可改 |
| `EveryDisabledKeyIsLocalized` | 有 `Enabled` 但 `FooDisabled` 缺中英 |
| `DestructiveDisablesDeclareHooks` | forge / share 丢了 `OnDisable`（开关说"关了"但绑定/链接还在） |
| `SectionsDeclareRoutes` | 有 `Section` 却没有 `APIPrefixes` |
| `DeclaredPrefixesOwnRealRoutes` | 声明的前缀没有任何真实路由，或路由归到了别的插件 |
| `FeatureSwitchesAreHot` | `<section>.enabled` 被判成需要重启 |
| `ConfigFieldsMatchHandler` | 插件清单与 `builtinConfigFields` 不一致 |
| `EveryRoutePrefixHasMountedRoutes` | 声明了前缀但一条路由都没挂上 |
| `PrefixedPluginsDeclareMountRoutes` | 有前缀但 `MountRoutes` 为空 |
| `SwitchHooksAreExclusive` | 同时声明 `OnDisable` 与 `SwitchEffect` |
| `TasksUseSwitchEffect` | 任务开关退回单向钩子（"重新打开"会变成空操作） |
| `EveryPluginPackageIsRegistered` | 建了 `internal/plugin/<id>/` 但 `all.go` 没注册 |

另有：

- `main_wiring_guard_test.go`：注册早于启停与路由；`defer stopPlugins()` 紧跟 `startPlugins`。
- `plugin_gate_test.go`：关闭后每个插件的每条路由都返回该插件的状态码；任务的读/清理仍可达；终端状态端点放行；公开分享路由不被门控；豁免都指向真实路由。
- `internal/plugins/*_test.go`：`ForRoute` 最长前缀 + 段边界、`Gate` 判定顺序、重复配置路径 panic。
- `internal/handler/features_test.go`：`/api/features` 三条轴、`disabled` 优先、空表 `[]`、只读。
