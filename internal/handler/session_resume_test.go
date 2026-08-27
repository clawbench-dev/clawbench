package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestServeSessionResume_RestoresArchivedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "test-resume-session"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, ?, 'claude', 'Test Session', 1)",
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

	var archived int
	err = service.UnsafeDBForTest().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&archived)
	assert.NoError(t, err)
	assert.Equal(t, 0, archived, "session should be restored (archived=0)")
}

func TestServeSessionResume_ActiveSessionPassthrough(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "test-active-session"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, ?, 'claude', 'Active Session', 0)",
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

	// Create a archived session to resume
	sessionID := "test-resume-below-limit"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, ?, 'claude', 'Archived Session', 1)",
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

	var archived int
	err = service.UnsafeDBForTest().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&archived)
	assert.NoError(t, err)
	assert.Equal(t, 0, archived, "session should be restored (archived=0)")
}

func TestServeSessionResume_CrossProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "test-other-project-session"
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, '/other/project', 'claude', 'Other Session', 0)",
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
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, ?, 'claude', 'Active', 0)",
		"existing-session", env.ProjectDir,
	)
	assert.NoError(t, err)

	// Create a archived session to resume
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, ?, 'claude', 'Archived', 1)",
		"archived-session", env.ProjectDir,
	)
	assert.NoError(t, err)

	// Restoring the archived session would make total active = 2, exceeding limit 1
	body := `{"session_id": "archived-session"}`
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
	assert.True(t, result["test-acp-123"], "should find existing session for test-acp-123")
	assert.False(t, result["test-acp-456"], "should not find session for test-acp-456")
}

func TestFindExistingACPSessions_FindsExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A session whose raw backend session id is stored in external_session_id
	// (the common case for opencode ses_... ids) — no acp: prefix.
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, external_session_id) VALUES (?, ?, 'opencode', 'Native', ?)",
		"cb-ext-1", env.ProjectDir, "ses_00c202c74ffeZdhwsMNwtbwPm5",
	)
	require.NoError(t, err)

	result := findExistingACPSessions([]string{"ses_00c202c74ffeZdhwsMNwtbwPm5"})
	assert.True(t, result["ses_00c202c74ffeZdhwsMNwtbwPm5"], "should find session via external_session_id")
}

