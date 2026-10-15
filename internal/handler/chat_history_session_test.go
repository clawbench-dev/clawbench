package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"clawbench/internal/store"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeToolCallDetail ---

func TestServeToolCallDetail_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)

	err = service.UpsertToolCall(msgID, sessionID, "toolu_td01", "Read", json.RawMessage(`{"file_path":"/tmp/test.go"}`), "contents", "success", "test.go", true, 0)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=toolu_td01&message_id="+fmt.Sprintf("%d", msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "Read", result["name"])
}

func TestServeToolCallDetail_MissingBothParams(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeToolCallDetail_BadMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=toolu_01&message_id=invalid", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeToolCallDetail_NoRecord(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=nonexistent&message_id=1", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeToolCallDetail_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)

	err = service.UpsertToolCall(msgID, sessionID, "toolu_td02", "Read", json.RawMessage(`{}`), "contents", "success", "", true, 0)
	require.NoError(t, err)

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=toolu_td02&message_id="+fmt.Sprintf("%d", msgID), nil)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(otherDir),
	})

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeToolCallDetail_PostRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/tool-call", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeToolCallDetail_SessionIDFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Simulate multiple assistant messages in a session
	msgID1, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"tool_use","name":"Read","id":"toolu_split01","status":"success","done":true}]}`, nil, false, "")
	require.NoError(t, err)

	msgID2, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"tool_use","name":"Write","id":"toolu_split02","status":"success","done":true}]}`, nil, false, "")
	require.NoError(t, err)

	// Tool call stored under msgID1 (first assistant message)
	err = service.UpsertToolCall(msgID1, sessionID, "toolu_split01", "Read", json.RawMessage(`{"file_path":"/tmp/a.go"}`), "contents-a", "success", "", true, 0)
	require.NoError(t, err)

	// Tool call stored under msgID2 (second assistant message)
	err = service.UpsertToolCall(msgID2, sessionID, "toolu_split02", "Write", json.RawMessage(`{"file_path":"/tmp/b.go"}`), "contents-b", "success", "", true, 0)
	require.NoError(t, err)

	// Case 1: Wrong message_id but correct session_id → should find via fallback
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/ai/chat/tool-call?tool_id=toolu_split01&message_id=%d&session_id=%s", msgID2, sessionID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeToolCallDetail, req)
	assertOK(t, w)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "Read", result["name"])

	// Case 2: No session_id, wrong message_id → 404
	req2 := newRequest(t, http.MethodGet, fmt.Sprintf("/api/ai/chat/tool-call?tool_id=toolu_split01&message_id=%d", msgID2), nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeToolCallDetail, req2)
	assert.Equal(t, http.StatusNotFound, w2.Code)

	// Case 3: Correct message_id, no session_id → should still work (primary lookup)
	req3 := newRequest(t, http.MethodGet, fmt.Sprintf("/api/ai/chat/tool-call?tool_id=toolu_split01&message_id=%d", msgID1), nil)
	req3 = withProjectCookie(req3, env.ProjectDir)
	w3 := callHandler(ServeToolCallDetail, req3)
	assertOK(t, w3)
	var result3 map[string]any
	require.NoError(t, json.Unmarshal(w3.Body.Bytes(), &result3))
	assert.Equal(t, "Read", result3["name"])
}

// --- ServeSessions GET with pagination ---

func TestServeSessions_Get_WithLimitParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for i := range 5 {
		_, err := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Session %d", i), "claude", "", "default", "chat")
		require.NoError(t, err)
	}

	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=2", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions := result["sessions"].([]any)
	assert.LessOrEqual(t, len(sessions), 2)
	assert.Equal(t, true, result["hasMore"])
}

func TestServeSessions_Get_AllSessionsNoLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.CreateSession(env.ProjectDir, "claude", "Session A", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, false, result["hasMore"])
}

// --- ServeSessions POST ---

func TestServeSessions_Post_WithTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"title":   "My Session",
		"backend": "claude",
		"agentId": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotEmpty(t, result["sessionId"])
}

func TestServeSessions_Post_AutoTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"backend": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotEmpty(t, result["title"])
}

