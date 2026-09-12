# Claude ACP 扩展面 — 与 CodeBuddy 对比

本文档回答"Claude 是否也支持 CodeBuddy 那套 ACP 非标准扩展"。结论：**不支持，而且两者的架构根本不同**——CodeBuddy 是"CLI 自己实现 ACP"，Claude 是"官方 ACP 适配器桥接 Claude Agent SDK"。

> **分析对象**
> - Claude：`@agentclientprotocol/claude-agent-acp` **v0.37.0**（`npx @agentclientprotocol/claude-agent-acp`，ClawBench `internal/ai/backends/claude/cli.go:19` 用的就是它），bundled `@agentclientprotocol/sdk` v0.22.1。
> - CodeBuddy：`@tencent-ai/codebuddy-code` v2.147.0（对比基线，见 [codebuddy_acp_extensions.md](codebuddy_acp_extensions.md)）。
>
> **方法**：该适配器**未混淆**且带 `.d.ts`，可直接读 `dist/acp-agent.js`（120KB，源码级可读）。

## 0. 结论速览

| 维度 | CodeBuddy | Claude（claude-agent-acp） |
|------|-----------|---------------------------|
| 架构 | CLI 进程内**自己实现** ACP server | **适配器**桥接 `@anthropic-ai/claude-agent-sdk` |
| `extMethod` 兜底 | ✅ 有（37 个 case 的大 switch） | ❌ **完全没有**（`ClaudeAcpAgent` 无此方法） |
| `session/steer` | ✅ 可用（实测 0.02s） | ❌ 不存在 |
| 消息队列族（`session/*_queue`） | ✅ 19 个私有方法 | ❌ 不存在（但有**内建 prompt 排队**，见 §3） |
| `session/inject_history` | ✅ | ❌ |
| `session/set_multitask` / `set_agent` | ✅ | ❌ |
| `session/set_model` | ✅ 私有方法 | ⚠️ `unstable_setSessionModel`（**标准 unstable**） |
| `_codebuddy.ai/*` 扩展 | 17 个 | ❌ 无（只有 `_claude/sdkMessage` 一个出站通知） |
| 私有 `_meta` key | 163 个 | 极少（`claudeCode` / `_claude/origin` / `terminal_*`，见 §4） |
| 能力协商（`clientCapabilities._meta`） | `codebuddy.ai` 命名空间 | `terminal-auth` / `terminal_output` / `auth._meta.gateway` |
| `agentCapabilities` 非标准字段 | `delegateToolsSupport` 等 | `_meta.claudeCode.promptQueueing` |
| `sessionCapabilities` | ❌ 不广告 | ✅ 完整广告（list/delete/fork/close/resume/additionalDirectories） |

**一句话**：CodeBuddy 的扩展面是**私有且庞大**的（21+ 私有方法）；Claude 的扩展面**极小**，几乎完全遵循 ACP 规范，只通过 `_meta` 暴露少量桥接信息。

---

## 1. 架构差异（根因）

### CodeBuddy：CLI 自己当 ACP server

```
ClawBench ──stdio JSON-RPC──> codebuddy --acp（进程内 ACP server + 自带 extMethod 大 switch）
```

非标准方法能被识别，是因为 CodeBuddy 自己写了 `extMethod(method, params)` 兜底分支。ACP SDK 的标准 switch 不认识的方法名全部转发进去。

### Claude：官方适配器桥接 SDK

```js
// dist/acp-agent.js:1-4
import { AgentSideConnection, ndJsonStream, RequestError } from "@agentclientprotocol/sdk";
import { deleteSession, getSessionMessages, listSessions, query } from "@anthropic-ai/claude-agent-sdk";
```

```
ClawBench ──stdio JSON-RPC──> claude-agent-acp（薄适配器）
                                    │  query() / AsyncGenerator
                                    ▼
                             @anthropic-ai/claude-agent-sdk ──> claude CLI 子进程
```

适配器只做**协议翻译**：ACP 方法 → SDK `query()` 调用 / 流消息 → ACP `session/update`。它**没有** `extMethod`，所以任何非标准方法（含 `_` 前缀扩展请求）都会被 SDK 回 `method-not-found`：

```js
// sdk/dist/acp.js:179-184（agent 侧请求分发 default 分支）
default:
    if (agent.extMethod) {
        return agent.extMethod(method, params);
    }
    throw RequestError.methodNotFound(method);
```

而 `ClaudeAcpAgent`（`dist/acp-agent.js:204`）**没有定义 `extMethod`**（`grep -c extMethod dist/acp-agent.js` = **0**）。它实现的 ACP 协议方法全部是规范方法：

