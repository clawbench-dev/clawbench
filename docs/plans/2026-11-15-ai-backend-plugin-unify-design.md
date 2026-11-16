# AI 后端插件化收口设计

> 目标读者：要动手改这块的人。
> 相关但不相同的文档：`docs/plans/2026-11-15-full-plugin-architecture-design.md` 讲的是**功能插件**（会议 / 邮件 / Forge 这些 Dock 页签），本文讲的是 **AI 后端插件**（claude / codebuddy / kimi 这些智能体）。两件事互不影响，不要混。

## 0. 一句话

改造前加一个后端是「新建 1 个目录 + 6 处登记 + 可能还要去核心包开 3 个文件」；现在
是「新建 1 个目录 + 跑一次 `go generate` + 两处人工声明」。

## 0.1 实施进度（2026-11-16）

| 阶段 | 状态 | 说明 |
|---|---|---|
| Phase 1 注册收编 | ✅ 完成 | 4 个注册入口 → 1 次 `backends.Register`；`CLI`/`Custom` 合并并接上 |
| Phase 2 自动发现 | ✅ 完成 | `genbackendimports` + `backends/all`；`main.go` 20 行 → 1 行 |
| Phase 3 行为钩子 | ✅ 完成（3 处例外） | 11 个 ACP 解析/桥接文件搬进子包 |
| Phase 4 搬 CLI 解析 | ✅ 完成 | vecli / deepseek / grok / claude(+StreamParser) / codex / opencode / pi / openclaw 全部搬走 |
| Phase 5 数据搬家 | ✅ 完成 | 4 张数据表全部搬进子包（见 0.2） |

**加一个后端现在要做的全部事情**：

1. 新建 `internal/ai/backends/<id>/`，一个 `init()` 里 `backends.Register(&BackendPlugin{...})`
2. `go generate ./internal/ai/backends/all`
3. 跑测试，两条守卫会点名要你补声明——都是**该由人拍板**的结论，不是遗漏：
   - `TestBackendSkillAwarenessMatchesReviewedTable` → 这个后端到底自不自己加载技能（1 行）
   - `TestSpecDocsBackendCountMatchesRegistry` → 文档里的后端计数（当前 4 处、2 个文件）

`cmd/server/main.go` 不用改，`model` / `service` / `handler` 的源码也不用改。

**实测**（2026-11-16，临时加了一个 `fakeacp` 后端再删掉）：改动只有 2 个文件——
新建的 `register.go` 与生成出来的 `all.go`；`go build ./...` 一次通过；守卫恰好点出
上面两条。改造前同样的事是 14 个文件（cursor）/ 22 个文件（command code）。

## 0.2 Phase 5 搬了什么

| 数据 | 原位置 | 现位置 |
|---|---|---|
| `providerEnvTable`（20 行 BYOK 环境变量） | `model/provider_env.go` | 各子包的 `ProviderEnv:` 字段 |
| `mcpUnsupportedBackends`（4 个） | `model/mcp.go` | `BackendSpec.MCPUnsupported` |
| 7 个内置模型目录 + 5 张名称表 + 2 张顺序表 | `model/catalogs.go` | 各子包的 `catalog.go` |
| 配置目录写入的 switch | `service/provider_config_dir.go` | 本分支已不存在（见 0.4） |

`model` 侧只剩「注册表 + 查表」：`RegisterProviderEnvSpec` / `RegisterMCPUnsupported`，
由 `backends.Register` 在 init 时转发。`model/catalogs.go` 整个文件删掉了。

## 0.3 搬完之后测试怎么接线（重要）

数据搬进插件后，**任何不 import 插件的测试二进制里注册表都是空的**。这不会让测试
静默通过，而是让每个 `BuildProviderEnv("claude", …)` 之类断言失败——所以每个受影响的
包都要有一个「只为副作用存在」的外部测试文件：

| 文件 | 为什么在这里 |
|---|---|
| `internal/model/backend_plugins_test.go` | `provider_env_test.go` / `mcp_test.go` 是 `package model`，内部测试包 import `backends/all` 会成环 |
| `internal/ai/backend_plugins_test.go` | 同上，`package ai` |
| `internal/service/backend_plugins_test.go` | codex 配置文件的测试读 `ProviderUsesConfigFile` |
| `internal/handler/backend_plugins_test.go` | provider / agent 绑定校验读真实后端的能力面 |

