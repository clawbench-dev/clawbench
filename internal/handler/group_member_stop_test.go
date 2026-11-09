package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeGroupMemberStop_StopsOnlyTheSpeakingMember(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostID, err := service.CreateGroup(env.ProjectDir, "停止测试", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	var cancelled atomic.Int32
	turnID := service.RegisterGroupMemberTurnCancel(groupID, hostID, func() { cancelled.Add(1) })
	t.Cleanup(func() { service.UnregisterGroupMemberTurnCancel(hostID, turnID) })

	req := newRequest(t, http.MethodPost, "/api/group/member-stop", map[string]any{
		"groupId": groupID, "memberId": hostID,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberStop, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Stopped bool   `json:"stopped"`
		Reason  string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, body.Stopped, "a speaking member must be stopped")
	assert.Equal(t, int32(1), cancelled.Load(), "the member's turn context must be cancelled")

	// The handle is consumed: a second click must not claim another stop.
	req2 := newRequest(t, http.MethodPost, "/api/group/member-stop", map[string]any{
		"groupId": groupID, "memberId": hostID,
	})
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandlerWithAuth(ServeGroupMemberStop, req2)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	var body2 struct {
		Stopped bool `json:"stopped"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body2))
	assert.False(t, body2.Stopped)
	assert.Equal(t, int32(1), cancelled.Load(), "a second stop must not cancel again")
}

// A member that is not speaking right now is not an error: the caller learns
// nothing was stopped, so a stale click cannot kill an unrelated turn.
func TestServeGroupMemberStop_IdleMemberReportsNothingStopped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostID, err := service.CreateGroup(env.ProjectDir, "停止空转", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/group/member-stop", map[string]any{
		"groupId": groupID, "memberId": hostID,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberStop, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Stopped bool   `json:"stopped"`
		Reason  string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.Stopped)
	assert.Equal(t, "idle", body.Reason)
}

func TestServeGroupMemberStop_RejectsMissingMember(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "停止参数", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/group/member-stop", map[string]any{"groupId": groupID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberStop, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// The group must belong to the requesting project: a member id from another
// project's group must not be stoppable.
func TestServeGroupMemberStop_RejectsForeignProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostID, err := service.CreateGroup(env.ProjectDir, "停止越权", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	var cancelled atomic.Int32
	turnID := service.RegisterGroupMemberTurnCancel(groupID, hostID, func() { cancelled.Add(1) })
	t.Cleanup(func() { service.UnregisterGroupMemberTurnCancel(hostID, turnID) })

	req := newRequest(t, http.MethodPost, "/api/group/member-stop", map[string]any{
		"groupId": groupID, "memberId": hostID,
	})
	req = withProjectCookie(req, t.TempDir())
	w := callHandlerWithAuth(ServeGroupMemberStop, req)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Equal(t, int32(0), cancelled.Load(), "a refused request must not cancel anything")
}

// The stop endpoint is called from a button that can fire in odd states (a
// stale tab, a request whose cookie expired). Each of those must answer with a
// clear status instead of a 500 or a silent 200.
func TestServeGroupMemberStop_RejectsBadRequests(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	t.Run("wrong method", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodGet, "/api/group/member-stop", nil), env.ProjectDir)
		w := callHandlerWithAuth(ServeGroupMemberStop, req)
		assertStatus(t, w, http.StatusMethodNotAllowed)
	})

	t.Run("missing project cookie", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/group/member-stop",
			map[string]any{"groupId": "g", "memberId": "m"})
		w := callHandlerWithAuth(ServeGroupMemberStop, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("malformed body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/group/member-stop",
			strings.NewReader("{not json"))
		req.Header.Set("Content-Type", "application/json")
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandlerWithAuth(ServeGroupMemberStop, req)
		assertStatus(t, w, http.StatusBadRequest)
	})
}
