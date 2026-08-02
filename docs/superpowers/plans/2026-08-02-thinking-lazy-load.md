# Thinking 独立存储与懒加载 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把深度思考（thinking）文本从 `chat_history.content` 拆到新表 `chat_thinking`，会话打开时不再传输 thinking 文本，前端展开 thinking 块时才按 `think_id` 懒加载。

**Architecture:** 完全镜像既有 tool-call 拆分模式：内容 JSON 里 thinking 块保留原位但去 `text`、加 `think_id`；`chat_thinking` 表存文本；写库路径（`SessionExecutor.Finalize`）与启动迁移负责拆分；前端 `stableBlockKey` 用 `think_id`，展开时经新 API 懒加载并缓存。WS 流式路径（`result.Blocks`）保持全量不变。fork/续会话同时复制 `chat_tool_calls` 与 `chat_thinking`（顺带修 fork 工具详情 404 存量 bug）。

**Tech Stack:** Go (database/sql + modernc.org/sqlite)、Vue 3 + TypeScript、Vitest。

---

## 参考文件

- 现有模式：`internal/service/tool_calls.go`（CRUD）、`internal/service/database.go:996`（工具迁移）、`internal/handler/chat_history.go:216`（ServeToolCallDetail）、`web/src/composables/useToolDetailDrawer.ts`（前端懒加载）
- 规格：`docs/superpowers/specs/2026-08-02-thinking-lazy-load-design.md`
- 测试数据库初始化：`internal/service/tool_calls_test.go:165`（`initTestDB` → `InitDB` 真实 schema）、`internal/service/database_test.go:2133`（迁移专用 schema）、`internal/handler/testutil_test.go:217`（handler schema）、`internal/service/scheduler_executor_test.go:104`（executor schema）
- 测试基建：service 测试用 `setupDB(t)` + `schema` 常量（`chat_test.go:23`）；executor 测试用 `setupExecutorDB(t)` + `setupExecutorSession(t, agentID)`（`session_executor_test.go:859`）；handler 测试用 `setupTestEnv(t)` / `newRequest` / `callHandler` / `withProjectCookie` / `assertOK`

---

### Task 1: chat_thinking 表 + service CRUD

**Files:**
- Modify: `internal/service/database.go:307-323`（createTables，chat_tool_calls 之后）
- Create: `internal/service/thinking.go`
- Test: `internal/service/thinking_test.go`

- [ ] **Step 1: 在 createTables 加表**

在 `internal/service/database.go` 的 `chat_tool_calls` 索引（`:323`）之后追加：

```sql
-- Thinking block detail storage (text split from chat_history.content for performance)
CREATE TABLE IF NOT EXISTS chat_thinking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    think_id TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(think_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
```

**同时**把 `chat_tool_calls` + `chat_thinking` 建表 SQL 追加到 `internal/service/chat_test.go:23` 的 `schema` 常量末尾（`CREATE TABLE IF NOT EXISTS`，供 `setupDB(t)` 的 fork/continue/purge 测试使用）：

```sql
CREATE TABLE IF NOT EXISTS chat_tool_calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL,
    session_id TEXT NOT NULL,
    tool_id TEXT NOT NULL,
    name TEXT NOT NULL,
    input TEXT NOT NULL DEFAULT '{}',
    output TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    done INTEGER NOT NULL DEFAULT 0,
    summary TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tool_id, message_id)
);
CREATE TABLE IF NOT EXISTS chat_thinking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL,
    session_id TEXT NOT NULL,
    think_id TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(think_id, message_id)
);
```

- [ ] **Step 2: 建 thinking.go（CRUD + ID 生成）**

创建 `internal/service/thinking.go`：

```go
package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// ThinkingRecord represents a row in the chat_thinking table.
type ThinkingRecord struct {
	ID        int64     `json:"id"`
	MessageID int64     `json:"message_id"`
	SessionID string    `json:"session_id"`
	ThinkID   string    `json:"think_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// generateThinkingID returns a think_id ("th_" + 32 hex chars).
func generateThinkingID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("th_%d", time.Now().UnixNano())
	}
	return "th_" + hex.EncodeToString(b)
}

// UpsertThinking inserts or updates a thinking record in chat_thinking.
// No-op when think_id or text is empty.
func UpsertThinking(messageID int64, sessionID, thinkID, text string) error {
	if thinkID == "" || text == "" {
		return nil
	}
	_, err := WriteExecContext(context.Background(), `
		INSERT INTO chat_thinking (message_id, session_id, think_id, text)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(think_id, message_id) DO UPDATE SET text = excluded.text
	`, messageID, sessionID, thinkID, text)
	if err != nil {
		return fmt.Errorf("UpsertThinking: %w", err)
	}
	return nil
}

// DeleteThinkingByMessage removes thinking records for a message.
// Called before insert in the Finalize write path for idempotency.
func DeleteThinkingByMessage(messageID int64) error {
	_, err := WriteExecContext(context.Background(), "DELETE FROM chat_thinking WHERE message_id = ?", messageID)
	if err != nil {
		return fmt.Errorf("DeleteThinkingByMessage: %w", err)
	}
	return nil
}