```
initialize, newSession, unstable_forkSession, resumeSession, loadSession,
listSessions, authenticate, prompt, cancel, closeSession,
unstable_deleteSession, unstable_setSessionModel, setSessionMode,
setSessionConfigOption, readTextFile, writeTextFile
```

（另有 `teardownSession` / `dispose` / `createSession` / `getOrCreateSession` / `applySessionMode` / `replaySessionHistory` / `updateConfigOption` / `applyConfigOptionValue` / `sendAvailableCommandsUpdate` 等内部辅助方法，非协议入口。）

> **推论**：Claude 侧**唯一**可用的扩展机制是 `_meta`（规范正式支持），而非扩展方法。

---

## 2. 方法对照

### 2.1 Claude 实现的全部 ACP 方法

| 方法 | 标准？ | 备注 |
|------|--------|------|
| `initialize` | ✅ | 见 §4 能力协商 |
| `session/new` | ✅ | `_meta.claudeCode.options` 可透传 SDK 选项 |
| `session/load` | ✅ | |
| `session/resume` | ✅ | |
| `session/fork` | ⚠️ unstable | `unstable_forkSession`（`resume` + `forkSession:true`） |
| `session/list` | ⚠️ unstable | |
| `session/delete` | ⚠️ unstable | |
| `session/close` | ✅ | |
| `session/prompt` | ✅ | **内建排队**，见 §3 |
| `session/cancel` | ✅ | |
| `session/set_mode` | ✅ | |
| `session/set_config_option` | ✅ | 只接受 `string` value（`typeof params.value !== "string"` 直接报错） |
| `session/set_model` | ⚠️ unstable | `unstable_setSessionModel`，做模型别名解析 |
| `authenticate` | ✅ | 多种 auth method，见 §4 |
| `fs/read_text_file` / `fs/write_text_file` | ✅ | client 侧回调 |
| `terminal/*` | ✅ | 经 client 回调 |

**没有**：`session/steer`、`session/inject_history`、任何队列族、`session/set_multitask`、`session/set_agent`。

### 2.2 CodeBuddy 有而 Claude 没有的

CodeBuddy 的 17 个 `_codebuddy.ai/*` + 19 个私有 `session/*`，Claude **全部没有**。其中对 ClawBench 最有价值的几个：

| CodeBuddy 能力 | Claude 替代方案 |
|----------------|----------------|
| `session/steer`（运行中插话） | ❌ 无直接等价；见 §3 的排队机制 |
| `session/inject_history` | ❌ 无 |
| 消息队列族 | ⚠️ 有内建排队（`promptQueueing`），但语义不同 |
| `session/set_multitask` | ❌ 无 |
| `_codebuddy.ai/question` | ❌ 无（Claude 走标准 `canUseTool` / permission 流程） |
| 文件回滚（`rollbackFiles`） | ❌ 无（Claude 有 `forkSession` 但不回滚工作区文件） |

---

## 3. Claude 的"替代能力"：内建 prompt 排队

虽然 Claude 没有 `session/steer`，但它有**协议层内建的排队**，这是 ClawBench 值得注意的差异。

**广告方式**（`dist/acp-agent.js:311-316`）：

```js
agentCapabilities: {
  _meta: { claudeCode: { promptQueueing: true } },   // ← 非标准能力位
  ...
}
```

**语义**（`dist/acp-agent.js:444-456`）：在 `session/prompt` 尚未返回时**再次调用 `session/prompt`**，不会报错，而是把消息推进 `session.input` 并挂起等待：

```js
if (session.promptRunning) {
    session.input.push(userMessage);
    const order = session.nextPendingOrder++;
    const cancelled = await new Promise((resolve) => {
        session.pendingMessages.set(promptUuid, { resolve, order });
    });
    if (cancelled) return { stopReason: "cancelled" };
}
```

当前一轮在流里回放（replay）到这条消息时，`handedOff = true`，排队的那次 `prompt()` 调用被唤醒并**接管**后续处理（`dist/acp-agent.js:766-775`）。

**与 CodeBuddy `steer` 的关键差异**：

| | CodeBuddy `session/steer` | Claude `promptQueueing` |
|---|---|---|
| 接口 | 私有方法 | **标准 `session/prompt`** |
| 注入时机 | **下一个模型请求前**（本轮内） | **本轮结束后**（下一轮） |
| 返回值 | `{steered:true, ownerRequestId}` | 被挂起的 `PromptResponse`（直到轮次切换） |
| 调用方式 | 需裸 JSON-RPC（非 `_` 前缀） | **无需任何改造**，标准 SDK 方法 |
| 客户端要求 | 无 | 需知悉"并发调 prompt 合法" |

