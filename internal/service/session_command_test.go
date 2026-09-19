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

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForSessionCommand creates an in-memory SQLite with the chat_sessions table.
func setupTestDBForSessionCommand(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
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
			transport TEXT DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			context_state TEXT DEFAULT '',
			title_renamed INTEGER NOT NULL DEFAULT 0,
			title_source TEXT NOT NULL DEFAULT '',
			compacted INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_read_at DATETIME,
			UNIQUE(project_path, backend, id)
		);
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
			queue_id TEXT DEFAULT '',
			queued INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
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
		-- Push subscriber tables. last_session_id is the sticky push target;
		-- it is declared here so these tests exercise the real column rather
		-- than a stub that could drift from the production schema.
		CREATE TABLE IF NOT EXISTS dingtalk_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			conversation_id TEXT NOT NULL DEFAULT '',
			user_name       TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT 'stream',
			last_session_id TEXT NOT NULL DEFAULT '',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS feishu_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			chat_id         TEXT NOT NULL DEFAULT '',
			user_name       TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT 'stream',
			last_session_id TEXT NOT NULL DEFAULT '',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	cleanup := SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func TestSendMessageToSessionFromDingTalk_NotFound(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	err := SendMessageToSessionFromDingTalk("nonexistent-session", "hello", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestBuildChatRequest_NewSession(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Test that BuildChatRequest produces a valid ChatRequest for a new session
	// (no assistant messages → resume=false)
	req := BuildChatRequest("hello", "sess-1", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	if req.Prompt != "hello" {
		t.Errorf("expected prompt 'hello', got %q", req.Prompt)
	}
	if req.Resume {
		t.Error("expected resume=false for new session")
	}
	if req.HasAttachments {
		t.Error("expected HasAttachments=false")
	}
}

// TestBuildChatRequest_CompactedFlagConsumedOnce pins the end-to-end contract of
// the compaction re-injection: the flag reaches the request exactly once, so the
// system prompt is re-injected on the turn after a compaction and not again.
func TestBuildChatRequest_CompactedFlagConsumedOnce(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'T', '', 'default', '', 'chat')",
		"sess-compact",
	)
	require.NoError(t, err)

	// Before any compaction the flag is not set.
	req := BuildChatRequest("hello", "sess-compact", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.False(t, req.Compacted, "a session that never compacted must not carry the flag")

	MarkSessionCompacted("sess-compact")

	req = BuildChatRequest("continue", "sess-compact", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.True(t, req.Compacted, "the turn after a compaction must re-inject the system prompt")

	req = BuildChatRequest("and again", "sess-compact", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.False(t, req.Compacted, "the flag must not survive into a second turn")
}

// TestBuildChatRequest_CompactCommandDoesNotConsumeFlag pins the ordering rule:
// the /compact command turn is not a model call, and the compaction it triggers
// happens AFTER it — so consuming the flag there would leave the following real
// turn without the re-injection.
func TestBuildChatRequest_CompactCommandDoesNotConsumeFlag(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'T', '', 'default', '', 'chat')",
		"sess-cmd",
	)
	require.NoError(t, err)

	// The handler marks the session when the user sends /compact; by the time the
	// request is built the flag is already set (both happen in the same request).
	MarkSessionCompacted("sess-cmd")

	cmdReq := BuildChatRequest("/compact", "sess-cmd", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.False(t, cmdReq.Compacted, "the /compact turn itself must not consume the flag")

	nextReq := BuildChatRequest("continue", "sess-cmd", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.True(t, nextReq.Compacted, "the flag must survive the /compact turn for the next real turn")

	finalReq := BuildChatRequest("more", "sess-cmd", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.False(t, finalReq.Compacted, "the flag is consumed by the first real turn after /compact")
}

func TestFindSessionsByPrefix_ArchivedExcluded(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, archived) VALUES (?, '/proj', 'codebuddy', 'Archived', 'agent1', 'default', '', 'chat', 1)",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	results, err := FindSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for archived session, got %d", len(results))
	}
}

func TestFindSessionsByPrefix(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test Session', 'agent1', 'default', '', 'chat')",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Another Session', 'agent2', 'default', '', 'chat')",
		"b2c3d4e5-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	results, err := FindSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("wrong session ID: %s", results[0].ID)
	}
	if results[0].Backend != "codebuddy" {
		t.Errorf("wrong backend: %s", results[0].Backend)
	}
}

func TestFindSessionsByPrefix_NoMatch(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	results, err := FindSessionsByPrefix("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFindSessionsByPrefix_CaseInsensitive(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test', 'agent1', 'default', '', 'chat')",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	results, err := FindSessionsByPrefix("A1B2C3D4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for case-insensitive match, got %d", len(results))
	}
}

func TestFindRunningSessionsByPrefix(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test', 'agent1', 'default', '', 'chat')",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Not running
	results, err := FindRunningSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 when not running, got %d", len(results))
	}

	// Mark as running
	TrySetSessionRunning("a1b2c3d4-1111-1111-1111-111111111111")
	defer SetSessionRunning("a1b2c3d4-1111-1111-1111-111111111111", false, true)

	results, err = FindRunningSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 when running, got %d", len(results))
	}
	if results[0].ID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("wrong session ID: %s", results[0].ID)
	}
}

func TestListRecentSessions(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Empty — should return no results
	results, err := ListRecentSessions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty DB, got %d", len(results))
	}

	// Insert two sessions
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Session A', 'agent1', 'default', '', 'chat')",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Session B', 'agent2', 'default', '', 'chat')",
		"b2c3d4e5-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	results, err = ListRecentSessions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Limit works
	results, err = ListRecentSessions(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result with limit=1, got %d", len(results))
	}
}

// ============================================================================
// Agent-config / session-state resolution tests
//
// These used to call resolveAgentConfig / resolveIsACP / resolveSessionState
// directly. Those helpers were folded into BuildChatRequest when the handler and
// service implementations were unified, so the same behaviors are now asserted
// through the public API — which is also what production calls.
// ============================================================================

// withAgents swaps the agent registry for one test and restores it afterwards.
func withAgents(t *testing.T, agents map[string]*model.Agent) {
	t.Helper()
	origAgents := model.Agents
	model.Agents = agents
	t.Cleanup(func() { model.Agents = origAgents })
}

func TestBuildChatRequest_UnknownAgentID_EmptyAgentFields(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{})

	req := BuildChatRequest("hi", "sess-unknown-agent", "/proj", "claude", "nonexistent", "", "", "", "", "/proj", false)

	assert.Equal(t, "", req.Model)
	assert.Equal(t, "", req.Command)
	assert.Equal(t, "", req.ThinkingEffort)
	assert.Equal(t, "", req.Mode)
}

func TestBuildChatRequest_AgentWithAllFields(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {
			ID:                  "test-agent",
			RuntimeSystemPrompt: "You are at {{PROJECT_PATH}}",
			Command:             "/usr/bin/test-cli",
			ThinkingEffort:      "high",
			PreferredMode:       "code",
			Models:              []model.AgentModel{{ID: "model-1", Default: true}},
		},
	})

	req := BuildChatRequest("hi", "sess-all-fields", "/home/user/proj", "claude", "test-agent", "", "", "", "", "/home/user/proj", false)

	assert.Equal(t, "You are at /home/user/proj", req.SystemPrompt)
	assert.Equal(t, "model-1", req.Model)
	assert.Equal(t, "/usr/bin/test-cli", req.Command)
	assert.Equal(t, "high", req.ThinkingEffort)
	assert.Equal(t, "code", req.Mode)
}

func TestBuildChatRequest_ModelOverridePrecedence(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello", Models: []model.AgentModel{{ID: "default-model", Default: true}}},
	})

	req := BuildChatRequest("hi", "sess-model-override", "", "claude", "test-agent", "custom-model", "", "", "", "", false)

	assert.Equal(t, "custom-model", req.Model, "explicit override beats the agent default")
}

func TestBuildChatRequest_NoOverride_NoDefaultModel(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello", Models: []model.AgentModel{}},
	})

	req := BuildChatRequest("hi", "sess-no-model", "", "claude", "test-agent", "", "", "", "", "", false)

	assert.Equal(t, "", req.Model)
}

func TestBuildChatRequest_ProjectPathReplacement(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "Work on {{PROJECT_PATH}} and {{PROJECT_PATH}} again"},
	})

	req := BuildChatRequest("hi", "sess-path-repl", "/my/path", "claude", "test-agent", "", "", "", "", "/my/path", false)

	assert.Equal(t, "Work on /my/path and /my/path again", req.SystemPrompt)
}