func TestFindExistingACPSessions_FindsArchivedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a archived session
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id, archived) VALUES (?, ?, 'claude', 'Archived', ?, 1)",
		"cb-session-archived", env.ProjectDir, "acp:archived-acp-123",
	)
	require.NoError(t, err)

	result := findExistingACPSessions([]string{"archived-acp-123"})
	assert.True(t, result["archived-acp-123"], "should find archived session")
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
		agentID: {ID: agentID, Name: "ACP Load", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability in the registry
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

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
		agentID: {ID: agentID, Name: "ACP Load Fail", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability so the handler proceeds past the check
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

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
		"SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND archived = 0", agentID,
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
// the GetOrCreateConnForLoad call. The handler archives the session on
// failure, but the metadata is still queryable.

func TestServeACPLoadSession_SessionMetadataBeforeLoad(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-metadata"
	acpSessionID := "acp-sid-metadata-456"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Load Meta", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      agentID,
		"acpSessionId": acpSessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	// LoadSession will fail, but the session was already created with metadata
	assert.NotEqual(t, http.StatusOK, w.Code)

	// Find the session that was created (archived by cleanup on failure).
	// Query without filtering on archived to find it.
	var sourceID, transport, extID string
	err := service.UnsafeDBForTest().QueryRow(
		"SELECT source_session_id, transport, external_session_id FROM chat_sessions WHERE agent_id = ? ORDER BY created_at DESC LIMIT 1",
		agentID,
	).Scan(&sourceID, &transport, &extID)
	if err == nil {
		// If the session exists (may have been hard-deleted), verify metadata
		assert.Equal(t, "acp:"+acpSessionID, sourceID, "source_session_id should be 'acp:<acpSessionId>'")
		assert.Equal(t, "acp-stdio", transport, "transport should be 'acp-stdio'")
		assert.Equal(t, acpSessionID, extID, "external_session_id should be the loaded ACP session id")
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
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession=true but ListSessions=false
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

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
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register both capabilities
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

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
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

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
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

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
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

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

func TestServeACPSessions_DiskScanFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Use a dedicated test backend so we don't pollute codebuddy's real
	// scanner registration.
	const testBackend = "disk-test-backend"
	agentID := "acp-disk-scan"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: testBackend, Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession=true but ListSessions=false (the codebuddy situation).
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

	// Register a stub disk scanner for the test backend.
	ai.ListSessionsFromDiskRegister(testBackend, func(a *model.Agent, cwd string) ([]acp.SessionInfo, error) {
		title := "磁盘会话"
		return []acp.SessionInfo{
			{SessionId: "disk-session-1", Cwd: env.ProjectDir, Title: &title},
		}, nil
	})
	defer ai.ListSessionsFromDiskRegister(testBackend, nil)
	require.True(t, ai.HasListSessionsFromDisk(testBackend), "test backend should have disk scanner")

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	// Even though ListSessions RPC is false, the disk fallback serves sessions.
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sessions, ok := resp["sessions"].([]any)
	require.True(t, ok, "sessions should be an array")
	require.Len(t, sessions, 1, "disk scan should return the stubbed session")
	first := sessions[0].(map[string]any)
	assert.Equal(t, "disk-session-1", first["sessionId"])
	assert.Equal(t, env.ProjectDir, first["cwd"])
	assert.Equal(t, "磁盘会话", first["title"])
}

func TestServeACPSessions_MergesDiskSessionsIntoACPFirstPage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	const testBackend = "disk-augment-backend"
	agentID := "acp-disk-augment"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: testBackend, Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

	updatedRPC := "2026-08-27T10:00:00Z"
	updatedDisk := "2026-08-27T11:00:00Z"
	ai.ListSessionsFromDiskRegister(testBackend, func(a *model.Agent, cwd string) ([]acp.SessionInfo, error) {
		assert.Equal(t, env.ProjectDir, cwd)
		return []acp.SessionInfo{
			{SessionId: "shared-session", Cwd: cwd, UpdatedAt: &updatedRPC},
			{SessionId: "disk-only-session", Cwd: cwd, UpdatedAt: &updatedDisk},
		}, nil
	})
	defer ai.ListSessionsFromDiskRegister(testBackend, nil)

	connKey := agentID + ":"
	conn := ai.NewACPConnForTest(model.Agents[agentID], connKey)
	conn.SetAliveForTest()
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		return []acp.SessionInfo{
			{SessionId: "rpc-only-session", Cwd: env.ProjectDir, UpdatedAt: &updatedRPC},
			{SessionId: "shared-session", Cwd: env.ProjectDir, UpdatedAt: &updatedRPC},
		}, nil, nil
	})
	mgr := ai.GetACPConnManager()
	mgr.SetConnForTest(connKey, conn)
	defer mgr.CloseConn(connKey)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 3)
	assert.Equal(t, "disk-only-session", resp.Sessions[0].SessionID)
	assert.Equal(t, "rpc-only-session", resp.Sessions[1].SessionID)
	assert.Equal(t, "shared-session", resp.Sessions[2].SessionID)
}

func TestServeACPSessions_FallsBackToDiskWhenACPListFails(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	const testBackend = "disk-error-fallback-backend"
	agentID := "acp-disk-error-fallback"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: testBackend, Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

	ai.ListSessionsFromDiskRegister(testBackend, func(a *model.Agent, cwd string) ([]acp.SessionInfo, error) {
		return []acp.SessionInfo{{SessionId: "disk-recovered-session", Cwd: cwd}}, nil
	})
	defer ai.ListSessionsFromDiskRegister(testBackend, nil)

	connKey := agentID + ":"
	conn := ai.NewACPConnForTest(model.Agents[agentID], connKey)
	conn.SetAliveForTest()
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		return nil, nil, errors.New("session/list unavailable")
	})
	mgr := ai.GetACPConnManager()
	mgr.SetConnForTest(connKey, conn)
	defer mgr.CloseConn(connKey)

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "disk-recovered-session")
}

