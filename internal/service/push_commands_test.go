package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// setupTestDBForPushCommands installs a minimal in-memory schema carrying only
// the tables the push-command capabilities touch: the project registry, chat
// sessions, chat history and the queue.
func setupTestDBForPushCommands(t *testing.T) func() {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	// :memory: is per-connection, so pin to one connection.
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	_, _ = db.Exec(store.ProjectsDDL)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			agent_source TEXT NOT NULL DEFAULT 'default',
			model TEXT NOT NULL DEFAULT '',
			external_session_id TEXT NOT NULL DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			title_source TEXT NOT NULL DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL DEFAULT 0,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			files TEXT DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			agent_id TEXT NOT NULL DEFAULT '',
			injected INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS queued_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			queue_id TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			files TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	cleanup := store.SetDBForTest(db, db)
	return func() {
		cleanup()
		_ = db.Close()
	}
}

func TestGetLastAssistantReply_SkipsStreaming(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, err := store.ProjectIDForPath("/tmp/push-proj")
	if err != nil {
		t.Fatalf("project id: %v", err)
	}
	const sid = "a1b2c3d4-0000-0000-0000-000000000000"
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")

	// An older finished reply, then a newer streaming one that must be ignored:
	// a half-written answer is worse than "no reply yet".
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', ?, 0)`, projectID, sid,
		`{"blocks":[{"type":"text","text":"old answer"}]}`)
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', ?, 1)`, projectID, sid,
		`{"blocks":[{"type":"text","text":"still streaming"}]}`)

	text, ok := GetLastAssistantReply(sid)
	if !ok {
		t.Fatal("expected a reply")
	}
	if text != "old answer" {
		t.Errorf("text = %q, want %q", text, "old answer")
	}
}

func TestGetLastAssistantReply_PrefersNewest(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, _ := store.ProjectIDForPath("/tmp/push-proj2")
	const sid = "b2c3d4e5-0000-0000-0000-000000000000"
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")

	for _, body := range []string{"first", "second"} {
		_, _ = store.WriteExec(
			`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
			 VALUES (?, ?, 'assistant', ?, 0)`, projectID, sid,
			`{"blocks":[{"type":"text","text":"`+body+`"}]}`)
	}

	text, ok := GetLastAssistantReply(sid)
	if !ok || text != "second" {
		t.Fatalf("got %q ok=%v, want %q", text, ok, "second")
	}
}

func TestGetLastAssistantReply_NoReply(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	text, ok := GetLastAssistantReply("ffffffff-0000-0000-0000-000000000000")
	if ok {
		t.Errorf("expected ok=false, got text %q", text)
	}
}

func TestResolveProjectSelector_IndexAndPath(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	if _, err := store.ProjectIDForPath("/tmp/proj-a"); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if _, err := store.ProjectIDForPath("/tmp/proj-b"); err != nil {
		t.Fatalf("register b: %v", err)
	}

	refs, err := ListProjectsForPush()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d projects, want 2", len(refs))
	}
	if refs[0].Index != 1 || refs[1].Index != 2 {
		t.Errorf("indices not 1-based: %+v", refs)
	}

	// Index selector resolves to the same path the list showed.
	path, err := ResolveProjectSelector("1", refs)
	if err != nil || path != refs[0].Path {
		t.Fatalf("index selector = %q %v", path, err)
	}

	// Full path selector is exact-match against the registry.
	path, err = ResolveProjectSelector("/tmp/proj-b", refs)
	if err != nil || path != refs[1].Path {
		t.Fatalf("path selector = %q %v", path, err)
	}

	// Out-of-range index names the total so the user can retry.
	if _, err := ResolveProjectSelector("9", refs); err == nil {
		t.Fatal("expected out-of-range error")
	}

	// Unknown path is refused rather than implicitly registered — otherwise a
	// typo would permanently pollute the project list.
	if _, err := ResolveProjectSelector("/tmp/never-registered", refs); err == nil {
		t.Fatal("expected unknown-path error")
	}

	// Basename matching is deliberately absent: two projects named "proj-a"
	// would be indistinguishable.
	if _, err := ResolveProjectSelector("proj-a", refs); err == nil {
		t.Fatal("expected basename selector to be rejected")
	}
}

func TestCreatePushSession_UnknownProjectRefused(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	if _, _, err := CreatePushSession("/tmp/never-registered"); err == nil {
		t.Fatal("expected an error for an unregistered project")
	}

	// The refusal must not have registered the path as a side effect.
	if _, ok, err := store.ProjectIDByPath("/tmp/never-registered"); err != nil || ok {
		t.Fatalf("path was registered anyway (ok=%v err=%v)", ok, err)
	}
}

func TestCreatePushSession_UsesDefaultAgent(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectPath := "/tmp/push-new-session"
	if _, err := store.ProjectIDForPath(projectPath); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// The agent registry is package-global; restore it afterwards so other
	// tests in this package see the real list.
	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)
	agent := &model.Agent{ID: "test-default-agent", Name: "T", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	sid, title, err := CreatePushSession(projectPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sid == "" {
		t.Fatal("empty session id")
	}
	if title == "" {
		t.Fatal("empty title")
	}

	// The created session must carry the default agent and be a plain chat.
	var agentID, sessionType string
	if err := store.ReadDB().QueryRow(
		"SELECT agent_id, session_type FROM chat_sessions WHERE id = ?", sid,
	).Scan(&agentID, &sessionType); err != nil {
		t.Fatalf("read back session: %v", err)
	}
	if agentID != agent.ID {
		t.Errorf("agent_id = %q, want %q", agentID, agent.ID)
	}
	if sessionType != "chat" {
		t.Errorf("session_type = %q, want chat", sessionType)
	}
}

func TestStopAndClearSession_ClearsQueueWhenIdle(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, _ := store.ProjectIDForPath("/tmp/push-stop")
	const sid = "c3d4e5f6-0000-0000-0000-000000000000"
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")

	for _, qid := range []string{"q1", "q2"} {
		_, _ = store.WriteExec(
			`INSERT INTO queued_messages (session_id, project_id, queue_id, content)
			 VALUES (?, ?, ?, 'msg')`, sid, projectID, qid)
	}

	wasRunning, cleared, err := StopAndClearSession(sid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Nothing is running in this fixture; the cancel is idempotent.
	if wasRunning {
		t.Error("expected wasRunning=false for an idle session")
	}
	if cleared != 2 {
		t.Errorf("cleared = %d, want 2", cleared)
	}

	remaining, err := GetQueuedQueueIDs(sid)
	if err != nil {
		t.Fatalf("list remaining: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("queue not emptied: %v", remaining)
	}
}

func TestStopAndClearSession_IdempotentOnEmptyQueue(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	wasRunning, cleared, err := StopAndClearSession("d4e5f6a7-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasRunning || cleared != 0 {
		t.Errorf("got wasRunning=%v cleared=%d, want false/0", wasRunning, cleared)
	}
}
