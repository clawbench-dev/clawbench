package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// --- RegisterSessionCancel / UnregisterSessionCancel ---

func TestRegisterSessionCancel(t *testing.T) {
	cleanupCancels()
	defer cleanupCancels()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterSessionCancel("session-cancel-1", cancel)

	// Cancel should be stored; loading and calling it should cancel the context
	val, ok := sessionCancels.Load("session-cancel-1")
	assert.True(t, ok)
	loadedCancel, ok := val.(context.CancelFunc)
	assert.True(t, ok)
	assert.NotNil(t, loadedCancel)
}

func TestUnregisterSessionCancel(t *testing.T) {
	cleanupCancels()
	defer cleanupCancels()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterSessionCancel("session-cancel-2", cancel)
	UnregisterSessionCancel("session-cancel-2")

	_, ok := sessionCancels.Load("session-cancel-2")
	assert.False(t, ok)
}

func TestUnregisterSessionCancel_Idempotent(t *testing.T) {
	cleanupCancels()

	// Should not panic when deleting nonexistent key
	assert.NotPanics(t, func() {
		UnregisterSessionCancel("nonexistent")
	})
}

// --- GetAndClearCancelReason ---

func TestGetAndClearCancelReason_UserReason(t *testing.T) {
	cleanupCancelReasons()
	defer cleanupCancelReasons()

	sessionCancelReasons.Store("session-reason-1", "user")

	reason := GetAndClearCancelReason("session-reason-1")
	assert.Equal(t, "user", reason)

	// Should be cleared after first call
	reason2 := GetAndClearCancelReason("session-reason-1")
	assert.Equal(t, "", reason2)
}

func TestGetAndClearCancelReason_DisconnectReason(t *testing.T) {
	cleanupCancelReasons()
	defer cleanupCancelReasons()

	sessionCancelReasons.Store("session-reason-2", "disconnect")

	reason := GetAndClearCancelReason("session-reason-2")
	assert.Equal(t, "disconnect", reason)
}

func TestGetAndClearCancelReason_NoReason(t *testing.T) {
	cleanupCancelReasons()

	reason := GetAndClearCancelReason("nonexistent")
	assert.Equal(t, "", reason)
}

func TestGetAndClearCancelReason_NonStringValue(t *testing.T) {
	cleanupCancelReasons()
	defer cleanupCancelReasons()

	// Store a non-string value to trigger the safe type assertion path (ISS-126)
	sessionCancelReasons.Store("session-nonstring", 12345)

	reason := GetAndClearCancelReason("session-nonstring")
	assert.Equal(t, "", reason)
}

// --- SetCancelReason ---

func TestSetCancelReason_StoresReason(t *testing.T) {
	cleanupCancelReasons()
	defer cleanupCancelReasons()

	SetCancelReason("session-set-1", "disconnect")

	reason := GetAndClearCancelReason("session-set-1")
	assert.Equal(t, "disconnect", reason)

	// Should be cleared after first read
	reason2 := GetAndClearCancelReason("session-set-1")
	assert.Equal(t, "", reason2)
}

func TestSetCancelReason_OverwritesPrevious(t *testing.T) {
	cleanupCancelReasons()
	defer cleanupCancelReasons()

	SetCancelReason("session-set-2", "disconnect")
	SetCancelReason("session-set-2", "user")

	reason := GetAndClearCancelReason("session-set-2")
	assert.Equal(t, "user", reason)
}

// --- CancelSession ---

func TestCancelSession_WithCancelFunc(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	ctx, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel("session-cancel-3", cancel)
	SetSessionRunning("session-cancel-3", true)
	RegisterSessionStream("session-cancel-3")

	result := CancelSession("session-cancel-3")
	assert.True(t, result)

	// Context should be cancelled
	assert.Error(t, ctx.Err())

	// Session should no longer be running
	assert.False(t, IsSessionRunning("session-cancel-3"))

	// Cancel reason should be "user"
	reason := GetAndClearCancelReason("session-cancel-3")
	assert.Equal(t, "user", reason)

	// Cancel func should be removed
	_, ok := sessionCancels.Load("session-cancel-3")
	assert.False(t, ok)
}

func TestCancelSession_NotRunning_NoCancelFunc(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	// Session not running and no cancel func - idempotent success
	result := CancelSession("session-idle")
	assert.True(t, result)
}

func TestCancelSession_Running_NoCancelFunc(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	SetSessionRunning("session-stuck", true)

	// Running session with no cancel func - force-clear to unstick
	result := CancelSession("session-stuck")
	assert.True(t, result)
	assert.False(t, IsSessionRunning("session-stuck"))
}

func TestCancelSession_Running_NoCancelFunc_ClearsQueue(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	SetSessionRunning("session-stuck-queue", true)
	// Enqueue a message to verify it gets cleared on force-cancel
	EnqueueMessage("session-stuck-queue", model.QueuedMessage{Text: "hello"})

	result := CancelSession("session-stuck-queue")
	assert.True(t, result)
	assert.False(t, IsSessionRunning("session-stuck-queue"))
	// Queue should be cleared
	assert.Nil(t, GetQueue("session-stuck-queue"))
}

func TestCancelSession_StuckThenNewMessage(t *testing.T) {
	// Simulate the exact bug scenario: session gets stuck (running=true, no cancel),
	// user cancels (force-clear), then sends a new message which should succeed.
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	// Simulate stuck state
	SetSessionRunning("session-bug-repro", true)

	// Cancel should force-clear and return true
	result := CancelSession("session-bug-repro")
	assert.True(t, result)
	assert.False(t, IsSessionRunning("session-bug-repro"))

	// Now TrySetSessionRunning should succeed (the session is unstuck)
	result2 := TrySetSessionRunning("session-bug-repro")
	assert.True(t, result2, "session should be startable after force-clear")
}

func TestCancelSession_CallsACPConnManagerCancelTurn(t *testing.T) {
	// Verify that CancelSession does not panic when ACPConnManager has no
	// connection for the session (CancelTurn is a no-op on nil conn).
	// The actual CancelTurn behavior is tested in acp_pool_test.go.
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RegisterSessionCancel("session-acp-cancel", cancel)
	SetSessionRunning("session-acp-cancel", true)
	RegisterSessionStream("session-acp-cancel")

	// CancelSession should succeed even without an ACP connection
	result := CancelSession("session-acp-cancel")
	assert.True(t, result)
	assert.False(t, IsSessionRunning("session-acp-cancel"))
	// Context should be cancelled
	assert.Error(t, ctx.Err())
}