机制：外部测试包（`package xxx_test`）与内部测试包编译进同一个二进制，Go 在任何测试
函数之前完成所有 import 的 init。**这是唯一能让包内测试看到插件数据的办法**，不要
改成在测试里手工播种夹具——那等于把刚搬走的数据再抄一份。

**副作用（值得知道）**：`TestAgentSelfTest_CliTransportOnAcpOnlyBackendFails` 原本用
`grok` 当「没有 CLI 实现」的样本，靠的正是「handler 测试没注册插件所以 grok 查不到
工厂」。注册表被填上后这个前提失效（grok 有 CLI 回退了），已改用 `cursor`。

## 0.4 例外与合并注意

**仍留在核心包的 3 个后端专属文件**（都在代码注释里写明原因）：

- `acp_opencode_children.go`：挂在 `ClawBenchACPClient` 的私有 map 上，是客户端逻辑而非事件映射。
- `acp_meta_codebuddy.go`：**不只是测试夹具**。`costFieldCarriesCredit`（CodeBuddy 把
  额度塞进标准 `usage_update.cost`，这条判断决定它能不能被记成美元成本）和
  `refusalDetailFromMeta` 都被通用 prompt 路径直接调用，另有十来个 `codebuddy.ai/*`
  键常量散落在核心与核心测试里。搬走要先给这两条策略找到能力位，属于重新设计而不是
  搬家。导出为 `ai.ExtractCodeBuddyMeta`，插件以 `ExtractMeta` 注册同一实现。
- `opencode_error_types.go`：ACP 侧错误分类器与 CLI 解析器共用的一张表。

**合并 main 时注意**：本分支从 `4ce9378c4` 分叉，main 后来加了「每智能体独立配置目录」
（`ProviderEnvSpec` 新增 `ConfigDir*` 字段）。冲突面只有 `model/provider_env.go` 与
`service/provider_config.go` 两个文件，但语义要人工搬：

- claude / kimi / pi / codebuddy 的 `ConfigDirEnv` / `ConfigDirFile` /
  `ConfigDirProtocols` / `ConfigDirProviderID` / `ConfigDirSecretInFile` /
  `ConfigDirDeclaresModels` / `ConfigDirAddressPrefix`，要跟着各自的
  `ProviderEnv` 一起进子包。
- main 修正过的**协议归属**已经先搬进本分支：codebuddy 的 `CODEBUDDY_*` 从
  Anthropic 改成 OpenAI、kimi 删掉 Anthropic 行（依据见两处代码注释）。这两处
  同时改了三张测试的期望值（`ProtocolSupportsAuthMode`、`TestProviderRejectsDeadAuthMode`、
  `TestProvidersAPI_InvalidAuthModeCarriesField`），已与 main 对齐。
- 本分支删除了 `model/catalogs.go`，main 未改动该文件，不会冲突。
- `service/provider_config.go` 的 `ApplyProviderConfigToBackend` 在本分支还写死
  `if backend == backendCodex`；main 已把它泛化成配置目录机制（claude / kimi / pi /
  codebuddy 都支持）。**合并时用 main 的版本，不要在分支上重复实现一遍**——那只会
  制造第二个冲突点。

**测试环境的两处既有失败**（与本改造无关，在主仓同样可复现或可解释）：

- `TestCleanupOrphans_KillsReParentedProcess`：需要子进程被 re-parent 到 PID 1，在本机
  的进程监管下不是 PID 1（实测 1435）。主仓同样失败。
- 6 个 `TestServeSharePage_*` / `TestSharePage_*`：需要 `internal/frontend/dist` 里有
  构建产物，全新 worktree 里那是空目录。跑过 `npm run build` 即消失。


---

## 1. 现状（带证据）

> 本节记录的是**改造前**的状态，留作「为什么这么改」的依据；当前的完成情况看 0.1。

### 1.1 已经做好的部分