func TestServeSessions_Post_AutoTitle_BasedOnMaxUnnamed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	newSessionTitle := func() string {
		req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
			"backend": "claude",
		})
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeSessions, req)
		assertOK(t, w)
		var result map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result["title"].(string)
	}

	// Two unnamed sessions → numbered 1 and 2.
	first := newSessionTitle()
	assert.Equal(t, "New Session 1", first)
	second := newSessionTitle()
	assert.Equal(t, "New Session 2", second)

	// Archive the highest-numbered unnamed session ("New Session 2"). With
	// max-based numbering the next title is again "New Session 2", never
	// exceeding the largest number still present among active unnamed sessions.
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	archivedID := ""
	for _, s := range sessions {
		if s.Title == "New Session 2" {
			archivedID = s.ID
		}
	}
	require.NotEmpty(t, archivedID)
	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+archivedID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	// "New Session 1" still exists → max is 1, so the new one is 2 (no duplicate
	// among active unnamed sessions).
	third := newSessionTitle()
	assert.Equal(t, "New Session 2", third)
}

func TestServeSessions_Post_LimitExceeded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origMax := model.SessionMaxCount
	defer func() { model.SessionMaxCount = origMax }()
	model.SessionMaxCount = 1

	_, err := service.CreateSession(env.ProjectDir, "claude", "First", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"backend": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeSessions_Post_NoAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	defer func() { model.Agents = origAgents }()
	model.Agents = map[string]*model.Agent{}

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"backend": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- ArchiveSession ---

func TestArchiveSession_OK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "To Delete", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)
}

func TestArchiveSession_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestArchiveSession_BadMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/archive", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeForkSession ---

