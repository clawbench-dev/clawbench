package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── PendingApproval annotation on ServeSessions GET ────────────────────────

func TestServeSessions_Get_AnnotatesPendingApprovalFromACP(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create two sessions
	sessionA, err := service.CreateSession(env.ProjectDir, "claude", "A", "claude", "", "default", "chat")
	require.NoError(t, err)
	sessionB, err := service.CreateSession(env.ProjectDir, "claude", "B", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Inject an ACP connection for sessionA with a pending permission entry.
	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionA, "acp-session-a")
	mgr.SetConnForTest(sessionA, conn)
	t.Cleanup(func() { mgr.CloseConn(sessionA) })

	// Register pending permission keyed on the ACP session ID
	key := ai.PermissionKey("acp-session-a", "toolcall-x")
	client.RegisterPendingPermissionForTest(key, &ai.PendingPermissionForTest{
		SessionID:  "acp-session-a",
		ToolCallID: "toolcall-x",
	})

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []model.ChatSession `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Sessions, 2)

	byID := map[string]model.ChatSession{}
	for _, s := range result.Sessions {
		byID[s.ID] = s
	}
	assert.True(t, byID[sessionA].PendingApproval, "sessionA should have PendingApproval=true")
	assert.False(t, byID[sessionB].PendingApproval, "sessionB should have PendingApproval=false")
}

func TestServeSessions_Get_NoPendingApprovalsReturnsFalse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "no-approval", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []model.ChatSession `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Sessions, 1)
	assert.Equal(t, sessionID, result.Sessions[0].ID)
	assert.False(t, result.Sessions[0].PendingApproval)
}

// ── ArchiveSession: ACP connection close on ACP-transport sessions ──────────

func TestArchiveSession_ClosesACPConnForACPTransport(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Register an ACP-capable agent so ArchiveSession's model.Agents lookup hits the
	// "acp-stdio" branch.
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"acp-test-agent": {
			ID:         "acp-test-agent",
			Backend:    "claude",
			Transport:  "cli",
			AcpCommand: "claude --acp",
		},
	}

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "acp-session", "acp-test-agent", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "acp-session-mapped")
	mgr.SetConnForTest(sessionID, conn)
	// Don't defer cleanup — ArchiveSession should close it.

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	// After archiving, the ACP connection for this session should be gone.
	// CloseConn runs in a goroutine, so wait briefly for it to complete.
	assert.Eventually(t, func() bool { return mgr.GetConn(sessionID) == nil }, 2*time.Second, 10*time.Millisecond, "ACP connection should be closed by ArchiveSession")
}

func TestArchiveSession_SkipsACPCloseForCLITransport(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	// CLI-transport agent — ArchiveSession should NOT attempt CloseConn
	model.Agents = map[string]*model.Agent{
		"cli-agent": {
			ID:        "cli-agent",
			Backend:   "claude",
			Transport: "cli",
		},
	}

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "cli-session", "cli-agent", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "acp-session-cli")
	mgr.SetConnForTest(sessionID, conn)
	t.Cleanup(func() { mgr.CloseConn(sessionID) })

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)
	// Connection may still exist since transport=cli skips the close path
}

func TestArchiveSession_UnknownAgentSkipsACPClose(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	// Empty Agents registry — model.Agents[agentID] lookup will fail
	model.Agents = map[string]*model.Agent{}

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "unknown-agent-session", "unknown-agent-id", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)
}

// ── ServeAISessionUpdate (PATCH /api/ai/session/update) ────────────────────

// ── ArchiveSession: empty session → hard-delete ─────────────────────────────

func TestArchiveSession_EmptySessionHardDeletes(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with no messages
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "empty-session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Verify it has zero finalized messages
	assert.Equal(t, 0, service.GetFinalizedMessageCount(sessionID))

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, true, result["destroyed"], "empty session should be hard-deleted (destroyed=true)")

	// Session should be physically gone — session count should be 0
	count, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "hard-deleted session should not count toward session total")
}

