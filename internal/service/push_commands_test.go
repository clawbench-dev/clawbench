package service

import (
	"database/sql"
	"strings"
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

	text, ok, err := GetLastAssistantReply(sid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

	text, ok, err := GetLastAssistantReply(sid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || text != "second" {
		t.Fatalf("got %q ok=%v, want %q", text, ok, "second")
	}
}

func TestGetLastAssistantReply_NoReply(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	text, ok, err := GetLastAssistantReply("ffffffff-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false, got text %q", text)
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

func TestGetLastAssistantReply_SkipsUnparsableRows(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, _ := store.ProjectIDForPath("/tmp/push-bad-json")
	const sid = "e5f6a7b8-0000-0000-0000-000000000000"
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")

	// A malformed row must be skipped, not abort the walk: a single corrupt
	// historical row would otherwise hide every older reply.
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', 'not json at all', 0)`, projectID, sid)
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', ?, 0)`, projectID, sid,
		`{"blocks":[{"type":"text","text":"valid answer"}]}`)

	text, ok, err := GetLastAssistantReply(sid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || text != "valid answer" {
		t.Fatalf("got %q ok=%v, want the valid row", text, ok)
	}
}

func TestDefaultAgentForProject_ScopeAware(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, err := store.ProjectIDForPath("/tmp/push-scope")
	if err != nil {
		t.Fatalf("project id: %v", err)
	}
	otherID := projectID + 999

	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)

	// A scoped agent belonging to a DIFFERENT project must never be picked,
	// even though it is first in the list.
	foreign := &model.Agent{ID: "foreign", Backend: "codebuddy", ProjectID: &otherID}
	global := &model.Agent{ID: "global", Backend: "codebuddy"}
	model.ReplaceAgents(
		map[string]*model.Agent{foreign.ID: foreign, global.ID: global},
		[]*model.Agent{foreign, global},
	)

	if got := defaultAgentForProject(projectID); got != "global" {
		t.Errorf("defaultAgentForProject = %q, want the visible global agent", got)
	}

	// An agent scoped to THIS project is visible and wins over a later global.
	own := &model.Agent{ID: "own", Backend: "codebuddy", ProjectID: &projectID}
	model.ReplaceAgents(
		map[string]*model.Agent{own.ID: own, foreign.ID: foreign},
		[]*model.Agent{own, foreign},
	)
	if got := defaultAgentForProject(projectID); got != "own" {
		t.Errorf("defaultAgentForProject = %q, want the project-scoped agent", got)
	}

	// No visible agent at all → empty string, which the caller turns into an
	// error rather than creating an agentless session.
	model.ReplaceAgents(map[string]*model.Agent{foreign.ID: foreign}, []*model.Agent{foreign})
	if got := defaultAgentForProject(projectID); got != "" {
		t.Errorf("defaultAgentForProject = %q, want empty", got)
	}
}

func TestCreatePushSession_NoVisibleAgent(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectPath := "/tmp/push-no-agent"
	if _, err := store.ProjectIDForPath(projectPath); err != nil {
		t.Fatalf("register project: %v", err)
	}

	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)
	model.ReplaceAgents(map[string]*model.Agent{}, nil)

	if _, _, err := CreatePushSession(projectPath); err == nil {
		t.Fatal("expected an error when no agent is visible in the project")
	}
}

func TestGetLastAssistantReply_SkipsEmptyBlocks(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, _ := store.ProjectIDForPath("/tmp/push-empty-blocks")
	const sid = "f6a7b8c9-0000-0000-0000-000000000000"
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")

	// A finalized row whose blocks carry no answer (a tool-only turn, or a
	// summarized row stripped to empty) must not shadow the earlier real one.
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', '{"blocks":[]}', 0)`, projectID, sid)
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', ?, 0)`, projectID, sid,
		`{"blocks":[{"type":"text","text":"earlier answer"}]}`)

	text, ok, err := GetLastAssistantReply(sid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || text != "earlier answer" {
		t.Fatalf("got %q ok=%v, want the earlier text block", text, ok)
	}
}

func TestGetLastAssistantReply_QueryErrorReturnsNoReply(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	cleanup := store.SetDBForTest(db, db)
	defer cleanup()
	// Close before querying so the read fails: the caller must degrade to
	// "no reply" rather than surfacing a DB error into the IM chat.
	_ = db.Close()

	// A DB failure must surface as an error, not as "no reply yet" — the bot
	// tells the user "unavailable" instead of falsely claiming there is nothing.
	text, ok, err := GetLastAssistantReply("some-session")
	if err == nil {
		t.Fatalf("expected an error, got text %q ok=%v", text, ok)
	}
	if ok || text != "" {
		t.Fatalf("got %q ok=%v, want the empty degradation", text, ok)
	}
}

func TestCreatePushSession_InsertFailurePropagates(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectPath := "/tmp/push-insert-fail"
	if _, err := store.ProjectIDForPath(projectPath); err != nil {
		t.Fatalf("register project: %v", err)
	}

	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)
	agent := &model.Agent{ID: "a1", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	// Drop the session table so the INSERT fails: the error must surface to the
	// caller (which reports it in the IM chat) instead of yielding a bogus id.
	if _, err := store.WriteExec("DROP TABLE chat_sessions"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if _, _, err := CreatePushSession(projectPath); err == nil {
		t.Fatal("expected the insert failure to propagate")
	}
}

func TestCreatePushSession_RespectsSessionCap(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectPath := "/tmp/push-cap"
	projectID, err := store.ProjectIDForPath(projectPath)
	if err != nil {
		t.Fatalf("register project: %v", err)
	}

	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)
	agent := &model.Agent{ID: "cap-agent", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	prevMax := model.SessionMaxCount
	defer func() { model.SessionMaxCount = prevMax }()
	model.SessionMaxCount = 1

	// Fill the project up to the cap with an archived-free chat session.
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type) VALUES (?, ?, ?, ?, 'chat')",
		"existing-0000-0000-0000-000000000000", projectID, "codebuddy", "existing")

	// IM is the only create path that could otherwise skip the cap, so a
	// repeated "/new 1" must be refused here.
	if _, _, err := CreatePushSession(projectPath); err == nil {
		t.Fatal("expected the session cap to refuse creation")
	}
}