// GetThinking retrieves a thinking record by think_id and message_id.
// Returns nil if not found.
func GetThinking(thinkID string, messageID int64) (*ThinkingRecord, error) {
	var r ThinkingRecord
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, think_id, text, created_at
		FROM chat_thinking WHERE think_id = ? AND message_id = ?
	`, thinkID, messageID).Scan(&r.ID, &r.MessageID, &r.SessionID, &r.ThinkID, &r.Text, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetThinking: %w", err)
	}
	return &r, nil
}

// GetThinkingBySession retrieves a thinking record by think_id and session_id.
// Fallback for ACP multi-assistant-message sessions where the frontend may not
// know the exact message_id (mirrors GetToolCallBySession).
func GetThinkingBySession(thinkID, sessionID string) (*ThinkingRecord, error) {
	var r ThinkingRecord
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, think_id, text, created_at
		FROM chat_thinking WHERE think_id = ? AND session_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, thinkID, sessionID).Scan(&r.ID, &r.MessageID, &r.SessionID, &r.ThinkID, &r.Text, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetThinkingBySession: %w", err)
	}
	return &r, nil
}
```

- [ ] **Step 3: 写失败测试（thinking_test.go）**

创建 `internal/service/thinking_test.go`（复用 `initTestDB`，真实 schema）：

```go
package service

import (
	"testing"

	"clawbench/internal/model"
)

func TestThinkingCRUD(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-sess-001"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("insert new thinking", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_abc123", "thinking text"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, err := GetThinking("th_abc123", msgID)
		if err != nil {
			t.Fatalf("GetThinking: %v", err)
		}
		if rec == nil {
			t.Fatal("GetThinking returned nil")
		}
		if rec.ThinkID != "th_abc123" || rec.Text != "thinking text" || rec.MessageID != msgID || rec.SessionID != sessionID {
			t.Errorf("record mismatch: %+v", rec)
		}
	})

	t.Run("upsert overwrites text", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_abc123", "updated text"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, _ := GetThinking("th_abc123", msgID)
		if rec.Text != "updated text" {
			t.Errorf("Text = %q, want updated text", rec.Text)
		}
	})

	t.Run("get missing returns nil", func(t *testing.T) {
		rec, err := GetThinking("th_missing", msgID)
		if err != nil || rec != nil {
			t.Errorf("expected nil,nil got %+v,%v", rec, err)
		}
	})

	t.Run("get by session fallback", func(t *testing.T) {
		rec, err := GetThinkingBySession("th_abc123", sessionID)
		if err != nil || rec == nil || rec.Text != "updated text" {
			t.Errorf("GetThinkingBySession failed: rec=%+v err=%v", rec, err)
		}
		rec2, err := GetThinkingBySession("th_abc123", "other-session")
		if err != nil || rec2 != nil {
			t.Errorf("expected nil for other session, got %+v,%v", rec2, err)
		}
	})

	t.Run("delete by message", func(t *testing.T) {
		if err := DeleteThinkingByMessage(msgID); err != nil {
			t.Fatalf("DeleteThinkingByMessage: %v", err)
		}
		rec, _ := GetThinking("th_abc123", msgID)
		if rec != nil {
			t.Error("expected nil after delete")
		}
	})
}

func TestGenerateThinkingID(t *testing.T) {
	a, b := generateThinkingID(), generateThinkingID()
	if a == "" || b == "" {
		t.Fatal("generateThinkingID returned empty")
	}
	if a == b {
		t.Error("two generated IDs should differ")
	}
}

// Guard: model package must compile alongside (ensures package wiring).
var _ = model.ChatSession{}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/ -run 'TestThinkingCRUD|TestGenerateThinkingID' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/database.go internal/service/thinking.go internal/service/thinking_test.go
git commit -m "feat(service): add chat_thinking table and CRUD"
```

---

### Task 2: slimThinkingInContent + Finalize 写库拆分

**Files:**
- Modify: `internal/service/thinking.go`（加 slimThinkingInContent、persistThinkingToDB）
- Modify: `internal/service/session_executor.go:521-523`（Finalize 调 persistThinkingToDB）
- Test: `internal/service/thinking_test.go`（追加 slim 测试）

- [ ] **Step 1: 写失败测试**

追加到 `internal/service/thinking_test.go`（顶部 import 补 `"encoding/json"`）：

```go
func TestSlimThinkingInContent(t *testing.T) {
	t.Run("extracts thinking and keeps metadata", func(t *testing.T) {
		in := `{"blocks":[
			{"type":"text","text":"intro"},
			{"type":"thinking","text":"deep reasoning","done":true},
			{"type":"tool_use","id":"toolu_x","name":"Bash","done":true}
		],"metadata":{"model":"claude"}}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil {
			t.Fatalf("slimThinkingInContent: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("records = %d, want 1", len(records))
		}
		if records[0].Text != "deep reasoning" || records[0].ThinkID == "" {
			t.Errorf("record mismatch: %+v", records[0])
		}
		var parsed struct {
			Blocks   []map[string]any `json:"blocks"`
			Metadata map[string]any   `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
			t.Fatalf("unmarshal slim: %v", err)
		}
		if parsed.Blocks[1]["think_id"] != records[0].ThinkID {
			t.Errorf("think_id not in slim block: %v", parsed.Blocks[1])
		}
		if _, hasText := parsed.Blocks[1]["text"]; hasText {
			t.Error("slim block should not have text")
		}
		if parsed.Blocks[1]["done"] != true {
			t.Error("slim block should preserve done")
		}
		if parsed.Blocks[0]["text"] != "intro" {
			t.Error("text block should be untouched")
		}
		if parsed.Metadata["model"] != "claude" {
			t.Error("metadata should be preserved")
		}
	})

	t.Run("no thinking returns unchanged", func(t *testing.T) {
		in := `{"blocks":[{"type":"text","text":"hi"}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil || len(records) != 0 || slim != in {
			t.Errorf("expected unchanged, got slim=%q records=%v err=%v", slim, records, err)
		}
	})

	t.Run("already slim thinking skipped", func(t *testing.T) {
		in := `{"blocks":[{"type":"thinking","think_id":"th_x","done":true}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil || len(records) != 0 || slim != in {
			t.Errorf("expected unchanged, got slim=%q records=%v err=%v", slim, records, err)
		}
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/ -run TestSlimThinkingInContent -v`
Expected: FAIL — `slimThinkingInContent` undefined

- [ ] **Step 3: 实现 slimThinkingInContent + persistThinkingToDB**

在 `internal/service/thinking.go` 追加（注意 `contentKeyBlocks` 是 service 包常量 `"blocks"`）：

```go
// slimThinkingInContent parses content JSON, extracts thinking block text into
// ThinkingRecord entries (generating think_id), and rewrites the content with
// slim thinking blocks ({type:"thinking", think_id, done} — text removed).
// If no thinking block has text, returns content unchanged with empty records.
func slimThinkingInContent(content string) (string, []ThinkingRecord, error) {
	var wrapper map[string]any
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return content, nil, fmt.Errorf("slimThinkingInContent: unmarshal: %w", err)
	}
	blocksRaw, ok := wrapper[contentKeyBlocks].([]any)
	if !ok {
		return content, nil, nil
	}
	var records []ThinkingRecord
	changed := false
	for i := range blocksRaw {
		block, ok := blocksRaw[i].(map[string]any)
		if !ok || block["type"] != "thinking" {
			continue
		}
		text, _ := block["text"].(string)
		if text == "" {
			continue
		}
		thinkID := generateThinkingID()
		delete(block, "text")
		block["think_id"] = thinkID
		records = append(records, ThinkingRecord{ThinkID: thinkID, Text: text})
		changed = true
	}
	if !changed {
		return content, nil, nil
	}
	slim, err := json.Marshal(wrapper)
	if err != nil {
		return content, nil, fmt.Errorf("slimThinkingInContent: marshal: %w", err)
	}
	return string(slim), records, nil
}

// persistThinkingToDB slims thinking text out of the DB content into chat_thinking.
// Returns the content to persist (slimmed if thinking records were extracted).
// The WS terminal event keeps full blocks; only the persisted content is slimmed.
func persistThinkingToDB(content string, streamingMsgID int64, sessionID string) string {
	if streamingMsgID <= 0 || sessionID == "" {
		return content
	}
	slimContent, records, err := slimThinkingInContent(content)
	if err != nil {
		slog.Warn("slim thinking failed; persisting full content", slog.Int64("msgID", streamingMsgID), slog.String("err", err.Error()))
		return content
	}
	if len(records) == 0 {
		return content
	}
	if err := DeleteThinkingByMessage(streamingMsgID); err != nil {
		slog.Warn("delete thinking for message failed", slog.Int64("msgID", streamingMsgID), slog.String("err", err.Error()))
	}
	for _, rec := range records {
		if err := UpsertThinking(streamingMsgID, sessionID, rec.ThinkID, rec.Text); err != nil {
			slog.Warn("upsert thinking failed", slog.String("thinkID", rec.ThinkID), slog.String("err", err.Error()))
		}
	}
	return slimContent
}
```

`thinking.go` 现有 import 需补 `"encoding/json"`、`"log/slog"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/ -run TestSlimThinkingInContent -v`
Expected: PASS

- [ ] **Step 5: 接入 Finalize**

修改 `internal/service/session_executor.go:521-523`：

```go
	content, blocks := e.buildContentJSON(blocks, result, responseMetadata)

	// Split thinking text out of the DB content into chat_thinking (lazy-load).
	// The WS terminal event keeps full blocks (result.Blocks); only the
	// persisted content is slimmed. StreamingMessageID is the streaming row.
	dbContent := persistThinkingToDB(content, e.cfg.StreamingMessageID, e.cfg.SessionID)

	msgID, err := FinalizeStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, dbContent)
```

- [ ] **Step 6: 写 Finalize 集成测试**

在 `internal/service/session_executor_test.go` 追加（复用 `setupExecutorDB`/`setupExecutorSession`）：

```go
func TestSessionExecutor_Finalize_SlimsThinkingToDB(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	streamingMsgID := GetStreamingMessageID(sid)
	if streamingMsgID == 0 {
		t.Fatal("expected non-zero streaming message ID from setup")
	}

	events := []ai.StreamEvent{
		{Type: "thinking", Content: "Let me think about this deeply."},
		{Type: "content", Content: "Answer here."},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	})
	result := executor.RunWithChannel(ch)
	finalized := executor.Finalize(result, nil)
	if finalized.MsgID == 0 {
		t.Fatal("expected non-zero message ID after Finalize")
	}

	// WS blocks must keep full thinking text.
	var wsThinking string
	for i := range finalized.Blocks {
		if finalized.Blocks[i].Type == "thinking" {
			wsThinking = finalized.Blocks[i].Text
		}
	}
	if wsThinking != "Let me think about this deeply." {
		t.Errorf("WS block thinking text = %q, want full text", wsThinking)
	}

	// DB content must be slim (thinking block has think_id, no text).
	var dbContent string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", finalized.MsgID).Scan(&dbContent)
	if err != nil {
		t.Fatalf("read db content: %v", err)
	}
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(dbContent), &parsed); err != nil {
		t.Fatalf("unmarshal db content: %v", err)
	}
	var thinkBlock map[string]any
	for _, b := range parsed.Blocks {
		if b["type"] == "thinking" {
			thinkBlock = b
		}
	}
	if thinkBlock == nil {
		t.Fatal("expected thinking block in DB content")
	}
	if _, hasText := thinkBlock["text"]; hasText {
		t.Error("DB thinking block should not have text")
	}
	thinkID, _ := thinkBlock["think_id"].(string)
	if thinkID == "" {
		t.Fatal("expected think_id in slim block")
	}

	// chat_thinking row exists with the extracted text.
	rec, err := GetThinking(thinkID, finalized.MsgID)
	if err != nil || rec == nil {
		t.Fatalf("expected thinking record, rec=%+v err=%v", rec, err)
	}
	if rec.Text != "Let me think about this deeply." {
		t.Errorf("record text = %q", rec.Text)
	}
}
```

注：`setupExecutorDB` 底层 schema 在 `scheduler_executor_test.go:104`，需在其 `chat_tool_calls` 表后补 `chat_thinking` 建表 SQL（与 Task 1 一致）。

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/service/ -run 'TestSessionExecutor_Finalize_SlimsThinkingToDB|TestSlimThinkingInContent' -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/service/thinking.go internal/service/thinking_test.go internal/service/session_executor.go internal/service/session_executor_test.go internal/service/scheduler_executor_test.go
git commit -m "feat(service): split thinking into chat_thinking on Finalize (lazy-load)"
```

---

### Task 3: MigrateThinkingFromContent 启动迁移

**Files:**
- Create: `internal/service/thinking_migrate.go`
- Test: `internal/service/thinking_migrate_test.go`
- Modify: `internal/service/database.go:784`（InitDB 调用点）

- [ ] **Step 1: 写失败测试**

创建 `internal/service/thinking_migrate_test.go`（自建 schema，仿 `setupTestDBForToolCallMigration`）：

```go
package service

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupTestDBForThinkingMigration(t *testing.T) func() {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	db.Exec("PRAGMA foreign_keys = ON")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			session_type TEXT NOT NULL DEFAULT 'chat',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_thinking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			think_id TEXT NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(think_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
		CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	cleanup := SetDBForTest(db, db)
	return func() { cleanup(); db.Close() }
}

func TestMigrateThinkingFromContent_ExtractsThinking(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-1', '/proj', 'claude', 'Test')")
	assert.NoError(t, err)

	oldContent := `{
		"blocks": [
			{"type": "text", "text": "I'll check."},
			{"type": "thinking", "text": "internal reasoning", "done": true},
			{"type": "text", "text": "Result"}
		]
	}`
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	msgID, _ := res.LastInsertId()

	MigrateThinkingFromContent()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	var thinkID, text string
	err = db.QueryRow("SELECT think_id, text FROM chat_thinking WHERE message_id = ?", msgID).Scan(&thinkID, &text)
	assert.NoError(t, err)
	assert.NotEmpty(t, thinkID)
	assert.Equal(t, "internal reasoning", text)

	var newContent string
	err = db.QueryRow("SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&newContent)
	assert.NoError(t, err)
	var parsed struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	json.Unmarshal([]byte(newContent), &parsed)
	assert.Len(t, parsed.Blocks, 3)
	var thinkBlock map[string]any
	json.Unmarshal(parsed.Blocks[1], &thinkBlock)
	assert.Equal(t, "thinking", thinkBlock["type"])
	assert.Equal(t, thinkID, thinkBlock["think_id"])
	_, hasText := thinkBlock["text"]
	assert.False(t, hasText)
}

func TestMigrateThinkingFromContent_IdempotentAndSkipsSlim(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-1', '/proj', 'claude', 'Test')")
	assert.NoError(t, err)
	oldContent := `{"blocks":[{"type":"thinking","text":"old","done":true},{"type":"text","text":"ok"}]}`
	_, err = db.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)

	MigrateThinkingFromContent()
	MigrateThinkingFromContent()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 1, count, "second run must be idempotent")

	// Streaming message must be skipped.
	_, err = db.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 1)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	MigrateThinkingFromContent()
	db.QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 1, count, "streaming message must be skipped")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/ -run TestMigrateThinkingFromContent -v`
Expected: FAIL — `MigrateThinkingFromContent` undefined

- [ ] **Step 3: 实现迁移**

创建 `internal/service/thinking_migrate.go`：

```go
package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// MigrateThinkingFromContent scans assistant messages that contain thinking blocks
// with text still embedded in content JSON, extracts them into chat_thinking,
// and rewrites content to the slim format (think_id instead of text).
// One-time migration for data created before the thinking-split feature.
// Runs in batches to avoid excessive memory usage on large databases.
func MigrateThinkingFromContent() {
	// Old-format rows have "type":"thinking" blocks WITHOUT "think_id".
	var needed int
	_ = dbRead.QueryRow(`
		SELECT COUNT(*) FROM chat_history h
		WHERE h.role = 'assistant'
		  AND h.content LIKE '%"type":"thinking"%'
		  AND h.content NOT LIKE '%think_id%'
		  AND h.streaming = 0
		  AND NOT EXISTS (
		    SELECT 1 FROM chat_thinking tc
		    WHERE tc.message_id = h.id
		    LIMIT 1
		  )
	`).Scan(&needed)
	if needed == 0 {
		return
	}
	slog.Info("migrating thinking text from chat_history to chat_thinking", slog.Int("rows", needed))

	batchSize := 200
	offset := 0
	migrated := 0
	failed := 0

	for {
		rows, err := dbRead.Query(`
			SELECT h.id, h.session_id, h.content FROM chat_history h
			WHERE h.role = 'assistant'
			  AND h.content LIKE '%"type":"thinking"%'
			  AND h.content NOT LIKE '%think_id%'
			  AND h.streaming = 0
			  AND NOT EXISTS (
			    SELECT 1 FROM chat_thinking tc
			    WHERE tc.message_id = h.id
			    LIMIT 1
			  )
			ORDER BY h.id
			LIMIT ? OFFSET ?`,
			batchSize, offset,
		)
		if err != nil {
			slog.Error("thinking migration: query failed", slog.String("err", err.Error()))
			return
		}

		type msgRow struct {
			ID        int64
			SessionID string
			Content   string
		}
		var batch []msgRow
		for rows.Next() {
			var r msgRow
			if err := rows.Scan(&r.ID, &r.SessionID, &r.Content); err != nil {
				slog.Error("thinking migration: scan failed", slog.String("err", err.Error()))
				continue
			}
			batch = append(batch, r)
		}
		_ = rows.Close()

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if err := migrateThinkingForRow(r.ID, r.SessionID, r.Content); err != nil {
				slog.Error("thinking migration: row failed",
					slog.Int64("id", r.ID),
					slog.String("err", err.Error()))
				failed++
				continue
			}
			migrated++
		}

		slog.Info("thinking migration progress",
			slog.Int("migrated", migrated),
			slog.Int("failed", failed),
			slog.Int("total", needed))

		if len(batch) < batchSize {
			break
		}
		offset += batchSize
	}

	slog.Info("thinking migration complete",
		slog.Int("migrated", migrated),
		slog.Int("failed", failed),
		slog.Int("needed", needed))
}

// migrateThinkingForRow processes a single chat_history row:
// 1. Extract thinking text via slimThinkingInContent (assigns think_id)
// 2. Insert into chat_thinking
// 3. Rewrite content to slim format
func migrateThinkingForRow(msgID int64, sessionID, content string) error {
	slimContent, records, err := slimThinkingInContent(content)
	if err != nil {
		return fmt.Errorf("slim thinking: %w", err)
	}
	if len(records) == 0 {
		return nil
	}
	for _, rec := range records {
		if err := UpsertThinking(msgID, sessionID, rec.ThinkID, rec.Text); err != nil {
			slog.Warn("thinking migration: upsert failed",
				slog.String("thinkID", rec.ThinkID),
				slog.String("err", err.Error()))
		}
	}
	_, err = WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", slimContent, msgID)
	if err != nil {
		return fmt.Errorf("update slim content: %w", err)
	}
	return nil
}

// keep the encoding/json import used (migrateThinkingForRow uses slimThinkingInContent;
// json import retained for parity/documentation).
var _ = json.Marshal
```

注：若不需要 `encoding/json`，删掉该 import 与 `var _ = json.Marshal`。优先删，保持 import 干净。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/ -run TestMigrateThinkingFromContent -v`
Expected: PASS

- [ ] **Step 5: 接入 InitDB**

修改 `internal/service/database.go:784`：

```go
	MigrateMetadataFromContent()
	MigrateToolCallsFromContent()
	MigrateThinkingFromContent()
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/thinking_migrate.go internal/service/thinking_migrate_test.go internal/service/database.go
git commit -m "feat(service): migrate existing thinking text into chat_thinking on startup"
```

---

### Task 4: Fork / 续会话复制 chat_tool_calls + chat_thinking

**Files:**
- Modify: `internal/service/continue_conversation.go`（新函数 + 两个调用点）
- Test: `internal/service/fork_session_test.go`、`internal/service/continue_conversation_test.go`

- [ ] **Step 1: 写失败测试（fork 复制）**

在 `internal/service/fork_session_test.go` 追加（`setupDB` 的 schema 已在 Task 1 补表，无需手动建表）：

```go
func TestForkSession_CopiesToolCallsAndThinking(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hello", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant",
		`{"blocks":[{"type":"tool_use","id":"toolu_01","name":"Read","done":true},{"type":"thinking","think_id":"th_01","done":true}]}`,
		nil, false, "")
	assert.NoError(t, err)
	assert.NoError(t, service.UpsertToolCall(asstID, sessID, "toolu_01", "Read", []byte(`{"file_path":"/a.go"}`), "contents", "success", "a.go", true))
	assert.NoError(t, service.UpsertThinking(asstID, sessID, "th_01", "deep text"))

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Hello", 0)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	var forkAsstID int64
	for _, m := range msgs {
		if m.Role == "assistant" {
			forkAsstID = m.ID
		}
	}

	rec, err := service.GetToolCall("toolu_01", forkAsstID)
	assert.NoError(t, err)
	assert.NotNil(t, rec, "tool call must be copied into fork")
	if rec != nil {
		assert.Equal(t, "contents", rec.Output)
		assert.Equal(t, newSessID, rec.SessionID)
	}

	th, err := service.GetThinking("th_01", forkAsstID)
	assert.NoError(t, err)
	assert.NotNil(t, th, "thinking must be copied into fork")
	if th != nil {
		assert.Equal(t, "deep text", th.Text)
		assert.Equal(t, newSessID, th.SessionID)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/ -run TestForkSession_CopiesToolCallsAndThinking -v`
Expected: FAIL — 断言 `rec != nil` / `th != nil` 失败

- [ ] **Step 3: 实现 copySessionDetailTables + 接入**

在 `internal/service/continue_conversation.go` 追加（import 需补 `"time"`，如已存在则跳过）：

```go
// copySessionDetailTables copies chat_tool_calls and chat_thinking rows from the
// source session to the fork/continued session, remapping message_id via idMap.
// Rows whose source message_id is not in idMap (e.g. fork truncation) are skipped.
func copySessionDetailTables(idMap map[int64]int64, sourceSessionID, newSessionID string) error {
	if len(idMap) == 0 {
		return nil
	}

	// chat_tool_calls
	rows, err := dbRead.Query(
		"SELECT message_id, tool_id, name, input, output, status, done, summary, created_at FROM chat_tool_calls WHERE session_id = ?",
		sourceSessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to query source tool calls: %w", err)
	}
	type toolRow struct {
		messageID int64
		toolID    string
		name      string
		input     string
		output    string
		status    string
		done      int
		summary   string
		createdAt time.Time
	}
	var toolRows []toolRow
	for rows.Next() {
		var r toolRow
		if err := rows.Scan(&r.messageID, &r.toolID, &r.name, &r.input, &r.output, &r.status, &r.done, &r.summary, &r.createdAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("failed to scan tool call: %w", err)
		}
		toolRows = append(toolRows, r)
	}
	_ = rows.Close()
	for _, r := range toolRows {
		newID, ok := idMap[r.messageID]
		if !ok {
			continue
		}
		if _, err := WriteExec(
			"INSERT OR REPLACE INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			newID, newSessionID, r.toolID, r.name, r.input, r.output, r.status, r.done, r.summary, r.createdAt,
		); err != nil {
			return fmt.Errorf("failed to copy tool call %s: %w", r.toolID, err)
		}
	}

	// chat_thinking
	rows, err = dbRead.Query(
		"SELECT message_id, think_id, text, created_at FROM chat_thinking WHERE session_id = ?",
		sourceSessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to query source thinking: %w", err)
	}
	type thinkRow struct {
		messageID int64
		thinkID   string
		text      string
		createdAt time.Time
	}
	var thinkRows []thinkRow
	for rows.Next() {
		var r thinkRow
		if err := rows.Scan(&r.messageID, &r.thinkID, &r.text, &r.createdAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("failed to scan thinking: %w", err)
		}
		thinkRows = append(thinkRows, r)
	}
	_ = rows.Close()
	for _, r := range thinkRows {
		newID, ok := idMap[r.messageID]
		if !ok {
			continue
		}
		if _, err := WriteExec(
			"INSERT OR REPLACE INTO chat_thinking (message_id, session_id, think_id, text, created_at) VALUES (?, ?, ?, ?, ?)",
			newID, newSessionID, r.thinkID, r.text, r.createdAt,
		); err != nil {
			return fmt.Errorf("failed to copy thinking %s: %w", r.thinkID, err)
		}
	}

	return nil
}
```

两个调用点：

`ForkSession`（`:349`，`copySessionSummaries` 之后）：

```go
	if err := copySessionSummaries(idMap); err != nil {
		return "", err
	}
	if err := copySessionDetailTables(idMap, sourceSessionID, newSessionID); err != nil {
		return "", err
	}
```

`ContinueFromExecution`（`:236` summaries 复制循环之后，`:257` return 之前）：

```go
	if err := copySessionDetailTables(idMap, sourceSessionID, newSessionID); err != nil {
		return "", false, err
	}
```

- [ ] **Step 4: 写续会话测试**

在 `internal/service/continue_conversation_test.go` 追加（复用 `helperCreateScheduledTask` / `helperCreateScheduledSession` / `helperCreateTaskExecution`，`setupDB` schema 已在 Task 1 补表）：

```go
func TestContinueFromExecution_CopiesToolCallsAndThinking(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Daily Review", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Daily Review")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Review", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant",
		`{"blocks":[{"type":"tool_use","id":"toolu_c01","name":"Bash","done":true},{"type":"thinking","think_id":"th_c01","done":true}]}`,
		nil, false, "")
	assert.NoError(t, err)
	assert.NoError(t, service.UpsertToolCall(asstID, sessID, "toolu_c01", "Bash", []byte(`{"command":"ls"}`), "out", "success", "ls", true))
	assert.NoError(t, service.UpsertThinking(asstID, sessID, "th_c01", "continued thought"))

	newSessID, exists, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.False(t, exists)
	assert.NotEmpty(t, newSessID)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	var newAsstID int64
	for _, m := range msgs {
		if m.Role == "assistant" {
			newAsstID = m.ID
		}
	}
	rec, err := service.GetToolCall("toolu_c01", newAsstID)
	assert.NoError(t, err)
	assert.NotNil(t, rec, "tool call must be copied into continued session")
	th, err := service.GetThinking("th_c01", newAsstID)
	assert.NoError(t, err)
	assert.NotNil(t, th, "thinking must be copied into continued session")
	if th != nil {
		assert.Equal(t, "continued thought", th.Text)
	}
}
```

注：`helperCreateScheduledTask` / `helperCreateScheduledSession` / `helperCreateTaskExecution` 需确认在测试包内已定义（`continue_conversation_test.go` 与 `scheduler_test.go` 用到）；若 `helperCreateScheduledSession` 不存在，改用 `helperCreateScheduledSession(t, "/project", "claude", "Daily Review")` 的替代助手名并保持签名一致。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/service/ -run 'TestForkSession_CopiesToolCallsAndThinking|TestContinueFromExecution_CopiesToolCallsAndThinking' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/service/continue_conversation.go internal/service/fork_session_test.go internal/service/continue_conversation_test.go
git commit -m "fix(service): copy chat_tool_calls and chat_thinking on fork/continue"
```

---

### Task 5: Purge / 硬删除清理 chat_thinking

**Files:**
- Modify: `internal/service/chat.go:1473`、`:1517`
- Test: `internal/service/chat_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/service/chat_test.go` 追加（`setupDB` schema 已在 Task 1 补表）：

```go
func TestHardDeleteSession_RemovesThinking(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Delete Me")
	asstID, err := service.AddChatMessage("/project", "claude", sid, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_del","done":true}]}`, nil, false, "")
	assert.NoError(t, err)
	assert.NoError(t, service.UpsertThinking(asstID, sid, "th_del", "doomed"))

	assert.NoError(t, service.HardDeleteSession(sid))

	var count int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE session_id = ?", sid).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "thinking rows must be purged with the session")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/ -run TestHardDeleteSession_RemovesThinking -v`
Expected: FAIL — `count != 0`

- [ ] **Step 3: 补 DELETE**

`internal/service/chat.go:1473` 之后（PurgeArchivedData）：

```go
	// Delete chat_tool_calls for these sessions
	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id IN ("+placeholders+")", args...)

	// Delete chat_thinking for these sessions
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id IN ("+placeholders+")", args...)
```

`internal/service/chat.go:1517` 之后（HardDeleteSession）：

```go
	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id = ?", sessionID)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/ -run TestHardDeleteSession_RemovesThinking -v`
Expected: PASS

- [ ] **Step 5: 跑 chat.go 全部测试防回归**

Run: `go test ./internal/service/ -run 'TestPurge|TestHardDelete' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat.go internal/service/chat_test.go
git commit -m "fix(service): purge chat_thinking on session hard-delete"
```

---

### Task 6: ServeThinkingDetail 端点 + 路由 + i18n

**Files:**
- Create: `internal/handler/chat_thinking.go`
- Modify: `internal/handler/handler.go:252`（注册路由）
- Modify: `internal/i18n/locales/active.en.yaml`、`active.zh.yaml`
- Modify: `internal/handler/testutil_test.go:232`（schema 补 chat_thinking）
- Test: `internal/handler/chat_thinking_test.go`

- [ ] **Step 1: handler test schema 补表**

`internal/handler/testutil_test.go` 在 `chat_tool_calls` 索引（`:232`）后加：

```sql
CREATE TABLE IF NOT EXISTS chat_thinking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    think_id TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(think_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
```

- [ ] **Step 2: 写失败测试**

创建 `internal/handler/chat_thinking_test.go`：

```go
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeThinkingDetail_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_01","done":true}]}`, nil, false, "")
	require.NoError(t, err)
	require.NoError(t, service.UpsertThinking(msgID, sessionID, "th_01", "deep reasoning"))

	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_01&message_id="+fmt.Sprintf("%d", msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "th_01", result["think_id"])
	assert.Equal(t, "deep reasoning", result["text"])
}

func TestServeThinkingDetail_MissingParams(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThinkingDetail_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_x&message_id=1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThinkingDetail_SessionIDFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	// thinking stored under msgID1, frontend asks with msgID2 + session_id
	msgID1, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_fb","done":true}]}`, nil, false, "")
	require.NoError(t, err)
	msgID2, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)
	require.NoError(t, service.UpsertThinking(msgID1, sessionID, "th_fb", "fallback text"))

	req := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/ai/chat/thinking?think_id=th_fb&message_id=%d&session_id=%s", msgID2, sessionID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assertOK(t, w)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "fallback text", result["text"])

	// no session_id, wrong message_id -> 404
	req2 := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/ai/chat/thinking?think_id=th_fb&message_id=%d", msgID2), nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeThinkingDetail, req2)
	assert.Equal(t, http.StatusNotFound, w2.Code)
}

func TestServeThinkingDetail_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_sec","done":true}]}`, nil, false, "")
	require.NoError(t, err)
	require.NoError(t, service.UpsertThinking(msgID, sessionID, "th_sec", "secret"))

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_sec&message_id="+fmt.Sprintf("%d", msgID), nil)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(otherDir)})
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

文件顶部 import 需含：`"encoding/json"`、`"fmt"`、`"net/http"`、`"net/url"`、`"os"`、`"testing"`、`"clawbench/internal/model"`、`"clawbench/internal/service"`、`"github.com/stretchr/testify/assert"`、`"github.com/stretchr/testify/require"`。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/handler/ -run TestServeThinkingDetail -v`
Expected: FAIL — `ServeThinkingDetail` undefined

- [ ] **Step 4: 实现 handler**

创建 `internal/handler/chat_thinking.go`：

```go
package handler

import (
	"fmt"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeThinkingDetail handles GET /api/ai/chat/thinking — returns the full text
// for a single thinking block from the chat_thinking table.
// Parameters: think_id (required), message_id (required), session_id (optional).
// When the think_id+message_id lookup fails, falls back to think_id+session_id
// (mirrors ServeToolCallDetail for ACP multi-message sessions).
func ServeThinkingDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	thinkID := r.URL.Query().Get("think_id")
	messageIDStr := r.URL.Query().Get("message_id")
	sessionID := r.URL.Query().Get("session_id")
	if thinkID == "" || messageIDStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ThinkIdAndMessageIdRequired")
		return
	}
	var messageID int64
	if _, err := fmt.Sscanf(messageIDStr, "%d", &messageID); err != nil || messageID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidMessageId")
		return
	}

	record, err := service.GetThinking(thinkID, messageID)
	if err != nil || record == nil {
		if sessionID != "" {
			record, err = service.GetThinkingBySession(thinkID, sessionID)
		}
		if err != nil || record == nil {
			writeLocalizedError(w, r, model.NotFound(fmt.Errorf("thinking not found"), "ThinkingNotFound"))
			return
		}
	}

	if service.GetSessionBackend(record.SessionID) == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	if sessionProject := service.GetSessionProjectPath(record.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	writeJSON(w, http.StatusOK, record)
}
```

- [ ] **Step 5: 注册路由**

`internal/handler/handler.go:252`（tool-call 注册之后）：

```go
	register("/api/ai/chat/tool-call", middleware.Auth(ServeToolCallDetail))
	register("/api/ai/chat/thinking", middleware.Auth(ServeThinkingDetail))
```

- [ ] **Step 6: 补 i18n 文案**

`internal/i18n/locales/active.en.yaml`：

```yaml
ThinkIdAndMessageIdRequired: think_id and message_id are required
ThinkingNotFound: thinking not found
```

`internal/i18n/locales/active.zh.yaml`：

```yaml
ThinkIdAndMessageIdRequired: 缺少 think_id 或 message_id
ThinkingNotFound: 思考内容未找到
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/handler/ -run TestServeThinkingDetail -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/handler/chat_thinking.go internal/handler/handler.go internal/i18n/locales/active.en.yaml internal/i18n/locales/active.zh.yaml internal/handler/testutil_test.go internal/handler/chat_thinking_test.go
git commit -m "feat(handler): serve thinking detail with lazy-load endpoint"
```

---

### Task 7: 前端 chatBlocks.ts 稳定 key（think_id）

**Files:**
- Modify: `web/src/utils/chatBlocks.ts`（`stableBlockKey` 逻辑实际在 ContentBlocks.vue，此任务处理 `parseAssistantContent` 的 thinking 透传）
- Modify: `web/src/components/chat/ContentBlocks.vue:370`（stableBlockKey）
- Test: `web/src/utils/__tests__/chatBlocks.test.ts`

- [ ] **Step 1: 写失败测试**

在 `web/src/utils/__tests__/chatBlocks.test.ts` 追加：

```ts
import { parseAssistantContent } from '@/utils/chatBlocks'

describe('parseAssistantContent slim thinking', () => {
  it('keeps slim thinking blocks with think_id and no text', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'hi' },
        { type: 'thinking', think_id: 'th_01', done: true },
      ],
    })
    const { blocks } = parseAssistantContent(content)
    expect(blocks[1].type).toBe('thinking')
    expect(blocks[1].think_id).toBe('th_01')
    expect(blocks[1].text).toBeUndefined()
    expect(blocks[1]._key).toBe('thinking-0')
  })

  it('keeps live thinking blocks with text', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'thinking', text: 'live thought', done: false }],
    })
    const { blocks } = parseAssistantContent(content)
    expect(blocks[0].text).toBe('live thought')
    expect(blocks[0].think_id).toBeUndefined()
  })
})
```

（若 `parseAssistantContent` 当前已能透传 slim 块，测试自然通过——此时本任务主要是 `stableBlockKey` 改动，见 Step 3。以实际跑测结果为准，若已通过则跳过实现直接提交。）

- [ ] **Step 2: 跑测试确认现状**

Run: `npx vitest run web/src/utils/__tests__/chatBlocks.test.ts`
Expected: PASS（若失败则需在 `chatBlocks.ts` 的 thinking 分支不做任何改写、仅保留 `_key` 兜底赋值）

- [ ] **Step 3: 改 ContentBlocks stableBlockKey**

`web/src/components/chat/ContentBlocks.vue:370`：

```js
function stableBlockKey(bi, block) {
  if (block.type === 'tool_use' && block.id) return block.id
  if (block.type === 'thinking') {
    if (block.think_id) return block.think_id
    if (block._key) return block._key
  }
  return `${block.type || 'other'}-${bi}`
}
```

- [ ] **Step 4: 跑前端相关测试防回归**

Run: `npx vitest run web/src/utils/__tests__/chatBlocks.test.ts web/src/components/chat/__tests__/ContentBlocks.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/utils/chatBlocks.ts web/src/utils/__tests__/chatBlocks.test.ts web/src/components/chat/ContentBlocks.vue
git commit -m "feat(web): use think_id as stable key for thinking blocks"
```

---

### Task 8: useThinkingContent composable

**Files:**
- Create: `web/src/composables/useThinkingContent.ts`
- Test: `web/src/composables/__tests__/useThinkingContent.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `web/src/composables/__tests__/useThinkingContent.test.ts`：

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useThinkingContent } from '@/composables/useThinkingContent'