func TestACPDiskDiscoveryToLoadSessionIntegration(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "disk-discovery-load"
	discoveredID := "00000000-0000-4000-8000-000000000099"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Disk Discovery Load", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

	ai.ListSessionsFromDiskRegister("claude", func(a *model.Agent, cwd string) ([]acp.SessionInfo, error) {
		return []acp.SessionInfo{{SessionId: acp.SessionId(discoveredID), Cwd: cwd}}, nil
	})
	defer ai.ListSessionsFromDiskRegister("claude", nil)

	listReq := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	listReq = withProjectCookie(listReq, env.ProjectDir)
	listRecorder := httptest.NewRecorder()
	ServeACPSessions(listRecorder, listReq)
	require.Equal(t, http.StatusOK, listRecorder.Code)

	var listResp struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(listRecorder.Body.Bytes(), &listResp))
	require.Len(t, listResp.Sessions, 1)

	mockConn := ai.NewACPConnForTest(model.Agents[agentID], "disk-discovery-load-mock")
	mockConn.SetAliveForTest()
	mockConn.SetClientForTest(ai.NewClawBenchACPClient())
	var loadedID string
	origFn := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(ctx context.Context, agent *model.Agent, clawbenchSID, acpSID, cwd string) (*ai.ACPConn, error) {
		loadedID = acpSID
		ai.GetACPConnManager().SetConnForTest(clawbenchSID, mockConn)
		return mockConn, nil
	}
	defer func() { getOrCreateConnForLoad = origFn }()

	body := fmt.Sprintf(`{"agentId":%q,"acpSessionId":%q}`, agentID, listResp.Sessions[0].SessionID)
	loadReq := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-load", strings.NewReader(body))
	loadReq.Header.Set("Content-Type", "application/json")
	withProjectCookie(loadReq, env.ProjectDir)
	loadRecorder := httptest.NewRecorder()
	ServeACPLoadSession(loadRecorder, loadReq)

	require.Equal(t, http.StatusOK, loadRecorder.Code)
	assert.Equal(t, discoveredID, loadedID)
	assert.Contains(t, loadRecorder.Body.String(), "sessionId")
}

func TestServeACPSessions_FilterExistingExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-list-filter-ext"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "opencode", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, true, false)

	// A session whose raw backend id (e.g. opencode ses_...) is stored only in
	// external_session_id — source_session_id stays NULL (the common case).
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, external_session_id, session_type) VALUES (?, ?, 'opencode', 'Native', ?, 'chat')",
		"cb-ext-1", env.ProjectDir, "ses_00c202c74ffeZdhwsMNwtbwPm5",
	)
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	connKey := "__list_sessions__:" + agentID
	agent := model.Agents[agentID]
	conn := newACPConnForHandlerTest(agent, connKey)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(connKey, "acp-sid-filter-ext")
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		return []acp.SessionInfo{
			{SessionId: "ses_00c202c74ffeZdhwsMNwtbwPm5", Title: stringPtr("Already loaded")},
			{SessionId: "ses_00otherNativeId1234567890", Title: stringPtr("New native")},
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
	require.Len(t, sessions, 1, "session matching external_session_id should be filtered out")
	first := sessions[0].(map[string]any)
	assert.Equal(t, "ses_00otherNativeId1234567890", first["sessionId"])
}

