# ACP 会话增量同步 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 ACP 模式下为当前会话提供"同步"按钮，通过 LoadSession 回放把外部新增消息增量合并进本地会话（按外部 messageId 去重，已存在消息不变）。

**Architecture:** 后端新增 `POST /api/ai/session/acp-sync` 端点：解析当前会话的 ACP 会话 ID（`external_session_id` / 活动连接 `acpSID`），复用 ACP 连接强制触发一次 LoadSession 回放，把回放缓冲按 role 分组并捕获每条消息的 ACP `messageId`，与本地 `chat_history.external_message_id` 对比后仅插入缺失消息。前端在 ChatInputBar actionbar 增加双向双箭头按钮，ACP 模式展示，点击调用新端点并在响应后重新加载当前会话历史。

**Tech Stack:** Go（net/http、sqlite、acp-go-sdk）、Vue 3 + TypeScript、lucide-vue-next、Vitest。

**设计文档:** `docs/superpowers/specs/2026-08-14-acp-session-sync-design.md`

---

## 文件结构

**后端**
- `internal/service/database.go` — `chat_history` 增列 `external_message_id`
- `internal/handler/session_resume.go` — 抽取回放分组/持久化公用函数，acp-load 顺带写 `external_message_id`
- `internal/handler/session_sync.go`（新建）— 分组函数 + `ServeACPSyncSession`
- `internal/handler/handler.go` — 注册路由
- `internal/ai/acp_pool.go` — 新增 `SyncLoadSession`、`GetAcpSessionID`、`SetLoadSessionActiveForTest`
- `internal/handler/session_sync_test.go`（新建）
- `internal/ai/acp_pool_test.go`（新增 SyncLoadSession 测试）

**前端**
- `web/src/composables/useAcpSession.ts` — 新增 `acpSyncSession`
- `web/src/components/chat/ChatInputBar.vue` — actionbar 按钮
- `web/src/components/chat/ChatPanelContent.vue` — 转发 `sync-acp-session`
- `web/src/App.vue` — 处理事件并刷新历史
- `web/src/composables/__tests__/useAcpSession.test.ts`
- `web/src/components/chat/__tests__/ChatInputBar.test.ts`

---

### Task 1: Schema 迁移 — `chat_history.external_message_id`

**Files:**
- Modify: `internal/service/database.go:213-224`（CREATE TABLE）
- Modify: `internal/service/database.go:180-195` 附近（pre-migration 区）
- Test: `internal/service/database_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/service/database_test.go` 新增测试，断言迁移后 `chat_history` 含 `external_message_id` 列：

```go
func TestMigrateAddsExternalMessageID(t *testing.T) {
	// 用不含该列的旧 schema 建表后触发迁移
	_, err := WriteExec(`CREATE TABLE chat_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_path TEXT NOT NULL, role TEXT NOT NULL,
		content TEXT NOT NULL, session_id TEXT,
		backend TEXT NOT NULL DEFAULT 'claude',
		streaming INTEGER NOT NULL DEFAULT 0,
		indexed INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)
	require.NoError(t, MigrateDatabaseForTest())

	var cnt int
	err = ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='external_message_id'").Scan(&cnt)
	require.NoError(t, err)
	assert.Equal(t, 1, cnt)
}
```

> 若 `database_test.go` 已有现成迁移辅助函数（如 `MigrateDatabaseForTest`），用其现有命名；否则在 `TestMain`/现有 `createTables` 调用链中复用实际迁移入口。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/service/ -run TestMigrateAddsExternalMessageID -v`
Expected: FAIL（列不存在，`cnt == 0`）

- [ ] **Step 3: 实现迁移**

在 `internal/service/database.go` 的 pre-migration 区（`indexed` 列迁移之后，约 195 行后）加入守卫式 ALTER：

```go
	// chat_history.external_message_id — external ACP messageId for incremental ACP sync dedup
	var hasExternalMsgID int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='external_message_id'").Scan(&hasExternalMsgID)
	if hasExternalMsgID == 0 {
		if _, err := WriteExec("ALTER TABLE chat_history ADD COLUMN external_message_id TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add external_message_id column: %w", err)
		}
	}
```

并在 `CREATE TABLE IF NOT EXISTS chat_history`（214-224 行）增加列定义，保持新库与迁移后一致：

```go
			indexed INTEGER NOT NULL DEFAULT 0,
			external_message_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/service/ -run TestMigrateAddsExternalMessageID -v`
Expected: PASS（`cnt == 1`）

- [ ] **Step 5: 提交**

```bash
git add internal/service/database.go internal/service/database_test.go
git commit -m "feat: chat_history 增加 external_message_id 列用于 ACP 增量同步去重"
```

---

