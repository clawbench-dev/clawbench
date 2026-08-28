package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeSessionsOverview_groupsAndFilters verifies the cross-project overview
// endpoint: sessions are grouped by project name and only sessions that are
// running, pending approval, or have unread messages are included.
func TestServeSessionsOverview_groupsAndFilters(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	projectB := env.WatchDir + "/project-b"
	db := service.UnsafeDBForTest()

	// projectA: running session A1
	sessionA1, err := service.CreateSession(env.ProjectDir, "claude", "A1", "claude", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(sessionA1, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionA1, false) })

	// projectA: read, completed session A3 — must be filtered out
	_, err = db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, archived, last_read_at) VALUES (?, ?, 'claude', 'A3', 'claude', 'default', '', 'chat', 0, CURRENT_TIMESTAMP)`, "a3-session", env.ProjectDir)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'read msg', 'a3-session', 'claude', 0)`, env.ProjectDir)
	require.NoError(t, err)

	// projectB: unread session B1 — assistant message newer than last_read_at
	_, err = db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, archived) VALUES (?, ?, 'claude', 'B1', 'claude', 'default', '', 'chat', 0)`, "b1-session", projectB)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'unread msg', 'b1-session', 'claude', 0)`, projectB)
	require.NoError(t, err)

	// projectB: pending approval session B2
	sessionB2, err := service.CreateSession(projectB, "claude", "B2", "claude", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionB2, "acp-session-b2")
	mgr.SetConnForTest(sessionB2, conn)
	t.Cleanup(func() { mgr.CloseConn(sessionB2) })
	key := ai.PermissionKey("acp-session-b2", "toolcall-b2")
	client.RegisterPendingPermissionForTest(key, &ai.PendingPermissionForTest{
		SessionID:  "acp-session-b2",
		ToolCallID: "toolcall-b2",
	})

	// Overview endpoint spans projects — no project cookie needed.
	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)

	var result struct {
		Projects []struct {
			Name     string `json:"name"`
			Sessions []struct {
				ID              string `json:"id"`
				Title           string `json:"title"`
				Running         bool   `json:"running"`
				PendingApproval bool   `json:"pendingApproval"`
				UnreadCount     int    `json:"unreadCount"`
			} `json:"sessions"`
		} `json:"projects"`
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	assert.Equal(t, 3, result.Total, "A3 (read, completed) must be filtered out")

	byName := map[string][]struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		Running         bool   `json:"running"`
		PendingApproval bool   `json:"pendingApproval"`
		UnreadCount     int    `json:"unreadCount"`
	}{}
	for _, p := range result.Projects {
		byName[p.Name] = p.Sessions
	}

	// projectA: A1 running
	assert.Contains(t, byName, env.ProjectDir, "projectA group should exist")
	aSessions := byName[env.ProjectDir]
	require.Len(t, aSessions, 1, "projectA should only contain the running session")
	assert.Equal(t, sessionA1, aSessions[0].ID)
	assert.True(t, aSessions[0].Running, "A1 should be running=true")
	assert.Equal(t, 0, aSessions[0].UnreadCount)

	// projectB: B1 unread + B2 pending
	assert.Contains(t, byName, projectB, "projectB group should exist")
	bSessions := byName[projectB]
	require.Len(t, bSessions, 2, "projectB should contain unread + pending sessions")
	bByID := map[string]struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		Running         bool   `json:"running"`
		PendingApproval bool   `json:"pendingApproval"`
		UnreadCount     int    `json:"unreadCount"`
	}{}
	for _, s := range bSessions {
		bByID[s.ID] = s
	}
	assert.Greater(t, bByID["b1-session"].UnreadCount, 0, "B1 should have unread count")
	assert.True(t, bByID[sessionB2].PendingApproval, "B2 should be pending approval")

	// A3 must be absent from every group
	for name, sessions := range byName {
		for _, s := range sessions {
			assert.NotEqual(t, "a3-session", s.ID, "A3 (read) must be filtered out of group %q", name)
		}
	}
}
