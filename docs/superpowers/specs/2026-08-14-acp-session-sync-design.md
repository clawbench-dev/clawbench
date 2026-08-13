# 设计文档：ACP 会话增量同步按钮

- 日期：2026-08-14
- 状态：已批准

## 目标

在 ACP 模式下，为当前会话提供"同步"能力：当外部（agent 侧）会话在 ClawBench 之外被更新时，按钮触发 LoadSession 回放，**增量**地把本地缺失的外部消息追加到当前 ClawBench 会话，已存在的消息保持不变。

## 需求要点

1. 同步按钮位于 ChatInputBar 的 actionbar（`chat-top-actions`）。
2. 图标为双向双箭头（lucide `ArrowRightLeft`）。
3. **仅 ACP 传输（acp-stdio）时展示**，不论当前是哪个会话。
4. 会话运行中 / 同步进行中时禁用。
5. 无 ACP 会话时禁用并提示（后端返回 `NoAcpSession`）。
6. 增量：按外部 messageId 去重，仅追加本地缺失消息。

## 关键事实（代码核对）

- ACP 会话的规范关联字段是 `chat_sessions.external_session_id`（`acp_pool.go:262` 用它预填 `conn.acpSID` 做 ResumeSession）。`source_session_id`（`acp:{id}`）仅是 acp-load 的辅助跟踪。
- ACP 消息带 `messageId`（UUID，`SessionUpdate*MessageChunk.MessageId`），但 `chat_history` 当前未持久化。
- `chat_history` 结构：本地自增 `id`，无外部消息 ID 列。
- `ensureAliveWithSession`（`acp_conn_lifecycle.go:90`）对"已存活 + 有 acpSID"的连接提前返回，不会重新回放。因此同步需在存活连接上**强制触发一次 LoadSession 回放**。
- `ServeACPLoadSession`（`session_resume.go:144`）现有回放分组/持久化逻辑可抽取复用（含 tool calls 持久化）。
- schema 迁移机制：`internal/service/database.go` 中守卫式 `ALTER TABLE ADD COLUMN`。

## 后端设计（Go）

### 1. Schema 迁移

`chat_history` 新增列 `external_message_id TEXT DEFAULT ''`。在 `internal/service/database.go` 迁移区加一条守卫式 `ALTER TABLE chat_history ADD COLUMN external_message_id TEXT DEFAULT ''`。

### 2. 回放逻辑抽公用

把 `ServeACPLoadSession`（`session_resume.go:298-409`）中的"读 load 缓冲 → 按 role 分组 → 捕获 tool calls → 批量插入"抽成可复用函数，并顺带把每条消息的 ACP `messageId` 写入新列。acp-load 与 acp-sync 共用同一套分组/持久化逻辑。

### 3. 新端点 `POST /api/ai/session/acp-sync`

`ServeACPSyncSession`：

1. 校验 POST 方法、项目 cookie。
2. 请求体 `{ agentId, sessionId }`。
3. 校验 agent 存在、支持 ACP（`SupportsACP`）、支持 LoadSession（`spec.ACPLoadSession`）。
4. 校验会话存在且属于当前项目。
5. 解析 ACP 会话 ID：优先用现有连接 `conn.acpSID`，否则查 `external_session_id`；都为空 → `400 NoAcpSession`。
6. 强制回放：新增 `ACPConn.SyncLoadSession(ctx, cwd, acpSID)`——设 `loadSessionActive` → 调 `conn.LoadSession` → 收集到 load 缓冲（复用 `recoverViaLoadSession` 的收集机制，但允许在存活连接上触发）。
7. 等待 500ms 收迟到通知 → `ClearLoadSessionActive` → 读缓冲 → 用抽出的公用函数分组（带 messageId）。
8. 查询该会话现有 `external_message_id` 集合，仅插入缺失的消息（含 tool calls 持久化）。
9. 返回 `{ ok: true, added: n }`，并 `ws.EmitToSession` 触发前端刷新。

## 前端设计（Vue 3）

### 1. `ChatInputBar.vue`

在 `chat-top-actions` 加按钮：

- 图标：lucide `ArrowRightLeft`，`size=14`。
- 显示条件：`isACP`（当前传输为 acp-stdio）。
- 禁用条件：会话运行中（`chatRunning`）或同步进行中。
- 点击：`emit('sync-acp-session')`。

### 2. `useAcpSession.ts`

新增 `acpSyncSession(sessionId)`：

- POST `/api/ai/session/acp-sync`，返回 `{ added }`。
- 成功：触发重新加载当前会话历史 + toast 提示新增条数。
- `NoAcpSession`：toast "当前会话尚未关联 ACP 会话"。

### 3. 事件链路

`ChatPanelContent.vue` 转发 `sync-acp-session` → `App.vue` 调 `acpSyncSession` 并刷新消息。

## 测试

- Go：`session_resume_sync_test.go` —— 端点鉴权 / 项目归属 / NoAcpSession / 仅追加缺失 / 工具调用持久化 / 已存在消息不变。
- Go：`acp_pool` 的 `SyncLoadSession` 存活连接强制回放。
- 迁移测试：新列存在。
- 前端：`ChatInputBar` 按钮显隐 / 禁用 / 触发；`useAcpSession.acpSyncSession` 成功与 NoAcpSession 分支。
