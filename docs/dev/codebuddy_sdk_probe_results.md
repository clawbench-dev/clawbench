# CodeBuddy Agent SDK 能力探针结果

> 调研日期：2026-09-25 ｜ SDK 版本：`@tencent-ai/agent-sdk@0.3.266` ｜ CLI：`@tencent-ai/codebuddy-code@2.156.0`
>
> 探针：`test/codebuddy-sdk/sdk_probe.mjs`，由 `internal/ai/codebuddy_sdk_integration_test.go` 驱动。
> 运行：`cd test/codebuddy-sdk && npm install` 然后
> `go test -v -run 'TestIntegration_CodeBuddySDK' -tags integration -timeout 2400s ./internal/ai/`

## 结论摘要

**SDK 可以作为第三种 transport，但不能"完全覆盖"现有功能。** 62 个能力点：**57 通过 / 4 缺口 / 1 未验证**，且缺口集中在 ClawBench 当前依赖最重的几个面上。

四条接入前提（硬断言）**全部成立**：能启动、`session_id` 可捕获、`resume` 保留上下文、内建工具可执行。

## 一、关键架构事实（读源码所得，非文档）

这一点决定了一切：

| 事实 | 证据 |
|---|---|
| SDK **不是 ACP** | 启动参数是 `--input-format=stream-json --output-format=stream-json --verbose`，无 `--acp`（`lib/transport/process-transport.js:292-296`） |
| SDK 是**子进程包装**，非 in-process | `spawn(runtime.command, [...runtime.args, cliJsPath, ...args])`（同文件 ~510 行） |
| SDK **自带整个 CLI** | 包内 `cli/bin/codebuddy` + 完整 `cli/dist-server/`（2844 文件 / 49MB tarball） |
| 控制面是**自研协议** | `hook_callback` / `can_use_tool` / `elicitation_create` 三种 subtype（`lib/_internal/query-controller.js:56-68`） |
| `@agentclientprotocol/sdk` 依赖**只用于类型对齐** | 源码注释「形状对齐 ACP ClientCapabilities.elicitation」，无 `ClientSideConnection` 使用 |

**含义**：SDK 与 ClawBench 现有的 `codebuddy --acp` 是**两条平行的子进程通道**，不是"更原生的替代"。换成 SDK 不会更接近 CodeBuddy 内核，只是换了一套控制协议。

## 二、能力点实测结果

### 通过（57）

| 组 | 覆盖 |
|---|---|
| basic | `query()`、消息类型、`session_id`、`cwd` |
| session | `resume` 保留上下文、`forkSession`、`continue`、`persistSession:false` |
| streaming | `includePartialMessages`（130+ `stream_event`）、AsyncIterable 输入、`interrupt()`、`AbortController` |
| permissions | `canUseTool` allow / deny / **改写输入**、plan / bypass 模式、allow/disallow/whitelist 工具 |
| hooks | PreToolUse / PostToolUse / UserPromptSubmit / SessionStart / Stop / **block** 全部触发 |
| tools | 内建 Bash、60 个工具清单、**进程内自定义 MCP 工具**、stdio MCP、`mcp__<server>__<tool>` 命名 |
| subagents | `agents` 定义 + 委派（工具名为 `Agent`，非 `Task`） |
| model | `thinking` 配置（**thinking block 确实存在**）、`effort`、`supportedModels()`、`setModel`、`accountInfo` |
| state | `supportedCommands()`（76 条斜杠命令）、`mcpServerStatus()` |
| metrics | token 用量（含 cache_read / cache_creation） |
| isolation | `systemPrompt` append / override、`additionalDirectories` |
| limits | `maxTurns`（抛 `ExecutionError`）、**图片输入可用** |
| v2 | `createSession` / 多轮 / `resumeSession`（unstable 但可用） |

### 缺口（4）

| 缺口 | 实测证据 | 对 ClawBench 的影响 |
|---|---|---|
| **无 mid-turn 注入** | SDK 只有 streaming input，没有 `session/steer` 等价物（探针未单列测试点，由 API 面判定） | **致命**：`midturn.go` 的 steer 能力无法迁移 |
| **无成本（credits）** | `result.total_cost_usd` 恒为 `0`，且 result 消息**没有 credit 字段** | 用量统计里的费用维度丢失（ACP 侧靠 `costFieldCarriesCredit` 绕过同一底层事实） |
| **`skills` 不下发** | `system:init` 的 key 集合里根本没有 `skills` | 只能继续走磁盘扫描 `~/.codebuddy/skills/` |
| **`plugins` 不下发** | 同上，`plugins` key 不存在 | 只能继续走磁盘扫描 `~/.codebuddy/plugins/cache/` |