| 项 | 证据 | 评价 |
|---|---|---|
| 有插件结构体 | `internal/ai/backends/plugin.go` 的 `BackendPlugin` | 方向对 |
| 有注册表 + 查表 | `registry.go`（`Register` / `Lookup` / 各类 `LookupACP*`） | 方向对 |
| 核心包不 import 子包（避免成环） | `all.go:13` 明确写了 | 必须保留 |
| 后端规格是纯数据 | `model.BackendSpec`（`discovery.go:13`） | 好 |
| 特殊约束用能力位表达 | openclaw 四条约束各落一个 bool：`ACPResumeIsProcessLocal` / `ModesMirrorThoughtLevel` / `ToolCallWrapperTitlePrefixes` / MCP 黑名单 | 好，这是样板 |
| 运行中插话按后端分列且已插件化 | `backends/claude/midturn.go`、`codebuddy/midturn.go`，经 `acp_wire.go` 注入 | 好 |

结论：**声明层做对了，注册层做了一半，实现层没做。**

### 1.2 六个具体问题

**P1｜同一件事有 4 个并行注册表。**
一个后端要分别报到：

| 注册入口 | 调用次数 | 位置 |
|---|---|---|
| `backends.Register(&BackendPlugin{})` | 20 | 各子包 `register.go` |
| `ai.RegisterBackend(id, factory)` | 13 | 各子包 `cli.go` / `custom.go` |
| `model.RegisterModelSource(...)` | 14 | 各子包 `discovery.go` |
| `ai.ListSessionsFromDiskRegister(id, fn)` | 2（codebuddy / codex） | 各子包 `list_sessions.go` |

**P2｜`BackendPlugin` 里的 `CLI` / `Custom` 两个字段是死的。**
`plugin.go:28`（`CLI *CLIPlugin`）、`plugin.go:33`（`Custom *CustomPlugin`）全仓只有 `registry_test.go:25` 用过，没有一个真实后端在用。`plugin.go:13` 的注释自己写着 "Eventually, ai.RegisterBackend should be deprecated in favor of a unified backends.Register()" —— 迁移停在了半路。

**P3｜两个完整后端实现住在核心包。**
`internal/ai/codex.go` 的 `CodexBackend`、`internal/ai/vecli.go` 的 `VeCLIBackend` 是完整实现；子包里的 `codex/custom.go`、`vecli/custom.go` 只是 `return &ai.CodexBackend{}` 的壳工厂。它们搬不走，是因为用到了 `emitStreamEvent`、`streamChanSize`、`parseVeCLISessionSummary` 这些未导出的东西。

**P4｜后端专属代码有一半还在核心包，靠 `switch backend` 分派。**

| | 行数 |
|---|---|
| `internal/ai/backends/*/` 所有子包（不含测试） | 4191 |
| `internal/ai/` 里以具体后端命名的文件（28 个，不含测试） | **4699** |
| 这 28 个文件对应的测试 | ≈8100 |

分派点：

- `acp_tool.go:19` / `acp_tool.go:48` —— 两个 `switch backend`，7 个后端各一个解析器
- `acp_meta.go:181` —— `extractMetaUsage` 的 `switch`
- `acp_parent_link.go:92` —— 父子工具调用关联的 `switch`
- `acp_events.go:223 / :305 / :557` —— 三处 `if backendID == "codebuddy"` / `isOpenCodePlanBackend`
- `codebuddy_plugin_scanner.go:31` —— `ScanCodeBuddyPluginCommands` 被 `acp_conn_lifecycle.go:1246` 直接调用

**P5｜核心包里还有 4 张「后端 → 某个事实」的表。**

| 表 | 位置 | 现在怎么用 |
|---|---|---|
| `providerEnvTable` | `model/provider_env.go:115` | BYOK 环境变量注入 |
| `mcpUnsupportedBackends` | `model/mcp.go:124` | 哪些后端会静默忽略 MCP |
| `userConfigDir` 的 switch | `service/provider_config_dir.go:56` | BYOK 配置目录落点 |
| `applyProviderConfigDir` 的 switch | `service/provider_config_dir.go:167` | 谁写哪种配置文件 |

（`model/modelprice.go` 的 `vendorFamilies` 按**模型 id 前缀**匹配，不是按后端，不属于这类问题。）

**P6｜守卫测试是硬编码表，逼着你改第 7、8 个文件。**

