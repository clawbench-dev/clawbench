package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ws"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const schema = `
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
	external_message_id TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	completed_at DATETIME
);
CREATE TABLE IF NOT EXISTS queued_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	queue_id TEXT NOT NULL,
	content TEXT NOT NULL,
	files TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_queued_session ON queued_messages(session_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_queued_identity ON queued_messages(session_id, queue_id);
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
	source_session_id TEXT DEFAULT NULL,
	transport TEXT DEFAULT '',
	auto_approve INTEGER NOT NULL DEFAULT 0,
	context_state TEXT DEFAULT '',
	title_renamed INTEGER NOT NULL DEFAULT 0,
	title_source TEXT NOT NULL DEFAULT '',
	pinned INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_read_at DATETIME,
	UNIQUE(project_path, backend, id)
);
CREATE TABLE IF NOT EXISTS recent_projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT UNIQUE NOT NULL,
	accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	is_default INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS project_meta (
	project_path TEXT PRIMARY KEY,
	next_session_number INTEGER NOT NULL DEFAULT 0,
	forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	name TEXT NOT NULL,
	cron_expr TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	prompt TEXT NOT NULL,
	script TEXT NOT NULL DEFAULT '',
	script_timeout INTEGER NOT NULL DEFAULT 0,
	session_id TEXT DEFAULT '',
	trigger_mode TEXT NOT NULL DEFAULT 'cron',
	event_types TEXT NOT NULL DEFAULT '',
	status TEXT DEFAULT 'active',
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
	status TEXT NOT NULL DEFAULT 'completed',
	read_at DATETIME,
	summary TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON scheduled_tasks(project_path, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at);
CREATE INDEX IF NOT EXISTS idx_history_unread ON chat_history(project_path, role, streaming, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_path, archived, sort_order ASC, created_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS summaries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	target_type TEXT NOT NULL,
	target_id   INTEGER NOT NULL,
	summary     TEXT NOT NULL,
	summary_cards TEXT NOT NULL DEFAULT '',
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(target_type, target_id)
);
CREATE INDEX IF NOT EXISTS idx_sessions_source_session ON chat_sessions(source_session_id) WHERE source_session_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS chat_metadata (
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
	cached_read_tokens INTEGER DEFAULT 0,
	cached_write_tokens INTEGER DEFAULT 0,
	thought_tokens INTEGER DEFAULT 0,
	total_tokens INTEGER DEFAULT 0,
	cache_creation_tokens INTEGER DEFAULT 0,
	cache_hit_tokens INTEGER DEFAULT 0,
	cache_miss_tokens INTEGER DEFAULT 0,
	credit REAL DEFAULT 0,
	usage_by_category TEXT DEFAULT '',
	session_id TEXT DEFAULT '',
	request_id TEXT DEFAULT '',
	trace_id TEXT DEFAULT '',
	agent_message_id TEXT DEFAULT '',
	message_request_id TEXT DEFAULT '',
	request_model_name TEXT DEFAULT '',
	response_model_id TEXT DEFAULT '',
	finish_reason TEXT DEFAULT '',
	outcome TEXT DEFAULT '',
	agent_phase TEXT DEFAULT '',
	project_path TEXT DEFAULT '',
	backend TEXT DEFAULT '',
	agent_id TEXT DEFAULT '',
	clawbench_session_id TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
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
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(tool_id, message_id)
);
CREATE TABLE IF NOT EXISTS chat_thinking (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	think_id TEXT NOT NULL,
	seq INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(think_id, message_id, seq)
);
CREATE TABLE IF NOT EXISTS session_tags (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	scope TEXT NOT NULL DEFAULT 'project',
	project_path TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(name, project_path)
);
CREATE TABLE IF NOT EXISTS session_tag_links (
	session_id TEXT NOT NULL,
	tag_id INTEGER NOT NULL REFERENCES session_tags(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(session_id, tag_id)
);
`

// setupDB creates an in-memory SQLite database with the required schema,
// sets service.DB, and returns a cleanup function.
func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	// Required for :memory: SQLite — every pooled connection is a separate
	// in-memory database. Without pinning to a single connection, any service
	// background goroutine that touches the DB (scheduler, cleanup worker,
	// pending-events queue) can open a second connection backed by an empty
	// database, surfacing as intermittent "no such table: chat_sessions".
	db.SetMaxOpenConns(1)

	_, err = db.Exec(schema)
	assert.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	return db
}

// helperCreateSession creates a session and asserts success, returning the session ID.
func helperCreateSession(t *testing.T, projectPath, backend, title string) string {
	t.Helper()
	id, err := service.CreateSession(projectPath, backend, title, "", "", "default", "chat")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
	t.Cleanup(func() {
		service.SetSessionRunning(id, false)
	})
	return id
}

// insertSessionWithTime inserts a chat session row with an explicit creation
// time and archived flag, used by recent-session listing tests.
func insertSessionWithTime(t *testing.T, projectPath, id, title, createdAt string, archived bool) {
	t.Helper()
	insertSessionWithTypeAndTime(t, projectPath, id, title, "chat", createdAt, archived)
}

// insertSessionWithTypeAndTime inserts a session row with an explicit
// session_type so type-filter tests can create both conversations and task
// executions ('scheduled').
func insertSessionWithTypeAndTime(t *testing.T, projectPath, id, title, sessionType, createdAt string, archived bool) {
	t.Helper()
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	_, err := service.UnsafeDBForTest().Exec("INSERT INTO chat_sessions (id, project_path, backend, title, session_type, archived, created_at, updated_at) VALUES (?, ?, 'claude', ?, ?, ?, ?, ?)",
		id, projectPath, title, sessionType, archivedInt, createdAt, createdAt)
	require.NoError(t, err)
}

// helperCreateScheduledSession creates a scheduled session and asserts success.
func helperCreateScheduledSession(t *testing.T, _, backend, title string) string {
	t.Helper()
	id, err := service.CreateSession("/project", backend, title, "", "", "default", "scheduled")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
	return id
}

// ---------- GetChatHistory / AddChatMessage ----------

func TestAddChatMessageAndGetHistory(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Hello", nil, false, "NewSession")
	assert.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "Hi there", nil, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Hello", msgs[0].Content)
	assert.Equal(t, sid, msgs[0].SessionID)
	assert.Equal(t, "claude", msgs[0].Backend)
	assert.False(t, msgs[0].Streaming)

	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Hi there", msgs[1].Content)
}

func TestGetChatHistory_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty")

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestAddChatMessage_AutoTitle(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	// First user message should auto-title the session
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "This is my question about Go testing", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "This is my question about Go testing", title)
}

// TestAddChatMessage_AutoTitleSkippedWhenUserRenamed verifies that a manual
// rename performed before the first message survives the auto-title step.
func TestAddChatMessage_AutoTitleSkippedWhenUserRenamed(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	// User renames the session before sending anything.
	require.NoError(t, service.SetSessionTitleLocked(sid, "My Custom Name"))
	renamed, err := service.GetSessionTitleRenamed(sid)
	require.NoError(t, err)
	assert.True(t, renamed)

	// First user message must NOT overwrite the custom title.
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "This should not become the title", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "My Custom Name", title)
}

// TestCreateSessionWithLockedTitle_SurvivesFirstMessage verifies that a title
// supplied at creation time is not replaced by the first user message.
func TestCreateSessionWithLockedTitle_SurvivesFirstMessage(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSessionWithLockedTitle("/project", "claude", "Chosen At Creation", "", "", "default", "chat")
	require.NoError(t, err)

	renamed, err := service.GetSessionTitleRenamed(sid)
	require.NoError(t, err)
	assert.True(t, renamed, "locked-title creation must set title_renamed")

	_, err = service.AddChatMessage("/project", "claude", sid, "user", "some first message", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "Chosen At Creation", title)
}

// TestCreateSession_DoesNotLockGeneratedTitle verifies the plain CreateSession
// path still allows first-message auto-titling.
func TestCreateSession_DoesNotLockGeneratedTitle(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "NewSession 1", "", "", "default", "chat")
	require.NoError(t, err)

	renamed, err := service.GetSessionTitleRenamed(sid)
	require.NoError(t, err)
	assert.False(t, renamed)

	_, err = service.AddChatMessage("/project", "claude", sid, "user", "auto title wins", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "auto title wins", title)
}

// TestAddChatMessage_AutoTitleSkippedForScheduledSession verifies that a
// task session keeps its created title (⏰ <task name>) instead of
// being overwritten by the task prompt on the first message.
func TestAddChatMessage_AutoTitleSkippedForScheduledSession(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSessionWithLockedTitle("/project", "claude", "⏰ Daily Code Review", "", "", "default", "scheduled")
	require.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sid, "user", "review the code please", nil, false, "Daily Code Review")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "⏰ Daily Code Review", title)
}

// TestAddChatMessage_AutoTitleStillAppliesForChatSession is a guard against the
// locked-title logic accidentally suppressing normal auto-titling.
func TestAddChatMessage_AutoTitleStillAppliesForChatSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "normal chat title", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "normal chat title", title)

	renamed, err := service.GetSessionTitleRenamed(sid)
	assert.NoError(t, err)
	assert.False(t, renamed, "auto-titled sessions must not be marked as renamed")
}

// TestAutoTitleAppliesToPlaceholderSource verifies the placeholder source path
// (plain CreateSession) leaves the title replaceable, so first-message
// auto-titling still applies.
func TestAutoTitleAppliesToPlaceholderSource(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")
	renamed, err := service.GetSessionTitleRenamed(sid)
	require.NoError(t, err)
	assert.False(t, renamed)

	// Because it is only a placeholder, the first message may auto-title it.
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "overwrites placeholder", nil, false, "NewSession")
	assert.NoError(t, err)
	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "overwrites placeholder", title)
}

func TestAddChatMessage_AutoTitleTruncated(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	longContent := strings.Repeat("啊", 60) // 60 runes, each is a multi-byte character
	_, err := service.AddChatMessage("/project", "claude", sid, "user", longContent, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, 53, utf8.RuneCountInString(title)) // 50 runes + "..."
	runes := []rune(title)
	assert.Equal(t, "啊", string(runes[49]))    // 50th rune is still content
	assert.Equal(t, "...", string(runes[50:])) // followed by ellipsis
}

func TestAddChatMessage_AutoTitleOnlyFirstUserMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "First message", nil, false, "NewSession")
	assert.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sid, "user", "Second message", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "First message", title) // Title unchanged after second message
}

func TestAddChatMessage_AutoTitleEmptyContentWithFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", []model.FileEntry{{Path: "file1.txt"}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "file1.txt", title)
}

func TestAddChatMessage_AutoTitleEmptyContentNoFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "NewSession", title)
}

func TestAddChatMessage_AutoTitleFromFiles_SingleFile(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", []model.FileEntry{{Path: "/src/main.go"}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "main.go", title)
}

func TestAddChatMessage_AutoTitleFromFiles_MultipleFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", []model.FileEntry{{Path: "photo.jpg"}, {Path: "document.pdf"}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "photo.jpg, document.pdf", title)
}

func TestAddChatMessage_AutoTitleFromFiles_LongNamesTruncated(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	longName1 := strings.Repeat("a", 30) + ".txt"
	longName2 := strings.Repeat("b", 30) + ".txt"
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", []model.FileEntry{{Path: longName1}, {Path: longName2}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	// Title should be truncated to 50 runes + "..."
	assert.Equal(t, string([]rune(longName1 + ", " + longName2)[:50])+"...", title)
}

func TestAddChatMessage_AutoTitleFromFiles_WithPathStripsToBasename(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", []model.FileEntry{{Path: "/home/user/project/.clawbench/uploads/image.png"}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "image.png", title)
}

func TestAddChatMessage_AutoTitleFromFiles_TextTakesPrecedence(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "My question", []model.FileEntry{{Path: "/src/main.go"}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "My question", title)
}

func TestExtractPlainText_PlainText(t *testing.T) {
	assert.Equal(t, "hello world", service.ExtractPlainText("hello world"))
}

func TestExtractPlainText_BlockJSON(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"退出 plan","input":null,"done":false}]}`
	assert.Equal(t, "退出 plan", service.ExtractPlainText(content))
}

func TestExtractPlainText_BlockJSONMultipleText(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"hello"},{"type":"text","text":"world"}]}`
	assert.Equal(t, "hello\n\nworld", service.ExtractPlainText(content))
}

func TestExtractPlainText_BlockJSONWithToolUse(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"read the file"},{"type":"tool_use","name":"read","input":{}}]}`
	assert.Equal(t, "read the file", service.ExtractPlainText(content))
}

func TestExtractPlainText_BlockJSONNoText(t *testing.T) {
	content := `{"blocks":[{"type":"tool_use","name":"read","input":{}}]}`
	// Recognized wrapper with no text blocks → empty string (no user-facing text)
	assert.Equal(t, "", service.ExtractPlainText(content))
}

func TestExtractPlainText_InvalidJSON(t *testing.T) {
	content := `{"blocks":invalid}`
	assert.Equal(t, content, service.ExtractPlainText(content))
}

func TestExtractPlainText_NestedACPNotificationJSON(t *testing.T) {
	// Historical dirty data: the whole ACP notification was serialized into the
	// text field of a text block. Must unwrap to the real user text.
	content := `{"blocks":[{"text":"{\"content\":{\"text\":\"hi\",\"type\":\"text\"},\"messageId\":\"85d9b9a9-00a4-4ea1-8abe-0d0ef6bc2426\",\"sessionUpdate\":\"user_message_chunk\"}","type":"text"}]}`
	assert.Equal(t, "hi", service.ExtractPlainText(content))
}

func TestExtractPlainText_NestedACPNotificationChinese(t *testing.T) {
	content := `{"blocks":[{"text":"{\"content\":{\"text\":\"你好\",\"type\":\"text\"},\"messageId\":\"c6d0b19a-5847-46f5-adce-5abb4f950b72\",\"sessionUpdate\":\"user_message_chunk\"}","type":"text"}]}`
	assert.Equal(t, "你好", service.ExtractPlainText(content))
}

func TestExtractPlainText_BareContentArray(t *testing.T) {
	// ACP content array serialized directly as the message content.
	content := `[{"type":"text","text":"hello from array"},{"type":"image","image":{}}]`
	assert.Equal(t, "hello from array", service.ExtractPlainText(content))
}

func TestExtractPlainText_BareContentArraySkipsThinking(t *testing.T) {
	// Non-text array elements (thinking etc.) must not leak into the extracted
	// text — mirrors the frontend behavior.
	content := `[{"type":"thinking","text":"inner reasoning"},{"type":"text","text":"final answer"}]`
	assert.Equal(t, "final answer", service.ExtractPlainText(content))
}

func TestExtractPlainText_DeepNestingCapped(t *testing.T) {
	// Pathological deep nesting must not hang or panic; it degrades gracefully
	// to empty (recognized wrapper whose text exceeds the unwrap depth).
	nested := `"leaf"`
	for range 12 {
		nested = `{"text":` + nested + `}`
	}
	assert.Equal(t, "", service.ExtractPlainText(nested))
}

func TestExtractPlainText_BareContentArrayStrings(t *testing.T) {
	content := `["part one", "part two"]`
	assert.Equal(t, "part one\n\npart two", service.ExtractPlainText(content))
}

func TestExtractPlainText_ACPNotificationDirect(t *testing.T) {
	// Whole ACP notification stored directly as content.
	content := `{"content":{"text":"直接存的通知","type":"text"},"messageId":"abc","sessionUpdate":"user_message_chunk"}`
	assert.Equal(t, "直接存的通知", service.ExtractPlainText(content))
}

func TestExtractPlainText_BlocksWithWhitespacePrefix(t *testing.T) {
	// JSON with leading whitespace/newline should still be unwrapped.
	content := "{\n  \"blocks\": [{\"type\": \"text\", \"text\": \"换行格式\"}]\n}"
	assert.Equal(t, "换行格式", service.ExtractPlainText(content))
}