### Task 2: 抽取回放分组公用函数并让 acp-load 写 messageId

**Files:**
- Create: `internal/handler/session_sync.go`
- Modify: `internal/handler/session_resume.go:298-409`
- Test: `internal/handler/session_sync_test.go`（新建，含分组断言）

- [ ] **Step 1: 写失败测试（分组捕获 messageId）**

在新建 `internal/handler/session_sync_test.go` 中测试分组函数能按 role 分组并捕获首个 messageId：

```go
package handler

import (
	"testing"

	"clawbench/internal/ai"
	"github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupLoadSessionReplay_CapturesMessageID(t *testing.T) {
	client := ai.NewClawBenchACPClient()
	u1 := "uuid-user-1"
	u2 := "uuid-user-2"
	a1 := "uuid-assistant-1"
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: &u1, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hi"}}}}},
		{SessionId: "s", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: &a1, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello"}}}}},
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: &u2, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "again"}}}}},
	})

	msgs := groupLoadSessionReplay(client)
	require.Len(t, msgs, 3)
	assert.Equal(t, "user", msgs[0].role)
	assert.Equal(t, u1, msgs[0].extMsgID)
	assert.Equal(t, "assistant", msgs[1].role)
	assert.Equal(t, a1, msgs[1].extMsgID)
	assert.Equal(t, "user", msgs[2].role)
	assert.Equal(t, u2, msgs[2].extMsgID)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/handler/ -run TestGroupLoadSessionReplay_CapturesMessageID -v`
Expected: FAIL（`groupLoadSessionReplay` 未定义）

- [ ] **Step 3: 实现分组函数**

在 `internal/handler/session_sync.go` 写入分组结构体和函数（抽取自 `session_resume.go:299-409` 的回放分组逻辑，仅增加 messageId 捕获；**保持 role 边界分组不变**，extMsgID 取每组首个非空 messageId）：

```go
package handler

import (
	"encoding/json"
	"log/slog"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// replayMessage 是一条从 LoadSession 回放重建的消息。
type replayMessage struct {
	role      string // strUser or strAssistant
	content   string // JSON: {"blocks":[...], "metadata":{...}}
	extMsgID  string // 外部 ACP messageId
	toolCalls []model.ContentBlock
}

// groupLoadSessionReplay 读取并清空 LoadSession 回放缓冲，按 role 边界分组
// 为消息，捕获每组首个外部 messageId。
func groupLoadSessionReplay(client *ai.ClawBenchACPClient) []replayMessage {
	var messages []replayMessage
	buf := client.GetAndClearLoadSessionBuf()

	var blocks []model.ContentBlock
	var currentRole string
	var currentMsgID string

	flush := func() {
		if len(blocks) == 0 || currentRole == "" {
			return
		}
		blocks = ai.MergeConsecutiveThinkingBlocks(blocks)
		var toolCalls []model.ContentBlock
		for _, b := range blocks {
			if b.Type == strToolUse && b.ID != "" {
				toolCalls = append(toolCalls, b)
			}
		}
		contentMap := map[string]any{strBlocks: blocks}
		if currentRole == strAssistant {
			contentMap["metadata"] = map[string]any{"transport": transportACP}
		}
		contentJSON, _ := json.Marshal(contentMap)
		messages = append(messages, replayMessage{
			role:      currentRole,
			content:   string(contentJSON),
			extMsgID:  currentMsgID,
			toolCalls: toolCalls,
		})
		blocks = nil
		currentMsgID = ""
	}

	for _, n := range buf {
		notifRole := strAssistant
		var notifMsgID string
		switch {
		case n.Update.UserMessageChunk != nil:
			notifRole = strUser
			if n.Update.UserMessageChunk.MessageId != nil {
				notifMsgID = *n.Update.UserMessageChunk.MessageId
			}
		case n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.MessageId != nil:
			notifMsgID = *n.Update.AgentMessageChunk.MessageId
		case n.Update.AgentThoughtChunk != nil && n.Update.AgentThoughtChunk.MessageId != nil:
			notifMsgID = *n.Update.AgentThoughtChunk.MessageId
		}

		if notifRole != currentRole && currentRole != "" {
			flush()
		}
		currentRole = notifRole
		if notifMsgID != "" && currentMsgID == "" {
			currentMsgID = notifMsgID
		}

		if n.Update.UserMessageChunk != nil {
			if text := n.Update.UserMessageChunk.Content.Text; text != nil && text.Text != "" {
				ai.AccumulateBlock(&blocks, ai.StreamEvent{Type: strContent, Content: text.Text})
			}
			continue
		}

		ch := make(chan ai.StreamEvent, 64)
		ai.MapACPSessionUpdateForTest(n.Update, ch)
		close(ch)
		for event := range ch {
			switch event.Type {
			case strContent, "thinking", "thinking_done", strToolUse, "tool_result", "warning", strError:
				ai.AccumulateBlock(&blocks, event)
			}
		}
	}
	flush()
	return messages
}

// persistReplayMessages 批量插入回放消息及其 tool calls，并记录外部 messageId。
// 返回实际插入条数。
func persistReplayMessages(sessionID, projectPath, backend string, messages []replayMessage) int {
	inserted := 0
	for _, msg := range messages {
		res, err := service.WriteExec(
			"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, indexed, external_message_id) VALUES (?, ?, ?, ?, ?, 0, 0, ?)",
			projectPath, backend, sessionID, msg.role, msg.content, msg.extMsgID,
		)
		if err != nil {
			slog.Error("handler: failed to save LoadSession replay message", "error", err)
			continue
		}
		msgID, _ := res.LastInsertId()
		for i := range msg.toolCalls {
			tc := &msg.toolCalls[i]
			inputJSON, _ := json.Marshal(tc.Input)
			if err := service.UpsertToolCall(msgID, sessionID, tc.ID, tc.Name, inputJSON, tc.Output, tc.Status, tc.Summary, tc.Done, tc.DurationMs); err != nil {
				slog.Warn("handler: failed to persist LoadSession replay tool call",
					"session_id", sessionID, "tool_id", tc.ID, "error", err)
			}
		}
		inserted++
	}
	return inserted
}
```

