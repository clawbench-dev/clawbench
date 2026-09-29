# Pi Coding Agent SDK 能力探针结果

> 调研日期：2026-09-28 ｜ SDK 版本：`@earendil-works/pi-coding-agent@0.87.1` ｜ CLI：`pi@0.84.1`
>
> 探针：`test/pi-sdk/pi_sdk_probe.mjs`，由 `internal/ai/pi_sdk_integration_test.go` 驱动。
> 运行：`cd test/pi-sdk && npm install` 然后
> `go test -v -run 'TestIntegration_PiSDK' -tags integration -timeout 1800s ./internal/ai/`

## 结论摘要

**SDK 覆盖面完整：45 个能力点全部通过，无缺口、无跳过。**

十条接入前提（硬断言）**全部成立**：能启动、文本增量流式、`session_id` 可捕获、`cwd` 生效、`continueRecent` 保留上下文、按 id 恢复、内建工具可执行、`toolcall_end` 事件完整、token 用量可读、`abort()` 能中断。

与 CodeBuddy SDK 探针的对比要点：**CodeBuddy SDK 最致命的缺口（无 mid-turn steer）在 Pi SDK 上不存在** —— `session.steer()` 实测可用；**CodeBuddy SDK 无 cost 字段，Pi SDK 直接给真实 USD**（`usage.cost.total`）。

## 一、关键架构事实（实测所得，非文档）

| 事实 | 证据 |
|---|---|
| SDK 是**真正的 in-process 嵌入**，不是子进程包装 | `createAgentSession()` 直接在当前 Node 进程内构建 `ModelRuntime` + `SettingsManager` + `SessionManager` + `ResourceLoader`，返回 `AgentSession`；事件走 `session.subscribe()`。与 CodeBuddy SDK（`spawn` 整个 CLI）形成对比 |
| SDK **不 spawn `pi` 二进制** | 认证读 `~/.pi/agent/auth.json`，与 PATH 上的 `pi` CLI 无关。故可用性门禁检查的是**凭据**而非 CLI |
| 事件模型与 `pi --mode json` 同源 | `AgentSessionEvent` 即 JSON/RPC 模式的事件形状（`docs/json.md` 为权威参考）；SDK 版额外带累积 `message` 快照 |
| 传输面自带 | `message_update`（含 `text_/thinking_/toolcall_` 六种 delta）、`tool_execution_*`、`agent_end`/`agent_settled`、`compaction_*`、`auto_retry_*`、`queue_update` 等 |

**含义**：本探针调研时 ClawBench 的 Pi 通道只有 CLI 行解析（`pi -p --mode json`，`backends/pi/cli.go`）。**该结论之后已变化**：Pi 现已接入 ACP（经 `pi-acp` 桥接，见 `backends/pi/cli.go`），故 SDK 是**第三条路径**，且是三者中唯一 in-process 的。本探针针对的是 SDK 而非 ACP，结论不受影响。

## 二、能力点实测结果（45/45 通过）

| 组 | 覆盖 |
|---|---|
| basic | `createAgentSession()`、`text_delta` 流式、`sessionId`、`cwd` 生效（实测 bash 读到了指定目录下的标记文件）、thinking 事件（`thinking_start/delta/end`）、模型解析 |
| session | `SessionManager.create` / `continueRecent` / `open(findById)` / `list` / `forkFrom` / `inMemory`，上下文跨恢复保留（实测记住了 4242 / 555 / 777） |
| streaming | `toolcall_start/delta/end`（end 带完整 `ToolCall`）、`tool_execution_start/update/end`、`usage`（input/output/cacheRead/cacheWrite）、`cost.total`（真实 USD）、`stopReason`、`turn_start/end` + `agent_end` + `agent_settled` |
| 中断控制 | `abort()` 实测 7-8ms 内完成且 `prompt()` **resolve 而非 reject**；`steer()` 中途注入成功（末条文本变为 STEERED）；`followUp()` 排队执行 |
| tools | 4 个默认内建（`read/bash/edit/write`）、8 个全量（含 `powershell/grep/find/ls`）、`tools` 白名单、`excludeTools` 黑名单、`defineTool` + `customTools` 进程内自定义工具、extension `tool_call` 钩子、`{block:true}` 拦截工具（实测 bash 以 `isError:true` + reason 回报） |
| model | 5 档 thinking（`off/minimal/low/medium/high`）、`setThinkingLevel`、`ModelRuntime.getAvailable()`（实测 49 个模型）、`setModel()` 实时切换、system prompt append / override、context files override |
| resources | `loadSkillsFromDir`、`PromptTemplate` 注册、`getSessionStats()`（含 token/cost/contextUsage）、`getContextUsage()`、`exportToJsonl`、`exportToHtml`、`compact()`（需 >20k 消息 token 才有切点） |
| robustness | 多会话隔离（B 会话不知道 A 的秘密）、`dispose()` 幂等、流式中第二次 `prompt()` **明确拒绝**并提示须选 `steer`/`followUp` |

### 与 ClawBench ACP 依赖面的逐条对照

探针的 gap report 会把下列每项映射到一个测试点。实测**全部 COVERED**，无 NO EQUIV：