| 守卫 | 位置 | 不填的后果 |
|---|---|---|
| 后端总数精确断言 | `model/discovery_test.go:35` | 测试红 |
| 技能目录双向表 | `backends/native_skills_test.go:54` | 测试红 |
| BYOK 表覆盖每个后端 | `backends/provider_env_coverage_test.go:50` | 测试红 |
| 中文硬编码白名单 | `service/i18n_hardcoded_guard_test.go:115` | 测试红 |

这些守卫本身有价值（防止漏登记），问题在于它们是**手工清单**，而不是从注册表派生。

### 1.3 加一个后端现在到底要动多少文件

拿两次真实接入统计（不是估算）：

| 后端 | 改动文件 | 其中真正让功能跑起来的 |
|---|---|---|
| Cursor（纯 ACP、行为规矩） | 14 | 2（新目录 + `main.go` 一行 import） |
| Command Code | 22 | 3（多一条 AUTH_REQUIRED 错误翻译） |

Cursor 那次剩下的 12 个：3 个前端展示（`backendNames.ts` / `agentIcons.ts` / `mono-icon-colors.css`）、4 个守卫测试、3 个文档、2 个测试文件。

---

## 2. 目标与非目标

### 2.1 目标

1. 加一个**行为规矩**的后端 = 新建 1 个目录 + 跑 `go generate`，其余零手工改动（前端图标/显示名走兜底）。
2. 加一个**有自己脾气**的后端（自定义工具解析、自定义 `_meta`、自定义计划桥接）= 全部代码都在自己的目录里，核心包不动。
3. 删掉重复的注册入口，一个后端只有一次 `init()`。
4. 所有守卫测试改成「从注册表/目录派生」，只保留真正需要人拍板的判断（比如「这个后端到底自不自己加载技能」）。

### 2.2 非目标

- **不做运行时动态加载**（Go 的 `plugin` 包跨平台限制太多，Windows 上基本不可用）。
- **不改 ACP 协议层**（连接、通知循环、权限、看门狗都不动）。
- **不改前端智能体选择器的交互**。
- **不合并 Qoder 国际版 / 国内版**（两个独立发行包，合并会让凭据串味）。
- **不把 `providerEnvTable` 里的数据搬进前端**（BYOK 是服务端行为）。

---

## 3. 目标结构

```
internal/ai/
├── acp_*.go                ← ACP 引擎（连接、事件、会话），只留通用逻辑
├── acp_behavior.go         ← 后端钩子接口 + 查表（由 backends 注入）
├── cli_backend.go          ← CLI 骨架，保留
└── backends/
    ├── plugin.go           ← 插件类型（BackendPlugin / CLIPlugin / ACPPlugin）
    ├── registry.go         ← Register / Lookup / All（Register 负责转发到各注册表）
    ├── acp_wire.go         ← 把查表函数注入 internal/ai，避免成环
    ├── all/all.go          ← 生成的空导入清单（go generate）
    ├── claude/             ← spec + CLI 解析 + ACP 解析 + meta + midturn + catalog
    ├── codebuddy/          ← spec + 团队桥接 + 计划桥接 + 命令扫描 + …
    └── …
```

> 与最初设想的差异：原计划把类型与注册表下沉到一个新的叶子包 `backendplugin`，
> 让 `backends` 能反向 import 子包。实际实施时发现没必要——保留 `backends` 作为
> 类型 + 注册表所在包，另建 `backends/all`（生成）专门负责 import 全部子包即可，
> 环同样被打破，而且少一个包、少一层重命名。成环约束写在 `all.go` 的文件注释里。

**为什么需要 `backendplugin` 这个新包**：现在子包 import `backends`，所以 `backends` 不能反过来 import 子包（`all.go:13` 的成环说明）。把「类型 + 注册表」下沉到叶子包后，子包只 import 叶子包，`backends/all` 就能 import 全部子包了。

> 名字待定：`backendplugin` 只是占位，也可以叫 `internal/ai/plugin`。**不要**叫 `core` —— 仓库里 `docs/spec/core/` 是文档目录，容易混。

---

## 4. 分五个阶段

每个阶段单独可编译、可合并、可回滚。**Phase 1 和 2 是纯机械改动，零行为变化**，建议先落地拿到收益；Phase 3/4 才是大工程。

### Phase 1｜注册收编（一个 `init()`）

**做法**：把 4 个注册入口合并成 `backends.Register` 一次调用，`Register` 内部再转发给 `ai` / `model`。