// TestBuildForkContextHandler_PreservesSummarizedAssistant is a regression test
// for fork "amnesia": buildForkContext must keep assistant replies even when the
// messages carry a reading summary (GetMessagesBySessionID would strip them).
func TestBuildForkContextHandler_PreservesSummarizedAssistant(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "first user question", nil, false, "")
	require.NoError(t, err)
	assistantMsg, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"text","text":"ai reply"}]}`, nil, false, "")
	require.NoError(t, err)

	// Give the assistant message a reading summary — this used to trigger
	// content stripping when building the fork context.
	_, err = store.WriteExec(
		"INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', ?, 'reading summary')",
		assistantMsg,
	)
	require.NoError(t, err)

	ctx := buildForkContext(sessionID)
	assert.Contains(t, ctx, "first user question")
	assert.Contains(t, ctx, "ai reply", "summarized assistant reply must survive fork context")
	assert.NotContains(t, ctx, "reading summary")
}

func TestServeForkSession_OK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Hello!", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork?session_id="+sessionID, map[string]any{
		"sessionId": sessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotEmpty(t, result["sessionId"])
}

func TestServeForkSession_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForkSession_BadMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/fork", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- getSessionID helper ---

func TestGetSessionID_FromQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test?session_id=abc", http.NoBody)
	assert.Equal(t, "abc", getSessionID(req))
}

func TestGetSessionID_FromCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: "from-cookie"})
	assert.Equal(t, "from-cookie", getSessionID(req))
}

func TestGetSessionID_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	assert.Equal(t, "", getSessionID(req))
}

// --- setSessionID helper ---

func TestSetSessionID_SetsCookie(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	setSessionID(w, req, "new-session-id")

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == model.ScopedCookieName("chat_session_id") {
			found = true
			assert.Equal(t, "new-session-id", c.Value)
			assert.True(t, c.HttpOnly)
		}
	}
	assert.True(t, found, "session cookie should be set")
}

// --- ServeAISessionUpdate PATCH ---

func TestServeAISessionUpdate_NoSID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"modelId": "test-model",
	})

	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeAISessionUpdate_BadMethod(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/update?session_id=abc", nil)
	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// The auto-cruise switch rides PATCH /api/ai/session/update as an optional
// pointer field: a cruise-only PATCH persists it to the session's context_state,
// and a PATCH without it leaves it untouched (not sent ≠ false).
func TestServeAISessionUpdate_CruiseToggle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Cruise", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Cruise-only PATCH → persisted.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sid,
		"cruise":    true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, service.GetSessionCruise(sid), "cruise must be persisted")

	// A PATCH without the field must not clear it.
	req = newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sid,
		"title":     "Renamed",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, service.GetSessionCruise(sid), "omitting cruise must not clear it")

	// An explicit false clears it.
	req = newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sid,
		"cruise":    false,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, service.GetSessionCruise(sid), "explicit false must clear cruise")
}

func TestServeAISessionUpdate_BodySessionIDOverridesCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create two sessions. The pin target is passed in the body (the session the
	// user long-pressed), while the cookie points at a different session. The
	// body id must win so pinning a non-active session hits the right one.
	_, err := service.CreateSession(env.ProjectDir, "claude", "Active", "claude", "", "default", "chat")
	require.NoError(t, err)
	targetID, err := service.CreateSession(env.ProjectDir, "claude", "Target", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": targetID,
		"pinned":    true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	req = withSessionCookie(req, targetID+"-different") // cookie points elsewhere

	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// The body-target session must now be pinned.
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	for _, s := range sessions {
		if s.ID == targetID {
			assert.True(t, s.Pinned, "body-target session should be pinned")
		}
	}
}

func TestServeAISessionUpdate_CookieSessionIDFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No body sessionId — falls back to the chat_session_id cookie.
	_, err := service.CreateSession(env.ProjectDir, "claude", "Active", "claude", "", "default", "chat")
	require.NoError(t, err)
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Cookie", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"title": "Renamed via Cookie",
	})
	req = withProjectCookie(req, env.ProjectDir)
	req = withSessionCookie(req, sid)

	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusOK, w.Code)

	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	for _, s := range sessions {
		if s.ID == sid {
			assert.Equal(t, "Renamed via Cookie", s.Title)
		}
	}
}

// --- AIChat: modelListState carries the resolved list ---
//
// GET /api/ai/chat must ship the same model-state shape as GET /api/agents: the
// raw ACP list (models) plus the CLI list (cliModels) and the resolved list
// (resolvedModels). The frontend renders resolvedModels directly instead of
// merging the two halves itself.
//
// Regression guard: this endpoint previously returned the raw ACP list only, so a
// client that consumed just modelListState.models lost every CLI model — and a
// brand-new session (which never talks to ACP) had no way to show ACP-only models
// at all.
func TestAIChat_NewSession_ModelListStateCarriesResolvedList(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	const agentID = "acp-model-list-agent"
	agent := &model.Agent{
		ID:         agentID,
		Name:       "ACP Model List Agent",
		Backend:    "acp-model-list",
		Transport:  "acp-stdio",
		AcpCommand: "acp-model-list --acp",
		Models: []model.AgentModel{
			{ID: "cli-only", Name: "CLI Only", Default: true},
			{ID: "auto", Name: "Auto"},
		},
	}
	model.Agents[agentID] = agent
	model.AgentList = append(model.AgentList, agent)

	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModels(agentID, []model.AgentModel{
		{ID: "auto", Name: "Auto"},
		{ID: "deepseek-v4-flash", Name: "Deepseek v4 Flash"},
	})
	t.Cleanup(func() { reg.UpdateModels(agentID, nil) })

	// Brand-new session: transport is left empty, matching createSession.
	sessionID, err := service.CreateSession(env.ProjectDir, agent.Backend, "New", agentID, "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		ModelListState *ai.ModelListState `json:"modelListState"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.ModelListState, "a new session must carry modelListState so the picker is populated")

	// The raw ACP list stays available.
	require.Len(t, resp.ModelListState.Models, 2)

	// The resolved list follows ACP membership ("auto" is ACP-reported, so it
	// survives; "cli-only" is not, so it is dropped) and includes ACP-only entries.
	require.NotEmpty(t, resp.ModelListState.ResolvedModels, "the resolved list must be present")
	ids := make([]string, 0, len(resp.ModelListState.ResolvedModels))
	for _, m := range resp.ModelListState.ResolvedModels {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, "auto")
	assert.Contains(t, ids, "deepseek-v4-flash")

	// The CLI list is shipped untouched for the CLI transport view.
	require.Len(t, resp.ModelListState.CLIModels, 2)
	assert.Equal(t, "cli-only", resp.ModelListState.CLIModels[0].ID)
}

// A CLI session must NOT be served ACP model state at all — the branch is gated
// on transport != "cli". Negative half of the contract above.
func TestAIChat_CLISession_OmitsModelListState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	const agentID = "acp-model-list-cli-agent"
	agent := &model.Agent{
		ID:         agentID,
		Name:       "CLI Session Agent",
		Backend:    "acp-model-list-cli",
		Transport:  "acp-stdio",
		AcpCommand: "acp-model-list-cli --acp",
		Models:     []model.AgentModel{{ID: "cli-only", Name: "CLI Only", Default: true}},
	}
	model.Agents[agentID] = agent
	model.AgentList = append(model.AgentList, agent)

	ai.GetAgentCapabilityRegistry().UpdateModels(agentID, []model.AgentModel{{ID: "auto", Name: "Auto"}})

	sessionID, err := service.CreateSession(env.ProjectDir, agent.Backend, "CLI", agentID, "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.UpdateSessionTransport(sessionID, "cli"))

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp["modelListState"], "CLI sessions must not receive ACP model state")
}