> 常量 `strBlocks/strUser/strAssistant/strContent/strToolUse/strError/transportACP` 已在 `session_resume.go` 顶部定义（包级），`session_sync.go` 同包可直接使用。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/handler/ -run TestGroupLoadSessionReplay_CapturesMessageID -v`
Expected: PASS

- [ ] **Step 5: 改造 acp-load 复用公用函数**

在 `internal/handler/session_resume.go` 中，把 goroutine 内 `session_resume.go:299-409` 的"读缓冲→分组→插入→tool calls"整段替换为：

```go
		// Read buffered notifications and group into messages (capturing
		// external messageId), then persist. acp-load persists the full replay.
		var messages []replayMessage
		if client != nil {
			messages = groupLoadSessionReplay(client)
		}
		persistReplayMessages(sessionID, projectPath, agent.Backend, messages)
```

删除原 `persistedMessage` 结构体、`type persistedMessage struct`、`flushBlocks`、内层循环及 `messages` 的插入/tool call 块。保留标题提取（`session_resume.go:411-425`）不变。

- [ ] **Step 6: 运行既有回放测试确认不回归**

Run: `go test ./internal/handler/ -run 'TestServeACPLoadSession|TestLoadSessionParsing' -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/handler/session_sync.go internal/handler/session_resume.go internal/handler/session_sync_test.go
git commit -m "feat: 抽取 ACP LoadSession 回放分组公用函数并捕获 external messageId"
```

---

### Task 3: ACPConn 新增 SyncLoadSession 强制回放

**Files:**
- Modify: `internal/ai/acp_pool.go`（新增 `SyncLoadSession`、`GetAcpSessionID`、`SetLoadSessionActiveForTest`）
- Test: `internal/ai/acp_pool_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/ai/acp_pool_test.go` 新增：已存在、未处于 load 状态的连接调用 `SyncLoadSession` 时应设置 `loadSessionActive`（跳过 RPC 由测试桩驱动）：

```go
func TestSyncLoadSession_SkipsWhenAlreadyActive(t *testing.T) {
	m := &ACPConnManager{}
	agent := &model.Agent{ID: "a", Backend: "claude"}
	conn := newACPConn(agent, "sid")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("sid", "acp-x")

	// 模拟 GetOrCreateConnForLoad 已触发过 LoadSession（flag=true）→ 应跳过 RPC
	conn.loadSessionActive.Store(true)
	err := conn.SyncLoadSession(context.Background(), "/proj", "acp-x")
	assert.NoError(t, err)
	assert.True(t, conn.loadSessionActive.Load())
}
```

> 若 `newACPConn` 为包内私有、`SetAliveForTest`/`SetSessionMappingForTest` 已有，直接用；否则参考现有 `InjectAliveConnForTest` 的构造方式。若 `SetAliveForTest` 不存在，改用 `InjectAliveConnForTest(m, ...)`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/ai/ -run TestSyncLoadSession -v`
Expected: FAIL（`SyncLoadSession` 未定义）

- [ ] **Step 3: 实现 SyncLoadSession**

在 `internal/ai/acp_pool.go` 新增（放在 `ClearLoadSessionActive` 之后，732 行附近）：

