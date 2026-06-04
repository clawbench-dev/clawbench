package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
)

func TestServeSessionMode_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/session/mode", nil)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeSessionMode_MissingProjectCookie(t *testing.T) {
	body := map[string]any{
		"sessionId": "s1",
		"modeId":    "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeSessionMode_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{
		"modeId": "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeSessionMode_MissingModeID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{
		"sessionId": "s1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeSessionMode_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Session ID that doesn't exist in DB — GetSessionProjectPath returns ""
	// which won't match the project cookie path → 403
	body := map[string]any{
		"sessionId": "nonexistent-session",
		"modeId":    "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeSessionMode_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session under a different project path
	otherProject := "/other-project"
	sessionID, err := service.CreateSession(otherProject, "claude", "Other Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Request with a cookie for env.ProjectDir, but session belongs to otherProject
	body := map[string]any{
		"sessionId": sessionID,
		"modeId":    "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeSessionMode_AgentNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with an agent_id that's not in model.Agents
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "unknown-agent", "", "default", "chat")
	require.NoError(t, err)

	body := map[string]any{
		"sessionId": sessionID,
		"modeId":    "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeSessionMode_NoACPConnection(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with "claude" agent (exists in model.Agents)
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No ACP pool entry for "claude" — GetOrCreate will fail
	// because there's no acp_command configured
	body := map[string]any{
		"sessionId": sessionID,
		"modeId":    "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeSessionMode_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with "claude" agent
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Inject a pool entry with a client and session mapping
	pool := ai.GetACPConnectionPool()
	client := ai.NewClawBenchACPClient()
	entry := &ai.ACPConnEntry{}
	entry.SetClientForTest(client)
	entry.SetSessionMappingForTest(sessionID, "acp-session-mode-test")
	entry.SetAliveForTest()
	pool.SetEntryForTest("claude", entry)
	defer pool.CloseConnection("claude")

	body := map[string]any{
		"sessionId": sessionID,
		"modeId":    "code",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/session/mode", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusOK)
	assertJSONField(t, w, "ok", true)
	assertJSONField(t, w, "modeId", "code")
}

func TestServeSessionMode_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/mode", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionMode, req)
	assertStatus(t, w, http.StatusBadRequest)
}