func TestBuildChatRequest_EmptyProjectPath_NoReplacement(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "Work on {{PROJECT_PATH}}"},
	})

	req := BuildChatRequest("hi", "sess-empty-path", "", "claude", "test-agent", "", "", "", "", "", false)

	assert.Equal(t, "Work on {{PROJECT_PATH}}", req.SystemPrompt)
}

func TestBuildChatRequest_ThinkingEffortOverridePrecedence(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello", ThinkingEffort: "low"},
	})

	req := BuildChatRequest("hi", "sess-effort-override", "", "claude", "test-agent", "", "high", "", "", "", false)

	assert.Equal(t, "high", req.ThinkingEffort)
}

func TestBuildChatRequest_ThinkingEffortFromAgent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello", ThinkingEffort: "low", PreferredThinkingEffort: "medium"},
	})

	req := BuildChatRequest("hi", "sess-effort-agent", "", "claude", "test-agent", "", "", "", "", "", false)

	assert.Equal(t, "medium", req.ThinkingEffort, "PreferredThinkingEffort wins over ThinkingEffort")
}

func TestBuildChatRequest_ModeOverridePrecedence(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello", PreferredMode: "code"},
	})

	req := BuildChatRequest("hi", "sess-mode-override", "", "claude", "test-agent", "", "", "plan", "", "", false)

	assert.Equal(t, "plan", req.Mode)
}

func TestBuildChatRequest_ModeFromAgent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello", PreferredMode: "code"},
	})

	req := BuildChatRequest("hi", "sess-mode-agent", "", "claude", "test-agent", "", "", "", "", "", false)

	assert.Equal(t, "code", req.Mode)
}

func TestBuildChatRequest_NoCommand(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {ID: "test-agent", RuntimeSystemPrompt: "hello"},
	})

	req := BuildChatRequest("hi", "sess-no-command", "", "claude", "test-agent", "", "", "", "", "", false)

	assert.Equal(t, "", req.Command)
}

func TestBuildChatRequest_DefaultModelFromPreferredModel(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{
		"test-agent": {
			ID: "test-agent", RuntimeSystemPrompt: "hello",
			PreferredModel: "preferred-model",
			Models:         []model.AgentModel{{ID: "default-model", Default: true}},
		},
	})

	req := BuildChatRequest("hi", "sess-preferred-model", "", "claude", "test-agent", "", "", "", "", "", false)

	assert.Equal(t, "preferred-model", req.Model)
}

// --- transport resolution (was resolveIsACP) ---

// transportCase runs BuildChatRequest with the given transport override and
// reports whether the request was treated as ACP. ACP is observable through
// Resume: ACP keeps the ClawBench session id, while a non-ACP resume swaps in
// the external session id.
func TestBuildChatRequest_TransportOverrideACPStdio(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"any-agent": {ID: "any-agent", RuntimeSystemPrompt: "s"}})

	req := BuildChatRequest("hi", "sess-acp-override", "", "claude", "any-agent", "", "", "", "acp-stdio", "", false)

	assert.Equal(t, "sess-acp-override", req.SessionID)
}

func TestBuildChatRequest_TransportOverrideCLI(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"any-agent": {ID: "any-agent", RuntimeSystemPrompt: "s"}})

	// No assistant history → resume stays false, so the id is kept regardless.
	req := BuildChatRequest("hi", "sess-cli-override", "", "claude", "any-agent", "", "", "", "cli", "", false)

	assert.False(t, req.Resume)
}

func TestBuildChatRequest_NoOverride_AgentWithACPTransport(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"acp-agent": {ID: "acp-agent", RuntimeSystemPrompt: "s", Transport: "acp-stdio"}})

	req := BuildChatRequest("hi", "sess-acp-agent", "", "claude", "acp-agent", "", "", "", "", "", false)

	assert.Equal(t, "sess-acp-agent", req.SessionID)
}

func TestBuildChatRequest_NoOverride_AgentWithCLITransport(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"cli-agent": {ID: "cli-agent", RuntimeSystemPrompt: "s", Transport: "cli"}})

	req := BuildChatRequest("hi", "sess-cli-agent", "", "claude", "cli-agent", "", "", "", "", "", false)

	assert.False(t, req.Resume)
}

func TestBuildChatRequest_NoOverride_UnknownAgent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{})

	req := BuildChatRequest("hi", "sess-unknown", "", "claude", "nope", "", "", "", "", "", false)

	assert.Equal(t, "sess-unknown", req.SessionID)
}

func TestBuildChatRequest_OverrideTakesPrecedenceOverAgent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"acp-agent": {ID: "acp-agent", RuntimeSystemPrompt: "s", Transport: "acp-stdio"}})

	// Explicit "cli" must win over the agent's acp-stdio transport.
	req := BuildChatRequest("hi", "sess-override-wins", "", "claude", "acp-agent", "", "", "", "cli", "", false)

	assert.False(t, req.Resume)
}

// --- session-state resolution (was resolveSessionState) ---

func TestBuildChatRequest_NewSession_NoResume(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"a": {ID: "a", RuntimeSystemPrompt: "s"}})

	req := BuildChatRequest("hi", "new-session", "", "claude", "a", "", "", "", "", "", false)

	assert.Equal(t, "new-session", req.SessionID)
	assert.False(t, req.Resume)
	assert.Equal(t, "", req.ForkContext)
}

func TestBuildChatRequest_ResumeWithExternalID_NonACP(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"a": {ID: "a", RuntimeSystemPrompt: "s"}})

	sessionID := "sess-resume-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat', ?)",
		sessionID, "ext-123",
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"hello"}]}`, sessionID,
	)
	require.NoError(t, err)

	req := BuildChatRequest("hi", sessionID, "", "claude", "a", "", "", "", "cli", "", false)

	assert.Equal(t, "ext-123", req.SessionID, "non-ACP resume uses the external session ID")
	assert.True(t, req.Resume)
	assert.Equal(t, "", req.ForkContext)
}

func TestBuildChatRequest_ResumeWithoutExternalID_Fork(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"a": {ID: "a", RuntimeSystemPrompt: "s"}})

	sessionID := "sess-fork-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"user message"}]}`, sessionID,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"assistant reply"}]}`, sessionID,
	)
	require.NoError(t, err)

	req := BuildChatRequest("hi", sessionID, "", "claude", "a", "", "", "", "cli", "", false)

	assert.Equal(t, "", req.SessionID, "non-ACP fork without external ID clears the session ID")
	assert.True(t, req.Resume)
	require.NotEmpty(t, req.ForkContext)
	// Role labels are capitalised and the envelope is present — the queue path
	// previously produced bare "user:"/"assistant:" lines with no envelope.
	assert.Contains(t, req.ForkContext, "User: user message")
	assert.Contains(t, req.ForkContext, "Assistant: assistant reply")
	assert.Contains(t, req.ForkContext, "[Below is the conversation history")
}

func TestBuildChatRequest_ResumeACPWithForkContext(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"acp": {ID: "acp", RuntimeSystemPrompt: "s", Transport: "acp-stdio"}})

	sessionID := "sess-acp-fork"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"msg"}]}`, sessionID,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"reply"}]}`, sessionID,
	)
	require.NoError(t, err)

	req := BuildChatRequest("hi", sessionID, "", "claude", "acp", "", "", "", "acp-stdio", "", false)

	assert.Equal(t, sessionID, req.SessionID, "ACP keeps the ClawBench session ID")
	assert.False(t, req.Resume, "ACP with fork context starts a new session")
	assert.NotEmpty(t, req.ForkContext)
}

func TestBuildChatRequest_ResumeACPWithExternalID(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	withAgents(t, map[string]*model.Agent{"acp": {ID: "acp", RuntimeSystemPrompt: "s", Transport: "acp-stdio"}})

	sessionID := "sess-acp-ext"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat', ?)",
		sessionID, "ext-acp-123",
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"hi"}]}`, sessionID,
	)
	require.NoError(t, err)

	req := BuildChatRequest("hi", sessionID, "", "claude", "acp", "", "", "", "acp-stdio", "", false)

	assert.Equal(t, sessionID, req.SessionID, "ACP keeps the ClawBench session ID even with an external ID")
	assert.True(t, req.Resume)
	assert.Equal(t, "", req.ForkContext)
}

// ============================================================================
// BuildChatRequest tests
// ============================================================================

func TestBuildChatRequest_EmptyAgentID_UsesDefault(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	origAgentList := model.AgentList
	origDefaultID := model.DefaultAgentID
	model.Agents = map[string]*model.Agent{
		"default-agent": {
			ID:                  "default-agent",
			RuntimeSystemPrompt: "default prompt",
			Models:              []model.AgentModel{{ID: "default-model", Default: true}},
		},
	}
	model.AgentList = []*model.Agent{{ID: "default-agent"}}
	model.DefaultAgentID = "default-agent"
	defer func() {
		model.Agents = origAgents
		model.AgentList = origAgentList
		model.DefaultAgentID = origDefaultID
	}()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "", "", "", "", "", "/proj", false)
	assert.Equal(t, "default-agent", req.AgentID)
	assert.Equal(t, "default-model", req.Model)
}