```go
// SyncLoadSession 强制对连接触发一次 LoadSession 回放，即使连接已存活。
// ensureAliveWithSession 对"已存活+有 acpSID"的连接会提前返回，因此同步场景
// 必须显式回放以获取外部最新历史。回放通知被收集到 load 缓冲供调用方持久化。
// 若 loadSessionActive 已为 true（GetOrCreateConnForLoad 刚在全新连接上触发过
// LoadSession），则跳过，避免重复回放。
func (c *ACPConn) SyncLoadSession(ctx context.Context, cwd, acpSID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loadSessionActive.Load() {
		return nil
	}
	c.loadSessionActive.Store(true)
	loadCtx, loadCancel := context.WithTimeout(ctx, 60*time.Second)
	defer loadCancel()
	loadResp, err := c.conn.LoadSession(loadCtx, acp.LoadSessionRequest{
		SessionId:  acp.SessionId(acpSID),
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		c.alive = false
		c.loadSessionActive.Store(false)
		return fmt.Errorf("acp: session/load: %w", err)
	}
	c.acpSID = acpSID
	c.lastLoadSessionResp = &loadResp
	c.lastUsed = time.Now()
	slog.Info("acp conn: SyncLoadSession replay completed", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	return nil
}

// GetAcpSessionID 返回连接当前绑定的 ACP 会话 ID。
func (c *ACPConn) GetAcpSessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acpSID
}

// SetLoadSessionActiveForTest 设置 loadSessionActive，用于测试跳过真实 RPC。
func (c *ACPConn) SetLoadSessionActiveForTest(v bool) {
	c.loadSessionActive.Store(v)
}
```

确认 `acp`、`fmt`、`time`、`slog` 均已 import（`acp_pool.go` 已使用 `acp`、`time`、`slog`；`fmt` 若缺则加）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/ai/ -run TestSyncLoadSession -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/ai/acp_pool.go internal/ai/acp_pool_test.go
git commit -m "feat: ACPConn.SyncLoadSession 在存活连接上强制回放以同步外部历史"
```

---

### Task 4: ServeACPSyncSession 端点 + 路由

**Files:**
- Modify: `internal/handler/session_sync.go`（加 `ServeACPSyncSession`）
- Modify: `internal/handler/handler.go:246`（注册路由）
- Test: `internal/handler/session_sync_test.go`

- [ ] **Step 1: 写失败测试（NoAcpSession + 归属校验）**

在 `internal/handler/session_sync_test.go` 新增：

```go
func TestServeACPSyncSession_NoAcpSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// 创建会话但 external_session_id 为空、无活动连接
	sid := createSessionForProject(t, env.ProjectDir, agentID)

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, "NoAcpSession", body["msgKey"])
}

func TestServeACPSyncSession_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	sid := createSessionForProject(t, env.ProjectDir, agentID)
	service.UpdateExternalSessionID(sid, "acp-xyz")

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, "/other/project") // 不同项目
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