func TestExtractPlainText_BlocksEmptyTextKeepsOriginal(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"","input":null,"done":false}]}`
	// Recognized wrapper with empty text → empty string (no user-facing text)
	assert.Equal(t, "", service.ExtractPlainText(content))
}

func TestExtractPlainText_PlainTextJSONLikeKeepsOriginal(t *testing.T) {
	// User typed something that starts with { but isn't a known wrapper.
	content := `{"foo": "bar"}`
	assert.Equal(t, content, service.ExtractPlainText(content))
}

func TestExtractPlainText_NonJSONArrayKeepsOriginal(t *testing.T) {
	content := "[PWA] Service Worker skipped"
	assert.Equal(t, content, service.ExtractPlainText(content))
}

func TestExtractPlainText_BlocksWithThinkingOnlyKeepsOriginal(t *testing.T) {
	content := `{"blocks":[{"type":"thinking","text":"inner thought"}]}`
	// Recognized wrapper with no text block → empty string
	assert.Equal(t, "", service.ExtractPlainText(content))
}

func TestAddChatMessage_AutoTitleBlockFormat(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	content := `{"blocks":[{"type":"text","text":"退出 plan","input":null,"done":false}]}`
	_, err := service.AddChatMessage("/project", "claude", sid, "user", content, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "退出 plan", title)
}

func TestAddChatMessage_WithFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "File Test")

	files := []model.FileEntry{{Path: "/path/file"}, {Path: "image.png"}, {Path: "doc.pdf"}}
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Check these", files, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, files, msgs[0].Files)
	assert.Equal(t, "/path/file", msgs[0].Files[0].Path)
}

func TestAddChatMessage_Streaming(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "partial...", nil, true, "")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.True(t, msgs[0].Streaming)
}

func TestAddChatMessage_AssistantDoesNotAutoTitle(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Original Title")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "AI response", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "Original Title", title) // Assistant messages don't change title
}

// ---------- CreateSession ----------

func TestCreateSession_UUIDFormat(t *testing.T) {
	setupDB(t)

	id, err := service.CreateSession("/project", "claude", "Test", "", "", "default", "chat")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)

	// UUID v4 format: 8-4-4-4-12 hex digits separated by dashes
	parts := strings.Split(id, "-")
	assert.Len(t, parts, 5, "UUID should have 5 parts separated by dashes")
	assert.Equal(t, 8, len(parts[0]))
	assert.Equal(t, 4, len(parts[1]))
	assert.Equal(t, 4, len(parts[2]))
	assert.Equal(t, 4, len(parts[3]))
	assert.Equal(t, 12, len(parts[4]))
}

func TestCreateSession_UniqueIDs(t *testing.T) {
	setupDB(t)

	id1, err := service.CreateSession("/project", "claude", "Session 1", "", "", "default", "chat")
	assert.NoError(t, err)
	id2, err := service.CreateSession("/project", "claude", "Session 2", "", "", "default", "chat")
	assert.NoError(t, err)
	assert.NotEqual(t, id1, id2)
}

// ---------- ArchiveSession ----------

func TestArchiveSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "msg", nil, false, "NewSession")
	assert.NoError(t, err)

	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Session should be invisible via user-facing APIs
	_, err = service.GetSessionTitle(sid)
	assert.Error(t, err) // archived sessions filtered by archived=0

	// Messages are still physically present (no message-level archival,
	// session-level archival controls visibility)
	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1) // messages still exist in DB

	// But the session is archived
	var archived int
	err = service.UnsafeDBForTest().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sid).Scan(&archived)
	assert.NoError(t, err)
	assert.Equal(t, 1, archived)

	// updated_at should have been set to the deletion timestamp
	var updatedAt string
	err = service.UnsafeDBForTest().QueryRow("SELECT updated_at FROM chat_sessions WHERE id = ?", sid).Scan(&updatedAt)
	assert.NoError(t, err)
	assert.NotEmpty(t, updatedAt)
}

func TestArchiveSession_RejectsNewMessages(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")

	err := service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Adding messages to a archived session should fail
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "after delete", nil, false, "NewSession")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "archived session")
}

func TestArchiveSession_GetSessionBackendHidden(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "codebuddy", "Backend Test")

	err := service.ArchiveSession("/project", "codebuddy", sid)
	assert.NoError(t, err)

	// GetSessionBackend should return empty for archived sessions
	backend := service.GetSessionBackend(sid)
	assert.Equal(t, "", backend)
}

func TestArchiveSession_GetSessionAgentIDHidden(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Agent Test", "my-agent", "gpt-4", "user", "chat")
	assert.NoError(t, err)

	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// GetSessionAgentID should return empty for archived sessions
	assert.Equal(t, "", service.GetSessionAgentID(sid))
}

func TestArchiveSession_DoesNotAffectOtherSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	_, _ = service.AddChatMessage("/project", "claude", sid1, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid2, "user", "msg2", nil, false, "NewSession")

	err := service.ArchiveSession("/project", "claude", sid1)
	assert.NoError(t, err)

	// sid2 should still be fully functional
	title, err := service.GetSessionTitle(sid2)
	assert.NoError(t, err)
	assert.Equal(t, "msg2", title) // auto-titled from first message

	msgs, err := service.GetChatHistory("/project", "claude", sid2)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestArchiveSession_SessionCountExcludesDeleted(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")

	countBefore, err := service.GetSessionCount("/project")
	assert.NoError(t, err)
	assert.Equal(t, 1, countBefore)

	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	countAfter, err := service.GetSessionCount("/project")
	assert.NoError(t, err)
	assert.Equal(t, 0, countAfter)
}

func TestArchiveSession_GetMessagesBySessionIDStillReturnsData(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "RAG Test")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "NewSession")

	err := service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// RAG API (GetMessagesBySessionID) should still return archived messages
	msgs, err := service.GetMessagesBySessionID(sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "hello", msgs[0].Content)
}

func TestArchiveSession_GetMessageByIDStillReturnsData(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "RAG Test")
	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "NewSession")
	assert.NoError(t, err)

	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// RAG API (GetMessageByID) should still return archived messages
	msg, err := service.GetMessageByID(msgID)
	assert.NoError(t, err)
	assert.Equal(t, "hello", msg.Content)
}

func TestArchiveSession_DeletedSessionNotInGetSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Active")
	archivedSID := helperCreateSession(t, "/project", "claude", "To Delete")

	err := service.ArchiveSession("/project", "claude", archivedSID)
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.NotEqual(t, archivedSID, sessions[0].ID)
}

// ---------- NextSessionNumber ----------

func TestNextSessionNumber_BasedOnMaxUnnamed(t *testing.T) {
	setupDB(t)

	// No numbered unnamed sessions → first is 1.
	n1, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 1, n1)

	// Create that unnamed session ("New Session 1") → next is 2.
	helperCreateSession(t, "/proj", "claude", "New Session 1")
	n2, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 2, n2)

	// A different project is independent.
	m1, err := service.NextSessionNumber("/other", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 1, m1)

	// Numbering is unified across backends — adding a claude session bumps it.
	helperCreateSession(t, "/proj", "claude", "New Session 2")
	n3, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 3, n3)
}

func TestNextSessionNumber_TakesMaxNotCount(t *testing.T) {
	setupDB(t)

	// Gap in numbers: 1 and 3 exist → next must be 4 (max), not 3 (count).
	helperCreateSession(t, "/proj", "claude", "New Session 1")
	helperCreateSession(t, "/proj", "claude", "New Session 3")
	n, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 4, n)
}

func TestNextSessionNumber_IgnoresNamedSessions(t *testing.T) {
	setupDB(t)

	// Explicitly-named sessions don't count toward the numbering.
	helperCreateSession(t, "/proj", "claude", "My Project")
	helperCreateSession(t, "/proj", "claude", "Another")

	n1, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 1, n1)
}

func TestNextSessionNumber_ResetsAfterAllUnnamedArchived(t *testing.T) {
	setupDB(t)

	// A single unnamed session is number 1.
	n1, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 1, n1)
	sid := helperCreateSession(t, "/proj", "claude", "New Session 1")

	// Archive the only numbered unnamed session — the max drops to 0.
	err = service.ArchiveSession("/proj", "claude", sid)
	assert.NoError(t, err)

	// No numbered unnamed session remains, so numbering resets to 1.
	n2, err := service.NextSessionNumber("/proj", "New Session")
	assert.NoError(t, err)
	assert.Equal(t, 1, n2)
}

// ---------- GetSessions ----------

func TestGetSessions_FiltersByProjectAndBackend(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/proj1", "claude", "C1")
	helperCreateSession(t, "/proj1", "codebuddy", "CB1")
	helperCreateSession(t, "/proj2", "claude", "C2")

	sessions, err := service.GetSessions("/proj1", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "claude", sessions[0].Backend)

	sessions, err = service.GetSessions("/proj1", "codebuddy")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "codebuddy", sessions[0].Backend)

	sessions, err = service.GetSessions("/proj2", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
}

func TestGetSessions_AllBackends(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/proj", "claude", "C1")
	helperCreateSession(t, "/proj", "codebuddy", "CB1")
	helperCreateSession(t, "/other", "claude", "C2")

	sessions, err := service.GetSessions("/proj", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)

	// Should NOT include /other
	for _, s := range sessions {
		// Can't directly check project_path since it's not in ChatSession,
		// but we know we created 2 sessions for /proj
		assert.Contains(t, []string{"claude", "codebuddy"}, s.Backend)
	}
}

// ---------- GetSessionBackend ----------

func TestGetSessionBackend(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "codebuddy", "Test")
	backend := service.GetSessionBackend(sid)
	assert.Equal(t, "codebuddy", backend)
}

func TestGetSessionBackend_NonExistent(t *testing.T) {
	setupDB(t)

	backend := service.GetSessionBackend("non-existent-id")
	assert.Equal(t, "", backend)
}

// ---------- Session Running ----------

func TestIsSessionRunning_DefaultFalse(t *testing.T) {
	setupDB(t)

	assert.False(t, service.IsSessionRunning("any-id"))
}

func TestSetSessionRunning(t *testing.T) {
	setupDB(t)

	service.SetSessionRunning("sess-1", true)
	assert.True(t, service.IsSessionRunning("sess-1"))

	service.SetSessionRunning("sess-1", false)
	assert.False(t, service.IsSessionRunning("sess-1"))
}

func TestTrySetSessionRunning(t *testing.T) {
	setupDB(t)

	// First try should succeed
	ok := service.TrySetSessionRunning("sess-2")
	assert.True(t, ok)
	assert.True(t, service.IsSessionRunning("sess-2"))

	// Second try should fail (already running)
	ok = service.TrySetSessionRunning("sess-2")
	assert.False(t, ok)

	// Clean up
	service.SetSessionRunning("sess-2", false)
}

// ---------- SessionHasAssistant ----------

func TestSessionHasAssistant(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// No assistant messages yet
	assert.False(t, service.SessionHasAssistant(sid))

	// Add a streaming assistant message - should NOT count
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "partial", nil, true, "")
	assert.NoError(t, err)
	assert.False(t, service.SessionHasAssistant(sid))

	// Add a finalized assistant message - should count
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "final", nil, false, "NewSession")
	assert.NoError(t, err)
	assert.True(t, service.SessionHasAssistant(sid))
}

// ---------- UpdateStreamingMessage / FinalizeStreamingMessage ----------

func TestUpdateStreamingMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "initial", nil, true, "")
	assert.NoError(t, err)

	err = service.UpdateStreamingMessage("/project", "claude", sid, "updated content")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "updated content", msgs[0].Content)
	assert.True(t, msgs[0].Streaming) // Still streaming
}

func TestFinalizeStreamingMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	_, err = service.FinalizeStreamingMessage("/project", "claude", sid, "final content")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "final content", msgs[0].Content)
	assert.False(t, msgs[0].Streaming) // No longer streaming
}

func TestUpdateStreamingMessage_NoStreamingRow(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "NoStream")

	// No streaming message exists; update should succeed but affect 0 rows
	err := service.UpdateStreamingMessage("/project", "claude", sid, "content")
	assert.NoError(t, err)
}

// ---------- CancelSession ----------

func TestCancelSession_NoCancelFunc(t *testing.T) {
	setupDB(t)

	ok := service.CancelSession("non-existent-session")
	// Non-running session with no cancel func is considered already cancelled (idempotent)
	assert.True(t, ok)
}

func TestCancelSession_WithCancelFunc(t *testing.T) {
	setupDB(t)

	sid := "cancel-test-session"
	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())

	service.RegisterSessionCancel(sid, cancel)
	service.SetSessionRunning(sid, true)

	// Cancel the session
	ok := service.CancelSession(sid)
	assert.True(t, ok)

	// Context should be cancelled
	<-ctx.Done()
	cancelled = true
	assert.True(t, cancelled)

	// Session should no longer be running
	assert.False(t, service.IsSessionRunning(sid))

	// Second cancel should return true (idempotent: session is no longer running)
	ok = service.CancelSession(sid)
	assert.True(t, ok)
}

// ---------- GetSessionTitle ----------

func TestGetSessionTitle(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "My Title")

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "My Title", title)
}

func TestGetSessionTitle_NonExistent(t *testing.T) {
	setupDB(t)

	_, err := service.GetSessionTitle("non-existent")
	assert.Error(t, err)
}

// ---------- Edge cases ----------

func TestAddChatMessage_MultipleSessionsIsolated(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	_, _ = service.AddChatMessage("/project", "claude", sid1, "user", "Msg in session 1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid2, "user", "Msg in session 2", nil, false, "NewSession")

	msgs1, err := service.GetChatHistory("/project", "claude", sid1)
	assert.NoError(t, err)
	assert.Len(t, msgs1, 1)
	assert.Equal(t, "Msg in session 1", msgs1[0].Content)

	msgs2, err := service.GetChatHistory("/project", "claude", sid2)
	assert.NoError(t, err)
	assert.Len(t, msgs2, 1)
	assert.Equal(t, "Msg in session 2", msgs2[0].Content)
}

func TestAddChatMessage_AutoTitleExactly50Runes(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New")

	// Exactly 50 runes - should NOT be truncated
	content := strings.Repeat("x", 50)
	_, err := service.AddChatMessage("/project", "claude", sid, "user", content, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, content, title) // No "..." appended
}

func TestAddChatMessage_AutoTitle51Runes(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New")

	// 51 runes - should be truncated to 50 + "..."
	content := strings.Repeat("x", 51)
	_, err := service.AddChatMessage("/project", "claude", sid, "user", content, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, strings.Repeat("x", 50)+"...", title)
}

func TestAddChatMessage_WithFilePath(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "FP Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "look at this", []model.FileEntry{{Path: "/src/main.go"}}, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, []model.FileEntry{{Path: "/src/main.go"}}, msgs[0].Files)
}

func TestGetSessions_OrderedByCreatedDesc(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "First")
	sid2 := helperCreateSession(t, "/project", "claude", "Second")

	// Set explicit created_at timestamps to guarantee ordering (SQLite time precision is seconds)
	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now', '-60 seconds') WHERE id = ?", sid1)
	assert.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now') WHERE id = ?", sid2)
	assert.NoError(t, err)

	// A later interaction must NOT reorder the list: update sid1's updated_at to be newest,
	// yet the list order must remain by created_at.
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '+60 seconds') WHERE id = ?", sid1)
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)

	// sid2 should be first since it was created most recently, regardless of interaction
	assert.Equal(t, sid2, sessions[0].ID)
	assert.Equal(t, sid1, sessions[1].ID)
}

func TestArchiveSession_NonExistentDoesNotError(t *testing.T) {
	setupDB(t)

	// Deleting a non-existent session should not return an error
	// (DELETE on non-existent rows is a no-op)
	err := service.ArchiveSession("/project", "claude", "non-existent-id")
	assert.NoError(t, err)
}

func TestRegisterUnregisterSessionCancel(t *testing.T) {
	setupDB(t)

	sid := "cancel-reg-test"
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	service.RegisterSessionCancel(sid, cancel)

	// Should be able to cancel
	ok := service.CancelSession(sid)
	assert.True(t, ok)

	// After cancel, register a new one
	_, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	service.RegisterSessionCancel(sid, cancel2)
	service.UnregisterSessionCancel(sid)

	// After unregister, cancel returns true (session not running → idempotent)
	ok = service.CancelSession(sid)
	assert.True(t, ok)
}

func TestFinalizeStreamingMessage_NoStreamingRow(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "NoStream")

	// No streaming message; finalize should succeed but affect 0 rows
	_, err := service.FinalizeStreamingMessage("/project", "claude", sid, "content")
	assert.NoError(t, err)
}

func TestAddChatMessage_AutoTitleWithFilePathAndContent(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New")

	// When content is non-empty, title comes from content (not files)
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Hello world", []model.FileEntry{{Path: "/some/file.go"}, {Path: "file.go"}}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "Hello world", title)
}

func TestGetChatHistory_DifferentBackendsIsolated(t *testing.T) {
	setupDB(t)

	sidC := helperCreateSession(t, "/project", "claude", "Claude")
	sidCB := helperCreateSession(t, "/project", "codebuddy", "CodeBuddy")

	_, _ = service.AddChatMessage("/project", "claude", sidC, "user", "claude msg", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "codebuddy", sidCB, "user", "codebuddy msg", nil, false, "NewSession")

	msgsC, err := service.GetChatHistory("/project", "claude", sidC)
	assert.NoError(t, err)
	assert.Len(t, msgsC, 1)
	assert.Equal(t, "claude msg", msgsC[0].Content)

	msgsCB, err := service.GetChatHistory("/project", "codebuddy", sidCB)
	assert.NoError(t, err)
	assert.Len(t, msgsCB, 1)
	assert.Equal(t, "codebuddy msg", msgsCB[0].Content)
}

// TestGetChatHistory_SameSecondOrdering verifies that messages inserted in the
// same second (identical created_at due to CURRENT_TIMESTAMP second precision)
// are returned in insertion order (id ASC) rather than non-deterministic order.
// This is the root cause of the bug where assistant output appeared above user input.
func TestGetChatHistory_SameSecondOrdering(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Same Second")

	// Insert user then assistant within the same second — no sleep
	userID, err := service.AddChatMessage("/project", "claude", sid, "user", "我自己定 AI-Driven Engineering", nil, false, "")
	assert.NoError(t, err)

	assistantID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "很好！AI-Driven Engineering — 精准", nil, false, "")
	assert.NoError(t, err)

	// assistantID > userID since AUTOINCREMENT guarantees this
	assert.Greater(t, assistantID, userID, "assistant message should have higher ID than user message")

	// Verify chronological order: user first, assistant second
	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role, "user message must come first")
	assert.Equal(t, "assistant", msgs[1].Role, "assistant message must come second")
}

func TestGetMessageIDBeforeTime(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "BeforeTime")

	// Insert messages with known timestamps
	service.UnsafeDBForTest().Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"/project", "claude", sid, "user", "msg1", "2025-01-01 10:00:00")
	service.UnsafeDBForTest().Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"/project", "claude", sid, "assistant", "msg2", "2025-01-01 10:00:01")
	service.UnsafeDBForTest().Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"/project", "claude", sid, "user", "msg3", "2025-01-01 10:00:02")

	// Query for messages before 10:00:02 — should return max ID of messages before that time
	id, err := service.GetMessageIDBeforeTime("/project", "claude", sid, "2025-01-01 10:00:02")
	assert.NoError(t, err)
	assert.Greater(t, id, 0, "should find a message ID before the given time")

	// Query with a time before all messages — should return 0
	id, err = service.GetMessageIDBeforeTime("/project", "claude", sid, "2025-01-01 09:00:00")
	assert.NoError(t, err)
	assert.Equal(t, 0, id, "should return 0 when no messages before the given time")
}

// Ensure TestMain-like global DB save/restore works correctly
func TestGlobalDBPreservedAcrossParallelTests(t *testing.T) {
	originalDB := service.UnsafeDBForTest()
	setupDB(t)
	// Within this test, service.UnsafeDBForTest() is our in-memory DB
	assert.NotNil(t, service.UnsafeDBForTest())
	assert.NotEqual(t, originalDB, service.UnsafeDBForTest()) // if originalDB was nil or different
}

func TestAddChatMessage_StreamingFalse(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "No Stream")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "final response", nil, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.False(t, msgs[0].Streaming)
}

func TestUpdateThenFinalizeStreaming(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Full Stream")

	// Start streaming
	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "start", nil, true, "")

	// Update content multiple times
	service.UpdateStreamingMessage("/project", "claude", sid, "start + more")
	service.UpdateStreamingMessage("/project", "claude", sid, "start + more + final")

	// Verify still streaming
	msgs, _ := service.GetChatHistory("/project", "claude", sid)
	assert.True(t, msgs[0].Streaming)
	assert.Equal(t, "start + more + final", msgs[0].Content)

	// Finalize
	service.FinalizeStreamingMessage("/project", "claude", sid, "complete response")

	msgs, _ = service.GetChatHistory("/project", "claude", sid)
	assert.False(t, msgs[0].Streaming)
	assert.Equal(t, "complete response", msgs[0].Content)
}

func TestCancelSession_CleansUpRunningState(t *testing.T) {
	setupDB(t)

	sid := fmt.Sprintf("cleanup-test-%d", len("x"))
	_, cancel := context.WithCancel(context.Background())

	service.RegisterSessionCancel(sid, cancel)
	service.SetSessionRunning(sid, true)
	assert.True(t, service.IsSessionRunning(sid))

	service.CancelSession(sid)
	assert.False(t, service.IsSessionRunning(sid))
}

// ---------- GetChatMessageCount ----------

func TestGetChatMessageCount(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")
	// Initially 0
	count, err := service.GetChatMessageCount(sid)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	// Add messages
	service.AddChatMessage("/project", "claude", sid, "user", "Hello", nil, false, "NewSession")
	service.AddChatMessage("/project", "claude", sid, "assistant", "Hi", nil, false, "NewSession")
	count, err = service.GetChatMessageCount(sid)
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetChatMessageCount_NonExistent(t *testing.T) {
	setupDB(t)
	count, err := service.GetChatMessageCount("non-existent")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetChatMessageCount_DBError(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")
	service.AddChatMessage("/project", "claude", sid, "user", "Hello", nil, false, "NewSession")

	origDB := service.UnsafeDBForTest()
	closedDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	closedDB.Close()
	cleanup := service.SetDBForTest(origDB, closedDB)

	count, err := service.GetChatMessageCount(sid)
	assert.Error(t, err, "count on a closed DB must surface the error, not silently return 0")
	assert.Equal(t, 0, count)

	cleanup()
	assert.Same(t, origDB, service.UnsafeDBForTest(), "DB handles must be restored after the test")
}

func TestGetFinalizedMessageCount_DBError(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")
	service.AddChatMessage("/project", "claude", sid, "user", "Hello", nil, false, "NewSession")

	closedDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	closedDB.Close()
	cleanup := service.SetDBForTest(service.UnsafeDBForTest(), closedDB)
	defer cleanup()

	count, err := service.GetFinalizedMessageCount(sid)
	assert.Error(t, err, "finalized count on a closed DB must surface the error, not silently return 0")
	assert.Equal(t, 0, count)
}

// ---------- UpdateLastRead ----------

func TestUpdateLastRead(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")
	// UpdateLastRead is now synchronous. Test the SQL directly to verify the UPDATE works.
	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET last_read_at = CURRENT_TIMESTAMP WHERE id = ?", sid)
	assert.NoError(t, err)
	var lastRead sql.NullTime
	err = service.UnsafeDBForTest().QueryRow("SELECT last_read_at FROM chat_sessions WHERE id = ?", sid).Scan(&lastRead)
	assert.NoError(t, err)
	assert.True(t, lastRead.Valid)
}

func TestUpdateLastRead_BroadcastsReadEvent(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-read", "")

	service.UpdateLastRead(sid)

	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Fatal("expected a session_update broadcast after UpdateLastRead")
	}
	assert.Equal(t, "session_update", buffered[0].Event)
	data, ok := buffered[0].Data.(*ws.SessionUpdateData)
	require.True(t, ok, "expected SessionUpdateData")
	assert.Equal(t, "read", data.Status)
	assert.Equal(t, sid, data.SessionID)
	assert.False(t, data.HasNewMessages)
}

func TestUpdateLastRead_AnchorsToNewestAssistantMessage(t *testing.T) {
	// Regression: the unread query compares h.created_at > s2.last_read_at with
	// second-precision SQLite DATETIME. If a message finalized in the same second
	// as the mark-read call, a plain CURRENT_TIMESTAMP would still leave it
	// "unread". UpdateLastRead must anchor last_read_at to at least the newest
	// finalized assistant message's created_at so the comparison is robust.
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Anchor")

	// Insert an assistant message with a fixed created_at so the comparison is
	// deterministic regardless of second boundaries.
	const msgCreated = "2025-01-01 10:00:00"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, ?, ?, 'assistant', 'final', 0, ?)",
		"/project", "claude", sid, msgCreated,
	)
	assert.NoError(t, err)

	service.UpdateLastRead(sid)

	// The driver reads DATETIME as ISO 8601 UTC — compare semantically via time.
	var lastRead sql.NullTime
	err = service.UnsafeDBForTest().QueryRow("SELECT last_read_at FROM chat_sessions WHERE id = ?", sid).Scan(&lastRead)
	assert.NoError(t, err)
	require.True(t, lastRead.Valid)
	msgTime, err := time.Parse("2006-01-02 15:04:05", msgCreated)
	assert.NoError(t, err)
	assert.False(t, lastRead.Time.Before(msgTime), "last_read_at must anchor to the newest finalized assistant message created_at")

	// After marking read, the session must no longer be unread.
	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount)
}

func TestUpdateLastRead_FallsBackToNowWithoutMessages(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "No Messages")

	service.UpdateLastRead(sid)

	var lastRead sql.NullString
	err := service.UnsafeDBForTest().QueryRow("SELECT last_read_at FROM chat_sessions WHERE id = ?", sid).Scan(&lastRead)
	assert.NoError(t, err)
	assert.True(t, lastRead.Valid, "last_read_at should still be set via CURRENT_TIMESTAMP fallback")
}

func TestUpdateLastRead_DoesNotAnchorBackwardsDuringStreamingTurn(t *testing.T) {
	// Regression: when the user cancels the turn they are viewing, the frontend
	// marks the session read on the "cancelled" session_update event — which is
	// emitted BEFORE the executor finalizes the interrupted reply (streaming=1
	// -> 0). Anchoring last_read_at to the newest *finalized* assistant message
	// therefore anchors DOWN to the previous turn's reply, and the interrupted
	// reply (created_at is newer) flips the session back to unread once it is
	// finalized. The user is looking right at the session, so it must stay read.
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Cancel Turn")

	// Previous turn: a finalized assistant reply from an earlier time.
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', 'old reply', 0, '2025-01-01 10:00:00')",
		"/project", sid,
	)
	require.NoError(t, err)

	// Current turn: user cancelled while the reply row is still streaming=1 —
	// exactly the state mark-read sees before FinalizeStreamingMessage runs.
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', 'partial reply', 1, '2025-01-01 10:05:00')",
		"/project", sid,
	)
	require.NoError(t, err)

	service.UpdateLastRead(sid)

	// The executor finalizes the interrupted reply immediately after.
	_, err = service.UnsafeDBForTest().Exec(
		"UPDATE chat_history SET streaming = 0 WHERE session_id = ? AND streaming = 1", sid,
	)
	require.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount,
		"the interrupted reply the user was watching must not become unread")
}

// TestUnread_ReplyReadMidTurnStillBecomesUnread is the core regression for the
// "completion popup fires but no unread badge" bug.
//
// The streaming placeholder row is inserted when the turn STARTS, so its
// created_at is the turn start, not the moment the reply landed. Reading the
// session while the turn is still running therefore pushes last_read_at past
// created_at, and with created_at semantics the finished reply could never
// register as unread again. completed_at timestamps the actual landing, so the
// reply still surfaces.
//
// The test models the real time gap explicitly: the read happens mid-turn and
// the reply lands later. (UpdateLastRead anchors to CURRENT_TIMESTAMP, and
// FinalizeStreamingMessage also stamps CURRENT_TIMESTAMP, so driving both back
// to back would land them in the same second and mask the behavior under test.)
func TestUnread_ReplyReadMidTurnStillBecomesUnread(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Mid-turn Read")

	// Previous turn, finalized long ago.
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at, completed_at) VALUES (?, 'claude', ?, 'assistant', 'old reply', 0, '2025-01-01 09:00:00', '2025-01-01 09:00:00')",
		"/project", sid)
	require.NoError(t, err)

	// Current turn starts at 10:00:00 — the streaming placeholder is created
	// then, so created_at is the TURN START (the bug's premise).
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', '', 1, '2025-01-01 10:00:00')",
		"/project", sid)
	require.NoError(t, err)

	// The user opens the session mid-turn at 10:00:30. The frontend always marks
	// the current session read on open, so last_read_at becomes that instant.
	// UpdateLastRead itself is exercised, then rewound to the mid-turn moment to
	// represent the minutes the turn still had left to run.
	service.UpdateLastRead(sid)
	_, err = service.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET last_read_at = '2025-01-01 10:00:30' WHERE id = ?", sid)
	require.NoError(t, err)

	// Precondition: while the reply is still streaming it is not counted.
	sessions, err := service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, 0, sessions[0].UnreadCount, "a still-streaming reply must not count as unread")

	// The turn finishes minutes later and the reply is finalized for real.
	msgID, err := service.FinalizeStreamingMessage("/project", "claude", sid, `{"blocks":[]}`)
	require.NoError(t, err)
	require.NotZero(t, msgID, "the streaming row must have been finalized")

	sessions, err = service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount,
		"a reply that landed after the user last read it must be unread — otherwise the completion popup shows with no badge")
}

// TestGetLiveRunState_ResolvesQuestionByIdOrder guards the subscribe-time
// recovery lookup. A client that subscribes mid-flight is handed the question
// row plus the stream_start that anchors the reply under it; the question is
// found by ID ORDER (greatest user id below the streaming row), because in this
// model a queued message is materialized immediately before its reply.
//
// Regression: the previous implementation resolved it through the answered
// queue id, which also failed for rows whose queue_id was empty.
func TestGetLiveRunState_ResolvesQuestionByIdOrder(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Live Run State")

	// A question, then the streaming reply it is waiting on.
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES (?, 'claude', ?, 'user', 'what is 2+2?', 0)",
		"/project", sid)
	require.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES (?, 'claude', ?, 'assistant', '', 1)",
		"/project", sid)
	require.NoError(t, err)

	msgID, qID, qContent := service.GetLiveRunState(sid)
	require.NotZero(t, msgID, "the streaming row must be reported")
	require.NotZero(t, qID, "the question must be found by id order")
	assert.Equal(t, "what is 2+2?", qContent)
	assert.Less(t, qID, msgID, "the question must precede the reply it anchors")

	// An idle session reports nothing: emitting a stream_start for it would open
	// a phantom placeholder on the client.
	_, err = service.UnsafeDBForTest().Exec(
		"UPDATE chat_history SET streaming = 0 WHERE session_id = ?", sid)
	require.NoError(t, err)
	idleMsgID, idleQID, _ := service.GetLiveRunState(sid)
	assert.Zero(t, idleMsgID, "nothing streaming → no live run state")
	assert.Zero(t, idleQID)
}

// TestUnread_MarkReadAfterCompletionClearsBadge is the counterpart: once the
// user is actually looking at the session when it completes, marking read must
// clear the badge. This is what keeps the fix from turning every reply into a
// permanent unread.
func TestUnread_MarkReadAfterCompletionClearsBadge(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Read After Completion")

	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', '', 1, '2025-01-01 10:00:00')",
		"/project", sid)
	require.NoError(t, err)

	// The reply lands while the user is viewing the session (the completion
	// event path then calls mark-read, as it does in the browser).
	_, err = service.FinalizeStreamingMessage("/project", "claude", sid, `{"blocks":[]}`)
	require.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount, "precondition: the fresh reply is unread")

	service.UpdateLastRead(sid)

	sessions, err = service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount,
		"marking read after the reply landed must clear the badge")
}

// TestUnread_LegacyRowWithoutCompletedAtFallsBackToCreatedAt covers rows written
// before the completed_at column existed. They must keep behaving exactly as
// before (created_at semantics), which the COALESCE fallback provides.
func TestUnread_LegacyRowWithoutCompletedAtFallsBackToCreatedAt(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Legacy Row")

	// Legacy finalized row: completed_at is NULL.
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', 'legacy reply', 0, '2025-01-01 10:00:00')",
		"/project", sid)
	require.NoError(t, err)

	// Never read → unread (last_read_at NULL).
	sessions, err := service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount, "an unread legacy row must still count as unread")

	// Read after it landed → not unread.
	service.UpdateLastRead(sid)
	sessions, err = service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount, "a read legacy row must not count as unread")
}

// TestFinalizeStreamingMessage_StampsCompletedAt pins the invariant the whole
// fix rests on: finalizing is what records when the reply landed, and that
// timestamp must not be the turn-start created_at.
func TestFinalizeStreamingMessage_StampsCompletedAt(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Completed At")

	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', '', 1, '2025-01-01 10:00:00')",
		"/project", sid)
	require.NoError(t, err)

	_, err = service.FinalizeStreamingMessage("/project", "claude", sid, `{"blocks":[]}`)
	require.NoError(t, err)

	var created, completed sql.NullTime
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT created_at, completed_at FROM chat_history WHERE session_id = ? AND role = 'assistant'",
		sid).Scan(&created, &completed)
	require.NoError(t, err)
	require.True(t, completed.Valid, "finalize must stamp completed_at")
	require.True(t, created.Valid)
	assert.True(t, completed.Time.After(created.Time),
		"completed_at (%s) must be later than the turn-start created_at (%s)", completed.Time, created.Time)
}

// TestUnread_CancelledTurnTheUserWasWatchingStaysRead guards e76a6d960 against
// the completed_at change.
//
// A user cancel happens IN the session the user is looking at, and the frontend
// marks it read as soon as the "cancelled" event arrives — which is emitted
// before the executor finalizes the interrupted reply (the agent process has to
// tear down first). If that finalize stamped completed_at, the reply's
// timestamp would land after the read and the session would flip back to
// unread even though the user is staring at it. The cancelled finalize must
// therefore leave completed_at NULL so the unread query falls back to
// created_at (the turn start, which precedes the cancel-time read).
func TestUnread_CancelledTurnTheUserWasWatchingStaysRead(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Cancelled Watching")

	// The interrupted reply, created at turn start (10:00:00).
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', 'partial', 1, '2025-01-01 10:00:00')",
		"/project", sid)
	require.NoError(t, err)

	// The user cancels at 10:00:05 while watching; the frontend marks read then.
	_, err = service.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET last_read_at = '2025-01-01 10:00:05' WHERE id = ?", sid)
	require.NoError(t, err)

	// The executor finalizes the interrupted reply after the agent tears down.
	_, err = service.FinalizeCancelledStreamingMessage("/project", "claude", sid, `{"blocks":[]}`)
	require.NoError(t, err)

	var completed sql.NullTime
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT completed_at FROM chat_history WHERE session_id = ? AND role = 'assistant'", sid).Scan(&completed)
	require.NoError(t, err)
	assert.False(t, completed.Valid, "a user-cancelled finalize must not stamp completed_at")

	sessions, err := service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount,
		"cancelling a session the user is watching must not make it unread again")
}

// TestUnread_CompletedTurnWhileAwayIsUnread is the mirror case that makes the
// completed_at distinction necessary: a turn that completes while the user has
// switched to another session must surface as unread, even though its
// created_at (turn start) predates the last read.
func TestUnread_CompletedTurnWhileAwayIsUnread(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Completed While Away")

	// Previous reply, finalized and read.
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at, completed_at) VALUES (?, 'claude', ?, 'assistant', 'old', 0, '2025-01-01 09:00:00', '2025-01-01 09:00:00')",
		"/project", sid)
	require.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET last_read_at = '2025-01-01 09:30:00' WHERE id = ?", sid)
	require.NoError(t, err)

	// A new turn starts at 10:00:00 and the user stays on it briefly, then
	// switches away. The turn keeps running and only lands at 10:05:00.
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', '', 1, '2025-01-01 10:00:00')",
		"/project", sid)
	require.NoError(t, err)

	_, err = service.FinalizeStreamingMessage("/project", "claude", sid, `{"blocks":[]}`)
	require.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount,
		"a reply that landed after the user left must be unread — this is the badge the completion popup announces")
}

// ---------- GetSessionAgentID ----------

func TestGetSessionAgentID(t *testing.T) {
	setupDB(t)
	// Create session with agent ID
	sid, err := service.CreateSession("/project", "claude", "Test", "my-agent", "gpt-4", "user", "chat")
	assert.NoError(t, err)
	assert.Equal(t, "my-agent", service.GetSessionAgentID(sid))
}

func TestGetSessionAgentID_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetSessionAgentID("non-existent"))
}

func TestGetSessionAgentID_EmptyAgent(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "No Agent")
	assert.Equal(t, "", service.GetSessionAgentID(sid))
}

// ---------- GetAndClearCancelReason ----------

func TestGetAndClearCancelReason_NoReason(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetAndClearCancelReason("non-existent"))
}

func TestGetAndClearCancelReason_WithReason(t *testing.T) {
	setupDB(t)
	sid := "test-reason-session"
	// Simulate setting cancel reason (as CancelSession does)
	service.RegisterSessionCancel(sid, func() {})
	service.SetSessionRunning(sid, true)
	// Cancel sets "user" reason
	service.CancelSession(sid)
	// Should return "user"
	assert.Equal(t, "user", service.GetAndClearCancelReason(sid))
	// Second call should return "" (cleared)
	assert.Equal(t, "", service.GetAndClearCancelReason(sid))
}

// ---------- ForceCancelSession ----------

func TestForceCancelSession(t *testing.T) {
	setupDB(t)
	sid := "force-cancel-test"
	ctx, cancel := context.WithCancel(context.Background())
	service.RegisterSessionCancel(sid, cancel)
	service.SetSessionRunning(sid, true)

	service.ForceCancelSession(sid)

	// Context should be cancelled
	<-ctx.Done()
	// Reason should be "disconnect"
	assert.Equal(t, "disconnect", service.GetAndClearCancelReason(sid))
}

func TestForceCancelSession_NoCancelFunc(t *testing.T) {
	setupDB(t)
	// Should not panic when no cancel func exists
	service.ForceCancelSession("non-existent")
}

// ---------- UpdateMessageContent ----------

func TestUpdateMessageContent(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")

	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "original", nil, false, "NewSession")
	assert.NoError(t, err)

	err = service.UpdateMessageContent(int(msgID), "updated content")
	assert.NoError(t, err)

	msgs, _ := service.GetChatHistory("/project", "claude", sid)
	assert.Equal(t, "updated content", msgs[0].Content)
}

func TestUpdateMessageContent_NonExistent(t *testing.T) {
	setupDB(t)
	// Updating non-existent message should not error (UPDATE affects 0 rows)
	err := service.UpdateMessageContent(99999, "content")
	assert.NoError(t, err)
}

// ---------- UpdateExternalSessionID / GetExternalSessionID ----------

func TestUpdateAndGetExternalSessionID(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "opencode", "Test")

	// Initially empty (populated later by session_capture)
	assert.Equal(t, "", service.GetExternalSessionID(sid))

	// Set external ID
	err := service.UpdateExternalSessionID(sid, "ext-session-123")
	assert.NoError(t, err)

	// Get external ID (retry: dbRead may lag behind dbWrite under concurrent test load)
	var got string
	assert.Eventually(t, func() bool {
		got = service.GetExternalSessionID(sid)
		return got == "ext-session-123"
	}, 2*time.Second, 10*time.Millisecond, "expected ext-session-123, got %q", got)
}

func TestGetExternalSessionID_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetExternalSessionID("non-existent"))
}

// ---------- GetExpiredArchivedSessions ----------

func TestGetExpiredArchivedSessions_NoExpired(t *testing.T) {
	setupDB(t)

	// Active session — should not appear
	sid := helperCreateSession(t, "/project", "claude", "Active")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg", nil, false, "NewSession")

	// Recently archived session — within retention period
	sid2 := helperCreateSession(t, "/project", "claude", "Recently Deleted")
	_ = service.ArchiveSession("/project", "claude", sid2)

	cutoff := time.Now().AddDate(0, 0, -90) // 90 days ago
	ids, err := service.GetExpiredArchivedSessions(cutoff)
	assert.NoError(t, err)
	assert.Empty(t, ids)
}

func TestGetExpiredArchivedSessions_WithExpired(t *testing.T) {
	setupDB(t)

	// Create and delete a session, then manually set its updated_at to 100 days ago
	sid := helperCreateSession(t, "/project", "claude", "Old Deleted")
	_ = service.ArchiveSession("/project", "claude", sid)

	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", sid)
	assert.NoError(t, err)

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := service.GetExpiredArchivedSessions(cutoff)
	assert.NoError(t, err)
	assert.Contains(t, ids, sid)
}

func TestGetExpiredArchivedSessions_ActiveSessionsNotIncluded(t *testing.T) {
	setupDB(t)

	// Create an active session with old updated_at
	sid := helperCreateSession(t, "/project", "claude", "Old Active")
	_, _ = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", sid)

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := service.GetExpiredArchivedSessions(cutoff)
	assert.NoError(t, err)
	assert.NotContains(t, ids, sid)
}

func TestGetExpiredArchivedSessions_MultipleExpired(t *testing.T) {
	setupDB(t)

	// Create multiple expired sessions
	expectedIDs := make([]string, 0, 3)
	for i := range 3 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("Old %d", i))
		_ = service.ArchiveSession("/project", "claude", sid)
		_, _ = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", sid)
		expectedIDs = append(expectedIDs, sid)
	}

	// Create a recently archived session that should NOT appear
	recentSID := helperCreateSession(t, "/project", "claude", "Recent")
	_ = service.ArchiveSession("/project", "claude", recentSID)

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := service.GetExpiredArchivedSessions(cutoff)
	assert.NoError(t, err)
	assert.Len(t, ids, 3)
	for _, id := range expectedIDs {
		assert.Contains(t, ids, id)
	}
	assert.NotContains(t, ids, recentSID)
}

// ---------- PurgeArchivedData ----------

func TestPurgeArchivedData_EmptyList(t *testing.T) {
	setupDB(t)

	sessions, messages, err := service.PurgeArchivedData(nil)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), sessions)
	assert.Equal(t, int64(0), messages)
}

func TestPurgeArchivedData_HardDeletesSessions(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Purge")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "reply1", nil, false, "NewSession")
	_ = service.ArchiveSession("/project", "claude", sid)

	sessionsPurged, messagesPurged, err := service.PurgeArchivedData([]string{sid})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)

	// Verify session is completely gone from DB
	var count int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", sid).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify messages are completely gone
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestPurgeArchivedData_CleansSessionTagLinks guards the retention path: the
// link table has no FK to chat_sessions, so without an explicit delete the rows
// survive the purge forever and the tag's session count stays inflated.
// HardDeleteSession does this cleanup; this path was missed.
func TestPurgeArchivedData_CleansSessionTagLinks(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Tagged")
	keep := helperCreateSession(t, "/project", "claude", "Keep")
	require.NoError(t, service.SetSessionTags(sid, "/project", []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(keep, "/project", []service.SessionTagRef{{Name: "bug"}}))
	_ = service.ArchiveSession("/project", "claude", sid)

	_, _, err := service.PurgeArchivedData([]string{sid})
	assert.NoError(t, err)

	// No orphan link row for the purged session.
	var count int
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM session_tag_links WHERE session_id = ?", sid,
	).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "purge must not leave orphan tag links")

	// The surviving session keeps its link, and the count is now accurate (1).
	tags, err := service.GetSessionTags(keep)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(tags))

	all, err := service.ListSessionTags("/project")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 1, all[0].Count, "tag count must reflect only live sessions")
}

func TestPurgeArchivedData_DoesNotPurgeActiveSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Active")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg", nil, false, "NewSession")

	// Try to purge an active (non-archived) session — should not delete it
	sessionsPurged, messagesPurged, err := service.PurgeArchivedData([]string{sid})
	assert.NoError(t, err)
	assert.Equal(t, int64(0), sessionsPurged) // WHERE archived = 1 prevents purge
	assert.Equal(t, int64(1), messagesPurged) // messages are deleted regardless of archived flag

	// Session should still exist (wasn't archived)
	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "msg", title)
}

func TestPurgeArchivedData_MultipleSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Purge 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Purge 2")
	_, _ = service.AddChatMessage("/project", "claude", sid1, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid2, "user", "msg2", nil, false, "NewSession")
	_ = service.ArchiveSession("/project", "claude", sid1)
	_ = service.ArchiveSession("/project", "claude", sid2)

	sessionsPurged, messagesPurged, err := service.PurgeArchivedData([]string{sid1, sid2})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)
}

func TestPurgeArchivedData_NonExistentSessionID(t *testing.T) {
	setupDB(t)

	sessionsPurged, messagesPurged, err := service.PurgeArchivedData([]string{"non-existent-id"})
	assert.NoError(t, err)
	assert.Equal(t, int64(0), sessionsPurged)
	assert.Equal(t, int64(0), messagesPurged)
}

// ---------- HardDeleteSession ----------

func TestHardDeleteSession_ActiveSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Active To HardDelete")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "reply1", nil, false, "NewSession")

	err := service.HardDeleteSession(sid)
	assert.NoError(t, err)

	// Verify all data is gone
	var count int
	assert.NoError(t, service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", sid).Scan(&count))
	assert.Equal(t, 0, count, "session should be gone")
	assert.NoError(t, service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&count))
	assert.Equal(t, 0, count, "messages should be gone")
}

func TestHardDeleteSession_ArchivedSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Archived To HardDelete")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg1", nil, false, "NewSession")
	_ = service.ArchiveSession("/project", "claude", sid)

	err := service.HardDeleteSession(sid)
	assert.NoError(t, err)

	var count int
	assert.NoError(t, service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", sid).Scan(&count))
	assert.Equal(t, 0, count, "archived session should be gone after hard delete")
}

func TestHardDeleteSession_NonExistentSession(t *testing.T) {
	setupDB(t)

	err := service.HardDeleteSession("nonexistent-session-id")
	assert.NoError(t, err, "hard-deleting non-existent session should not error")
}

// ---------- AddChatMessage guard against archived session ----------

func TestAddChatMessage_RejectsDeletedSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")
	_ = service.ArchiveSession("/project", "claude", sid)

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "after delete", nil, false, "NewSession")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "archived session")
}

func TestAddChatMessage_NonExistentSessionStillWorks(t *testing.T) {
	setupDB(t)

	// Non-existent session doesn't have a archived=1 row, so the guard doesn't block
	// (This is the existing behavior — message gets inserted with orphaned session_id)
	_, err := service.AddChatMessage("/project", "claude", "non-existent-session", "user", "orphan msg", nil, false, "NewSession")
	assert.NoError(t, err)
}

// ---------- SessionType (Task 2/3/4) ----------

func TestCreateSession_ScheduledType(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Scheduled Session", "", "", "default", "scheduled")
	assert.NoError(t, err)
	assert.NotEmpty(t, sid)

	// Verify session_type is stored correctly in DB
	var sessionType string
	err = service.UnsafeDBForTest().QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sid).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "scheduled", sessionType)
}

func TestCreateSession_DefaultsToChatType(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Chat Session", "", "", "default", "")
	assert.NoError(t, err)
	assert.NotEmpty(t, sid)

	// Verify session_type defaults to 'chat'
	var sessionType string
	err = service.UnsafeDBForTest().QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sid).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "chat", sessionType)
}

func TestGetSessions_FiltersBySessionType(t *testing.T) {
	setupDB(t)

	// Create a chat session and a scheduled session
	chatSID := helperCreateSession(t, "/project", "claude", "Chat Session")
	_ = chatSID
	schedSID := helperCreateScheduledSession(t, "/project", "claude", "Scheduled Session")
	_ = schedSID

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "chat", sessions[0].SessionType)
	assert.Equal(t, "Chat Session", sessions[0].Title)
}

func TestGetSessionCount_ExcludesScheduledSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Chat Session")
	helperCreateScheduledSession(t, "/project", "claude", "Scheduled Session")

	count, err := service.GetSessionCount("/project")
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGetSessions_SessionTypeField(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Chat Session")
	_ = sid

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "chat", sessions[0].SessionType)
}

func TestGetSessions_AllBackendsFiltersBySessionType(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Chat")
	helperCreateScheduledSession(t, "/project", "claude", "Scheduled")
	helperCreateSession(t, "/project", "codebuddy", "Chat CB")

	// Only chat sessions should appear
	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	for _, s := range sessions {
		assert.Equal(t, "chat", s.SessionType)
	}
}

// ---------- GetSessionsPaged ----------

func TestGetSessionsPaged_NoLimit_ReturnsAll(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "S1")
	helperCreateSession(t, "/project", "claude", "S2")
	helperCreateSession(t, "/project", "claude", "S3")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 0, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_LimitGreaterThanTotal(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "S1")
	helperCreateSession(t, "/project", "claude", "S2")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_LimitEqualsTotal(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "S1")
	helperCreateSession(t, "/project", "claude", "S2")
	helperCreateSession(t, "/project", "claude", "S3")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 3, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
	assert.False(t, hasMore) // limit+1=4, only 3 exist, so no more
}

func TestGetSessionsPaged_LimitLessThanTotal_HasMore(t *testing.T) {
	setupDB(t)

	for i := range 5 {
		helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
	}

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 3, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
	assert.True(t, hasMore)
}

func TestGetRecentSessions_NewestFirstIncludesArchived(t *testing.T) {
	setupDB(t)

	// Insert with explicit created_at in non-chronological order.
	insertSessionWithTime(t, "/project", "old", "Old", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "new", "New", "2024-03-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "arch", "Archived", "2024-02-01 10:00:00", true)

	sessions, _, err := service.GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, sessions, 3)
	// Reverse chronological order (newest first).
	assert.Equal(t, "new", sessions[0].ID)
	assert.Equal(t, "arch", sessions[1].ID)
	assert.Equal(t, "old", sessions[2].ID)
	// Archived sessions are included with the flag set.
	assert.True(t, sessions[1].Archived)
	assert.False(t, sessions[2].Archived)
}

func TestGetRecentSessions_ProjectScopedAndLimited(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "a", "A", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "b", "B", "2024-01-02 10:00:00", false)
	insertSessionWithTime(t, "/other", "c", "C", "2024-01-03 10:00:00", false)

	// Other project must be excluded.
	sessions, _, err := service.GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, sessions, 2)

	// Limit truncates the newest-first list.
	sessions, _, err = service.GetRecentSessions("/project", 1, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "b", sessions[0].ID)
}

func TestGetRecentSessions_EmptyProjectBrowsesAll(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "a", "A", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/other", "b", "B", "2024-01-02 10:00:00", false)

	// Empty project path → across all projects (CLI global browse).
	sessions, _, err := service.GetRecentSessions("", 0, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "b", sessions[0].ID)
}

func TestGetRecentSessions_NoSessions(t *testing.T) {
	setupDB(t)

	sessions, _, err := service.GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 0)
}

func TestGetRecentSessions_ArchiveFilter(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "active-1", "A1", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "arch-1", "R1", "2024-02-01 10:00:00", true)
	insertSessionWithTime(t, "/project", "active-2", "A2", "2024-03-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "arch-2", "R2", "2024-04-01 10:00:00", true)

	// Active only.
	active, _, err := service.GetRecentSessions("/project", 0, service.SessionArchiveFilterActive, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, active, 2)
	for _, s := range active {
		assert.False(t, s.Archived)
	}
	assert.Equal(t, "active-2", active[0].ID)

	// Archived only.
	archived, _, err := service.GetRecentSessions("/project", 0, service.SessionArchiveFilterArchived, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, archived, 2)
	for _, s := range archived {
		assert.True(t, s.Archived)
	}
	assert.Equal(t, "arch-2", archived[0].ID)

	// All (default) includes both.
	all, _, err := service.GetRecentSessions("/project", 0, service.SessionArchiveFilterAll, "", "", "", "", "", "")
	assert.NoError(t, err)
	assert.Len(t, all, 4)
}

func TestGetRecentSessions_SortOrderOldest(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "new", "New", "2024-03-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "old", "Old", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "mid", "Mid", "2024-02-01 10:00:00", false)

	oldest, _, err := service.GetRecentSessions("/project", 0, "", "", service.SessionSortOldest, "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, oldest, 3)
	assert.Equal(t, "old", oldest[0].ID)
	assert.Equal(t, "mid", oldest[1].ID)
	assert.Equal(t, "new", oldest[2].ID)
}

func TestNormalizeSessionArchiveFilterAndSortOrder(t *testing.T) {
	assert.Equal(t, "all", service.NormalizeSessionArchiveFilter(""))
	assert.Equal(t, "all", service.NormalizeSessionArchiveFilter("bogus"))
	assert.Equal(t, "active", service.NormalizeSessionArchiveFilter(" Active "))
	assert.Equal(t, "archived", service.NormalizeSessionArchiveFilter("ARCHIVED"))

	assert.Equal(t, "relevance", service.NormalizeSessionSortOrder(""))
	assert.Equal(t, "relevance", service.NormalizeSessionSortOrder("bogus"))
	assert.Equal(t, "newest", service.NormalizeSessionSortOrder("Newest"))
	assert.Equal(t, "oldest", service.NormalizeSessionSortOrder(" OLDEST "))
}

func TestNormalizeSessionTypeFilter(t *testing.T) {
	assert.Equal(t, "all", service.NormalizeSessionTypeFilter(""))
	assert.Equal(t, "all", service.NormalizeSessionTypeFilter("bogus"))
	assert.Equal(t, "chat", service.NormalizeSessionTypeFilter(" CHAT "))
	assert.Equal(t, "task", service.NormalizeSessionTypeFilter("Task"))
}

func TestSessionTypeDBValue(t *testing.T) {
	// "all" must yield the empty string so callers can use it as "no predicate".
	assert.Equal(t, "", service.SessionTypeDBValue(""))
	assert.Equal(t, "", service.SessionTypeDBValue("all"))
	assert.Equal(t, "", service.SessionTypeDBValue("bogus"))
	assert.Equal(t, "chat", service.SessionTypeDBValue("chat"))
	// The user-facing "task" maps onto the DB's 'scheduled'.
	assert.Equal(t, "scheduled", service.SessionTypeDBValue("task"))
}

// TestGetRecentSessions_TypeFilterSeparation locks down the browse-mode rule:
// the type filter never mixes session kinds. "all" and "chat" both list
// conversations; only an explicit "task" lists task executions.
func TestGetRecentSessions_TypeFilterSeparation(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "conv", "Conversation", "2024-01-01 10:00:00", false)
	insertSessionWithTypeAndTime(t, "/project", "job", "Task run", "scheduled", "2024-02-01 10:00:00", false)

	// Default / "all" → conversations only, never tasks.
	all, _, err := service.GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "conv", all[0].ID)
	assert.Equal(t, "chat", all[0].SessionType)

	// Explicit "chat" behaves like "all".
	chat, _, err := service.GetRecentSessions("/project", 0, "", service.SessionTypeFilterChat, "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, chat, 1)
	assert.Equal(t, "conv", chat[0].ID)

	// Explicit "task" lists the task execution instead.
	task, _, err := service.GetRecentSessions("/project", 0, "", service.SessionTypeFilterTask, "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, task, 1)
	assert.Equal(t, "job", task[0].ID)
	assert.Equal(t, "scheduled", task[0].SessionType)

	// The type filter combines with the archive filter rather than replacing it.
	archivedTask, _, err := service.GetRecentSessions("/project", 0, service.SessionArchiveFilterArchived, service.SessionTypeFilterTask, "", "", "", "", "")
	assert.NoError(t, err)
	assert.Len(t, archivedTask, 0)
}

// ---------- SearchSessionsByTitle ----------

func TestSearchSessionsByTitle_MatchesTitleAndOrdersNewestFirst(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "a", "数据库优化方案", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "b", "数据库迁移记录", "2024-03-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "c", "前端重构", "2024-02-01 10:00:00", false)

	got, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, got, 2)
	// Newest first.
	assert.Equal(t, "b", got[0].ID)
	assert.Equal(t, "a", got[1].ID)
	assert.Equal(t, "数据库迁移记录", got[0].Title)
}

// Every term must be present: a second term narrows the result rather than
// widening it, which is what makes a multi-word query useful.
func TestSearchSessionsByTitle_AllTermsMustMatch(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "both", "数据库优化方案", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "one", "数据库迁移记录", "2024-01-02 10:00:00", false)
	insertSessionWithTime(t, "/project", "none", "前端重构", "2024-01-03 10:00:00", false)

	got, err := service.SearchSessionsByTitle("/project", []string{"数据库", "优化"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "both", got[0].ID)

	// Terms are ANDed, so a term absent from every title yields nothing even
	// though the first term alone would have matched two rows.
	got, err = service.SearchSessionsByTitle("/project", []string{"数据库", "不存在"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	assert.Empty(t, got)
}

// A literal % or _ in the query must not act as a LIKE wildcard, and the
// backslash escape must survive the round trip.
func TestSearchSessionsByTitle_EscapesLikeWildcards(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "pct", "覆盖率 100% 达成", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "other", "覆盖率统计", "2024-01-02 10:00:00", false)

	got, err := service.SearchSessionsByTitle("/project", []string{"100%"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pct", got[0].ID)

	// A bare "%" must match only titles that literally contain it, not every
	// title in the project.
	got, err = service.SearchSessionsByTitle("/project", []string{"%"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pct", got[0].ID)

	// A backslash in the query is matched literally too.
	insertSessionWithTime(t, "/project", "bs", `路径 C:\temp 记录`, "2024-01-03 10:00:00", false)
	got, err = service.SearchSessionsByTitle("/project", []string{`C:\temp`}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "bs", got[0].ID)
}

func TestSearchSessionsByTitle_Filters(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "active", "数据库优化", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "archived", "数据库归档", "2024-02-01 10:00:00", true)
	insertSessionWithTime(t, "/other", "otherproj", "数据库优化", "2024-03-01 10:00:00", false)
	insertSessionWithTypeAndTime(t, "/project", "task", "数据库任务", "scheduled", "2024-04-01 10:00:00", false)

	// Project scope is always applied.
	got, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	// Conversations only: the task execution is excluded by default.
	require.Len(t, got, 2)
	for _, s := range got {
		assert.NotEqual(t, "task", s.ID)
		assert.NotEqual(t, "otherproj", s.ID)
	}

	// Archive filter narrows to one side.
	active, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, service.SessionArchiveFilterActive, "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "active", active[0].ID)

	archived, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, service.SessionArchiveFilterArchived, "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, archived, 1)
	assert.Equal(t, "archived", archived[0].ID)

	// Type filter switches to task executions.
	tasks, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", service.SessionTypeFilterTask, "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, "task", tasks[0].ID)

	// Time range bounds the creation time.
	windowed, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "2024-01-15 00:00:00", "2024-03-15 00:00:00", "", "")
	assert.NoError(t, err)
	require.Len(t, windowed, 1)
	assert.Equal(t, "archived", windowed[0].ID)

	// Limit truncates the newest-first list.
	limited, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 1, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, "archived", limited[0].ID)
}

func TestSearchSessionsByTitle_EmptyTermsMatchNothing(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "a", "任意标题", "2024-01-01 10:00:00", false)

	// No terms → no matches. Callers wanting the whole project use
	// GetRecentSessions; returning everything here would silently turn a
	// title search into a browse.
	got, err := service.SearchSessionsByTitle("/project", nil, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	assert.Empty(t, got)

	got, err = service.SearchSessionsByTitle("/project", []string{}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	assert.Empty(t, got)
}

// SQLite's LIKE folds ASCII case, so an ASCII query matches regardless of how
// the title was typed.
func TestSearchSessionsByTitle_CaseInsensitiveASCII(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "a", "Fix RAG Indexer", "2024-01-01 10:00:00", false)

	got, err := service.SearchSessionsByTitle("/project", []string{"rag"}, 0, "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)
}

// The single-session and excluded-session scopes mirror the content channel's.
// exclude_session_id is what keeps the current conversation out of its own
// /cb-chatsearch results, so the title channel must honor it too.
func TestSearchSessionsByTitle_SessionScopes(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "s1", "数据库优化", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "s2", "数据库迁移", "2024-01-02 10:00:00", false)
	insertSessionWithTime(t, "/project", "s3", "数据库归档", "2024-01-03 10:00:00", false)

	// sessionID narrows to exactly that session.
	one, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "s2", "")
	assert.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "s2", one[0].ID)

	// excludeSessionID drops it, leaving the other two.
	rest, err := service.SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "", "s2")
	assert.NoError(t, err)
	require.Len(t, rest, 2)
	for _, s := range rest {
		assert.NotEqual(t, "s2", s.ID)
	}
}

func TestEscapeLikePattern(t *testing.T) {
	// Escaping is what makes the ESCAPE '\' clause meaningful; without it a
	// query of "%" would match every row.
	assert.Equal(t, `100\%`, service.EscapeLikePatternForTest("100%"))
	assert.Equal(t, `a\_b`, service.EscapeLikePatternForTest("a_b"))
	assert.Equal(t, `C:\\temp`, service.EscapeLikePatternForTest(`C:\temp`))
	assert.Equal(t, "plain", service.EscapeLikePatternForTest("plain"))
}

func TestGetRecentSessions_CursorPaginationNewest(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "s1", "S1", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "s2", "S2", "2024-02-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "s3", "S3", "2024-03-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "s4", "S4", "2024-04-01 10:00:00", false)

	// Page 1: newest first, 2 rows + hasMore.
	page1, hasMore, err := service.GetRecentSessions("/project", 2, "", "", "", "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, "s4", page1[0].ID)
	assert.Equal(t, "s3", page1[1].ID)

	// Page 2: cursor from the last row of page 1.
	cursor := page1[len(page1)-1].CreatedAt.Format("2006-01-02 15:04:05")
	page2, hasMore2, err := service.GetRecentSessions("/project", 2, "", "", "", "", "", cursor, page1[1].ID)
	assert.NoError(t, err)
	require.Len(t, page2, 2)
	assert.False(t, hasMore2)
	assert.Equal(t, "s2", page2[0].ID)
	assert.Equal(t, "s1", page2[1].ID)

	// No overlap between pages.
	seen := map[string]bool{}
	for _, s := range append(page1, page2...) {
		require.False(t, seen[s.ID], "session %s appeared twice", s.ID)
		seen[s.ID] = true
	}
	assert.Len(t, seen, 4)
}

func TestGetRecentSessions_CursorPaginationOldest(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "s1", "S1", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "s2", "S2", "2024-02-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "s3", "S3", "2024-03-01 10:00:00", false)

	page1, hasMore, err := service.GetRecentSessions("/project", 2, "", "", service.SessionSortOldest, "", "", "", "")
	assert.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, "s1", page1[0].ID)
	assert.Equal(t, "s2", page1[1].ID)

	cursor := page1[len(page1)-1].CreatedAt.Format("2006-01-02 15:04:05")
	page2, hasMore2, err := service.GetRecentSessions("/project", 2, "", "", service.SessionSortOldest, "", "", cursor, page1[1].ID)
	assert.NoError(t, err)
	require.Len(t, page2, 1)
	assert.False(t, hasMore2)
	assert.Equal(t, "s3", page2[0].ID)
}

func TestGetRecentSessions_CursorWithSameTimestamp(t *testing.T) {
	setupDB(t)

	// Rows sharing a created_at must still paginate without skip/duplicate:
	// the id tie-break carries the cursor forward.
	insertSessionWithTime(t, "/project", "a", "A", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "b", "B", "2024-01-01 10:00:00", false)
	insertSessionWithTime(t, "/project", "c", "C", "2024-01-01 10:00:00", false)

	seen := map[string]bool{}
	cursor, cursorID := "", ""
	for range 5 {
		page, hasMore, err := service.GetRecentSessions("/project", 1, "", "", "", "", "", cursor, cursorID)
		assert.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.False(t, seen[page[0].ID], "duplicate %s", page[0].ID)
		seen[page[0].ID] = true
		cursor = page[0].CreatedAt.Format("2006-01-02 15:04:05")
		cursorID = page[0].ID
		if !hasMore {
			break
		}
	}
	assert.Len(t, seen, 3)
}

func TestGetRecentSessions_TimeRangeFilter(t *testing.T) {
	setupDB(t)

	insertSessionWithTime(t, "/project", "jan", "Jan", "2024-01-15 10:00:00", false)
	insertSessionWithTime(t, "/project", "feb", "Feb", "2024-02-15 10:00:00", false)
	insertSessionWithTime(t, "/project", "mar", "Mar", "2024-03-15 10:00:00", false)

	// Inclusive window covering February only.
	feb, _, err := service.GetRecentSessions("/project", 0, "", "", "", "2024-02-01 00:00:00", "2024-02-29 23:59:59", "", "")
	assert.NoError(t, err)
	require.Len(t, feb, 1)
	assert.Equal(t, "feb", feb[0].ID)

	// Lower bound only.
	fromFeb, _, err := service.GetRecentSessions("/project", 0, "", "", "", "2024-02-01 00:00:00", "", "", "")
	assert.NoError(t, err)
	assert.Len(t, fromFeb, 2)

	// Upper bound only.
	toFeb, _, err := service.GetRecentSessions("/project", 0, "", "", "", "", "2024-02-29 23:59:59", "", "")
	assert.NoError(t, err)
	assert.Len(t, toFeb, 2)

	// Window outside the data set → no rows.
	none, _, err := service.GetRecentSessions("/project", 0, "", "", "", "2025-01-01 00:00:00", "2025-12-31 23:59:59", "", "")
	assert.NoError(t, err)
	assert.Len(t, none, 0)

	// Time range combines with the archive filter rather than replacing it.
	insertSessionWithTime(t, "/project", "feb-arch", "FebArch", "2024-02-20 10:00:00", true)
	febActive, _, err := service.GetRecentSessions("/project", 0, service.SessionArchiveFilterActive, "", "", "2024-02-01 00:00:00", "2024-02-29 23:59:59", "", "")
	assert.NoError(t, err)
	require.Len(t, febActive, 1)
	assert.Equal(t, "feb", febActive[0].ID)
}

func TestGetSessionsPaged_CursorSecondPage(t *testing.T) {
	setupDB(t)

	// Create 5 sessions with staggered created_at times
	for i := range 5 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
		// Stagger created_at so ordering is deterministic
		_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now', ? || ' seconds') WHERE id = ?", fmt.Sprintf("-%d", (4-i)*60), sid)
		assert.NoError(t, err)
	}

	// First page: limit=2, no cursor
	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 2, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.True(t, hasMore)

	// Use last session as cursor
	lastSession := sessions[len(sessions)-1]
	cursor := lastSession.CreatedAt.Format("2006-01-02 15:04:05")
	cursorID := lastSession.ID

	// Second page: cursor from last session of first page
	sessions2, hasMore2, err := service.GetSessionsPaged("/project", "", 2, cursor, cursorID, nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions2, 2)
	assert.True(t, hasMore2)

	// Verify no overlap between page 1 and page 2
	page1IDs := make(map[string]bool)
	for _, s := range sessions {
		page1IDs[s.ID] = true
	}
	for _, s := range sessions2 {
		assert.False(t, page1IDs[s.ID], "session %s should not appear in both pages", s.ID)
	}
}

func TestGetSessionsPaged_CursorLastPage(t *testing.T) {
	setupDB(t)

	for i := range 5 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
		_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now', ? || ' seconds') WHERE id = ?", fmt.Sprintf("-%d", (4-i)*60), sid)
		assert.NoError(t, err)
	}

	// First page: limit=3
	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 3, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.True(t, hasMore)

	// Second page: cursor from last session
	lastSession := sessions[len(sessions)-1]
	cursor := lastSession.CreatedAt.Format("2006-01-02 15:04:05")
	cursorID := lastSession.ID

	sessions2, hasMore2, err := service.GetSessionsPaged("/project", "", 3, cursor, cursorID, nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions2, 2) // only 2 remaining
	assert.False(t, hasMore2)
}

func TestGetSessionsPaged_EmptyProject(t *testing.T) {
	setupDB(t)

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Empty(t, sessions)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_FiltersByProject(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/proj1", "claude", "P1-S1")
	helperCreateSession(t, "/proj1", "claude", "P1-S2")
	helperCreateSession(t, "/proj2", "claude", "P2-S1")

	sessions, hasMore, err := service.GetSessionsPaged("/proj1", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_ExcludesDeletedSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Active")
	archivedSID := helperCreateSession(t, "/project", "claude", "Deleted")
	err := service.ArchiveSession("/project", "claude", archivedSID)
	assert.NoError(t, err)

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.False(t, hasMore)
	assert.Equal(t, "Active", sessions[0].Title)
}

func TestGetSessionsPaged_ExcludesScheduledSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Chat")
	helperCreateScheduledSession(t, "/project", "claude", "Scheduled")

	sessions, _, err := service.GetSessionsPaged("/project", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "Chat", sessions[0].Title)
}

func TestGetSessionsPaged_OrderedByCreatedDesc(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Old")
	sid2 := helperCreateSession(t, "/project", "claude", "New")

	// Set explicit created_at timestamps to guarantee ordering (SQLite time precision is seconds)
	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now', '-60 seconds') WHERE id = ?", sid1)
	assert.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now') WHERE id = ?", sid2)
	assert.NoError(t, err)

	// A later interaction on the older session must NOT change ordering.
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '+60 seconds') WHERE id = ?", sid1)
	assert.NoError(t, err)

	sessions, _, err := service.GetSessionsPaged("/project", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.Equal(t, sid2, sessions[0].ID) // most recently created first
	assert.Equal(t, sid1, sessions[1].ID)
}

func TestGetSessionsPaged_AllPagesCoverAllSessions(t *testing.T) {
	setupDB(t)

	// Create 7 sessions with staggered created_at times
	allIDs := make([]string, 0, 7)
	for i := range 7 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
		allIDs = append(allIDs, sid)
		_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = datetime('now', ? || ' seconds') WHERE id = ?", fmt.Sprintf("-%d", (6-i)*60), sid)
		assert.NoError(t, err)
	}

	// Paginate through all sessions: limit=3
	var collectedIDs []string
	cursor := ""
	cursorID := ""
	limit := 3
	page := 0

	for {
		sessions, hasMore, err := service.GetSessionsPaged("/project", "", limit, cursor, cursorID, nil, nil, "")
		assert.NoError(t, err)
		assert.NotEmpty(t, sessions, "page %d should not be empty", page)

		for _, s := range sessions {
			collectedIDs = append(collectedIDs, s.ID)
		}

		if !hasMore {
			break
		}

		lastSession := sessions[len(sessions)-1]
		cursor = lastSession.CreatedAt.Format("2006-01-02 15:04:05")
		cursorID = lastSession.ID
		page++

		if page > 10 {
			t.Fatal("too many pages, infinite loop?")
		}
	}

	// All sessions should be collected without duplicates
	assert.Len(t, collectedIDs, 7)
	uniqueIDs := make(map[string]bool)
	for _, id := range collectedIDs {
		assert.False(t, uniqueIDs[id], "duplicate session ID: %s", id)
		uniqueIDs[id] = true
	}
	// All original IDs should be present
	for _, id := range allIDs {
		assert.True(t, uniqueIDs[id], "missing session ID: %s", id)
	}
}

func TestGetSessionsPaged_SameTimestampTiebreaker(t *testing.T) {
	setupDB(t)

	// Create 3 sessions: 2 with same created_at, 1 with a later created_at
	// to test the (created_at = cursor AND id < cursorID) tiebreaker
	sid1 := helperCreateSession(t, "/project", "claude", "Tie1")
	sid2 := helperCreateSession(t, "/project", "claude", "Tie2")
	sid3 := helperCreateSession(t, "/project", "claude", "Newer")

	// Set sid1 and sid2 to the same created_at, sid3 slightly newer
	baseTime := "2026-01-15 12:00:00"
	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = ? WHERE id = ?", baseTime, sid1)
	assert.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = ? WHERE id = ?", baseTime, sid2)
	assert.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET created_at = '2026-01-15 12:01:00' WHERE id = ?", sid3)
	assert.NoError(t, err)

	// First page: limit=2 — should get sid3 (newest) and one of sid1/sid2
	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 2, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.True(t, hasMore)

	// Second page: cursor from last session of page 1
	lastSession := sessions[len(sessions)-1]
	cursor := lastSession.CreatedAt.Format("2006-01-02 15:04:05")
	cursorID := lastSession.ID

	sessions2, hasMore2, err := service.GetSessionsPaged("/project", "", 2, cursor, cursorID, nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions2, 1) // only 1 remaining
	assert.False(t, hasMore2)

	// Verify no overlap
	page1IDs := make(map[string]bool)
	for _, s := range sessions {
		page1IDs[s.ID] = true
	}
	for _, s := range sessions2 {
		assert.False(t, page1IDs[s.ID], "session %s should not appear in both pages", s.ID)
	}
}

// TestGetSessionsPaged_CursorIsCreatedAtNotUpdatedAt pins the pagination
// contract: the paged query orders and filters by created_at, so the cursor
// must be created_at. Using updated_at (which is >= created_at and bumped on
// every message) as the cursor makes the `created_at < cursor` filter match
// rows already returned on the previous page, duplicating the list — the
// regression that produced duplicate sessions in the UI.
func TestGetSessionsPaged_CursorIsCreatedAtNotUpdatedAt(t *testing.T) {
	setupDB(t)

	// Three sessions ordered by created_at: oldest → newest.
	sidOld := helperCreateSession(t, "/project", "claude", "Old")
	sidMid := helperCreateSession(t, "/project", "claude", "Mid")
	sidNew := helperCreateSession(t, "/project", "claude", "New")
	for id, offset := range map[string]int{sidOld: -120, sidMid: -60, sidNew: 0} {
		_, err := service.UnsafeDBForTest().Exec(
			"UPDATE chat_sessions SET created_at = datetime('now', ? || ' seconds') WHERE id = ?",
			fmt.Sprintf("%d", offset), id,
		)
		assert.NoError(t, err)
	}

	// Push the OLDEST session's updated_at far into the future. If the cursor
	// were updated_at, page 2 would re-return it (and its neighbors) because
	// their created_at is < that future timestamp.
	_, err := service.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET updated_at = datetime('now', '+1 day') WHERE id = ?", sidOld,
	)
	assert.NoError(t, err)

	// Page 1 (limit=1) → newest session.
	page1, hasMore, err := service.GetSessionsPaged("/project", "", 1, "", "", nil, nil, "")
	assert.NoError(t, err)
	require.Len(t, page1, 1)
	assert.Equal(t, sidNew, page1[0].ID)
	assert.True(t, hasMore)

	// Page 2 uses the created_at cursor — must be the middle session, NOT a
	// repeat of page 1.
	cursor := page1[0].CreatedAt.Format("2006-01-02 15:04:05")
	page2, _, err := service.GetSessionsPaged("/project", "", 1, cursor, page1[0].ID, nil, nil, "")
	assert.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, sidMid, page2[0].ID)

	// Sanity: feeding an updated_at value as the cursor re-returns page 1's row.
	// sidOld.updated_at is +1 day, so `created_at < <that>` matches sidNew —
	// the exact duplicate-producing behavior this contract guards against.
	var oldUpdatedAt string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT updated_at FROM chat_sessions WHERE id = ?", sidOld,
	).Scan(&oldUpdatedAt)
	require.NoError(t, err)
	badCursor, _, err := service.GetSessionsPaged("/project", "", 1, oldUpdatedAt, page1[0].ID, nil, nil, "")
	assert.NoError(t, err)
	require.Len(t, badCursor, 1)
	assert.Equal(t, sidNew, badCursor[0].ID,
		"an updated_at cursor must re-return page 1 (demonstrating the duplicate bug)")
}

// ---------- GetSessionTitlesBatch ----------

func TestGetSessionTitlesBatch_Empty(t *testing.T) {
	setupDB(t)

	titles, err := service.GetSessionTitlesBatch(nil)
	assert.NoError(t, err)
	assert.Empty(t, titles)

	titles, err = service.GetSessionTitlesBatch([]string{})
	assert.NoError(t, err)
	assert.Empty(t, titles)
}

func TestGetSessionTitlesBatch_SingleSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "My Title")

	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	assert.Equal(t, "My Title", titles[sid])
}

func TestGetSessionTitlesBatch_MultipleSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Title 1")
	sid2 := helperCreateSession(t, "/project", "codebuddy", "Title 2")

	titles, err := service.GetSessionTitlesBatch([]string{sid1, sid2})
	assert.NoError(t, err)
	assert.Equal(t, "Title 1", titles[sid1])
	assert.Equal(t, "Title 2", titles[sid2])
}

func TestGetSessionTitlesBatch_ExcludesEmptyTitles(t *testing.T) {
	setupDB(t)

	// Create session with a title, then set it to empty
	sid := helperCreateSession(t, "/project", "claude", "Has Title")
	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET title = '' WHERE id = ?", sid)
	assert.NoError(t, err)

	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "empty title should not be included")
}

func TestGetSessionTitlesBatch_ExcludesDeletedSessions(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")
	_ = service.ArchiveSession("/project", "claude", sid)

	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "archived session should not appear in batch titles")
}

func TestGetSessionTitlesBatch_NonExistentID(t *testing.T) {
	setupDB(t)

	titles, err := service.GetSessionTitlesBatch([]string{"non-existent-id"})
	assert.NoError(t, err)
	_, ok := titles["non-existent-id"]
	assert.False(t, ok, "non-existent ID should not appear in titles")
}

// ---------- GetSessionTitlesBatchIncludeArchived ----------

func TestGetSessionTitlesBatchIncludeArchived_IncludesArchivedSessions(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Deleted Session Title")
	_ = service.ArchiveSession("/project", "claude", sid)

	// Regular batch excludes archived sessions
	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "GetSessionTitlesBatch should exclude archived sessions")

	// IncludeArchived variant includes archived sessions
	titlesInc, err := service.GetSessionTitlesBatchIncludeArchived([]string{sid})
	assert.NoError(t, err)
	title, ok := titlesInc[sid]
	assert.True(t, ok, "GetSessionTitlesBatchIncludeArchived should include archived sessions")
	assert.Equal(t, "Deleted Session Title", title)
}

func TestGetSessionTitlesBatchIncludeArchived_ExcludesEmptyTitles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Has Title")
	_, err := service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET title = '' WHERE id = ?", sid)
	assert.NoError(t, err)

	titles, err := service.GetSessionTitlesBatchIncludeArchived([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "empty title should not be included even in IncludeArchived variant")
}

func TestGetSessionTitlesBatchIncludeArchived_Empty(t *testing.T) {
	setupDB(t)

	titles, err := service.GetSessionTitlesBatchIncludeArchived([]string{})
	assert.NoError(t, err)
	assert.Empty(t, titles)
}

// ---------- GetSessionFullInfo ----------

func TestGetSessionFullInfo(t *testing.T) {
	_ = setupDB(t)

	sid, err := service.CreateSession("/my/project", "claude", "Full Info Test", "my-agent", "gpt-4o", "user", "chat")
	assert.NoError(t, err)

	info := service.GetSessionFullInfo(sid)
	assert.NotNil(t, info)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "/my/project", info.ProjectPath)
	assert.Equal(t, "Full Info Test", info.Title)
	assert.Equal(t, "my-agent", info.AgentID)
	assert.Equal(t, "gpt-4o", info.Model)
	assert.Equal(t, "", info.Transport)
}

func TestGetSessionFullInfo_WithThinkingEffort(t *testing.T) {
	_ = setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Thinking", "claude", "", "default", "chat")
	assert.NoError(t, err)
	// ThinkingEffort is no longer persisted to DB; it comes from ACP runtime.
	// This test now only verifies that GetSessionFullInfo returns without error.
	info := service.GetSessionFullInfo(sid)
	assert.NotNil(t, info)
}

func TestGetSessionFullInfo_NotFound(t *testing.T) {
	_ = setupDB(t)

	info := service.GetSessionFullInfo("nonexistent")
	assert.Nil(t, info)
}

func TestGetSessionFullInfo_Deleted(t *testing.T) {
	_ = setupDB(t)

	sid, _ := service.CreateSession("/project", "claude", "Deleted", "claude", "", "default", "chat")
	service.ArchiveSession("/project", "claude", sid)

	info := service.GetSessionFullInfo(sid)
	assert.Nil(t, info)
}

// ---------- GetSessionInfo ----------

func TestGetSessionInfo(t *testing.T) {
	_ = setupDB(t)

	s1, _ := service.CreateSession("/project", "claude", "My Session", "claude", "claude-sonnet-4-6", "default", "chat")

	info, err := service.GetSessionInfo(s1)
	assert.NoError(t, err)
	assert.Equal(t, "My Session", info.Title)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "claude", info.AgentID)
	assert.Equal(t, "claude-sonnet-4-6", info.Model)
	assert.Equal(t, "", info.Transport)
}

// ---------- SaveMetadata ----------

func TestSaveMetadata(t *testing.T) {
	db := setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Meta")
	msgID, err := service.AddChatMessage("/project", "claude", sid, "assistant", `{"blocks":[],"metadata":{"model":"gpt-4","inputTokens":100,"outputTokens":50}}`, nil, false, "")
	assert.NoError(t, err)
	assert.Greater(t, msgID, int64(0))

	meta := &ai.Metadata{
		Mode:           "code",
		ThinkingEffort: "high",
		Transport:      "acp-stdio",
		Model:          "gpt-4",
		InputTokens:    100,
		OutputTokens:   50,
		WallMs:         3200,
		CostUSD:        0.005,
		StopReason:     "stop",
	}
	err = service.SaveMetadata(msgID, meta)
	assert.NoError(t, err)

	// Verify the row was inserted
	var mode, model, transport string
	var inputTokens, outputTokens, wallMs int
	var costUsd float64
	err = db.QueryRow("SELECT mode, model, transport, input_tokens, output_tokens, wall_ms, cost_usd FROM chat_metadata WHERE message_id = ?", msgID).
		Scan(&mode, &model, &transport, &inputTokens, &outputTokens, &wallMs, &costUsd)
	assert.NoError(t, err)
	assert.Equal(t, "code", mode)
	assert.Equal(t, "gpt-4", model)
	assert.Equal(t, "acp-stdio", transport)
	assert.Equal(t, 100, inputTokens)
	assert.Equal(t, 50, outputTokens)
	assert.Equal(t, 3200, wallMs)
	assert.InDelta(t, 0.005, costUsd, 0.0001)
}

func TestSaveMetadata_ExtendedColumns(t *testing.T) {
	db := setupDB(t)

	sid := helperCreateSession(t, "/project", "codebuddy", "MetaExt")
	msgID, err := service.AddChatMessage("/project", "codebuddy", sid, "assistant", `{"blocks":[],"metadata":{}}`, nil, false, "")
	assert.NoError(t, err)
	assert.Greater(t, msgID, int64(0))

	meta := &ai.Metadata{
		Model:               "glm-5.1",
		InputTokens:         29495,
		OutputTokens:        3,
		TotalTokens:         29498,
		CachedReadTokens:    8192,
		CachedWriteTokens:   0,
		ThoughtTokens:       0,
		CacheCreationTokens: 1200,
		CacheHitTokens:      8192,
		CacheMissTokens:     21303,
		Credit:              1.57,
		UsageByCategory:     map[string]int64{"tools": 22701, "conversation": 3894},
		SessionID:           "sess-xyz",
		RequestID:           "req-123",
		TraceID:             "trace-456",
		MessageID:           "agent-msg-1",
		MessageRequestID:    "msgreq-abc",
		RequestModelName:    "GLM-5.1",
		ResponseModelID:     "ep-b3mrev6r",
		FinishReason:        "stop",
		Outcome:             "SUCCESS",
		AgentPhase:          "completing",
	}
	err = service.SaveMetadata(msgID, meta)
	assert.NoError(t, err)

	var totalTokens, cachedRead, cachedWrite, thought, cacheCreation, cacheHit, cacheMiss int
	var credit float64
	var categoryJSON, requestID, traceID, responseModelID, sessionID string
	var agentMessageID, messageRequestID, requestModelName, finishReason, outcome, agentPhase string
	err = db.QueryRow(
		`SELECT total_tokens, cached_read_tokens, cached_write_tokens, thought_tokens,
		        cache_creation_tokens, cache_hit_tokens, cache_miss_tokens, credit,
		        usage_by_category, request_id, trace_id, response_model_id, session_id,
		        agent_message_id, message_request_id, request_model_name,
		        finish_reason, outcome, agent_phase
		 FROM chat_metadata WHERE message_id = ?`,
		msgID,
	).Scan(&totalTokens, &cachedRead, &cachedWrite, &thought,
		&cacheCreation, &cacheHit, &cacheMiss, &credit,
		&categoryJSON, &requestID, &traceID, &responseModelID, &sessionID,
		&agentMessageID, &messageRequestID, &requestModelName,
		&finishReason, &outcome, &agentPhase)
	assert.NoError(t, err)
	assert.Equal(t, 29498, totalTokens)
	assert.Equal(t, 8192, cachedRead)
	assert.Equal(t, 0, cachedWrite)
	assert.Equal(t, 0, thought)
	assert.Equal(t, 1200, cacheCreation)
	assert.Equal(t, 8192, cacheHit)
	assert.Equal(t, 21303, cacheMiss)
	assert.Equal(t, 1.57, credit)
	assert.Equal(t, `{"conversation":3894,"tools":22701}`, categoryJSON)
	assert.Equal(t, "req-123", requestID)
	assert.Equal(t, "trace-456", traceID)
	assert.Equal(t, "ep-b3mrev6r", responseModelID)
	assert.Equal(t, "sess-xyz", sessionID)
	assert.Equal(t, "agent-msg-1", agentMessageID)
	assert.Equal(t, "msgreq-abc", messageRequestID)
	assert.Equal(t, "GLM-5.1", requestModelName)
	assert.Equal(t, "stop", finishReason)
	assert.Equal(t, "SUCCESS", outcome)
	assert.Equal(t, "completing", agentPhase)
}

func TestSaveMetadata_NilMeta(t *testing.T) {
	_ = setupDB(t)
	err := service.SaveMetadata(1, nil)
	assert.NoError(t, err)
}

func TestSaveMetadata_ZeroMessageID(t *testing.T) {
	_ = setupDB(t)
	err := service.SaveMetadata(0, &ai.Metadata{Model: "test"})
	assert.NoError(t, err)
}

func TestMigrateMetadataFromContent_BackfillsFullColumns(t *testing.T) {
	db := setupDB(t)

	sid := helperCreateSession(t, "/project", "codebuddy", "MigrateMeta")
	// Insert an assistant message whose content JSON carries the full metadata
	// (token splits, category, trace identity), WITHOUT calling SaveMetadata —
	// mimicking legacy rows that predate the chat_metadata backfill.
	meta := ai.Metadata{
		InputTokens:         100,
		OutputTokens:        5,
		TotalTokens:         105,
		CachedReadTokens:    40,
		CacheHitTokens:      40,
		CacheMissTokens:     60,
		CacheCreationTokens: 10,
		Credit:              0.25,
		UsageByCategory:     map[string]int64{"tools": 90, "conversation": 15},
		SessionID:           sid,
		RequestID:           "req-mig",
		MessageID:           "agent-msg-mig",
		ResponseModelID:     "ep-mig",
		FinishReason:        "end_turn",
		Outcome:             "SUCCESS",
		AgentPhase:          "completing",
	}
	metaJSON, _ := json.Marshal(meta)
	content := `{"blocks":[],"metadata":` + string(metaJSON) + `}`
	msgID, err := service.AddChatMessage("/project", "codebuddy", sid, "assistant", content, nil, false, "")
	assert.NoError(t, err)

	service.MigrateMetadataFromContent()

	var cacheHit, cacheMiss, cacheCreation int
	var credit float64
	var categoryJSON, requestID, agentMessageID, finishReason string
	err = db.QueryRow(
		`SELECT cache_hit_tokens, cache_miss_tokens, cache_creation_tokens, credit,
		        usage_by_category, request_id, agent_message_id, finish_reason
		 FROM chat_metadata WHERE message_id = ?`,
		msgID,
	).Scan(&cacheHit, &cacheMiss, &cacheCreation, &credit,
		&categoryJSON, &requestID, &agentMessageID, &finishReason)
	assert.NoError(t, err)
	assert.Equal(t, 40, cacheHit)
	assert.Equal(t, 60, cacheMiss)
	assert.Equal(t, 10, cacheCreation)
	assert.Equal(t, 0.25, credit)
	assert.Equal(t, `{"conversation":15,"tools":90}`, categoryJSON)
	assert.Equal(t, "req-mig", requestID)
	assert.Equal(t, "agent-msg-mig", agentMessageID)
	assert.Equal(t, "end_turn", finishReason)
}

func TestGetSessionInfo_NotFound(t *testing.T) {
	_ = setupDB(t)

	_, err := service.GetSessionInfo("nonexistent")
	assert.Error(t, err)
}

func TestGetSessionInfo_Deleted(t *testing.T) {
	_ = setupDB(t)

	s1, _ := service.CreateSession("/project", "claude", "Deleted", "claude", "", "default", "chat")
	service.ArchiveSession("/project", "claude", s1)

	_, err := service.GetSessionInfo(s1)
	assert.Error(t, err)
}

// ---------- GetSessions UnreadCount ----------

func TestGetSessions_UnreadCount_NoMessages(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Empty Session")

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_AllUnread(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Unread All")

	// Add assistant messages (no last_read_at set → all unread)
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "reply 1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "reply 2", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 2, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_OnlyAssistantCounts(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Mixed Messages")

	// User messages should NOT be counted as unread
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)
	// Assistant messages should be counted
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "hi", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_StreamingExcluded(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Streaming")

	// Streaming assistant message should NOT count as unread
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "partial", nil, true, "")
	assert.NoError(t, err)
	// Finalized assistant message should count
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "done", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_AfterLastRead(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Read Some")

	// Add 3 assistant messages
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "old 1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "old 2", nil, false, "")
	assert.NoError(t, err)

	// Mark as read
	service.UpdateLastRead(sid)

	// Small sleep to ensure created_at is after last_read_at (SQLite second precision)
	time.Sleep(1100 * time.Millisecond)

	// Add 1 more assistant message after reading
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "new 1", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount, "only messages after last_read_at should be unread")
}

func TestGetSessions_UnreadCount_MultipleSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	// sid1: 3 unread
	_, _ = service.AddChatMessage("/project", "claude", sid1, "assistant", "a1", nil, false, "")
	_, _ = service.AddChatMessage("/project", "claude", sid1, "assistant", "a2", nil, false, "")
	_, _ = service.AddChatMessage("/project", "claude", sid1, "assistant", "a3", nil, false, "")

	// sid2: 1 unread
	_, _ = service.AddChatMessage("/project", "claude", sid2, "assistant", "b1", nil, false, "")

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)

	sessionMap := make(map[string]int)
	for _, s := range sessions {
		sessionMap[s.ID] = s.UnreadCount
	}
	assert.Equal(t, 3, sessionMap[sid1])
	assert.Equal(t, 1, sessionMap[sid2])
}

func TestGetSessions_UnreadCountScopedToProject(t *testing.T) {
	db := setupDB(t)
	_ = db

	// Create sessions in two different projects
	s1, _ := service.CreateSession("/project-a", "claude", "S1", "claude", "", "default", "chat")
	s2, _ := service.CreateSession("/project-b", "claude", "S2", "claude", "", "default", "chat")

	// Add assistant messages to both sessions
	service.AddChatMessage("/project-a", "claude", s1, "assistant", `{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	service.AddChatMessage("/project-b", "claude", s2, "assistant", `{"blocks":[{"type":"text","text":"world"}]}`, nil, false, "")

	// Query project-a only — should have 1 unread
	sessions, err := service.GetSessions("/project-a", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount, "unread count should only count messages in project-a")

	// Verify project-b also has 1 unread independently
	sessionsB, err := service.GetSessions("/project-b", "")
	assert.NoError(t, err)
	assert.Len(t, sessionsB, 1)
	assert.Equal(t, 1, sessionsB[0].UnreadCount, "unread count should only count messages in project-b")
}