func TestArchiveSession_SessionWithMessagesSoftDeletes(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "has-messages", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Add a message so it's not empty
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "hello", nil, false, "")
	require.NoError(t, err)
	assert.Equal(t, 1, service.GetFinalizedMessageCount(sessionID))

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, false, result["destroyed"], "session with messages should be soft-archived (destroyed=false)")

	// Session should still exist (archived=1), not hard-deleted — messages still accessible
	assert.Equal(t, 1, service.GetChatMessageCount(sessionID))
}

func TestArchiveSession_RunningSessionCancelledBeforeArchive(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "running-archive", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "hello", nil, false, "")
	require.NoError(t, err)

	// Mark the session as running — archive must cancel it (force-clear) first.
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, false, result["destroyed"])
	assert.False(t, service.IsSessionRunning(sessionID), "running state should be cleared after archive")
}

func TestDestroySession_RunningSessionCancelledBeforeDestroy(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "running-destroy", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "hello", nil, false, "")
	require.NoError(t, err)

	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(DestroySession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, true, result["destroyed"])

	// Session and its messages are physically gone.
	count, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestServeAISessionUpdate_InvalidJSON(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id=abc", nil)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeAISessionUpdate_EmptyBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Patch target", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Empty payload — none of the if branches should fire, but the response
	// should still be 200 OK.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
}

func TestServeAISessionUpdate_ModelID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Patch model", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"modelId": "claude-sonnet-4-6",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// Verify the model was persisted
	got := service.GetSessionModel(sessionID)
	assert.Equal(t, "claude-sonnet-4-6", got)
}

func TestServeAISessionUpdate_TransportToCLI_ClosesACPConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"acp-agent": {ID: "acp-agent", Backend: "claude", Transport: "cli", AcpCommand: "claude --acp"},
	}

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-transport", "acp-agent", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "acp-session-x")
	mgr.SetConnForTest(sessionID, conn)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"transport": "cli",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
	assert.Nil(t, mgr.GetConn(sessionID), "transport=cli should close the ACP connection")
}

func TestServeAISessionUpdate_TransportNonCLI(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"acp-agent": {ID: "acp-agent", Backend: "claude", Transport: "cli", AcpCommand: "claude --acp"},
	}

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-transport-other", "acp-agent", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"transport": "acp-stdio",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
}

func TestServeAISessionUpdate_ModeIDWithoutConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-mode", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No ACP conn — the mode branch should be a no-op (conn == nil), but
	// the handler should still return 200.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"modeId": "code",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
}

func TestServeAISessionUpdate_ThinkingEffortWithoutConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-effort", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"thinkingEffort": "high",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
}

func TestServeAISessionUpdate_AutoApprove(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-auto", "claude", "", "default", "chat")
	require.NoError(t, err)

	// First send autoApprove=true
	autoApprove := true
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"autoApprove": autoApprove,
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	got := service.GetSessionAutoApprove(sessionID)
	assert.True(t, got)

	// Toggle to false
	autoApprove = false
	req = newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"autoApprove": autoApprove,
	})
	w = callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	got = service.GetSessionAutoApprove(sessionID)
	assert.False(t, got)
}

func TestServeAISessionUpdate_Title(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-title", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"title": "My Custom Session Name",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	got, err := service.GetSessionTitle(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "My Custom Session Name", got)
}

func TestServeAISessionUpdate_AutoApproveWithACPConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-auto-acp", "claude", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "acp-session-aa")
	mgr.SetConnForTest(sessionID, conn)
	t.Cleanup(func() { mgr.CloseConn(sessionID) })

	autoApprove := true
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"autoApprove": autoApprove,
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
}

func TestServeSessionsOverview_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions/overview", nil)
	w := callHandler(ServeSessionsOverview, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeSessionsOverview_DBError(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	db := service.UnsafeDBForTest()
	require.NoError(t, db.Close())

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	w := callHandler(ServeSessionsOverview, req)

	assertStatus(t, w, http.StatusInternalServerError)
}

func TestServeSessionsOverview_EmptyResult(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	w := callHandler(ServeSessionsOverview, req)

	assertOK(t, w)
	var result struct {
		Projects []any `json:"projects"`
		Total    int   `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, 0, result.Total)
	assert.Empty(t, result.Projects)
}
