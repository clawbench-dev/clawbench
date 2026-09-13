# CodeBuddy ACP 非标准扩展接口清单

本文档整理 `@tencent-ai/codebuddy-code`（`codebuddy --acp`）在 [ACP v1 规范](https://agentclientprotocol.com/protocol/overview) 之外额外实现的接口。ClawBench 通过 `internal/ai/` 下的 `ACPBackend` / `ClawBenchACPClient` 与 CodeBuddy 以 ACP stdio 通信，这些扩展是评估"能否接入 steer / 消息队列 / 回滚 / 多任务"等私有能力的基础。

> **分析对象**：`@tencent-ai/codebuddy-code` **v2.147.0**（本机 `~/.nvm/.../node_modules/@tencent-ai/codebuddy-code`）。
> **方法**：静态分析 `dist/codebuddy.js`（TUI bundle）、`dist/codebuddy-headless.js`（`--acp` 实际加载的 bundle）、`dist-server/8569.codebuddy.js`（内置 ACP SDK）与 `CHANGELOG.md`。
> 本文所有结论均可在上述产物中复核，复核命令见 [附录 A](#附录-a-复核命令)。

## 0. 快速结论

CodeBuddy 在标准 ACP 之上叠了 **四层**非标准扩展：

| 层 | 机制 | 数量 | ClawBench 现状 |
|----|------|------|----------------|
| 1 | `extMethod` 扩展请求（`_` 前缀） | 17 个 | **未接入**（未实现 `ExtensionMethodHandler`） |
| 2 | 非 `_` 前缀的自定义 `session/*` 方法 | 19 个 | **未接入** |
| 3 | agent→client 扩展通知（`ExtensionMethod` 枚举） | 10 个 | **未接入** |
| 4 | 非标准 `sessionUpdate` 类型 + `_meta` 扩展字段 | 7 类 / 163 个 meta key | **部分接入**（仅 usage/trace/agentPhase 等少量 key） |

> `extMethod` 大 switch 共 **37 个 case** = 17 个 `_` 前缀 + 20 个非 `_` 前缀。非 `_` 前缀中 `session/set_config_option` 是**标准方法**（SDK 标准 switch 已处理，`extMethod` 里是冗余兜底），故真正的"自定义 session 方法"为 **19 个**。

ACP 规范为扩展预留了两个正式入口（见 [Extensibility](https://agentclientprotocol.com/protocol/extensibility)）：

- **方法名以 `_` 开头** → 自定义 JSON-RPC request/notification，对端不认识时必须回 `Method not found`（notification 则静默忽略）
- **任意类型的 `_meta` 字段** → 挂载自定义数据，但 **不得在规范类型根层新增字段**

CodeBuddy 的第 1 层合规；第 2、3、4 层则在不同程度上**超出规范**（详见各节）。

---

## 1. `extMethod`：client → agent 扩展请求

CodeBuddy 的 agent 侧把所有非标准请求都收敛到一个 `extMethod(method, params)` 大 switch 中（`dist/codebuddy.js` / `dist/codebuddy-headless.js`）。

这个 switch 是 ACP SDK 标准 `requestHandler` 的 **`default` 兜底分支**：SDK 标准 switch 只认识 19 个规范方法（`initialize`、`session/new`、`session/load`、`session/list`、`session/delete`、`session/fork`、`session/resume`、`session/close`、`session/set_mode`、`session/set_config_option`、`session/prompt`、`authenticate`、`logout`、`providers/*`、`nes/*`），其余全部转发给 `extMethod`。

`extMethod` 内部同时匹配两类方法名：

- **`_codebuddy.ai/*`**（17 个）—— 合规的 ACP 扩展方法
- **`session/*`**（19 个自定义 + 1 个标准冗余）—— 不合规（见 §2）

### 1.1 `_codebuddy.ai/*` 方法清单

| 方法 | 用途 | 参数 | 返回 |
|------|------|------|------|
| `_codebuddy.ai/getUserInfo` | 当前登录用户信息 | `{}` | `{userInfo: {userId, userName, userNickname, enterpriseId, enterpriseName, enterprise, authType, token, accessToken}}`；未登录时 `{userInfo: undefined}` |
| `_codebuddy.ai/resolveInterruption` | **响应工具权限/提问**（收到 `interruption_request` 或 `question` 后回决策） | `{sessionId, toolCallId, decision, answers?}` | `{}`（`decision ∈ allow \| allowAll \| deny \| rejectAndExitPlan`） |
| `_codebuddy.ai/delegateToolsChanged` | 客户端注册的委托工具发生变化 | `{sessionId?, providerId?, changeType: added\|removed, tools?: [{id, name, description, inputSchema, provider, category, requiresApproval}], toolIds?}` | `{}` |
| `_codebuddy.ai/requestSensitiveApproval` | 敏感数据外发审批（fail-close 语义） | `{approvalId, sessionId, detectionId, items: [{plaintext, ruleName?, supportsEncryption?}], timeoutMs?}` | `{choice: allow-original \| ... , reason?}`；参数非法时 `{choice: "deny", reason}` |
| `_codebuddy.ai/cancelSensitiveApproval` | 取消在途敏感审批 | `{approvalId, sessionId}` | `{}` |
| `_codebuddy.ai/refreshPlugins` | 刷新产品与工具（插件变更） | `{reason?}` | `{}`（若变更仍在 pending 则仅记日志） |
| `_codebuddy.ai/reconcilePlugins` | 对账已配置插件 | `{reason?, requiredPluginIds?}` | `{ready, requiredPluginIds, missingPluginIds, errors}` |
| `_codebuddy.ai/getAgents` | 主 Agent 目录（见 §1.3） | `{sessionId?}` | agent 列表；未 opt-in 时抛 `methodNotFound` |
| `_codebuddy.ai/session/rollback` | 会话回滚到指定消息/请求（见 §1.2） | `{sessionId, targetMessageId?, targetRequestId?, forkPointId?, reason?}` | `{applied, actualForkPointId?, error?}` |
| `_codebuddy.ai/session/previewFileRollback` | 文件级回滚**预览** | `{sessionId, targetRequestId}` | 预览结果 |
| `_codebuddy.ai/session/rollbackFiles` | 文件级回滚**执行** | `{sessionId, targetRequestId}` | 回滚结果；失败抛 `-32010` + `{code}` |
| `_codebuddy.ai/session/cancelBackgroundTask` | 终止后台任务（含 `nohup` 派生的孙进程） | `{sessionId, terminalId}` | `{}` |
| `_codebuddy.ai/mcpUiReadResource` | MCP UI 读资源 | `{serverName, uri}` | `{contents}` |
| `_codebuddy.ai/mcpUiCallTool` | MCP UI 调工具（需审批） | `{sessionId, serverName, toolName, arguments}` | 工具结果 |
| `_codebuddy.ai/mcpUiUpdateModelContext` | 写入 MCP UI 模型上下文 | `{sessionId, content?, structuredContent?}` | `{}` |
| `_codebuddy.ai/mcpUiRequestDisplayMode` | 请求切换展示模式 | `{sessionId, serverName, mode?}` | `{mode}` |
| `_codebuddy.ai/mcpUiResourceTeardown` | MCP UI 资源回收 | `{sessionId, serverName}` | `{}` |

> **MCP UI 专属门控**：`extMethod` 入口处有 `method.startsWith("_codebuddy.ai/mcpUi") && !isWebUiClient()` → 直接抛 `methodNotFound`。即所有 `mcpUi*` 方法**仅对 `clientInfo.name === "codebuddy-web-ui"` 的客户端开放**。

### 1.2 `session/rollback` 的三种调用形态

`handleRollback` 根据参数分派（`dist/codebuddy.js`）：

1. **`{forkPointId}`**（无 `targetMessageId` / `targetRequestId`）→ 直接回滚到该 fork point；`reason === "resend_edit"` 时额外生成 `editedUserItemId = "root-rewind-<ts>"` 并写入历史
2. **`{targetMessageId}` 或 `{targetRequestId}`** → 先经 `resolveResendForkPoint` 解析出 fork point（支持用 `userMessageId` 命中，或按 `providerData.conversationRequestId` 命中后再取其 `parentId`），解析失败返回 `{applied: false, error: "Cannot resolve fork point"}`
3. **`reason === "resend_edit"`** → 走"编辑并重发"路径，保留 `editedUserItemId`

### 1.3 `getAgents` / `session/set_agent` 的 opt-in 门控

两者都要求 **客户端在 `initialize` 时声明 `clientCapabilities._meta["codebuddy.ai"].mainAgentSupport === true`**，且服务端 gate 放行：

```js
// 失败时的日志（含 gate 诊断信息）
"[ACP Agent] getAgents refused: optIn=${mainAgentOptIn} gate=${isEnabled(connectionId)} connectionId=${connectionId}"
```

`session/set_agent` 失败时额外区分错误码：

- opt-in/gate 不通过 → `methodNotFound("session/set_agent")`
- agent 切换被锁 → `-32011` + `{reason: "agent_switch_locked"}`

### 1.4 ClawBench 现状

`ClawBenchACPClient`（`internal/ai/acp_client.go`）**未实现 `acp.ExtensionMethodHandler`**。因此：

- agent 发来的 `_` 前缀 **request** → acp-go-sdk 回 `Method not found`
- agent 发来的 `_` 前缀 **notification** → acp-go-sdk 静默忽略（`connection.go` 对 `_` 前缀的 `-32601` 直接 return）

这也与 ClawBench 当前的 AskUserQuestion 策略一致：ClawBench 在系统提示词（`internal/model/agent.go`）中**显式禁止模型调用 `AskUserQuestion` 工具**，改用 `<ask-question>` XML 标签约定，由 `internal/ai/block_helpers.go` 的 `ConvertAskQuestionBlocks` 解析成卡片。同时 ClawBench 从未在 `initialize` 广告 `question: true`，CodeBuddy 也会把内置 `AskUserQuestion` 判定为不可用（`isEnabled` 返回 false，见 §4.1）——两条路径目前是"双重关闭"状态。

> 该结论已有实证：`internal/ai/codebuddy_acp_ask_question_integration_test.go` 中的 `TestCodebuddyACP_AskUserQuestionTool_NotAvailable` 驱动真实 `codebuddy --acp`，确认 ACP 模式下暴露的工具列表里**没有** `AskUserQuestion`，agent 会明说没有该工具；对照组 `TestCodebuddyACP_AskQuestionXML_OverACP` 验证 `<ask-question>` XML 通道在 ACP 下正常工作。
>
> ⚠️ **后续实测更正（见 §9）**：当客户端广告 `clientCapabilities._meta["codebuddy.ai"].question = true` 后，CodeBuddy **会**暴露 `AskUserQuestion` 工具，但它仍以**普通工具调用**（`sessionUpdate:"tool_call"` + `toolName=AskUserQuestion` + PermissionApproval）的形式出现，**不是** `_codebuddy.ai/question` 扩展请求。即"广告 question=true"与"走扩展通道"是两件独立的事。

---

## 2. 非 `_` 前缀的自定义 `session/*` 方法

这些方法**不满足 ACP 扩展方法命名约定**（`_` 前缀），在 ACP 规范里完全不存在。它们能被 CodeBuddy 处理，是因为 CodeBuddy 自己的 agent 侧 `extMethod` 是**兜底分支**：ACP SDK 的标准 `switch` 不认识的方法名一律转发给 `extMethod`，而 `extMethod` 内部又按完整方法名匹配这些 `session/*` 字符串。

> **含义**：这些方法名是 CodeBuddy 私有约定。**换任何其他 ACP agent 都会返回 `Method not found`**，客户端必须按 backend 分支处理。

### 2.1 清单

| 方法 | 用途 | 参数 |
|------|------|------|
| `session/set_model` | 切换模型（内部名 `unstable_setSessionModel`） | `{sessionId, modelId}` |
| `session/set_agent` | 切换主 Agent | `{sessionId, agentName}` |
| `session/set_multitask` | 开关 Multitask 协调器模式 | `{sessionId, enabled?}` |
| `session/inject_history` | **直接注入历史条目** | `{sessionId, messages: [{role: user\|assistant\|tool_call\|tool_result, content, arguments?, name?, tool_call_name?, output?, usage?}]}` |
| `session/steer` | **运行中插入用户消息**（"立即发送"） | `{sessionId, contentBlocks, clientUserMessageId?, expectedRequestId?, _meta?}` |
| `session/get_message_queue` | 读取消息队列 | `{sessionId}` |
| `session/save_message_queue` | 覆盖保存队列 | `{conversationId, items, version, ...}` |
| `session/enqueue_message` | 入队一条消息 | `{sessionId, contentBlocks}` |
| `session/remove_queue_item` | 移除队列项 | `{sessionId, itemId}` |
| `session/pop_queue_item_for_edit` | 取出队列项用于编辑 | `{sessionId, itemId}` → `{item?, queue}` |
| `session/reorder_queue` | 重排队列 | `{sessionId, orderedIds}` |
| `session/send_queue_item_now` | 立即发送队列项 | `{sessionId, itemId}` |
| `session/activate_queue` | 激活队列（派发队首） | `{sessionId}` |
| `session/pause_queue` | 暂停队列 | `{sessionId, reason?}` |
| `session/resume_queue` | 恢复队列 | `{sessionId}` |
| `session/enqueue_followup` | 入队 followup | `{sessionId, contentBlocks, hold?}` |
| `session/get_followup_queue` | 读取 followup 队列 | `{sessionId}` → `{sessionId, items: [{id, preview, teammate?, hold, createdAt}]}` |
| `session/remove_followup` | 移除 followup | `{sessionId, itemId}` |
| `session/send_followup_now` | 立即发送 followup | `{sessionId, itemId}` |

### 2.2 `session/steer`（最有接入价值）

语义：把消息**插入正在执行的那一轮**，模型在下一个请求前注入，不需要先 `session/cancel`。

```js
// 参数校验（handleSteer）
sessionId            // 必填，必须存在活跃 session
contentBlocks        // 必填，非空数组；经 lO() 校验，不合法时返回 {steered:false}
clientUserMessageId  // 可选，非空字符串；用于请求回执（原样 message id）
expectedRequestId    // 可选；若与当前 conversationRequestId 不符 → {steered:false, reason:"stale"}
_meta                // 可选，客户端不透明数据，原样落盘到该消息的 providerData.clientMeta

// 返回
{steered: true, ownerRequestId}   // 成功，ownerRequestId 为本轮不透明 ID
{steered: false}                  // contentBlocks 校验失败
{steered: false, reason: "stale"} // expectedRequestId 不匹配
{steered: false, reason: "idle"}  // 当前 session 非运行态或无 conversationRequestId
```

关键行为（`CHANGELOG.md`）：

- 消息**真正注入模型上下文那一刻**，agent 会额外发一条 `user_message_chunk`（带 `clientUserMessageId` 回执）
- `_meta` 原样落盘到 `providerData.clientMeta`（不解读、不改注入、不加 `isMeta`）
- `send_followup_now` 内部也复用 `handleSteer`

### 2.3 门控条件

| 方法族 | 门控 |
|--------|------|
| 消息队列族 | 产品特性 `ProductFeature.MessageQueueDeferredDispatch === true`，否则 `methodNotFound` |
| followup 族 | `isFollowupQueueAvailable(session)` = `isWebUiAcpSession(session)` **且** `!isDeferredDispatchEnabledSync()` |
| `steer` / `inject_history` / `set_model` / `set_multitask` | 仅需活跃 session；`set_multitask` 额外要求 session 归属当前 connection |

> **注意**：队列族与 followup 族门控**互斥**（一个要求 deferred dispatch 开、一个要求关），说明它们是两套并行的排队实现（Mode A / Mode B）。

---

## 3. agent → client 扩展通知

CodeBuddy 的 `ExtensionMethod` 枚举（`dist/codebuddy.js`）：

```js
ExtensionMethod = {
  ARTIFACT:              "_codebuddy.ai/artifact",
  ARTIFACT_BATCH:        "_codebuddy.ai/artifactBatch",
  QUESTION:              "_codebuddy.ai/question",           // request
  CHECKPOINT:            "_codebuddy.ai/checkpoint",
  COMMAND:               "_codebuddy.ai/command",
  AUTH_URL:              "_codebuddy.ai/authUrl",
  FILE_HISTORY_SNAPSHOT: "_codebuddy.ai/file_history_snapshot",
  DELEGATE_TOOL:         "_codebuddy.ai/delegateTool",       // request
  DELEGATE_TOOLS_CHANGED:"_codebuddy.ai/delegateToolsChanged",
  UI_CONTROL:            "_codebuddy.ai/uiControl",
}
KNOWN_EXTENSIONS = [ARTIFACT, ARTIFACT_BATCH, QUESTION, CHECKPOINT, COMMAND,
                    AUTH_URL, FILE_HISTORY_SNAPSHOT, DELEGATE_TOOL, DELEGATE_TOOLS_CHANGED]
```

### 3.1 逐项说明

| 方法 | 方向 | 用途 | 参数 |
|------|------|------|------|
| `artifact` | notification | 产物（artifact）创建/更新/删除 | `{sessionId, event: created\|updated\|deleted, artifact: {uri, ...}}` |
| `artifactBatch` | notification | 批量产物通知 | 批量结构 |
| `question` | **request** | AskUserQuestion 交互（见 §4.1） | `{sessionId, toolCallId, inputType: "question", schema: {questions: [{id, question, header, options, multiSelect}]}}` → client 回 `{outcome: {outcome: "submitted", data: {answers}}}` 或 `{outcome: {outcome: "cancelled", reason}}` |
| `checkpoint` | notification | 文件检查点创建/更新 | `{sessionId, event: created\|updated, checkpoint: {id, createdAt, label, fileChanges: {files, totalAdditions, totalDeletions}}}` |
| `command` | notification | 工作区信息等命令通道 | `{sessionId, action: "workspace_info", params: {isGitWorkspace}}` |
| `authUrl` | notification | 外部 OAuth 登录 URL | `{authUrl, provider: "external"}` |
| `file_history_snapshot` | notification | 文件历史快照 | 快照结构 |
| `delegateTool` | **request** | 让 client 执行其注册的委托工具 | `{sessionId, toolCallId, toolId, input, timeout}` → client 回 `{status: success\|error\|..., output?, error?}` |
| `delegateToolsChanged` | — | 枚举中存在，但未找到对应 handler（仅 `extMethod` 有同名 client→agent 方向） | — |
| `uiControl` | — | 枚举中存在，但未找到任何引用/构造点 | — |

> `KNOWN_EXTENSIONS` 不含 `UI_CONTROL`，且全 bundle 中 `ExtensionMethod.UI_CONTROL` 出现 0 次——该常量目前是**预留未实现**。

### 3.2 ClawBench 现状

同样因未实现 `ExtensionMethodHandler` 而全部未接入。`artifact` / `checkpoint` / `authUrl` / `command` 作为 notification 会被 acp-go-sdk 静默忽略；`question` / `delegateTool` 作为 request 会被回 `Method not found`（CodeBuddy 侧 `handleExtMethod` 收到未知方法会返回 `{outcome: {outcome: "cancelled", reason: "unknown method"}}`，即 AskUserQuestion 会**被降级为"用户拒绝回答"**）。

---

## 4. 能力协商（`initialize` 的 `_meta`）

ACP 规范允许 `initialize` 的 `clientCapabilities` / `agentCapabilities` 携带 `_meta`，CodeBuddy 用它做双向能力协商。

### 4.1 client → agent

| 位置 | 字段 | 效果 |
|------|------|------|
| `clientCapabilities._meta["codebuddy.ai"]` | `question: true` | **启用 `_codebuddy.ai/question` 扩展** + 启用内置 `AskUserQuestion` 工具。为 false/缺失时，`AskUserQuestion` 的 `isEnabled` 返回 false（ACP 分支） |
| 同上 | `terminalOutput: true` | 启用 `terminal_update` 推送 |
| 同上 | `terminalOutputChunk: true` | 启用 `terminal_output_chunk` 推送 |
| 同上 | `mainAgentSupport: true` | 打开主 Agent gate → 解锁 `_codebuddy.ai/getAgents` / `session/set_agent` / `multitaskSupport` 广告 |
| 同上 | `fileReferencesPathOnly: true` | 文件引用仅发路径（不内联内容） |
| 同上 | `promptSuggestion: true` | 接收 `session_info_update._meta["codebuddy.ai/promptSuggestion"]` |
| 同上 | `weixinpay.interception: true` | 微信支付拦截相关能力 |
| `clientCapabilities._meta["workbuddy.desktop"]` | `detailedCheckpoint: true` | **另一个命名空间**：接收带完整 fileChanges 的详细 checkpoint |
| `clientCapabilities.elicitation.form` | `true`（ACP 标准 unstable） | 标准 elicitation 表单能力 |
| `clientInfo.name` | `"codebuddy-web-ui"` | **解锁全部 `_codebuddy.ai/mcpUi*`**、`sessionSnapshot`、`replayWindowQueue` 等 Web UI 专属行为 |

> `question` 的完整判定链（`dist/codebuddy.js`）：
> ```js
> if (!0 === ctx.options.acp) {
>   const conn = ...getConnection(acpConnectionId)
>   const caps = conn.getClientCapabilities()
>   return caps._meta?.["codebuddy.ai"]?.question === true
> }
> ```
> 即 **ACP 模式下 `AskUserQuestion` 工具的可用性完全由客户端能力决定**。

### 4.2 agent → client

`initialize` 响应的 `agentCapabilities`（非规范字段）：

```js
agentCapabilities: {
  promptCapabilities: { image: true, embeddedContext: true },   // 标准
  mcpCapabilities:    { http: true, sse: true },                // 标准
  loadSession:        true,                                     // 标准
  delegateToolsSupport: true,        // ← 非标准：支持委托工具
  mainAgentSupport:  <bool>,         // ← 非标准：主 Agent 能力（受 client opt-in + gate）
  multitaskSupport:  <bool>,         // ← 非标准：Multitask 支持
  processDefaults:   {...},          // ← 非标准：仅 mainAgentOptIn 时附带
}
```

`authMethods` 恒为 4 项：`iOA` / `external` / `internal` / `selfhosted`。

> **注意**：CodeBuddy **不广告 `sessionCapabilities`**（`grep -c sessionCapabilities` = 0）。这意味着 `session/list`、`session/delete`、`session/fork`、`session/resume` 这些规范中的 capability-gated 方法，CodeBuddy 实际是**无条件实现**的（ClawBench 侧 `internal/ai/acp_conn_lifecycle.go` 从 `initResp.AgentCapabilities.SessionCapabilities` 读取 List/Delete 能力，对 CodeBuddy 会读到 false，需依赖 `BackendSpec` 兜底）。
>
> ⚠️ **实测补充（§9.3）**：上面这段 `agentCapabilities` 是从**原始 wire** 抄的。如果改用 acp-go-sdk 的 `AgentCapabilities` typed struct 序列化，`delegateToolsSupport` / `mainAgentSupport` / `multitaskSupport` 会**全部消失**——因为 SDK 类型定义里没有这三个字段，`json.Unmarshal` 静默丢弃。读取这些非标准能力必须直接解析原始 JSON。

### 4.3 `multitask` 的端到端契约

1. 客户端在 `clientCapabilities._meta["codebuddy.ai"].mainAgentSupport = true`
2. agent 在 `agentCapabilities.multitaskSupport = true` 广告
3. 客户端从 `configOptions` 发现 `Multitask` 开关（`{type: "boolean", id: "multitask", category: "_codebuddy.ai/multitask"}`），可通过 `session/set_config_option` 控制
4. 也可直接用 `session/set_multitask {sessionId, enabled}` 控制
5. 后台 worker 结束 → agent 在**空闲会话**上开新一轮，先发 `session_info_update._meta["codebuddy.ai/unsolicitedTurn"] = {requestId, reason: "background_drain"}`，宿主须用其中 `requestId` 打开新 Request

---

## 5. 非标准 `sessionUpdate` 类型

ACP v1 规范的 `SessionUpdate` 判别联合共 **11 种**：
`user_message_chunk`、`agent_message_chunk`、`agent_thought_chunk`、`tool_call`、`tool_call_update`、`plan`、`available_commands_update`、`current_mode_update`、`config_option_update`、`session_info_update`、`usage_update`。

CodeBuddy **实际构造**的 `sessionUpdate` 值共 **18 种**（`grep -o 'sessionUpdate:"[a-z_]*"' dist/codebuddy-headless.js | sort -u`），其中 **7 种非标准**：

| 类型 | 标准？ | 说明 |
|------|--------|------|
| `agent_message_chunk` | ✅ | |
| `agent_thought_chunk` | ✅ | |
| `user_message_chunk` | ✅ | 历史回放 / 多页面广播 / steer 注入回执 |
| `tool_call` | ✅ | |
| `tool_call_update` | ✅ | status 额外含 `running`（非标准枚举值） |
| `plan` | ✅ | |
| `available_commands_update` | ✅ | |
| `current_mode_update` | ✅ | |
| `config_option_update` | ✅ | |
| `session_info_update` | ✅ | **核心扩展载体**，大量信息经 `_meta` 传递 |
| `usage_update` | ✅ | |
| **`session_end`** | ❌ | 本轮结束：`{stopReason: end_turn\|cancelled\|refusal, errorMessage?, finishReason?}` |
| **`model_update`** | ❌ | `{availableModels: [{id, name}], currentModelId}` |
| **`tool_call_pending`** | ❌ | 工具调用占位（流式参数开始前的 pending 状态） |
| **`tool_call_arguments_delta`** | ❌ | 工具参数流式增量 |
| **`tool_call_arguments_done`** | ❌ | 工具参数流式结束 |
| **`terminal_update`** | ❌ | 终端状态：`{terminalId, command?, cwd?, exitStatus?, output?}` |
| **`terminal_output_chunk`** | ❌ | 终端输出增量：`{terminalId, data: base64}` |

### 5.1 内部流式类型

`tool_call_pending` / `tool_call_arguments_delta` / `tool_call_arguments_done` 被 CodeBuddy 自身归为 **`INTERNAL_STREAMING_UPDATES`**：

```js
static INTERNAL_STREAMING_UPDATES = new Set([
  "tool_call_pending", "tool_call_arguments_delta", "tool_call_arguments_done"
])
static isInternalStreamingUpdate(u) { ... }
```

**所有** `convertOpenAIEventToAcp(...)` 的消费点都会先 `.filter(u => !isInternalStreamingUpdate(u))` 再转发/回放。即：

- 这些类型**仅存在于 agent 内部管线**，用于驱动 `JsonStreamParser` 累积工具参数
- 对外（含 `session/load` 历史回放）会被**过滤掉**，客户端不应依赖收到它们

> `dist-server/8569.codebuddy.js` 中仍出现这三个字符串，是内置 ACP SDK 的类型定义残留，非实际发射路径。

### 5.2 `mode_update` 并不存在

`model_update` 有实际构造点，但 **`sessionUpdate:"mode_update"` 在全 bundle 中出现 0 次**——`"mode_update"` 这个字符串仅出现在内嵌 OpenAPI 的 schema 定义里（`ModeUpdate` 类型声明），没有任何构造/发送点。权限模式变化只通过标准的 `current_mode_update` 发送。即该类型是**文档声明了但未实现**（或已废弃）。

---

## 6. `_meta` 扩展字段

全 bundle 中共 **163 个** `codebuddy.ai/*` 键。按用途分类：

### 6.1 身份 / 追踪

`requestId`、`messageId`、`messageRequestId`、`promptRequestId`、`conversationId`、`conversationRequestId`、`llmMessageId`、`userMessageId`、`toolCallId`、`modelRequestId`、`traceId`、`traceparent`、`userId`、`expertId`、`sessionId`、`newSessionId`、`parentSessionId`

### 6.2 Agent / 子智能体 / Team

`agentPhase`、`agentColor`、`agent`、`isSubagent`、`isSubAgent`、`subagentType`、`parentToolCallId`、`isBackground`、`mainAgent`、`multitask`、`isTeamMember`、`memberEvent`、`memberName`、`memberHistoryItemId`、`teamUpdate`、`teamResumeIntent`、`delegateTool`、`delegateToolsChanged`、`workflowRunId`、`workflowStatus`、`workflowPhase`、`workflowPhaseCount`、`workflowName`、`workflowError`、`workflowEventKind`、`workflowAgentCount`、`workflowAgentKey`、`workflowAgentLabel`、`workflowAgentPhase`、`workflowAgentTokens`、`workflowAgentError`、`workflowCachedCount`

### 6.3 权限 / 审批 / 沙箱

`permissionResolved`、`decision`、`interruptionRequest`、`resolveInterruption`、`interceptType`、`sandboxIntercept`、`sandboxApprovalMode`、`sandboxDisabledReason`、`approvalCategory`、`approvalKind`、`approvalPolicyId`、`approvalScope`、`requestSensitiveApproval`、`cancelSensitiveApproval`、`sensitiveProtectionApproval`、`sensitiveOriginalGrantKey`、`sensitiveInputDecision`、`skillApprovalDecision`、`bypassHint`、`mcpUiIntercept`

### 6.4 会话状态 / 回放

`historyReplay`、`historyReplayTotalItems`、`sessionSnapshot`、`sessionReset`、`isCompactInternal`、`compactType`、`compact-cancelled`、`compact-limit-reached`、`checkpoint`、`file_history_snapshot`、`contextUsed`、`goalStatus`、`goalProgress`、`goalRecap`、`goalResult`、`progress`、`terminationReason`、`cancelReason`、`streamKeepalive`、`unsolicitedTurn`、`promptSuggestion`、`followupQueue`、`message_queue_update`、`steerClientMeta`

### 6.5 工具 / 模型

`toolName`、`toolArgumentsComplete`、`toolFailReason`、`toolCancelReason`、`responseModelId`、`requestModelId`、`requestModelName`、`finishReason`、`outcome`、`usageByCategory`、`mode`

### 6.6 IM 渠道

`channelSource`、`channelSender`、`channelChatId`、`channelChatType`

### 6.7 嵌套命名空间 `_meta["codebuddy.ai"] = {...}`

除扁平键外，还有一批**嵌套**在 `_meta["codebuddy.ai"]` 对象内的键（注意与扁平键 `codebuddy.ai/xxx` 形式不同）：

| 键 | 说明 |
|----|------|
| `mode` | `"history"`（历史回放标记） |
| `messageId` / `offset` | 回放偏移 |
| `isHistoryReplay` / `isSessionSeparator` / `separatorExtra` | 历史分隔标记 |
| `toolName` / `toolMetaData: {mcpProgress?, mcpUi?}` | 工具元数据 |
| `end` | 终端结束标记 |
| `elicitationId` | elicitation 关联 |
| `weixinpayResult` | 微信支付结果 |

### 6.8 ClawBench 已解析的键

`internal/ai/acp_meta_codebuddy.go` 目前只解析：

```
codebuddy.ai/usageByCategory   codebuddy.ai/requestId
codebuddy.ai/traceId           codebuddy.ai/traceparent
codebuddy.ai/messageId         codebuddy.ai/messageRequestId
codebuddy.ai/requestModelId    codebuddy.ai/requestModelName
codebuddy.ai/responseModelId   codebuddy.ai/finishReason
codebuddy.ai/outcome           codebuddy.ai/agentPhase
+ 扁平的 _meta.usage（OpenAI 风格 token 用量）
```

其余 150+ 键**未被消费**（含 `interruptionRequest`、`teamUpdate`、`historyReplay`、`promptSuggestion` 等语义价值较高的键）。

---

## 7. 与 ACP 规范的偏离汇总

| 偏离 | 规范要求 | CodeBuddy 实际 |
|------|----------|----------------|
| 自定义方法命名 | 必须以 `_` 开头 | `session/set_model`、`session/steer`、`session/inject_history`、队列族等 **19 个**方法**无 `_` 前缀** |
| `sessionUpdate` 判别联合 | 固定 11 种 | 额外发 `session_end` / `model_update` / `tool_call_pending` / `tool_call_arguments_delta` / `tool_call_arguments_done` / `terminal_update` / `terminal_output_chunk` |
| `ToolCallStatus` 枚举 | `pending\|in_progress\|completed\|failed` | `tool_call_update.status` 额外含 `running` |
| `agentCapabilities` | 规范字段集 | 额外 `delegateToolsSupport` / `mainAgentSupport` / `multitaskSupport` / `processDefaults` |
| `sessionCapabilities` | 用 capability 声明 list/delete/fork/resume | **完全不广告**，但无条件实现 |
| `usage_update.cost` | 货币金额 | 塞入 credit 数值（`currency` 为空）；ClawBench 已在 `costFieldCarriesCredit` 中丢弃 |
| `clientCapabilities._meta` | 可自由使用 | 用了**两个命名空间**：`codebuddy.ai` 与 `workbuddy.desktop` |
| `clientInfo.name` | 仅标识 | 用作功能门控（`"codebuddy-web-ui"` 解锁 mcpUi*） |
| 扩展方法应回 `Method not found` | ✅ | agent 侧合规；但 `question`/`delegateTool` 被拒后返回的是 `{outcome: "cancelled"}` 而非 JSON-RPC error |

---

## 8. 接入建议

### 8.1 前置改造（一次性）

1. **实现 `acp.ExtensionMethodHandler`**：acp-go-sdk v0.13.5 已提供 `CallExtension` / `NotifyExtension` 与 `HandleExtensionMethod` 接口（`extensions.go`），ClawBench 侧只需让 `ClawBenchACPClient` 实现该方法即可接收 `_` 前缀 request。

   > ⚠️ **SDK 有两个硬约束**（`extensions.go` + `client.go`，本地与 upstream 均已确认）：
   >
   > 1. `validateExtensionMethodName` 强制方法名必须以 `_` 开头。因此 `CallExtension` **无法发送 `session/steer`、`session/inject_history`、`session/set_model` 等 §2 中的非 `_` 前缀方法**，调用会直接返回错误（实测：`extension method name must start with '_' (got "session/set_model")`）。
   > 2. `ClientSideConnection.conn` 是**非导出字段**，且 SDK **未提供任何 accessor**（无 `Conn()` 方法）。因此无法绕过校验、拿到底层 `*Connection` 去调 `SendRequest`。
   >
   > 结论：接入 §2 的方法需要在 ClawBench 侧**自行实现 JSON-RPC 写入**——`acp_conn_lifecycle.go` 中 `cmd.StdinPipe()` 得到的 `stdinPipe` 目前只传给了 `acp.NewClientSideConnection`、未被保留，需将其存入 `ACPConn` 以便直接写 JSON-RPC 行。这是接入 `session/steer` 的主要改造点。

2. **扩展 `initialize` 的 `clientCapabilities._meta`**：按需广告 `question` / `terminalOutput` / `mainAgentSupport` / `promptSuggestion` 等。
3. **扩展 `_meta` 解析**：至少补 `interruptionRequest`、`historyReplay`、`promptSuggestion`、`teamUpdate`。

### 8.2 候选接入项（按价值排序）

| 能力 | 方法 | 价值 | 代价 |
|------|------|------|------|
| **运行中插话** | `session/steer` | 高 —— 替代"cancel 再发"的粗暴路径，保持本轮上下文 | 需非 `_` 前缀的裸 JSON-RPC；需处理 `steerClientMeta` / `clientUserMessageId` 回执 |
| **交互式提问** | `_codebuddy.ai/question`（request） | 高 —— 原生问卷 UI，替代 XML 解析 | 需实现 ExtensionMethodHandler + 广告 `question: true`；会改变现有 AskUserQuestion 行为 |
| **消息队列** | 队列族 | 中 —— 与服务端队列语义对齐 | 受产品特性门控；与 ClawBench 自有的 DB 队列可能冲突 |
| **权限响应** | `_codebuddy.ai/resolveInterruption` | 中 —— 目前靠标准 `session/request_permission` 已可用 | 需确认 CodeBuddy 何时走哪条路径 |
| **文件回滚** | `previewFileRollback` / `rollbackFiles` | 中 | 需先有 checkpoint 数据 |
| **Multitask** | `session/set_multitask` | 低（当前阶段） | 门控复杂，`unsolicitedTurn` 需宿主开新 Request |
| **MCP UI** | `mcpUi*` | 低 | 仅 `codebuddy-web-ui` 可用，需伪装 clientInfo |

### 8.3 风险提示

- 以上全部为 **CodeBuddy 私有约定，无版本稳定性承诺**。bundle 已混淆，方法名/参数可能在小版本内变更（对比 `CHANGELOG.md` 可见 `session/steer` 的返回结构曾调整过）。
- 接入应做**能力探测**（先广告 capability 再调用），失败时静默降级，不可硬依赖。
- `session/steer` 与 ClawBench 现有 `CancelSession` 路径存在语义交叠，需明确"何时 cancel、何时 steer"的产品规则。

---

## 9. 实测验证结果（探针）

以上 §1–§8 全部来自**静态分析**。本节是**真机实测**结论，探针位于
`internal/ai/codebuddy_acp_ext_probe_integration_test.go`（`-tags integration`）：

```bash
go test -v -run 'TestCodebuddyACP_ExtProbe' -tags integration -timeout 600s ./internal/ai/
```

探针自行实现了 §8.1 提出的"裸 JSON-RPC 通道"原型：`syncWriter`（出站写锁）+
`demuxReader`（入站 tee + 按 `cb-` id 前缀拣出探针响应）。

| 探针 | 验证项 | 结果 |
|------|--------|------|
| `RawMethodTransport` | P1 非 `_` 前缀方法可经裸 JSON-RPC 发送 | ✅ **CONFIRMED** |
| 同上 | P2 自建字符串 ID 与 SDK 数值 ID 互不干扰 | ✅ **CONFIRMED** |
| `SteerDuringPrompt` | P3 运行中并发 steer 不阻塞、不破坏 prompt | ✅ **CONFIRMED** |
| `CapabilityNegotiation` | 能力广告生效（`mainAgentSupport` 门控翻转） | ✅ **CONFIRMED** |
| `ExtensionMethodRouting` | 档 1 的 `ExtensionMethodHandler` 路由 | ⚠️ **部分**（见下） |

### 9.1 P1 / P2 — 裸 JSON-RPC 通道成立

```
P1 session/set_model raw response: {"id":"cb-1","jsonrpc":"2.0",
    "result":{"modelId":"clawbench-ext-probe-nonexistent-model"}}
P2 CONFIRMED: string ids ("cb-N") routed to the probe while SDK uses numeric ids;
    no leftover pending requests
```

- 非 `_` 前缀方法确实被 CodeBuddy 的 `extMethod` 兜底分支处理（返回业务 result，而非 `-32601`）
- 自建 `"cb-N"` 字符串 ID 与 SDK 的数值 ID 共存无冲突，响应正确回流到探针
- **结论**：§8.1 的改造方案（写锁 + 按 id 前缀 demux）**可行且已被原型验证**

### 9.2 P3 — `session/steer` 可运行中插话

```
P3 session/steer raw response (0.02s): {"result":{"steered":true,
    "ownerRequestId":"01a09331f3c775c68a16bd326b45d671"}}
P3 CONFIRMED: steer accepted mid-turn; elapsed=0.02s — NOT blocked behind the prompt
P3 CONFIRMED: prompt completed cleanly after steer; stopReason="end_turn"
P3 steer marker present in final output: true
```

- steer 在 prompt 在途时 **0.02s** 返回，未被 prompt 阻塞
- prompt 之后正常 `end_turn`，未被 steer 破坏
- 注入的标记文本（`CLAWBENCH-STEER-MARKER`）**确实出现在最终输出**里，证明消息真的进了模型上下文
- **结论**：`session/steer` 作为"运行中插话"的原语**完全可用**

### 9.3 能力协商确实生效

对比"不广告"与"广告"两次 `initialize` 的**原始 wire 响应**：

| 客户端广告 | `mainAgentSupport`（agent 回） |
|-----------|------------------------------|
| 不广告 | `false` |
| `_meta["codebuddy.ai"].mainAgentSupport = true` | **`true`** |

同时确认 `acp-go-sdk` **不会**丢弃 `ClientCapabilities.Meta`——我们广告的
`{"codebuddy.ai":{...}}` 原样出现在 wire 请求里：

```json
{"method":"initialize","params":{"clientCapabilities":{"_meta":{"codebuddy.ai":{
  "fileReferencesPathOnly":true,"mainAgentSupport":true,"promptSuggestion":true,
  "question":true,"terminalOutput":true,"terminalOutputChunk":true}}, ...}}}
```

> **额外发现**：`agentCapabilities` 的非标准字段（`delegateToolsSupport` /
  `mainAgentSupport` / `multitaskSupport`）**不在** acp-go-sdk 的
  `AgentCapabilities` 类型定义里，会被 `json.Unmarshal` **静默丢弃**。
> 要读取它们必须从**原始 wire** 取（探针用 `findRawResponse` 演示了做法）。
> 这也解释了 §4.2 中为何 typed dump 看不到这些字段。
>
> 另外确认：`session/new` 的 `configOptions` 里确实广告了 `id:"multitask"`
> （`type:"boolean"`, `category:"_codebuddy.ai/multitask"`），验证了 §4.3
> "Multitask 可走标准 `session/set_config_option`"的推断。

### 9.4 `_codebuddy.ai/question` 在 stdio 下不可用（负面结论）

这是本次实测最重要的**否定性发现**。实验设置：广告 `question: true`，
然后要求模型调用 `AskUserQuestion`。

观测（原始 wire）：

```
tools the model invoked this turn: [AskUserQuestion AskUserQuestion PermissionApproval]
```

```
{"sessionUpdate":"tool_call","toolCallId":"call_00_NeiOz...","title":"AskUserQuestion",
 "kind":"other","_meta":{"codebuddy.ai/toolName":"AskUserQuestion", ...}}
```

- 模型**确实调用了** `AskUserQuestion`（两次），所以不是"模型没调"导致的假阴性
- 但它走的是**普通工具调用**通道（`sessionUpdate:"tool_call"` + PermissionApproval）
- **没有**出现任何 `_codebuddy.ai/question` 扩展请求
- 探针同时捕获到的扩展请求只有 `_codebuddy.ai/command`（workspace_info）

**结论**：在**原始 stdio ACP 会话**下，广告 `question: true` 会让 CodeBuddy
暴露 `AskUserQuestion` 工具，但**不会**把提问路由到 `_codebuddy.ai/question`
扩展通道。与源码分析一致——该扩展分支的门控要求
`runContext.context.meta.acpConnectionId`，而该字段只由 CodeBuddy 自己的
**HTTP ACP 网关**（`POST /api/v1/acp`）设置，stdio 会话不设。

> **对 ClawBench 的含义**：档 1 中"用 `_codebuddy.ai/question` 替换
> `<ask-question>` XML 约定"这条路**在 stdio 下走不通**，应放弃该设想，
> 继续使用现有的 XML 约定。

### 9.6 P4 — 插入处可精确识别（"截断为两条"，已实现）

**问题**：steer 接入后，运行中插入的用户消息排在**正在流式输出的助手回复之后**
（`[Q1] [助手仍在流式] [Q2]`），Q2 后面空无一物，看着像"发了没人理"。修法是
在插入处把助手回复切成两条：

```
[Q1] [助手回复·前段] [Q2] [助手回复·后段]
```

这要求宿主能**精确定位插入边界**，否则只能靠猜测切分（不安全）。探针
`TestCodebuddyACP_ExtProbe_SteerSplitBoundary` 采集 steer 前后**全部 1103 帧**原始
wire，逐帧打相对 steer 时刻的时间戳。

```
P4a steer accepted=true ownerRequestId="01a0952e313a7621a0a0c92040509755"
P4a sessionUpdate type counts: map[agent_message_chunk:192 agent_thought_chunk:980
    session_info_update:26 tool_call:6 tool_call_update:60 usage_update:9
    user_message_chunk:1 ...]
P4b OBSERVED 1 user_message_chunk frame(s):
    [0] at steer+2.76s: {"method":"session/update","params":{"update":{
        "_meta":{
          "codebuddy.ai/conversationRequestId":"01a0952e313a7621a0a0c92040509755",
          "codebuddy.ai/messageId":"clawbench-split-probe-1",
          "codebuddy.ai/messageRequestId":"clawbench-split-probe-1",
          "codebuddy.ai/requestId":"..."},
        "content":{"type":"text", ...}}}}
P4c DECISIVE: boundary frame at index 147/1103;
    agent_message_chunk before=9 after=169
P4c *** SPLIT IS FEASIBLE ***
```

**三个决定性结论**：

1. **边界信号存在且唯一**：整轮 1276 个 `sessionUpdate` 中
   `user_message_chunk` **恰好出现 1 次**，就在 steer 之后 2.76s。文档 §5 称其为
   "steer 注入回执"，实测证实。

2. **可精确关联，无需解析 agent 内部 id**：
   `_meta["codebuddy.ai/messageId"]` **等于我们发送的 `clientUserMessageId`**
   （`"clawbench-split-probe-1"`，match=true），
   `_meta["codebuddy.ai/conversationRequestId"]` **等于 steer 响应的
   `ownerRequestId`**（match=true）。宿主可以用自己生成的 id 定位边界。

3. **恰好夹在前段与后段之间**（这是充分条件）：边界帧在 index 147，
   其前有 9 个 `agent_message_chunk`、其后有 169 个。邻域可见边界帧紧跟在
   `tool_call_update` 之后、`session_info_update` 之前：

   ```
   [146] session_update/tool_call_update   @ steer+2.70s
   >> [147] session_update/user_message_chunk @ steer+2.76s
   [148] session_update/session_info_update @ steer+2.76s
   ```

> **接入含义**：实现"截断为两条"**不需要**任何启发式推断。宿主在收到
> `user_message_chunk` 时，把当前累积的助手内容定稿为"前段"、开一条新的助手
> 消息作为"后段"，边界即该帧的位置。`clientUserMessageId`（我们生成的 queueId）
> 让这个判断与具体的 steer 调用一一对应。

#### 已实现（2026-09-12）

按上述结论落地，分层与本仓库既有约定一致（通用传输 / 后端策略 / 核心流程零 backend 名）：

| 层 | 位置 | 职责 |
|----|------|------|
| 边界观测 | `internal/ai/acp_events.go` `UserMessageChunk` 分支 | 认领 echo → 发 `steer_boundary` 事件 |
| 回执登记 | `internal/ai/acp_pool.go` `ExpectSteerEcho`/`ForgetSteerEcho` | 只认**我们自己发起**的注入 id |
| 策略 | `internal/ai/backends/codebuddy/midturn.go` | 发 steer 前登记 echo；未命中则撤销 |
| 切分 | `internal/service/session_executor.go` `splitAtSteerBoundary` | 定稿前段 + 建新流式行（锚定注入问题） |
| 前端 | `chatStreamUtils.ts` `ws_stream_split` | 前段去 streaming、push 后段气泡 |

**触发方式（重要）**：发送消息**永远只排队**，任何后端都一样。并入当前轮是用户在
**排队气泡上显式点按钮**才发生的，不是发送的副作用——否则支持 steer 的后端会把消息
悄悄吞进正在生成的回复里，用户既看不到排队气泡也无从操作。所以全后端流程统一，**只有
按钮标签不同**：

| 后端 | 按钮 | 行为 |
|------|------|------|
| 支持 steer 且当前是 ACP（`model.Agent.SupportsMidTurn` + transport） | 「插入当前回复」 | `POST /api/ai/queue/inject` — 认领原排队行（保留 DB id）→ `session/steer` |
| 其余 | 「中断并发送」 | `POST /api/ai/queue/interrupt` — 停当前轮，drain loop 按序接着跑队列 |

按钮能力位是**后端 × transport** 两个条件：注入走 agent 的活 ACP 连接，所以同一个
支持 steer 的后端在 CLI 会话下无法插入，此时标签必须回退成「中断并发送」，否则
标签就与实际行为不符。

「中断并发送」的语义是**「停当前回复，队列按序继续」**，不是把这条消息提到队首
—— 队列顺序即 DB id 顺序，重排会破坏「插入保留原行 id」所依赖的整套性质。因此
`queueId` 在中断端点里是**新鲜度校验**（该消息必须仍在排队），而非提权参数。

**关键设计**：

1. **gating 是必需的**。`user_message_chunk` 同时用于 LoadSession 回放与多页广播，
   不加门控会把它们误判成实时注入、错误切分回复。门控依据是
   `_meta["codebuddy.ai/messageId"]` 是否命中本连接登记过的注入 id；
   `claimPendingSteerID` 是**一次性**的，重放不会二次触发。
2. **DB id 顺序天然正确**。前段保留原流式行 id、注入问题随后落库、后段是新行，
   于是 `Q1(1) → 前段(2) → Q2(3) → 后段(4)`，纯 id 排序就是期望的对话顺序，
   前端无需任何特例排序。
3. **失败降级而非丢内容**。前段定稿失败 → 继续写原行（退回单条）；后段建行失败 →
   补建一条无锚点的流式行，保证剩余内容不丢。
4. **`ws_stream_start` 加了一道保护**：不再无条件覆写流式气泡的 id，只在气泡**尚无
   数字 id** 时采纳。否则一条针对前段的迟到/重复 `stream_start` 会把后段气泡改名，
   两条消息塌回一条。
5. **中断需要每轮独立 context，且必须按轮次 id 定位**。drain loop 原本所有轮次共用一个
   长生命周期 ctx，直接取消它会连带杀掉后续排队消息。因此每轮派生自己的 `turnCtx`
   并登记一个单调递增的 turnID（`sessionTurnCancels`），中断只停这一轮，队列照常继续。
   带 id 是必需的：调用方读到「当前轮」后，旧轮可能已结束、drain loop 已启动下一条
   排队消息，无 id 校验就会砍掉用户并未要求停止的那一轮（check-then-act 竞态）。
   `InterruptSessionTurnIfCurrent` 只在 id 仍然匹配时生效。
6. **中断 ≠ 取消**：中断**保留队列**、**不打「已取消」角标**、会话继续运行；用户取消则
   清空队列、标记 cancelled、结束会话。`drainHandleTerminal` 里两条分支刻意分开。

**端到端验证**（`TestCodebuddyACP_SteerSplitE2E_BoundaryReachesStream`，驱动生产
`ACPConn` 与真实 agent）：

```
CONFIRMED: steer_boundary event reached the stream, clientUserMessageID="clawbench-split-e2e-1"
session/steer accepted
content events after boundary: 13
```

**风险**：§10 已声明这些是私有约定，无版本稳定性承诺。边界信号消失时宿主**降级**
（退回单条助手消息），不会切错位置。

### 9.7 修正后的接入优先级

| 能力 | 实测结论 | 建议 |
|------|----------|------|
| `session/steer`（裸 JSON-RPC） | ✅ 可用 | **优先**——需 §8.1 的写锁 + demux |
| 裸 JSON-RPC 通道本身 | ✅ 已验证 | 是上面所有非 `_` 方法的前置设施 |
| `user_message_chunk` 边界信号 | ✅ 可精确定位插入点 | 支持"截断为两条"，需新增事件类型 |
| 读取非标准 `agentCapabilities` | ✅ 需读 raw wire | 接入时必须绕开 SDK typed struct |
| `_codebuddy.ai/question` | ❌ stdio 下不触发 | **放弃**，保留 `<ask-question>` XML |
| `_codebuddy.ai/*` 其它扩展 | ⚠️ 未逐一实测 | 路由机制已证明（收到 `command`），按需接入 |
| Multitask | ✅ 走标准 config | 用 `set_config_option(configId:"multitask")` |

### 9.8 探针复现

```bash
# 全部探针（约 2 分钟）
go test -v -run 'TestCodebuddyACP_ExtProbe' -tags integration -timeout 600s ./internal/ai/

# 单跑
go test -v -run 'TestCodebuddyACP_ExtProbe_RawMethodTransport'      -tags integration ./internal/ai/
go test -v -run 'TestCodebuddyACP_ExtProbe_SteerDuringPrompt'       -tags integration ./internal/ai/
go test -v -run 'TestCodebuddyACP_ExtProbe_SteerSplitBoundary'      -tags integration ./internal/ai/
go test -v -run 'TestCodebuddyACP_ExtProbe_CapabilityNegotiation'  -tags integration ./internal/ai/
go test -v -run 'TestCodebuddyACP_ExtProbe_ExtensionMethodRouting' -tags integration ./internal/ai/

# 端到端：边界事件真的从生产链路冒出来（§9.6 已实现部分）
go test -v -run 'TestCodebuddyACP_SteerSplitE2E' -tags integration ./internal/ai/
```

前置条件：本机安装 `codebuddy` CLI 且已登录。

---

## 附录 A：复核命令

```bash
D=~/.nvm/versions/node/v24.14.0/lib/node_modules/@tencent-ai/codebuddy-code

# 1) agent 侧 extMethod 大 switch（client→agent 扩展请求全清单）
python3 -c "
import re
p='$D/dist/codebuddy-headless.js'
s=open(p,encoding='utf-8',errors='replace').read()
m=re.search(r'async extMethod\([^)]*\)\{', s)
seg=s[m.start():m.start()+9000]
seg=seg[:seg.find('default:')]
for c in re.findall(r'case\s*([^:]+):', seg): print(c)
"

# 1b) SDK 标准 requestHandler switch（对比出哪些是标准方法）
python3 -c "
import re
p='$D/dist/lazy-headless/569.8cfb7a1c.js'   # 文件名含 hash，用 ls 确认
s=open(p,encoding='utf-8',errors='replace').read()
m=re.search(r'requestHandler=async\([^)]*\)=>\{switch\(', s)
seg=s[m.start():m.start()+6000]
seg=seg[:seg.find('default:')]
for c in re.findall(r'case\s*([^:]+):', seg): print(c)
"

# 2) ExtensionMethod 枚举（agent→client 扩展通知）
grep -o 'ExtensionMethod={ARTIFACT[^}]*}' $D/dist/codebuddy.js | head -1

# 3) 全部 meta key
grep -o 'codebuddy\.ai/[a-zA-Z0-9_./-]*' $D/dist/codebuddy.js | sort -u | wc -l   # 163
grep -o 'codebuddy\.ai/[a-zA-Z0-9_./-]*' $D/dist/codebuddy.js | sort -u

# 4) 实际构造的 sessionUpdate 值
grep -o 'sessionUpdate:"[a-z_]*"' $D/dist/codebuddy-headless.js | sort -u

# 5) 内嵌 OpenAPI（含 ACP 方法文档）
python3 -c "
p='$D/dist/codebuddy.js'
s=open(p,encoding='utf-8',errors='replace').read()
i=s.find('resolveInterruption \\\\u54CD\\\\u5E94')
seg=s[i:i+120000].encode().decode('unicode_escape',errors='replace')
import re
for m in re.finditer(r'### ([^\n]+)', seg): print(m.group(1))
"

# 6) 能力协商门控
grep -o 'clientCapabilities?._meta?\.\["codebuddy.ai"\][^;]\{0,300\}' $D/dist/codebuddy.js | head -3

# 7) 确认 headless（--acp 实际 bundle）与 TUI bundle 一致
grep -c 'session/get_message_queue' $D/dist/codebuddy-headless.js $D/dist/codebuddy.js

# 8) 确认 mode_update 未实现
grep -c 'sessionUpdate:"mode_update"' $D/dist/codebuddy-headless.js   # 0
grep -o '"mode_update"' $D/dist/codebuddy-headless.js | wc -l        # 1（仅 OpenAPI schema）
```

## 附录 B：版本对照

| 项 | 值 |
|----|-----|
| 分析版本 | `@tencent-ai/codebuddy-code` v2.147.0 |
| `--acp` 实际加载 | `dist/codebuddy-headless.js`（`bin/codebuddy` 中 `args.includes('--acp')` → `isHeadless`） |
| 内置 ACP SDK | `dist-server/8569.codebuddy.js`（bundled `@agentclientprotocol/sdk`） |
| ClawBench 侧 ACP SDK | `github.com/coder/acp-go-sdk v0.13.5` |
| ACP 规范参考 | `~/projects/agent-client-protocol/schema/v1/schema.json` |

> 复核时若方法清单与本文档不符，以 bundle 实际内容为准，并更新本文件。