// TestGetSessions_UnreadCount_IgnoresHistoryFromOtherProject pins the
// h.project_path = s.project_path predicate in unreadCountSubquery.
//
// It is redundant for rows written by current code, so a reader is tempted to
// delete it — but historic rows can carry a project_path that differs from
// their session's (messages were once persisted under the cookie's project
// instead of the session's owner, ISS-420). Without the predicate such a row is
// counted here while UpdateLastRead anchors on a different set, and the badge
// never clears. Verified by mutation: removing the predicate makes this fail.
func TestGetSessions_UnreadCount_IgnoresHistoryFromOtherProject(t *testing.T) {
	db := setupDB(t)

	insertSessionWithTime(t, "/projectA", "sess-a", "A", "2025-01-01 10:00:00", false)

	// One legitimate unread reply for sess-a, in sess-a's own project.
	_, err := db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'assistant', 'real reply', '2025-01-01 10:00:05')", "/projectA", "sess-a")
	require.NoError(t, err)

	// A stray reply for the SAME session id but tagged with another project.
	// This is the ISS-420 shape: it must not be attributed to sess-a.
	_, err = db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'assistant', 'stray reply', '2025-01-01 10:00:06')", "/projectB", "sess-a")
	require.NoError(t, err)

	sessions, err := service.GetSessions("/projectA", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount,
		"a reply whose project_path disagrees with its session must not count as unread")

	// The overview path shares the same subquery and must agree.
	overview, err := service.GetOverviewSessions()
	require.NoError(t, err)
	require.Len(t, overview, 1)
	assert.Equal(t, 1, overview[0].UnreadCount,
		"overview must agree with the per-project list about unread")
}

