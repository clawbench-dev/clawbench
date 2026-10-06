package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentMap builds the registry map the shared withAgents helper expects.
func agentMap(agents ...*model.Agent) map[string]*model.Agent {
	m := map[string]*model.Agent{}
	for _, a := range agents {
		m[a.ID] = a
	}
	return m
}

// TestResolveSessionSpeakers_SingleAgent pins the ordinary path: a non-group
// session resolves only its session-level agent (no member roster).
func TestResolveSessionSpeakers_SingleAgent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/speakertest"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())

	withAgents(t, agentMap(&model.Agent{ID: "solo", Name: "Solo Agent", Backend: "claude", Avatar: "<svg/>"}))

	sessionID, err := CreateSession(project, "claude", "T", "solo", "", "default", "chat")
	require.NoError(t, err)

	sessionAgent, speakers := ResolveSessionSpeakers(sessionID, true)
	require.NotNil(t, sessionAgent)
	assert.Equal(t, "Solo Agent", sessionAgent.Name)
	assert.Equal(t, "claude", sessionAgent.Backend)
	assert.Equal(t, "<svg/>", sessionAgent.Avatar)
	assert.Nil(t, speakers, "a non-group session must have no member roster")
}

// TestResolveSessionSpeakers_Group pins the group path: the session agent is the
// HOST, and the roster maps each member row id to its own identity — including a
// member that LEFT (its past speech must keep resolving).
func TestResolveSessionSpeakers_Group(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/speakertest"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())

	withAgents(t, agentMap(
		&model.Agent{ID: "codebuddy", Name: "Host Agent", Backend: "codebuddy"},
		&model.Agent{ID: "claude", Name: "Alice Agent", Backend: "claude", Avatar: "<alice/>"},
		&model.Agent{ID: "codex", Name: "Bob Agent", Backend: "codex"},
	))

	groupID, hostMemberID, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	aliceID, err := AddGroupMember(project, groupID, "claude", "claude", "Alice")
	require.NoError(t, err)
	bobID, err := AddGroupMember(project, groupID, "codex", "codex", "Bob")
	require.NoError(t, err)
	// Bob leaves; his speech remains in the timeline.
	require.NoError(t, RemoveGroupMember(groupID, bobID))

	sessionAgent, speakers := ResolveSessionSpeakers(groupID, true)
	require.NotNil(t, sessionAgent)
	assert.Equal(t, "Host Agent", sessionAgent.Name)

	require.Contains(t, speakers, hostMemberID)
	require.Contains(t, speakers, aliceID)
	require.Contains(t, speakers, bobID, "a left member must still resolve (past speech)")

	assert.Equal(t, "Alice Agent", speakers[aliceID].Name)
	assert.Equal(t, "claude", speakers[aliceID].Backend)
	assert.Equal(t, "<alice/>", speakers[aliceID].Avatar)
	assert.Equal(t, "Bob Agent", speakers[bobID].Name)
}

// TestResolveSessionSpeakers_NoAvatarOnSharePath pins the privacy boundary: the
// public share path must never carry user-configured avatars.
func TestResolveSessionSpeakers_NoAvatarOnSharePath(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/speakertest"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())

	withAgents(t, agentMap(
		&model.Agent{ID: "codebuddy", Name: "Host Agent", Backend: "codebuddy", Avatar: "<host/>"},
		&model.Agent{ID: "claude", Name: "Alice Agent", Backend: "claude", Avatar: "<alice/>"},
	))

	groupID, hostMemberID, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	aliceID, err := AddGroupMember(project, groupID, "claude", "claude", "Alice")
	require.NoError(t, err)

	sessionAgent, speakers := ResolveSessionSpeakers(groupID, false)
	require.NotNil(t, sessionAgent)
	assert.Empty(t, sessionAgent.Avatar, "share path must not leak the session agent's avatar")
	assert.Empty(t, speakers[hostMemberID].Avatar)
	assert.Empty(t, speakers[aliceID].Avatar, "share path must not leak member avatars")
	// Names/backends still resolve so the viewer shows the right brand icon.
	assert.Equal(t, "claude", speakers[aliceID].Backend)
}

// TestResolveSessionSpeakers_UnknownSession is the defensive path: an unknown id
// yields nothing rather than a panic.
func TestResolveSessionSpeakers_UnknownSession(t *testing.T) {
	setupGroupDB(t)
	sessionAgent, speakers := ResolveSessionSpeakers("no-such-session", true)
	assert.Nil(t, sessionAgent)
	assert.Nil(t, speakers)
}

// TestResolveSessionSpeakers_MissingAgentFallsBackToRow keeps the row's own
// backend/name when the agent is no longer in the registry (deleted agent), so
// the icon still renders instead of vanishing.
func TestResolveSessionSpeakers_MissingAgentFallsBackToRow(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/speakertest"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())

	// Registry deliberately empty: no agent resolves.
	withAgents(t, map[string]*model.Agent{})

	sessionID, err := CreateSession(project, "codex", "T", "ghost-agent", "", "default", "chat")
	require.NoError(t, err)

	sessionAgent, _ := ResolveSessionSpeakers(sessionID, true)
	require.NotNil(t, sessionAgent)
	assert.Equal(t, "codex", sessionAgent.Backend, "falls back to the session row's backend")
	assert.Empty(t, sessionAgent.Avatar)
}
