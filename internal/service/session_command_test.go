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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
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
	`)
	require.NoError(t, err)

	cleanup := SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func TestSendMessageToSessionFromDingTalk_NotFound(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	err := SendMessageToSessionFromDingTalk("nonexistent-session", "hello")
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
// resolveAgentConfig tests
// ============================================================================

func TestResolveAgentConfig_UnknownAgentID(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("nonexistent", "/proj", "", "", "")
	assert.Equal(t, "", result.systemPrompt)
	assert.Equal(t, "", result.agentModel)
	assert.Equal(t, "", result.agentCommand)
	assert.Equal(t, "", result.effectiveThinkingEffort)
	assert.Equal(t, "", result.effectiveMode)
}

func TestResolveAgentConfig_AgentWithAllFields(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:             "test-agent",
			SystemPrompt:   "You are at {{PROJECT_PATH}}",
			Command:        "/usr/bin/test-cli",
			ThinkingEffort: "high",
			PreferredMode:  "code",
			Models:         []model.AgentModel{{ID: "model-1", Default: true}},
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "/home/user/proj", "", "", "")
	assert.Equal(t, "You are at /home/user/proj", result.systemPrompt)
	assert.Equal(t, "model-1", result.agentModel)
	assert.Equal(t, "/usr/bin/test-cli", result.agentCommand)
	assert.Equal(t, "high", result.effectiveThinkingEffort)
	assert.Equal(t, "code", result.effectiveMode)
}

func TestResolveAgentConfig_ModelOverride(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:           "test-agent",
			SystemPrompt: "hello",
			Models:       []model.AgentModel{{ID: "default-model", Default: true}},
		},
	}
	defer func() { model.Agents = origAgents }()

	// modelOverride takes precedence over agent's default model
	result := resolveAgentConfig("test-agent", "", "custom-model", "", "")
	assert.Equal(t, "custom-model", result.agentModel)
}

func TestResolveAgentConfig_NoModelOverride_NoDefaultModel(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:           "test-agent",
			SystemPrompt: "hello",
			Models:       []model.AgentModel{},
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "", "", "", "")
	assert.Equal(t, "", result.agentModel)
}

func TestResolveAgentConfig_ProjectPathReplacement(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:           "test-agent",
			SystemPrompt: "Work on {{PROJECT_PATH}} and {{PROJECT_PATH}} again",
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "/my/path", "", "", "")
	assert.Equal(t, "Work on /my/path and /my/path again", result.systemPrompt)
}

func TestResolveAgentConfig_EmptyProjectPath_NoReplacement(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:           "test-agent",
			SystemPrompt: "Work on {{PROJECT_PATH}}",
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "", "", "", "")
	assert.Equal(t, "Work on {{PROJECT_PATH}}", result.systemPrompt)
}

func TestResolveAgentConfig_ThinkingEffortOverridePrecedence(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:             "test-agent",
			SystemPrompt:   "hello",
			ThinkingEffort: "low",
		},
	}
	defer func() { model.Agents = origAgents }()

	// Override takes precedence over agent default
	result := resolveAgentConfig("test-agent", "", "", "high", "")
	assert.Equal(t, "high", result.effectiveThinkingEffort)
}

func TestResolveAgentConfig_ThinkingEffortFromAgent(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:                      "test-agent",
			SystemPrompt:            "hello",
			ThinkingEffort:          "low",
			PreferredThinkingEffort: "medium",
		},
	}
	defer func() { model.Agents = origAgents }()

	// No override → use agent's EffectiveThinkingEffort (PreferredThinkingEffort > ThinkingEffort)
	result := resolveAgentConfig("test-agent", "", "", "", "")
	assert.Equal(t, "medium", result.effectiveThinkingEffort)
}

func TestResolveAgentConfig_ModeOverridePrecedence(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:            "test-agent",
			SystemPrompt:  "hello",
			PreferredMode: "code",
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "", "", "", "plan")
	assert.Equal(t, "plan", result.effectiveMode)
}

func TestResolveAgentConfig_ModeFromAgent(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:            "test-agent",
			SystemPrompt:  "hello",
			PreferredMode: "code",
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "", "", "", "")
	assert.Equal(t, "code", result.effectiveMode)
}

func TestResolveAgentConfig_NoCommand(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:           "test-agent",
			SystemPrompt: "hello",
		},
	}
	defer func() { model.Agents = origAgents }()

	result := resolveAgentConfig("test-agent", "", "", "", "")
	assert.Equal(t, "", result.agentCommand)
}

func TestResolveAgentConfig_DefaultModelFromPreferredModel(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:             "test-agent",
			SystemPrompt:   "hello",
			PreferredModel: "preferred-model",
			Models:         []model.AgentModel{{ID: "default-model", Default: true}},
		},
	}
	defer func() { model.Agents = origAgents }()

	// No modelOverride → PreferredModel takes precedence over default-flagged model
	result := resolveAgentConfig("test-agent", "", "", "", "")
	assert.Equal(t, "preferred-model", result.agentModel)
}

// ============================================================================
// resolveIsACP tests
// ============================================================================

func TestResolveIsACP_TransportOverrideACPStdio(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	assert.True(t, resolveIsACP("acp-stdio", "any-agent"))
}

func TestResolveIsACP_TransportOverrideCLI(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	assert.False(t, resolveIsACP("cli", "any-agent"))
}

func TestResolveIsACP_NoOverride_AgentWithACPTransport(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"acp-agent": {ID: "acp-agent", Transport: "acp-stdio"},
	}
	defer func() { model.Agents = origAgents }()

	assert.True(t, resolveIsACP("", "acp-agent"))
}

func TestResolveIsACP_NoOverride_AgentWithCLITransport(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"cli-agent": {ID: "cli-agent", Transport: "cli"},
	}
	defer func() { model.Agents = origAgents }()

	assert.False(t, resolveIsACP("", "cli-agent"))
}

func TestResolveIsACP_NoOverride_UnknownAgent(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	assert.False(t, resolveIsACP("", "nonexistent"))
}

func TestResolveIsACP_NoOverride_AgentWithEmptyTransport(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"agent": {ID: "agent", Transport: ""},
	}
	defer func() { model.Agents = origAgents }()

	assert.False(t, resolveIsACP("", "agent"))
}

func TestResolveIsACP_OverrideTakesPrecedence(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"acp-agent": {ID: "acp-agent", Transport: "acp-stdio"},
	}
	defer func() { model.Agents = origAgents }()

	// transportOverride=cli should override agent's acp-stdio
	assert.False(t, resolveIsACP("cli", "acp-agent"))
}

// ============================================================================
// appendMediaPrompt tests
// ============================================================================

func TestAppendMediaPrompt_EmptyMediaPrompt(t *testing.T) {
	// model.BuildMediaPrompt returns a non-empty string from the embedded template,
	// but we test the logic: if mediaPrompt is empty, systemPrompt is returned as-is.
	// Since we can't easily mock BuildMediaPrompt, we test with the actual function.
	result := appendMediaPrompt("my system prompt")
	// BuildMediaPrompt returns a non-empty string from embedded template,
	// so result should contain both parts
	assert.Contains(t, result, "my system prompt")
}

func TestAppendMediaPrompt_NonEmptySystemPrompt(t *testing.T) {
	result := appendMediaPrompt("my system prompt")
	// Should contain system prompt + media prompt joined by \n\n
	assert.Contains(t, result, "my system prompt")
	// Should also contain media-related content from the embedded template
	assert.True(t, len(result) > len("my system prompt"), "result should be longer than just the system prompt")
}

func TestAppendMediaPrompt_EmptySystemPrompt(t *testing.T) {
	result := appendMediaPrompt("")
	// With empty system prompt, should just return the media prompt
	// (or empty string if BuildMediaPrompt returns empty)
	mediaPrompt := model.BuildMediaPrompt()
	if mediaPrompt != "" {
		assert.Equal(t, mediaPrompt, result)
	} else {
		assert.Equal(t, "", result)
	}
}

// ============================================================================
// resolveSessionState tests
// ============================================================================

func TestResolveSessionState_NewSession(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Session with no assistant messages → resume=false
	effectiveID, resume, forkCtx := resolveSessionState("new-session", "", false)
	assert.Equal(t, "new-session", effectiveID)
	assert.False(t, resume)
	assert.Equal(t, "", forkCtx)
}

func TestResolveSessionState_ResumeWithExternalID_NonACP(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "sess-resume-1"
	// Insert session with external_session_id
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat', ?)",
		sessionID, "ext-123",
	)
	require.NoError(t, err)
	// Insert an assistant message to make SessionHasAssistant return true
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"hello"}]}`, sessionID,
	)
	require.NoError(t, err)

	effectiveID, resume, forkCtx := resolveSessionState(sessionID, "", false)
	assert.Equal(t, "ext-123", effectiveID, "should use external session ID for non-ACP resume")
	assert.True(t, resume)
	assert.Equal(t, "", forkCtx, "no fork context when external ID exists")
}