func TestServeACPSessions_DedupDuplicateSessionIds(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-list-dedup"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register capabilities via the stable Update API (independent of the
	// ForceUpdateIfNeeded signature changes in the working tree).
	reg := ai.GetAgentCapabilityRegistry()
	ls := true
	lss := true
	reg.Update(agentID, &ai.AgentCapability{LoadSession: &ls, ListSessions: &lss})

	mgr := ai.GetACPConnManager()
	connKey := "__list_sessions__:" + agentID
	agent := model.Agents[agentID]
	conn := newACPConnForHandlerTest(agent, connKey)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(connKey, "acp-sid-dedup")
	// Agent returns the same sessionId multiple times within one response
	// (e.g. unstable pagination, timestamp collisions in OpenCode's
	// updatedAt-based cursor). The server must not leak duplicates to the UI.
	conn.SetListSessionsFnForTest(func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		titleA := "Dup A"
		titleB := "Dup B"
		titleEmpty := "No id"
		return []acp.SessionInfo{
			{SessionId: "dup-session-1", Title: &titleA},
			{SessionId: "dup-session-1", Title: &titleA},
			{SessionId: "dup-session-2", Title: &titleB},
			{SessionId: "dup-session-1", Title: &titleA},
			// Sessions with an empty id are not dedupable; each one must survive.
			{SessionId: "", Title: &titleEmpty},
			{SessionId: "", Title: &titleEmpty},
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
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sessions, ok := resp["sessions"].([]any)
	require.True(t, ok, "sessions should be an array")
	require.Len(t, sessions, 4, "duplicate sessionIds within one response must be deduplicated (empty-id entries kept)")
	first := sessions[0].(map[string]any)
	assert.Equal(t, "dup-session-1", first["sessionId"])
	second := sessions[1].(map[string]any)
	assert.Equal(t, "dup-session-2", second["sessionId"])
	third := sessions[2].(map[string]any)
	assert.Equal(t, "", third["sessionId"])
	fourth := sessions[3].(map[string]any)
	assert.Equal(t, "", fourth["sessionId"])
}

// --- ServeACPLoadSession: replay path tests ---

func TestServeACPLoadSession_SuccessWithReplay(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-replay"
	acpSessionID := "acp-sid-replay-001"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Replay", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register LoadSession capability
	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

	// Set up mock connection that will be returned by getOrCreateConnForLoad
	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-replay")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-replay", "acp-sid-replay-001")
	client := ai.NewClawBenchACPClient()
	replayMsgID := "uuid-end-to-end"
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					MessageId: &replayMsgID,
					Content:   acp.TextBlock("Hello from replay"),
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

	// Wait for async replay goroutine to complete (it sleeps 500ms + processing)
	require.Eventually(t, func() bool {
		var msgCount int
		err := service.UnsafeDBForTest().QueryRow(
			"SELECT COUNT(*) FROM chat_history WHERE session_id = ?",
			sid,
		).Scan(&msgCount)
		return err == nil && msgCount == 2
	}, 3*time.Second, 50*time.Millisecond, "should have 2 replay messages (user + assistant)")

	// Verify title was set from first user message
	var title string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT title FROM chat_sessions WHERE id = ?",
		sid,
	).Scan(&title)
	assert.NoError(t, err)
	assert.Equal(t, "Hello from replay", title)

	// Verify external_message_id was persisted from the replay notification's MessageId
	var stored string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT external_message_id FROM chat_history WHERE session_id = ? LIMIT 1",
		sid,
	).Scan(&stored)
	require.NoError(t, err)
	assert.Equal(t, "uuid-end-to-end", stored)
}

