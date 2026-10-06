package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

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

// TestServeGroupCreateWithMembers pins the one-step creation endpoint (design
// §7.1, decision #25): a single POST with hostAgentId + memberAgentIds creates
// the group and ALL members at once, so the frontend no longer follows up with
// a separate add-members call.
func TestServeGroupCreateWithMembers(t *testing.T) {
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
		OK           bool   `json:"ok"`
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.OK)
	require.NotEmpty(t, created.GroupID)

	// Both members exist after the single call — no follow-up needed.
	members, err := service.ListGroupMembers(created.GroupID)
	require.NoError(t, err)
	require.Len(t, members, 2, "host + 1 member must be created in one call")
	require.Equal(t, created.HostMemberID, service.GetGroupHostMember(created.GroupID))

	hostSeen := false
	for _, m := range members {
		if m.ID == created.HostMemberID {
			hostSeen = true
			require.Equal(t, "codebuddy", m.AgentID)
		}
	}
	require.True(t, hostSeen, "host row must be among the created members")
}

// TestServeGroupCreateInvalidMemberAborts pins atomicity at the HTTP boundary:
// an unknown agent id must fail the request and leave no group behind.
func TestServeGroupCreateInvalidMemberAborts(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "no-such-agent-xyz"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var count int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE session_type = 'group'",
	).Scan(&count))
	require.Zero(t, count, "failed create must not leave a group row")
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

// TestServeSessions_GroupRowCarriesMemberPreview pins the session-list contract
// for group rows: a group session carries a compact `groupMembers` preview
// (active members only), while a plain chat session omits the key entirely.
func TestServeSessions_GroupRowCarriesMemberPreview(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// A plain chat session in the same project must NOT grow the field.
	chatID, err := service.CreateSession(env.ProjectDir, "codebuddy", "plain", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []model.ChatSession `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	byID := map[string]model.ChatSession{}
	for _, s := range result.Sessions {
		byID[s.ID] = s
	}

	group, ok := byID[groupID]
	require.True(t, ok, "group session must appear in the list")
	require.Len(t, group.GroupMembers, 2, "host + added member")
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range group.GroupMembers {
		names[m.Name] = true
		ids[m.ID] = true
	}
	assert.True(t, names["Host"] && names["Claude"], "preview names=%v", names)
	assert.True(t, ids[hostMemberID] && ids[memberID], "preview must carry member row ids")

	// The plain chat row omits the field entirely (omitempty + nil slice → the
	// key is absent, so a non-group payload is byte-for-byte unchanged). Assert
	// on the raw JSON, not the decoded slice, which cannot tell absent from [].
	// The `byID` guard is load-bearing: without it a payload that dropped the
	// plain row entirely would make the loop below pass vacuously.
	require.Contains(t, byID, chatID, "the plain chat row must be in the payload")
	require.Contains(t, w.Body.String(), `"groupMembers"`, "the group row must carry the preview key")
	var raw struct {
		Sessions []map[string]any `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	sawChat := false
	for _, s := range raw.Sessions {
		if s["id"] == chatID {
			sawChat = true
			_, present := s["groupMembers"]
			assert.False(t, present, "non-group session must not carry the groupMembers key")
		}
	}
	assert.True(t, sawChat, "plain chat row must appear in the raw payload")

	// Members themselves are hidden from the list.
	assert.NotContains(t, byID, hostMemberID)
	assert.NotContains(t, byID, memberID)
}

// TestServeSessionsOverview_GroupRowCarriesMemberPreview pins the same contract
// on the cross-project overview endpoint. A group only appears there when it is
// running/pending/unread, so the fixture marks it unread.
func TestServeSessionsOverview_GroupRowCarriesMemberPreview(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	_, err = service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// Make the group unread so the overview (running/pending/unread only)
	// includes it: an assistant message newer than last_read_at.
	_, err = store.UnsafeDBForTest().Exec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
		 VALUES (?, 'assistant', 'unread', ?, 'codebuddy', 0)`,
		store.ProjectIDForTest(t, env.ProjectDir), groupID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)

	var result struct {
		Projects []struct {
			Sessions []struct {
				ID           string                     `json:"id"`
				SessionType  string                     `json:"sessionType"`
				GroupMembers []model.GroupMemberPreview `json:"groupMembers"`
			} `json:"sessions"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	found := false
	for _, p := range result.Projects {
		for _, s := range p.Sessions {
			if s.ID != groupID {
				continue
			}
			found = true
			assert.Equal(t, "group", s.SessionType)
			require.Len(t, s.GroupMembers, 2)
		}
	}
	assert.True(t, found, "group session must appear in the overview")
}

// Removing the host must be refused with 409 so the frontend can explain why,
// rather than the generic 500 the unguarded path produced.
func TestServeGroupMembers_RefuseHostRemoval(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"title": "讨论组", "hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	req = newRequest(t, http.MethodDelete, "/api/group/members", map[string]any{"groupId": created.GroupID, "memberId": created.HostMemberID})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "CannotRemoveHost")
}
