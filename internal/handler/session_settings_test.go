package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeSessionSettings_ReturnsSessionState pins the endpoint the settings
// drawer uses for a targeted session (single chat, group, or group member row).
//
// A group member row has NO chat_history of its own — all member speech lands on
// the group timeline — so GET /api/ai/chat cannot describe it. The drawer must
// still open on a member, which is why this endpoint addresses sessions
// uniformly.
func TestServeSessionSettings_ReturnsSessionState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Settings", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		OK          bool   `json:"ok"`
		SessionID   string `json:"sessionId"`
		SessionType string `json:"sessionType"`
		AgentID     string `json:"agentId"`
		Backend     string `json:"backend"`
		Transport   string `json:"transport"`
		Cruise      bool   `json:"cruise"`
		IsMember    bool   `json:"isGroupMember"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.True(t, got.OK)
	assert.Equal(t, sid, got.SessionID)
	assert.Equal(t, "chat", got.SessionType)
	assert.Equal(t, "codebuddy", got.AgentID)
	assert.False(t, got.IsMember)
}

// TestServeSessionSettings_GroupMemberRow pins the member case: the same
// endpoint describes a hidden member row, and reports it as such.
func TestServeSessionSettings_GroupMemberRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	req = newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+created.HostMemberID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		SessionType string `json:"sessionType"`
		IsMember    bool   `json:"isGroupMember"`
		AgentID     string `json:"agentId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "group_member", got.SessionType)
	assert.True(t, got.IsMember)
	assert.Equal(t, "codebuddy", got.AgentID)
}

// TestServeSessionSettings_Guards pins the rejections.
func TestServeSessionSettings_Guards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Wrong method.
	req := newRequest(t, http.MethodPost, "/api/ai/session/settings", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// Missing session_id.
	req = newRequest(t, http.MethodGet, "/api/ai/session/settings", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Unknown session.
	req = newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id=nope", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// TestServeSessionSettingsUpdate_WritesTargetSession pins that the write goes to
// the session named in the body (a member row), not the request's current one.
func TestServeSessionSettingsUpdate_WritesTargetSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Target", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": sid,
		"cruise":    true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.True(t, service.GetSessionCruise(sid), "cruise must be persisted on the target session")
	// Cruise forces auto-approve on (one-directional, unattended runs).
	assert.True(t, service.GetSessionAutoApprove(sid), "enabling cruise must force auto-approve on")
}

// TestServeSessionSettingsUpdate_GroupMemberBusy pins the concurrency guard: a
// member's connection is busy during a group turn, so a config write must be
// refused rather than interleaved with the running prompt.
func TestServeSessionSettingsUpdate_GroupMemberBusy(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	service.SetSessionRunning(created.GroupID, true, true)
	t.Cleanup(func() { service.SetSessionRunning(created.GroupID, false, true) })

	req = newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": created.HostMemberID,
		"modeId":    "plan",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}
