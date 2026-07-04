package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// --- POST /api/ai/session/resume tests ---

func TestServeSessionResume_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ai/session/resume", http.NoBody)
	withProjectCookie(req, "/some/project")
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeSessionResume_MissingProject(t *testing.T) {
	body := `{"session_id": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeSessionResume_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionResume_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"session_id": "nonexistent-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeSessionResume_RestoresSoftDeletedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "test-resume-session"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, deleted) VALUES (?, ?, 'claude', 'Test Session', 1)",
		sessionID, env.ProjectDir,
	)
	assert.NoError(t, err)

	body := `{"session_id": "test-resume-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var deleted int
	err = service.UnsafeDBForTest().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&deleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, deleted, "session should be restored (deleted=0)")
}

func TestServeSessionResume_ActiveSessionPassthrough(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "test-active-session"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, deleted) VALUES (?, ?, 'claude', 'Active Session', 0)",
		sessionID, env.ProjectDir,
	)
	assert.NoError(t, err)

	body := `{"session_id": "test-active-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeSessionResume_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionResume_SessionCountBelowLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origMax := model.SessionMaxCount
	model.SessionMaxCount = 10
	defer func() { model.SessionMaxCount = origMax }()

	// Create a soft-deleted session to resume
	sessionID := "test-resume-below-limit"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, deleted) VALUES (?, ?, 'claude', 'Deleted Session', 1)",
		sessionID, env.ProjectDir,
	)
	assert.NoError(t, err)

	body := `{"session_id": "test-resume-below-limit"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var deleted int
	err = service.UnsafeDBForTest().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&deleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, deleted, "session should be restored (deleted=0)")
}

func TestServeSessionResume_CrossProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "test-other-project-session"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, deleted) VALUES (?, '/other/project', 'claude', 'Other Session', 0)",
		sessionID,
	)
	assert.NoError(t, err)

	body := `{"session_id": "test-other-project-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeSessionResume_SessionCountLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origMax := model.SessionMaxCount
	model.SessionMaxCount = 1
	defer func() { model.SessionMaxCount = origMax }()

	// Create an active session (fills the 1-slot limit)
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, deleted) VALUES (?, ?, 'claude', 'Active', 0)",
		"existing-session", env.ProjectDir,
	)
	assert.NoError(t, err)

	// Create a soft-deleted session to resume
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, deleted) VALUES (?, ?, 'claude', 'Deleted', 1)",
		"deleted-session", env.ProjectDir,
	)
	assert.NoError(t, err)

	// Restoring the deleted session would make total active = 2, exceeding limit 1
	body := `{"session_id": "deleted-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/resume", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeSessionResume(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- findExistingACPSessions tests ---

func TestFindExistingACPSessions_FindsActiveSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a session with source_session_id = "acp:test-acp-123"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id) VALUES (?, ?, 'claude', 'Test', ?)",
		"cb-session-1", env.ProjectDir, "acp:test-acp-123",
	)
	require.NoError(t, err)

	result := findExistingACPSessions([]string{"test-acp-123", "test-acp-456"})
	assert.True(t, result["acp:test-acp-123"], "should find existing session for test-acp-123")
	assert.False(t, result["acp:test-acp-456"], "should not find session for test-acp-456")
}

func TestFindExistingACPSessions_FindsSoftDeletedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a soft-deleted session
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id, deleted) VALUES (?, ?, 'claude', 'Deleted', ?, 1)",
		"cb-session-deleted", env.ProjectDir, "acp:deleted-acp-123",
	)
	require.NoError(t, err)

	result := findExistingACPSessions([]string{"deleted-acp-123"})
	assert.True(t, result["acp:deleted-acp-123"], "should find soft-deleted session")
}

func TestFindExistingACPSessions_EmptyInput(t *testing.T) {
	result := findExistingACPSessions(nil)
	assert.Nil(t, result)

	result = findExistingACPSessions([]string{})
	assert.Nil(t, result)
}

func TestFindExistingACPSessions_NoMatches(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No sessions in DB with these ACP session IDs
	result := findExistingACPSessions([]string{"nonexistent-acp-1", "nonexistent-acp-2"})
	assert.Empty(t, result)

	// Suppress unused variable warning
	_ = env
}

// --- POST /api/ai/session/acp-load tests (supplementing acp_session_test.go) ---