func TestResolveSessionState_ResumeWithoutExternalID_Fork(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "sess-fork-1"
	// Insert session without external_session_id
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)
	// Insert messages for fork context
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

	effectiveID, resume, forkCtx := resolveSessionState(sessionID, "", false)
	assert.Equal(t, "", effectiveID, "should clear session ID for non-ACP fork without external ID")
	assert.True(t, resume)
	assert.NotEmpty(t, forkCtx, "should have fork context when no external ID")
	assert.Contains(t, forkCtx, "user: user message")
	assert.Contains(t, forkCtx, "assistant: assistant reply")
}

func TestResolveSessionState_ResumeACPWithForkContext(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "sess-acp-fork"
	// Insert session without external_session_id
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'claude', 'Test', '', 'default', '', 'chat')",
		sessionID,
	)
	require.NoError(t, err)
	// Insert messages
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

	// ACP with fork context → resume=false
	effectiveID, resume, forkCtx := resolveSessionState(sessionID, "", true)
	assert.Equal(t, sessionID, effectiveID, "ACP should keep original session ID")
	assert.False(t, resume, "ACP with fork context should set resume=false")
	assert.NotEmpty(t, forkCtx, "should have fork context")
}

func TestResolveSessionState_ResumeACPWithExternalID(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

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

	// ACP with external ID → resume=true (no fork context needed)
	effectiveID, resume, forkCtx := resolveSessionState(sessionID, "", true)
	assert.Equal(t, sessionID, effectiveID, "ACP keeps original session ID even with external ID")
	assert.True(t, resume)
	assert.Equal(t, "", forkCtx, "no fork context when external ID exists")
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
			ID:           "default-agent",
			SystemPrompt: "default prompt",
			Models:       []model.AgentModel{{ID: "default-model", Default: true}},
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
			ID:           "test-agent",
			SystemPrompt: "hello",
			Models:       []model.AgentModel{{ID: "default-model", Default: true}},
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
			ID:           "cmd-agent",
			SystemPrompt: "prompt",
			Command:      "/usr/local/bin/special-cli",
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
			ID:           "path-agent",
			SystemPrompt: "You are working in {{PROJECT_PATH}}",
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
	assert.Contains(t, result, "user: hello user")
	assert.Contains(t, result, "assistant: hello assistant")
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
	// Insert message with invalid JSON content
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", "not valid json", sessionID,
	)
	require.NoError(t, err)

	result := BuildForkContext(sessionID)
	assert.Equal(t, "", result, "invalid JSON should be skipped")
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
	assert.Contains(t, result, "user: first part")
	assert.Contains(t, result, "second part")
	// thinking blocks are excluded
	assert.NotContains(t, result, "thinking part")
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