> 若无现成 `createSessionForProject` helper，用 `service.CreateSession(env.ProjectDir, "claude", "", agentID, "", "default", "chat")` 创建并保留返回的 sid。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/handler/ -run TestServeACPSyncSession -v`
Expected: FAIL（`ServeACPSyncSession` 未定义）

- [ ] **Step 3: 实现 ServeACPSyncSession**

在 `internal/handler/session_sync.go` 追加（复用 `getOrCreateConnForLoad`、`withProjectCookie` 测试注入的 conn 桩）：

```go
// ServeACPSyncSession handles POST /api/ai/session/acp-sync — 复用当前会话的
// ACP 连接强制 LoadSession 回放，按 external messageId 增量合并外部新增消息到
// 当前会话，已存在消息保持不变。返回新增条数。
//
//nolint:gocognit // orchestration 顺序性高，拆分反而难读
func ServeACPSyncSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
		return
	}

	var req struct {
		AgentID   string `json:"agentId"`
		SessionID string `json:"sessionId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AgentID == "" || req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.RLock()
	agent, ok := model.Agents[req.AgentID]
	configMutex.RUnlock()
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}
	if !agent.SupportsACP() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	spec := model.FindSpecByBackend(agent.Backend)
	if spec == nil || !spec.ACPLoadSession {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return
	}

	// 校验会话归属当前项目，并读取 external_session_id
	var sessProject, extID string
	err := service.ReadDB().QueryRowContext(
		r.Context(),
		"SELECT project_path, external_session_id FROM chat_sessions WHERE id = ?",
		req.SessionID,
	).Scan(&sessProject, &extID)
	if errors.Is(err, sql.ErrNoRows) {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}
	if sessProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// 解析 ACP 会话 ID：优先活动连接 acpSID，其次 external_session_id
	acpSID := extID
	if acpSID == "" {
		if conn := ai.GetACPConnManager().GetConn(req.SessionID); conn != nil {
			acpSID = conn.GetAcpSessionID()
		}
	}
	if acpSID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoAcpSession")
		return
	}

	// 复用连接（全新连接会在此触发 LoadSession；已存活连接提前返回）
	conn, err := getOrCreateConnForLoad(r.Context(), agent, req.SessionID, acpSID, projectPath)
	if err != nil {
		if ai.IsACPResourceNotFound(err) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ACPSessionNotFound")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 强制回放（已存活连接在此触发 LoadSession）
	if err := conn.SyncLoadSession(r.Context(), projectPath, acpSID); err != nil {
		if ai.IsACPResourceNotFound(err) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ACPSessionNotFound")
			return
		}
		slog.Error("handler: SyncLoadSession failed", "session_id", req.SessionID, "acp_sid", acpSID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 等待迟到通知进入缓冲，再读取
	time.Sleep(500 * time.Millisecond)
	conn.ClearLoadSessionActive()

	var messages []replayMessage
	if client := conn.GetClient(); client != nil {
		messages = groupLoadSessionReplay(client)
	}

	// 计算本地已有 external_message_id 集合
	existing := map[string]struct{}{}
	rows, err := service.ReadDB().Query(
		"SELECT external_message_id FROM chat_history WHERE session_id = ? AND external_message_id != ''",
		req.SessionID,
	)
	if err != nil {
		slog.Error("handler: failed to query existing external_message_id", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	for rows.Next() {
		var mid string
		if err := rows.Scan(&mid); err == nil {
			existing[mid] = struct{}{}
		}
	}
	rows.Close()

	// 仅追加缺失（extMsgID 为空的消息不参与增量同步，避免重复）
	var toPersist []replayMessage
	for _, m := range messages {
		if m.extMsgID == "" {
			continue
		}
		if _, dup := existing[m.extMsgID]; dup {
			continue
		}
		toPersist = append(toPersist, m)
	}
	added := persistReplayMessages(req.SessionID, projectPath, agent.Backend, toPersist)

	slog.Info("handler: acp-sync completed",
		"session_id", req.SessionID, "agent", req.AgentID, "acp_sid", acpSID,
		"replayed", len(messages), "added", added)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"added": added,
	})
}
```

- 需要确认已有符号：
- `middleware.GetProjectFromCookie`、`writeLocalizedError`、`writeLocalizedErrorf`、`decodeJSON`、`writeJSON`、`configMutex`、`model.FindSpecByBackend`、`ai.IsACPResourceNotFound`、`transportACP` — 均已在 handler 包使用。
- `writeLocalizedErrorf(..., "NoAcpSession")` 会把 `MsgKey` 设为 `NoAcpSession`，与测试断言一致；前端据 `msgKey` 提示。
- `conn.GetClient()` — 确认 ACPConn 有该方法（`session_resume.go:269` 用了 `conn.GetClient()`）。
- `errors`、`sql`、`time` import 需加入 `session_sync.go`。

- [ ] **Step 4: 注册路由**

在 `internal/handler/handler.go:246` 之后追加：

```go
	register("/api/ai/session/acp-sync", middleware.Auth(ServeACPSyncSession))
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/handler/ -run TestServeACPSyncSession -v`
Expected: PASS

- [ ] **Step 6: 新增"仅追加缺失"测试**

在 `internal/handler/session_sync_test.go` 新增（复用 `newACPReplayConn` 桩，模拟回放两条消息，其中一条本地已有）：

```go
func TestServeACPSyncSession_IncrementalMerge(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := setupACPReplayAgent(t)
	// 会话 + 已有消息（external_message_id = m1）
	sid := createSessionForProject(t, env.ProjectDir, agentID)
	service.UpdateExternalSessionID(sid, "acp-1")
	_, err := service.WriteExec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', 'existing', 'm1')",
		env.ProjectDir, sid,
	)
	require.NoError(t, err)

	// 回放：m1（已存在）+ m2（新增）
	restore := newACPReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{SessionId: "acp-1", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: strPtr("m1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "existing"}}}}},
		{SessionId: "acp-1", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: strPtr("m2"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "newly added"}}}}},
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct{ Added int `json:"added"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 1, resp.Added)

	// 总数 = 原有1 + 新增1
	var cnt int
	_ = service.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt)
	assert.Equal(t, 2, cnt)
}