const TAG = 'ThinkingContent'

describe('useThinkingContent', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useThinkingContent().clearThinkingCache()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches and caches thinking text by think_id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ think_id: 'th_1', text: 'deep reasoning' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking } = useThinkingContent()
    const text = await loadThinking('th_1', 42)
    expect(text).toBe('deep reasoning')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/chat/thinking?think_id=th_1&message_id=42',
    )

    // Second call hits cache — no second fetch
    const text2 = await loadThinking('th_1', 42)
    expect(text2).toBe('deep reasoning')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('dedupes concurrent fetches for the same think_id', async () => {
    let resolveFetch: (v: unknown) => void
    const fetchMock = vi.fn().mockImplementation(
      () => new Promise((resolve) => {
        resolveFetch = resolve
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking } = useThinkingContent()
    const p1 = loadThinking('th_1', 42)
    const p2 = loadThinking('th_1', 42)
    resolveFetch!({ ok: true, json: async () => ({ think_id: 'th_1', text: 'x' }) })
    const [r1, r2] = await Promise.all([p1, p2])
    expect(r1).toBe('x')
    expect(r2).toBe('x')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('appends session_id when provided and reports errors', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 404 })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, errors } = useThinkingContent()
    await expect(loadThinking('th_1', 42, 'sess-9')).rejects.toThrow()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/chat/thinking?think_id=th_1&message_id=42&session_id=sess-9',
    )
    expect(errors.value['th_1']).toBeTruthy()
  })

  it('clearThinkingCache clears cached text', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ think_id: 'th_1', text: 'deep reasoning' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, cachedText, clearThinkingCache } = useThinkingContent()
    await loadThinking('th_1', 42)
    expect(cachedText('th_1')).toBe('deep reasoning')
    clearThinkingCache()
    expect(cachedText('th_1')).toBeUndefined()
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useThinkingContent.test.ts`
Expected: FAIL — module not found

- [ ] **Step 3: 实现 composable**

创建 `web/src/composables/useThinkingContent.ts`：

```ts
import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'ThinkingContent'