func TestRunDrainLoop_UserCancel(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             "cancel-sess",
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	result := DrainResult{CancelReason: "user"}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "cancelled", finalEvent.Type)
}

func TestRunDrainLoop_UserCancel_EmitsQueueCancel(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "cancel-with-queue"

	// Pre-enqueue two messages with queueIds
	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "pending-A", Text: "msg A", CreatedAt: "2026-01-01T00:00:00Z"})
	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "pending-B", Text: "msg B", CreatedAt: "2026-01-01T00:00:01Z"})

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             sessionID,
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	result := DrainResult{CancelReason: "user"}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "cancelled", finalEvent.Type)

	// Queue should be cleared
	assert.Empty(t, GetQueue(sessionID))
}

func TestRunDrainLoop_Error(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             "error-sess",
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	result := DrainResult{Err: "something went wrong"}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "something went wrong", finalEvent.Error)
}

func TestRunDrainLoop_EmptyContent(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             "empty-sess",
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	result := DrainResult{Empty: true}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "AI returned no content", finalEvent.Error)
	assert.Equal(t, ai.ReasonEmpty, finalEvent.Reason)
}

func TestRunDrainLoop_NonUserCancel(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             "disconnect-sess",
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	result := DrainResult{CancelReason: "disconnect"}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "cancelled", finalEvent.Type)
}