```go
// backends/plugin.go 新增字段
type BackendPlugin struct {
    ID     string
    Spec   model.BackendSpec
    CLI    *CLIPlugin          // 已有字段，接上
    Custom *CustomPlugin       // 已有字段，接上
    ACP    *ACPPlugin

    ModelSource      model.ModelSource    // 新增：模型发现
    ListSessions     ai.ListSessionsFromDiskFn // 新增：磁盘会话扫描
    ProviderEnv      *model.ProviderEnvSpec    // 新增字段：BYOK（类型已存在）
    MCPUnsupported   bool                       // 新增字段：MCP 黑名单
    ProviderConfig   *model.ProviderConfigSpec  // 新增字段 + 新类型（Phase 5 才消费）
    MidTurn          ai.MidTurnInjector
    MidTurnSplitsReply bool
}
```

```go
// backends/registry.go 的 Register 末尾
func Register(p *BackendPlugin) {
    // …原有去重与存表…

    switch {
    case p.Custom != nil:
        ai.RegisterBackend(p.ID, p.Custom.NewBackend)
    case p.CLI != nil:
        ai.RegisterBackend(p.ID, func() ai.AIBackend { return p.CLI.NewBackend() })
    }
    if p.ModelSource != nil {
        model.RegisterModelSource(p.ModelSource)
    }
    if p.ListSessions != nil {
        ai.ListSessionsFromDiskRegister(p.ID, p.ListSessions)
    }
    if p.ProviderEnv != nil {
        model.RegisterProviderEnvSpec(p.ID, *p.ProviderEnv)
    }
    if p.MCPUnsupported {
        model.RegisterMCPUnsupported(p.ID)
    }
}
```

**改动面**：20 个子包各改 1–3 个文件（把 `cli.go` / `discovery.go` / `list_sessions.go` 的 `init()` 合并进 `register.go`），加上 `ai/factory.go` 的 `RegisterBackend` 降级为内部函数（不再被外部调用，但保留给 `Register` 用）。

**风险**：`ai.RegisterBackend` 重复注册会 panic —— 合并时必须同时删掉旧的调用，不能只加不减。用 `TestRegistry_RegisterDuplicatePanics` 的思路加一条「每个后端恰好注册一次」的守卫。

**收益**：一个后端只有一次 `init()`；`plugin.CLI` / `plugin.Custom` 从死字段变成真字段；P1、P2 解决。

---

### Phase 2｜自动发现（干掉 `main.go` 的 20 行）

**做法**：

1. 新建 `internal/ai/backends/all/all.go`，内容是 20 行空导入，文件头写 `// Code generated by go generate; DO NOT EDIT.`
2. 新建 `internal/cmd/genbackendimports/main.go`：`os.ReadDir("internal/ai/backends")`，跳过 `all`，按目录名生成空导入。
3. 在 `backends/all` 里加 `//go:generate go run ../../../cmd/genbackendimports`。
4. 新增守卫测试 `all_test.go`：对比目录列表与生成文件内容，不一致就红，提示「跑 go generate」。
5. `cmd/server/main.go:29-48` 的 20 行空导入 → 1 行 `_ "clawbench/internal/ai/backends/all"`。
6. 5 个重复写了空导入块的测试文件（`acp_register_test.go`、`provider_env_coverage_test.go`、`native_skills_test.go`、`acp_integration_test.go`、`model/discovery_test.go`）全部改成 import `backends/all`。

**为什么不能彻底零手工**：Go 没有运行期包发现。能做到的上限是「生成文件 + 守卫测试告诉你忘了跑」，这样漏掉时会**在测试里报错**，而不是运行时静默少一个后端。

**收益**：`main.go` 再也不用改；5 个测试文件的重复导入消失。

---

### Phase 3｜行为钩子 + 搬 ACP 解析（最大的一块）

**做法**：定义 `ACPBehavior` 接口，默认实现放在 `ai`，后端只覆盖自己需要的方法。