const thinkingTextCache = new Map<string, string>()
const inFlight = new Map<string, Promise<string>>()

function clearThinkingCache() {
  thinkingTextCache.clear()
}

export function useThinkingContent() {
  const loading = ref<Record<string, boolean>>({})
  const errors = ref<Record<string, string>>({})

  function cachedText(thinkId: string): string | undefined {
    return thinkingTextCache.get(thinkId)
  }

  async function loadThinking(thinkId: string, msgId: string | number, sessionId?: string): Promise<string> {
    const cached = thinkingTextCache.get(thinkId)
    if (cached !== undefined) return cached
    const pending = inFlight.get(thinkId)
    if (pending) return pending

    loading.value[thinkId] = true
    delete errors.value[thinkId]
    const p = doFetch(thinkId, msgId, sessionId)
    inFlight.set(thinkId, p)
    try {
      const text = await p
      return text
    } catch (e) {
      errors.value[thinkId] = e instanceof Error ? e.message : String(e)
      throw e
    } finally {
      loading.value[thinkId] = false
      inFlight.delete(thinkId)
    }
  }

  return { loading, errors, cachedText, loadThinking, clearThinkingCache }
}

async function doFetch(thinkId: string, msgId: string | number, sessionId?: string): Promise<string> {
  let url = `/api/ai/chat/thinking?think_id=${encodeURIComponent(thinkId)}&message_id=${encodeURIComponent(msgId)}`
  if (sessionId) url += `&session_id=${encodeURIComponent(sessionId)}`
  const resp = await fetch(url)
  if (!resp.ok) {
    throw new Error(`thinking fetch failed: ${resp.status}`)
  }
  const data = await resp.json()
  if (!data.text) {
    throw new Error('thinking text empty')
  }
  thinkingTextCache.set(thinkId, data.text)
  return data.text
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useThinkingContent.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useThinkingContent.ts web/src/composables/__tests__/useThinkingContent.test.ts
git commit -m "feat(web): add useThinkingContent composable for lazy thinking load"
```

---

### Task 9: ContentBlocks.vue 懒加载渲染

**Files:**
- Modify: `web/src/components/chat/ContentBlocks.vue`（handleThinkingClick、getThinkingHtml、错误重试 UI、i18n key）
- Modify: `web/src/i18n/locales/zh.ts`、`en.ts`（`chat.contentBlocks.thinkingLoadFailed`）
- Test: `web/src/components/chat/__tests__/ContentBlocks.test.ts`

- [ ] **Step 1: 补 i18n key**

`web/src/i18n/locales/en.ts` 的 `chat.contentBlocks` 下加：
```ts
thinkingLoadFailed: 'Failed to load thinking',
```
`web/src/i18n/locales/zh.ts` 加：
```ts
thinkingLoadFailed: '思考内容加载失败',
```

- [ ] **Step 2: 写失败测试**

在 `web/src/components/chat/__tests__/ContentBlocks.test.ts` 追加（复用其现有 `mountBlocks` 助手与 i18n messages；若未导入 `flushPromises`，在顶部 `import { flushPromises } from '@vue/test-utils'`；test 的 i18n `contentBlocks` 里补 `thinkingLoadFailed`）：

```ts
describe('thinking lazy load', () => {
  it('expanding a slim thinking block fetches and renders text', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ think_id: 'th_1', text: 'loaded reasoning' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountBlocks({
      msgId: 'm1',
      sessionId: 's1',
      blocks: [{ type: 'thinking', think_id: 'th_1', done: true }],
      streaming: false,
      active: true,
    })

    await wrapper.find('.thinking-header').trigger('click')
    await nextTick()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/chat/thinking?think_id=th_1&message_id=m1&session_id=s1',
    )
    expect(wrapper.find('.thinking-content-wrapper').classes()).toContain('thinking-content-open')

    await flushPromises()
    await nextTick()
    expect(wrapper.find('.thinking-inline-content').html()).toContain('loaded reasoning')
  })

  it('renders existing text directly without fetching', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountBlocks({
      msgId: 'm1',
      sessionId: 's1',
      blocks: [{ type: 'thinking', text: 'inline thought', done: true }],
      streaming: false,
      active: true,
    })

    await wrapper.find('.thinking-header').trigger('click')
    await nextTick()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.thinking-inline-content').html()).toContain('inline thought')
  })

  it('shows error retry and refetches on retry click', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 404 })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_2', text: 'recovered' }) })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountBlocks({
      msgId: 'm1',
      sessionId: 's1',
      blocks: [{ type: 'thinking', think_id: 'th_2', done: true }],
      streaming: false,
      active: true,
    })

    await wrapper.find('.thinking-header').trigger('click')
    await flushPromises()
    await nextTick()
    expect(wrapper.find('.thinking-inline-content').html()).toContain('Failed to load thinking')

    // Retry: click the header again (expanded-done + error → re-trigger load)
    await wrapper.find('.thinking-header').trigger('click')
    await flushPromises()
    await nextTick()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.thinking-inline-content').html()).toContain('recovered')
  })
})
```

- [ ] **Step 3: 跑测试确认失败**

Run: `npx vitest run web/src/components/chat/__tests__/ContentBlocks.test.ts -t "thinking lazy load"`
Expected: FAIL — fetch 未被调用 / 内容未渲染

- [ ] **Step 4: 实现懒加载**

`web/src/components/chat/ContentBlocks.vue` 顶部 setup 加 import：

```js
import { useThinkingContent } from '@/composables/useThinkingContent.ts'
```

setup 内实例化：

```js
const thinkingContent = useThinkingContent()
```

`handleThinkingClick`（`:376`）在展开分支加触发，且错误态下再次点击 = 重试（而非折叠）：

```js
function handleThinkingClick(block, bi) {
  const blockKey = stableBlockKey(bi, block)
  if (isThinkingCollapsed(block, bi)) {
    expandingThinking.value[blockKey] = true
    thinkingExpanded.value[blockKey] = true
    blockHtmlCache.value = {}
    if (!block.text && block.think_id) {
      thinkingContent.loadThinking(block.think_id, props.msgId, props.sessionId)
        .catch(() => { /* error surfaced via errors ref */ })
    }
    const t = setTimeout(() => {
      delete expandingThinking.value[blockKey]
    }, EXPAND_TRANSITION_MS)
    _collapseTimers.push(t)
  } else if (isThinkingExpandedDone(block, bi)) {
    // Retry failed lazy-load when clicking an error-state slim block;
    // otherwise collapse.
    if (!block.text && block.think_id && thinkingContent.errors.value[block.think_id]) {
      thinkingContent.loadThinking(block.think_id, props.msgId, props.sessionId)
        .catch(() => { /* error surfaced via errors ref */ })
    } else {
      triggerThinkingCollapse(blockKey)
    }
  }
}
```

`getThinkingHtml`（`:550`）替换为：

```js
/** Get HTML for thinking block content. Live blocks render text inline;
 *  slim blocks (think_id) render from the lazy-load cache/loading/error state. */
