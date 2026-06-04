package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
)

func TestServePermissionRespond_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/permission/respond", nil)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServePermissionRespond_MissingProjectCookie(t *testing.T) {
	body := map[string]any{
		"sessionId":  "s1",
		"toolCallId": "t1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServePermissionRespond_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{
		"toolCallId": "t1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServePermissionRespond_MissingToolCallID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{
		"sessionId": "s1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServePermissionRespond_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Session ID that doesn't exist in DB — GetSessionProjectPath returns ""
	// which won't match the project cookie path
	body := map[string]any{
		"sessionId":  "nonexistent-session",
		"toolCallId": "t1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServePermissionRespond_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session under a different project path
	otherProject := "/other-project"
	sessionID, err := service.CreateSession(otherProject, "claude", "Other Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Request with a cookie for env.ProjectDir, but session belongs to otherProject
	body := map[string]any{
		"sessionId":  sessionID,
		"toolCallId": "t1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServePermissionRespond_SessionExistsNoACPClient(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the test project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No ACP connection pool entry for "claude" agent — GetClient returns nil
	body := map[string]any{
		"sessionId":  sessionID,
		"toolCallId": "t1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServePermissionRespond_ACPClientNoSessionMapping(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the test project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Inject a pool entry with a client but no session mapping for this ClawBench session
	pool := ai.GetACPConnectionPool()
	client := ai.NewClawBenchACPClient()
	entry := &ai.ACPConnEntry{}
	entry.SetClientForTest(client)
	pool.SetEntryForTest("claude", entry)
	defer pool.CloseConnection("claude")

	body := map[string]any{
		"sessionId":  sessionID,
		"toolCallId": "t1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServePermissionRespond_PermissionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the test project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Inject a pool entry with a client and session mapping
	pool := ai.GetACPConnectionPool()
	client := ai.NewClawBenchACPClient()
	entry := &ai.ACPConnEntry{}
	entry.SetClientForTest(client)
	entry.SetSessionMappingForTest(sessionID, "acp-session-123")
	pool.SetEntryForTest("claude", entry)
	defer pool.CloseConnection("claude")

	// No pending permission registered — RespondPermission returns false
	body := map[string]any{
		"sessionId":  sessionID,
		"toolCallId": "t1",
		"optionId":   "allow",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServePermissionRespond_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the test project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Set up ACP pool with client and session mapping
	pool := ai.GetACPConnectionPool()
	client := ai.NewClawBenchACPClient()
	entry := &ai.ACPConnEntry{}
	entry.SetClientForTest(client)
	entry.SetSessionMappingForTest(sessionID, "acp-session-456")
	pool.SetEntryForTest("claude", entry)
	defer pool.CloseConnection("claude")

	// Register a pending permission so RespondPermission finds it
	key := ai.PermissionKey("acp-session-456", "toolcall-1")
	client.RegisterPendingPermissionForTest(key, &ai.PendingPermissionForTest{})

	body := map[string]any{
		"sessionId":  sessionID,
		"toolCallId": "toolcall-1",
		"optionId":   "allow",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusOK)
	assertJSONField(t, w, "ok", true)
}

func TestServePermissionRespond_Cancelled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the test project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Set up ACP pool with client and session mapping
	pool := ai.GetACPConnectionPool()
	client := ai.NewClawBenchACPClient()
	entry := &ai.ACPConnEntry{}
	entry.SetClientForTest(client)
	entry.SetSessionMappingForTest(sessionID, "acp-session-789")
	pool.SetEntryForTest("claude", entry)
	defer pool.CloseConnection("claude")

	// Register a pending permission
	key := ai.PermissionKey("acp-session-789", "toolcall-2")
	client.RegisterPendingPermissionForTest(key, &ai.PendingPermissionForTest{})

	body := map[string]any{
		"sessionId":  sessionID,
		"toolCallId": "toolcall-2",
		"cancelled":  true,
	}
	req := newRequest(t, http.MethodPost, "/api/ai/permission/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusOK)
	assertJSONField(t, w, "ok", true)
}

func TestServePermissionRespond_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/ai/permission/respond", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServePermissionRespond, req)
	assertStatus(t, w, http.StatusBadRequest)
}