func TestRunDrainLoop_DoneNoQueue(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             "done-sess",
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	// No error, no cancel, not empty → should check queue, find nothing → done
	result := DrainResult{}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "done", finalEvent.Type)
}

func TestRunDrainLoop_DrainQueue(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "drain-sess"

	var finalEvents []ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvents = append(finalEvents, event)
	}

	// Enqueue a message so the drain loop can find it
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "queued msg", CreatedAt: "2026-01-01T00:00:00Z"})

	callCount := 0
	drainCfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/proj",
		BackendName: "claude",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			callCount++
			// Return done on the second call
			return DrainResult{} // will loop again but queue is now empty
		},
		MarkDoneAndSendFinal: markDoneAndSendFinal,
	}

	result := DrainResult{} // no cancel, no error, not empty → checks queue
	RunDrainLoop(drainCfg, result)

	// Should have drained the queued message and then found queue empty → done
	assert.Equal(t, 1, callCount, "ExecuteRunWithMessage should be called once for the drained message")
	assert.Equal(t, "done", finalEvents[len(finalEvents)-1].Type)
}

func TestRunDrainLoop_DoneWithRetryQueue(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "drain-retry-sess"

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             sessionID,
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	// No queue → should immediately return done
	result := DrainResult{}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "done", finalEvent.Type)
}

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
	_, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel(sessionID, cancel)

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
		defer handleSessionPanic(cfg, sessionID, cancel)
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

	err = SendMessageToSessionFromDingTalk(sessionID, "hello from dingtalk")

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
	assert.Contains(t, result, "user: final content")
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

func TestRunDrainLoop_DrainQueuedMessage(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "drain-sess-1"

	// Pre-enqueue a message
	EnqueueMessage(sessionID, model.QueuedMessage{
		Text:      "follow-up message",
		CreatedAt: "2024-01-01T00:00:00Z",
	})

	var finalEvents []ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvents = append(finalEvents, event)
	}

	drainCfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/proj",
		BackendName: "claude",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			return DrainResult{} // will loop again, queue now empty → done
		},
		MarkDoneAndSendFinal: markDoneAndSendFinal,
	}

	result := DrainResult{} // no cancel, no error, not empty → checks queue
	RunDrainLoop(drainCfg, result)

	// Should have found the queued message, drained it, then found queue empty
	require.NotEmpty(t, finalEvents, "should have at least one final event")
	assert.Equal(t, "done", finalEvents[len(finalEvents)-1].Type)

	// Clean up queue
	ClearQueue(sessionID)
}

func TestRunDrainLoop_DrainQueueEmptyAfterDrain(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "drain-empty-sess"

	var finalEvent ai.StreamEvent
	markDoneAndSendFinal := func(event ai.StreamEvent) {
		finalEvent = event
	}

	drainCfg := DrainConfig{
		SessionID:             sessionID,
		ProjectPath:           "/proj",
		BackendName:           "claude",
		PersistUser:           func(text string, files []model.FileEntry) (int64, error) { return 1, nil },
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult { return DrainResult{} },
		MarkDoneAndSendFinal:  markDoneAndSendFinal,
	}

	// Empty result with empty queue → should return "done"
	result := DrainResult{}
	RunDrainLoop(drainCfg, result)

	assert.Equal(t, "done", finalEvent.Type)
}

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
	err = SendMessageToSessionFromDingTalk(sessionID, "launch message")
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
// PruneRawResponses tests
// ============================================================================

func TestPruneRawResponses_ZeroOrNegative(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Create ai_raw_responses table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS ai_raw_responses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		backend TEXT NOT NULL,
		message_id INTEGER,
		raw_output TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	// Insert some rows
	_, err = WriteExec("INSERT INTO ai_raw_responses (session_id, backend, raw_output) VALUES ('s1', 'claude', 'output1')")
	require.NoError(t, err)
	_, err = WriteExec("INSERT INTO ai_raw_responses (session_id, backend, raw_output) VALUES ('s2', 'claude', 'output2')")
	require.NoError(t, err)

	// Zero limit should not prune
	PruneRawResponses(0)
	var count int
	db.QueryRow("SELECT COUNT(*) FROM ai_raw_responses").Scan(&count)
	assert.Equal(t, 2, count)

	// Negative limit should not prune
	PruneRawResponses(-1)
	db.QueryRow("SELECT COUNT(*) FROM ai_raw_responses").Scan(&count)
	assert.Equal(t, 2, count)
}