// With no CLI list to resolve against, the raw ACP state must still be delivered
// rather than dropped — losing the list entirely would be worse than shipping it
// unmerged.
func TestAIChat_NoCLIList_StillReturnsRawACPState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	const agentID = "acp-no-cli-list-agent"
	agent := &model.Agent{
		ID:         agentID,
		Name:       "No CLI List Agent",
		Backend:    "acp-no-cli",
		Transport:  "acp-stdio",
		AcpCommand: "acp-no-cli --acp",
		Models:     nil, // discovery produced nothing
	}
	model.Agents[agentID] = agent
	model.AgentList = append(model.AgentList, agent)

	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModels(agentID, []model.AgentModel{{ID: "auto", Name: "Auto"}})
	t.Cleanup(func() { reg.UpdateModels(agentID, nil) })

	sessionID, err := service.CreateSession(env.ProjectDir, agent.Backend, "NoCLI", agentID, "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		ModelListState *ai.ModelListState `json:"modelListState"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.ModelListState, "the ACP list must not be dropped just because enrichment was impossible")
	require.Len(t, resp.ModelListState.Models, 1)
	assert.Equal(t, "auto", resp.ModelListState.Models[0].ID)
	assert.Empty(t, resp.ModelListState.ResolvedModels)
}

// --- ServeAISessionUpdate: click-time model switch ---

// A live ACP connection must receive the model switch at CLICK time (not
// deferred to the next message), and the choice must be persisted.
func TestServeAISessionUpdate_AppliesModelToLiveConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "hermes", "Hermes", "hermes", "", "default", "chat")
	require.NoError(t, err)

	agent := &model.Agent{ID: "hermes", Backend: "hermes", Transport: "acp-stdio", AcpCommand: "hermes acp"}
	conn := ai.GetACPConnManager().InjectAliveConnForTest(sid, agent, "acp-sid-1", nil)
	ai.RegisterModelSwitchMethodForTest(t, "hermes", "session/set_model")
	rec := ai.CaptureRawCallsForTest(conn)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sid) })

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sid, map[string]any{
		"modelId": "minimax:MiniMax-M3",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	calls := rec.Calls()
	require.Len(t, calls, 1, "the switch must be applied at click time")
	assert.Equal(t, "session/set_model", calls[0].Method)
	assert.Equal(t, "minimax:MiniMax-M3", calls[0].Params["modelId"])
	assert.Equal(t, "minimax:MiniMax-M3", service.GetSessionModel(sid), "the choice must be persisted")
}

// A busy agent is TRANSIENT: keep the choice (persist it) and report it as
// deferred rather than failing — the next prompt re-applies it.
func TestServeAISessionUpdate_BusyModelSwitchIsDeferred(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "hermes", "Hermes", "hermes", "", "default", "chat")
	require.NoError(t, err)

	agent := &model.Agent{ID: "hermes", Backend: "hermes", Transport: "acp-stdio", AcpCommand: "hermes acp"}
	conn := ai.GetACPConnManager().InjectAliveConnForTest(sid, agent, "acp-sid-2", nil)
	ai.RegisterModelSwitchMethodForTest(t, "hermes", "session/set_model")
	ai.FailRawCallsForTest(conn, -32603, "Session is busy; switch models while the session is idle")
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sid) })

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sid, map[string]any{
		"modelId": "minimax:MiniMax-M3",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)

	require.Equal(t, http.StatusOK, w.Code, "a busy session must not fail the request: %s", w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["modelSwitchDeferred"], "the response must say the switch was deferred")
	assert.Equal(t, "minimax:MiniMax-M3", service.GetSessionModel(sid), "the choice must still be kept")
}