```go
// backendplugin/plugin.go（类型层）
type ACPBehavior interface {
    ParseToolCall(tc acp.SessionUpdateToolCall) *ai.ToolCall
    ParseToolCallUpdate(tcu acp.SessionToolCallUpdate) *ai.ToolCall
    ExtractMeta(meta map[string]any) *ai.MetaExtraction
    ParentToolCallID(meta map[string]any) string
    // 可选钩子，默认全空
    OnToolCall(conn *ai.ACPConn, tc acp.SessionUpdateToolCall)
    OnToolCallUpdate(conn *ai.ACPConn, ch chan<- ai.StreamEvent, tcu acp.SessionToolCallUpdate)
    OnSessionInfoUpdate(conn *ai.ACPConn, ch chan<- ai.StreamEvent, meta map[string]any)
    IsChildSessionUpdate(meta map[string]any) bool
}

type BaseACPBehavior struct{}   // 通用实现，后端嵌入它
```

**核心包要改的 3 处**：`acp_tool.go` 的两个 `switch` → `behavior.ParseToolCall`；`acp_meta.go:181` → `behavior.ExtractMeta`；`acp_parent_link.go:92` → `behavior.ParentToolCallID`。`acp_events.go` 的三处 `if` → 三个可选钩子。

**要导出的符号**（一次性，用薄包装避免改 200 个调用点）：

```go
// internal/ai/export.go（新增）
func EmitStreamEvent(ch chan<- StreamEvent, source string, ev StreamEvent) { emitStreamEvent(ch, source, ev) }
func TryEmitStreamEvent(...) bool  { … }
func MetaString(v any) string      { return metaString(v) }
func MetaInt(v any) int            { return metaInt(v) }
func MetaFloat(v any) float64      { return metaFloat(v) }
func ExtractToolNameForBackend(...) string { … }
const StreamChanSize = streamChanSize
```

`MetaExtraction` 类型改为导出（约 15 个字段，字段本来就有注释，纯改名）。

**要搬的文件**（约 2230 行 + 对应测试）：

| 源（`internal/ai/`） | 行数 | 去向 | 备注 |
|---|---|---|---|
| `acp_claude_tool.go` | 63 | `backends/claude/acp_tool.go` | qoder / qodercn 复用，改为 import claude |
| `acp_codebuddy_tool.go` | 60 | `backends/codebuddy/` | |
| `acp_codex_tool.go` | 153 | `backends/codex/` | |
| `acp_kimi_tool.go` | 75 | `backends/kimi/` | |
| `acp_opencode_tool.go` | 41 | `backends/opencode/` | mimo 复用 |
| `acp_meta_claude.go` | 152 | `backends/claude/meta.go` | codex 复用 |
| `acp_meta_codebuddy.go` | 283 | `backends/codebuddy/meta.go` | |
| `acp_opencode_children.go` | 143 | `backends/opencode/children.go` | |
| `opencode_plan_bridge.go` | 164 | `backends/opencode/plan.go` | |
| `codebuddy_team_bridge.go` | 281 | `backends/codebuddy/team.go` | |
| `codebuddy_plan_bridge.go` | 172 | `backends/codebuddy/plan.go` | |
| `codebuddy_command_merge.go` | 46 | `backends/codebuddy/command_merge.go` | |
| `codebuddy_plugin_scanner.go` | 155 | `backends/codebuddy/plugin_scan.go` | 改为 `Spec.ExtraCommands` 钩子 |
| `pi_tool.go`（ACP 部分） | 252 | `backends/pi/` | 与 Phase 4 一起搬更省事 |
| `openclaw_tool.go` | 196 | `backends/openclaw/` | |

**做法建议**：不要一次搬完。先搬 **codebuddy**（钩子用得最全，能验证接口是否够用），再搬 opencode（子会话过滤 + 计划桥接），最后批量搬剩下的纯解析器。每搬一个后端，`switch` 里就少一个 `case`。

**风险**：
- `extractClaudeMeta` 被 codex 复用 → codex 子包 import `backends/claude`。这是**有意的**（两者同一协议家族），要在注释里写明，否则以后有人以为是复制粘贴。
- 同理 mimo → opencode，qodercn → qoder/claude。
- `forwardACPEvent` 是团队桥接用的，它绑定了 ACP 通知 goroutine 的「不能阻塞」语义。搬出去时要么导出，要么让钩子只返回 `[]StreamEvent` 由核心包转发。**推荐后者**——把「能不能阻塞」的决策留在核心包，子包只负责产生事件。

---

### Phase 4｜搬 CLI 解析器 + 两个自定义后端