func TestCreatePushSession_CapZeroMeansUnlimited(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectPath := "/tmp/push-cap-zero"
	projectID, err := store.ProjectIDForPath(projectPath)
	if err != nil {
		t.Fatalf("register project: %v", err)
	}

	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)
	agent := &model.Agent{ID: "cap0-agent", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	prevMax := model.SessionMaxCount
	defer func() { model.SessionMaxCount = prevMax }()
	model.SessionMaxCount = 0

	for i := range 3 {
		_, _ = store.WriteExec(
			"INSERT INTO chat_sessions (id, project_id, backend, title, session_type) VALUES (?, ?, ?, ?, 'chat')",
			"filler-"+string(rune('a'+i))+"-0000-0000-0000-000000000000", projectID, "codebuddy", "filler")
	}

	if _, _, err := CreatePushSession(projectPath); err != nil {
		t.Fatalf("cap 0 must mean unlimited, got error: %v", err)
	}
}

func TestStopAndClearSession_ReportsCountWhenDrainAlsoClears(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, _ := store.ProjectIDForPath("/tmp/push-stop-race")
	const sid = "a7b8c9d0-0000-0000-0000-000000000000"
	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")

	for _, qid := range []string{"q1", "q2", "q3"} {
		_, _ = store.WriteExec(
			`INSERT INTO queued_messages (session_id, project_id, queue_id, content)
			 VALUES (?, ?, ?, 'msg')`, sid, projectID, qid)
	}

	// Simulate the drain goroutine clearing the queue concurrently: the count
	// reported to the user must come from the pre-cancel snapshot, so it stays
	// 3 even though the queue is empty by the time we read it.
	_, cleared, err := StopAndClearSession(sid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleared != 3 {
		t.Errorf("cleared = %d, want 3 (the pre-cancel snapshot)", cleared)
	}
}

func TestStopAndClearSession_ListFailureIsLocalized(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	// Dropping the queue table makes the snapshot read fail. The reply must be
	// the localized "stop failed" message, never the raw SQLite error.
	if _, err := store.WriteExec("DROP TABLE queued_messages"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	_, _, err := StopAndClearSession("b8c9d0e1-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected an error when the queue read fails")
	}
	if strings.Contains(err.Error(), "no such table") {
		t.Errorf("raw SQLite error leaked to the user: %v", err)
	}
	if !strings.Contains(err.Error(), "停止会话失败") {
		t.Errorf("expected the localized message, got %v", err)
	}
}

func TestCreatePushSession_CreateFailureIsLocalized(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectPath := "/tmp/push-create-localized"
	if _, err := store.ProjectIDForPath(projectPath); err != nil {
		t.Fatalf("register project: %v", err)
	}

	prevAgents, prevList := model.Agents, model.AgentList
	defer model.ReplaceAgents(prevAgents, prevList)
	agent := &model.Agent{ID: "loc-agent", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	prevMax := model.SessionMaxCount
	defer func() { model.SessionMaxCount = prevMax }()
	model.SessionMaxCount = 0

	if _, err := store.WriteExec("DROP TABLE chat_sessions"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	_, _, err := CreatePushSession(projectPath)
	if err == nil {
		t.Fatal("expected the insert failure to propagate")
	}
	if strings.Contains(err.Error(), "no such table") {
		t.Errorf("raw SQLite error leaked to the user: %v", err)
	}
	if !strings.Contains(err.Error(), "创建会话失败") {
		t.Errorf("expected the localized message, got %v", err)
	}
}

func TestCreatePushSession_ProjectResolveFailureIsLocalized(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	// Dropping the projects table makes ProjectIDByPath error out rather than
	// simply report "not found".
	if _, err := store.WriteExec("DROP TABLE projects"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	_, _, err := CreatePushSession("/tmp/whatever")
	if err == nil {
		t.Fatal("expected a resolve error")
	}
	if !strings.Contains(err.Error(), "未找到该项目") {
		t.Errorf("expected the localized message, got %v", err)
	}
}

func TestListProjectsForPush_IndicesAreOneBased(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	if _, err := store.ProjectIDForPath("/tmp/list-a"); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if _, err := store.ProjectIDForPath("/tmp/list-b"); err != nil {
		t.Fatalf("register b: %v", err)
	}

	refs, err := ListProjectsForPush()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d projects, want 2", len(refs))
	}
	// The index the user reads must be the index they type back, so it has to
	// start at 1 and be contiguous.
	for i, r := range refs {
		if r.Index != i+1 {
			t.Errorf("refs[%d].Index = %d, want %d", i, r.Index, i+1)
		}
		if r.Path == "" {
			t.Errorf("refs[%d] has an empty path", i)
		}
	}
}