// ---------- GetOverviewSessions ----------

func TestGetOverviewSessions_crossProjectUnread(t *testing.T) {
	db := setupDB(t)

	// projectA: A1 has unread assistant message; A2 is read
	// projectB: B1 has no messages
	// archived: archived session in projectA must be excluded
	insertSessionWithTime(t, "/projectA", "session-A1", "A1 unread", "2025-01-01 10:00:00", false)
	insertSessionWithTime(t, "/projectA", "session-A2", "A2 read", "2025-01-01 10:00:01", false)
	insertSessionWithTime(t, "/projectB", "session-B1", "B1 empty", "2025-01-01 10:00:02", false)
	insertSessionWithTime(t, "/projectA", "session-archived", "Archived", "2025-01-01 10:00:03", true)

	_, err := db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'user', 'hello', '2025-01-01 10:00:00')", "/projectA", "session-A1")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'assistant', 'unread reply', '2025-01-01 10:00:05')", "/projectA", "session-A1")
	require.NoError(t, err)

	// A2 is read: assistant message created before last_read_at
	_, err = db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'assistant', 'old reply', '2025-01-01 10:00:02')", "/projectA", "session-A2")
	require.NoError(t, err)
	_, err = db.Exec("UPDATE chat_sessions SET last_read_at = '2025-01-01 10:00:10' WHERE id = 'session-A2'")
	require.NoError(t, err)

	sessions, err := service.GetOverviewSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 3, "should return A1, A2, B1 (archived excluded)")

	byID := make(map[string]model.ChatSession, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}

	// A1: unread assistant message → unread > 0
	a1, ok := byID["session-A1"]
	require.True(t, ok, "session-A1 should be present")
	assert.Equal(t, "/projectA", a1.ProjectPath)
	assert.Equal(t, 1, a1.UnreadCount, "A1 has one unread assistant message")

	// A2: read → unread == 0
	a2, ok := byID["session-A2"]
	require.True(t, ok, "session-A2 should be present")
	assert.Equal(t, "/projectA", a2.ProjectPath)
	assert.Equal(t, 0, a2.UnreadCount, "A2 was read, no unread messages")

	// B1: no messages → unread == 0
	b1, ok := byID["session-B1"]
	require.True(t, ok, "session-B1 should be present")
	assert.Equal(t, "/projectB", b1.ProjectPath)
	assert.Equal(t, 0, b1.UnreadCount, "B1 has no messages")

	// Archived session must not be returned
	_, ok = byID["session-archived"]
	assert.False(t, ok, "archived session must not be returned")
}

