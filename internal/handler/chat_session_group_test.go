package handler

import (
	"net/http"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A group's members each own a separate ACP connection keyed by the MEMBER row
// id, so closing the group row's connection (the pre-existing behavior) leaves
// every member process alive. Archiving must close them all (decision #57).
func TestArchiveSession_ClosesGroupMemberConnections(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "讨论组", "claude", "claude", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// Give the group a finalized message so archiving takes the soft-archive
	// path (the empty-session path hard-deletes and has its own close).
	_, err = service.AddChatMessage(env.ProjectDir, "claude", groupID, "user", "hello", nil, false, "")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	for _, sid := range []string{groupID, hostMemberID, memberID} {
		conn := &ai.ACPConn{}
		conn.SetClientForTest(ai.NewClawBenchACPClient())
		conn.SetSessionMappingForTest(sid, "acp-"+sid)
		mgr.SetConnForTest(sid, conn)
	}
	t.Cleanup(func() {
		mgr.CloseConn(groupID)
		mgr.CloseConn(hostMemberID)
		mgr.CloseConn(memberID)
	})

	req := newRequest(t, http.MethodDelete, "/api/ai/session?session_id="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ArchiveSession, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// CloseConn runs in a goroutine (it may block on cmd.Wait()), so poll.
	require.Eventually(t, func() bool {
		return mgr.GetConn(hostMemberID) == nil && mgr.GetConn(memberID) == nil
	}, 2*time.Second, 10*time.Millisecond, "member connections must be closed when the group is archived")
}

// Destroying a group must likewise close every member connection.
func TestDestroySession_ClosesGroupMemberConnections(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "讨论组", "claude", "claude", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	for _, sid := range []string{groupID, hostMemberID, memberID} {
		conn := &ai.ACPConn{}
		conn.SetClientForTest(ai.NewClawBenchACPClient())
		conn.SetSessionMappingForTest(sid, "acp-"+sid)
		mgr.SetConnForTest(sid, conn)
	}
	t.Cleanup(func() {
		mgr.CloseConn(groupID)
		mgr.CloseConn(hostMemberID)
		mgr.CloseConn(memberID)
	})

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(DestroySession, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Eventually(t, func() bool {
		return mgr.GetConn(hostMemberID) == nil && mgr.GetConn(memberID) == nil
	}, 2*time.Second, 10*time.Millisecond, "member connections must be closed when the group is destroyed")
}

// Toggling auto-approve on a group must apply to every MEMBER row, not just the
// group row: members each own an ACP connection keyed by their member row id,
// and the permission handler reads the member row's flag (decision #61). The
// group row has no connection, so the old code was a dead switch.
func TestServeAISessionUpdate_GroupAutoApproveAppliesToMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "讨论组", "claude", "claude", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// Both members have live ACP connections (registered under the MEMBER id).
	mgr := ai.GetACPConnManager()
	for _, sid := range []string{hostMemberID, memberID} {
		conn := &ai.ACPConn{}
		conn.SetClientForTest(ai.NewClawBenchACPClient())
		conn.SetSessionMappingForTest(sid, "acp-"+sid)
		mgr.SetConnForTest(sid, conn)
	}
	t.Cleanup(func() {
		mgr.CloseConn(hostMemberID)
		mgr.CloseConn(memberID)
	})

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":   groupID,
		"autoApprove": true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Every member row must carry auto_approve=1.
	for _, id := range []string{hostMemberID, memberID} {
		assert.True(t, service.GetSessionAutoApprove(id),
			"member row %s must have auto_approve enabled", id)
	}
	// And every live member connection must have synced its runtime flag.
	assert.True(t, mgr.GetConn(hostMemberID).IsAutoApprove(), "host connection must sync auto-approve")
	assert.True(t, mgr.GetConn(memberID).IsAutoApprove(), "member connection must sync auto-approve")

	// Toggling back off must clear every member row too.
	req = newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":   groupID,
		"autoApprove": false,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, id := range []string{hostMemberID, memberID} {
		assert.False(t, service.GetSessionAutoApprove(id),
			"member row %s must have auto_approve disabled again", id)
	}
}

// An ordinary chat still uses the single-row path (no member fan-out).
func TestServeAISessionUpdate_ChatAutoApproveStillWorks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "T", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":   sid,
		"autoApprove": true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, service.GetSessionAutoApprove(sid))
}

// The member-closing helper must be a no-op for an ordinary chat: it must not
// look up members (there are none) or close anything.
func TestCloseGroupMemberConns_NonGroupNoop(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "T", "claude", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(ai.NewClawBenchACPClient())
	conn.SetSessionMappingForTest(sid, "acp-"+sid)
	mgr.SetConnForTest(sid, conn)
	t.Cleanup(func() { mgr.CloseConn(sid) })

	closeGroupMemberConns(sid)

	assert.NotNil(t, mgr.GetConn(sid), "a non-group session must not be touched by the member closer")
	assert.Equal(t, "chat", service.GetSessionType(sid))
	_ = model.Agents
}