| ClawBench ACP 依赖 | SDK 等价物 |
|---|---|
| 新建会话 + id 捕获 | `session.sessionId`（无独立握手） |
| resume / load session | `SessionManager.continueRecent` / `open(findById)` |
| 流式内容增量 | `message_update` → `assistantMessageEvent.text_delta` |
| 扩展思考 | `thinking_*` 事件 + `setThinkingLevel` |
| 工具调用流式 | `toolcall_end` 带完整 `ToolCall` |
| 工具执行生命周期 | `tool_execution_start/update/end` |
| token 用量 | `message_end.message.usage` |
| 成本（USD） | `usage.cost.total`（**真实 USD，非 credits**） |
| 取消 / 中断 | `session.abort()` + `waitForIdle()` |
| **mid-turn 注入（ACP session/steer）** | **`session.steer()`** ← CodeBuddy SDK 缺此能力 |
| 权限审批往返 | extension `tool_call` 返回 `{block,reason}` |
| 模型列举 / 切换 | `ModelRuntime.getAvailable()` + `session.setModel()` |
| system prompt 注入 | `DefaultResourceLoader` 的 `systemPrompt` / `appendSystemPrompt` override |
| 进程内自定义工具 | `defineTool` + `customTools` |
| skills 面 | `loadSkillsFromDir` / loader skill paths |
| 斜杠命令 / prompt 模板 | `registerCommand`（实测 `/probe-command` 被派发且 handler 执行 1 次）+ `PromptTemplate` |
| 压缩 | `session.compact()`（自动 threshold/overflow 同样支持） |

## 三、踩坑记录（接入时须注意）

1. **compaction 有最小体量门槛**。`compact()` 对会话太小会抛 `Nothing to compact (session too small)`（`agent-session.js:1886`）。切点搜索从最新往回累加**消息** token 直到 `keepRecentTokens`（默认 20000），所以必须先堆够 >20k token 的消息内容。探针用 6 轮长文本复述构建（实测 tokensBefore≈38k 才成功）。注意 `getSessionStats().tokens.total` 含 cacheRead，会显著大于消息体量，**不能用它判断是否够**。
2. **`exportToHtml()` 不能用于 in-memory 会话**（`Cannot export in-memory session to HTML`）。须用 `SessionManager.create(...)` 的持久化会话。
3. **`typebox` 必须显式安装**。SDK 自己依赖 typebox 但**嵌套在包内**，`import { Type } from 'typebox'` 从探针目录解析不到。`package.json` 已按 SDK 的 `overrides` 锁 `typebox@1.3.27`（与 codebuddy 探针锁 `zod` 同理）。
4. **`abort()` 语义是 resolve 而非 reject**。`await session.prompt(...)` 在 abort 后正常返回，靠 `message_end.stopReason` / 事件流判断是否被中断，不能靠异常。
5. **流式中第二次 `prompt()` 直接抛错**（要求显式 `streamingBehavior`）。这与 ClawBench 排队消息的语义需要显式映射到 `steer()` 或 `followUp()`。

## 四、与现有 Pi CLI 通道的关系

| 维度 | 现有 CLI 通道 | SDK 通道 |
|---|---|---|
| 形态 | `pi -p --mode json` 子进程 + 行解析 | in-process 嵌入 |
| 事件源 | stdout JSONL（`PiStreamParser`） | `session.subscribe()` |
| 会话恢复 | `--session <id>` / `--continue`（`external_session_id` 落库） | `SessionManager.open/continueRecent`（SDK 直接管会话文件） |
| 认证 | Pi CLI 自己的配置 | `~/.pi/agent/auth.json`（同一份） |
| 依赖 | PATH 上的 `pi` 二进制 | npm 包（实测 436MB `node_modules`） |

**注意**：SDK 是 Node 库，Go 后端要用它必须引入 Node sidecar 进程 —— 那本质上是**换一种子进程协议**（从 JSONL stdout 换成 SDK 自定义的 NDJSON/RPC），并不是"消灭子进程"。这一点与 CodeBuddy SDK 探针的结论相同。

## 五、建议

**SDK 在能力面完全够用，但是否接入是工程取舍而非能力问题。**

1. **能力上无阻塞**：45/45 通过，ACP 依赖面逐条 COVERED，包括 CodeBuddy SDK 缺失的 steer 与 cost。
2. **收益**：SDK 独有的是进程内自定义工具、extension 钩子（可拦截/改写工具调用）、`steer`/`followUp` 的原生队列、以及无需解析 stdout 的结构化事件。
3. **代价**：须新增 Node sidecar（436MB 依赖）+ 一套 `StreamEvent` 映射 + 会话存储从 `external_session_id` 迁到 SDK 的 `SessionManager`。
4. **风险低**：探针运行期间**未观测到任何异步回调崩溃**（`sdk_async_crashes` 为空），不像 CodeBuddy SDK 有会杀掉宿主进程的未捕获异常缺陷。

**若现有 CLI 行解析已稳定**，接入 SDK 的边际收益主要在工具钩子与 steer 队列；**若要做 steer/审批类交互**，SDK 是比现有 CLI 通道更直接的路径。

## 相关

- 探针：`test/pi-sdk/pi_sdk_probe.mjs`
- Go 测试：`internal/ai/pi_sdk_integration_test.go`
- 现有 Pi CLI 通道：`internal/ai/backends/pi/cli.go`、`internal/ai/pi_stream.go`、`internal/ai/pi_tool.go`
- Pi 工具 Schema 参考：`docs/dev/pi_tool_definitions.md`
- 同类探针（CodeBuddy）：`docs/dev/codebuddy_sdk_probe_results.md`
