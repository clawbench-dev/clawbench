# @resume — ACP 会话恢复设计

## 概述

支持 `@resume` 命令，用于恢复当前智能体的历史 ACP 会话。仅 ACP 传输且具备 `LoadSession` 能力的 agent 可用。用户输入 `@resume` 后，前端通过 ACP `ListSessions` 获取会话列表，在 BottomSheet 中结构化展示，用户点击确认后通过 `LoadSession` 恢复会话到新 ClawBench 会话，重放消息全部存入数据库。

## 用户操作流程

1. 用户在聊天输入栏输入 `@resume`（仅 ACP + LoadSession 能力的 agent 在 autocomplete 中展示）
2. 前端拦截消息发送（不发到后端聊天流），改为调用 `GET /api/agents/{agentId}/acp-sessions`
3. BottomSheet 弹出展示 ACP 会话列表（title、updatedAt、cwd）
4. 用户点击某条会话 → 弹出 DialogOverlay 确认（"恢复此会话？将创建新会话并加载历史消息"）
5. 用户确认 → 前端调用 `POST /api/ai/session/acp-load`，展示全屏遮罩 "正在恢复会话..."
6. 后端创建新 ClawBench 会话，执行 `LoadSession`，收集重放消息批量写入 `chat_history`
7. 请求完成后前端关闭遮罩，`switchSession` → `loadHistory` 一次性展示所有消息

**关键约束**：`@resume` 不向聊天发送任何文本消息，纯 UI 交互命令。

## 后端 API

### GET /api/agents/{agentId}/acp-sessions

获取 ACP agent 的会话列表。

- **请求参数**：`?cursor=xxx`（可选分页）
- **后端逻辑**：
  1. 校验 agent 存在且为 ACP 传输且具有 LoadSession 能力
  2. 通过 `ACPConnManager.GetConn()` 获取活跃 ACP 连接
  3. 调用 `conn.ListSessions()` 获取 `[]acp.SessionInfo`
  4. 过滤掉当前活跃的 `acpSID`（避免恢复到自身）
  5. 返回给前端
- **响应格式**：
```json
{
  "sessions": [
    {
      "sessionId": "abc-123",
      "title": "Fix auth bug",
      "cwd": "/home/user/project",
      "updatedAt": "2026-06-10T08:30:00Z"
    }
  ],
  "nextCursor": "xyz-456"
}
```

### POST /api/ai/session/acp-load

创建新会话并执行 LoadSession 恢复 ACP 会话。**同步请求**（非 SSE）。

- **请求体**：
```json
{
  "agentId": "agent-1",
  "acpSessionId": "abc-123",
  "projectId": "project-1"
}
```
- **后端逻辑**：
  1. 校验 agent 存在且支持 LoadSession
  2. 创建新 `ChatSession`：
     - `Backend: "acp"`, `AgentID: agentId`
     - `SourceSessionID: "acp:{acpSessionId}"`（前缀区分来源）
  3. 通过 `ACPConnManager.GetOrCreateConn()` 为新会话创建 ACP 连接
  4. `ensureAliveWithSession()` 检测 acp-load 标记 → 调用 `LoadSession` 而非 `NewSession`/`ResumeSession`
  5. `conn.LoadSession(sessionId, cwd)` → 返回 `{Modes, ConfigOptions}`
  6. 缓存 LoadSession 返回的 mode/config 状态
  7. 监听 `SessionUpdate` 通知，收集所有重放消息
  8. 将消息批量写入 `chat_history`
  9. 收到完成信号或超时后，返回 `{ sessionId: "new-clawbench-session-id" }`
- **超时**：60 秒，超时后返回已收集消息 + 警告
- **并发控制**：同一 agent 同时只允许一个 LoadSession 操作，后续请求返回 409

## LoadSession 能力检测

- `AgentCapabilities.LoadSession` 由 ACP `Initialize` 响应返回
- `AgentCapability` 结构体新增 `LoadSession bool` 字段
- 持久化到 `agents` 表新增列 `acp_load_session`
- `AgentCapabilityRegistry.GetLoadSession()` 返回能力状态
- `GET /api/agents` 响应中包含 `loadSession` 字段
- 前端据此决定 `@resume` 是否在 autocomplete 中展示
- 非 ACP agent 或无 LoadSession 能力时，`@resume` 不可见，手动输入时 toast 提示不支持

## 前端组件

### ChatInputBar 修改

在 `@` 命令 autocomplete 列表新增 `@resume`，条件为当前会话的 agent 是 ACP 传输且具有 `LoadSession` 能力。

用户选择或输入 `@resume ` 后，拦截消息发送，触发 AcpSessionDrawer。

### AcpSessionDrawer 组件（新增）

BottomSheet 组件，展示 ACP 会话列表：

- **列表项**：title（主行）、cwd + updatedAt（副行）
- **空状态**：agent 无历史会话时展示提示 "无历史会话"
- **加载状态**：skeleton / spinner
- **分页**：通过 `nextCursor` 支持滚动到底部自动加载更多
- **点击行为**：弹出 `DialogOverlay` 确认 → 调用 `POST /api/ai/session/acp-load`

### useAcpSession composable（新增）

- `loadAcpSessions(agentId, cursor?)` — 调用 `GET /api/agents/{agentId}/acp-sessions`
- `acpLoadSession(agentId, acpSessionId, projectId)` — 调用 `POST /api/ai/session/acp-load`
- `acpSessions` ref — 列表数据
- `acpSessionsLoading` ref — 加载状态
- `acpResuming` ref — 恢复中状态（控制遮罩）
- `nextCursor` ref — 分页游标