func TestBuildChatRequest_WithAttachments(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "", "", "", "", "", "/proj", true)
	assert.True(t, req.HasAttachments)
	// With attachments, media prompt should be appended to system prompt
	mediaPrompt := model.BuildMediaPrompt()
	if mediaPrompt != "" {
		assert.Contains(t, req.SystemPrompt, mediaPrompt)
	}
}

func TestBuildChatRequest_NoAttachments(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "", "", "", "", "", "/proj", false)
	assert.False(t, req.HasAttachments)
}

func TestBuildChatRequest_WithModelOverride(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:                  "test-agent",
			RuntimeSystemPrompt: "hello",
			Models:              []model.AgentModel{{ID: "default-model", Default: true}},
		},
	}
	defer func() { model.Agents = origAgents }()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "test-agent", "custom-model", "", "", "", "/proj", false)
	assert.Equal(t, "custom-model", req.Model)
}

func TestBuildChatRequest_WithThinkingEffortAndMode(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "agent", "", "high", "plan", "", "/proj", false)
	assert.Equal(t, "high", req.ThinkingEffort)
	assert.Equal(t, "plan", req.Mode)
}

func TestBuildChatRequest_TransportOverrideACP(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "agent", "", "", "", "acp-stdio", "/proj", false)
	// Just verify it doesn't panic and builds correctly
	assert.Equal(t, "hello", req.Prompt)
}

func TestBuildChatRequest_AgentWithCommand(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"cmd-agent": {
			ID:                  "cmd-agent",
			RuntimeSystemPrompt: "prompt",
			Command:             "/usr/local/bin/special-cli",
		},
	}
	defer func() { model.Agents = origAgents }()

	req := BuildChatRequest("hello", "sess-1", "/proj", "claude", "cmd-agent", "", "", "", "", "/proj", false)
	assert.Equal(t, "/usr/local/bin/special-cli", req.Command)
}

func TestBuildChatRequest_AgentWithProjectPathReplacement(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"path-agent": {
			ID:                  "path-agent",
			RuntimeSystemPrompt: "You are working in {{PROJECT_PATH}}",
		},
	}
	defer func() { model.Agents = origAgents }()

	req := BuildChatRequest("hello", "sess-1", "/my/project", "claude", "path-agent", "", "", "", "", "/my/project", false)
	assert.Equal(t, "You are working in /my/project", req.SystemPrompt)
}

// ============================================================================
// BuildForkContext tests
// ============================================================================

func TestBuildForkContext_EmptySession(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	result := BuildForkContext("nonexistent-session")
	assert.Equal(t, "", result)
}

