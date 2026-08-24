package handler

import (
	"net/http"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeSessionReset_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/session/reset", nil)
	w := callHandler(ServeSessionReset, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeSessionReset_MissingProjectCookie(t *testing.T) {
	body := map[string]any{"sessionId": "s1"}
	req := newRequest(t, http.MethodPost, "/api/ai/session/reset", body)
	w := callHandler(ServeSessionReset, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeSessionReset_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{}
	req := newRequest(t, http.MethodPost, "/api/ai/session/reset", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionReset, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeSessionReset_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session under a different project path
	otherProject := "/other-project"
	sessionID, err := service.CreateSession(otherProject, "claude", "Other Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Request with a cookie for env.ProjectDir, but session belongs to otherProject
	body := map[string]any{"sessionId": sessionID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/reset", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionReset, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeSessionReset_NonexistentSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"sessionId": "nonexistent-session"}
	req := newRequest(t, http.MethodPost, "/api/ai/session/reset", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionReset, req)
	// GetSessionProjectPath returns "" which won't match the project cookie path
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeSessionReset_PreservesMappingAndClosesConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Reset Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Set an external session ID mapping so we can verify it is PRESERVED —
	// reset recycles the agent process but keeps the mapping so the next
	// prompt resumes the same agent session (context preserved).
	require.NoError(t, service.UpdateExternalSessionID(sessionID, "ext-session-123"))
	assert.Equal(t, "ext-session-123", service.GetExternalSessionID(sessionID))

	// Inject a fake ACP connection into the pool
	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "ext-session-123")
	mgr.SetConnForTest(sessionID, conn)

	body := map[string]any{"sessionId": sessionID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/reset", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionReset, req)
	assertOK(t, w)

	// external_session_id must be preserved so ResumeSession can re-attach
	assert.Equal(t, "ext-session-123", service.GetExternalSessionID(sessionID))

	// ACP connection should be closed (runs in goroutine, wait briefly)
	assert.Eventually(t, func() bool { return mgr.GetConn(sessionID) == nil },
		2*time.Second, 10*time.Millisecond, "ACP connection should be closed by reset")
}

func TestServeSessionReset_NoConnIsStillOK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Session exists but no ACP connection in pool — reset should be a safe no-op
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Reset Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.UpdateExternalSessionID(sessionID, "ext-session-456"))

	body := map[string]any{"sessionId": sessionID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/reset", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionReset, req)
	assertOK(t, w)

	// Mapping preserved even with no connection in the pool
	assert.Equal(t, "ext-session-456", service.GetExternalSessionID(sessionID))
}
