package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeGroupCreate error branches ---

func TestServeGroupCreate_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/group/create", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code, w.Body.String())
}

func TestServeGroupCreate_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"hostAgentId": "codebuddy"})
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestServeGroupCreate_MalformedBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", nil)
	req.Body = http.NoBody
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupCreate_HostNotAmongMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupCreate_NoMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"memberAgentIds": []string{},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// A host-only request with no member list falls back to a one-member group.
func TestServeGroupCreate_HostOnlyFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID string `json:"groupId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	members, err := service.ListGroupMembers(created.GroupID)
	require.NoError(t, err)
	require.Len(t, members, 1, "host-only fallback creates exactly the host row")
}

// Free mode (no host) needs at least two members; a lone member is refused.
func TestServeGroupCreate_FreeModeNeedsTwoMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"memberAgentIds": []string{"claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// Free mode with two members succeeds and reports mode=free.
func TestServeGroupCreate_FreeModeTwoMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"memberAgentIds": []string{"claude", "codebuddy"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID string `json:"groupId"`
		Mode    string `json:"mode"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "free", created.Mode)
}

// A group counts toward the session limit; when the cap is already reached the
// create must be refused with 409.
func TestServeGroupCreate_SessionLimitReached(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	orig := model.SessionMaxCount
	model.SessionMaxCount = 1
	t.Cleanup(func() { model.SessionMaxCount = orig })

	// One existing session fills the cap.
	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "single", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "SessionLimitReached")
}

// --- ServeGroupMembers dispatch / error branches ---

func TestServeGroupMembers_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/group/members", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code, w.Body.String())
}

func TestServeGroupMembers_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/group/members?groupId=x", nil)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestServeGroupMembers_ListMissingGroupID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/group/members", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupMembers_ListUnknownGroup(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/group/members?groupId=does-not-exist", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	// A missing session is a 404 (not a 403): the ownership check distinguishes
	// "deleted" from "not yours".
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestServeGroupMembers_AddMissingFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": "", "agentIds": []string{}})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupMembers_AddUnknownGroup(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": "nope", "agentIds": []string{"claude"}})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// Unknown agent ids are skipped; the resolvable ones are still added.
func TestServeGroupMembers_AddSkipsUnknownAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/group/members", map[string]any{
		"groupId":  groupID,
		"agentIds": []string{"claude", "no-such-agent-xyz"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		MemberIDs []string `json:"memberIds"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.MemberIDs, 1, "only the resolvable agent is added")
}

func TestServeGroupMembers_RemoveMissingFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/group/members", map[string]any{"groupId": "", "memberId": ""})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupMembers_RemoveUnknownGroup(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/group/members", map[string]any{"groupId": "nope", "memberId": "m1"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// Removing a non-host member succeeds and archives the row.
func TestServeGroupMembers_RemoveMemberOK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/group/members", map[string]any{"groupId": groupID, "memberId": memberID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	for _, m := range members {
		if m.ID == memberID {
			assert.True(t, m.Left, "removed member must be archived")
		}
	}
}

// --- ServeGroupSettings error branches ---

func TestServeGroupSettings_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/group/settings", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code, w.Body.String())
}

func TestServeGroupSettings_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": "g", "maxRounds": 3})
	w := callHandlerWithAuth(ServeGroupSettings, req)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestServeGroupSettings_InvalidParams(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": "", "maxRounds": 0})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupSettings_UnknownGroup(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": "nope", "maxRounds": 3})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// --- helper unit coverage ---

func TestGroupPlaceholderTitle(t *testing.T) {
	assert.Equal(t, "群聊", groupPlaceholderTitle(""))
	assert.Contains(t, groupPlaceholderTitle("codebuddy"), "的群聊")
}

// groupSessionLimitOK returns true unconditionally when the cap is unlimited.
func TestGroupSessionLimitOK_Unlimited(t *testing.T) {
	orig := model.SessionMaxCount
	model.SessionMaxCount = 0
	t.Cleanup(func() { model.SessionMaxCount = orig })

	rec := httptest.NewRecorder()
	req := newRequest(t, http.MethodPost, "/x", nil)
	assert.True(t, groupSessionLimitOK(rec, req, "/nonexistent"))
}

// writeGroupCreateError maps the free-mode error to 400.
func TestWriteGroupCreateError_FreeNeedsTwo(t *testing.T) {
	rec := httptest.NewRecorder()
	req := newRequest(t, http.MethodPost, "/x", nil)
	writeGroupCreateError(rec, req, service.ErrFreeGroupNeedsTwoMembers)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// writeGroupCreateError maps the member-limit error to 409.
func TestWriteGroupCreateError_MemberLimit(t *testing.T) {
	rec := httptest.NewRecorder()
	req := newRequest(t, http.MethodPost, "/x", nil)
	writeGroupCreateError(rec, req, service.ErrGroupMemberLimit)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// writeGroupCreateError maps an unrecognized error to 500.
func TestWriteGroupCreateError_Default(t *testing.T) {
	rec := httptest.NewRecorder()
	req := newRequest(t, http.MethodPost, "/x", nil)
	writeGroupCreateError(rec, req, errSentinel{})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

type errSentinel struct{}

func (errSentinel) Error() string { return "boom" }

// --- ServeGroupMembers add over-limit (covers the 409 branch) ---

func TestServeGroupMembers_AddOverLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Register enough agents to exceed the member cap.
	origAgents := model.Agents
	agents := map[string]*model.Agent{}
	for i := range service.MaxGroupMembers + 2 {
		id := "agent-" + string(rune('a'+i))
		agents[id] = &model.Agent{ID: id, Name: id, Backend: "claude"}
	}
	agents["codebuddy"] = &model.Agent{ID: "codebuddy", Name: "Test", Backend: "codebuddy"}
	model.Agents = agents
	t.Cleanup(func() { model.Agents = origAgents })

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	ids := make([]string, 0, service.MaxGroupMembers+2)
	for i := range service.MaxGroupMembers + 2 {
		ids = append(ids, "agent-"+string(rune('a'+i)))
	}
	req := newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": groupID, "agentIds": ids})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "GroupMemberLimitReached")
}

// decodeJSON rejects an empty body for the member add path.
func TestServeGroupMembers_AddMalformedBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/members", nil)
	req.Body = http.NoBody
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupMembers_RemoveMalformedBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/group/members", nil)
	req.Body = http.NoBody
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestServeGroupSettings_MalformedBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/group/settings", nil)
	req.Body = http.NoBody
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
