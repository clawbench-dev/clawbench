package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
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
	count, err := service.GetFinalizedMessageCount(sessionID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, true, result["destroyed"], "empty session should be hard-deleted (destroyed=true)")

	// Session should be physically gone — session count should be 0
	count, err = service.GetSessionCount(env.ProjectDir)
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
	count, err := service.GetFinalizedMessageCount(sessionID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, false, result["destroyed"], "session with messages should be soft-archived (destroyed=false)")

	// Session should still exist (archived=1), not hard-deleted — messages still accessible
	count, err = service.GetChatMessageCount(sessionID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestArchiveSession_EmptySessionHardDelete_RAGPurgeErrorStillDestroys(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "rag-err", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Inject a RAG purge callback that reports an error. The empty-session
	// hard-delete path must log the warning and still destroy the session.
	service.SetPurgeRAGChunksFn(func(sessionIDs []string) (int64, error) {
		return 0, assert.AnError
	})
	defer service.SetPurgeRAGChunksFn(nil)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["destroyed"], "session must still be destroyed when RAG purge fails")

	count, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestArchiveSession_EmptySessionHardDelete_RAGPurgeChunksLogged(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "rag-chunks", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Inject a RAG purge callback that reports deleted chunks. The hard-delete
	// path logs the count and still destroys the session.
	service.SetPurgeRAGChunksFn(func(sessionIDs []string) (int64, error) {
		return 3, nil
	})
	defer service.SetPurgeRAGChunksFn(nil)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["destroyed"])

	count, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestArchiveSession_CountErrorFallsBackToSoftArchive(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "count-error-session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "important content", nil, false, "")
	require.NoError(t, err)

	// Force the read pool (used by GetFinalizedMessageCount) to fail. The write
	// pool stays healthy, so ArchiveSession's UPDATE/HardDeleteSession would
	// succeed if the empty-session branch were wrongly taken.
	closedDB, err := service.InitInMemoryDB()
	require.NoError(t, err)
	_ = closedDB.Close()
	cleanup := service.SetDBForTest(service.UnsafeDBForTest(), closedDB)
	defer cleanup()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID+"&backend=claude", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, false, result["destroyed"], "count failure must NOT hard-delete a session that may have content")

	// The session must still exist with its messages intact.
	cleanup()
	count, err := service.GetChatMessageCount(sessionID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "session content must survive a count failure during archive")

	var archived int
	require.NoError(t, service.UnsafeDBForTest().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&archived))
	assert.Equal(t, 1, archived, "session should be soft-archived when the count query fails")
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

// TestServeAISessionUpdate_WhitespaceTitleIgnored verifies a whitespace-only
// title is treated as "no rename": it must not blank the title nor latch the
// title_renamed flag (which would permanently disable auto-titling).
func TestServeAISessionUpdate_WhitespaceTitleIgnored(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Original Title", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"title": "   ",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	got, err := service.GetSessionTitle(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "Original Title", got, "whitespace-only title must be ignored")

	renamed, err := service.GetSessionTitleRenamed(sessionID)
	require.NoError(t, err)
	assert.False(t, renamed, "whitespace-only title must not latch title_renamed")
}

// TestServeAISessionUpdate_TitleMarksRenamed verifies that a manual rename sets
// title_source=custom so the first-message auto-title will not clobber it.
func TestServeAISessionUpdate_TitleMarksRenamed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "New Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"title": "User Chosen Name",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	renamed, err := service.GetSessionTitleRenamed(sessionID)
	require.NoError(t, err)
	assert.True(t, renamed, "manual rename must mark the title custom")

	// The first user message must not overwrite the user's title.
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "this is a long first message", nil, false, "NewSession")
	require.NoError(t, err)

	got, err := service.GetSessionTitle(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "User Chosen Name", got)
}

// TestServeAISessionUpdate_TitleViaBodySessionID verifies that the rename
// target is taken from the body sessionId when present, even when the
// chat_session_id cookie points at a DIFFERENT session. This is the
// "renamed title gets clobbered" bug: the frontend sends {sessionId, title} in
// the body, so resolving the target from the cookie would rename the wrong
// session and leave the intended one unlocked for the first message to
// overwrite.
func TestServeAISessionUpdate_TitleViaBodySessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target, err := service.CreateSession(env.ProjectDir, "claude", "New Session 1", "claude", "", "default", "chat")
	require.NoError(t, err)
	other, err := service.CreateSession(env.ProjectDir, "claude", "New Session 2", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No query param. Cookie points at `other`; body points at `target`.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": target,
		"title":     "Renamed Target",
	})
	req = withProjectCookie(req, env.ProjectDir)
	req = withSessionCookie(req, other)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	gotTarget, err := service.GetSessionTitle(target)
	require.NoError(t, err)
	assert.Equal(t, "Renamed Target", gotTarget, "body sessionId must win over the cookie")

	gotOther, err := service.GetSessionTitle(other)
	require.NoError(t, err)
	assert.Equal(t, "New Session 2", gotOther, "the cookie's session must be untouched")

	// And the target's first message must not overwrite the rename.
	_, err = service.AddChatMessage(env.ProjectDir, "claude", target, "user", "first message", nil, false, "NewSession")
	require.NoError(t, err)
	gotTarget, err = service.GetSessionTitle(target)
	require.NoError(t, err)
	assert.Equal(t, "Renamed Target", gotTarget)
}