func strPtr(s string) *string { return &s }
```

运行: `go test ./internal/handler/ -run TestServeACPSyncSession_IncrementalMerge -v`
Expected: PASS（`added == 1`，总数为 2）

> 注意 `newACPReplayConn` 返回的 conn 其 `loadSessionActive` 初始为 false，`SyncLoadSession` 会走 RPC 分支导致 nil `c.conn` 报错。在测试桩里需让 conn 的 `loadSessionActive` 为 true：在 `newACPReplayConn` 基础上，测试内调用 `conn.SetLoadSessionActiveForTest(true)` 后再跑端点。因此 Step 6 代码中 `restore` 改为：

```go
	conn := newSyncReplayConn(t, model.Agents[agentID], []acp.SessionNotification{...})
	defer conn.restore()
```

并在 `session_sync_test.go` 增加 helper：

```go
// newSyncReplayConn 同 newACPReplayConn，但预置 loadSessionActive=true 以跳过
// SyncLoadSession 的真实 RPC，直接使用预填充的回放缓冲。
func newSyncReplayConn(t *testing.T, agent *model.Agent, buf []acp.SessionNotification) struct{ restore func() } {
	t.Helper()
	conn := ai.NewACPConnForTest(agent, "dummy-clawbench-sid")
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest(buf)
	conn.SetClientForTest(client)
	conn.SetLoadSessionActiveForTest(true)

	orig := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(_ context.Context, _ *model.Agent, _, _, _ string) (*ai.ACPConn, error) {
		return conn, nil
	}
	return struct{ restore func() }{restore: func() { getOrCreateConnForLoad = orig }}
}
```

- [ ] **Step 7: 提交**

```bash
git add internal/handler/session_sync.go internal/handler/handler.go internal/handler/session_sync_test.go
git commit -m "feat: 新增 POST /api/ai/session/acp-sync 增量合并外部 ACP 消息"
```

---

### Task 5: 前端 composable — useAcpSession.acpSyncSession

**Files:**
- Modify: `web/src/composables/useAcpSession.ts`
- Test: `web/src/composables/__tests__/useAcpSession.test.ts`

- [ ] **Step 1: 写失败测试**

在 `web/src/composables/__tests__/useAcpSession.test.ts` 新增 describe（沿用现有 mock fetch 模式）：

```ts
describe('acpSyncSession', () => {
  it('returns added count on success', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ok: true, added: 3 }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const { acpSyncSession } = useAcpSession({ currentAgentId: ref('agent1') })
    const result = await acpSyncSession('sid-1')
    expect(result).toEqual({ added: 3 })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/ai/session/acp-sync')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ agentId: 'agent1', sessionId: 'sid-1' })
    vi.unstubAllGlobals()
  })

  it('returns null and shows toast on NoAcpSession', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ msgKey: 'NoAcpSession' }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const toast = useToast()
    const showSpy = vi.spyOn(toast, 'show')
    const { acpSyncSession } = useAcpSession({ currentAgentId: ref('agent1') })
    const result = await acpSyncSession('sid-1')
    expect(result).toBeNull()
    expect(showSpy).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useAcpSession.test.ts -t acpSyncSession`
Expected: FAIL（`acpSyncSession` 不存在）

- [ ] **Step 3: 实现 acpSyncSession**

在 `web/src/composables/useAcpSession.ts` 内新增（返回结构 `{ added: number } | null`，错误分支用 `gt()` 提示，`NoAcpSession` 用专有文案）：

```ts
/**
 * 增量同步当前会话：复用 ACP LoadSession 回放，把外部新增消息合并进本地会话。
 * 返回 { added }；无 ACP 会话返回 null 并提示。
 */
