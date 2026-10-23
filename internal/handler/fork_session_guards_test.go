package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Fork: group target validation ───────────────────────────────────────────

// A named host with no member list means a host-only group (mirrors
// POST /api/group/create). Rejecting it would make "fork into a solo group"
// impossible from the dialog's simplest path.
func TestServeForkSession_GroupTargetHostOnly(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srcID, err := service.CreateSession(env.ProjectDir, "codebuddy", "源会话", "codebuddy", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessageWithAgent(env.ProjectDir, "codebuddy", srcID, "user", "背景内容", nil, false, "", "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{
		"sessionId":   srcID,
		"targetKind":  "group",
		"hostAgentId": "codebuddy",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeForkSession, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "group", service.GetSessionType(decodeForkSessionID(t, w)))
}

// A host that is not among the named members is ambiguous — the group would
// either gain an unrequested member or run without its host. Refuse instead.
func TestServeForkSession_GroupTargetHostNotInMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srcID, err := service.CreateSession(env.ProjectDir, "codebuddy", "源会话", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{
		"sessionId":      srcID,
		"targetKind":     "group",
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A group target with no host and no members describes nothing.
func TestServeForkSession_GroupTargetWithoutMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srcID, err := service.CreateSession(env.ProjectDir, "codebuddy", "源会话", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{
		"sessionId":  srcID,
		"targetKind": "group",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// An unknown member id must fail the whole fork — seeding a group with a
// missing member would leave a member row that can never connect.
func TestServeForkSession_GroupTargetUnknownMember(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srcID, err := service.CreateSession(env.ProjectDir, "codebuddy", "源会话", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{
		"sessionId":      srcID,
		"targetKind":     "group",
		"memberAgentIds": []string{"does-not-exist"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// An untitled source still produces a usable notice: the fallback title keeps
// the timeline row readable instead of leaving an empty subject.
func TestServeForkSession_UntitledSourceUsesFallbackTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostID, err := service.CreateGroup(env.ProjectDir, "", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	_, err = service.AddChatMessageWithAgent(env.ProjectDir, "codebuddy", groupID, "assistant", "群里的发言", nil, false, "", hostID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{"sessionId": groupID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeForkSession, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	msgs, err := service.GetMessagesBySessionIDRaw(decodeForkSessionID(t, w))
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.NotEmpty(t, msgs[0].Content, "the notice must still name a subject")
}

// ── Fork: error mapping ─────────────────────────────────────────────────────

// writeForkError is the single place a fork failure becomes a status code. A
// wrong mapping turns a client mistake into a 500 (or hides a real fault behind
// a 4xx), so every branch is pinned here.
func TestWriteForkError_MapsErrorsToStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"session limit", errString("session limit reached"), http.StatusConflict},
		{"group member limit", service.ErrGroupMemberLimit, http.StatusConflict},
		{"free group needs two members", service.ErrFreeGroupNeedsTwoMembers, http.StatusBadRequest},
		{"member row source", errString("cannot fork session x: it is a group member"), http.StatusBadRequest},
		{"unknown fork point", errString("message 42 not found in session x"), http.StatusBadRequest},
		{"streaming fork point", errString("cannot fork at streaming message"), http.StatusBadRequest},
		{"missing session", errString("source session x not found"), http.StatusNotFound},
		{"anything else", errString("disk on fire"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ai/session/fork", http.NoBody)
			w := httptest.NewRecorder()
			writeForkError(w, req, tc.err)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func decodeForkSessionID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.SessionID
}
