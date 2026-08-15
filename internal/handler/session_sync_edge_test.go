package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- ServeACPSyncSession error / edge branches ----

func TestServeACPSyncSession_WrongMethod(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/session/acp-sync", map[string]string{
		"agentId": "x", "sessionId": "y",
	})
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeACPSyncSession_NoProjectSelected(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": "x", "sessionId": "y",
	})
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeACPSyncSession_InvalidBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/ai/session/acp-sync", http.NoBody)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPSyncSession_EmptyFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": "", "sessionId": "",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPSyncSession_AgentNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": "ghost", "sessionId": "y",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeACPSyncSession_AgentNoACP(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "no-acp-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "NoACP", Backend: "claude", Transport: "", AcpCommand: ""},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": agentID, "sessionId": "y",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPSyncSession_BackendNotImplemented(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Agent supports ACP but its backend has no spec (ACPLoadSession unknown → nil spec).
	agentID := "specless-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Specless", Backend: "no-such-backend", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": agentID, "sessionId": "y",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestServeACPSyncSession_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": agentID, "sessionId": "does-not-exist",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeACPSyncSession_ConnGenericError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)
	service.UpdateExternalSessionID(sid, "acp-xyz")

	orig := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(context.Context, *model.Agent, string, string, string) (*ai.ACPConn, error) {
		return nil, errors.New("boom")
	}
	defer func() { getOrCreateConnForLoad = orig }()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": agentID, "sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServeACPSyncSession_ConnResourceNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)
	service.UpdateExternalSessionID(sid, "acp-xyz")

	orig := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(context.Context, *model.Agent, string, string, string) (*ai.ACPConn, error) {
		return nil, errors.New("acp: session resource not found")
	}
	defer func() { getOrCreateConnForLoad = orig }()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId": agentID, "sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWaitForReplaySettled_NilClient(t *testing.T) {
	waitForReplaySettled(nil) // should return immediately, no panic
}

func TestFilterSystemPromptText(t *testing.T) {
	// Pure <system-reminder> block → dropped.
	assert.Equal(t, "", filterSystemPromptText("<system-reminder data-role=\"command-caveat\">Caveat: ...</system-reminder>"))
	assert.Equal(t, "", filterSystemPromptText("  <system-reminder>...</system-reminder>  "))

	// Legacy "[System Instructions: ...]\n\n<real text>" → keep real text.
	assert.Equal(t, "你叫什么名字", filterSystemPromptText("[System Instructions: ## User Interaction\n...我同意后再实施。]\n\n你叫什么名字"))
	// System instructions with no real text after → dropped.
	assert.Equal(t, "", filterSystemPromptText("[System Instructions: ...]\n\n  "))

	// Normal text → unchanged.
	assert.Equal(t, "你几岁了", filterSystemPromptText("你几岁了"))
	assert.Equal(t, "", filterSystemPromptText("   "))
}