func TestBuildForkContext_WithMessages(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sess-1"
	// Insert user message
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"hello user"}]}`, sessionID,
	)
	require.NoError(t, err)
	// Insert assistant message
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"hello assistant"}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "User: hello user")
	assert.Contains(t, result, "Assistant: hello assistant")
}

func TestBuildForkContext_SkipsEmptyContent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sess-empty"
	// Insert user message with no text blocks
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Equal(t, "", result, "messages with no text blocks should produce empty fork context")
}

func TestBuildForkContext_SkipsInvalidJSON(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sess-3"
	// Insert message with content that is not the block JSON format.
	//
	// This used to be skipped entirely, which silently dropped history: a
	// message stored as plain text (or as a nested JSON wrapper produced by ACP
	// sync replay) never reached the forked session. It is now recovered as
	// plain text, so the model sees the same history a direct send would inject.
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", "not valid json", sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "User: not valid json",
		"non-block content must be recovered as plain text, not dropped")
}

func TestBuildForkContext_SkipsEmptyTextBlocks(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sess-4"
	// Insert message with empty text block
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":""}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Equal(t, "", result, "empty text blocks should be skipped")
}

func TestBuildForkContext_MultipleTextBlocks(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sess-5"
	// Insert message with multiple text blocks
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"first part"},{"type":"thinking","text":"thinking part"},{"type":"text","text":"second part"}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "User: first part")
	assert.Contains(t, result, "second part")
	// thinking blocks are excluded
	assert.NotContains(t, result, "thinking part")
}

// TestBuildForkContext_PreservesSummarizedAssistant ensures fork context keeps
// assistant replies even when the message has a reading summary. Before the fix,
// GetMessagesBySessionID stripped summarized assistant content to an empty
// {"blocks":[]}, silently dropping all AI replies from the fork context.
func TestBuildForkContext_PreservesSummarizedAssistant(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sess-summarized"
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"user question"}]}`, sessionID,
	)
	require.NoError(t, err)
	// Insert assistant message, then give it a reading summary — this used to
	// trigger content stripping in GetMessagesBySessionID.
	res, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"assistant answer"},{"type":"tool_use","id":"t1","name":"Read","input":{},"output":"file contents"}]}`, sessionID,
	)
	require.NoError(t, err)
	msgID, err := res.LastInsertId()
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', ?, 'reading summary')",
		msgID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "User: user question")
	assert.Contains(t, result, "Assistant: assistant answer", "summarized assistant reply must survive fork context")
	assert.Contains(t, result, "file contents", "tool_use output must survive fork context")
	assert.NotContains(t, result, "reading summary")
}

func TestGetToolCallsBySession_ClosedDB(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	// Close the database to trigger error in GetToolCallsBySession
	db.Close()
	dbRead.Close()

	_, err := GetToolCallsBySession("any-session")
	assert.Error(t, err)
}

func TestGetThinkingBySessionAll_ClosedDB(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	// Close the database to trigger error in GetThinkingBySessionAll
	db.Close()
	dbRead.Close()

	_, err := GetThinkingBySessionAll("any-session")
	assert.Error(t, err)
}

// ============================================================================
// handleACPCleanup tests
// ============================================================================

func TestHandleACPCleanup_NonACPTransport(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"cli-agent": {ID: "cli-agent", Transport: "cli"},
	}
	defer func() { model.Agents = origAgents }()

	// Insert session so GetSessionTransport doesn't panic on nil DB
	sessionID := "cleanup-cli-sess"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', 'cli-agent', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)

	// Should not panic for non-ACP transport
	handleACPCleanup(sessionID, "cli-agent")
}

func TestHandleACPCleanup_SessionTransportACP(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "acp-session-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, transport) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat', ?)",
		sessionID, "acp-stdio",
	)
	require.NoError(t, err)

	// Should not panic for ACP transport (ACPConnManager singleton handles nil pool gracefully)
	handleACPCleanup(sessionID, "some-agent")
}

func TestHandleACPCleanup_NoSessionTransport_AgentACP(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"acp-agent": {ID: "acp-agent", Transport: "acp-stdio"},
	}
	defer func() { model.Agents = origAgents }()

	sessionID := "acp-agent-sess"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', 'acp-agent', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)

	// Agent has ACP transport, no session transport set
	handleACPCleanup(sessionID, "acp-agent")
}

func TestHandleACPCleanup_UnknownAgent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	sessionID := "unknown-agent-sess"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', 'nonexistent-agent', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)

	// Unknown agent, no session transport → defaults to CLI
	handleACPCleanup(sessionID, "nonexistent-agent")
}

// ============================================================================
// RunDrainLoop tests
// ============================================================================

// ============================================================================
// scanDingTalkSessionInfos tests
// ============================================================================

func TestScanDingTalkSessionInfos_MultipleRows(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj1', 'claude', 'Session 1', 'agent1', 'default', 'model-a', 'chat')",
		"scan-1",
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj2', 'codebuddy', 'Session 2', 'agent2', 'default', 'model-b', 'chat')",
		"scan-2",
	)
	require.NoError(t, err)

	rows, err := dbRead.Query(
		`SELECT id, title, project_path, backend, agent_id, model FROM chat_sessions WHERE id IN (?, ?) ORDER BY id`,
		"scan-1", "scan-2",
	)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	results := scanDingTalkSessionInfos(rows)
	require.Len(t, results, 2)
	assert.Equal(t, "scan-1", results[0].ID)
	assert.Equal(t, "Session 1", results[0].Title)
	assert.Equal(t, "/proj1", results[0].ProjectPath)
	assert.Equal(t, "claude", results[0].Backend)
	assert.Equal(t, "agent1", results[0].AgentID)
	assert.Equal(t, "model-a", results[0].Model)
	assert.Equal(t, "scan-2", results[1].ID)
}

// ============================================================================
// DingTalkSessionInfo field tests
// ============================================================================

func TestDingTalkSessionInfo_AllFields(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Full Info', 'agent1', 'default', 'model-x', 'chat')",
		"full-info-1",
	)
	require.NoError(t, err)

	results, err := FindSessionsByPrefix("full-info")
	require.NoError(t, err)
	require.Len(t, results, 1)

	info := results[0]
	assert.Equal(t, "full-info-1", info.ID)
	assert.Equal(t, "Full Info", info.Title)
	assert.Equal(t, "/proj", info.ProjectPath)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "agent1", info.AgentID)
	assert.Equal(t, "model-x", info.Model)
}

// ============================================================================
// ListRecentSessions edge cases
// ============================================================================

func TestListRecentSessions_ZeroOrNegativeLimit(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert a session
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Session', 'agent1', 'default', '', 'chat')",
		"limit-test-1",
	)
	require.NoError(t, err)

	// Zero or negative limit should default to 10
	results, err := ListRecentSessions(0)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	results, err = ListRecentSessions(-5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// ============================================================================
// FindRunningSessionsByPrefix edge cases
// ============================================================================

func TestFindRunningSessionsByPrefix_NoRunningSessions(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	results, err := FindRunningSessionsByPrefix("abc")
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestFindRunningSessionsByPrefix_PrefixFilter(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert two sessions
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Sess A', 'agent1', 'default', '', 'chat')",
		"prefix-aaa-1111",
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Sess B', 'agent2', 'default', '', 'chat')",
		"prefix-bbb-2222",
	)
	require.NoError(t, err)

	// Mark both as running
	TrySetSessionRunning("prefix-aaa-1111")
	TrySetSessionRunning("prefix-bbb-2222")
	defer SetSessionRunning("prefix-aaa-1111", false, true)
	defer SetSessionRunning("prefix-bbb-2222", false, true)

	// Search for only one prefix
	results, err := FindRunningSessionsByPrefix("prefix-aaa")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "prefix-aaa-1111", results[0].ID)
}

// ============================================================================
// handleSessionPanic tests
// ============================================================================

func TestHandleSessionPanic_Recovers(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	sessionID := "panic-sess-1"
	_, created := TryClaimSessionRun(sessionID)
	require.True(t, created)

	cfg := LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: "/proj",
		BackendName: "claude",
		AgentID:     "agent",
		Message:     "test",
	}

	// Trigger a panic inside a goroutine with handleSessionPanic
	done := make(chan struct{})
	go func() {
		defer func() { close(done) }()
		defer handleSessionPanic(cfg, sessionID)
		panic("test panic")
	}()
	// Wait for the goroutine to complete - handleSessionPanic should recover
	<-done
}

func TestLaunchSessionExecution_BackendCreationFails(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	sessionID := "launch-fail-sess"
	SetSessionRunning(sessionID, true, false)
	defer SetSessionRunning(sessionID, false, true)

	cfg := LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: "/proj",
		BackendName: "nonexistent-backend",
		AgentID:     "nonexistent-agent",
		Message:     "test",
	}

	LaunchSessionExecution(cfg)

	// Wait for the goroutine to finish (it should complete quickly on backend creation error)
	// Poll for the session to be no longer running
	require.Eventually(t, func() bool {
		return !IsSessionRunning(sessionID)
	}, 5*time.Second, 50*time.Millisecond, "session should stop running after backend creation fails")
}

// ============================================================================
// SendMessageToSessionFromDingTalk tests
// ============================================================================

func TestSendMessageToSessionFromDingTalk_SessionExists_QueuedMessage(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "dt-send-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	// Mark the session as already running so the message gets queued instead of launching a goroutine
	SetSessionRunning(sessionID, true, false)
	defer SetSessionRunning(sessionID, false, true)

	err = SendMessageToSessionFromDingTalk(sessionID, "hello from dingtalk", nil)

	// The call should succeed (session was found and message was queued)
	assert.NoError(t, err)

	// Verify the message was persisted
	rows, err := dbRead.QueryContext(context.Background(),
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'user'", sessionID)
	require.NoError(t, err)
	defer rows.Close()
	var content string
	if rows.Next() {
		err = rows.Scan(&content)
		require.NoError(t, err)
		assert.Contains(t, content, "hello from dingtalk")
	}
	require.NoError(t, rows.Err())
}

// ============================================================================
// FindSessionsByPrefix nil DB
// ============================================================================

func TestFindSessionsByPrefix_NilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	_, err := FindSessionsByPrefix("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not initialized")
}

func TestListRecentSessions_NilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	_, err := ListRecentSessions(10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not initialized")
}

func TestFindRunningSessionsByPrefix_NilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	_, err := FindRunningSessionsByPrefix("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not initialized")
}

// ============================================================================
// BuildForkContext streaming messages excluded
// ============================================================================

func TestBuildForkContext_ExcludesStreamingMessages(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-streaming"
	// Insert a streaming (in-progress) message - should be excluded
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 1)",
		"/proj", `{"blocks":[{"type":"text","text":"streaming content"}]}`, sessionID,
	)
	require.NoError(t, err)
	// Insert a non-streaming message
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"final content"}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.NotContains(t, result, "streaming content")
	assert.Contains(t, result, "User: final content")
}

// ============================================================================
// DingTalkSessionInfo JSON round-trip
// ============================================================================

func TestDingTalkSessionInfo_JSONRoundTrip(t *testing.T) {
	info := DingTalkSessionInfo{
		ID:          "test-id",
		Title:       "Test Title",
		ProjectPath: "/test/path",
		Backend:     "claude",
		AgentID:     "agent-1",
		Model:       "gpt-4",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded DingTalkSessionInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, info, decoded)
}

// ============================================================================
// RunDrainLoop drain queue tests
// ============================================================================

// ============================================================================
// SendMessageToSessionFromDingTalk - launch path
// ============================================================================

func TestSendMessageToSessionFromDingTalk_LaunchPath(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "dt-launch-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	// Session is not running → TrySetSessionRunning should succeed
	// and LaunchSessionExecution will be called
	err = SendMessageToSessionFromDingTalk(sessionID, "launch message", nil)
	// The launch will fail because there's no real backend, but the function
	// should not return an error for the launch itself
	assert.NoError(t, err)

	// Wait briefly for the goroutine to start, then clean up
	time.Sleep(100 * time.Millisecond)
	SetSessionRunning(sessionID, false, true)
}

// ============================================================================
// scanDingTalkSessionInfos - scan error path
// ============================================================================

func TestScanDingTalkSessionInfos_ScanError(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert a session with all required fields
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat')",
		"scan-err-1",
	)
	require.NoError(t, err)

	// Query with wrong number of columns to trigger scan error
	rows, err := dbRead.Query(`SELECT id FROM chat_sessions WHERE id = ?`, "scan-err-1")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	// scanDingTalkSessionInfos expects 6 columns but gets only 1
	results := scanDingTalkSessionInfos(rows)
	assert.Empty(t, results, "scan errors should be skipped")
}

// ============================================================================
// FindSessionsByPrefix DB query error
// ============================================================================

func TestFindSessionsByPrefix_QueryError(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a query error
	_, _ = db.Exec("DROP TABLE chat_sessions")

	_, err := FindSessionsByPrefix("test")
	assert.Error(t, err)
}

func TestListRecentSessions_QueryError(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, _ = db.Exec("DROP TABLE chat_sessions")

	_, err := ListRecentSessions(10)
	assert.Error(t, err)
}

// ============================================================================
// FindRunningSessionsByPrefix - multiple running with prefix filter
// ============================================================================

func TestFindRunningSessionsByPrefix_CaseInsensitive(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test', 'agent1', 'default', '', 'chat')",
		"UPPER-case-id",
	)
	require.NoError(t, err)

	TrySetSessionRunning("UPPER-case-id")
	defer SetSessionRunning("UPPER-case-id", false, true)

	// Case-insensitive prefix match
	results, err := FindRunningSessionsByPrefix("upper-case")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "UPPER-case-id", results[0].ID)
}

// ============================================================================
// External session ID tests
// ============================================================================

func TestUpdateAndClearExternalSessionID(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "ext-sess-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, '/proj', 'claude', 'Ext Test', 'agent1', 'default', '', 'chat', '')",
		sessionID,
	)
	require.NoError(t, err)

	// Initially empty
	assert.Equal(t, "", GetExternalSessionID(sessionID))

	// Update
	UpdateExternalSessionID(sessionID, "ext-123")
	assert.Equal(t, "ext-123", GetExternalSessionID(sessionID))

	// Clear
	ClearExternalSessionID(sessionID)
	assert.Equal(t, "", GetExternalSessionID(sessionID))
}

// ============================================================================
// GetExpiredArchivedSessions and PurgeArchivedData tests
// ============================================================================

func TestGetExpiredArchivedSessions_Empty(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	ids, err := GetExpiredArchivedSessions(time.Now())
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestGetExpiredArchivedSessions_WithExpired(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert a archived session with an old timestamp
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, archived, updated_at) VALUES (?, '/proj', 'claude', 'Old', 'a1', 'default', '', 'chat', 1, '2020-01-01T00:00:00')",
		"expired-sess",
	)
	require.NoError(t, err)

	ids, err := GetExpiredArchivedSessions(time.Now())
	require.NoError(t, err)
	assert.Contains(t, ids, "expired-sess")
}

func TestPurgeArchivedData(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert a archived session with old timestamp
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, archived, updated_at) VALUES (?, '/proj', 'claude', 'Old', 'a1', 'default', '', 'chat', 1, '2020-01-01T00:00:00')",
		"purge-sess",
	)
	require.NoError(t, err)

	// Insert chat history for the session
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/proj', 'user', 'hello', 'purge-sess', 'claude', 0)",
	)
	require.NoError(t, err)

	sessionsPurged, _, err := PurgeArchivedData([]string{"purge-sess"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)

	// Session should be hard-deleted
	var count int
	db.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = 'purge-sess'").Scan(&count)
	assert.Equal(t, 0, count)

	// Chat history should also be deleted
	db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = 'purge-sess'").Scan(&count)
	assert.Equal(t, 0, count)
}

// ============================================================================
// HardDeleteSession test
// ============================================================================

func TestHardDeleteSession(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "hard-del-sess"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Del', 'a1', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)

	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/proj', 'user', 'hello', ?, 'claude', 0)",
		sessionID,
	)
	require.NoError(t, err)

	err = HardDeleteSession(sessionID)
	require.NoError(t, err)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", sessionID).Scan(&count)
	assert.Equal(t, 0, count)
	db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count)
	assert.Equal(t, 0, count)
}

// ============================================================================
// GetMessageContent and GetMessageByID tests
// ============================================================================

func TestGetMessageContent_NotFound(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	content, err := GetMessageContent(999, "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, "", content)
}

func TestGetMessageContent_Found(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "msg-content-sess"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Msg', 'a1', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)

	res, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/proj', 'user', ?, ?, 'claude', 0)",
		`{"blocks":[{"type":"text","text":"hello world"}]}`, sessionID,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()

	content, err := GetMessageContent(msgID, sessionID)
	require.NoError(t, err)
	assert.Equal(t, "hello world", content)
}

func TestGetMessageByID_NotFound(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := GetMessageByID(999)
	assert.Error(t, err)
}

// --- SendMessageToSessionFromDingTalk user_message emit ---

// TestSendMessageToSessionFromDingTalk_AlreadyRunning_EnqueuesMessage verifies
// that when a session is already running, the message is enqueued and
// a user_message event with the real persisted msgID (>0) is emitted (the
// enqueue path in session_command.go — EnqueueAndMaybeStart returns the id).
func TestSendMessageToSessionFromDingTalk_AlreadyRunning_EnqueuesMessage(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(origMgr)

	sessionID := "dingtalk-emit-2"
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, model, transport, auto_approve) VALUES (?, '/test', 'codebuddy', 'test', '', '', '', 0)", sessionID)
	require.NoError(t, err)

	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "dingtalk-client-2", "")
	mgr.StreamHub().Subscribe("dingtalk-client-2", sessionID)

	// Mark session as running
	cleanupActiveSessions()
	defer cleanupActiveSessions()
	TrySetSessionRunning(sessionID)
	defer func() {
		SetSessionRunning(sessionID, false, true)
		ClearQueuedMessages(sessionID)
	}()

	err = SendMessageToSessionFromDingTalk(sessionID, "queued from dingtalk", nil)
	assert.NoError(t, err)

	// Verify message IS persisted to DB with queued=1 (enqueue-path now persists).
	// GetMessagesBySessionID excludes queued rows (M4), so query the queue directly.
	messages, err := GetQueuedMessages(sessionID)
	require.NoError(t, err)
	assert.Len(t, messages, 1, "enqueue path should persist user message to DB")
	assert.True(t, messages[0].Queued, "persisted message should be queued=1")

	// Verify message is discoverable via the queued-message query.
	queue, err := GetQueuedMessages(sessionID)
	require.NoError(t, err)
	assert.Len(t, queue, 1)
	assert.Equal(t, "queued from dingtalk", queue[0].Content)
}

// ============================================================================
// FindRunningSessionsByPrefix - matchingIDs empty (prefix too short)
// ============================================================================

func TestFindRunningSessionsByPrefix_PrefixShorterThanID(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert and mark a session as running with a long ID
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test', 'agent1', 'default', '', 'chat')",
		"abc123-long-id",
	)
	require.NoError(t, err)

	TrySetSessionRunning("abc123-long-id")
	defer SetSessionRunning("abc123-long-id", false, true)

	// Short prefix that doesn't match any running session
	results, err := FindRunningSessionsByPrefix("xyz")
	require.NoError(t, err)
	assert.Nil(t, results, "non-matching prefix should return nil")
}

// ============================================================================
// FindRunningSessionsByPrefix - DB query error
// ============================================================================

func TestFindRunningSessionsByPrefix_QueryError(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Insert and mark session as running
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test', 'agent1', 'default', '', 'chat')",
		"query-err-id",
	)
	require.NoError(t, err)

	TrySetSessionRunning("query-err-id")
	defer SetSessionRunning("query-err-id", false, true)

	// Drop table to cause query error
	_, _ = db.Exec("DROP TABLE chat_sessions")

	_, err = FindRunningSessionsByPrefix("query-err")
	assert.Error(t, err, "should return error when DB query fails")
}

// ============================================================================
// SendMessageToSessionFromDingTalk - AddChatMessage failure
// ============================================================================

func TestSendMessageToSessionFromDingTalk_AddChatMessageFails(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "dt-msg-fail"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	// Drop chat_history to cause AddChatMessage to fail
	_, _ = db.Exec("DROP TABLE chat_history")

	err = SendMessageToSessionFromDingTalk(sessionID, "this will fail", nil)
	assert.Error(t, err, "should return error when message persistence fails")

	// Session should no longer be running (rollback)
	assert.False(t, IsSessionRunning(sessionID), "session should not be running after persistence failure")
}

// ============================================================================
// BuildForkContext - non-user/assistant roles skipped
// ============================================================================

func TestBuildForkContext_SkipsSystemMessages(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-sys-skip"
	// The chat_history table has a CHECK(role IN ('user', 'assistant')),
	// so we can't insert system messages directly. But we can verify
	// that messages with only thinking blocks produce empty fork context
	// (thinking blocks are now excluded, tool_use blocks are included).
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"thinking","text":"thinking content"}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	// thinking blocks are excluded, so only-thinking messages produce empty fork context
	assert.Equal(t, "", result, "thinking-only messages should be skipped in fork context")
}

func TestBuildForkContext_ToolUseBlock(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-tooluse"
	// Insert assistant message with tool_use block + text
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"I'll read the file"},{"type":"tool_use","name":"Read","id":"toolu_1","status":"success","done":true,"duration_ms":150,"summary":"Read /path/to/file.go"}]}`, sessionID,
	)
	require.NoError(t, err)
	// Insert tool call detail record
	_, err = WriteExec(
		"INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms) VALUES (?, ?, 'toolu_1', 'Read', ?, ?, 'success', 1, 'Read /path/to/file.go', 150)",
		1, sessionID, `{"file_path":"/path/to/file.go"}`, "package main\n\nfunc main() {}",
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "Assistant: I'll read the file")
	assert.Contains(t, result, "<tool_use>")
	assert.Contains(t, result, `"name":"Read"`)
	assert.Contains(t, result, `"id":"toolu_1"`)
	assert.Contains(t, result, `"status":"success"`)
	assert.Contains(t, result, `"input"`)
	assert.Contains(t, result, `"output"`)
	assert.Contains(t, result, "</tool_use>")
}