另有一个**未验证**项：`resumeSessionAt`（按消息 id 恢复）—— 探针无法稳定观测 message id，标记为 deferred 而非通过。这一项不影响结论，因为 `resume`（按 session id）已验证可用。

`PostToolUse.updatedToolOutput`（压缩工具输出省 token）实测**不生效**：模型看到的仍是原始输出。因该能力当前 ACP 侧也未使用，未计入缺口。

### 三个 API 文档与实现不符之处（已按源码纠正）

1. **`ContentBlock` 有 6 种**，包含 `ThinkingContentBlock` 与 `ImageContentBlock`（文档说 TS 侧没有 thinking，**错**）。实测 thinking block 确实产出。
2. **`createSdkMcpServer` 收 options 对象** `{name, version, tools}`，**不是**位置参数 `(name, {tools})`。
3. **`tool()` 的 `inputSchema` 必须是 Zod shape**，传 `{a:'number'}` 字符串描述会报 `inputSchema must be a Zod schema or raw shape`。

## 三、发现的 SDK 健壮性缺陷

**异步回调里的未捕获异常会杀掉宿主进程。**

```
Error: Transport not started
    at ProcessTransport.writeLine (lib/transport/process-transport.js:845)
    at ProcessTransport.handleMcpMessageRequest (lib/transport/process-transport.js:984)
```

触发场景：使用过进程内 SDK MCP server 的会话结束后，迟到的 MCP 控制消息抵达已关闭的 transport → 从异步回调抛错 → 默认终止 Node 进程。实测在 `subagents` 组把整个探针打断（33/62 点后死亡，见 `probe-final.log`）。

探针现已挂 `uncaughtException` / `unhandledRejection` 兜底并把命中记入 `sdk_async_crashes`，Go 侧单列一段展示。

对 ClawBench 的含义：若接入 SDK，必须自己兜底，否则一次迟到消息就会打掉整个后端进程。

> 注意：`setting_sources_none` / `setting_sources_project` 的"通过"是**观察性通过**——它们记录的事实是「不带 `user` scope 就无法认证」（登录态存在 `~/.codebuddy`，被 SDK 的默认隔离一并排除）。这是文档化的默认行为而非缺陷，故记为通过并在 detail 里写明；接 SDK 时必须显式传 `settingSources: ['user']`。

## 四、改动量评估（若要接入）

| 面 | 规模 |
|---|---|
| 专属 CodeBuddy 生产代码 | ~1,850 LOC |
| 内联 CodeBuddy 条件分支 | ~600-800 LOC（约 25 个共享文件） |
| CodeBuddy 专属测试 | ~7,445 LOC（24 文件） |
| 消费的 `codebuddy.ai/*` meta key | ~25 个 |
| 磁盘耦合 | `~/.codebuddy/{skills,plugins/cache,projects,local_storage,.mcp.json}` |

要新增第三种 transport，需改动：`BackendPlugin`/`BackendSpec`（新增 SDK 字段）、transport 字符串比较点（~10 处裸字符串）、`factory.go` 的 `effectiveTransport`、`PATCH /api/agents` 校验、以及实现 `AIBackend` 的 `StreamEvent` 全词表映射。**且必须保留 ACP 通道**，因为 steer 无替代。

## 五、建议

**不建议为 CodeBuddy 引入 SDK transport。** 理由：

1. **steer 无法迁移** —— 这是 ClawBench 相对其它客户端的差异化能力（`midturn.go` + `acp_raw_rpc.go` 整套机制），SDK 没有等价物。
2. **不是更原生** —— 同样是子进程 + JSON 行协议，只是控制面从 ACP 换成自研协议；换来的是更少的扩展面（skills/plugins 不下发、无 credit）。
3. **收益面窄** —— SDK 独有的是 hooks（可拦截/改写工具输入输出）与进程内自定义工具，而 ClawBench 目前没有这两类需求。
4. **有进程级风险** —— 上述未捕获异常缺陷需要额外兜底。

**若未来确需 hooks 或进程内自定义工具**，更务实的路径是保留 ACP 为主通道，仅用 SDK 做旁路能力补充，而不是整体替换。

## 相关

- 探针：`test/codebuddy-sdk/sdk_probe.mjs`
- Go 测试：`internal/ai/codebuddy_sdk_integration_test.go`
- ACP 侧现有扩展面：`docs/dev/codebuddy_acp_extensions.md`