> **对 ClawBench 的含义**：Claude 不需要裸 JSON-RPC 通道——并发 `Prompt()` 是 agent 侧支持的。但**ClawBench 自身目前不支持并发 prompt**，这是接入的真正障碍：
>
> - `ACPConn.Prompt` 会 `client.RegisterSession(acpSID, streamCh)`（`acp_conn_prompt.go:71`），而 `RegisterSession` 是**覆盖写**（`acp_client.go:74`：`c.sessionRoutes[acpSessionID] = ch`）
> - 同时 `Prompt` 末尾有 `defer client.UnregisterSession(acpSID)`，`UnregisterSession` 是**无条件 delete**（`acp_client.go:91`）
>
> 即：若对同一 ACP session 并发调用两次 `Prompt`，第二个的 `RegisterSession` 会顶掉第一个的 channel，第一个的 `defer UnregisterSession` 又会把第二个的 channel 删掉——两个流的 SessionUpdate 都会路由错乱或丢失。
>
> 要用 Claude 的排队能力，需要先把 ClawBench 的会话路由改成**支持多订阅者**（或为"排队中的 prompt"单独管理 channel 生命周期）。**这是一处真实的基础设施改造**，比"无需改造"要复杂。

---

## 4. Claude 的扩展面（`_meta`）

Claude 的扩展几乎全部通过规范的 `_meta` 字段，数量很少：

### 4.1 client → agent（`clientCapabilities`）

| 位置 | 字段 | 效果 |
|------|------|------|
| `auth._meta.gateway` | `true` | 启用 `gateway` / `gateway-bedrock` 认证方式（自定义 Anthropic 协议网关） |
| `auth.terminal` | `true` | 启用终端登录方式 |
| `_meta["terminal-auth"]` | `true` | 启用带命令详情的终端登录 |
| `_meta["terminal_output"]` | `true` | 启用 Bash 工具的 `_meta.terminal_info` / `terminal_output` 流式终端输出 |

### 4.2 agent → client（出站）

| 位置 | 用途 |
|------|------|
| `extNotification("_claude/sdkMessage", ...)` | **唯一的扩展方法**：把原始 Claude SDK 消息透传给客户端（默认关闭，需 `_meta.claudeCode.emitRawSDKMessages`） |
| `session/update._meta["_claude/origin"]` | usage_update 上标记消息来源 |
| `session/update._meta.claudeCode.toolName` | 工具名（对应 CodeBuddy 的 `codebuddy.ai/toolName`） |
| `session/update._meta.claudeCode.toolResponse` | 工具结构化响应 |
| `session/update._meta.claudeCode.parentToolUseId` | 子智能体父工具调用 ID（对应 CodeBuddy 的 `parentToolCallId`） |
| `_meta.terminal_info` / `terminal_output` | 终端输出（需客户端广告 `terminal_output`） |

### 4.3 `_meta.claudeCode.options`（`session/new` 透传）

Claude 允许客户端通过 `session/new` 的 `_meta.claudeCode.options` 把参数**直接透传给 Claude Agent SDK**：

```js
const sessionMeta = params._meta;                              // :1465
const userProvidedOptions = sessionMeta?.claudeCode?.options;   // :1466
```

以及 `_meta.claudeCode.emitRawSDKMessages`（`true` 或过滤器数组）控制 `_claude/sdkMessage` 的转发粒度。

> 这是一个**很强的扩展点**：理论上能透传 SDK 的 `hooks` / `additionalDirectories` / `env` 等。ClawBench 若要用，需确认 SDK 选项白名单与安全边界。

---

## 5. `sessionUpdate` 类型对照

Claude 发出的 `sessionUpdate`（`grep 'sessionUpdate: "'`）**全部是标准类型**：

```
agent_message_chunk, agent_thought_chunk, available_commands_update,
config_option_update, current_mode_update, plan, tool_call,
tool_call_update, usage_update
```

**没有**任何非标准类型（对比 CodeBuddy 有 7 类非标准：`session_end` / `model_update` / `tool_call_pending` / `tool_call_arguments_*` / `terminal_update` / `terminal_output_chunk`）。

注意 Claude **不发** `session_end`（CodeBuddy 发）——轮次结束通过 `session/prompt` 的返回 `stopReason` 表达。

---

## 6. 对 ClawBench 的含义

### 6.1 接入策略必须按 backend 分支

Claude 和 CodeBuddy 的扩展面**没有交集**，任何接入都必须按 backend 走不同路径：

| 需求 | CodeBuddy 路径 | Claude 路径 |
|------|---------------|------------|
| 运行中插话 | `session/steer`（裸 JSON-RPC） | 并发 `session/prompt`（标准） |
| 读取原始 SDK 消息 | 无 | `_claude/sdkMessage`（需广告 `emitRawSDKMessages`） |
| 终端输出流 | `terminal_output_chunk`（非标准 update） | `_meta.terminal_output`（标准 update + meta） |
| 子智能体归属 | `codebuddy.ai/parentToolCallId`（扁平） | `claudeCode.parentToolUseId`（嵌套） |
| 工具名 | `codebuddy.ai/toolName` | `claudeCode.toolName` |