// A provider that is not connected fails identically on retry: report it now
// with the agent's own explanation, and do NOT persist the rejected choice.
func TestServeAISessionUpdate_HardModelSwitchFailureIsReported(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "hermes", "Hermes", "hermes", "", "default", "chat")
	require.NoError(t, err)
	before := service.GetSessionModel(sid)

	agent := &model.Agent{ID: "hermes", Backend: "hermes", Transport: "acp-stdio", AcpCommand: "hermes acp"}
	conn := ai.GetACPConnManager().InjectAliveConnForTest(sid, agent, "acp-sid-3", nil)
	ai.RegisterModelSwitchMethodForTest(t, "hermes", "session/set_model")
	ai.FailRawCallsWithDetailForTest(conn, -32602, "Invalid params", `{"details":"GitHub Copilot is not connected"}`)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sid) })

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sid, map[string]any{
		"modelId": "copilot:gpt-5.4",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ModelSwitchFailedDetail", resp["msgKey"])
	// The agent's own explanation must reach the response so the toast can name
	// the real cause (which provider to connect).
	detail, ok := resp["detail"].(map[string]any)
	require.True(t, ok, "detail payload missing: %v", resp["detail"])
	assert.Equal(t, "GitHub Copilot is not connected", detail["Detail"])
	assert.Equal(t, before, service.GetSessionModel(sid), "a rejected switch must not be persisted")
}

// No live connection (CLI transport, or an ACP session that has not started
// yet): persist only. The next prompt applies it — unchanged behavior.
func TestServeAISessionUpdate_NoLiveConnPersistsOnly(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "CLI", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sid, map[string]any{
		"modelId": "claude-opus-4-5",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "claude-opus-4-5", service.GetSessionModel(sid))
}

// A hard model rejection must not discard the OTHER fields in the same request:
// model is applied last, so everything else is already committed when the 422
// comes back. Regression guard for the early-return that used to drop
// transport/autoApprove/title/pinned/tags.
func TestServeAISessionUpdate_HardModelFailureKeepsOtherFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "hermes", "Hermes", "hermes", "", "default", "chat")
	require.NoError(t, err)

	agent := &model.Agent{ID: "hermes", Backend: "hermes", Transport: "acp-stdio", AcpCommand: "hermes acp"}
	conn := ai.GetACPConnManager().InjectAliveConnForTest(sid, agent, "acp-sid-order", nil)
	ai.RegisterModelSwitchMethodForTest(t, "hermes", "session/set_model")
	ai.FailRawCallsWithDetailForTest(conn, -32602, "Invalid params", `{"details":"not connected"}`)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sid) })

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sid, map[string]any{
		"modelId": "copilot:gpt-5.4", // will be rejected
		"title":   "Renamed anyway",
		"pinned":  true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

	// The title/pinned writes happened BEFORE the model apply, so they must be live.
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	for _, s := range sessions {
		if s.ID == sid {
			assert.Equal(t, "Renamed anyway", s.Title, "title must not be dropped by the model failure")
			assert.True(t, s.Pinned, "pinned must not be dropped by the model failure")
		}
	}
}

// writeModelSwitchError is the single mapper from a rejected switch to the 422
// the frontend toasts. Its three branches must each produce a distinct, valid
// response — a defensive non-ModelSwitchError, an error without agent detail,
// and one carrying the agent's own explanation.
func TestWriteModelSwitchError(t *testing.T) {
	t.Run("non-model-switch error falls back to the bare key", func(t *testing.T) {
		req := newRequest(t, http.MethodPatch, "/api/ai/session/update", nil)
		w := httptest.NewRecorder()
		writeModelSwitchError(w, req, errors.New("unrelated failure"))

		require.Equal(t, http.StatusUnprocessableEntity, w.Code)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "ModelSwitchFailed", resp["msgKey"])
	})

	t.Run("empty detail uses the bare key, not a dangling colon", func(t *testing.T) {
		req := newRequest(t, http.MethodPatch, "/api/ai/session/update", nil)
		w := httptest.NewRecorder()
		writeModelSwitchError(w, req, &ai.ModelSwitchError{Code: -32603, Detail: ""})

		require.Equal(t, http.StatusUnprocessableEntity, w.Code)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "ModelSwitchFailed", resp["msgKey"])
	})

	t.Run("agent detail is surfaced in both msgKey and detail", func(t *testing.T) {
		req := newRequest(t, http.MethodPatch, "/api/ai/session/update", nil)
		w := httptest.NewRecorder()
		writeModelSwitchError(w, req, &ai.ModelSwitchError{Code: -32602, Detail: "provider not connected"})

		require.Equal(t, http.StatusUnprocessableEntity, w.Code)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "ModelSwitchFailedDetail", resp["msgKey"])
		detail, ok := resp["detail"].(map[string]any)
		require.True(t, ok, "detail payload missing: %v", resp["detail"])
		assert.Equal(t, "provider not connected", detail["Detail"])
	})
}
