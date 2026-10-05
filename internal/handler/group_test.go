package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeGroupCreateAndMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a group with an explicit host agent.
	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"title": "讨论组", "hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		OK           bool   `json:"ok"`
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.OK)
	require.NotEmpty(t, created.GroupID)
	require.NotEmpty(t, created.HostMemberID)

	// The group session has session_type='group'.
	require.Equal(t, "group", service.GetSessionType(created.GroupID))

	// Add a member (batch).
	req = newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": created.GroupID, "agentIds": []string{"claude"}})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// List members: host + added, with isHost on the host.
	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+created.GroupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var listed struct {
		Members []struct {
			ID     string `json:"id"`
			IsHost bool   `json:"isHost"`
			Left   bool   `json:"left"`
		} `json:"members"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Members, 2)
	hostSeen := false
	for _, m := range listed.Members {
		if m.ID == created.HostMemberID {
			hostSeen = true
			assert.True(t, m.IsHost, "host member must carry isHost=true")
		}
	}
	assert.True(t, hostSeen, "host member must be listed")
}

func TestServeGroupSettings(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"groupId": groupID, "maxRounds": 7})
	_ = body
	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": groupID, "maxRounds": 7})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, 7, service.GetGroupMaxRounds(groupID))
}

// TestAIChatDelegatesGroupSend verifies a POST to /api/ai/chat for a group
// session returns the group-delegation response (the orchestrator runs in the
// background; the handler itself must not run a single-agent turn).
func TestAIChatDelegatesGroupSend(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"message": "hi"})
	_ = body
	req := newRequest(t, http.MethodPost, "/api/ai/chat", map[string]any{"message": "hi"})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: groupID})
	w := callHandlerWithAuth(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["group"], "group send must be delegated to the orchestrator")
}