> ClawBench 现有的 `internal/ai/acp_meta_claude.go` 已经处理了 Claude 的 `_meta.quota.token_count`，但**未**处理 `claudeCode.toolName` / `parentToolUseId` / `_claude/origin` / `terminal_output`。

### 6.2 好消息：Claude 侧协议面不需要裸 JSON-RPC

CodeBuddy 接入 `session/steer` 需要自建裸 JSON-RPC 通道（写锁 + demux）；**Claude 侧不需要**——它的排队能力就是标准 `session/prompt`，用现有 SDK 方法即可调用。

但**代价在 ClawBench 自身**：需要把会话路由从"单 channel 覆盖"改为支持并发（见 §3 末尾的 `RegisterSession`/`UnregisterSession` 覆盖/删除问题）。所以两边的改造量**都不小，只是位置不同**：

| | CodeBuddy | Claude |
|---|---|---|
| 改造位置 | ClawBench ACP 传输层（写锁 + stdout demux） | ClawBench 会话路由（多订阅者） |
| 协议侧 | 私有方法，需裸 JSON-RPC | 标准方法，SDK 直接支持 |
| 语义 | 本轮内注入 | 下一轮排队 |

### 6.3 `sessionCapabilities` 的差异要注意

Claude **完整广告** `sessionCapabilities`（`list`/`delete`/`fork`/`close`/`resume`/`additionalDirectories`），而 CodeBuddy **完全不广告**。ClawBench `acp_conn_lifecycle.go:587-588` 从 `initResp.AgentCapabilities.SessionCapabilities` 读 List/Delete 能力——对 Claude 会正确读到 `true`，对 CodeBuddy 读到 `false`（需 `BackendSpec` 兜底）。这个差异是**已有逻辑的正确行为**，但说明"能力探测"不能跨 backend 假设一致。

---

## 7. 是否值得为 Claude 做探针？

CodeBuddy 的探针（`codebuddy_acp_ext_probe_integration_test.go`）存在的理由是：**私有接口多、bundle 混淆、需要实证**。

Claude 的情况不同：

- 适配器**源码可读**（未混淆 + `.d.ts`），接口即代码，歧义小
- 扩展面小，且都是规范的 `_meta`
- 唯一的"行为性"问题是 **prompt 排队语义**（并发 `prompt()` 的挂起/接管时机），这个值得实测

**建议**：若要验证，只做一个聚焦探针——**并发 `session/prompt` 的排队行为**（验证 `promptQueueing` 语义、确认第二轮何时被唤醒、`stopReason` 如何返回），而不是像 CodeBuddy 那样铺开。

---

## 附录：复核命令

```bash
D=~/.nvm/versions/node/v24.14.0/lib/node_modules/@agentclientprotocol/claude-agent-acp

# 1) 确认 ClaudeAcpAgent 没有 extMethod
grep -n "extMethod" $D/dist/acp-agent.js          # 只应有 extNotification("_claude/sdkMessage")
grep -n "^    async " $D/dist/acp-agent.js        # 列出全部实现的方法

# 2) 能力广告
sed -n '305,345p' $D/dist/acp-agent.js

# 3) 内建排队
sed -n '444,460p' $D/dist/acp-agent.js            # promptRunning 分支
sed -n '760,780p'  $D/dist/acp-agent.js            # handedOff 接管

# 4) 全部 _meta
grep -n "_meta:" -A 4 $D/dist/acp-agent.js

# 5) 全部 sessionUpdate 类型（应全是标准）
grep -n 'sessionUpdate: "' $D/dist/acp-agent.js | sed 's/.*sessionUpdate: "\([a-z_]*\)".*/\1/' | sort -u

# 6) SDK 的 extMethod 兜底（无则 method-not-found）
sed -n '176,186p' $D/node_modules/@agentclientprotocol/sdk/dist/acp.js
```

## 附录：版本对照

| 项 | 值 |
|----|-----|
| Claude 适配器 | `@agentclientprotocol/claude-agent-acp` v0.37.0 |
| bundled ACP SDK | `@agentclientprotocol/sdk` v0.22.1 |
| 桥接目标 | `@anthropic-ai/claude-agent-sdk`（`query()`） |
| ClawBench 侧 ACP SDK | `github.com/coder/acp-go-sdk` v0.13.5 |
| CodeBuddy 对比基线 | `@tencent-ai/codebuddy-code` v2.147.0 |
| ClawBench backend 定义 | `internal/ai/backends/claude/cli.go:19` |