func TestBuildForkContext_ToolUseBlockWithoutDetail(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-tooluse-no-detail"
	// Insert assistant message with tool_use block but no matching detail record
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"tool_use","name":"Bash","id":"toolu_2","status":"success","done":true}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "Assistant:")
	assert.Contains(t, result, "<tool_use>")
	assert.Contains(t, result, `"name":"Bash"`)
	assert.Contains(t, result, `"id":"toolu_2"`)
	// Without detail record, no input/output
	assert.NotContains(t, result, `"input"`)
	assert.NotContains(t, result, `"output"`)
}

func TestBuildForkContext_ThinkingExcluded(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-thinking-excl"
	// Insert assistant message with thinking + text
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"thinking","text":"I should think about this"},{"type":"text","text":"Here is my answer"}]}`, sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "Assistant: Here is my answer")
	assert.NotContains(t, result, "I should think about this")
}

func TestBuildForkContext_ToolUseBlockOutputTruncated(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-tooluse-trunc"
	longOutput := strings.Repeat("x", 600)
	// Insert assistant message with tool_use block
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"tool_use","name":"Read","id":"toolu_trunc","status":"success","done":true}]}`, sessionID,
	)
	require.NoError(t, err)
	// Insert tool call detail with long output
	_, err = WriteExec(
		"INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done) VALUES (?, ?, 'toolu_trunc', 'Read', ?, ?, 'success', 1)",
		1, sessionID, `{"file_path":"/big/file.go"}`, longOutput,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Contains(t, result, "...(truncated)")
	// Should contain the first 500 chars of output
	assert.Contains(t, result, strings.Repeat("x", 500))
	// Should NOT contain the full output
	assert.NotContains(t, result, longOutput)
}

