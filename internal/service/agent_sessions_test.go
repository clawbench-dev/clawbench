package service_test

import (
	"database/sql"
	"fmt"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAgentSessionsSchema builds the minimum schema ListAgentSessions touches:
// chat_sessions (with the context_state column the host pointer lives in) and
// projects (for the cross-project path). Deliberately NOT the usage fixture's
// schema — that one has no context_state, so a test written on it could never
// exercise the host-vs-member distinction.
func setupAgentSessionsSchema(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDBForAgents(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '',
			agent_id TEXT DEFAULT '', agent_name TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT '', archived INTEGER NOT NULL DEFAULT 0,
			context_state TEXT DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL DEFAULT '', cron_expr TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '', prompt TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active'
		);
	`)
	require.NoError(t, err)
	return db
}

// insertAgentSessionRow inserts a chat_sessions row with an explicit timestamp.
func insertAgentSessionRow(t *testing.T, db *sql.DB, id, agentID, sessionType, groupID, updatedAt string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, agent_id, session_type, group_id, title, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, agentID, sessionType, groupID, "title-"+id, updatedAt,
	)
	require.NoError(t, err)
}

// TestListAgentSessions_ThreeRoles is the core contract: a 1:1 chat, a group the
// agent HOSTS, and a group the agent merely belongs to must each be reported
// with the right role — and the group must be reported through its TIMELINE row
// (the member row is hidden and has no messages).
func TestListAgentSessions_ThreeRoles(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	insertAgentSessionRow(t, db, "chat1", "a", "chat", "", "2026-01-01 10:00:00")
	// Group the agent hosts: the group row names it as agent_id AND the host
	// pointer names its member row.
	insertAgentSessionRow(t, db, "grp-host", "a", "group", "", "2026-01-02 10:00:00")
	insertAgentSessionRow(t, db, "m-host", "a", "group_member", "grp-host", "2026-01-02 10:00:00")
	// Group the agent is only a member of.
	insertAgentSessionRow(t, db, "grp-plain", "other", "group", "", "2026-01-03 10:00:00")
	insertAgentSessionRow(t, db, "m-plain", "a", "group_member", "grp-plain", "2026-01-03 10:00:00")

	_, err := db.Exec(
		`UPDATE chat_sessions SET context_state = json_set('{}', '$.host_member_id', json('"m-host"')) WHERE id = 'grp-host'`,
	)
	require.NoError(t, err)

	page, err := service.ListAgentSessions("a", 0, "", "")
	require.NoError(t, err)
	require.Len(t, page.Sessions, 3)

	bySession := map[string]service.AgentSessionItem{}
	for _, it := range page.Sessions {
		bySession[it.SessionID] = it
	}

	assert.Equal(t, service.AgentSessionRoleChat, bySession["chat1"].Role)
	assert.Equal(t, service.AgentSessionRoleHost, bySession["grp-host"].Role,
		"the group the agent hosts must be reported as host")
	assert.Equal(t, service.AgentSessionRoleMember, bySession["grp-plain"].Role,
		"a plain membership must not be reported as host")

	// The list points at the group timeline row, never the hidden member row.
	for _, it := range page.Sessions {
		assert.NotEqual(t, "group_member", it.SessionType,
			"member rows are hidden; the entry must open the group instead")
	}
	assert.Equal(t, "m-host", bySession["grp-host"].EntryID,
		"the entry id identifies the member instance")
}

// TestListAgentSessions_FreeGroupFirstMemberIsNotHost pins the trap that makes a
// naive `agent_id = X AND session_type = 'group'` query wrong: in FREE mode the
// group row's agent_id is the FIRST MEMBER's agent (it exists only to satisfy
// NOT NULL and drive the list icon), so that member must be reported as a plain
// member, not as the host.
func TestListAgentSessions_FreeGroupFirstMemberIsNotHost(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	// Free mode: no host pointer at all, group row's agent_id = first member.
	insertAgentSessionRow(t, db, "grp-free", "a", "group", "", "2026-01-01 10:00:00")
	insertAgentSessionRow(t, db, "m-first", "a", "group_member", "grp-free", "2026-01-01 10:00:00")
	insertAgentSessionRow(t, db, "m-second", "b", "group_member", "grp-free", "2026-01-01 10:00:00")

	page, err := service.ListAgentSessions("a", 0, "", "")
	require.NoError(t, err)
	require.Len(t, page.Sessions, 1)
	assert.Equal(t, service.AgentSessionRoleMember, page.Sessions[0].Role,
		"free mode has no host; the first member named by the group row is still a member")
}

// TestListAgentSessions_SameAgentTwiceInOneGroup: each membership is an
// independent instance, and the settings page counts them separately — so the
// list must contain one entry per instance or the number and the rows disagree.
func TestListAgentSessions_SameAgentTwiceInOneGroup(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	insertAgentSessionRow(t, db, "grp", "x", "group", "", "2026-01-01 10:00:00")
	insertAgentSessionRow(t, db, "m1", "a", "group_member", "grp", "2026-01-01 10:00:00")
	insertAgentSessionRow(t, db, "m2", "a", "group_member", "grp", "2026-01-01 10:00:00")
	_, err := db.Exec(`UPDATE chat_sessions SET agent_name = 'Alice' WHERE id = 'm2'`)
	require.NoError(t, err)

	page, err := service.ListAgentSessions("a", 0, "", "")
	require.NoError(t, err)
	require.Len(t, page.Sessions, 2, "one entry per member instance")
	assert.Equal(t, 2, page.Total, "total must match the row count")

	names := []string{page.Sessions[0].MemberName, page.Sessions[1].MemberName}
	assert.Contains(t, names, "", "the first instance keeps its bare name")
	assert.Contains(t, names, "Alice", "the second instance's name is surfaced")
}

// TestListAgentSessions_ArchiveAsymmetry: archived CHATS count (and must be
// listed), archived MEMBER rows do not (they are "left the group", not a
// conversation). This mirrors GetAgentUsage exactly.
func TestListAgentSessions_ArchiveAsymmetry(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	insertAgentSessionRow(t, db, "c-live", "a", "chat", "", "2026-01-02 10:00:00")
	insertAgentSessionRow(t, db, "c-arch", "a", "chat", "", "2026-01-01 10:00:00")
	_, err := db.Exec(`UPDATE chat_sessions SET archived = 1 WHERE id = 'c-arch'`)
	require.NoError(t, err)

	insertAgentSessionRow(t, db, "grp", "a", "group", "", "2026-01-03 10:00:00")
	insertAgentSessionRow(t, db, "m-left", "a", "group_member", "grp", "2026-01-03 10:00:00")
	_, err = db.Exec(`UPDATE chat_sessions SET archived = 1 WHERE id = 'm-left'`)
	require.NoError(t, err)

	page, err := service.ListAgentSessions("a", 0, "", "")
	require.NoError(t, err)

	ids := make([]string, 0, len(page.Sessions))
	for _, it := range page.Sessions {
		ids = append(ids, it.SessionID)
	}
	assert.ElementsMatch(t, []string{"c-live", "c-arch"}, ids,
		"archived chats are listed; the left membership is not")
	assert.Equal(t, 2, page.Total)

	// The header number and the list length must agree — that is the whole
	// reason the list is cross-project and archive-inclusive.
	usage, err := service.GetAgentUsage("a")
	require.NoError(t, err)
	assert.Equal(t, usage.Sessions, page.Total)
	assert.Equal(t, usage.Sessions, len(page.Sessions))
}

// TestListAgentSessions_KeysetPagination covers the tie-break: every membership
// of one group shares the group's updated_at, so paging on the timestamp alone
// would skip or repeat rows. The entry id breaks the tie.
func TestListAgentSessions_KeysetPagination(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	// Five memberships in ONE group ⇒ five rows with the SAME updated_at.
	insertAgentSessionRow(t, db, "grp", "z", "group", "", "2026-01-05 10:00:00")
	for i := 1; i <= 5; i++ {
		insertAgentSessionRow(t, db, fmt.Sprintf("m%d", i), "a", "group_member", "grp", "2026-01-05 10:00:00")
	}

	seen := map[string]bool{}
	cursor, cursorID := "", ""
	pages := 0
	for {
		page, err := service.ListAgentSessions("a", 2, cursor, cursorID)
		require.NoError(t, err)
		pages++
		for _, it := range page.Sessions {
			require.Falsef(t, seen[it.EntryID], "entry %s returned twice", it.EntryID)
			seen[it.EntryID] = true
		}
		if !page.HasMore {
			break
		}
		cursor, cursorID = page.NextCursor, page.NextCursorID
		require.LessOrEqual(t, pages, 10, "pagination did not terminate")
	}

	assert.Len(t, seen, 5, "every entry must appear exactly once across pages")
	assert.Equal(t, 3, pages, "5 rows at 2 per page")
}

// TestListAgentSessions_Empty makes sure an agent with no conversations returns
// an empty slice (JSON `[]`, not null) and a zero total.
func TestListAgentSessions_Empty(t *testing.T) {
	_ = setupAgentSessionsSchema(t)

	page, err := service.ListAgentSessions("ghost", 50, "", "")
	require.NoError(t, err)
	assert.NotNil(t, page.Sessions, "must be an empty slice so the client renders [] not null")
	assert.Empty(t, page.Sessions)
	assert.Zero(t, page.Total)
	assert.False(t, page.HasMore)
}

// TestListAgentSessions_CrossProject proves the scope matches the count: the
// same agent used in two projects lists both, each carrying its own project path
// so the client can switch before opening.
func TestListAgentSessions_CrossProject(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	_, err := db.Exec(`INSERT INTO projects (id, path) VALUES (1, '/proj/one'), (2, '/proj/two')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_id, agent_id, session_type, title, updated_at)
		 VALUES ('p1', 1, 'a', 'chat', 'one', '2026-01-01 10:00:00'),
		        ('p2', 2, 'a', 'chat', 'two', '2026-01-02 10:00:00')`,
	)
	require.NoError(t, err)

	page, err := service.ListAgentSessions("a", 0, "", "")
	require.NoError(t, err)
	require.Len(t, page.Sessions, 2)

	paths := map[string]string{}
	for _, it := range page.Sessions {
		paths[it.SessionID] = it.ProjectPath
	}
	assert.Equal(t, "/proj/one", paths["p1"])
	assert.Equal(t, "/proj/two", paths["p2"])
}

// Guard against the fixture drifting from the production session-type
// constants: the union hard-codes the two types it matches on.
func TestListAgentSessions_UsesProductionSessionTypes(t *testing.T) {
	assert.Equal(t, "chat", store.SessionTypeChat)
	assert.Equal(t, "group_member", store.SessionTypeGroupMember)
}

// A member row whose group row is gone (a dangling group_id, e.g. after a
// partial delete) is still counted by GetAgentUsage. The list must therefore
// still return it — otherwise the header prints a number the rows cannot
// account for. It degrades to an untitled, project-less entry.
func TestListAgentSessions_DanglingGroupStillListed(t *testing.T) {
	db := setupAgentSessionsSchema(t)

	insertAgentSessionRow(t, db, "orphan", "a", "group_member", "missing-group", "2026-01-01 10:00:00")

	page, err := service.ListAgentSessions("a", 0, "", "")
	require.NoError(t, err)

	usage, err := service.GetAgentUsage("a")
	require.NoError(t, err)

	assert.Equal(t, usage.Sessions, page.Total, "header count and list total must agree")
	assert.Len(t, page.Sessions, usage.Sessions,
		"a member row must never be dropped from the list while the count includes it")
	require.Len(t, page.Sessions, 1)
	assert.Equal(t, "orphan", page.Sessions[0].EntryID)
	assert.Empty(t, page.Sessions[0].ProjectPath, "no group row ⇒ no project to report")
}