### @resume 消息拦截

在 `ChatInputBar` 的发送逻辑中，检测消息以 `@resume` 开头时：
1. 不调用后端 chat API
2. 清空输入框
3. 打开 `AcpSessionDrawer` BottomSheet
4. 触发 `loadAcpSessions()` 加载列表

### Badge 渲染

`contentBlocks.ts` 的 `AT_COMMAND_RE` 添加 `@resume`，使其在用户消息中渲染为紫色 badge（fallback 显示）。

### 加载遮罩

- **触发**：用户确认恢复 → `acpResuming = true`，关闭 BottomSheet，展示遮罩 "正在恢复会话..."
- **关闭**：`POST /api/ai/session/acp-load` 成功返回后，`acpResuming = false`，`switchSession` 一次性加载消息
- **超时提示**：15 秒无响应追加 "恢复较慢，请稍候..."
- **错误**：请求失败时关闭遮罩，toast 提示错误

### Store 变更

`stores/app.ts` 新增 `acpSessionsOpen` ref 控制 BottomSheet 显示。

## ACP 连接管理

### LoadSession 分支

`ensureAliveWithSession()` 需新增 LoadSession 分支：
- 当会话标记为 `acp-load` 类型且有 `acpSessionId` 时，调用 `LoadSession` 而非 `NewSession`/`ResumeSession`
- `spawnLocked()` 启动 agent 进程后，执行 `conn.LoadSession(sessionId, cwd, mcpservers)`
- 缓存 `LoadSessionResponse{Modes, ConfigOptions}`
- 后续行为与 `NewSession` 一致（可以正常 Prompt）

### 消息收集

LoadSession 返回后，agent 通过 `SessionUpdate` 通知重放消息。后端需：
1. 在 `ClawBenchACPClient` 中识别 LoadSession 重放的消息（与正常 Prompt 消息区分）
2. 收集所有重放消息到缓冲区
3. 检测重放完成（agent 发送完成信号或超时）
4. 批量写入 `chat_history` 表

## 错误处理

| 场景 | 处理方式 |
|------|---------|
| Agent 不支持 ListSessions | 返回 501，toast "该智能体不支持会话列表" |
| Agent 不支持 LoadSession | `@resume` 不在 autocomplete，手动输入时 toast 提示 |
| ACP 连接未建立/已断开 | 尝试重连，失败则 toast "智能体连接不可用" |
| LoadSession 调用失败 | 返回错误，toast "会话恢复失败" |
| 重放超时（60s） | 返回已收集消息 + 警告，前端提示部分消息可能缺失 |
| 恢复的会话是当前活跃会话 | 列表中过滤掉当前 `acpSID` |
| 同时多个恢复请求 | 同一 agent 返回 409 |
| Agent 进程在重放中崩溃 | 返回已收集消息 + 错误提示 |
| 空列表 | BottomSheet 展示 "无历史会话" |

## 边界情况

- **重复恢复**：同一 ACP 会话可被多次恢复（每次创建新 ClawBench 会话）
- **项目隔离**：LoadSession 传入当前项目 cwd
- **sourceSessionId**：格式 `acp:{acpSessionId}`，与定时任务的 `sourceSessionId` 共存，可渲染紫色 "恢复" badge

## 与现有功能的关系

- **SessionDrawer**：恢复的会话正常展示，带 `sourceSessionId` badge
- **Continue Conversation**：恢复的会话后续可正常使用"继续对话"
- **AutoResumeBackend**：`@resume` 仅适用于 ACP，与 AutoResumeBackend 无关
- **session_resume**：现有 `POST /api/ai/session/resume` 是恢复软删除的 ClawBench 会话，与 ACP LoadSession 无关

## 文件变更清单

### 后端 Go

| 文件 | 变更 |
|------|------|
| `internal/model/agent.go` | `AgentCapability` 新增 `LoadSession bool` |
| `internal/ai/agent_capability.go` | 新增 `GetLoadSession()`，`ForceUpdate()` 提取能力，持久化 |
| `internal/ai/acp_pool.go` | `ACPConn` 新增 `ListSessions()` 方法，`ensureAliveWithSession()` 新增 LoadSession 分支 |
| `internal/ai/acp_backend.go` | `cacheNewSessionState()` 提取 LoadSession 能力 |
| `internal/ai/acp_client.go` | 识别 LoadSession 重放消息，支持消息收集回调 |
| `internal/handler/agent.go` | 新增 `ServeACPSessions` handler |
| `internal/handler/chat.go` | 新增 `ServeACPLoadSession` handler |
| `internal/service/database.go` | 新增 migration：`agents` 表新增 `acp_load_session` 列 |
| `cmd/server/main.go` | 注册新路由 |

### 前端 Vue

| 文件 | 变更 |
|------|------|
| `web/src/utils/contentBlocks.ts` | `AT_COMMAND_RE` 添加 `@resume` |
| `web/src/components/chat/ChatInputBar.vue` | autocomplete 添加 `@resume`，拦截发送 |
| `web/src/composables/useAcpSession.ts` | **新增** |
| `web/src/components/chat/AcpSessionDrawer.vue` | **新增** |
| `stores/app.ts` | 新增 `acpSessionsOpen` ref |

### Mock agent

| 文件 | 变更 |
|------|------|
| `cmd/acp-mock/main.go` | 实现 `ListSessions` 和 `LoadSession` handler |