// TestServeAISessionUpdate_BodySessionIDUnknown verifies a body sessionId that
// does not exist is rejected instead of silently falling back to the cookie.
func TestServeAISessionUpdate_BodySessionIDUnknown(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": "does-not-exist",
		"title":     "Whatever",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusNotFound)
}

// TestServeAISessionUpdate_BodySessionIDForeignProject verifies the project
// ownership guard: a body sessionId belonging to another project is rejected.
func TestServeAISessionUpdate_BodySessionIDForeignProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	foreign := filepath.Join(env.WatchDir, "other-project")
	require.NoError(t, os.MkdirAll(foreign, 0o755))
	sessionID, err := service.CreateSession(foreign, "claude", "Foreign", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sessionID,
		"title":     "Should Not Apply",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusForbidden)
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

// ── DestroySession: DB-only removal, ACP connection closed (never deleted) ──

func TestDestroySession_ClosesACPConnAndHardDeletes(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

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

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "destroy-me", "acp-test-agent", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "acp-session-destroy")
	mgr.SetConnForTest(sessionID, conn)
	// Don't defer cleanup — DestroySession should close it.

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DestroySession, req)
	assertOK(t, w)

	// The ACP connection is closed (CloseConn removes it from the manager) —
	// never ACP session/delete'd. The connection being gone proves the close
	// path ran; DestroySession no longer calls DeleteSession at all.
	assert.Eventually(t, func() bool { return mgr.GetConn(sessionID) == nil }, 2*time.Second, 10*time.Millisecond, "ACP connection should be closed by DestroySession")

	// DB records are gone (chat_sessions hard-deleted, no orphans in history).
	var nSessions, nHistory int
	db := service.UnsafeDBForTest()
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", sessionID).Scan(&nSessions))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&nHistory))
	assert.Equal(t, 0, nSessions, "chat_sessions row should be hard-deleted")
	assert.Equal(t, 0, nHistory, "chat_history rows should be removed")
}

func TestDestroySession_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(DestroySession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDestroySession_BadMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/destroy", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(DestroySession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeAISessionUpdate_ThinkingEffort_UsesAdvertisedWireID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "effort-wire-agent", Backend: "claude", Transport: "acp-stdio", AcpCommand: "claude --acp"}
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-effort-wire", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	conn := ai.NewACPConnForTest(agent, sessionID)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(sessionID, "acp-sid-effort-wire")
	// claude-agent-acp advertises its effort option as "effort" (issue #429);
	// a caller passing the legacy spelling must still resolve to it.
	conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})

	var (
		mu   sync.Mutex
		sent []string // captured wire config ids
	)
	conn.SetConfigOptionFnForTest(func(_ context.Context, _, configID, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, configID)
		return nil
	})
	ai.GetACPConnManager().SetConnForTest(sessionID, conn)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sessionID) })

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"thinkingEffort": "high",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// The forward is async; wait for the RPC to land.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sent) == 1
	}, 2*time.Second, 5*time.Millisecond, "PATCH must forward the effort change to the ACP agent")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"effort"}, sent, "handler must send the agent-advertised wire id, not the legacy hardcoded one")
}

func TestServeAISessionUpdate_ThinkingEffort_FallbackWhenUnadvertised(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "effort-fallback-agent", Backend: "claude", Transport: "acp-stdio", AcpCommand: "claude --acp"}
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "patch-effort-fallback", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	conn := ai.NewACPConnForTest(agent, sessionID)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(sessionID, "acp-sid-effort-fallback")
	// No advertised ids → historical hardcoded "thinkingEffort" is used.

	var (
		mu   sync.Mutex
		sent []string
	)
	conn.SetConfigOptionFnForTest(func(_ context.Context, _, configID, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, configID)
		return nil
	})
	ai.GetACPConnManager().SetConnForTest(sessionID, conn)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sessionID) })

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"thinkingEffort": "medium",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sent) == 1
	}, 2*time.Second, 5*time.Millisecond, "PATCH must forward the effort change to the ACP agent")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"thinkingEffort"}, sent, "unadvertised agent keeps the historical wire id")
}
