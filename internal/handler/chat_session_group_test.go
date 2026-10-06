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