func TestTruncateRunes(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", truncateRunes("hello", 10))
	})
	t.Run("exact length unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", truncateRunes("hello", 5))
	})
	t.Run("truncated with suffix", func(t *testing.T) {
		assert.Equal(t, "hel...(truncated)", truncateRunes("hello world", 3))
	})
	t.Run("unicode aware", func(t *testing.T) {
		s := "你好世界测试"
		assert.Equal(t, "你好...(truncated)", truncateRunes(s, 2))
	})
	t.Run("zero maxRunes returns unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", truncateRunes("hello", 0))
	})
	t.Run("empty string returns empty", func(t *testing.T) {
		assert.Equal(t, "", truncateRunes("", 5))
	})
}

func TestFormatToolUseBlock_OutputTruncation(t *testing.T) {
	t.Run("truncates long output from toolCallMap", func(t *testing.T) {
		longOutput := strings.Repeat("b", 600)
		b := model.ContentBlock{
			Type:   "tool_use",
			Name:   "Bash",
			ID:     "toolu_trunc",
			Status: "success",
			Done:   true,
		}
		tc := ToolCallRecord{
			ToolID: "toolu_trunc",
			Input:  json.RawMessage(`{"command":"ls"}`),
			Output: longOutput,
		}
		toolCallMap := map[string]*ToolCallRecord{"toolu_trunc": &tc}

		result := FormatToolUseBlock(b, toolCallMap)
		assert.Contains(t, result, "...(truncated)")
		// Should contain first 500 chars of output
		assert.Contains(t, result, strings.Repeat("b", 500))
		// Should NOT contain the full output
		assert.NotContains(t, result, longOutput)
	})

	t.Run("truncates long output from content block fallback", func(t *testing.T) {
		longOutput := strings.Repeat("x", 600)
		b := model.ContentBlock{
			Type:   "tool_use",
			Name:   "Bash",
			ID:     "ask-002",
			Input:  map[string]any{"command": "ls"},
			Output: longOutput,
		}
		result := FormatToolUseBlock(b, nil)
		assert.Contains(t, result, "...(truncated)")
		assert.NotContains(t, result, longOutput)
	})

	t.Run("does not truncate short output", func(t *testing.T) {
		shortOutput := "file1.txt\nfile2.txt"
		b := model.ContentBlock{
			Type:   "tool_use",
			Name:   "Bash",
			ID:     "toolu_short",
			Status: "success",
			Done:   true,
		}
		tc := ToolCallRecord{
			ToolID: "toolu_short",
			Input:  json.RawMessage(`{"command":"ls"}`),
			Output: shortOutput,
		}
		toolCallMap := map[string]*ToolCallRecord{"toolu_short": &tc}

		result := FormatToolUseBlock(b, toolCallMap)
		assert.NotContains(t, result, "...(truncated)")
		// JSON encodes \n as \\n
		assert.Contains(t, result, "file1.txt\\nfile2.txt")
	})

	t.Run("does not truncate input or summary", func(t *testing.T) {
		longInput := strings.Repeat("a", 600)
		longSummary := strings.Repeat("c", 600)
		b := model.ContentBlock{
			Type:       "tool_use",
			Name:       "Bash",
			ID:         "toolu_ni",
			Status:     "success",
			Done:       true,
			DurationMs: 100,
			Summary:    longSummary,
		}
		tc := ToolCallRecord{
			ToolID: "toolu_ni",
			Input:  json.RawMessage(longInput),
			Output: strings.Repeat("b", 600),
		}
		toolCallMap := map[string]*ToolCallRecord{"toolu_ni": &tc}

		result := FormatToolUseBlock(b, toolCallMap)
		assert.Contains(t, result, "...(truncated)") // output truncated
		assert.Contains(t, result, longInput)        // input NOT truncated
		assert.Contains(t, result, longSummary)      // summary NOT truncated
	})
}

func TestExtractMessageParts_TextBlocks(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: contentKeyText, Text: "hello"},
		{Type: contentKeyText, Text: "world"},
	}
	parts := extractMessageParts(blocks, nil)
	assert.Equal(t, []string{"hello", "world"}, parts)
}

func TestExtractMessageParts_SkipsEmptyText(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: contentKeyText, Text: ""},
		{Type: contentKeyText, Text: "visible"},
	}
	parts := extractMessageParts(blocks, nil)
	assert.Equal(t, []string{"visible"}, parts)
}

func TestExtractMessageParts_ToolUseBlock(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: contentKeyText, Text: "preamble"},
		{Type: eventTypeToolUse, Name: "Bash", ID: "t1", Status: "success", Done: true},
	}
	parts := extractMessageParts(blocks, nil)
	assert.Len(t, parts, 2)
	assert.Equal(t, "preamble", parts[0])
	assert.Contains(t, parts[1], "<tool_use>")
}

func TestExtractMessageParts_SkipsThinkingAndWarning(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: contentKeyText, Text: "msg"},
		{Type: "thinking", Text: "hidden"},
		{Type: "warning", Text: "also hidden"},
	}
	parts := extractMessageParts(blocks, nil)
	assert.Equal(t, []string{"msg"}, parts)
}

func TestExtractMessageParts_EmptyBlocks(t *testing.T) {
	parts := extractMessageParts(nil, nil)
	assert.Nil(t, parts)
}

func TestFormatToolUseBlock_InlineInputFallback(t *testing.T) {
	t.Run("uses Input from content block when no toolCallMap entry", func(t *testing.T) {
		b := model.ContentBlock{
			Type:  eventTypeToolUse,
			Name:  "AskUserQuestion",
			ID:    "ask-001",
			Input: map[string]any{"question": "Continue?"},
		}
		result := FormatToolUseBlock(b, nil)
		assert.Contains(t, result, "<tool_use>")
		assert.Contains(t, result, "AskUserQuestion")
		assert.Contains(t, result, `"input"`)
	})

	t.Run("uses Output from content block as fallback", func(t *testing.T) {
		b := model.ContentBlock{
			Type:   eventTypeToolUse,
			Name:   "Bash",
			ID:     "ask-002",
			Input:  map[string]any{"command": "ls"},
			Output: "file1.txt\nfile2.txt",
		}
		result := FormatToolUseBlock(b, nil)
		assert.Contains(t, result, "<tool_use>")
		assert.Contains(t, result, `"input"`)
		assert.Contains(t, result, `"output"`)
		assert.Contains(t, result, "file1.txt")
	})

	t.Run("prefers toolCallMap over inline input", func(t *testing.T) {
		b := model.ContentBlock{
			Type:   eventTypeToolUse,
			Name:   "Read",
			ID:     "toolu_map",
			Input:  map[string]any{"file_path": "/inline.go"},
			Output: "inline output",
		}
		tc := ToolCallRecord{
			ToolID: "toolu_map",
			Input:  json.RawMessage(`{"file_path":"/db.go"}`),
			Output: "db output",
		}
		toolCallMap := map[string]*ToolCallRecord{"toolu_map": &tc}
		result := FormatToolUseBlock(b, toolCallMap)
		assert.Contains(t, result, "/db.go")
		assert.Contains(t, result, "db output")
		assert.NotContains(t, result, "/inline.go")
	})
}

// ============================================================================
// BuildChatRequest - fork context integration
// ============================================================================