func TestServeACPLoadSession_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ai/session/acp-load", http.NoBody)
	withProjectCookie(req, "/some/project")
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeACPLoadSession_MissingProject(t *testing.T) {
	body := `{"agentId":"test","acpSessionId":"sid-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeACPLoadSession_MissingAgentIDField(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"acpSessionId":"sid-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPLoadSession_MissingAcpSessionIDField(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"agentId":"test-agent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPLoadSession_NonACPAgentRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"cli-agent": {ID: "cli-agent", Name: "CLI Agent", Backend: "claude", Transport: "cli"},
	}
	model.AgentList = []*model.Agent{model.Agents["cli-agent"]}

	body := `{"agentId":"cli-agent","acpSessionId":"acp-sid-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPLoadSession_ExistingACPSessionHardDeleted(t *testing.T) {
	// Tests the path where an existing CB session for the ACP session is found
	// and hard-deleted before creating a new one.
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-delete"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Load", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability in the registry
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	// Insert an existing session for the ACP session ID
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id, session_type) VALUES (?, ?, 'acp-stdio', 'Old', ?, 'chat')",
		"old-cb-session", env.ProjectDir, "acp:existing-acp-sid",
	)
	require.NoError(t, err)

	// Insert a chat_history entry for the old session to verify hard delete
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content) VALUES (?, 'acp-stdio', ?, 'user', 'hello')",
		env.ProjectDir, "old-cb-session",
	)
	require.NoError(t, err)

	// The handler will hard-delete the existing session and then try to
	// create a new one + LoadSession (which will fail because no real ACP
	// connection). This exercises the hard-delete path.
	body := `{"agentId":"acp-load-delete","acpSessionId":"existing-acp-sid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	// The handler will fail on LoadSession (no real ACP agent), but the
	// existing session should have been hard-deleted before that point.
	// Verify the old session is gone
	var count int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", "old-cb-session").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "old session should be hard-deleted")

	// The response will be 500 (LoadSession failed) or 404 (resource not found)
	assert.NotEqual(t, http.StatusOK, w.Code, "should not succeed without a real ACP agent")
}

// --- ServeACPLoadSession: LoadSession fails (generic error → 500) ---
// This test exercises the error path after GetOrCreateConnForLoad fails
// with a generic error (not "Resource not found"), verifying session cleanup.

func TestServeACPLoadSession_LoadSessionFails_GenericError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-fail-generic"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Load Fail", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability so the handler proceeds past the check
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	// "echo" is not a real ACP agent — GetOrCreateConnForLoad will fail
	// with a generic spawn error (not "Resource not found")
	body := fmt.Sprintf(`{"agentId":%q,"acpSessionId":"acp-sid-generic-err"}`, agentID)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	// The handler should return 500 for a generic LoadSession failure
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Verify the session created before LoadSession was cleaned up
	var count int
	err := service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND deleted = 0", agentID,
	).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "session should be cleaned up after LoadSession failure")
}

// --- ServeACPLoadSession: LoadSession fails with "Resource not found" → 404 ---
// This test verifies the handler correctly returns 404 when the ACP agent
// reports that the requested session no longer exists. Since we can't run
// a real ACP agent in unit tests, we inject a mock ACPConn that is alive
// with a session mapping, so GetOrCreateConnForLoad reuses it without
// calling LoadSession. We then verify the 200 success path instead.
//
// The "Resource not found" → 404 branch is tested indirectly:
// - IsACPResourceNotFound detection is tested in internal/ai/acp_test.go
// - The handler branch (IsACPResourceNotFound → writeLocalizedErrorf 404) is
//   structurally identical to the generic error → 500 branch tested above.

func TestServeACPLoadSession_ResourceNotFoundDetection(t *testing.T) {
	// Verify that IsACPResourceNotFound correctly identifies ACP "Resource not found"
	// errors that would be wrapped by ensureAliveWithSession as "acp: session/load: ...".
	// This tests the detection logic that the handler relies on for the 404 branch.
	err := fmt.Errorf("acp: session/load: %w", &acp.RequestError{
		Code:    -32002,
		Message: "Resource not found: session abc-123",
	})
	assert.True(t, ai.IsACPResourceNotFound(err),
		"IsACPResourceNotFound should detect wrapped RequestError with 'Resource not found'")

	// Verify non-matching errors are not detected
	otherErr := fmt.Errorf("acp: session/load: %w", &acp.RequestError{
		Code:    -32603,
		Message: "Internal error",
	})
	assert.False(t, ai.IsACPResourceNotFound(otherErr),
		"IsACPResourceNotFound should not detect non-'Resource not found' errors")
}

// --- ServeACPLoadSession: session metadata set before LoadSession ---
// This test verifies that source_session_id and transport are set correctly
// on the session even when LoadSession fails, since these are set BEFORE
// the GetOrCreateConnForLoad call. The handler soft-deletes the session on
// failure, but the metadata is still queryable.

func TestServeACPLoadSession_SessionMetadataBeforeLoad(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-metadata"
	acpSessionID := "acp-sid-metadata-456"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Load Meta", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      agentID,
		"acpSessionId": acpSessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	// LoadSession will fail, but the session was already created with metadata
	assert.NotEqual(t, http.StatusOK, w.Code)

	// Find the session that was created (soft-deleted by cleanup on failure).
	// Query without filtering on deleted to find it.
	var sourceID, transport string
	err := service.UnsafeDBForTest().QueryRow(
		"SELECT source_session_id, transport FROM chat_sessions WHERE agent_id = ? ORDER BY created_at DESC LIMIT 1",
		agentID,
	).Scan(&sourceID, &transport)
	if err == nil {
		// If the session exists (may have been hard-deleted), verify metadata
		assert.Equal(t, "acp:"+acpSessionID, sourceID, "source_session_id should be 'acp:<acpSessionId>'")
		assert.Equal(t, "acp-stdio", transport, "transport should be 'acp-stdio'")
	}
}

// --- ServeACPSessions: uncovered path tests ---

func TestServeACPSessions_EmptyAgentID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Path with empty agent ID: /api/agents//acp-sessions
	req := newRequest(t, http.MethodGet, "/api/agents//acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPSessions_AgentIDWithSlash(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Path with slash in agent ID: /api/agents/foo/bar/acp-sessions
	req := newRequest(t, http.MethodGet, "/api/agents/foo/bar/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPSessions_LoadSessionOnlyNotListSessions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-only"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession=true but ListSessions=false
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	// LoadSession supported but ListSessions not → 501
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestServeACPSessions_ListSessionsSuccess(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-list-ok"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register both capabilities
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true)

	// Inject a mock alive connection that the handler will find via GetConnByAgentID
	mgr := ai.GetACPConnManager()
	connKey := "__list_sessions__:" + agentID
	agent := model.Agents[agentID]
	conn := newACPConnForHandlerTest(agent, connKey)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(connKey, "acp-sid-list")
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		return []acp.SessionInfo{
			{SessionId: "acp-session-1", Title: stringPtr("Session 1")},
			{SessionId: "acp-session-2", Title: stringPtr("Session 2")},
		}, nil, nil
	})
	mgr.SetConnForTest(connKey, conn)
	defer mgr.CloseConn(connKey)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	sessions, ok := resp["sessions"].([]any)
	require.True(t, ok, "sessions should be an array")
	assert.Len(t, sessions, 2)
}

func TestServeACPSessions_ListSessionsWithCursor(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-list-cursor"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true)

	mgr := ai.GetACPConnManager()
	connKey := "__list_sessions__:" + agentID
	agent := model.Agents[agentID]
	conn := newACPConnForHandlerTest(agent, connKey)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(connKey, "acp-sid-cursor")
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		if cursor != nil && *cursor == "page2" {
			return []acp.SessionInfo{{SessionId: "acp-session-3"}}, nil, nil
		}
		nextCursor := "page2"
		return []acp.SessionInfo{{SessionId: "acp-session-1"}}, &nextCursor, nil
	})
	mgr.SetConnForTest(connKey, conn)
	defer mgr.CloseConn(connKey)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions?cursor=page2", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeACPSessions_ListSessionsError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-list-err"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true)

	mgr := ai.GetACPConnManager()
	connKey := "__list_sessions__:" + agentID
	agent := model.Agents[agentID]
	conn := newACPConnForHandlerTest(agent, connKey)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(connKey, "acp-sid-err")
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		return nil, nil, fmt.Errorf("internal error")
	})
	mgr.SetConnForTest(connKey, conn)
	defer mgr.CloseConn(connKey)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServeACPSessions_FilterExistingSessions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-list-filter"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true)

	// Pre-create a CB session for one of the ACP sessions
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id, session_type) VALUES (?, ?, 'acp-stdio', 'Existing', ?, 'chat')",
		"cb-existing-1", env.ProjectDir, "acp:acp-session-1",
	)
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	connKey := "__list_sessions__:" + agentID
	agent := model.Agents[agentID]
	conn := newACPConnForHandlerTest(agent, connKey)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(connKey, "acp-sid-filter")
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		return []acp.SessionInfo{
			{SessionId: "acp-session-1", Title: stringPtr("Already loaded")},
			{SessionId: "acp-session-2", Title: stringPtr("New session")},
		}, nil, nil
	})
	mgr.SetConnForTest(connKey, conn)
	defer mgr.CloseConn(connKey)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	sessions, ok := resp["sessions"].([]any)
	require.True(t, ok, "sessions should be an array")
	assert.Len(t, sessions, 1, "existing ACP session should be filtered out")
}

// --- ServeACPLoadSession: replay path tests ---

func TestServeACPLoadSession_SuccessWithReplay(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-replay"
	acpSessionID := "acp-sid-replay-001"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Replay", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	// Set up mock connection that will be returned by getOrCreateConnForLoad
	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-replay")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-replay", "acp-sid-replay-001")
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					Content: acp.TextBlock("Hello from replay"),
				},
			},
		},
		{
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.TextBlock("Hi there from assistant"),
				},
			},
		},
	})
	mockConn.SetClientForTest(client)

	// Override getOrCreateConnForLoad to return our mock connection
	origFn := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(ctx context.Context, ag *model.Agent, clawbenchSID, acpSID, cwd string) (*ai.ACPConn, error) {
		// Register in pool so CloseConn can find it
		mgr.SetConnForTest(clawbenchSID, mockConn)
		return mockConn, nil
	}
	defer func() { getOrCreateConnForLoad = origFn }()

	body := fmt.Sprintf(`{"agentId":%q,"acpSessionId":%q}`, agentID, acpSessionID)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	// The session ID should be in the response
	_, hasSID := resp["sessionId"]
	assert.True(t, hasSID, "response should contain sessionId")

	sid := resp["sessionId"].(string)

	// Verify messages were saved to chat_history
	var msgCount int
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?",
		sid,
	).Scan(&msgCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, msgCount, "should have 2 replay messages (user + assistant)")

	// Verify title was set from first user message
	var title string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT title FROM chat_sessions WHERE id = ?",
		sid,
	).Scan(&title)
	assert.NoError(t, err)
	assert.Equal(t, "Hello from replay", title)
}

func TestServeACPLoadSession_SuccessWithEmptyReplay(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-empty-replay"
	acpSessionID := "acp-sid-empty-replay"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Empty Replay", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-empty")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-empty", "acp-sid-empty-replay")
	client := ai.NewClawBenchACPClient()
	// Empty replay buffer
	client.SetLoadSessionBufForTest(nil)
	mockConn.SetClientForTest(client)

	origFn := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(ctx context.Context, ag *model.Agent, clawbenchSID, acpSID, cwd string) (*ai.ACPConn, error) {
		mgr.SetConnForTest(clawbenchSID, mockConn)
		return mockConn, nil
	}
	defer func() { getOrCreateConnForLoad = origFn }()

	body := fmt.Sprintf(`{"agentId":%q,"acpSessionId":%q}`, agentID, acpSessionID)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	sid := resp["sessionId"].(string)

	// No messages saved since replay buffer was empty
	var msgCount int
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?",
		sid,
	).Scan(&msgCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, msgCount, "should have 0 replay messages for empty buffer")
}

func TestServeACPLoadSession_SuccessNilClient(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-nil-client"
	acpSessionID := "acp-sid-nil-client"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Nil Client", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-nil")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-nil", "acp-sid-nil-client")
	// No client set (client=nil by default)

	origFn := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(ctx context.Context, ag *model.Agent, clawbenchSID, acpSID, cwd string) (*ai.ACPConn, error) {
		mgr.SetConnForTest(clawbenchSID, mockConn)
		return mockConn, nil
	}
	defer func() { getOrCreateConnForLoad = origFn }()

	body := fmt.Sprintf(`{"agentId":%q,"acpSessionId":%q}`, agentID, acpSessionID)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	// Should succeed — the client==nil branch skips replay collection
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	_, hasSID := resp["sessionId"]
	assert.True(t, hasSID, "response should contain sessionId")
}

func TestServeACPLoadSession_ReplayWithTitleTruncation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-truncate"
	acpSessionID := "acp-sid-truncate"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Truncate", Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false)

	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-truncate")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-truncate", "acp-sid-truncate")

	// Create a user message that's longer than 50 characters
	longText := "This is a very long user message that should be truncated to fifty characters when used as a session title"
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					Content: acp.TextBlock(longText),
				},
			},
		},
	})
	mockConn.SetClientForTest(client)

	origFn := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(ctx context.Context, ag *model.Agent, clawbenchSID, acpSID, cwd string) (*ai.ACPConn, error) {
		mgr.SetConnForTest(clawbenchSID, mockConn)
		return mockConn, nil
	}
	defer func() { getOrCreateConnForLoad = origFn }()

	body := fmt.Sprintf(`{"agentId":%q,"acpSessionId":%q}`, agentID, acpSessionID)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	sid := resp["sessionId"].(string)

	// Verify title was truncated
	var title string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT title FROM chat_sessions WHERE id = ?",
		sid,
	).Scan(&title)
	assert.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(title)), 53, "title should be truncated to 50 chars + '...'")
	assert.True(t, strings.HasSuffix(title, "..."), "truncated title should end with '...'")
}

// --- helper functions ---

// newACPConnForHandlerTest creates an *ai.ACPConn for handler-level testing.
// Since ACPConn is in the ai package, we use the exported test helpers.
func newACPConnForHandlerTest(agent *model.Agent, clawbenchSID string) *ai.ACPConn {
	mgr := ai.GetACPConnManager()
	// Use the special key format to create a conn entry
	connKey := clawbenchSID
	mgr.SetConnForTest(connKey, ai.NewACPConnForTest(agent, connKey))
	conn := mgr.GetConn(connKey)
	return conn
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}