function getThinkingHtml(bi, block) {
  if (block.text) {
    return getThinkingTextHtml(block.text, bi, block)
  }
  if (block.think_id) {
    const text = thinkingContent.cachedText(block.think_id)
    if (text) return renderMarkdownHtml(text)
    if (thinkingContent.errors.value[block.think_id]) {
      return `<div class="thinking-load-error"><span>${t('chat.contentBlocks.thinkingLoadFailed')}</span><button class="thinking-retry-btn" onclick="this.closest('.chat-thinking').querySelector('.thinking-header').click()">${t('chat.contentBlocks.retry')}</button></div>`
    }
    return '<div class="placeholder-dots"><span></span><span></span><span></span></div>'
  }
  return ''
}

/** Existing throttled streaming/inline render path (unchanged behavior). */
function getThinkingTextHtml(text, bi, block) {
  if (!props.streaming || !props.active) {
    return renderMarkdownHtml(text)
  }
  const cacheKey = `t-${stableBlockKey(bi, block)}`
  if (blockHtmlCache.value[cacheKey] !== undefined) {
    if (!_throttleTimer) {
      const newCache = { ...blockHtmlCache.value }
      newCache[cacheKey] = renderMarkdownHtml(text)
      blockHtmlCache.value = newCache
      _throttleTimer = setTimeout(flushBlockHtml, THROTTLE_MS)
    } else {
      _throttlePending = true
    }
    return blockHtmlCache.value[cacheKey]
  }
  const html = renderMarkdownHtml(text)
  blockHtmlCache.value = { ...blockHtmlCache.value, [cacheKey]: html }
  return html
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `npx vitest run web/src/components/chat/__tests__/ContentBlocks.test.ts`
Expected: PASS

- [ ] **Step 6: 跑前端全量 thinking/chat 相关测试防回归**

Run: `npx vitest run web/src/components/chat/__tests__/ContentBlocks.test.ts web/src/utils/__tests__/chatStreamUtils.test.ts`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/src/components/chat/ContentBlocks.vue web/src/components/chat/__tests__/ContentBlocks.test.ts web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(web): lazy-load thinking text on expand"
```

---

### Task 10: 会话切换清空 thinking 缓存

**Files:**
- Modify: `web/src/composables/useChatSession.ts:581`（switchSession）或 `useChatRender.ts:94`（currentSessionId watch）
- Test: 复用 Task 8 的 `clearThinkingCache` 行为（已覆盖），本任务为接线

- [ ] **Step 1: 接线清缓存**

`web/src/composables/useChatRender.ts` 的 `watch(currentSessionId, ...)`（`:94`）内补一行：

```ts
watch(currentSessionId, () => {
  staticBlockCache.clear()
  clearThinkingCache()
})
```

`useChatRender.ts` 顶部 import：

```ts
import { clearThinkingCache } from '@/composables/useThinkingContent.ts'
```

注：`useThinkingContent.ts` 需要 `export { clearThinkingCache }`（Task 8 中已通过 composable 返回暴露；此处模块级再单独导出一次）。在 `useThinkingContent.ts` 追加：

```ts
export { clearThinkingCache }
```

（若报循环依赖，把 `clearThinkingCache` 提为模块函数后再导出，按实际报错调整。）

- [ ] **Step 2: 跑前端测试防回归**

Run: `npx vitest run web/src/composables/__tests__/useChatRender.test.ts web/src/composables/__tests__/useChatSession.test.ts`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add web/src/composables/useChatRender.ts web/src/composables/useThinkingContent.ts
git commit -m "feat(web): clear thinking cache on session switch"
```

---

## 验证清单（全部完成后）

```bash
go test ./...                  # 后端全量（含新增 thinking/migrate/handler/fork 测试）
npx vitest run                # 前端全量
./scripts/pre-push-checks.sh --skip-coverage   # 提交前检查（若 coverage 门槛卡住再用）
```

## 已知取舍

- `rag message <id>` CLI（`internal/cli/rag.go`）slim 后不再显示 thinking——不在本次范围，后续可在 `GetMessageByID` 回填
- RAG 索引/摘要本就排除 thinking 块，不受影响