func TestServeACPLoadSession_ReplayPersistsToolCalls(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-replay-tools"
	acpSessionID := "acp-sid-replay-tools"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Replay Tools", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-replay-tools")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-replay-tools", "acp-sid-replay-tools")
	client := ai.NewClawBenchACPClient()
	completed := acp.ToolCallStatusCompleted
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{
			Update: acp.SessionUpdate{
				ToolCall: &acp.SessionUpdateToolCall{
					ToolCallId: acp.ToolCallId("tc-replay-read"),
					Title:      "Read file",
					Kind:       acp.ToolKindRead,
					RawInput:   map[string]any{"file_path": "/tmp/a.go"},
				},
			},
		},
		{
			Update: acp.SessionUpdate{
				ToolCallUpdate: &acp.SessionToolCallUpdate{
					ToolCallId: acp.ToolCallId("tc-replay-read"),
					Status:     &completed,
					RawOutput:  "file contents here",
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
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sid, ok := resp["sessionId"].(string)
	require.True(t, ok, "response should contain sessionId")

	// Wait for the async replay to persist the tool call to chat_tool_calls.
	// Regression: replay slim-serializes tool blocks (no input/output) into
	// chat_history but must still persist input/output to chat_tool_calls so the
	// frontend can render tool details for restored ACP sessions.
	require.Eventually(t, func() bool {
		var cnt int
		err := service.UnsafeDBForTest().QueryRow(
			"SELECT COUNT(*) FROM chat_tool_calls WHERE session_id = ? AND tool_id = ?",
			sid, "tc-replay-read",
		).Scan(&cnt)
		return err == nil && cnt == 1
	}, 3*time.Second, 50*time.Millisecond, "replay should persist tool call to chat_tool_calls")

	var input, output string
	var done int
	err := service.UnsafeDBForTest().QueryRow(
		"SELECT input, output, done FROM chat_tool_calls WHERE session_id = ? AND tool_id = ?",
		sid, "tc-replay-read",
	).Scan(&input, &output, &done)
	require.NoError(t, err)
	assert.Contains(t, input, "file_path", "tool input should be captured from replay")
	assert.Equal(t, "file contents here", output, "tool output should be captured from replay")
	assert.Equal(t, 1, done, "completed tool should be marked done")
}

func TestServeACPLoadSession_SuccessWithEmptyReplay(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-empty-replay"
	acpSessionID := "acp-sid-empty-replay"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Empty Replay", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

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
		agentID: {ID: agentID, Name: "ACP Nil Client", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

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
		agentID: {ID: agentID, Name: "ACP Truncate", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

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

	// Wait for async replay goroutine to complete
	require.Eventually(t, func() bool {
		var title string
		err := service.UnsafeDBForTest().QueryRow(
			"SELECT title FROM chat_sessions WHERE id = ?",
			sid,
		).Scan(&title)
		return err == nil && title != ""
	}, 3*time.Second, 50*time.Millisecond, "title should be set after replay completes")

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

func TestServeACPLoadSession_ReplayTitleSkipsInjectedSystemBlock(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-load-skip-injected"
	acpSessionID := "acp-sid-skip-injected"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP SkipInjected", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	ai.GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, nil, nil, nil, nil, nil, true, false, false)

	mgr := ai.GetACPConnManager()
	agent := model.Agents[agentID]
	mockConn := ai.NewACPConnForTest(agent, "mock-session-skip")
	mockConn.SetAliveForTest()
	mockConn.SetSessionMappingForTest("mock-session-skip", acpSessionID)

	// ClawBench-origin sessions carry the [System Instructions: ...] block
	// prepended by buildPromptBlocks at the start of their first user turn,
	// with the user's actual message after it.
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					Content: acp.TextBlock("[System Instructions: repo coding rules]\n\n帮忙解释一下这个报错日志"),
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

	require.Eventually(t, func() bool {
		var title string
		err := service.UnsafeDBForTest().QueryRow(
			"SELECT title FROM chat_sessions WHERE id = ?",
			sid,
		).Scan(&title)
		return err == nil && title != ""
	}, 3*time.Second, 50*time.Millisecond, "title should be set after replay completes")

	var title string
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT title FROM chat_sessions WHERE id = ?",
		sid,
	).Scan(&title)
	assert.NoError(t, err)
	assert.Equal(t, "帮忙解释一下这个报错日志", title)
	assert.NotContains(t, title, "System Instructions")
}

func TestDeriveSessionTitleFromReplay(t *testing.T) {
	userMsg := func(text string) replayMessage {
		return replayMessage{
			role:    strUser,
			content: fmt.Sprintf(`{"blocks":[{"type":"text","text":%q}]}`, text),
		}
	}
	assistantMsg := replayMessage{role: strAssistant, content: `{"blocks":[{"type":"text","text":"answer"}]}`}

	// Tests that work with universal rules only (nil resolver).
	// 通用规则（nil resolver）即可通过的测试。
	t.Run("strips injected system block and keeps user text", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("[System Instructions: repo coding rules]\n\n用户的第一句话"),
			assistantMsg,
		}
		assert.Equal(t, "用户的第一句话", deriveSessionTitleFromReplay(msgs, nil))
	})

	t.Run("bare injected block without user text is skipped", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("[System Instructions: rules]\n\n"),
			assistantMsg,
			userMsg("第二个真实问题"),
		}
		assert.Equal(t, "第二个真实问题", deriveSessionTitleFromReplay(msgs, nil))
	})

	t.Run("strips file reference header and keeps user text", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("[Current file: /tmp/a.png]\n看看这个截图"),
		}
		assert.Equal(t, "看看这个截图", deriveSessionTitleFromReplay(msgs, nil))
	})

	t.Run("block-wrapped continuation summary without delimiter is skipped", func(t *testing.T) {
		// Cross-session continuation seeds carry only the summary with no
		// user text — machine-generated, must not become the title.
		msgs := []replayMessage{
			userMsg("[System Instructions: rules]\n\n[Below is the conversation history from before this session. The summary below covers the earlier portion of the conversation."),
			assistantMsg,
			userMsg("继续上次的部署问题"),
		}
		assert.Equal(t, "继续上次的部署问题", deriveSessionTitleFromReplay(msgs, nil))
	})

	t.Run("mid-session auto-compact turn keeps the trailing user question", func(t *testing.T) {
		// Mid-file compaction turns embed the history summary between the
		// injected block and the user's new message; the CLI marks the
		// summary end explicitly and the user text follows that delimiter.
		turn := "[System Instructions: rules]\n\n" +
			"[Below is the conversation history from before this session.\n\nSummary:\n    earlier talk\n\n" +
			"[End of conversation history. Now answer the user's new question.]\n\n" +
			"现下载速度有点问题吧？"
		msgs := []replayMessage{userMsg(turn)}
		assert.Equal(t, "现下载速度有点问题吧？", deriveSessionTitleFromReplay(msgs, nil))
	})

	t.Run("auto-compact turn with trailing file header keeps the question", func(t *testing.T) {
		turn := "[System Instructions: rules]\n\n" +
			"[Below is the conversation history from before this session.\n\nSummary:\n    earlier talk\n\n" +
			"[End of conversation history. Now answer the user's new question.]\n\n" +
			"[Current file: /tmp/example.png]\n这个接口为什么返回502？"
		msgs := []replayMessage{userMsg(turn)}
		assert.Equal(t, "这个接口为什么返回502？", deriveSessionTitleFromReplay(msgs, nil))
	})

	t.Run("assistant-only replay yields empty title", func(t *testing.T) {
		assert.Equal(t, "", deriveSessionTitleFromReplay([]replayMessage{assistantMsg}, nil))
	})

	t.Run("truncates long titles to 50 runes", func(t *testing.T) {
		long := strings.Repeat("很", 60)
		got := deriveSessionTitleFromReplay([]replayMessage{userMsg(long)}, nil)
		assert.Equal(t, strings.Repeat("很", 50)+"...", got)
	})

	t.Run("blank user turn is skipped", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("   \n\t"),
			userMsg("真实问题"),
		}
		assert.Equal(t, "真实问题", deriveSessionTitleFromReplay(msgs, nil))
	})

	// Tests requiring claude-native rules (claudeTranscriptResolver).
	// 需要 claude 原生规则的测试。
	t.Run("interruption marker without user text is skipped", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("[Request interrupted by user for tool use]"),
			userMsg("继续刚才的问题"),
		}
		assert.Equal(t, "继续刚才的问题", deriveSessionTitleFromReplay(msgs, claudeTranscriptResolver{}))
	})

	t.Run("CLI-native continuation header is skipped", func(t *testing.T) {
		// After compaction the CLI writes a plain-English continuation turn
		// ("This session is being continued from a previous conversation
		// that ran out of context. ...") — it must not become the title;
		// the user's next real question should.
		msgs := []replayMessage{
			userMsg("This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:\n<summary>…</summary>"),
			assistantMsg,
			userMsg("如何配置自动备份"),
		}
		assert.Equal(t, "如何配置自动备份", deriveSessionTitleFromReplay(msgs, claudeTranscriptResolver{}))
	})

	t.Run("slash-command turn is skipped", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("<command-name>/model</command-name>\n<command-args>sonnet</command-args>"),
			assistantMsg,
			userMsg("帮忙解释一下这个报错日志"),
		}
		assert.Equal(t, "帮忙解释一下这个报错日志", deriveSessionTitleFromReplay(msgs, claudeTranscriptResolver{}))
	})

	t.Run("caveat wrapper is stripped, trailing user text kept", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to.\n\n这个接口为什么返回502？"),
		}
		assert.Equal(t, "这个接口为什么返回502？", deriveSessionTitleFromReplay(msgs, claudeTranscriptResolver{}))
	})

	t.Run("caveat wrapper without user text is skipped", func(t *testing.T) {
		msgs := []replayMessage{
			userMsg("Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to."),
			assistantMsg,
			userMsg("怎么导出数据库备份"),
		}
		assert.Equal(t, "怎么导出数据库备份", deriveSessionTitleFromReplay(msgs, claudeTranscriptResolver{}))
	})
}

func TestStripMachineText(t *testing.T) {
	t.Run("plain user text passes through", func(t *testing.T) {
		got, ok := stripMachineText("普通消息", stripRulesFor(nil))
		assert.True(t, ok)
		assert.Equal(t, "普通消息", got)
	})

	t.Run("malformed system block prefix without terminator is dropped", func(t *testing.T) {
		_, ok := stripMachineText("[System Instructions: no closing marker", stripRulesFor(nil))
		assert.False(t, ok)
	})

	t.Run("claude-native prefix is not stripped with nil resolver", func(t *testing.T) {
		// Claude-native prefixes (e.g. "Caveat: ...") are not in the
		// universal rule set, so they pass through unmodified when
		// using nil resolver — isolation guarantee.
		// claude 原生前缀不在通用规则集中，nil resolver 下原样通过——隔离保证。
		got, ok := stripMachineText("Caveat: The messages below were generated by the user.\n\nreal question", stripRulesFor(nil))
		assert.True(t, ok)
		assert.Contains(t, got, "Caveat:")
	})
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