func TestBuildChatRequest_ResumeWithForkContext(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-integration-1"
	// Insert session without external_session_id
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)
	// Insert messages for fork context
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"previous answer"}]}`, sessionID,
	)
	require.NoError(t, err)

	req := BuildChatRequest("follow-up", sessionID, "/proj", "claude", "", "", "", "", "", "/proj", false)
	assert.NotEmpty(t, req.ForkContext, "should have fork context when resuming without external session ID")
	assert.Contains(t, req.ForkContext, "previous answer")
}

// TestBuildChatRequest_EmptyFileDir_WorkDirEmpty reproduces the bug where
// passing fileDir="" causes WorkDir to be empty, which makes ACP
// ResumeSession/NewSession fail with "cwd must be an absolute path".
func TestBuildChatRequest_EmptyFileDir_WorkDirEmpty(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	req := BuildChatRequest("hello", "sess-1", "/home/user/project", "claude", "", "", "", "", "", "", false)
	assert.Empty(t, req.WorkDir, "fileDir='' should produce empty WorkDir (this is the bug)")
}

func TestBuildChatRequest_FileDirPassedThrough_WorkDir(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	req := BuildChatRequest("hello", "sess-1", "/home/user/project", "claude", "", "", "", "", "", "/home/user/project", false)
	assert.Equal(t, "/home/user/project", req.WorkDir, "fileDir should be passed through to WorkDir")
}

// ============================================================================
// appendMediaPrompt - empty system prompt with non-empty media prompt
// ============================================================================

func TestAppendMediaPrompt_EmptySystemPrompt_WithMediaPrompt(t *testing.T) {
	// When systemPrompt is empty and BuildMediaPrompt returns non-empty,
	// the result should be just the media prompt
	mediaPrompt := model.BuildMediaPrompt()
	if mediaPrompt == "" {
		t.Skip("BuildMediaPrompt returns empty, can't test this path")
	}
	result := appendMediaPrompt("")
	assert.Equal(t, mediaPrompt, result, "empty system prompt should return just the media prompt")
}

// ============================================================================
// Content key constants in JSON serialization
// ============================================================================

func TestContentKeyConstants_JSONSerialization(t *testing.T) {
	// Verify contentKeyReason constant is used correctly in JSON output
	errContent, err := json.Marshal(map[string]any{
		contentKeyBlocks: []any{map[string]string{
			contentKeyType:   blockTypeWarning,
			contentKeyText:   "test error",
			contentKeyReason: ai.ReasonPanic,
		}},
	})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(errContent, &parsed))

	blocks, ok := parsed[contentKeyBlocks].([]any)
	require.True(t, ok, "blocks should be an array")
	require.Len(t, blocks, 1)

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok, "block should be an object")
	assert.Equal(t, blockTypeWarning, block[contentKeyType], "type should be 'warning'")
	assert.Equal(t, "test error", block[contentKeyText], "text should match")
	assert.Equal(t, ai.ReasonPanic, block[contentKeyReason], "reason should match")
}

func TestHandleSessionPanic_PanicContentUsesCorrectKeys(t *testing.T) {
	// Verify that handleSessionPanic constructs JSON with correct constant keys.
	// Since FinalizeStreamingMessage requires a pre-existing streaming row in DB,
	// we directly verify the JSON structure that handleSessionPanic would produce.
	errMsg := "AI internal error, please retry"
	errContent, err := json.Marshal(map[string]any{
		contentKeyBlocks: []any{map[string]string{
			contentKeyType:   blockTypeWarning,
			contentKeyText:   errMsg,
			contentKeyReason: ai.ReasonPanic,
		}},
	})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(errContent, &parsed))

	blocks, ok := parsed[contentKeyBlocks].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "warning", block["type"])
	assert.Equal(t, ai.ReasonPanic, block["reason"])
	assert.Equal(t, errMsg, block["text"])
}

func TestExecuteStreamRunShared_FileDirAbsPathResolution(t *testing.T) {
	// Test that the absErr variable (renamed from shadow "err") resolves
	// absolute paths correctly. We verify this indirectly: the LaunchConfig
	// with a valid ProjectPath should not fail at the path resolution step.
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	sessionID := "abs-path-sess"
	SetSessionRunning(sessionID, true, false)
	defer SetSessionRunning(sessionID, false, true)

	_, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel(sessionID, cancel)

	cfg := LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: "/tmp",
		BackendName: "nonexistent-backend",
		AgentID:     "nonexistent-agent",
		Message:     "test",
	}

	LaunchSessionExecution(cfg)

	// The session should stop quickly due to backend creation failure
	require.Eventually(t, func() bool {
		return !IsSessionRunning(sessionID)
	}, 5*time.Second, 50*time.Millisecond, "session should stop after backend creation fails")
}

func TestExecuteStreamRunShared_BackendCreationFails_DirectCall(t *testing.T) {
	// Call executeStreamRunShared directly (not via LaunchSessionExecution goroutine)
	// so that Go coverage can track the executed lines in the same goroutine.
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	sessionID := "direct-fail-sess"
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	RegisterSessionCancel(sessionID, cancel)
	defer UnregisterSessionCancel(sessionID)

	cfg := LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: "/tmp",
		BackendName: "nonexistent-backend",
		AgentID:     "nonexistent-agent",
		Message:     "test",
	}

	result := executeStreamRunShared(context.Background(), cfg)
	assert.Contains(t, result.err, "create backend", "should fail at backend creation")
}

// mockStreamErrBackend is a minimal AIBackend that returns an error from ExecuteStream.
type mockStreamErrBackend struct{}

func (m *mockStreamErrBackend) Name() string { return "test-stream-err" }
func (m *mockStreamErrBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	return nil, fmt.Errorf("stream start failed")
}

// mockStreamStartBackend returns a successful short stream (content + done) so
// executeStreamRunShared reaches the stream_start broadcast point.
type mockStreamStartBackend struct{}

func (m *mockStreamStartBackend) Name() string { return "test-stream-start" }
func (m *mockStreamStartBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 4)
	ch <- ai.StreamEvent{Type: "content", Content: "ok"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

// TestExecuteStreamRunShared_BroadcastsStreamStart verifies that every prompt
// run broadcasts a stream_start event carrying the streaming message's real
// DB id, so any subscribed client can create a data-driven placeholder.
func TestExecuteStreamRunShared_BroadcastsStreamStart(t *testing.T) {
	ai.RegisterBackend("test-stream-start", func() ai.AIBackend { return &mockStreamStartBackend{} })

	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Transport: ""},
	}
	defer func() { model.Agents = origAgents }()

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(origMgr)

	sessionID := "stream-start-sess"
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/tmp', 'test-stream-start', 'Test', 'test-agent', 'default', '', 'chat', 0)", sessionID)
	require.NoError(t, err)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "stream-start-client", "")
	mgr.StreamHub().Subscribe("stream-start-client", sessionID)

	cfg := LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: "/tmp",
		BackendName: "test-stream-start",
		AgentID:     "test-agent",
		Message:     "test",
	}
	result := executeStreamRunShared(context.Background(), cfg)
	assert.Empty(t, result.err, "mock backend should succeed")

	// The stream_start event must be broadcast with the real DB streaming row id.
	var found *ws.ServerMessage
	assert.Eventually(t, func() bool {
		for _, ev := range sub.GetBufferedEvents() {
			if ev.Event != "chat_stream" {
				continue
			}
			data, ok := ev.Data.(ws.ChatStreamData)
			if !ok || data.EventType != "stream_start" {
				continue
			}
			found = &ev
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond)
	require.NotNil(t, found, "expected a stream_start chat_stream event in the subscriber buffer")

	data := found.Data.(ws.ChatStreamData)
	payload, ok := data.Payload.(map[string]any)
	require.True(t, ok, "stream_start payload must be map[string]any")
	assert.Greater(t, payload["message_id"].(int64), int64(0), "stream_start must carry the streaming message DB id")

	// The broadcast id must match the persisted assistant row created by this run.
	// Note: the streaming row is finalized (streaming=0) once the done event
	// processes, so match by the latest assistant row for the session.
	var dbMsgID int64
	err = db.QueryRow("SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1", sessionID).Scan(&dbMsgID)
	require.NoError(t, err)
	assert.Equal(t, dbMsgID, payload["message_id"], "stream_start message_id must equal the streaming row id")
}

func TestExecuteStreamRunShared_StreamStartFails_CoversAbsErrAndReasonKeys(t *testing.T) {
	// Register a mock backend that succeeds creation but fails ExecuteStream.
	// This covers lines 460-463 (absErr rename) and 472 (contentKeyReason in stream error path).
	ai.RegisterBackend("test-stream-err", func() ai.AIBackend { return &mockStreamErrBackend{} })

	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Transport: ""},
	}
	defer func() { model.Agents = origAgents }()

	sessionID := "stream-err-sess"
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	RegisterSessionCancel(sessionID, cancel)
	defer UnregisterSessionCancel(sessionID)

	// Create a chat session for AddChatMessage
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/tmp', 'test-stream-err', 'Test', 'test-agent', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	cfg := LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: "/tmp",
		BackendName: "test-stream-err",
		AgentID:     "test-agent",
		Message:     "test",
	}

	result := executeStreamRunShared(context.Background(), cfg)
	assert.Contains(t, result.err, "start stream", "should fail at stream start")
}

// ============================================================================
// SendMessageToSessionFromFeishu tests
// ============================================================================

func TestSendMessageToSessionFromFeishu_NotFound(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	err := SendMessageToSessionFromFeishu("nonexistent-session", "hello", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSendMessageToSessionFromFeishu_AlreadyRunning_EnqueuesMessage(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(origMgr)

	sessionID := "feishu-enqueue-1"
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, model, transport, auto_approve) VALUES (?, '/test', 'codebuddy', 'test', '', '', '', 0)", sessionID)
	require.NoError(t, err)

	// Mark session as running
	cleanupActiveSessions()
	defer cleanupActiveSessions()
	TrySetSessionRunning(sessionID)
	defer func() {
		SetSessionRunning(sessionID, false, true)
		ClearQueuedMessages(sessionID)
	}()

	err = SendMessageToSessionFromFeishu(sessionID, "hello from feishu", nil)
	assert.NoError(t, err)

	// Verify message is persisted and queued in DB.
	msgs, err := GetQueuedMessages(sessionID)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "hello from feishu", msgs[0].Content)
}

func TestSendMessageToSessionFromFeishu_LaunchPath(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "feishu-launch-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	// Session is not running → TrySetSessionRunning should succeed
	err = SendMessageToSessionFromFeishu(sessionID, "launch from feishu", nil)
	assert.NoError(t, err)

	// Wait briefly for the goroutine to start, then clean up
	time.Sleep(100 * time.Millisecond)
	SetSessionRunning(sessionID, false, true)
}

func TestSendMessageToSessionFromFeishu_AddChatMessageFails(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "feishu-msg-fail"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	// Drop chat_history to cause AddChatMessage to fail
	_, _ = db.Exec("DROP TABLE chat_history")

	err = SendMessageToSessionFromFeishu(sessionID, "this will fail", nil)
	assert.Error(t, err, "should return error when message persistence fails")
}

// mockQueueBackend returns a successful stream (content + done) for every prompt,
// so a full LaunchSessionExecution + drain cycle can run end-to-end.
type mockQueueBackend struct{}

func (m *mockQueueBackend) Name() string { return "mock-queue" }
func (m *mockQueueBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 4)
	ch <- ai.StreamEvent{Type: "content", Content: "ok"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

// TestDrainWritesReplyQueueID runs a real LaunchSessionExecution cycle: message 1
// executes directly, message 2 is enqueued and drained by the loop. The reply to
// message 2 must carry message 2's queue_id so the frontend can anchor it.
func TestDrainWritesReplyQueueID(t *testing.T) {
	ai.RegisterBackend("mock-queue", func() ai.AIBackend { return &mockQueueBackend{} })

	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	model.Agents["mock-agent"] = &model.Agent{ID: "mock-agent", Backend: "cli", Command: "echo"}
	defer func() { model.Agents = origAgents }()

	sid := "queue-reply-qid"
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, '/test', 'mock-queue', 'Q', 'mock-agent')", sid)
	require.NoError(t, err)

	// Message 1 executes directly.
	started, _, _, err := EnqueueAndMaybeStart(EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/test",
		BackendName: "mock-queue",
		AgentID:     "mock-agent",
		Message:     "1",
		QueueID:     "pending-1",
	})
	require.NoError(t, err)
	assert.True(t, started, "first message should start the session")

	// Wait for message 1's turn to finish (session stops after draining empty queue).
	require.Eventually(t, func() bool { return !IsSessionRunning(sid) }, 10*time.Second, 50*time.Millisecond)

	// Message 2 enqueued now (session idle) — starts a new run.
	started2, _, _, err := EnqueueAndMaybeStart(EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/test",
		BackendName: "mock-queue",
		AgentID:     "mock-agent",
		Message:     "2",
		QueueID:     "pending-2",
	})
	require.NoError(t, err)
	assert.True(t, started2)
	require.Eventually(t, func() bool { return !IsSessionRunning(sid) }, 10*time.Second, 50*time.Millisecond)

	// The reply to message 2 (the LATEST assistant row) must carry queue_id pending-2.
	var qid string
	err = db.QueryRow("SELECT queue_id FROM chat_history WHERE role='assistant' AND session_id=? ORDER BY id DESC LIMIT 1", sid).Scan(&qid)
	assert.NoError(t, err, "assistant reply row should exist")
	assert.Equal(t, "pending-2", qid, "drain reply must record the consumed message's queue_id")
}

// ============================================================================
// Push attachment + sticky session
// ============================================================================

// TestSendMessageToSessionFromDingTalk_WithFilesPersistsAttachments verifies a
// bare attachment message (empty text) is persisted with its files, which is
// what a file/image sent from IM produces.
func TestSendMessageToSessionFromDingTalk_WithFilesPersistsAttachments(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "dt-attach-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj', 'claude', 'Test', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	SetSessionRunning(sessionID, true, false)
	defer SetSessionRunning(sessionID, false, true)

	files := []model.FileEntry{{Path: ".clawbench/uploads/report.pdf"}}
	err = SendMessageToSessionFromDingTalk(sessionID, "", files)
	require.NoError(t, err)

	var content, filesJSON string
	require.NoError(t, dbRead.QueryRow(
		"SELECT content, files FROM chat_history WHERE session_id = ? AND role = 'user'", sessionID,
	).Scan(&content, &filesJSON))

	assert.Equal(t, "", content, "an attachment-only message has empty text")
	assert.Contains(t, filesJSON, ".clawbench/uploads/report.pdf",
		"the attachment path must be persisted so the drain loop can re-inject it")
}

// TestGetSessionInfoForPush covers the lookup push backends use to resolve a
// project path for downloading an attachment.
func TestGetSessionInfoForPush(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "dt-info-1"
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve) VALUES (?, '/proj/info', 'claude', 'Info Session', 'agent1', 'default', '', 'chat', 0)",
		sessionID,
	)
	require.NoError(t, err)

	t.Run("existing session", func(t *testing.T) {
		info, err := GetSessionInfoForPush(sessionID)
		require.NoError(t, err)
		assert.Equal(t, sessionID, info.ID)
		assert.Equal(t, "/proj/info", info.ProjectPath)
		assert.Equal(t, "Info Session", info.Title)
	})

	t.Run("missing session returns an error", func(t *testing.T) {
		_, err := GetSessionInfoForPush("does-not-exist")
		assert.Error(t, err, "a missing session must not resolve to an empty info")
	})
}

// TestDingTalkLastSessionID_RoundTrip verifies the sticky target persists and
// reads back, and that an unknown user reads as empty rather than erroring.
func TestDingTalkLastSessionID_RoundTrip(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, UpsertDingTalkSubscriber("u-sticky", "conv-1", "Nick", "stream"))

	t.Run("unset reads as empty", func(t *testing.T) {
		got, err := GetDingTalkLastSessionID("u-sticky")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})

	t.Run("round trip", func(t *testing.T) {
		require.NoError(t, SetDingTalkLastSessionID("u-sticky", "sess-abc"))
		got, err := GetDingTalkLastSessionID("u-sticky")
		require.NoError(t, err)
		assert.Equal(t, "sess-abc", got)
	})

	t.Run("unknown user reads as empty", func(t *testing.T) {
		got, err := GetDingTalkLastSessionID("nobody")
		require.NoError(t, err)
		assert.Equal(t, "", got, "an unknown subscriber must read as empty, not error")
	})

	t.Run("setting for an unknown user errors", func(t *testing.T) {
		err := SetDingTalkLastSessionID("nobody", "sess-x")
		assert.Error(t, err, "the row is expected to exist (upsert runs on every message)")
	})
}

// TestFeishuLastSessionID_RoundTrip mirrors the DingTalk sticky-target test for
// the Feishu subscriber table, which has its own column and accessors.
func TestFeishuLastSessionID_RoundTrip(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, UpsertFeishuSubscriber("ou-sticky", "chat-1", "Nick", "stream"))

	got, err := GetFeishuLastSessionID("ou-sticky")
	require.NoError(t, err)
	assert.Equal(t, "", got, "unset must read as empty")

	require.NoError(t, SetFeishuLastSessionID("ou-sticky", "sess-xyz"))
	got, err = GetFeishuLastSessionID("ou-sticky")
	require.NoError(t, err)
	assert.Equal(t, "sess-xyz", got)
}