| 源（`internal/ai/`） | 行数 | 去向 |
|---|---|---|
| `claude_tool.go` | 280 | `backends/claude/cli_tool.go` |
| `codex.go` + `codex_stream.go` + `codex_tool.go` + `codex_think.go` | 9+634+62+47 | `backends/codex/` |
| `deepseek_stream.go` + `deepseek_tool.go` | 125+56 | `backends/deepseek/` |
| `grok_stream.go` | 175 | `backends/grok/` |
| `opencode_stream.go` + `opencode_tool.go` | 542+61 | `backends/opencode/` |
| `pi_stream.go` + `pi_tool.go`（CLI 部分） | 213+252 | `backends/pi/` |
| `vecli.go` + `vecli_stream.go` | 141+118 | `backends/vecli/` |

`CodexBackend` / `VeCLIBackend` 整体搬走，子包的 `custom.go` 直接从壳工厂变成真实现（`p.Custom.NewBackend` 指向本包的构造函数）。

**这一步做完，核心包 `internal/ai/` 里不再有以具体后端命名的文件。** 可以用一条守卫测试钉住：`internal/ai/*.go` 的文件名不得匹配后端 id 列表。

---

### Phase 5｜数据搬家 + 清理（已完成）

| 项 | 改造前 | 现在 |
|---|---|---|
| `providerEnvTable` | `model/provider_env.go` 一张 20 行大表 | 每个后端在自己的注册文件里写 `ProviderEnv:`；`model` 提供 `RegisterProviderEnvSpec` |
| `mcpUnsupportedBackends` | `model/mcp.go` | `Spec.MCPUnsupported = true`（openclaw 那段长注释跟着搬过去） |
| `userConfigDir` / `applyProviderConfigDir` 的 switch | `service/provider_config_dir.go` | **本分支已不存在**：配置目录写入只剩 codex 一条路径，没有按后端分派的 switch |
| 静态模型目录 `model/catalogs.go` | 一个 127 行的中心文件 | 搬进各自子包的 `catalog.go`（没有塞进 `discovery.go`：两者生命周期不同，目录是数据、发现是逻辑） |
| 守卫表 | 4 张手工清单 | 后端总数 / BYOK 覆盖 / i18n 白名单均已派生；`native_skills_test` 保留（它编码的是人拍板的结论） |
| 死代码 | `plugin.CLI` / `plugin.Custom` 无人用 | 已合并成 `CLI` 一个真字段；grok 里遗留的空 `init()` 已删 |

**`catalogs.go` 搬走的代价**：以前「所有内置模型目录在一个文件里，一眼能看全」。现在
的可发现性靠一行命令：`grep -rl "Catalog = \[\]model.AgentModel" internal/ai/backends/`
（`docs/spec/core/ai-backend.md` 里写了这条）。

---

## 5. 全部改动清单（按阶段汇总）

| 阶段 | 新增 | 修改 | 删除 |
|---|---|---|---|
| 1 | `backends/plugin.go` 新字段；`model.RegisterProviderEnvSpec` / `RegisterMCPUnsupported` | 20 个子包的注册文件；`ai/factory.go`；`backends/registry.go` | 13 处 `ai.RegisterBackend` 调用、14 处 `model.RegisterModelSource` 调用、2 处 `ListSessionsFromDiskRegister` 调用 |
| 2 | `backends/all/all.go`（生成）；`cmd/genbackendimports/main.go`；`all_test.go` | `cmd/server/main.go`；5 个测试文件的导入块 | 5 处重复空导入 |
| 3 | `backendplugin/`（或 `ai/plugin/`）整包；`ai/export.go`；`ai/acp_behavior.go` | `acp_tool.go` / `acp_meta.go` / `acp_parent_link.go` / `acp_events.go` / `acp_conn_lifecycle.go` | 15 个文件搬走（约 2230 行 + 测试） |
| 4 | 各子包的解析器文件 | 各子包的 `custom.go` / `cli.go` | 13 个文件搬走（约 2470 行 + 测试） |
| 5 | `model.RegisterProviderConfigWriter`；`docs/spec/core/backend-catalogs.md` | `provider_env.go` / `mcp.go` / `provider_config_dir.go` / `catalogs.go` / 4 张守卫表 | `model/catalogs.go` 的中心表（内容搬进子包） |