async function acpSyncSession(sessionId: string): Promise<{ added: number } | null> {
  const aid = currentAgentId.value
  if (!aid || !sessionId) return null
  try {
    const resp = await fetch('/api/ai/session/acp-sync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agentId: aid, sessionId }),
    })
    if (!resp.ok) {
      let msgKey = ''
      try {
        const errData = await resp.json()
        msgKey = errData?.msgKey || ''
      } catch { /* ignore */ }
      if (msgKey === 'NoAcpSession') {
        toast.show(gt('chat.acpSession.noAcpSession'), { type: 'info', icon: '🔄' })
      } else {
        toast.show(gt('chat.acpSession.syncFailed'), { type: 'error', icon: '⚠️' })
      }
      return null
    }
    const data = await resp.json()
    return { added: typeof data.added === 'number' ? data.added : 0 }
  } catch (err: unknown) {
    appLog.e(TAG, 'acpSyncSession failed:', err)
    toast.show(gt('chat.acpSession.syncFailed'), { type: 'error', icon: '⚠️' })
    return null
  }
}
```

在 return 对象（`acpLoadSession` 附近，137-146 行）加入 `acpSyncSession`。

- [ ] **Step 4: 加 i18n key**

在 `web/src/i18n` 的中文/英文 locale 文件中，`chat.acpSession` 命名空间下新增：

```ts
noAcpSession: '当前会话尚未关联 ACP 会话',
syncFailed: 'ACP 同步失败',
```

（英文相应为 `This session is not linked to an ACP session` / `ACP sync failed`）

- [ ] **Step 5: 运行测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useAcpSession.test.ts -t acpSyncSession`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add web/src/composables/useAcpSession.ts web/src/composables/__tests__/useAcpSession.test.ts web/src/i18n
git commit -m "feat: useAcpSession.acpSyncSession 调用 acp-sync 端点"
```

---

### Task 6: ChatInputBar actionbar 按钮

**Files:**
- Modify: `web/src/components/chat/ChatInputBar.vue`
- Test: `web/src/components/chat/__tests__/ChatInputBar.test.ts`

- [ ] **Step 1: 写失败测试**

在 `ChatInputBar.test.ts` 新增：ACP 模式（`currentTransport='acp-stdio'`）下显示同步按钮，非 ACP 不显示；点击发出 `sync-acp-session`：

```ts
it('shows sync button in ACP transport and emits sync-acp-session', async () => {
  const wrapper = mount(ChatInputBar, {
    props: {
      currentTransport: 'acp-stdio',
      currentAgentId: 'agent1',
      currentSessionId: 'sid-1',
      chatRunning: false,
      messages: [],
      agents: [],
    },
  })
  const btn = wrapper.find('.chat-action-btn.acp-sync-btn')
  expect(btn.exists()).toBe(true)
  await btn.trigger('click')
  expect(wrapper.emitted('sync-acp-session')).toBeTruthy()
})

it('hides sync button when not ACP transport', () => {
  const wrapper = mount(ChatInputBar, {
    props: { currentTransport: 'cli', currentAgentId: 'agent1', currentSessionId: 'sid-1', chatRunning: false, messages: [] },
  })
  expect(wrapper.find('.chat-action-btn.acp-sync-btn').exists()).toBe(false)
})
```

> 参照现有 `ChatInputBar.test.ts` 的 mount props 最小集合（可能需补 required props），以现有测试为准补全。

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run web/src/components/chat/__tests__/ChatInputBar.test.ts -t "sync"`
Expected: FAIL（无 `acp-sync-btn`）

- [ ] **Step 3: 实现按钮**

在 `web/src/components/chat/ChatInputBar.vue` 模板的 `chat-top-actions`（第 4-35 行）中，`chat-action-group` 之后、auto-speech 按钮之前，插入（仅 ACP transport 展示；会话运行中或同步进行中禁用）：

```html
      <button
        v-if="isACPTransport"
        class="chat-action-btn acp-sync-btn"
        :class="{ disabled: chatRunning || acpSyncing }"
        :disabled="chatRunning || acpSyncing"
        @click="$emit('sync-acp-session')"
        :title="chatRunning ? t('chat.actions.acpSyncRunning') : t('chat.actions.acpSync')"
        :aria-label="t('chat.actions.acpSync')"
      >
        <ArrowRightLeft :size="14" :stroke-width="1.5" />
      </button>
```

- [ ] **Step 4: script 部分**

- 在 lucide import（266 行）加入 `ArrowRightLeft`。
- 在 `defineEmits`（436-457 行）加入 `'sync-acp-session'`。
- 新增状态 `const acpSyncing = ref(false)`；暴露 setter 供父级在同步期间禁用（可选，若父级需驱动则加）：

```ts
const acpSyncing = ref(false)
function setAcpSyncing(v: boolean) { acpSyncing.value = v }
```

> 若父级通过 prop 驱动禁用更贴合现有风格，则改为新增 prop `acpSyncing: Boolean`，由 `ChatPanelContent` 传入。二选一，保持一致。

- [ ] **Step 5: 加 i18n key**

`chat.actions.acpSync`（ACP 同步）、`chat.actions.acpSyncRunning`（会话运行中，暂不可同步）。

- [ ] **Step 6: 运行测试确认通过**

Run: `npx vitest run web/src/components/chat/__tests__/ChatInputBar.test.ts -t "sync"`
Expected: PASS

- [ ] **Step 7: 样式**

在 `web/src/components/chat/ChatInputBar.vue` `<style>` 的 `.chat-action-btn` 区追加 `.acp-sync-btn`（可复用现有 `.chat-action-btn` 样式，无需新增规则；若需区分则补 padding 对齐）。

- [ ] **Step 8: 提交**

```bash
git add web/src/components/chat/ChatInputBar.vue web/src/components/chat/__tests__/ChatInputBar.test.ts web/src/i18n
git commit -m "feat: ChatInputBar actionbar 新增 ACP 同步按钮（双向双箭头）"
```

---

### Task 7: 事件链路 — ChatPanelContent 转发 + App.vue 处理刷新

