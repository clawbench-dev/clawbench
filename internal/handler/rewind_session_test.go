package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ServeSessionRewind: POST /api/ai/session/rewind ──────────────────────

func TestServeSessionRewind_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/rewind", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeSessionRewind_MissingProjectCookie(t *testing.T) {
	body := map[string]any{"sessionId": "s1", "beforeMessageId": 3}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeSessionRewind_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"beforeMessageId": 3}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionRewind_MissingBeforeMessageId(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	body := map[string]any{"sessionId": sessID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionRewind_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession("/other-project", "claude", "Other", "claude", "", "default", "chat")
	require.NoError(t, err)

	body := map[string]any{"sessionId": sessionID, "beforeMessageId": 1}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeSessionRewind_NonexistentSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"sessionId": "nonexistent-session", "beforeMessageId": 1}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeSessionRewind_SuccessTruncatesAndResetsSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	asst1ID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)
	// Deleted message stored in the real JSON-blocks format.
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", `{"blocks":[{"type":"text","text":"editable Q2"}]}`, nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A2", nil, false, "")
	require.NoError(t, err)

	// Simulate a stale AI-side mapping that must be cleared.
	require.NoError(t, service.UpdateExternalSessionID(sessionID, "ext-stale-session"))
	assert.Equal(t, "ext-stale-session", service.GetExternalSessionID(sessionID))

	// Inject a fake ACP connection into the pool.
	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(sessionID, "ext-stale-session")
	mgr.SetConnForTest(sessionID, conn)

	body := map[string]any{"sessionId": sessionID, "beforeMessageId": asst1ID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))
	assert.Equal(t, sessionID, result["sessionId"])
	assert.Equal(t, float64(2), result["deletedCount"])
	assert.Equal(t, "editable Q2", result["restoredText"])

	// History truncated in place.
	msgs, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "Q1", msgs[0].Content)
	assert.Equal(t, "A1", msgs[1].Content)

	// External session mapping cleared.
	assert.Equal(t, "", service.GetExternalSessionID(sessionID))

	// ACP connection closed (goroutine — wait briefly).
	assert.Eventually(t, func() bool { return mgr.GetConn(sessionID) == nil },
		2*time.Second, 10*time.Millisecond, "ACP connection should be closed by rewind")
}

func TestServeSessionRewind_NoConnIsStillOK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	asstID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q2", nil, false, "")
	require.NoError(t, err)

	body := map[string]any{"sessionId": sessionID, "beforeMessageId": asstID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assertOK(t, w)
}

func TestServeSessionRewind_NothingToRewind(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	asstID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)

	// Anchor is the last message — nothing after it, and the mapping must NOT be cleared.
	require.NoError(t, service.UpdateExternalSessionID(sessionID, "ext-keep"))
	body := map[string]any{"sessionId": sessionID, "beforeMessageId": asstID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	assert.Equal(t, "ext-keep", service.GetExternalSessionID(sessionID),
		"a no-op rewind must not clear the AI-side mapping")
}

func TestServeSessionRewind_NothingToRewindDoesNotCancelRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	asstID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)

	// Mark the session as running (simulates an in-flight AI turn answering the
	// anchor's question that has not yet produced a reply row after A1).
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	// Nothing follows the anchor — a no-op rewind. The running session must NOT
	// be cancelled by this failed request.
	body := map[string]any{"sessionId": sessionID, "beforeMessageId": asstID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	assert.True(t, service.IsSessionRunning(sessionID),
		"a nothing-to-rewind request must not cancel the running session")
}

func TestServeSessionRewind_InvalidAnchor(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	userID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)

	// Anchor is a user message — invalid rewind point.
	body := map[string]any{"sessionId": sessionID, "beforeMessageId": userID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionRewind_InvalidAnchorDoesNotCancelRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)
	user2ID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q2", nil, false, "")
	require.NoError(t, err)

	// Mark the session as running (simulates an in-flight AI turn).
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	// Anchor is a user message — invalid. The running session must NOT be
	// cancelled by this failed request (anchor validation runs first).
	body := map[string]any{"sessionId": sessionID, "beforeMessageId": user2ID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	assert.True(t, service.IsSessionRunning(sessionID),
		"an invalid rewind request must not cancel the running session")
}

func TestServeSessionRewind_SuccessCancelsRunningSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	asstID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q2", nil, false, "")
	require.NoError(t, err)

	// A running session must be cancelled and drained before truncation.
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	body := map[string]any{"sessionId": sessionID, "beforeMessageId": asstID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assertOK(t, w)

	assert.False(t, service.IsSessionRunning(sessionID),
		"a valid rewind of a running session must cancel it")
}

func TestServeSessionRewind_TruncateErrorIsMappedTo400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Session exists but the anchor row was deleted between validation and
	// truncation (concurrent rewind race). TruncateSessionAfterMessage then
	// returns ErrRewindAnchorNotFound, which the handler maps to 400.
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Rewind Session", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Q1", nil, false, "")
	require.NoError(t, err)
	asstID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "A1", nil, false, "")
	require.NoError(t, err)

	// Directly exercising the handler path that maps a truncation error to 400:
	// validate passes, but a prior no-op rewind deleted the row.
	_, err = service.TruncateSessionAfterMessage(sessionID, asstID)
	require.NoError(t, err)

	body := map[string]any{"sessionId": sessionID, "beforeMessageId": asstID}
	req := newRequest(t, http.MethodPost, "/api/ai/session/rewind", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionRewind_MalformedJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/rewind", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeSessionRewind_MethodNotAllowedForGet(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/ai/session/rewind", http.NoBody)
	req = withProjectCookie(req, "proj")
	w := callHandler(ServeSessionRewind, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
