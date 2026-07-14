package service

import (
	"database/sql"
	"testing"

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
			deleted INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_read_at DATETIME,
			UNIQUE(project_path, backend, id)
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

func TestFindSessionsByPrefix_DeletedExcluded(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, deleted) VALUES (?, '/proj', 'codebuddy', 'Deleted', 'agent1', 'default', '', 'chat', 1)",
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
		t.Errorf("expected 0 results for deleted session, got %d", len(results))
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