**Files:**
- Modify: `web/src/components/chat/ChatPanelContent.vue`
- Modify: `web/src/App.vue`
- Test: `web/src/components/chat/__tests__/ChatPanelContent.test.ts`（若存在）

- [ ] **Step 1: ChatPanelContent 转发事件**

在 `ChatInputBar` 的 props/事件绑定处（96-110 行）加：

```html
      @sync-acp-session="$emit('sync-acp-session')"
```

并在其 `defineEmits` 中加入 `'sync-acp-session'`。

- [ ] **Step 2: App.vue 绑定并处理**

在 `ChatPanelContent`（229 行）绑定：

```html
                    @sync-acp-session="handleSyncAcpSession"
```

新增处理函数（放在 `handleAcpSessionSelect` 附近，1055 行后）：

```ts
async function handleSyncAcpSession() {
  const sid = sessionIdentity.currentSessionId.value
  const agentId = sessionIdentity.currentAgentId.value
  if (!sid || !agentId) return
  const acpSync = useAcpSession({ currentAgentId: computed(() => sessionIdentity.currentAgentId.value) })
  const res = await acpSync.acpSyncSession(sid)
  if (res) {
    // 重新加载当前会话历史以显示合并后的外部消息
    await chatPanel.loadHistory?.(false, true, true)
    if (res.added > 0) {
      toast.show(t('chat.acpSession.synced', { count: res.added }), { type: 'success', icon: '🔄' })
    } else {
      toast.show(t('chat.acpSession.syncedNone'), { type: 'info', icon: '🔄' })
    }
  }
}
```

- 需要 import `useAcpSession`（若未引入）与 `computed`。
- 需要能调用当前会话历史刷新。若 `ChatPanelContent` 通过 ref 暴露 `loadHistory`，用 `chatPanelRef.value.loadHistory(...)`；否则改为通过 `sessionIdentity` 内部机制。查看现有 App.vue 是否持有 `chatPanelRef`（`handleReconnect` 用过类似刷新，见 866-868 行）。若没有 ref，改用 `window.dispatchEvent` 或复用现有刷新入口。**以现有可用的刷新机制为准**。
- 新增 i18n `chat.acpSession.synced`（已同步 {{count}} 条新消息）与 `chat.acpSession.syncedNone`（本地已是最新）。

- [ ] **Step 3: 运行前端类型检查与相关测试**

Run: `cd web && npx vue-tsc --noEmit`
Expected: 通过

Run: `npx vitest run web/src/components/chat/__tests__/ChatInputBar.test.ts`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add web/src/components/chat/ChatPanelContent.vue web/src/App.vue web/src/i18n
git commit -m "feat: 接通 ACP 同步按钮事件链路并在完成后刷新会话历史"
```

---

### Task 8: 全量校验

**Files:**（无新文件）

- [ ] **Step 1: 后端测试**

Run: `go build ./... && go test ./internal/handler/... ./internal/ai/... ./internal/service/...`
Expected: 全部 PASS

- [ ] **Step 2: 前端测试 + lint**

Run: `cd web && npx vitest run && npm run lint`
Expected: 全部 PASS

- [ ] **Step 3: 推送前全量检查**

Run: `./scripts/pre-push-checks.sh --skip-coverage`
Expected: 全部 PASS

- [ ] **Step 4: 人工冒烟**

1. 启动服务，选择 ACP 后端（如 claude）的会话。
2. 在 ACP 模式下确认 actionbar 出现双向双箭头按钮；会话运行中按钮禁用。
3. 外部（另一工具）向同一 ACP 会话追加消息后，点按钮 → toast 提示新增条数，本地消息列表出现外部新消息，已有消息不变。
4. 新建未发消息的 ACP 会话 → 按钮点击提示"当前会话尚未关联 ACP 会话"。

---

## Self-Review 记录

- **Spec 覆盖**：schema（Task 1）、回放抽取+messageId（Task 2）、SyncLoadSession（Task 3）、端点+增量合并（Task 4）、前端 composable（Task 5）、按钮（Task 6）、事件链路刷新（Task 7）、校验（Task 8）。按钮显隐/禁用、NoAcpSession 提示均已覆盖。
- **类型一致**：`replayMessage{role, content, extMsgID, toolCalls}`、`groupLoadSessionReplay`、`persistReplayMessages`、`SyncLoadSession`、`acpSyncSession`、`handleSyncAcpSession` 在任务间命名一致。
- **待确认项**（实施时以现有代码为准）：
  - `newACPReplayConn`/`createSessionForProject` helper 是否已存在，或按现有测试模式补建。
  - App.vue 刷新当前会话历史的可用入口（`chatPanelRef` 是否存在）。
  - `chatHistoryExists`/`createTables` 之外的迁移入口是否含 `chat_history`。
