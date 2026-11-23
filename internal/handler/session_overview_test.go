package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/store"

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

	// Canonical, because the overview resolves each session's project_id back to
	// the registry's canonical path (which filepath.Join would not match on
	// Windows, where the separator differs).
	projectB := canonPath(filepath.Join(env.WatchDir, "project-b"))
	require.NoError(t, os.MkdirAll(projectB, 0o755))
	db := store.UnsafeDBForTest()

	// projectA: running session A1
	sessionA1, err := service.CreateSession(env.ProjectDir, "claude", "A1", "claude", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(sessionA1, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionA1, false) })

	// projectA: read, completed session A3 — must be filtered out
	_, err = db.Exec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived, last_read_at) VALUES (?, ?, 'claude', 'A3', 'claude', 'default', '', 'chat', 0, CURRENT_TIMESTAMP)`, "a3-session", store.ProjectIDForTest(t, env.ProjectDir))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'read msg', 'a3-session', 'claude', 0)`, store.ProjectIDForTest(t, env.ProjectDir))
	require.NoError(t, err)

	// projectB: unread session B1 — assistant message newer than last_read_at
	_, err = db.Exec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived) VALUES (?, ?, 'claude', 'B1', 'claude', 'default', '', 'chat', 0)`, "b1-session", store.ProjectIDForTest(t, projectB))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'unread msg', 'b1-session', 'claude', 0)`, store.ProjectIDForTest(t, projectB))
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
				Backend         string `json:"backend"`
				AgentID         string `json:"agentId"`
				Model           string `json:"model"`
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
		Backend         string `json:"backend"`
		AgentID         string `json:"agentId"`
		Model           string `json:"model"`
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
	// Row rendering needs agent identity — the overview must carry it through
	// (frontend renders the same agent icon/model chips as the main list).
	assert.Equal(t, "claude", aSessions[0].Backend, "backend must be populated for row rendering")
	assert.Equal(t, "claude", aSessions[0].AgentID, "agentId must be populated for row rendering")

	// projectB: B1 unread + B2 pending
	assert.Contains(t, byName, projectB, "projectB group should exist")
	bSessions := byName[projectB]
	require.Len(t, bSessions, 2, "projectB should contain unread + pending sessions")
	bByID := map[string]struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		Backend         string `json:"backend"`
		AgentID         string `json:"agentId"`
		Model           string `json:"model"`
		Running         bool   `json:"running"`
		PendingApproval bool   `json:"pendingApproval"`
		UnreadCount     int    `json:"unreadCount"`
	}{}
	for _, s := range bSessions {
		bByID[s.ID] = s
	}
	assert.Greater(t, bByID["b1-session"].UnreadCount, 0, "B1 should have unread count")
	assert.True(t, bByID[sessionB2].PendingApproval, "B2 should be pending approval")
	assert.Equal(t, "claude", bByID[sessionB2].Backend, "backend must be populated for row rendering")
	assert.Equal(t, "claude", bByID[sessionB2].AgentID, "agentId must be populated for row rendering")

	// A3 must be absent from every group
	for name, sessions := range byName {
		for _, s := range sessions {
			assert.NotEqual(t, "a3-session", s.ID, "A3 (read) must be filtered out of group %q", name)
		}
	}
}

// A project whose directory no longer exists has no place in the overview:
// its card could never be opened, so both an active group and a merely-recent
// card must be dropped. Existing projects keep their cards, and `total` counts
// only the sessions actually returned.
func TestServeSessionsOverview_DropsProjectsWithMissingDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	db := store.UnsafeDBForTest()
	missingActive := canonPath(filepath.Join(env.WatchDir, "gone-active"))
	missingRecent := canonPath(filepath.Join(env.WatchDir, "gone-recent"))
	existingRecent := canonPath(filepath.Join(env.WatchDir, "still-here"))
	require.NoError(t, os.MkdirAll(existingRecent, 0o755))

	// Unread session in a missing directory: would otherwise be an active group.
	missingActiveID := store.ProjectIDForTest(t, missingActive)
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived) VALUES (?, ?, 'claude', 'Ghost active', 'claude', 'default', '', 'chat', 0)`, "ghost-active", missingActiveID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'unread', 'ghost-active', 'claude', 0)`, missingActiveID)
	require.NoError(t, err)

	// Read, message-less session in a missing directory: would otherwise be a
	// recent-project card (activity is recent, nothing unread).
	_, err = db.Exec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived) VALUES (?, ?, 'claude', 'Ghost recent', 'claude', 'default', '', 'chat', 0)`, "ghost-recent", store.ProjectIDForTest(t, missingRecent))
	require.NoError(t, err)

	// The same shape in an existing directory must stay: proof the filter is
	// about the directory, not about recent cards in general.
	_, err = db.Exec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived) VALUES (?, ?, 'claude', 'Live recent', 'claude', 'default', '', 'chat', 0)`, "live-recent", store.ProjectIDForTest(t, existingRecent))
	require.NoError(t, err)

	// And an active session in the existing project directory.
	existingSession, err := service.CreateSession(env.ProjectDir, "claude", "Live", "claude", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(existingSession, true)
	t.Cleanup(func() { service.SetSessionRunning(existingSession, false) })

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)

	var result struct {
		Projects []struct {
			Name     string `json:"name"`
			Sessions []struct {
				ID string `json:"id"`
			} `json:"sessions"`
		} `json:"projects"`
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	names := make([]string, 0, len(result.Projects))
	for _, p := range result.Projects {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, env.ProjectDir, "existing project with an active session must stay")
	assert.Contains(t, names, existingRecent, "recent card for an existing directory must stay")
	assert.NotContains(t, names, missingActive, "active group for a missing directory must be dropped")
	assert.NotContains(t, names, missingRecent, "recent card for a missing directory must be dropped")
	assert.Equal(t, 1, result.Total, "total must count only sessions that are actually returned")
}