---

## 6. 风险与对策

| 风险 | 后果 | 对策 |
|---|---|---|
| 拆 `backendplugin` 时把 `ai` 的依赖带进去 | 新叶子包反过来依赖 `ai` → 又成环 | 叶子包只允许 import `model` + 标准库 + acp SDK；加一条 `go list` 守卫测试 |
| `ai.RegisterBackend` 双注册 panic | 启动即崩 | Phase 1 必须「加一处、删一处」成对改；每个后端加一条「恰好注册一次」断言 |
| 搬解析器时导出面失控 | 核心包变成一堆 `XxxForBackend` | 只导出 Phase 3 列的那 10 个通用 helper + `MetaExtraction`；新增导出必须写清「为什么子包需要它」 |
| 搬测试文件 | 覆盖率按包统计，搬家后新旧包都可能掉到 80% 以下 | 测试跟着代码走（同包），并检查 `./scripts/check-go-coverage.sh`；参考既有教训：抽包必须同步建新包的测试 |
| `backends/all` 生成物与目录不同步 | 新后端静默不注册 | 守卫测试对比目录与生成文件，不一致直接红并打印 `go generate` 命令 |
| 一次搬太多后端 | review 面巨大、回归难定位 | 每个后端一个 PR；先搬 codebuddy 验证钩子设计，再批量 |
| `qoder/qodercn → claude`、`mimo → opencode` 的跨子包 import | 有人误以为是复制粘贴 | 在 import 处写一行注释说明「同一协议家族，共用解析器」 |

---

## 7. 验收结果（2026-11-16）

1. **新增一个假后端**（`backends/fakeacp`，只有 spec + `AcpCommand` + 空 `ProviderEnv`）：
   - ✅ 只新建 1 个目录、跑 1 次 `go generate`（共 2 个文件）
   - ✅ `go build ./...` 通过
   - ✅ 两条守卫点名要求补声明（技能审阅表、文档计数），其余全绿
   - ✅ `/api/backends` 会自动带上它（列表来自 `BackendRegistry`，不需要改 handler）
   - 实测完已删除，`all.go` 回到 20 个后端
2. `internal/ai/*.go` 里不再有以具体后端命名的文件 —— **未完全达成，剩 3 个**，理由见 0.4
   （`acp_opencode_children.go` / `acp_meta_codebuddy.go` / `opencode_error_types.go`）。
   已加守卫 `TestCorePackageHasNoBackendSpecificFiles`：白名单只允许这 3 个文件名，
   新增的必须搬进子包（变异验证：临时放一个 `codex_probe.go` 会被点名）。
3. ✅ 一个后端只有一次 `init()`（20 个子包实测全为 1）。
4. ✅ `cmd/server/main.go` 只有一行后端 import（`_ "clawbench/internal/ai/backends/all"`）。
5. ✅ `go test ./...` 只剩两组环境性失败（见 0.4），其余全绿。

---

## 8. 工作量（按文件/行数，不按时间）

| 阶段 | 机械改动量 | 需要判断的地方 |
|---|---|---|
| 1 | 20 个子包 + 3 个核心文件 | 哪些字段该进 `BackendPlugin`（只进「后端自己知道的事实」） |
| 2 | 1 个生成器 + 7 个文件 | 生成器要不要用 `go/packages`（我倾向 `os.ReadDir` 就够） |
| 3 | 15 个文件 / 2230 行 + ≈2900 行测试 | `ACPBehavior` 的方法集是否够用（先搬 codebuddy 验证）；`forwardACPEvent` 的归属 |
| 4 | 13 个文件 / 2470 行 + ≈5200 行测试 | `CodexBackend` / `VeCLIBackend` 搬走后的导出边界 |
| 5 | 4 张表 + 1 个中心文件 | 配置写入函数搬进子包后，`service` 如何调用（`model` 注册表） |

---

## 9. 明确不做

- 不做运行期 `plugin` 动态加载。
- 不把 `BackendSpec` 拆成十几个小接口（现在一个结构体已经够用，拆了反而更难读）。
- 不为「前端图标」再开一套插件机制（图标有首字母兜底，不值得）。
- 不重构 ACP 连接池 / 事件循环（那是另一条线，风险独立）。
- 不合并 Qoder 两个发行版。