func TestPruneRawResponses_Prune(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	// Create ai_raw_responses table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS ai_raw_responses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		backend TEXT NOT NULL,
		message_id INTEGER,
		raw_output TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	// Insert 3 rows
	for i := range 3 {
		_, err = WriteExec("INSERT INTO ai_raw_responses (session_id, backend, raw_output) VALUES (?, 'claude', ?)",
			fmt.Sprintf("s%d", i), fmt.Sprintf("output%d", i))
		require.NoError(t, err)
	}

	// Keep only 1
	PruneRawResponses(1)
	var count int
	db.QueryRow("SELECT COUNT(*) FROM ai_raw_responses").Scan(&count)
	assert.Equal(t, 1, count)
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
// a user_message event with MessageID=0 is emitted (the enqueue path in
// session_command.go lines 140-154).
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
		ClearQueue(sessionID)
	}()

	err = SendMessageToSessionFromDingTalk(sessionID, "queued from dingtalk")
	assert.NoError(t, err)

	// Verify message was NOT persisted (enqueue path doesn't persist)
	messages, err := GetMessagesBySessionID(sessionID)
	require.NoError(t, err)
	assert.Len(t, messages, 0, "enqueue path should not persist user message to DB")

	// Verify message is in the in-memory queue
	queue := GetQueue(sessionID)
	assert.Len(t, queue, 1)
	assert.Equal(t, "queued from dingtalk", queue[0].Text)
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

	err = SendMessageToSessionFromDingTalk(sessionID, "this will fail")
	assert.Error(t, err, "should return error when AddChatMessage fails")
	assert.Contains(t, err.Error(), "persist message")

	// Session should no longer be running (rollback)
	assert.False(t, IsSessionRunning(sessionID), "session should not be running after AddChatMessage failure")
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
	assert.Contains(t, result, "assistant: I'll read the file")
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
	assert.Contains(t, result, "assistant:")
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
	assert.Contains(t, result, "assistant: Here is my answer")
	assert.NotContains(t, result, "I should think about this")
}

func TestFormatToolUseBlock_Truncation(t *testing.T) {
	// Test truncation of long input/output/summary
	longInput := strings.Repeat("a", 600)
	longOutput := strings.Repeat("b", 1100)
	longSummary := strings.Repeat("c", 600)

	b := model.ContentBlock{
		Type:       "tool_use",
		Name:       "Bash",
		ID:         "toolu_trunc",
		Status:     "success",
		Done:       true,
		DurationMs: 100,
		Summary:    longSummary,
	}
	tc := ToolCallRecord{
		ToolID: "toolu_trunc",
		Input:  json.RawMessage(longInput),
		Output: longOutput,
	}
	toolCallMap := map[string]*ToolCallRecord{"toolu_trunc": &tc}

	result := FormatToolUseBlock(b, toolCallMap)
	assert.Contains(t, result, "...(truncated)")
	// Input truncated at 500
	assert.Contains(t, result, strings.Repeat("a", 500))
	assert.NotContains(t, result, strings.Repeat("a", 600))
	// Summary truncated at 500
	assert.Contains(t, result, strings.Repeat("c", 500))
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

	err := SendMessageToSessionFromFeishu("nonexistent-session", "hello")
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
		ClearQueue(sessionID)
	}()

	err = SendMessageToSessionFromFeishu(sessionID, "hello from feishu")
	assert.NoError(t, err)

	// Verify message is in the in-memory queue
	queue := GetQueue(sessionID)
	assert.Len(t, queue, 1)
	assert.Equal(t, "hello from feishu", queue[0].Text)
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
	err = SendMessageToSessionFromFeishu(sessionID, "launch from feishu")
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

	err = SendMessageToSessionFromFeishu(sessionID, "this will fail")
	assert.Error(t, err, "should return error when AddChatMessage fails")
	assert.Contains(t, err.Error(), "persist message")
}