// TestGetOverviewSessions_sameIDAcrossProjects ensures unread counts stay
// isolated when two projects share the same session id (the natural key is
// UNIQUE(project_path, backend, id)). A regression test for the case where
// the unread subquery is keyed only by session_id and would let one project's
// count leak into the other.
//
// NOTE: the production schema declares id TEXT PRIMARY KEY, which by itself
// forbids duplicate ids. To exercise the intended composite-key scenario we
// recreate chat_sessions here with PRIMARY KEY (project_path, backend, id).
func TestGetOverviewSessions_sameIDAcrossProjects(t *testing.T) {
	db := setupDB(t)

	_, err := db.Exec("DROP TABLE chat_sessions")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE chat_sessions (
		id TEXT NOT NULL,
		project_path TEXT NOT NULL,
		backend TEXT NOT NULL,
		title TEXT NOT NULL,
		agent_id TEXT DEFAULT '',
		agent_source TEXT DEFAULT 'default',
		model TEXT DEFAULT '',
		session_type TEXT NOT NULL DEFAULT 'chat',
		external_session_id TEXT DEFAULT '',
		source_session_id TEXT DEFAULT NULL,
		transport TEXT DEFAULT '',
		auto_approve INTEGER NOT NULL DEFAULT 0,
		context_state TEXT DEFAULT '',
		archived INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_read_at DATETIME,
		PRIMARY KEY (project_path, backend, id)
	)`)
	require.NoError(t, err)

	// Both projects use the same session id — allowed by the composite key
	insertSessionWithTime(t, "/projectA", "shared-session", "A shared", "2025-01-01 10:00:00", false)
	insertSessionWithTime(t, "/projectB", "shared-session", "B shared", "2025-01-01 10:00:01", false)

	// projectA: 2 unread assistant messages (last_read_at is NULL → all unread)
	for range 2 {
		_, err := db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'assistant', 'a reply', '2025-01-01 10:00:05')", "/projectA", "shared-session")
		require.NoError(t, err)
	}
	// projectB: 3 unread assistant messages
	for range 3 {
		_, err := db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, 'claude', ?, 'assistant', 'b reply', '2025-01-01 10:00:05')", "/projectB", "shared-session")
		require.NoError(t, err)
	}

	sessions, err := service.GetOverviewSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 2, "two entries expected: same id in two projects")

	byProject := make(map[string]model.ChatSession, len(sessions))
	for _, s := range sessions {
		byProject[s.ProjectPath] = s
	}

	// Same session id appears in both projects
	assert.Equal(t, "shared-session", byProject["/projectA"].ID)
	assert.Equal(t, "shared-session", byProject["/projectB"].ID)

	// Unread counts must not leak across projects
	a, ok := byProject["/projectA"]
	require.True(t, ok, "projectA session should be present")
	assert.Equal(t, 2, a.UnreadCount, "projectA unread must not include projectB's messages")

	b, ok := byProject["/projectB"]
	require.True(t, ok, "projectB session should be present")
	assert.Equal(t, 3, b.UnreadCount, "projectB unread must not include projectA's messages")
}

// ---------- GetSessionsPaged UnreadCount ----------

func TestGetSessionsPaged_UnreadCount(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged Unread")

	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "msg", nil, false, "")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "", nil, nil, "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount)
	assert.False(t, hasMore)
}

// ---------- dbRead initialization ----------

func TestDBRead_Initialized_ChatDB(t *testing.T) {
	_ = setupDB(t)
	assert.NotNil(t, service.ReadDB(), "dbRead should be initialized in test setup")
}

// ---------- GetRunningSessionIDs ----------

func TestGetRunningSessionIDs_Empty(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	ids := service.GetRunningSessionIDs()
	assert.Empty(t, ids)
}

func TestGetRunningSessionIDs_MultipleRunning(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	service.SetSessionRunning("sess-1", true)
	service.SetSessionRunning("sess-2", true)
	service.SetSessionRunning("sess-3", true)

	ids := service.GetRunningSessionIDs()
	assert.Len(t, ids, 3)

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	assert.True(t, idSet["sess-1"])
	assert.True(t, idSet["sess-2"])
	assert.True(t, idSet["sess-3"])

	// Clean up
	service.SetSessionRunning("sess-1", false)
	service.SetSessionRunning("sess-2", false)
	service.SetSessionRunning("sess-3", false)
}

func TestGetRunningSessionIDs_SomeRunning(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	service.SetSessionRunning("sess-1", true)
	service.SetSessionRunning("sess-2", true)

	ids := service.GetRunningSessionIDs()
	assert.Len(t, ids, 2)

	// After stopping one
	service.SetSessionRunning("sess-1", false)
	ids = service.GetRunningSessionIDs()
	assert.Len(t, ids, 1)
	assert.Equal(t, "sess-2", ids[0])

	// Clean up
	service.SetSessionRunning("sess-2", false)
}

// ---------- GetLatestSessionID ----------

func TestGetLatestSessionID(t *testing.T) {
	_ = setupDB(t)

	// No sessions yet
	_, _, err := service.GetLatestSessionID("/project")
	assert.Error(t, err)

	// Create a session
	s1, _ := service.CreateSession("/project", "claude", "First", "claude", "", "default", "chat")

	// Should return it
	id, backend, err := service.GetLatestSessionID("/project")
	assert.NoError(t, err)
	assert.Equal(t, s1, id)
	assert.Equal(t, "claude", backend)

	// Create another and force its updated_at ahead (SQLite timestamps have second precision)
	s2, _ := service.CreateSession("/project", "codebuddy", "Second", "codebuddy", "", "default", "chat")
	service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '+1 second') WHERE id = ?", s2)

	// Should return the newer one
	id, backend, err = service.GetLatestSessionID("/project")
	assert.NoError(t, err)
	assert.Equal(t, s2, id)
	assert.Equal(t, "codebuddy", backend)

	// Different project should return error
	_, _, err = service.GetLatestSessionID("/other-project")
	assert.Error(t, err)
}

func TestGetLatestSessionID_ExcludesDeleted(t *testing.T) {
	_ = setupDB(t)

	s1, _ := service.CreateSession("/project", "claude", "First", "claude", "", "default", "chat")
	s2, _ := service.CreateSession("/project", "codebuddy", "Second", "codebuddy", "", "default", "chat")

	// Delete the most recent session
	service.ArchiveSession("/project", "codebuddy", s2)

	// Should return the remaining session
	id, backend, err := service.GetLatestSessionID("/project")
	assert.NoError(t, err)
	assert.Equal(t, s1, id)
	assert.Equal(t, "claude", backend)
}

func TestGetRunningSessionIDs_AfterClearAll(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	service.SetSessionRunning("sess-1", true)
	service.SetSessionRunning("sess-2", true)

	service.SetSessionRunning("sess-1", false)
	service.SetSessionRunning("sess-2", false)

	ids := service.GetRunningSessionIDs()
	assert.Empty(t, ids)
}

// ========== CreateSession: external_session_id initialization ==========

// TestCreateSession_ExternalSessionIDEmpty verifies that CreateSession
// initializes external_session_id to empty string. The real external ID is
// populated later via session_capture events from the AI backend.
func TestCreateSession_ExternalSessionIDEmpty(t *testing.T) {
	setupDB(t)

	for _, backend := range []string{"codebuddy", "claude", "opencode", "pi"} {
		t.Run(backend, func(t *testing.T) {
			sid, err := service.CreateSession("/project", backend, "Test "+backend, "", "", "default", "chat")
			assert.NoError(t, err)
			assert.NotEmpty(t, sid)

			extID := service.GetExternalSessionID(sid)
			assert.Equal(t, "", extID, "external_session_id should be empty for backend %s", backend)
		})
	}
}

// TestCreateSession_ExternalSessionIDPersistedInDB explicitly verifies
// that the INSERT statement in CreateSession writes external_session_id = ”.
func TestCreateSession_ExternalSessionIDPersistedInDB(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "DB Check", "", "", "default", "chat")
	assert.NoError(t, err)

	var extID string
	err = service.UnsafeDBForTest().QueryRow("SELECT external_session_id FROM chat_sessions WHERE id = ?", sid).Scan(&extID)
	assert.NoError(t, err)
	assert.Equal(t, "", extID, "external_session_id column should be empty in DB")
}

// ========== ArchiveSession: does NOT modify chat_history ==========

// TestArchiveSession_DoesNotModifyChatHistory verifies that ArchiveSession
// only archives the session record and does NOT touch chat_history
// at all. This is critical — the old code set chat_history.archived=1,
// which caused data loss when restoring sessions.
func TestArchiveSession_DoesNotModifyChatHistory(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")

	// Add messages
	msgID1, err := service.AddChatMessage("/project", "claude", sid, "user", "message 1", nil, false, "NewSession")
	assert.NoError(t, err)
	msgID2, err := service.AddChatMessage("/project", "claude", sid, "assistant", "response 1", nil, false, "")
	assert.NoError(t, err)

	// Verify messages exist before deletion
	var countBefore int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&countBefore)
	assert.NoError(t, err)
	assert.Equal(t, 2, countBefore)

	// Delete the session
	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Verify messages are still present with identical content
	var countAfter int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&countAfter)
	assert.NoError(t, err)
	assert.Equal(t, 2, countAfter, "chat_history rows should NOT be modified by ArchiveSession")

	// Verify individual message content is unchanged
	msg1, err := service.GetMessageByID(msgID1)
	assert.NoError(t, err)
	assert.Equal(t, "message 1", msg1.Content)

	msg2, err := service.GetMessageByID(msgID2)
	assert.NoError(t, err)
	assert.Equal(t, "response 1", msg2.Content)
}

// ========== Restoring a archived session preserves all chat history ==========

// TestRestoreDeletedSession_PreservesChatHistory verifies that when a
// archived session is restored (archived=0), all chat history messages
// are still accessible. This was a critical bug where restored sessions
// had no chat history because chat_history.deleted was set to 1 on delete.
func TestRestoreDeletedSession_PreservesChatHistory(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete & Restore")

	// Add messages before deletion
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "before delete", nil, false, "NewSession")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "before delete reply", nil, false, "")
	assert.NoError(t, err)

	// Delete the session
	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Verify GetChatHistory still returns messages (no deleted column in chat_history)
	msgsAfterDelete, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgsAfterDelete, 2, "messages should still exist after session archival")

	// Restore the session by setting archived=0
	_, err = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET archived = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", sid)
	assert.NoError(t, err)

	// Verify restored session can be found by GetSessionBackend
	backend := service.GetSessionBackend(sid)
	assert.Equal(t, "claude", backend, "restored session should be visible via GetSessionBackend")

	// Verify chat history is fully intact
	msgsAfterRestore, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgsAfterRestore, 2, "all messages should be preserved after restore")
	assert.Equal(t, "before delete", msgsAfterRestore[0].Content)
	assert.Equal(t, "before delete reply", msgsAfterRestore[1].Content)

	// Verify the session can accept new messages after restore
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "after restore", nil, false, "")
	assert.NoError(t, err)

	msgsFinal, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgsFinal, 3)
	assert.Equal(t, "after restore", msgsFinal[2].Content)
}

// ---------- GetSessionModel / UpdateSessionModel ----------

func TestGetSessionModel(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Test", "claude", "claude-sonnet-4-6", "default", "chat")
	assert.NoError(t, err)

	modelID := service.GetSessionModel(sid)
	assert.Equal(t, "claude-sonnet-4-6", modelID)
}

func TestGetSessionModel_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "No Model")
	modelID := service.GetSessionModel(sid)
	assert.Equal(t, "", modelID)
}

func TestGetSessionModel_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetSessionModel("non-existent"))
}

func TestGetSessionModel_DeletedSession(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Deleted", "claude", "gpt-4", "default", "chat")
	assert.NoError(t, err)

	err = service.ArchiveSession("/project", "claude", sid)
	assert.NoError(t, err)

	assert.Equal(t, "", service.GetSessionModel(sid))
}

func TestUpdateSessionModel(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Model Test")

	err := service.UpdateSessionModel(sid, "gpt-4o")
	assert.NoError(t, err)

	modelID := service.GetSessionModel(sid)
	assert.Equal(t, "gpt-4o", modelID)
}

func TestUpdateSessionModel_NonExistent(t *testing.T) {
	setupDB(t)

	err := service.UpdateSessionModel("non-existent", "gpt-4o")
	assert.NoError(t, err) // UPDATE on non-existent row is a no-op
}

// ---------- GetSessionAutoApprove / UpdateSessionAutoApprove ----------

func TestGetSessionAutoApprove_DefaultOff(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "AutoApprove Test")
	assert.False(t, service.GetSessionAutoApprove(sid))
}

func TestUpdateSessionAutoApprove_Enable(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "AutoApprove Enable")
	err := service.UpdateSessionAutoApprove(sid, true)
	assert.NoError(t, err)
	assert.True(t, service.GetSessionAutoApprove(sid))
}

func TestUpdateSessionAutoApprove_Disable(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "AutoApprove Disable")
	service.UpdateSessionAutoApprove(sid, true)
	service.UpdateSessionAutoApprove(sid, false)
	assert.False(t, service.GetSessionAutoApprove(sid))
}

// ---------- CreateSession initializes auto_approve from the agent default ----------

// TestCreateSession_AutoApproveFromAgentDefault verifies that creating a session
// with an agent configured AutoApprove=true persists auto_approve=1 in the DB at
// creation time (not just as in-memory frontend display state).
func TestCreateSession_AutoApproveFromAgentDefault(t *testing.T) {
	setupDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"auto-agent": {ID: "auto-agent", Backend: "claude", AutoApprove: true},
	}
	defer func() { model.Agents = origAgents }()

	sid, err := service.CreateSession("/project", "claude", "Auto Default", "auto-agent", "", "user", "chat")
	require.NoError(t, err)

	assert.True(t, service.GetSessionAutoApprove(sid),
		"session created with an auto-approve agent must be persisted with auto_approve=1")
}

// TestCreateSession_AutoApproveOffWhenAgentDefaultOff verifies the flag stays
// off for an agent that has not opted in.
func TestCreateSession_AutoApproveOffWhenAgentDefaultOff(t *testing.T) {
	setupDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"plain-agent": {ID: "plain-agent", Backend: "claude", AutoApprove: false},
	}
	defer func() { model.Agents = origAgents }()

	sid, err := service.CreateSession("/project", "claude", "Plain Default", "plain-agent", "", "user", "chat")
	require.NoError(t, err)

	assert.False(t, service.GetSessionAutoApprove(sid),
		"session with a non-auto-approve agent must default to auto_approve=0")
}

// TestCreateSession_AutoApproveUnknownAgent verifies an unknown/empty agent ID
// does not panic and leaves the flag off.
func TestCreateSession_AutoApproveUnknownAgent(t *testing.T) {
	setupDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	sid, err := service.CreateSession("/project", "claude", "Unknown Agent", "ghost-agent", "", "default", "chat")
	require.NoError(t, err)

	assert.False(t, service.GetSessionAutoApprove(sid))
}

// TestCreateSession_AutoApproveAppliesToScheduledSessions documents that the
// agent default also applies to scheduled-task sessions created via
// CreateSession. Harmless for both transports: ACP forces auto-approve anyway,
// CLI ignores the flag.
func TestCreateSession_AutoApproveAppliesToScheduledSessions(t *testing.T) {
	setupDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"auto-agent": {ID: "auto-agent", Backend: "claude", AutoApprove: true},
	}
	defer func() { model.Agents = origAgents }()

	sid, err := service.CreateSession("/project", "claude", "⏰ Scheduled", "auto-agent", "", "default", "scheduled")
	require.NoError(t, err)

	assert.True(t, service.GetSessionAutoApprove(sid))
}

// TestForkSession_InheritsAgentAutoApproveDefault verifies a forked session
// picks up the agent's auto-approve default, matching a freshly created session
// for the same agent.
func TestForkSession_InheritsAgentAutoApproveDefault(t *testing.T) {
	setupDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"auto-agent": {ID: "auto-agent", Backend: "claude", AutoApprove: true},
	}
	defer func() { model.Agents = origAgents }()

	src, err := service.CreateSession("/project", "claude", "Source", "auto-agent", "", "user", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", src, "user", "Hello", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", src, "assistant", "Hi", nil, false, "")
	require.NoError(t, err)

	forked, err := service.ForkSession(src, "/project", "[Fork] Source", 0, "")
	require.NoError(t, err)

	assert.True(t, service.GetSessionAutoApprove(forked),
		"forked session must inherit the agent's auto-approve default")
}

// ---------- GetStreamingMessageID ----------

func TestGetStreamingMessageID_Found(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream Test")

	// Add a streaming message then finalize it
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	_, err = service.FinalizeStreamingMessage("/project", "claude", sid, "final content")
	assert.NoError(t, err)

	id := service.GetStreamingMessageID(sid)
	assert.Greater(t, id, int64(0))
}

func TestGetStreamingMessageID_NoAssistantMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty")
	id := service.GetStreamingMessageID(sid)
	assert.Equal(t, int64(0), id)
}

func TestGetStreamingMessageID_PrefersStreaming(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream Pref")

	// Add a finalized message (streaming=0)
	finalizedID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "finalized", nil, true, "")
	assert.NoError(t, err)
	_, err = service.FinalizeStreamingMessage("/project", "claude", sid, "final content")
	assert.NoError(t, err)

	// Add a streaming message (streaming=1) — should be preferred
	streamingID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	id := service.GetStreamingMessageID(sid)
	assert.Equal(t, streamingID, id, "should prefer streaming=1 message over streaming=0")
	assert.NotEqual(t, finalizedID, id, "should NOT return the finalized message")
}

func TestGetStreamingMessageID_OnlyStreamingNoFinalized(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Only Stream")

	// Add only a streaming message, never finalize it
	streamingID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	id := service.GetStreamingMessageID(sid)
	assert.Equal(t, streamingID, id, "should return streaming=1 message even with no finalized messages")
}

func TestGetStreamingMessageID_SessionIsolation(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	// Session 1: add a streaming message
	s1StreamingID, err := service.AddChatMessage("/project", "claude", sid1, "assistant", "s1 streaming", nil, true, "")
	assert.NoError(t, err)

	// Session 2: add a different streaming message
	s2StreamingID, err := service.AddChatMessage("/project", "claude", sid2, "assistant", "s2 streaming", nil, true, "")
	assert.NoError(t, err)

	id1 := service.GetStreamingMessageID(sid1)
	id2 := service.GetStreamingMessageID(sid2)

	assert.Equal(t, s1StreamingID, id1, "session 1 should return its own streaming message")
	assert.Equal(t, s2StreamingID, id2, "session 2 should return its own streaming message")
	assert.NotEqual(t, id1, id2, "different sessions should not return each other's message IDs")
}

// ---------- GetStreamingMessageID ----------

func TestGetStreamingMessageID_LiveRow(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream Info")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "question", nil, false, "")
	assert.NoError(t, err)
	streamingID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "{}", nil, true, "")
	assert.NoError(t, err)

	assert.Equal(t, streamingID, service.GetStreamingMessageID(sid))
}

// ---------- GetUnindexedMessages / MarkMessageIndexed / UnindexedCount ----------

func TestGetUnindexedMessages_Empty(t *testing.T) {
	setupDB(t)

	msgs, err := service.GetUnindexedMessages(10)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestGetUnindexedMessages_WithMessages(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Index Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "world", nil, false, "")
	assert.NoError(t, err)

	msgs, err := service.GetUnindexedMessages(10)
	assert.NoError(t, err)
	assert.NotEmpty(t, msgs)
}

func TestMarkMessageIndexed(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Index Test")
	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)

	err = service.MarkMessageIndexed(msgID)
	assert.NoError(t, err)

	// Verify the message is now indexed
	var indexed int
	err = service.UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE id = ?", msgID).Scan(&indexed)
	assert.NoError(t, err)
	assert.Equal(t, 1, indexed)
}

func TestMarkMessagesIndexed(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Batch Index Test")
	msg1, _ := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	msg2, _ := service.AddChatMessage("/project", "claude", sid, "user", "world", nil, false, "")

	err := service.MarkMessagesIndexed([]int64{msg1, msg2})
	assert.NoError(t, err)

	// Verify both messages are indexed
	var count int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE id IN (?, ?) AND indexed = 1", msg1, msg2).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 2, count)

	// Empty slice should be no-op
	err = service.MarkMessagesIndexed(nil)
	assert.NoError(t, err)
}

func TestResetAllIndexed(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Reset Index Test")
	msg1, _ := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	msg2, _ := service.AddChatMessage("/project", "claude", sid, "user", "world", nil, false, "")

	// Mark both as indexed
	err := service.MarkMessagesIndexed([]int64{msg1, msg2})
	assert.NoError(t, err)

	// Verify both are indexed
	var indexedCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE id IN (?, ?) AND indexed = 1", msg1, msg2).Scan(&indexedCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, indexedCount)

	// Reset all indexed flags
	affected, err := service.ResetAllIndexed()
	assert.NoError(t, err)
	assert.Equal(t, int64(2), affected)

	// Verify both are now unindexed
	var unindexedCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE id IN (?, ?) AND indexed = 0", msg1, msg2).Scan(&unindexedCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, unindexedCount)
}

func TestUnindexedCount(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Count Test")

	// Initially 0
	count, err := service.UnindexedCount()
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Add messages (new messages default to indexed=0)
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)

	count, err = service.UnindexedCount()
	assert.NoError(t, err)
	assert.Greater(t, count, 0)
}

func TestTotalAndIndexedMessageCount(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Count Test")

	// Initially 0
	total, err := service.TotalMessageCount()
	assert.NoError(t, err)
	assert.Equal(t, 0, total)
	indexed, err := service.IndexedMessageCount()
	assert.NoError(t, err)
	assert.Equal(t, 0, indexed)

	// Add 3 messages
	for i := range 3 {
		id, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("msg %d", i), nil, false, "")
		assert.NoError(t, err)
		if i < 2 {
			assert.NoError(t, service.MarkMessageIndexed(id))
		}
	}

	total, err = service.TotalMessageCount()
	assert.NoError(t, err)
	assert.Equal(t, 3, total)
	indexed, err = service.IndexedMessageCount()
	assert.NoError(t, err)
	assert.Equal(t, 2, indexed)
}

// ---------- GetChatHistoryPaged (cursor-based) ----------

func TestGetChatHistoryPaged_LimitAndBeforeID(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged History")

	// Add 5 messages
	msgIDs := make([]int64, 0, 5)
	for i := range 5 {
		id, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("msg %d", i), nil, false, "")
		assert.NoError(t, err)
		msgIDs = append(msgIDs, id)
	}

	// Get last 2 messages with limit only (no cursor)
	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 2, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg 3", msgs[0].Content)
	assert.Equal(t, "msg 4", msgs[1].Content)

	// Get 2 messages before the last message (cursor-based)
	msgs, _, err = service.GetChatHistoryPaged("/project", "claude", sid, 2, int(msgIDs[4]))
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg 2", msgs[0].Content)
	assert.Equal(t, "msg 3", msgs[1].Content)
}

// ---------- GetSessionProjectPath ----------

func TestGetSessionProjectPath(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/my/project", "claude", "Test")

	path := service.GetSessionProjectPath(sid)
	assert.Equal(t, "/my/project", path)
}

func TestGetSessionProjectPath_NonExistent(t *testing.T) {
	setupDB(t)

	path := service.GetSessionProjectPath("non-existent")
	assert.Equal(t, "", path)
}

func TestGetSessionProjectPath_Archived(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/my/project", "claude", "Test")
	_ = service.ArchiveSession("/my/project", "claude", sid)

	path := service.GetSessionProjectPath(sid)
	assert.Equal(t, "", path, "archived session should return empty project path")
}

// ---------- GetLatestUserModel ----------

func TestGetLatestUserModel_Found(t *testing.T) {
	setupDB(t)

	_, err := service.CreateSession("/project", "claude", "Test", "claude", "gpt-4o", "user", "chat")
	assert.NoError(t, err)

	modelID := service.GetLatestUserModel("claude", "/project")
	assert.Equal(t, "gpt-4o", modelID)
}

func TestGetLatestUserModel_WithThinkingEffort(t *testing.T) {
	setupDB(t)

	_, err := service.CreateSession("/project", "claude", "Test", "claude", "gpt-4o", "user", "chat")
	assert.NoError(t, err)

	// ThinkingEffort is no longer persisted to DB; only model is returned
	modelID := service.GetLatestUserModel("claude", "/project")
	assert.Equal(t, "gpt-4o", modelID)
}

func TestGetLatestUserModel_NotFound(t *testing.T) {
	setupDB(t)

	modelID := service.GetLatestUserModel("claude", "/project")
	assert.Equal(t, "", modelID)
}

// ---------- GetChatHistoryPaged: all branches ----------

func TestGetChatHistoryPaged_NoLimit(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged All")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "msg1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "msg2", nil, false, "")
	assert.NoError(t, err)

	// limit=0 returns all messages
	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
}

func TestGetChatHistoryPaged_LimitOnly(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged Limit")

	for i := range 5 {
		_, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("msg %d", i), nil, false, "")
		assert.NoError(t, err)
	}

	// limit=3, no cursor — should return the 3 most recent
	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 3, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 3)
	assert.Equal(t, "msg 2", msgs[0].Content) // oldest of the 3
	assert.Equal(t, "msg 4", msgs[2].Content) // newest
}

func TestGetChatHistoryPaged_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty Paged")

	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 10, 0)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

// TestGetChatHistoryPaged_ExcludesQueuedMessages verifies the core invariant of
// the dedicated queued_messages table: a message that is still queued has NO
// chat_history row, so it is absent from the paged history and from the total.
// This is what makes DB id order equal conversational order once it drains.
func TestGetChatHistoryPaged_ExcludesQueuedMessages(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged Queue")
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "normal", nil, false, "")
	assert.NoError(t, err)

	// A still-queued message lives only in queued_messages.
	_, err = service.AddQueuedMessage("/project", "claude", sid, "queued msg", nil, "pending-abc", "")
	require.NoError(t, err)

	msgs, total, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1, "a queued message must not appear in chat_history")
	assert.Equal(t, "normal", msgs[0].Content)
	assert.Equal(t, 1, total, "the total counts conversation history only")

	// It is still visible through the queue API.
	queue, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	require.Len(t, queue, 1)
	assert.Equal(t, "pending-abc", queue[0].QueueID)
	assert.Equal(t, "queued msg", queue[0].Text)
}

// TestAddQueuedMessage_Basic verifies that AddQueuedMessage inserts a row into
// queued_messages (not chat_history) and returns its row id.
func TestAddQueuedMessage_Basic(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Basic")

	id, err := service.AddQueuedMessage("/project", "claude", sid, "hello", nil, "pending-1", "")
	assert.NoError(t, err)
	assert.Greater(t, id, int64(0))

	var queueID, content string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT queue_id, content FROM queued_messages WHERE id = ?", id,
	).Scan(&queueID, &content)
	assert.NoError(t, err)
	assert.Equal(t, "pending-1", queueID)
	assert.Equal(t, "hello", content)

	// Nothing was written to chat_history at enqueue time.
	var historyRows int
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid,
	).Scan(&historyRows))
	assert.Zero(t, historyRows, "enqueue must not materialize a chat_history row")
}

// TestAddQueuedMessage_SetsSessionTitleOnFirstMessage verifies B3: the first
// user message updates the session title (reusing AddChatMessage's logic).
func TestAddQueuedMessage_SetsSessionTitleOnFirstMessage(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Initial")

	id, err := service.AddQueuedMessage("/project", "claude", sid, "help me fix the build", nil, "pending-1", "")
	assert.NoError(t, err)
	assert.Greater(t, id, int64(0))

	var title string
	err = service.UnsafeDBForTest().QueryRow("SELECT title FROM chat_sessions WHERE id = ?", sid).Scan(&title)
	assert.NoError(t, err)
	assert.Equal(t, "help me fix the build", title, "first user message should update session title")
}

// TestAddQueuedMessage_WithFiles verifies file attachment persistence on the
// queued row (the files JSON round-trips through queued_messages).
func TestAddQueuedMessage_WithFiles(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Files")

	files := []model.FileEntry{{Path: "/src/a.go", IsDir: false}}
	id, err := service.AddQueuedMessage("/project", "claude", sid, "", files, "pending-2", "")
	assert.NoError(t, err)
	assert.Greater(t, id, int64(0))

	msgs, err := service.GetQueuedMessages(sid)
	assert.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Files, 1)
	assert.Equal(t, "/src/a.go", msgs[0].Files[0].Path)
}

// TestAddQueuedMessage_EmptyQueueID verifies auto-generated queue_id when none provided.
func TestAddQueuedMessage_EmptyQueueID(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue AutoID")

	id, err := service.AddQueuedMessage("/project", "claude", sid, "msg", nil, "", "")
	assert.NoError(t, err)

	var queueID string
	err = service.UnsafeDBForTest().QueryRow("SELECT queue_id FROM queued_messages WHERE id = ?", id).Scan(&queueID)
	assert.NoError(t, err)
	assert.NotEmpty(t, queueID, "auto-generated queue_id should not be empty")
}

// TestAddQueuedMessage_TitleFailureDoesNotFailEnqueue verifies that the
// auto-title step is best-effort on the enqueue path: the message is still
// queued even when the title update cannot run. The enqueue is a single INSERT
// into queued_messages (no chat_history row, no shared transaction), so a title
// failure must not roll the message back — losing a queued message is far worse
// than a session keeping its placeholder title.
func TestAddQueuedMessage_TitleFailureDoesNotFailEnqueue(t *testing.T) {
	db := setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Title Fail")

	// Drop chat_sessions so applyAutoTitle's UPDATE fails while the queued_messages
	// INSERT still succeeds.
	_, err := db.Exec("DROP TABLE chat_sessions")
	require.NoError(t, err)

	id, err := service.AddQueuedMessage("/project", "claude", sid, "still queued", nil, "q-titlefail", "")
	assert.NoError(t, err, "a title failure must not fail the enqueue")
	assert.Greater(t, id, int64(0))

	var content string
	require.NoError(t, db.QueryRow("SELECT content FROM queued_messages WHERE id = ?", id).Scan(&content))
	assert.Equal(t, "still queued", content)
}

// TestClaimNextAndMaterialize_FIFO verifies messages are claimed in insertion
// order and materialized into chat_history with increasing ids.
func TestClaimNextAndMaterialize_FIFO(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue FIFO")

	id1, _ := service.AddQueuedMessage("/project", "claude", sid, "first", nil, "q-1", "")
	id2, _ := service.AddQueuedMessage("/project", "claude", sid, "second", nil, "q-2", "")
	assert.Less(t, id1, id2)

	row, msgID, ok, err := service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "first", row.Content)
	assert.Equal(t, "q-1", row.QueueID)
	assert.Greater(t, msgID, int64(0))

	row, _, ok, err = service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "second", row.Content)
	assert.Equal(t, "q-2", row.QueueID)

	// Both are now real chat_history rows in claim order.
	msgs, err := service.GetChatHistory("/project", "claude", sid)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "first", msgs[0].Content)
	assert.Equal(t, "second", msgs[1].Content)
}

// TestClaimNextAndMaterialize_Empty verifies an empty queue returns ok=false
// with no error.
func TestClaimNextAndMaterialize_Empty(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Empty")

	row, msgID, ok, err := service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, int64(0), row.ID)
	assert.Equal(t, int64(0), msgID)
}

// TestClaimNextAndMaterialize_DeletesQueueRow verifies the claim moves the
// message out of queued_messages (the row is truly gone) and into chat_history,
// so a second claim finds nothing.
func TestClaimNextAndMaterialize_DeletesQueueRow(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Consumed")

	_, _ = service.AddQueuedMessage("/project", "claude", sid, "msg", nil, "q-1", "")
	_, msgID, ok, err := service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.True(t, ok)

	assert.Equal(t, 0, service.GetQueuedCount(sid), "claimed row must leave the queue")
	var historyRows int
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE id = ?", msgID,
	).Scan(&historyRows))
	assert.Equal(t, 1, historyRows, "claimed row must be materialized into chat_history")

	// Second claim finds nothing (the row is no longer queued).
	_, _, ok, err = service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// TestClaimNextAndMaterialize_WithFiles verifies file entries survive the claim.
func TestClaimNextAndMaterialize_WithFiles(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Files2")

	files := []model.FileEntry{{Path: "/src/b.go", IsDir: false}}
	_, _ = service.AddQueuedMessage("/project", "claude", sid, "with files", files, "q-1", "")

	row, _, ok, err := service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, row.Files, 1)
	assert.Equal(t, "/src/b.go", row.Files[0].Path)
}

// TestGetChatHistoryPaged_TotalExcludesQueued verifies the total returned by
// GetChatHistoryPaged counts only conversation history, so hasMore arithmetic
// no longer has to subtract a queued count.
func TestGetChatHistoryPaged_TotalExcludesQueued(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Paged QueuedCount")

	// 2 normal messages + 2 queued messages.
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "normal1", nil, false, "")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "normal2", nil, false, "")
	_, _ = service.AddQueuedMessage("/project", "claude", sid, "queued1", nil, "q-1", "")
	_, _ = service.AddQueuedMessage("/project", "claude", sid, "queued2", nil, "q-2", "")

	msgs, total, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2, "only history rows are returned")
	assert.Equal(t, 2, total, "total excludes queued rows")
}

// TestGetChatHistoryPaged_HasMoreExcludesQueued reproduces the scenario the old
// dual-count formula handled: 50 history messages + 15 queued. The 15 queued
// messages are invisible to history, so an initial load of 40 returns 40 history
// rows and hasMore is simply loaded(40) < total(50).
func TestGetChatHistoryPaged_HasMoreExcludesQueued(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Paged HasMore")

	// 50 normal history messages.
	for i := range 50 {
		_, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("hist-%d", i), nil, false, "")
		assert.NoError(t, err)
	}
	// 15 queued messages appended last (newest).
	for i := range 15 {
		_, err := service.AddQueuedMessage("/project", "claude", sid, fmt.Sprintf("queued-%d", i), nil, fmt.Sprintf("q-%d", i), "")
		assert.NoError(t, err)
	}

	msgs, total, err := service.GetChatHistoryPaged("/project", "claude", sid, 40, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 40, "initial load returns newest 40 history rows")
	assert.Equal(t, 50, total, "total counts history only; queued rows are not history")
	assert.True(t, len(msgs) < total, "hasMore must be true: 40 < 50")
}

// TestClaimNextAndMaterialize_Atomic_NoDoubleConsume verifies two goroutines
// claiming concurrently never consume the same row (DELETE + INSERT in one
// transaction under the write mutex).
func TestClaimNextAndMaterialize_Atomic_NoDoubleConsume(t *testing.T) {
	db := setupDB(t)
	// Single connection so the :memory: database is shared across goroutines.
	db.SetMaxOpenConns(1)
	sid := helperCreateSession(t, "/project", "claude", "Queue Atomic")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "only-msg", nil, "q-1", "")
	assert.NoError(t, err)

	results := make([]bool, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, ok, derr := service.ClaimNextAndMaterialize(sid)
			assert.NoError(t, derr)
			results[i] = ok
		}(i)
	}
	wg.Wait()

	consumed := 0
	for _, ok := range results {
		if ok {
			consumed++
		}
	}
	assert.Equal(t, 1, consumed, "exactly one goroutine may claim the row")

	// Second claim finds nothing.
	_, _, ok, err := service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// TestClaimNextAndMaterialize_DBError_NotEmptyQueue verifies a real DB error
// (distinct from an empty queue) is surfaced to the caller so the drain loop
// retries instead of treating it as "queue empty" and silently dropping the
// message (B4). A closed DB yields such an error.
func TestClaimNextAndMaterialize_DBError_NotEmptyQueue(t *testing.T) {
	db := setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue DBError")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "persisted msg", nil, "q-1", "")
	assert.NoError(t, err)

	// Close the DB underneath — the next claim must return a real error,
	// NOT (false, nil) which the drain loop would treat as "empty".
	require.NoError(t, db.Close())

	_, _, ok, derr := service.ClaimNextAndMaterialize(sid)
	assert.False(t, ok, "must not report a successful claim")
	assert.Error(t, derr, "a real DB error must be surfaced, not swallowed as empty")
}

// TestQueuedMessage_PersistsAcrossRestart verifies a queued message survives a
// simulated restart (fresh DB handle over the same file): the row is still in
// queued_messages and discoverable via GetQueuedMessages.
func TestQueuedMessage_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(schema)
	require.NoError(t, err)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})

	sid := helperCreateSession(t, "/project", "claude", "Queue Restart")

	_, err = service.AddQueuedMessage("/project", "claude", sid, "before restart", nil, "q-restart", "")
	assert.NoError(t, err)

	// Simulate restart: close the DB and reopen the same file.
	service.SetDBForTest(nil, nil)
	require.NoError(t, db.Close())

	db2, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	db2.SetMaxOpenConns(1)
	cleanup2 := service.SetDBForTest(db2, db2)
	t.Cleanup(func() {
		cleanup2()
		db2.Close()
	})

	msgs, err := service.GetQueuedMessages(sid)
	assert.NoError(t, err)
	require.Len(t, msgs, 1, "queued message must survive restart")
	assert.Equal(t, "before restart", msgs[0].Text)
	assert.Equal(t, "q-restart", msgs[0].QueueID)
}

// TestQueuedMessage_DrainedAfterRestart verifies a queued message left over
// from before a restart can be claimed by the drain loop afterwards.
func TestQueuedMessage_DrainedAfterRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(schema)
	require.NoError(t, err)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})

	sid := helperCreateSession(t, "/project", "claude", "Queue Restart Drain")

	_, err = service.AddQueuedMessage("/project", "claude", sid, "stale queued", nil, "q-stale", "")
	assert.NoError(t, err)

	// Simulate restart.
	service.SetDBForTest(nil, nil)
	require.NoError(t, db.Close())

	db2, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	db2.SetMaxOpenConns(1)
	cleanup2 := service.SetDBForTest(db2, db2)
	t.Cleanup(func() {
		cleanup2()
		db2.Close()
	})

	// Drain the leftover message after restart.
	row, msgID, ok, err := service.ClaimNextAndMaterialize(sid)
	assert.NoError(t, err)
	assert.True(t, ok, "leftover queued message must be claimable after restart")
	assert.Equal(t, "stale queued", row.Content)

	// It is now a chat_history row, not a queued row.
	var content string
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT content FROM chat_history WHERE id = ?", msgID,
	).Scan(&content))
	assert.Equal(t, "stale queued", content)
	assert.Equal(t, 0, service.GetQueuedCount(sid))
}

// ---------- CreateSession: session_type default ----------

func TestCreateSession_EmptySessionTypeDefaultsToChat(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Default Type", "", "", "default", "")
	assert.NoError(t, err)

	var sessionType string
	err = service.UnsafeDBForTest().QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sid).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "chat", sessionType)
}

// ---------- GetConversationIndex ----------

func TestGetConversationIndex_Basic(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Index Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "First question", nil, false, "NewSession")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", `{"blocks":[{"type":"text","text":"Answer"}]}`, nil, false, "NewSession")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "Second question", []model.FileEntry{{Path: "file1.go"}}, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 3)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "First question", msgs[0].Content)
	assert.Nil(t, msgs[0].Summary)

	// Assistant row: content is emptied, the fallback preview lands in Summary.
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Empty(t, msgs[1].Content)
	require.NotNil(t, msgs[1].Summary)
	assert.Equal(t, "Answer", *msgs[1].Summary)

	assert.Equal(t, "user", msgs[2].Role)
	assert.Equal(t, "Second question", msgs[2].Content)
	assert.Equal(t, []model.FileEntry{{Path: "file1.go"}}, msgs[2].Files)
}

func TestGetConversationIndex_PrefersStoredSummary(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stored Summary")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", `{"blocks":[{"type":"text","text":"raw reply text"}]}`, nil, false, "NewSession")
	require.NoError(t, err)

	// Give it a reading summary — that must win over the reply text.
	var asstID int64
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant'", sid,
	).Scan(&asstID))
	require.NoError(t, service.SaveSummary("chat_message", asstID, "stored summary"))

	msgs, err := service.GetConversationIndex(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].Summary)
	assert.Equal(t, "stored summary", *msgs[0].Summary)
	assert.Empty(t, msgs[0].Content, "assistant content must not cross the wire")
}

func TestGetConversationIndex_AssistantFallbackUsesLastAnswer(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Last Answer")

	// The reply has tool noise plus a final answer — the index must show the
	// conclusion, not the intermediate commentary.
	content := `{"blocks":[` +
		`{"type":"text","text":"Let me check that."},` +
		`{"type":"tool_use","name":"Read","id":"t1"},` +
		`{"type":"text","text":"The build passes."}` +
		`]}`
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", content, nil, false, "NewSession")
	require.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].Summary)
	assert.Equal(t, "The build passes.", *msgs[0].Summary)
}

func TestGetConversationIndex_AssistantFallbackTruncates(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Truncate")

	long := strings.Repeat("测", indexSummaryMaxRunesForTest+50)
	content := `{"blocks":[{"type":"text","text":"` + long + `"}]}`
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", content, nil, false, "NewSession")
	require.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].Summary)
	assert.Equal(t, indexSummaryMaxRunesForTest+1, utf8.RuneCountInString(*msgs[0].Summary)) // + ellipsis
	assert.True(t, strings.HasSuffix(*msgs[0].Summary, "…"))
}

// indexSummaryMaxRunesForTest mirrors the unexported cap in chat.go so the
// assertion above states the bound explicitly instead of restating the constant.
const indexSummaryMaxRunesForTest = 200

func TestGetConversationIndex_AssistantNoText(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "No Text")

	// A tool-call-only turn has no answer text — the row exists but is empty.
	content := `{"blocks":[{"type":"tool_use","name":"Bash","id":"t1"}]}`
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", content, nil, false, "NewSession")
	require.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].Summary, "an assistant row must always carry a (possibly empty) summary")
	assert.Empty(t, *msgs[0].Summary)
}

func TestGetConversationIndex_AssistantPlainTextContent(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Plain Text")

	// Non-block assistant content (no {"blocks":...}) must still produce a row.
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "plain answer", nil, false, "NewSession")
	require.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].Summary)
	assert.Equal(t, "plain answer", *msgs[0].Summary)
}

func TestGetConversationIndex_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty Index")

	msgs, err := service.GetConversationIndex(sid)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestGetConversationIndex_OnlyAssistantMessages(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "No User Msgs")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "Hello", nil, false, "NewSession")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "World", nil, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "assistant", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestGetConversationIndex_SkipsStreaming(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Streaming Test")

	// Add a streaming user message (streaming=1) — should be excluded
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Streaming msg", nil, true, "NewSession")
	assert.NoError(t, err)
	// Add a non-streaming user message — should be included
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "Done msg", nil, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "Done msg", msgs[0].Content)
}

// TestGetConversationIndex_ExcludesQueued verifies a still-queued message is
// absent from the conversation index: it has no chat_history row yet, so it
// cannot be summarized or listed until it is drained.
func TestGetConversationIndex_ExcludesQueued(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Queued Test")

	// A still-queued message lives only in queued_messages.
	_, err := service.AddQueuedMessage("/project", "claude", sid, "Pending question", nil, "q-pending", "")
	require.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "Real question", nil, false, "NewSession")
	require.NoError(t, err)

	msgs, err := service.GetConversationIndex(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Real question", msgs[0].Content)
}

func TestGetConversationIndex_OrderPreserved(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Order Test")

	for i := range 5 {
		_, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("Question %d", i+1), nil, false, "NewSession")
		assert.NoError(t, err)
		_, err = service.AddChatMessage("/project", "claude", sid, "assistant", fmt.Sprintf("Answer %d", i+1), nil, false, "NewSession")
		assert.NoError(t, err)
	}

	msgs, err := service.GetConversationIndex(sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 10)
	for i := range 5 {
		assert.Equal(t, "user", msgs[i*2].Role)
		assert.Equal(t, fmt.Sprintf("Question %d", i+1), msgs[i*2].Content)
		assert.Equal(t, "assistant", msgs[i*2+1].Role)
		require.NotNil(t, msgs[i*2+1].Summary)
		assert.Equal(t, fmt.Sprintf("Answer %d", i+1), *msgs[i*2+1].Summary)
	}
}

// ---------- GetMessageContent ----------

func TestGetMessageContent_NormalLookup(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")
	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "Hello world", nil, false, "")
	assert.NoError(t, err)

	content, err := service.GetMessageContent(msgID, sid)
	assert.NoError(t, err)
	assert.Equal(t, "Hello world", content)
}

func TestGetMessageContent_BlockJSONExtracted(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")
	// Add a message with block-format JSON content
	blockJSON := `{"blocks":[{"type":"text","text":"Plain text here"}]}`
	msgID, err := service.AddChatMessage("/project", "claude", sid, "assistant", blockJSON, nil, false, "")
	assert.NoError(t, err)

	content, err := service.GetMessageContent(msgID, sid)
	assert.NoError(t, err)
	assert.Equal(t, "Plain text here", content)
}

func TestGetMessageContent_CrossSessionReturnsEmpty(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")
	msgID, err := service.AddChatMessage("/project", "claude", sid1, "user", "Secret content", nil, false, "")
	assert.NoError(t, err)

	// Query with wrong session — should return empty string, not the content
	content, err := service.GetMessageContent(msgID, sid2)
	assert.NoError(t, err)
	assert.Equal(t, "", content)
}

func TestGetMessageContent_NonExistentID(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	content, err := service.GetMessageContent(99999, sid)
	assert.NoError(t, err)
	assert.Equal(t, "", content)
}

// --- ContextState persistence tests ---

func TestSaveAndGetContextState(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Initially, no context state
	state := service.GetContextState(sid)
	assert.Nil(t, state)

	// Save a full context state
	service.SaveContextState(sid, &service.ContextState{
		Mode: &service.ModeStatePersist{
			CurrentModeID:  "code",
			AvailableModes: []service.ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
		},
		ThinkingEffort: &service.ThinkingEffortPersist{
			CurrentID:       "high",
			AvailableLevels: []service.ThinkingEffortDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}},
		},
		Usage: &service.UsageStatePersist{
			Used: 50000, Size: 200000,
			InputTokens: 100, OutputTokens: 200,
			Cost: 0.5, Currency: "USD",
		},
	})

	state = service.GetContextState(sid)
	assert.NotNil(t, state)
	assert.Equal(t, "code", state.Mode.CurrentModeID)
	assert.Len(t, state.Mode.AvailableModes, 2)
	assert.Equal(t, "high", state.ThinkingEffort.CurrentID)
	assert.Len(t, state.ThinkingEffort.AvailableLevels, 2)
	assert.Equal(t, 50000, state.Usage.Used)
	assert.Equal(t, 200000, state.Usage.Size)
	assert.Equal(t, 0.5, state.Usage.Cost)
	assert.Equal(t, "USD", state.Usage.Currency)
}

func TestSaveContextState_PartialUpdate(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Save mode first
	service.SaveContextState(sid, &service.ContextState{
		Mode: &service.ModeStatePersist{
			CurrentModeID:  "ask",
			AvailableModes: []service.ModeDef{{ID: "ask", Name: "Ask"}},
		},
	})

	// Then save usage only — mode should survive because the handler
	// reads existing state before partial update (not tested here, but
	// we verify that a fresh SaveContextState replaces the whole JSON)
	service.SaveContextState(sid, &service.ContextState{
		Usage: &service.UsageStatePersist{Used: 1000, Size: 5000},
	})

	state := service.GetContextState(sid)
	assert.NotNil(t, state)
	// Mode was overwritten because SaveContextState replaces the whole column
	assert.Nil(t, state.Mode) // this is expected — partial update happens at handler level
	assert.NotNil(t, state.Usage)
	assert.Equal(t, 1000, state.Usage.Used)
}

func TestSaveContextState_NilState(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Save nil — should not write
	service.SaveContextState(sid, nil)
	state := service.GetContextState(sid)
	assert.Nil(t, state)
}

func TestPatchContextStateMerge_ModeOnly(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Patch only mode — should create fresh JSON
	modeJSON, _ := json.Marshal(service.ModeStatePersist{
		CurrentModeID:  "code",
		AvailableModes: []service.ModeDef{{ID: "code", Name: "Code"}},
	})
	service.PatchContextStateMerge(sid, map[string]string{"mode": string(modeJSON)})

	state := service.GetContextState(sid)
	assert.NotNil(t, state)
	assert.Equal(t, "code", state.Mode.CurrentModeID)
	assert.Nil(t, state.ThinkingEffort) // not set yet
	assert.Nil(t, state.Usage)          // not set yet
}

func TestPatchContextStateMerge_MergeDoesNotOverwrite(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// First: save mode
	modeJSON, _ := json.Marshal(service.ModeStatePersist{
		CurrentModeID:  "code",
		AvailableModes: []service.ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
	})
	service.PatchContextStateMerge(sid, map[string]string{"mode": string(modeJSON)})

	// Second: patch usage only — mode should survive
	usageJSON, _ := json.Marshal(service.UsageStatePersist{
		Used: 50000, Size: 200000,
	})
	service.PatchContextStateMerge(sid, map[string]string{"usage": string(usageJSON)})

	state := service.GetContextState(sid)
	assert.NotNil(t, state)
	// Mode survived!
	assert.Equal(t, "code", state.Mode.CurrentModeID)
	assert.Len(t, state.Mode.AvailableModes, 2)
	// Usage was added
	assert.Equal(t, 50000, state.Usage.Used)
	assert.Equal(t, 200000, state.Usage.Size)
}

func TestPatchContextStateMerge_UpdateCurrentModeOnly(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// First: save full mode state
	modeJSON, _ := json.Marshal(service.ModeStatePersist{
		CurrentModeID:  "code",
		AvailableModes: []service.ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
	})
	service.PatchContextStateMerge(sid, map[string]string{"mode": string(modeJSON)})

	// Second: patch only currentModeId (user switches mode via PATCH API)
	patchJSON, _ := json.Marshal(service.ModeStatePersist{CurrentModeID: "ask"})
	service.PatchContextStateMerge(sid, map[string]string{"mode": string(patchJSON)})

	state := service.GetContextState(sid)
	assert.Equal(t, "ask", state.Mode.CurrentModeID)
	// AvailableModes were overwritten with nil since patchJSON has omitempty
	// This is expected — the ACP agent will re-emit availableModes in next event
}

func TestPatchContextStateMerge_EmptySessionID(t *testing.T) {
	setupDB(t)

	// Should not crash with empty sessionID
	modeJSON, _ := json.Marshal(service.ModeStatePersist{CurrentModeID: "code"})
	service.PatchContextStateMerge("", map[string]string{"mode": string(modeJSON)})
}

func TestGetContextState_MalformedJSON(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Directly write malformed JSON
	_, _ = service.WriteExec("UPDATE chat_sessions SET context_state = '{invalid json' WHERE id = ?", sid)

	// Should return nil and not crash
	state := service.GetContextState(sid)
	assert.Nil(t, state)
}

func TestPatchContextStateMerge_EmptyPatches(t *testing.T) {
	setupDB(t)

	// Should not crash with empty patches
	service.PatchContextStateMerge("some-session", map[string]string{})
}

func TestGetContextState_DeletedSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	service.SaveContextState(sid, &service.ContextState{
		Usage: &service.UsageStatePersist{Used: 100, Size: 500},
	})

	// Delete session
	service.ArchiveSession("/project", "claude", sid)

	state := service.GetContextState(sid)
	assert.Nil(t, state)
}

func TestGetContextState_EmptyColumn(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Fresh session has empty context_state
	state := service.GetContextState(sid)
	assert.Nil(t, state)
}

func TestPersistContextStateFromEvent_ModeUpdate(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Initially empty
	state := service.GetContextState(sid)
	assert.Nil(t, state)

	// Persist mode_update event
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{
		Type: "mode_update",
		Mode: &ai.ModeState{
			CurrentModeID:  "code",
			AvailableModes: []ai.ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
		},
	})

	state = service.GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Mode)
	assert.Equal(t, "code", state.Mode.CurrentModeID)
	assert.Equal(t, 2, len(state.Mode.AvailableModes))
	assert.Equal(t, "code", state.Mode.AvailableModes[0].ID)
	assert.Equal(t, "Ask", state.Mode.AvailableModes[1].Name)
}

func TestPersistContextStateFromEvent_UsageUpdate(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Persist usage_update event
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{
		Type: "usage_update",
		Usage: &ai.UsageState{
			Used:         50000,
			Size:         200000,
			InputTokens:  4530,
			OutputTokens: 1441,
			Cost:         0.87,
			Currency:     "USD",
		},
	})

	state := service.GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 50000, state.Usage.Used)
	assert.Equal(t, 200000, state.Usage.Size)
	assert.Equal(t, 4530, state.Usage.InputTokens)
	assert.Equal(t, 1441, state.Usage.OutputTokens)
	assert.Equal(t, 0.87, state.Usage.Cost)
	assert.Equal(t, "USD", state.Usage.Currency)
}

func TestPersistContextStateFromEvent_MergesDifferentTypes(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Persist mode first
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{
		Type: "mode_update",
		Mode: &ai.ModeState{CurrentModeID: "code"},
	})

	// Then persist usage — should not overwrite mode
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{
		Type:  "usage_update",
		Usage: &ai.UsageState{Used: 30000, Size: 100000},
	})

	// Then persist thinking effort — should not overwrite mode or usage
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{
		Type:           "thinking_effort_update",
		ThinkingEffort: &ai.ThinkingEffortState{CurrentID: "high"},
	})

	state := service.GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Mode)
	assert.Equal(t, "code", state.Mode.CurrentModeID)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 30000, state.Usage.Used)
	assert.Equal(t, 100000, state.Usage.Size)
	require.NotNil(t, state.ThinkingEffort)
	assert.Equal(t, "high", state.ThinkingEffort.CurrentID)
}

func TestPersistContextStateFromEvent_NilPayload(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Nil payloads should be skipped without writing anything
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{Type: "mode_update", Mode: nil})
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{Type: "usage_update", Usage: nil})
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{Type: "thinking_effort_update", ThinkingEffort: nil})

	state := service.GetContextState(sid)
	assert.Nil(t, state)
}

func TestPersistContextStateFromEvent_EmptySessionID(t *testing.T) {
	// Should not crash with empty session ID
	service.PersistContextStateFromEvent("", ai.StreamEvent{
		Type:  "usage_update",
		Usage: &ai.UsageState{Used: 100, Size: 200},
	})
}

func TestPersistContextStateFromEvent_IgnoresOtherEventTypes(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// Non-context event types should not write anything
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{Type: "content", Content: "hello"})
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{Type: "metadata"})
	service.PersistContextStateFromEvent(sid, ai.StreamEvent{Type: "done"})

	state := service.GetContextState(sid)
	assert.Nil(t, state)
}

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

// ---------- GetChatHistoryPaged: summary content stripping ----------

func TestGetChatHistoryPaged_SummaryStripsContent(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Summary View")

	asstID, err := service.AddChatMessage("/project", "claude", sid, "assistant",
		`{"blocks":[{"type":"text","text":"long assistant answer"}]}`, nil, false, "")
	assert.NoError(t, err)

	cards := &model.SummaryCards{
		Tools: []model.SummaryTool{{Name: "Bash", ID: "tool_bash"}},
	}
	assert.NoError(t, service.SaveSummaryWithCards("chat_message", asstID, "reading summary", cards))

	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "assistant", msgs[0].Role)
	assert.Equal(t, `{"blocks":[]}`, msgs[0].Content, "summarized messages must strip blocks")
	require.NotNil(t, msgs[0].Summary)
	assert.Equal(t, "reading summary", *msgs[0].Summary)
	require.NotNil(t, msgs[0].SummaryCards)
	require.Len(t, msgs[0].SummaryCards.Tools, 1)
	assert.Equal(t, "Bash", msgs[0].SummaryCards.Tools[0].Name)
}

func TestGetChatHistoryPaged_SummaryPreservesMetadata(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Summary View Metadata")

	asstID, err := service.AddChatMessage("/project", "claude", sid, "assistant",
		`{"blocks":[{"type":"text","text":"answer"}],"metadata":{"transport":"cli","model":"glm-5.1","inputTokens":54494,"outputTokens":16,"durationMs":2965,"wallMs":7890,"sessionId":"ext-123"},"cancelled":true}`, nil, false, "")
	assert.NoError(t, err)
	assert.NoError(t, service.SaveSummaryWithCards("chat_message", asstID, "reading summary", nil))

	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	require.Len(t, msgs, 1)

	var parsed struct {
		Blocks    []any          `json:"blocks"`
		Metadata  map[string]any `json:"metadata"`
		Cancelled bool           `json:"cancelled"`
	}
	assert.NoError(t, json.Unmarshal([]byte(msgs[0].Content), &parsed))
	assert.Empty(t, parsed.Blocks, "blocks must be stripped for summarized messages")
	assert.True(t, parsed.Cancelled, "cancelled flag must be preserved")

	require.NotNil(t, parsed.Metadata, "metadata must be preserved for summarized messages")
	assert.Equal(t, "cli", parsed.Metadata["transport"])
	assert.Equal(t, "glm-5.1", parsed.Metadata["model"])
	assert.Equal(t, float64(54494), parsed.Metadata["inputTokens"])
	assert.Equal(t, float64(16), parsed.Metadata["outputTokens"])
	assert.Equal(t, float64(7890), parsed.Metadata["wallMs"])
	assert.Equal(t, "ext-123", parsed.Metadata["sessionId"])
}

func TestGetChatHistoryPaged_SummaryKeepsStreamingContent(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Summary View Streaming")

	asstID, err := service.AddChatMessage("/project", "claude", sid, "assistant",
		`{"blocks":[{"type":"text","text":"in-flight streaming answer"}]}`, nil, true, "")
	assert.NoError(t, err)

	cards := &model.SummaryCards{TaskIDs: []int64{7}}
	assert.NoError(t, service.SaveSummaryWithCards("chat_message", asstID, "reading summary", cards))

	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].Streaming)
	assert.NotEqual(t, "", msgs[0].Content, "streaming messages must keep content even when summarized")
	assert.NotNil(t, msgs[0].Summary)
}

func TestGetChatHistoryPaged_SummaryKeepsEmptySummaryContent(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Summary View Empty Summary")

	asstID, err := service.AddChatMessage("/project", "claude", sid, "assistant",
		`{"blocks":[{"type":"text","text":"too short to summarize"}]}`, nil, false, "")
	assert.NoError(t, err)

	// Empty summary — the frontend omits content for summarized messages.
	assert.NoError(t, service.SaveSummaryWithCards("chat_message", asstID, "", nil))

	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.NotEqual(t, "", msgs[0].Content, "messages with an empty summary must keep content so they remain visible")
	require.NotNil(t, msgs[0].Summary)
	assert.Equal(t, "", *msgs[0].Summary)
}

func TestReplaceSessionHistory_ReplacesMessages(t *testing.T) {
	db := setupDB(t)
	projectPath := "/proj"
	sid := helperCreateSession(t, projectPath, "claude", "Test")

	_, err := db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', 'old1', 'm1')", projectPath, sid)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'assistant', 'old2', 'm2')", projectPath, sid)
	require.NoError(t, err)

	// Replace with a single new message.
	msgs := []service.ReplayMessage{{Role: "user", Content: `{"blocks":[{"type":"text","text":"new"}]}`, ExtMsgID: "n1"}}
	n, err := service.ReplaceSessionHistory(sid, projectPath, "claude", msgs)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Old messages are gone; only the new one remains.
	var cnt int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt))
	assert.Equal(t, 1, cnt)

	var content string
	require.NoError(t, db.QueryRow("SELECT content FROM chat_history WHERE session_id = ?", sid).Scan(&content))
	assert.Contains(t, content, `"text":"new"`)
}

func TestReplaceSessionHistory_PersistsToolCalls(t *testing.T) {
	db := setupDB(t)
	projectPath := "/proj"
	sid := helperCreateSession(t, projectPath, "claude", "Test")

	msgs := []service.ReplayMessage{{
		Role:     "assistant",
		Content:  `{"blocks":[{"type":"tool_use","id":"tc1","name":"Bash","input":{"cmd":"ls"}}]}`,
		ExtMsgID: "n1",
		ToolCalls: []model.ContentBlock{{
			Type:   "tool_use",
			ID:     "tc1",
			Name:   "Bash",
			Input:  map[string]any{"cmd": "ls"},
			Output: "out",
			Status: "completed",
			Done:   true,
		}},
	}}

	// Regression: must not deadlock (nested writeMu acquisition) and tool calls
	// must be persisted inside the same transaction as the message.
	n, err := service.ReplaceSessionHistory(sid, projectPath, "claude", msgs)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var msgID int64
	require.NoError(t, db.QueryRow("SELECT id FROM chat_history WHERE session_id = ?", sid).Scan(&msgID))
	var tcCount int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_tool_calls WHERE message_id = ? AND session_id = ?", msgID, sid).Scan(&tcCount))
	assert.Equal(t, 1, tcCount)

	var output, status string
	var doneInt int
	require.NoError(t, db.QueryRow("SELECT output, status, done FROM chat_tool_calls WHERE message_id = ? AND tool_id = ?", msgID, "tc1").Scan(&output, &status, &doneInt))
	assert.Equal(t, "out", output)
	assert.Equal(t, "completed", status)
	assert.Equal(t, 1, doneInt)
}

func TestReplaceSessionHistory_RollbackRestoresHistory(t *testing.T) {
	db := setupDB(t)
	projectPath := "/proj"
	sid := helperCreateSession(t, projectPath, "claude", "Test")

	_, err := db.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', 'old', 'm1')", projectPath, sid)
	require.NoError(t, err)

	// Force the transaction to fail AFTER the DELETE has removed the old rows: an
	// invalid role violates the CHECK(role IN ('user','assistant')) constraint.
	// The transaction must roll back, restoring the original history intact.
	msgs := []service.ReplayMessage{{
		Role:    "system", // violates CHECK constraint
		Content: `{"blocks":[{"type":"text","text":"new"}]}`,
	}}
	n, err := service.ReplaceSessionHistory(sid, projectPath, "claude", msgs)
	require.Error(t, err)
	assert.Equal(t, 0, n)

	var cnt int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt))
	assert.Equal(t, 1, cnt)
	var content string
	require.NoError(t, db.QueryRow("SELECT content FROM chat_history WHERE session_id = ?", sid).Scan(&content))
	assert.Equal(t, "old", content)
}

// TestEnqueueAndMaybeStart_NotRunning_StartsGoroutine verifies that when the
// session is not running, EnqueueAndMaybeStart starts an AI execution goroutine
// (which drains the queued message). B1: the first message is consumed by
// consumeFirstQueuedMessage so the drain loop does NOT execute it a second time.
func TestEnqueueAndMaybeStart_NotRunning_StartsGoroutine(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Enqueue Start")

	// Verify running state becomes true (goroutine started).
	started, _, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "hello",
		QueueID:     "pending-1",
	})
	assert.NoError(t, err)
	assert.True(t, started, "should start a goroutine when session not running")

	// B1 regression: the first message is consumed synchronously by
	// consumeFirstQueuedMessage (the goroutine runs it via cfg.Message), so the
	// DB queue must be empty — the drain loop would otherwise dequeue it again.
	assert.Equal(t, 0, service.GetQueuedCount(sid), "first message must be consumed, not left queued for double execution")

	// Clean up: cancel the session to kill the started goroutine, then wait
	// for it to fully unwind BEFORE the test DB is torn down (otherwise the
	// goroutine's deferred cleanup touches a nil DB).
	service.CancelSession(sid)
	time.Sleep(200 * time.Millisecond)
}

// TestEnqueueAndMaybeStart_FirstMessageRace_PreservesEarlierQueued verifies
// the R1 race: when an earlier message is already queued (queued=1) and the
// session is still marked idle, EnqueueAndMaybeStart (acting as the "winner"
// that claims the session) must consume ITS OWN freshly-inserted row, NOT the
// pre-existing queued row. Dequeueing the pre-existing row while running its
// own message would leave the earlier message queued for double-execution
// (or, in the symmetric race, dropped entirely).
func TestEnqueueAndMaybeStart_FirstMessageRace_PreservesEarlierQueued(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Enqueue Race")

	// Earlier message already queued (e.g. from a previous enqueue whose drain
	// loop exited — the exact B2 window). Session still NOT running.
	earlierID, err := service.AddQueuedMessage("/project", "claude", sid, "earlier", nil, "pending-earlier", "")
	assert.NoError(t, err)
	assert.Greater(t, earlierID, int64(0))

	// EnqueueAndMaybeStart wins the idle-session claim and runs "current"
	// directly via cfg.Message. It must consume only its own row.
	started, _, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "current",
		QueueID:     "pending-current",
	})
	assert.NoError(t, err)
	assert.True(t, started, "session was idle, should start")

	// The current message is executed directly (consumed). The EARLIER queued
	// message must remain queued — the drain loop inside the started goroutine
	// is responsible for consuming it.
	msgs, err := service.GetQueuedMessages(sid)
	assert.NoError(t, err)
	require.Len(t, msgs, 1, "earlier message must still be queued, only current consumed")
	assert.Equal(t, "earlier", msgs[0].Text)
	assert.Equal(t, "pending-earlier", msgs[0].QueueID)

	// Clean up: cancel to kill the started goroutine, then wait for unwind.
	service.CancelSession(sid)
	time.Sleep(200 * time.Millisecond)
}

// TestEnqueueAndMaybeStart_ConcurrentEnqueues_NoMessageLoss verifies the R1
// race under real concurrency: two messages enqueued concurrently on an idle
// session. Exactly one EnqueueAndMaybeStart wins TrySetSessionRunning. The
// winner's message is executed directly (consumeQueuedMessageByID) and the
// loser's message must either stay queued for the drain loop OR be picked up
// by the B2 self-heal — but never lost and never executed twice.
func TestEnqueueAndMaybeStart_ConcurrentEnqueues_NoMessageLoss(t *testing.T) {
	db := setupDB(t)
	// Single connection so the :memory: SQLite database is shared across all
	// goroutines (each pooled connection otherwise gets its own private
	// in-memory database, hiding rows written by other connections).
	db.SetMaxOpenConns(1)
	_ = db
	// Register a blocking mock backend so the winner's goroutine stays alive
	// while the loser enqueues. With an unknown backend the winner would fail
	// immediately (unsupported backend), clearing the running flag before the
	// loser's TrySetSessionRunning — letting both enqueues "start" and making
	// the exactly-one assertion flaky.
	const backendID = "test-enqueue-concurrent"
	gate := make(chan struct{})
	ai.RegisterBackend(backendID, func() ai.AIBackend { return &enqueueBlockingBackend{gate: gate} })
	t.Cleanup(func() { close(gate) })
	sid := helperCreateSession(t, "/project", backendID, "Enqueue Concurrent")

	var wg sync.WaitGroup
	startedFlags := make([]bool, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			started, _, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
				SessionID:   sid,
				ProjectPath: "/project",
				BackendName: backendID,
				Message:     fmt.Sprintf("msg-%d", i),
				QueueID:     fmt.Sprintf("pending-%d", i),
			})
			assert.NoError(t, err)
			startedFlags[i] = started
		}(i)
	}
	wg.Wait()

	// Exactly one of the two must have started a goroutine.
	startedCount := 0
	for _, s := range startedFlags {
		if s {
			startedCount++
		}
	}
	assert.Equal(t, 1, startedCount, "exactly one enqueue should win the idle claim")

	// Both messages must exist in the DB — no message may be lost. The loser
	// stays queued for the drain loop (the winner's goroutine is blocked on the
	// gate, so the B2 self-heal has not claimed it yet).
	msgs, err := service.GetChatHistory("/project", backendID, sid)
	assert.NoError(t, err)
	userCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userCount++
		}
	}
	assert.Equal(t, 2, userCount, "both enqueued user messages must be present in DB")

	// Clean up: cancel kills the running session and releases the gate.
	service.CancelSession(sid)
	time.Sleep(300 * time.Millisecond)
}

// enqueueBlockingBackend blocks until the session is cancelled so an execution
// goroutine stays alive while a concurrent enqueue runs — otherwise the winner
// would finish immediately and clear the running flag before the loser's
// TrySetSessionRunning, making the exactly-one assertion flaky.
type enqueueBlockingBackend struct {
	gate chan struct{}
}

func (m *enqueueBlockingBackend) Name() string { return "test-enqueue-concurrent" }
func (m *enqueueBlockingBackend) ExecuteStream(ctx context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 4)
	go func() {
		defer close(ch)
		select {
		case <-m.gate:
		case <-ctx.Done():
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()
	return ch, nil
}

// TestEnqueueAndMaybeStart_Running_DoesNotStartGoroutine verifies that when the
// session is already running, EnqueueAndMaybeStart does NOT start a second
// goroutine — it signals the existing drain loop instead.
func TestEnqueueAndMaybeStart_Running_DoesNotStartGoroutine(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Enqueue Running")

	// Mark session as already running.
	service.SetSessionRunning(sid, true)

	started, _, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "hello",
		QueueID:     "pending-2",
	})
	assert.NoError(t, err)
	assert.False(t, started, "should NOT start a goroutine when session already running")

	// Message should be queued in DB (drain loop will pick it up).
	assert.Equal(t, 1, service.GetQueuedCount(sid))

	// Clean up: cancel kills the running session AND prevents the B2
	// self-heal goroutine (scheduled 100ms later) from starting a new one.
	// Wait for the self-heal window to pass so its goroutine can unwind
	// before the test DB is torn down.
	service.CancelSession(sid)
	time.Sleep(200 * time.Millisecond)
}

// TestQueuedMessage_DrainYieldsConversationalIdOrder is the end-to-end proof of
// the design that replaced the reply-anchor column: queued messages 2/3 are
// materialized only when drained, so the DB id order (msg2, reply2, msg3,
// reply3) already IS the conversational order. There is no queue_id anchor on
// the reply any more, because there is nothing to re-anchor.
func TestQueuedMessage_DrainYieldsConversationalIdOrder(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Queue Order")

	// msg1 + reply1 complete normally.
	msg1ID, err := service.AddChatMessage("/project", "claude", sid, "user", "1", nil, false, "")
	require.NoError(t, err)
	reply1ID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "reply1", nil, false, "")
	require.NoError(t, err)

	// msg2, msg3 wait in the queue — they have NO chat_history rows yet.
	_, err = service.AddQueuedMessage("/project", "claude", sid, "2", nil, "pending-2", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "3", nil, "pending-3", "")
	require.NoError(t, err)

	// The drain loop claims msg2, then its reply, then msg3, then its reply.
	_, msg2ID, ok, err := service.ClaimNextAndMaterialize(sid)
	require.NoError(t, err)
	require.True(t, ok)
	reply2ID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "reply2", nil, false, "")
	require.NoError(t, err)

	_, msg3ID, ok, err := service.ClaimNextAndMaterialize(sid)
	require.NoError(t, err)
	require.True(t, ok)
	reply3ID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "reply3", nil, false, "")
	require.NoError(t, err)

	// Id order is exactly the conversational order — no re-anchoring needed.
	assert.Less(t, msg1ID, reply1ID)
	assert.Less(t, reply1ID, msg2ID)
	assert.Less(t, msg2ID, reply2ID)
	assert.Less(t, reply2ID, msg3ID)
	assert.Less(t, msg3ID, reply3ID)

	// History read back in id order gives the natural conversation.
	msgs, err := service.GetChatHistory("/project", "claude", sid)
	require.NoError(t, err)
	require.Len(t, msgs, 6)
	want := []string{"1", "reply1", "2", "reply2", "3", "reply3"}
	for i, w := range want {
		assert.Equal(t, w, msgs[i].Content, "message %d", i)
	}
}

func TestPatchContextStateMerge_UsageNakedZeroKeepsStoredWindow(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// First: a real usage_update establishes a known window.
	service.PatchContextStateMerge(sid, map[string]string{
		"usage": `{"used":300000,"size":1000000,"inputTokens":301000,"cost":5.0}`,
	})

	// Then a naked usage_update (used=0, size=0, higher cost only) arrives —
	// the exact bug scenario. It must NOT regress the stored window.
	service.PatchContextStateMerge(sid, map[string]string{
		"usage": `{"used":0,"size":0,"cost":6.08}`,
	})

	state := service.GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 1000000, state.Usage.Size, "stored window must survive a naked usage_update")
	assert.Equal(t, 300000, state.Usage.Used, "stored used must survive a naked usage_update")
	assert.Equal(t, 301000, state.Usage.InputTokens, "stored token fields must survive")
	assert.Equal(t, 6.08, state.Usage.Cost, "cost is monotonic — the higher cost applies")
}

func TestPatchContextStateMerge_UsageRealUpdateApplies(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	service.PatchContextStateMerge(sid, map[string]string{
		"usage": `{"used":1000,"size":200000,"cost":1.0}`,
	})

	// A genuinely informative notification updates the state normally.
	service.PatchContextStateMerge(sid, map[string]string{
		"usage": `{"used":15000,"size":200000,"cost":1.2,"inputTokens":15000}`,
	})

	state := service.GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 15000, state.Usage.Used)
	assert.Equal(t, 200000, state.Usage.Size)
	assert.Equal(t, 15000, state.Usage.InputTokens)
	assert.Equal(t, 1.2, state.Usage.Cost)
}

func TestPatchContextStateMerge_UsageFirstWriteAsIs(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// No prior state — the first usage patch is written as-is (nothing to
	// protect against regression).
	service.PatchContextStateMerge(sid, map[string]string{
		"usage": `{"used":0,"size":0,"cost":0.5}`,
	})

	state := service.GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 0, state.Usage.Used)
	assert.Equal(t, 0, state.Usage.Size)
	assert.Equal(t, 0.5, state.Usage.Cost)
}

// TestPinnedSessionSortOrder covers the pinned block: pinned sessions lead the
// list (pinned DESC is the leading sort key, ahead of the manual order), and
// pinning must not bump updated_at (which drives the relative-time label and
// GetLatestSessionID's "most recent" pick).
func TestPinnedSessionSortOrder(t *testing.T) {
	db := setupDB(t)
	projectPath := "/test/pinned-sort"

	// Create sessions with different pinned states. created_at is set explicitly
	// so the newest-first tiebreak (all default to sort_order 0) is deterministic.
	s1 := helperCreateSession(t, projectPath, "claude", "First")
	s2 := helperCreateSession(t, projectPath, "claude", "Second")
	s3 := helperCreateSession(t, projectPath, "claude", "Third")
	_, err := service.WriteExec("UPDATE chat_sessions SET created_at = '2024-01-01 00:00:00' WHERE id = ?", s1)
	require.NoError(t, err)
	_, err = service.WriteExec("UPDATE chat_sessions SET created_at = '2024-01-02 00:00:00' WHERE id = ?", s2)
	require.NoError(t, err)
	_, err = service.WriteExec("UPDATE chat_sessions SET created_at = '2024-01-03 00:00:00' WHERE id = ?", s3)
	require.NoError(t, err)

	// Pin is a UI preference, not session activity: it must not bump updated_at.
	// Backdate s1's updated_at first so a bump would be visible when it is pinned.
	_, err = service.WriteExec(
		"UPDATE chat_sessions SET updated_at = '2020-01-01 00:00:00' WHERE id = ?", s1,
	)
	require.NoError(t, err)

	// Pin the OLDEST session: it must jump to the top despite being the oldest.
	require.NoError(t, service.UpdateSessionPinned(s1, true))

	var updatedAt string
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT updated_at FROM chat_sessions WHERE id = ?", s1).Scan(&updatedAt))
	assert.Contains(t, updatedAt, "2020-01-01",
		"pinning must not rewrite updated_at")

	sessions, err := service.GetSessions(projectPath, "")
	require.NoError(t, err)
	require.Len(t, sessions, 3)

	// The pinned row leads; the unpinned rest keep newest-first.
	assert.Equal(t, s1, sessions[0].ID, "pinned session must lead")
	assert.True(t, sessions[0].Pinned)
	assert.Equal(t, s3, sessions[1].ID, "unpinned rows stay newest-first")
	assert.Equal(t, s2, sessions[2].ID)

	// Unpinning drops it back into the manual order (newest-first tiebreak).
	require.NoError(t, service.UpdateSessionPinned(s1, false))
	sessions, err = service.GetSessions(projectPath, "")
	require.NoError(t, err)
	for _, s := range sessions {
		assert.False(t, s.Pinned, "all markers must be cleared")
	}
	assert.Equal(t, s3, sessions[0].ID, "unpinned list is newest-first")

	// Pinning several sessions puts them all ahead of the unpinned ones.
	require.NoError(t, service.UpdateSessionPinned(s1, true))
	require.NoError(t, service.UpdateSessionPinned(s3, true))
	sessions, err = service.GetSessions(projectPath, "")
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.True(t, sessions[0].Pinned, "pinned block leads")
	assert.True(t, sessions[1].Pinned, "pinned block leads")
	assert.False(t, sessions[2].Pinned, "unpinned tail follows")
	assert.ElementsMatch(t, []string{s1, s3}, []string{sessions[0].ID, sessions[1].ID})

	_ = db
}

// TestReorderSessionsIgnoresPinned guards the pinned block against the drag
// write: pinned rows are positioned by pinned DESC, not sort_order, so a
// reorder must neither move them nor overwrite the sort_order they held before
// being pinned.
func TestReorderSessionsIgnoresPinned(t *testing.T) {
	setupDB(t)
	projectPath := "/test/reorder-pinned"

	a := helperCreateSession(t, projectPath, "claude", "A")
	b := helperCreateSession(t, projectPath, "claude", "B")
	pinned := helperCreateSession(t, projectPath, "claude", "Pinned")

	// Give the pinned row a distinctive sort_order, then pin it.
	require.NoError(t, service.ReorderSessions(projectPath, []string{b, a, pinned}))
	require.NoError(t, service.UpdateSessionPinned(pinned, true))
	var before int
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT sort_order FROM chat_sessions WHERE id = ?", pinned).Scan(&before))

	// The client (wrongly) posts the pinned row at the bottom. It must be
	// ignored: pinned stays on top and keeps its old sort_order.
	require.NoError(t, service.ReorderSessions(projectPath, []string{a, b, pinned}))

	sessions, err := service.GetSessions(projectPath, "")
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.Equal(t, pinned, sessions[0].ID, "a pinned row cannot be dragged out of the pinned block")

	var after int
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT sort_order FROM chat_sessions WHERE id = ?", pinned).Scan(&after))
	assert.Equal(t, before, after, "a pinned row's sort_order must not be rewritten")

	// The unpinned pair still honours the posted order.
	assert.Equal(t, []string{a, b}, []string{sessions[1].ID, sessions[2].ID})
}

// TestReorderSessionsPersistsManualOrder covers the drag-order write: ids
// become sort_order by index, the list reads back in that order, and a
// session from another project cannot be renumbered through it.
func TestReorderSessionsPersistsManualOrder(t *testing.T) {
	setupDB(t)
	projectPath := "/test/reorder"

	a := helperCreateSession(t, projectPath, "claude", "A")
	b := helperCreateSession(t, projectPath, "claude", "B")
	c := helperCreateSession(t, projectPath, "claude", "C")
	foreign := helperCreateSession(t, "/test/reorder-other", "claude", "Foreign")

	// Drag C to the top, then A, then B.
	require.NoError(t, service.ReorderSessions(projectPath, []string{c, a, b}))

	sessions, err := service.GetSessions(projectPath, "")
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.Equal(t, []string{c, a, b}, []string{sessions[0].ID, sessions[1].ID, sessions[2].ID})
	assert.Equal(t, []int{0, 1, 2}, []int{sessions[0].SortOrder, sessions[1].SortOrder, sessions[2].SortOrder})

	// A foreign-project id must be ignored, not renumbered.
	require.NoError(t, service.ReorderSessions(projectPath, []string{foreign}))
	var foreignOrder int
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT sort_order FROM chat_sessions WHERE id = ?", foreign).Scan(&foreignOrder))
	assert.Equal(t, 0, foreignOrder, "another project's session must not be renumbered")

	// The reorder must not touch updated_at (it is a UI preference).
	var updatedAt string
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT updated_at FROM chat_sessions WHERE id = ?", c).Scan(&updatedAt))
	assert.NotEmpty(t, updatedAt)
}

// TestReorderSessionsKeepsUnpostedVisibleRowsBelow guards the safety net: the
// client posts every row it displays, but a row created between its load and
// the drop is not in ids. Such a row defaults to sort_order 0 and would
// otherwise sort above the whole dragged block, so the reorder must push it
// below the posted rows.
func TestReorderSessionsKeepsUnpostedVisibleRowsBelow(t *testing.T) {
	setupDB(t)
	projectPath := "/test/reorder-prefix"

	// All rows start at sort_order 0 (the un-dragged default). created_at is
	// pinned so the newest-first tiebreak is deterministic.
	ids := make([]string, 0, 5)
	for i, title := range []string{"A", "B", "C", "D", "E"} {
		id := helperCreateSession(t, projectPath, "claude", title)
		ids = append(ids, id)
		_, err := service.WriteExec(
			"UPDATE chat_sessions SET created_at = ? WHERE id = ?",
			fmt.Sprintf("2024-01-%02d 00:00:00", i+1), id,
		)
		require.NoError(t, err)
	}

	// The user swaps the first two rows; the other three were not posted.
	require.NoError(t, service.ReorderSessions(projectPath, []string{ids[1], ids[0]}))

	sessions, err := service.GetSessions(projectPath, "")
	require.NoError(t, err)
	require.Len(t, sessions, 5)
	// The two dragged rows lead, in the posted order...
	assert.Equal(t, []string{ids[1], ids[0]}, []string{sessions[0].ID, sessions[1].ID})
	// ...and the unposted rows follow, none interleaved above them.
	assert.Equal(t, []string{ids[4], ids[3], ids[2]},
		[]string{sessions[2].ID, sessions[3].ID, sessions[4].ID})
}

// TestReorderSessionsIgnoresArchived is the regression guard for the drag
// slowness: the reorder used to renumber EVERY non-pinned row in the project,
// including archived ones the list never shows (measured 1993 rows rewritten
// for a 3-row list, ~3s per drag). Archived rows must be left untouched.
func TestReorderSessionsIgnoresArchived(t *testing.T) {
	setupDB(t)
	projectPath := "/test/reorder-archived"

	visible := helperCreateSession(t, projectPath, "claude", "Visible")
	archived := helperCreateSession(t, projectPath, "claude", "Archived")
	// A distinctive sort_order proves the archived row is not rewritten.
	_, err := service.WriteExec("UPDATE chat_sessions SET sort_order = 42 WHERE id = ?", archived)
	require.NoError(t, err)
	_, err = service.WriteExec("UPDATE chat_sessions SET archived = 1 WHERE id = ?", archived)
	require.NoError(t, err)

	require.NoError(t, service.ReorderSessions(projectPath, []string{visible}))

	var archivedOrder int
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT sort_order FROM chat_sessions WHERE id = ?", archived).Scan(&archivedOrder))
	assert.Equal(t, 42, archivedOrder, "an archived row must not be renumbered")

	// The visible row is numbered from 0 (archived rows do not occupy an index).
	var visibleOrder int
	require.NoError(t, service.UnsafeDBForTest().
		QueryRow("SELECT sort_order FROM chat_sessions WHERE id = ?", visible).Scan(&visibleOrder))
	assert.Equal(t, 0, visibleOrder)
}

// TestNewSessionLandsOnTop guards the "new session goes to the top" rule: a
// fresh session defaults to sort_order 0 and wins the created_at DESC tiebreak,
// so it leads even after the user has dragged other rows.
func TestNewSessionLandsOnTop(t *testing.T) {
	setupDB(t)
	projectPath := "/test/new-on-top"

	a := helperCreateSession(t, projectPath, "claude", "A")
	b := helperCreateSession(t, projectPath, "claude", "B")
	// Put B on top, then A. Their created_at differs, so pin down the drag order
	// explicitly by backdating to make the assertion meaningful.
	_, err := service.WriteExec("UPDATE chat_sessions SET created_at = '2024-01-01 00:00:00' WHERE id = ?", a)
	require.NoError(t, err)
	_, err = service.WriteExec("UPDATE chat_sessions SET created_at = '2024-01-02 00:00:00' WHERE id = ?", b)
	require.NoError(t, err)
	require.NoError(t, service.ReorderSessions(projectPath, []string{b, a}))

	fresh := helperCreateSession(t, projectPath, "claude", "Fresh")
	// The fresh session defaults to sort_order 0, tied with b; give it a newer
	// created_at so the tiebreak is deterministic rather than second-precision.
	_, err = service.WriteExec("UPDATE chat_sessions SET created_at = '2024-06-01 00:00:00' WHERE id = ?", fresh)
	require.NoError(t, err)

	sessions, err := service.GetSessions(projectPath, "")
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.Equal(t, fresh, sessions[0].ID, "a new session must land at the top of the manual order")
	assert.Equal(t, b, sessions[1].ID)
	assert.Equal(t, a, sessions[2].ID)
}

// TestPinnedSessionPaginationNoDuplicates guards the keyset cursor against the
// full sort key. Ordering is (pinned DESC, sort_order ASC, created_at DESC,
// id DESC); when rows share a (pinned, sort_order) — the state after the #492
// backfill, and any list the user has never dragged — paging on created_at
// alone can skip or repeat rows. The cursor must carry pinned and sort_order
// too.
func TestPinnedSessionPaginationNoDuplicates(t *testing.T) {
	setupDB(t)
	projectPath := "/test/pinned-pagination"

	// Six sessions with strictly increasing created_at (helperCreateSession
	// assigns CURRENT_TIMESTAMP, so make the order deterministic explicitly).
	ids := make([]string, 0, 6)
	for _, title := range []string{"A", "B", "C", "D", "E", "F"} {
		ids = append(ids, helperCreateSession(t, projectPath, "claude", title))
		_, err := service.WriteExec(
			"UPDATE chat_sessions SET created_at = ? WHERE id = ?",
			fmt.Sprintf("2024-0%d-01 00:00:00", len(ids)+1), ids[len(ids)-1],
		)
		require.NoError(t, err)
	}

	// Force a shared sort_order, which is what an un-dragged list looks like
	// (and what the migration backfill produces). Ties then fall back to
	// created_at DESC, id DESC — so the newest session (ids[5]) leads.
	_, err := service.WriteExec("UPDATE chat_sessions SET sort_order = 0 WHERE project_path = ?", projectPath)
	require.NoError(t, err)

	// Pin the OLDEST session so the page boundary falls between the pinned block
	// and the unpinned rows — the case where a created_at-only cursor would
	// re-return the pinned row on page 2.
	require.NoError(t, service.UpdateSessionPinned(ids[0], true))

	page1, hasMore, err := service.GetSessionsPaged(projectPath, "", 3, "", "", nil, nil, "")
	require.NoError(t, err)
	require.True(t, hasMore, "there are more rows after page 1")
	require.Len(t, page1, 3)
	assert.Equal(t, ids[0], page1[0].ID, "the pinned row leads")
	assert.True(t, page1[0].Pinned)

	last1 := page1[len(page1)-1]
	lastOrder := last1.SortOrder
	lastPinned := last1.Pinned
	page2, _, err := service.GetSessionsPaged(
		projectPath, "", 3,
		last1.CreatedAt.Format("2006-01-02 15:04:05"), last1.ID, &lastOrder, &lastPinned, "",
	)
	require.NoError(t, err)

	seen := map[string]int{}
	for _, s := range append(append([]model.ChatSession{}, page1...), page2...) {
		seen[s.ID]++
	}
	for id, n := range seen {
		assert.Equalf(t, 1, n, "session %s must appear exactly once across pages", id)
	}
	assert.Len(t, seen, 6, "both pages together must cover every session")
}

// TestUpdateQueuedMessageQuoteNote covers editing a quote note while the message
// is still queued: there is no chat_history row to update, so the queue-side
// counterpart rewrites the queued_messages files JSON instead (the drain loop
// re-reads it when it materializes the row).
func TestUpdateQueuedMessageQuoteNote(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Q")

	const qid = "q-queued-quote"
	files := []model.FileEntry{{Path: "src/a.go", Kind: "quote", ID: "quote-1", Text: "x := 1", Note: "old"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "解释一下", files, qid, "")
	require.NoError(t, err)

	entries, err := service.UpdateQueuedMessageQuoteNote(sid, qid, "quote-1", "new note")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "new note", entries[0].Note)

	// The edit must be visible through the queue API too.
	queue, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	require.Len(t, queue, 1)
	require.Len(t, queue[0].Files, 1)
	assert.Equal(t, "new note", queue[0].Files[0].Note)

	// A message that is not queued (or a quote that does not exist) → not found.
	_, err = service.UpdateQueuedMessageQuoteNote(sid, "q-does-not-exist", "quote-1", "x")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)
	_, err = service.UpdateQueuedMessageQuoteNote(sid, qid, "quote-does-not-exist", "x")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)

	// Empty arguments short-circuit rather than matching an arbitrary row.
	_, err = service.UpdateQueuedMessageQuoteNote(sid, "", "quote-1", "x")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)
}
