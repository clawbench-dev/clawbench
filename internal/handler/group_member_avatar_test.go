package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeGroupMemberAvatars_StoresAndLists pins the write path for a member's
// own avatar: a valid SVG is persisted (agent_avatar) and echoed back by the
// member list with the member's REAL agent name alongside it.
func TestServeGroupMemberAvatars_StoresAndLists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	const validSVG = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`

	req := newRequest(t, http.MethodPost, "/api/group/member-avatars", map[string]any{
		"groupId": groupID,
		"avatars": []map[string]any{{"memberId": hostMemberID, "avatar": validSVG}},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberAvatars, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var saved struct {
		OK    bool `json:"ok"`
		Saved int  `json:"saved"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	assert.True(t, saved.OK)
	assert.Equal(t, 1, saved.Saved)

	// The member list must carry both the stored avatar and the underlying
	// agent's real name (so the UI can disclose it when the nickname differs).
	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var listed struct {
		Members []struct {
			ID        string `json:"id"`
			Avatar    string `json:"avatar"`
			AgentName string `json:"agentName"`
		} `json:"members"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Members, 1)
	assert.Equal(t, hostMemberID, listed.Members[0].ID)
	assert.Equal(t, validSVG, listed.Members[0].Avatar)
	assert.Equal(t, "Test", listed.Members[0].AgentName, "agentName must be the real agent's display name")
}

// TestServeGroupMemberAvatars_RejectsUnsafeSVG pins the stored-XSS gate: an SVG
// carrying a <script> vector must be rejected with 400 and NOT persisted, since
// the stored string is replayed into the public share page.
func TestServeGroupMemberAvatars_RejectsUnsafeSVG(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	const unsafeSVG = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`

	req := newRequest(t, http.MethodPost, "/api/group/member-avatars", map[string]any{
		"groupId": groupID,
		"avatars": []map[string]any{{"memberId": hostMemberID, "avatar": unsafeSVG}},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberAvatars, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// Nothing was written.
	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Empty(t, members[0].Avatar, "an unsafe avatar must never be persisted")
}

// TestServeGroupMemberAvatars_EmptyAvatarRejected pins that an empty avatar is
// refused too (avatarSVGLooksSafe returns false for empty input), so a caller
// cannot use the endpoint to clear a stored avatar with a bogus payload.
func TestServeGroupMemberAvatars_EmptyAvatarRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/group/member-avatars", map[string]any{
		"groupId": groupID,
		"avatars": []map[string]any{{"memberId": hostMemberID, "avatar": ""}},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberAvatars, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// TestServeGroupMemberAvatars_ScopedByGroup pins the service WHERE guard: an
// avatar addressed to a member row id that belongs to a DIFFERENT group must
// not be written (a caller cannot write across groups by learning the row id).
func TestServeGroupMemberAvatars_ScopedByGroup(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupA, memberA, err := service.CreateGroup(env.ProjectDir, "A", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	groupB, _, err := service.CreateGroup(env.ProjectDir, "B", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	const validSVG = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`

	// Address memberA under groupB: the UPDATE is scoped by group_id, so it
	// matches no row and leaves memberA untouched.
	affected, err := service.SetMemberAvatar(groupB, memberA, validSVG)
	require.NoError(t, err)
	assert.Zero(t, affected, "a cross-group member id must update no row")

	members, err := service.ListGroupMembers(groupA)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Empty(t, members[0].Avatar, "cross-group avatar write must be a no-op")
}

// TestServeGroupMemberAvatars_BatchWritesAll pins the batch contract: a valid
// 2-member payload writes both members in one round trip and reports saved == 2.
func TestServeGroupMemberAvatars_BatchWritesAll(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostID, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	const hostSVG = `<svg xmlns="http://www.w3.org/2000/svg"><g id="host"/></svg>`
	const memberSVG = `<svg xmlns="http://www.w3.org/2000/svg"><g id="member"/></svg>`

	req := newRequest(t, http.MethodPost, "/api/group/member-avatars", map[string]any{
		"groupId": groupID,
		"avatars": []map[string]any{
			{"memberId": hostID, "avatar": hostSVG},
			{"memberId": memberID, "avatar": memberSVG},
		},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberAvatars, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var saved struct {
		Saved int `json:"saved"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	assert.Equal(t, 2, saved.Saved)

	byID := map[string]string{}
	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	for _, m := range members {
		byID[m.ID] = m.Avatar
	}
	assert.Equal(t, hostSVG, byID[hostID])
	assert.Equal(t, memberSVG, byID[memberID])
}

// TestServeGroupMemberAvatars_BatchValidatesAllBeforeWrite pins validate-all-
// then-write: when the SECOND item is unsafe the whole request is rejected AND
// the first (valid) member is left untouched — no half-applied batch.
func TestServeGroupMemberAvatars_BatchValidatesAllBeforeWrite(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostID, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	const validSVG = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
	const unsafeSVG = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`

	req := newRequest(t, http.MethodPost, "/api/group/member-avatars", map[string]any{
		"groupId": groupID,
		"avatars": []map[string]any{
			{"memberId": hostID, "avatar": validSVG},
			{"memberId": memberID, "avatar": unsafeSVG},
		},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMemberAvatars, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	for _, m := range members {
		assert.Empty(t, m.Avatar, "a rejected batch must not write any member (member %s)", m.ID)
	}
}