func TestCancelSession_SendsCancelledEvent(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	_, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel("session-event", cancel)
	SetSessionRunning("session-event", true)
	ch := RegisterSessionStream("session-event")

	result := CancelSession("session-event")
	assert.True(t, result)

	// Should receive a cancelled event
	select {
	case event := <-ch:
		assert.Equal(t, "cancelled", event.Type)
	case <-time.After(time.Second):
		t.Fatal("expected cancelled event on stream channel")
	}
}

// --- ForceCancelSession ---

func TestForceCancelSession(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	ctx, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel("session-force", cancel)
	SetSessionRunning("session-force", true)

	ForceCancelSession("session-force")

	// Context should be cancelled
	assert.Error(t, ctx.Err())

	// Cancel reason should be "disconnect"
	reason := GetAndClearCancelReason("session-force")
	assert.Equal(t, "disconnect", reason)

	// Cancel func should be removed
	_, ok := sessionCancels.Load("session-force")
	assert.False(t, ok)
}

func TestForceCancelSession_NotFound(t *testing.T) {
	cleanupAllSessionState()

	// Should not panic on nonexistent session
	assert.NotPanics(t, func() {
		ForceCancelSession("nonexistent")
	})
}

// --- SendSessionEvent ---

func TestSendSessionEvent_Success(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	ch := RegisterSessionStream("session-event-test")

	event := ai.StreamEvent{Type: "content", Content: "hello"}
	sent := SendSessionEvent("session-event-test", event)
	assert.True(t, sent)

	received := <-ch
	assert.Equal(t, "content", received.Type)
	assert.Equal(t, "hello", received.Content)
}

func TestSendSessionEvent_SessionNotFound(t *testing.T) {
	cleanupStreams()

	sent := SendSessionEvent("nonexistent", ai.StreamEvent{Type: "content"})
	assert.False(t, sent)
}

func TestSendSessionEvent_FullChannel(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	RegisterSessionStream("session-full")

	// Fill the channel buffer (capacity is sessionStreamBufferSize)
	for range sessionStreamBufferSize {
		sent := SendSessionEvent("session-full", ai.StreamEvent{Type: "content", Content: "x"})
		assert.True(t, sent)
	}

	// Next send should fail (non-blocking)
	sent := SendSessionEvent("session-full", ai.StreamEvent{Type: "done"})
	assert.False(t, sent, "SendSessionEvent should return false when channel is full")
}

// --- TrySetSessionRunning ---

func TestTrySetSessionRunning_Success(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	result := TrySetSessionRunning("session-try-1")
	assert.True(t, result)
	assert.True(t, IsSessionRunning("session-try-1"))
}

func TestTrySetSessionRunning_AlreadyRunning(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	result1 := TrySetSessionRunning("session-try-2")
	assert.True(t, result1)

	result2 := TrySetSessionRunning("session-try-2")
	assert.False(t, result2, "Second TrySetSessionRunning should return false")
}

func TestTrySetSessionRunning_DifferentSessions(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	result1 := TrySetSessionRunning("session-a")
	assert.True(t, result1)
	assert.True(t, IsSessionRunning("session-a"))

	result2 := TrySetSessionRunning("session-b")
	assert.True(t, result2)
	assert.True(t, IsSessionRunning("session-b"))

	// Both should be running independently
	assert.True(t, IsSessionRunning("session-a"))
}

func TestTrySetSessionRunning_FailedTryDoesNotAffectExisting(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	// First TrySet succeeds
	assert.True(t, TrySetSessionRunning("session-x"))
	// Second TrySet on same ID fails
	assert.False(t, TrySetSessionRunning("session-x"))
	// But session is still marked as running
	assert.True(t, IsSessionRunning("session-x"))
}

func TestSetSessionRunning_TrySetMixedSequence(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	// Start via SetSessionRunning
	SetSessionRunning("session-mix", true)
	assert.True(t, IsSessionRunning("session-mix"))

	// TrySetSessionRunning on already-running session should fail
	assert.False(t, TrySetSessionRunning("session-mix"))

	// Stop via SetSessionRunning
	SetSessionRunning("session-mix", false)
	assert.False(t, IsSessionRunning("session-mix"))

	// Now TrySetSessionRunning should succeed
	assert.True(t, TrySetSessionRunning("session-mix"))
	assert.True(t, IsSessionRunning("session-mix"))
}

func TestTrySetSessionRunning_Concurrent(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	// Multiple goroutines try to set the same session as running.
	// Exactly one should succeed.
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if TrySetSessionRunning("session-concurrent-try") {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, 1, successCount, "Exactly one TrySetSessionRunning should succeed")
	assert.True(t, IsSessionRunning("session-concurrent-try"))
}

func TestSetSessionRunning_FalseRemovesKey(t *testing.T) {
	cleanupActiveSessions()
	defer cleanupActiveSessions()

	SetSessionRunning("session-rm", true)
	assert.True(t, IsSessionRunning("session-rm"))

	SetSessionRunning("session-rm", false)
	assert.False(t, IsSessionRunning("session-rm"))
}

// --- Concurrent access tests ---

func TestSendSessionEvent_ConcurrentAccess(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	RegisterSessionStream("session-concurrent")

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Send 50 events concurrently (buffer is sessionStreamBufferSize, so most should succeed)
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sent := SendSessionEvent("session-concurrent", ai.StreamEvent{Type: "content"})
			if sent {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, 50, successCount, "All 50 events should be sent (buffer is sessionStreamBufferSize)")
}

// --- Helpers ---

func cleanupCancels() {
	sessionCancels.Range(func(key, _ interface{}) bool {
		sessionCancels.Delete(key)
		return true
	})
}

func cleanupCancelReasons() {
	sessionCancelReasons.Range(func(key, _ interface{}) bool {
		sessionCancelReasons.Delete(key)
		return true
	})
}

func cleanupActiveSessions() {
	activeMu.Lock()
	defer activeMu.Unlock()
	activeSessions = make(map[string]bool)
}

func cleanupAllSessionState() {
	cleanupActiveSessions()
	cleanupCancels()
	cleanupCancelReasons()
	cleanupStreams()
}

// --- getSessionResponsePreview tests ---

func setupChatTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS chat_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_path TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
		content TEXT NOT NULL,
		files TEXT,
		session_id TEXT,
		backend TEXT NOT NULL DEFAULT 'claude',
		streaming INTEGER NOT NULL DEFAULT 0,
		indexed INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func insertTestMessage(t *testing.T, db *sql.DB, sessionID, role, content string) {
	t.Helper()
	_, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, ?, ?, ?, 'claude', 0)",
		"/test", role, content, sessionID)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestGetSessionResponsePreview_WithTextBlock(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	content := model.ContentBlock{Type: "text", Text: "你好，这是AI的回复内容"}
	blocks := map[string]any{"blocks": []model.ContentBlock{content}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-1", "user", "问题")
	insertTestMessage(t, db, "session-preview-1", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-1")
	assert.Equal(t, "你好，这是AI的回复内容", result)
}

func TestGetSessionResponsePreview_Truncation(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// responsePreviewMaxRunes+1 runes — should be truncated
	longText := strings.Repeat("测", responsePreviewMaxRunes+1)
	content := model.ContentBlock{Type: "text", Text: longText}
	blocks := map[string]any{"blocks": []model.ContentBlock{content}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-2", "user", "问题")
	insertTestMessage(t, db, "session-preview-2", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-2")
	runes := []rune(longText)
	assert.Equal(t, string(runes[:responsePreviewMaxRunes])+"…", result)
	assert.Equal(t, responsePreviewMaxRunes+1, utf8.RuneCountInString(result)) // maxRunes + ellipsis
}

// TestGetSessionResponsePreview_FallbackTruncation verifies that the longest-text
// fallback path truncates when the best text block exceeds responsePreviewMaxRunes.
func TestGetSessionResponsePreview_FallbackTruncation(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	// [text("very long..."), tool_use] — no text AFTER tool_use, falls back to longest text block
	longText := strings.Repeat("测", responsePreviewMaxRunes+1)
	textBlock := model.ContentBlock{Type: "text", Text: longText}
	toolBlock := model.ContentBlock{Type: "tool_use", Name: "Bash", ID: "tool-1"}
	blocks := map[string]any{"blocks": []model.ContentBlock{textBlock, toolBlock}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-fallback-trunc", "user", "分析代码")
	insertTestMessage(t, db, "session-preview-fallback-trunc", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-fallback-trunc")
	runes := []rune(longText)
	assert.Equal(t, string(runes[:responsePreviewMaxRunes])+"…", result)
}

func TestGetSessionResponsePreview_NoAssistantMessage(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	insertTestMessage(t, db, "session-preview-3", "user", "只有用户消息")

	result := getSessionResponsePreview("session-preview-3")
	assert.Equal(t, "", result)
}

func TestGetSessionResponsePreview_NoMessages(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	result := getSessionResponsePreview("session-nonexistent")
	assert.Equal(t, "", result)
}

func TestGetSessionResponsePreview_SkipsToolUseBlocks(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	toolBlock := model.ContentBlock{Type: "tool_use", Name: "Read", ID: "tool-1"}
	textBlock := model.ContentBlock{Type: "text", Text: "工具执行后的文本"}
	blocks := map[string]any{"blocks": []model.ContentBlock{toolBlock, textBlock}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-4", "user", "问题")
	insertTestMessage(t, db, "session-preview-4", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-4")
	assert.Equal(t, "工具执行后的文本", result)
}

func TestGetSessionResponsePreview_PrefersTextAfterLastToolUse(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Scenario: [text("Reading file..."), tool_use, text("Here is the analysis")]
	// The preview should return "Here is the analysis", not "Reading file..."
	textBeforeTool := model.ContentBlock{Type: "text", Text: "正在读取文件…"}
	toolBlock := model.ContentBlock{Type: "tool_use", Name: "Read", ID: "tool-1"}
	textAfterTool := model.ContentBlock{Type: "text", Text: "这是最终的分析结果"}
	blocks := map[string]any{"blocks": []model.ContentBlock{textBeforeTool, toolBlock, textAfterTool}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-after-tool", "user", "分析代码")
	insertTestMessage(t, db, "session-preview-after-tool", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-after-tool")
	assert.Equal(t, "这是最终的分析结果", result)
}

func TestGetSessionResponsePreview_MultipleToolUses(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Scenario: [tool_use, text("intermediate"), tool_use, text("final answer")]
	// Should return "final answer" — text after the LAST tool_use
	tool1 := model.ContentBlock{Type: "tool_use", Name: "Read", ID: "tool-1"}
	textMiddle := model.ContentBlock{Type: "text", Text: "中间结果"}
	tool2 := model.ContentBlock{Type: "tool_use", Name: "Grep", ID: "tool-2"}
	textFinal := model.ContentBlock{Type: "text", Text: "最终结论"}
	blocks := map[string]any{"blocks": []model.ContentBlock{tool1, textMiddle, tool2, textFinal}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-multi-tool", "user", "搜索代码")
	insertTestMessage(t, db, "session-preview-multi-tool", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-multi-tool")
	assert.Equal(t, "最终结论", result)
}

func TestGetSessionResponsePreview_OnlyToolUses(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Only tool_use blocks, no text after — should return empty
	tool1 := model.ContentBlock{Type: "tool_use", Name: "Read", ID: "tool-1"}
	tool2 := model.ContentBlock{Type: "tool_use", Name: "Grep", ID: "tool-2"}
	blocks := map[string]any{"blocks": []model.ContentBlock{tool1, tool2}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-only-tools", "user", "搜索代码")
	insertTestMessage(t, db, "session-preview-only-tools", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-only-tools")
	assert.Equal(t, "", result)
}

func TestGetSessionResponsePreview_TextBeforeToolOnly(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// [text("thinking..."), tool_use] — no text AFTER tool_use, falls back to longest text block
	textBlock := model.ContentBlock{Type: "text", Text: "让我思考一下"}
	toolBlock := model.ContentBlock{Type: "tool_use", Name: "Read", ID: "tool-1"}
	blocks := map[string]any{"blocks": []model.ContentBlock{textBlock, toolBlock}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-text-before-tool", "user", "分析代码")
	insertTestMessage(t, db, "session-preview-text-before-tool", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-text-before-tool")
	assert.Equal(t, "让我思考一下", result)
}

// --- Real-data based tests (extracted from ClawBench production database) ---

func TestGetSessionResponsePreview_RealData_TextThenToolThenSummary(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Real pattern from session 93c986e1, message id=1063:
	//   [thinking, text("方案一已经在上一轮实现了。验证一下当前状态："), tool_use(Bash), tool_use(Bash), text("方案一已在 commit b4d7b73 中实现完毕...")]
	// Before fix: would return "方案一已经在上一轮实现了..." (intermediate commentary)
	// After fix: should return "方案一已在 commit b4d7b73 中实现完毕..." (final answer)
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "Let me verify the current state of the implementation."},
		{Type: "text", Text: "方案一已经在上一轮实现了。验证一下当前状态："},
		{Type: "tool_use", Name: "Bash", ID: "tool-verify-1"},
		{Type: "tool_use", Name: "Bash", ID: "tool-verify-2"},
		{Type: "text", Text: "方案一已在 commit `b4d7b73` 中实现完毕，全部 14 个测试通过。"},
	}
	contentJSON, _ := json.Marshal(map[string]any{"blocks": blocks})
	insertTestMessage(t, db, "session-real-text-tool-summary", "user", "实现方案一")
	insertTestMessage(t, db, "session-real-text-tool-summary", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-real-text-tool-summary")
	assert.Equal(t, "方案一已在 commit `b4d7b73` 中实现完毕，全部 14 个测试通过。", result)
}

func TestGetSessionResponsePreview_RealData_ToolThenWorktreeReport(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Real pattern from session dd1968cf, message id=1059:
	//   [thinking, tool_use(Bash), text("Worktree 已创建：\n\n- **路径**: `/root/code/clawbench/.worktrees/fix-push-summary-55`...")]
	// Simple case: tool then final answer text — should return the text
	finalText := "Worktree 已创建：\n\n- **路径**: `/root/code/clawbench/.worktrees/fix-push-summary-55`\n- **分支**: `fix/push-summary-55`"
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "I'll create a worktree for this fix."},
		{Type: "tool_use", Name: "Bash", ID: "tool-worktree"},
		{Type: "text", Text: finalText},
	}
	contentJSON, _ := json.Marshal(map[string]any{"blocks": blocks})
	insertTestMessage(t, db, "session-real-tool-worktree", "user", "创建worktree")
	insertTestMessage(t, db, "session-real-tool-worktree", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-real-tool-worktree")
	// Should start with the final answer, not with thinking or tool output
	assert.Contains(t, result, "Worktree 已创建")
	// With responsePreviewMaxRunes=512, this text (110 runes) fits without truncation
	assert.Equal(t, finalText, result)
}

func TestGetSessionResponsePreview_RealData_MultiToolInterleavedWithText(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Real pattern from session da4003a0, message id=1047:
	//   [thinking, tool_use(Bash), text("有问题！..."), tool_use(Bash), tool_use(Bash),
	//    text("确认问题：..."), tool_use(Bash), text("两个文件..."), tool_use(Bash),
	//    tool_use(Bash), text("等等..."), tool_use(Bash), text("现在删除..."),
	//    tool_use(Bash), tool_use(Bash), text("最后验证..."), tool_use(Bash),
	//    tool_use(Bash), text("清理完成！总结一下做了什么：...")]
	// 18 blocks total — should return the LAST text after the LAST tool_use
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "Let me investigate the root directory."},
		{Type: "tool_use", Name: "Bash", ID: "tool-ls"},
		{Type: "text", Text: "有问题！`/root/code/` 根目录下出现了不该有的文件。"},
		{Type: "tool_use", Name: "Bash", ID: "tool-check-1"},
		{Type: "tool_use", Name: "Bash", ID: "tool-check-2"},
		{Type: "text", Text: "确认问题：这是某个子 Agent 误执行了 pnpm 命令。"},
		{Type: "tool_use", Name: "Bash", ID: "tool-rm"},
		{Type: "text", Text: "清理完成！总结一下做了什么：\n\n### 清理操作\n\n1. **删除了根目录误创建的文件**"},
	}
	contentJSON, _ := json.Marshal(map[string]any{"blocks": blocks})
	insertTestMessage(t, db, "session-real-multi-interleaved", "user", "检查根目录")
	insertTestMessage(t, db, "session-real-multi-interleaved", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-real-multi-interleaved")
	// Should return text after last tool_use (tool-rm), not the earlier texts
	assert.Equal(t, "清理完成！总结一下做了什么：\n\n### 清理操作\n\n1. **删除了根目录误创建的文件**", result)
}

func TestGetSessionResponsePreview_RealData_ThinkingThenToolThenIssueLink(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Real pattern from session bb92e480, message id=1039:
	//   [thinking, tool_use(Bash), text("已创建 Issue: https://github.com/xulongzhe/clawbench/issues/55")]
	// Short final text — perfect for push notification
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "I should create a GitHub issue for this bug."},
		{Type: "tool_use", Name: "Bash", ID: "tool-gh-issue"},
		{Type: "text", Text: "已创建 Issue: https://github.com/xulongzhe/clawbench/issues/55"},
	}
	contentJSON, _ := json.Marshal(map[string]any{"blocks": blocks})
	insertTestMessage(t, db, "session-real-issue-link", "user", "创建Issue")
	insertTestMessage(t, db, "session-real-issue-link", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-real-issue-link")
	assert.Equal(t, "已创建 Issue: https://github.com/xulongzhe/clawbench/issues/55", result)
}

func TestGetSessionResponsePreview_RealData_ThreeToolsThenWorktreeReport(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Real pattern from session bb92e480, message id=1055:
	//   [thinking, tool_use(Bash), tool_use(Bash), tool_use(Bash), text("Worktree 已创建：...")]
	// Multiple consecutive tool_use blocks, then final text
	finalText := "Worktree 已创建：\n\n- **路径**: `/root/code/clawbench/.worktrees/fix-jpush-init-timing`"
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "I need to create a worktree for the JPush fix."},
		{Type: "tool_use", Name: "Bash", ID: "tool-fetch"},
		{Type: "tool_use", Name: "Bash", ID: "tool-branch"},
		{Type: "tool_use", Name: "Bash", ID: "tool-worktree"},
		{Type: "text", Text: finalText},
	}
	contentJSON, _ := json.Marshal(map[string]any{"blocks": blocks})
	insertTestMessage(t, db, "session-real-three-tools", "user", "创建worktree")
	insertTestMessage(t, db, "session-real-three-tools", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-real-three-tools")
	assert.Contains(t, result, "Worktree 已创建")
	// With responsePreviewMaxRunes=512, this text fits without truncation
	assert.Equal(t, finalText, result)
}

func TestGetSessionResponsePreview_RealData_PureTextSummary(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Real pattern from session id=726 (no tool_use at all):
	//   [text("好的。后台耗电优化到此为止，总结已完成的改动：\n\n1. **webView.onPause()**...")]
	// Pure text response — should return as-is (lastToolIdx=-1, scan from start)
	finalText := "好的。后台耗电优化到此为止，总结已完成的改动：\n\n1. **`webView.onPause()`** — 后台停止渲染管线，释放 CPU/GPU\n2. **`webView.pauseTimers()`** — 强制停止所有 JS 定时器"
	blocks := []model.ContentBlock{
		{Type: "text", Text: finalText},
	}
	contentJSON, _ := json.Marshal(map[string]any{"blocks": blocks})
	insertTestMessage(t, db, "session-real-pure-text", "user", "还有其他优化吗")
	insertTestMessage(t, db, "session-real-pure-text", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-real-pure-text")
	assert.Contains(t, result, "后台耗电优化到此为止")
	// With responsePreviewMaxRunes=512, this text fits without truncation
	assert.Equal(t, finalText, result)
}

func TestGetSessionResponsePreview_UsesLastAssistantMessage(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	firstContent := model.ContentBlock{Type: "text", Text: "第一次回复"}
	firstBlocks := map[string]any{"blocks": []model.ContentBlock{firstContent}}
	firstJSON, _ := json.Marshal(firstBlocks)
	insertTestMessage(t, db, "session-preview-5", "user", "问题1")
	insertTestMessage(t, db, "session-preview-5", "assistant", string(firstJSON))

	secondContent := model.ContentBlock{Type: "text", Text: "第二次回复"}
	secondBlocks := map[string]any{"blocks": []model.ContentBlock{secondContent}}
	secondJSON, _ := json.Marshal(secondBlocks)
	insertTestMessage(t, db, "session-preview-5", "user", "问题2")
	insertTestMessage(t, db, "session-preview-5", "assistant", string(secondJSON))

	result := getSessionResponsePreview("session-preview-5")
	assert.Equal(t, "第二次回复", result)
}

func TestGetSessionResponsePreview_InvalidJSON(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	insertTestMessage(t, db, "session-preview-6", "user", "问题")
	insertTestMessage(t, db, "session-preview-6", "assistant", "not valid json {{{")

	result := getSessionResponsePreview("session-preview-6")
	assert.Equal(t, "", result)
}

func TestGetSessionResponsePreview_NoTextBlocks(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	toolBlock := model.ContentBlock{Type: "tool_use", Name: "Read", ID: "tool-1"}
	blocks := map[string]any{"blocks": []model.ContentBlock{toolBlock}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-7", "user", "问题")
	insertTestMessage(t, db, "session-preview-7", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-7")
	assert.Equal(t, "", result)
}

func TestGetSessionResponsePreview_ExactMaxRunes(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Exactly responsePreviewMaxRunes runes — should NOT be truncated
	exactText := strings.Repeat("一二三四", responsePreviewMaxRunes/4)
	content := model.ContentBlock{Type: "text", Text: exactText}
	blocks := map[string]any{"blocks": []model.ContentBlock{content}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-8", "user", "问题")
	insertTestMessage(t, db, "session-preview-8", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-8")
	assert.Equal(t, exactText, result)
	assert.Equal(t, responsePreviewMaxRunes, utf8.RuneCountInString(result))
}

func TestGetSessionResponsePreview_OneOverMaxRunes(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// responsePreviewMaxRunes+1 runes — should be truncated to maxRunes + …
	longText := strings.Repeat("一二三四", responsePreviewMaxRunes/4) + "五"
	content := model.ContentBlock{Type: "text", Text: longText}
	blocks := map[string]any{"blocks": []model.ContentBlock{content}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-preview-9", "user", "问题")
	insertTestMessage(t, db, "session-preview-9", "assistant", string(contentJSON))

	result := getSessionResponsePreview("session-preview-9")
	assert.Equal(t, strings.Repeat("一二三四", responsePreviewMaxRunes/4)+"…", result)
}

// --- emitSessionEvent with response preview ---

func TestEmitSessionEvent_CompletedWithPreview(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Insert assistant message for preview
	content := model.ContentBlock{Type: "text", Text: "AI完成了任务"}
	blocks := map[string]any{"blocks": []model.ContentBlock{content}}
	contentJSON, _ := json.Marshal(blocks)
	insertTestMessage(t, db, "session-emit-1", "user", "问题")
	insertTestMessage(t, db, "session-emit-1", "assistant", string(contentJSON))

	// Insert a session row so GetSessionProjectPath can look it up
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS chat_sessions (id TEXT PRIMARY KEY, project_path TEXT, backend TEXT, title TEXT, external_session_id TEXT DEFAULT '')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		"session-emit-1", "/home/user/test-project", "codebuddy", "Test Session")
	require.NoError(t, err)

	// Set up ws manager and a subscriber to capture the event
	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-emit", "")
	_ = sub

	EmitSessionEvent("session-emit-1", "completed", true)

	// Verify the buffered event has response_preview
	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected at least one buffered event")
	}
	data, ok := buffered[0].Data.(*ws.SessionUpdateData)
	if !ok {
		t.Fatal("expected SessionUpdateData")
	}
	assert.Equal(t, "completed", data.Status)
	assert.Equal(t, "session-emit-1", data.SessionID)
	assert.Equal(t, "AI完成了任务", data.ResponsePreview)
	assert.Equal(t, "/home/user/test-project", data.ProjectPath)
}

func TestEmitSessionEvent_RunningNoPreview(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-emit2", "")
	_ = sub

	EmitSessionEvent("session-emit-2", "running", false)

	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected at least one buffered event")
	}
	data, ok := buffered[0].Data.(*ws.SessionUpdateData)
	if !ok {
		t.Fatal("expected SessionUpdateData")
	}
	assert.Equal(t, "running", data.Status)
	assert.Equal(t, "", data.ResponsePreview)
}

// --- GetSessionStream edge cases ---

func TestGetSessionStream_NotRegistered(t *testing.T) {
	cleanupStreams()

	ch, ok := GetSessionStream("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, ch)
}

func TestTryClaimSSEStream_BasicFlow(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	RegisterSessionStream("claim-test")
	defer UnregisterSessionStream("claim-test")

	// First claim succeeds
	assert.True(t, TryClaimSSEStream("claim-test"))
	// Second claim fails
	assert.False(t, TryClaimSSEStream("claim-test"))
	// Release and reclaim
	ReleaseSSEStream("claim-test")
	assert.True(t, TryClaimSSEStream("claim-test"))
	ReleaseSSEStream("claim-test")
}

// --- emitSessionEvent with nil ws manager ---

func TestEmitSessionEvent_NilManager(t *testing.T) {
	ws.SetManagerForTest(nil)

	// Should not panic when ws manager is nil
	assert.NotPanics(t, func() {
		EmitSessionEvent("session-nil-mgr", "running", false)
	})
}

// --- CancelSession with bad cancel type ---

func TestCancelSession_BadCancelType(t *testing.T) {
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	// Store a non-CancelFunc value
	sessionCancels.Store("session-bad-cancel", "not-a-cancel-func")
	SetSessionRunning("session-bad-cancel", true)

	result := CancelSession("session-bad-cancel")
	assert.False(t, result, "should return false when cancel func has wrong type")
}

// --- UnregisterSessionStream ---

func TestUnregisterSessionStream(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	ch := RegisterSessionStream("session-unreg")
	UnregisterSessionStream("session-unreg")

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after unregister")
}

func TestUnregisterSessionStream_Nonexistent(t *testing.T) {
	cleanupStreams()

	// Should not panic
	assert.NotPanics(t, func() {
		UnregisterSessionStream("nonexistent")
	})
}

// --- SetSessionRunning with skipEvent ---

func TestSetSessionRunning_SkipEventTrue(t *testing.T) {
	cleanupActiveSessions()

	// Set running with skipEvent=true — should NOT emit event
	SetSessionRunning("session-skip", true, true)
	assert.True(t, IsSessionRunning("session-skip"))

	// Stop with skipEvent=true — should NOT emit completed event
	SetSessionRunning("session-skip", false, true)
	assert.False(t, IsSessionRunning("session-skip"))
}

// --- emitTaskEvent tests ---

func TestEmitTaskEvent_WithSessionIDAndProjectPath(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-task-emit", "")
	_ = sub

	emitTaskEvent("42", "completed", "100", "session-task-1", "/home/user/project", "test task")

	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected at least one buffered event")
	}
	data, ok := buffered[0].Data.(*ws.TaskUpdateData)
	if !ok {
		t.Fatal("expected TaskUpdateData")
	}
	assert.Equal(t, "42", data.TaskID)
	assert.Equal(t, "completed", data.Status)
	assert.Equal(t, "100", data.ExecutionID)
	assert.Equal(t, "session-task-1", data.SessionID)
	assert.Equal(t, "/home/user/project", data.ProjectPath)
	assert.Equal(t, "test task", data.SessionTitle)
}

func TestEmitTaskEvent_EmptyOptionalFields(t *testing.T) {
	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-task-emit2", "")
	_ = sub

	emitTaskEvent("43", "failed", "101", "", "", "")

	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected at least one buffered event")
	}
	data, ok := buffered[0].Data.(*ws.TaskUpdateData)
	if !ok {
		t.Fatal("expected TaskUpdateData")
	}
	assert.Equal(t, "43", data.TaskID)
	assert.Equal(t, "failed", data.Status)
	assert.Equal(t, "", data.SessionID)
	assert.Equal(t, "", data.ProjectPath)
}

func TestEmitTaskEvent_NilManager(t *testing.T) {
	ws.SetManagerForTest(nil)

	// Should not panic when ws manager is nil
	assert.NotPanics(t, func() {
		emitTaskEvent("44", "running", "102", "session-nil", "/project", "")
	})
}

// --- executeTask tests (covers emitTaskEvent call sites in scheduler.go) ---

const execTaskSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	agent_source TEXT DEFAULT 'default',
	model TEXT DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	external_session_id TEXT DEFAULT '',
	deleted INTEGER NOT NULL DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(project_path, backend, id)
);
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	name TEXT NOT NULL,
	cron_expr TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	prompt TEXT NOT NULL,
	session_id TEXT,
	status TEXT NOT NULL DEFAULT 'active',
	repeat_mode TEXT NOT NULL DEFAULT 'unlimited',
	max_runs INTEGER DEFAULT 0,
	last_run_at DATETIME,
	next_run_at DATETIME,
	run_count INTEGER DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	trigger_type TEXT NOT NULL DEFAULT 'auto',
	status TEXT NOT NULL DEFAULT 'running',
	read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
CREATE TABLE IF NOT EXISTS ai_raw_responses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	raw_output TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func setupExecTaskDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(execTaskSchema)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestExecuteTask_BackendCreationFailed(t *testing.T) {
	// Set up DB with scheduler schema
	origDB := DB
	origDBRead := DBRead
	db := setupExecTaskDB(t)
	DB = db
	DBRead = db // Same instance for :memory: SQLite — data is shared
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Set up ws manager to capture events
	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Register an agent with an unsupported backend — ai.NewBackend will return error
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-unsupported-backend": {Backend: "nonexistent_backend_xyz"},
	}
	defer func() { model.Agents = origAgents }()

	// Insert a task into DB so the foreign key in task_executions works
	result, err := db.Exec(`INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, repeat_mode, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"/test-project", "Test Task", "0 * * * *", "test-unsupported-backend", "hello", "unlimited", "active")
	require.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Construct task directly (GetTaskByID fails on NULL session_id with string Scan)
	task := &model.ScheduledTask{
		ID:          taskID,
		ProjectPath: "/test-project",
		Name:        "Test Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-unsupported-backend",
		Prompt:      "hello",
		RepeatMode:  "unlimited",
		Status:      "active",
	}

	s := NewScheduler()
	defer s.Stop()

	// Subscribe a client to capture events
	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-exec", "")
	_ = sub

	// Execute the task — should fail at backend creation and emit "failed" event
	s.executeTask(task, "/test-project", "manual")

	// Give a small window for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify only "failed" event was broadcast (no "running" event when backend creation fails — ISS-128)
	buffered := sub.GetBufferedEvents()
	if len(buffered) < 1 {
		t.Fatalf("expected at least 1 buffered event (failed), got %d", len(buffered))
	}

	// Only event should be "failed" (backend creation failed — no "running" event per ISS-128 fix)
	data1, ok := buffered[0].Data.(*ws.TaskUpdateData)
	if !ok {
		t.Fatal("expected TaskUpdateData for first event")
	}
	assert.Equal(t, "failed", data1.Status)
	assert.Equal(t, fmt.Sprintf("%d", taskID), data1.TaskID)
	assert.NotEmpty(t, data1.SessionID, "failed event should have session_id")
	assert.Equal(t, "/test-project", data1.ProjectPath, "failed event should have project_path")
}

// --- executeTask: ExecuteStream error path (covers scheduler.go:681-687) ---

func TestExecuteTask_ExecuteStreamError(t *testing.T) {
	// When backend creation succeeds but ExecuteStream fails,
	// executeTask should emit "failed" events (running + failed) and return.
	origDB := DB
	origDBRead := DBRead
	db := setupExecTaskDB(t)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Set up ws manager to capture events
	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Register an agent with "codex" backend — NewBackend succeeds,
	// but ExecuteStream will fail because the codex binary doesn't exist.
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-codex": {
			ID:      "test-codex",
			Name:    "Test Codex",
			Backend: "codex",
			Command: "/nonexistent/binary/that/does/not/exist",
		},
	}
	defer func() { model.Agents = origAgents }()

	// Insert a task into DB
	result, err := db.Exec(`INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, repeat_mode, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"/test-project", "Stream Error Task", "0 * * * *", "test-codex", "hello", "unlimited", "active")
	require.NoError(t, err)
	taskID, _ := result.LastInsertId()

	task := &model.ScheduledTask{
		ID:          taskID,
		ProjectPath: "/test-project",
		Name:        "Stream Error Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-codex",
		Prompt:      "hello",
		RepeatMode:  "unlimited",
		Status:      "active",
	}

	s := NewScheduler()
	defer s.Stop()

	// Subscribe a client to capture events
	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-stream-err", "")
	_ = sub

	// Execute the task — should fail at ExecuteStream and emit "failed" event
	s.executeTask(task, "/test-project", "auto")

	// Give a small window for async processing
	time.Sleep(200 * time.Millisecond)

	// Verify "failed" event was broadcast
	buffered := sub.GetBufferedEvents()
	if len(buffered) < 1 {
		t.Fatalf("expected at least 1 buffered event, got %d", len(buffered))
	}

	// Find the "failed" event
	foundFailed := false
	for _, evt := range buffered {
		data, ok := evt.Data.(*ws.TaskUpdateData)
		if !ok {
			continue
		}
		if data.Status == "failed" {
			foundFailed = true
			assert.Equal(t, fmt.Sprintf("%d", taskID), data.TaskID)
			assert.NotEmpty(t, data.SessionID)
			break
		}
	}
	assert.True(t, foundFailed, "expected a 'failed' event to be emitted")

	// Verify execution was recorded with "failed" status
	var execStatus string
	err = db.QueryRow("SELECT status FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1", taskID).Scan(&execStatus)
	if err == nil {
		assert.Equal(t, "failed", execStatus, "execution should be marked as failed")
	}
}

// --- executeTask: agent not found path (covers scheduler.go:551-561) ---

func TestExecuteTask_AgentNotFound(t *testing.T) {
	// When the agent is not found in model.Agents, executeTask should
	// pause the task and return without creating a session.
	origDB := DB
	origDBRead := DBRead
	db := setupExecTaskDB(t)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Set up ws manager
	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// No agents registered — any agent_id will be "not found"
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	// Insert a task
	result, err := db.Exec(`INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, repeat_mode, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"/test-project", "Missing Agent Task", "0 * * * *", "nonexistent-agent", "hello", "unlimited", "active")
	require.NoError(t, err)
	taskID, _ := result.LastInsertId()

	task := &model.ScheduledTask{
		ID:          taskID,
		ProjectPath: "/test-project",
		Name:        "Missing Agent Task",
		CronExpr:    "0 * * * *",
		AgentID:     "nonexistent-agent",
		Prompt:      "hello",
		RepeatMode:  "unlimited",
		Status:      "active",
	}

	s := NewScheduler()
	defer s.Stop()

	// Execute should pause the task and not panic
	s.executeTask(task, "/test-project", "auto")

	// Verify task was paused
	var status string
	err = db.QueryRow("SELECT status FROM scheduled_tasks WHERE id = ?", taskID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "paused", status, "task should be paused when agent not found")
}

// --- executeTask: SessionExecutor delegation (covers scheduler.go:691-740) ---
// These tests simulate the code path where executeTask creates a streaming
// placeholder message, constructs SessionExecutor(ModeScheduled), calls
// RunWithChannel, and handles the result.

func TestExecuteTask_SessionExecutor_CompletedWithTerminalEvent(t *testing.T) {
	// Simulate the happy path: streaming placeholder → SessionExecutor(ModeScheduled)
	// → RunWithChannel with "done" terminal event → Finalize.
	origDB := DB
	origDBRead := DBRead
	db := setupExecTaskDB(t)
	// Need chat_metadata table for Finalize
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS chat_metadata (
		message_id INTEGER PRIMARY KEY,
		mode TEXT DEFAULT '',
		thinking_effort TEXT DEFAULT '',
		transport TEXT DEFAULT '',
		model TEXT DEFAULT '',
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		wall_ms INTEGER DEFAULT 0,
		cost_usd REAL DEFAULT 0,
		stop_reason TEXT DEFAULT '',
		is_error INTEGER DEFAULT 0,
		error_message TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Create a session for this execution
	sessionID, err := CreateSession("/test-project", "test", "Exec Task", "test", "", "default", "scheduled")
	require.NoError(t, err)

	// Step 1: Create streaming placeholder message (same as executeTask line 691-692)
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	msgID, err := AddChatMessage("/test-project", "test", sessionID, "assistant", string(emptyContent), nil, true, "")
	require.NoError(t, err)
	require.NotZero(t, msgID, "expected non-zero msgID for streaming placeholder")

	// Step 2: Build event channel with content + terminal event
	events := []ai.StreamEvent{
		{Type: "content", Content: "scheduled task output"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 10, OutputTokens: 20}},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)

	// Step 3: Create SessionExecutor with ModeScheduled (same as executeTask line 696-706)
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test-project",
		BackendName: "test",
		SessionID:   sessionID,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "run task", ScheduledExecution: true},
		TaskID:      1,
		ExecutionID: 1,
		TriggerType: "auto",
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Step 4: Call RunWithChannel (same as executeTask line 707)
	runResult := executor.RunWithChannel(ch)

	// Step 5: Verify result — executeTask checks ReceivedTerminal (line 726)
	assert.True(t, runResult.ReceivedTerminal, "expected ReceivedTerminal=true for completed execution")
	assert.Empty(t, runResult.CancelReason, "expected empty CancelReason in scheduled mode")
	assert.NotEmpty(t, runResult.Blocks, "expected at least one block")

	// Step 6: Call Finalize (same as executeTask line 740)
	runResult = executor.Finalize(runResult, nil)

	assert.NotZero(t, runResult.MsgID, "expected non-zero MsgID after Finalize")
	assert.NotNil(t, runResult.Metadata, "expected Metadata after Finalize")
	assert.Equal(t, 10, runResult.Metadata.InputTokens)

	// Verify the streaming message was finalized (streaming=0)
	var streaming int
	err = db.QueryRow(
		"SELECT streaming FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&streaming)
	require.NoError(t, err)
	assert.Equal(t, 0, streaming, "message should be finalized (streaming=0)")
}

func TestExecuteTask_SessionExecutor_ChannelCloseNoTerminal(t *testing.T) {
	// Simulate CLI crash: channel closes without "done"/"error" event.
	// executeTask checks !ReceivedTerminal → marks as failed (line 726-736).
	origDB := DB
	origDBRead := DBRead
	db := setupExecTaskDB(t)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	sessionID, err := CreateSession("/test-project", "test", "Crash Task", "test", "", "default", "scheduled")
	require.NoError(t, err)

	// Create streaming placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test-project", "test", sessionID, "assistant", string(emptyContent), nil, true, "")

	// Channel closes without terminal event (CLI crash)
	events := []ai.StreamEvent{
		{Type: "content", Content: "partial output before crash"},
	}
	ch := make(chan ai.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test-project",
		BackendName: "test",
		SessionID:   sessionID,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "run task", ScheduledExecution: true},
		TaskID:      1,
		ExecutionID: 1,
		TriggerType: "auto",
	}
	executor := NewSessionExecutor(context.Background(), cfg)
	runResult := executor.RunWithChannel(ch)

	// executeTask checks: if !runResult.ReceivedTerminal → mark failed
	assert.False(t, runResult.ReceivedTerminal, "expected ReceivedTerminal=false when channel closes without terminal event")
}

func TestExecuteTask_SessionExecutor_ContextCancelled(t *testing.T) {
	// Simulate context cancellation during execution.
	// executeTask checks ctx.Err() == context.Canceled (line 710-720).
	origDB := DB
	origDBRead := DBRead
	db := setupExecTaskDB(t)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	sessionID, err := CreateSession("/test-project", "test", "Cancel Task", "test", "", "default", "scheduled")
	require.NoError(t, err)

	// Create streaming placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test-project", "test", sessionID, "assistant", string(emptyContent), nil, true, "")

	// Create a channel that blocks (simulates long-running stream)
	events := make(chan ai.StreamEvent, 10)
	events <- ai.StreamEvent{Type: "content", Content: "start"}

	ctx, cancel := context.WithCancel(context.Background())
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test-project",
		BackendName: "test",
		SessionID:   sessionID,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "run task", ScheduledExecution: true},
		TaskID:      1,
		ExecutionID: 1,
		TriggerType: "auto",
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Cancel context after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	runResult := executor.RunWithChannel(events)

	// executeTask checks: if ctx.Err() == context.Canceled → mark cancelled
	assert.Equal(t, context.Canceled, ctx.Err(), "expected context.Canceled")
	assert.False(t, runResult.ReceivedTerminal, "should not have ReceivedTerminal when context cancelled")
}

// --- EmitSessionEvent with toolName ---

func TestEmitSessionEvent_PermissionPendingWithToolName(t *testing.T) {
	origDB := DB
	origDBRead := DBRead
	db := setupChatTestDB(t)
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Insert a session row so GetSessionProjectPath can look it up
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS chat_sessions (id TEXT PRIMARY KEY, project_path TEXT, backend TEXT, title TEXT, external_session_id TEXT DEFAULT '')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		"session-pp-1", "/home/user/project", "codebuddy", "Test Session")
	require.NoError(t, err)

	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-pp", "")
	_ = sub

	// Call with permission_pending and toolName
	EmitSessionEvent("session-pp-1", "permission_pending", true, "WriteTextFile")

	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected at least one buffered event")
	}
	data, ok := buffered[0].Data.(*ws.SessionUpdateData)
	if !ok {
		t.Fatal("expected SessionUpdateData")
	}
	assert.Equal(t, "permission_pending", data.Status)
	assert.Equal(t, "session-pp-1", data.SessionID)
	assert.Equal(t, "WriteTextFile", data.ToolName)
	assert.Equal(t, "/home/user/project", data.ProjectPath)
}

// --- triggerChatSummarization simple mode with WS broadcast ---

func TestTriggerChatSummarization_SimpleMode_BroadcastsWSUpdate(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	origEnabled := chatSummaryEnabled.Load()
	origMode := GetChatSummaryMode()
	defer func() {
		chatSummaryEnabled.Store(origEnabled)
		SetChatSummaryMode(origMode)
	}()

	SetChatSummaryMode("simple")
	chatSummaryEnabled.Store(true)

	// Set up WS manager to capture broadcast
	mgr := ws.NewManagerForTest(nil)
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-summary", "")

	// Insert session + messages
	sessionID := "test-simple-broadcast"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (200, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"text","text":"Here's the answer."}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (201, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	triggerChatSummarization(sessionID)

	// Should have saved the summary
	summary, found := GetSummary("chat_message", 201)
	assert.True(t, found)
	assert.Equal(t, "Here's the answer.", summary)

	// Should have broadcast summary_update via WS
	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected at least one buffered summary_update event")
	}
	assert.Equal(t, "summary_update", buffered[0].Event)
}

// --- triggerChatSummarization AI mode with nil summarizer ---

func TestTriggerChatSummarization_AIMode_NilSummarizer_ReturnsEarly(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	origEnabled := chatSummaryEnabled.Load()
	origMode := GetChatSummaryMode()
	origSummarizer := taskSummarizerInstance
	defer func() {
		chatSummaryEnabled.Store(origEnabled)
		SetChatSummaryMode(origMode)
		taskSummarizerInstance = origSummarizer
	}()

	SetChatSummaryMode("ai")
	chatSummaryEnabled.Store(true)
	taskSummarizerInstance = nil // No summarizer available

	sessionID := "test-ai-nil"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (300, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"text","text":"Answer"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (301, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	// Should not panic with nil summarizer
	triggerChatSummarization(sessionID)

	// No summary should be saved (AI mode, no summarizer)
	_, found := GetSummary("chat_message", 301)
	assert.False(t, found)
}

// --- triggerChatSummarization simple mode with SaveSummary error ---

func TestTriggerChatSummarization_SimpleMode_SaveSummaryError(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	origEnabled := chatSummaryEnabled.Load()
	origMode := GetChatSummaryMode()
	defer func() {
		chatSummaryEnabled.Store(origEnabled)
		SetChatSummaryMode(origMode)
	}()

	SetChatSummaryMode("simple")
	chatSummaryEnabled.Store(true)

	// Drop summaries table to force SaveSummary error
	_, _ = db.Exec("DROP TABLE summaries")

	sessionID := "test-simple-save-error"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (500, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"text","text":"The answer is 42."}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (501, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	// Should not panic, just log warning and return
	triggerChatSummarization(sessionID)
}
