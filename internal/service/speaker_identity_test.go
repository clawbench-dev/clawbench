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

	sessionAgent, speakers := ResolveSessionSpeakers(sessionID)
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

	sessionAgent, speakers := ResolveSessionSpeakers(groupID)
	require.NotNil(t, sessionAgent)
	// The session-level agent is the group row's identity; it uses the LIVE
	// agent name.
	assert.Equal(t, "Host Agent", sessionAgent.Name)

	require.Contains(t, speakers, hostMemberID)
	require.Contains(t, speakers, aliceID)
	require.Contains(t, speakers, bobID, "a left member must still resolve (past speech)")

	// Member names are the STORED names (agent_name), NOT the live agents.name.
	// The registry calls them "Alice Agent"/"Bob Agent", but the members joined
	// as "Alice"/"Bob"; that snapshot is authoritative.
	assert.Equal(t, "Alice", speakers[aliceID].Name)
	assert.Equal(t, "claude", speakers[aliceID].Backend)
	assert.Equal(t, "<alice/>", speakers[aliceID].Avatar)
	assert.Equal(t, "Bob", speakers[bobID].Name)
}

// TestResolveSessionSpeakers_MemberNameIsSnapshot pins the regression this
// design fixes: a member's speaker name is the name it joined with, so renaming
// the underlying agent later does NOT retroactively rename its past speech in
// share snapshots. The session-level agent (host) still tracks the live name.
func TestResolveSessionSpeakers_MemberNameIsSnapshot(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/speakertest-snapshot"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())

	withAgents(t, agentMap(
		&model.Agent{ID: "codebuddy", Name: "Host Agent", Backend: "codebuddy"},
		&model.Agent{ID: "claude", Name: "Alice Agent", Backend: "claude"},
	))

	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	aliceID, err := AddGroupMember(project, groupID, "claude", "claude", "Alice")
	require.NoError(t, err)

	// The user renames the agent AFTER the member joined.
	withAgents(t, agentMap(
		&model.Agent{ID: "codebuddy", Name: "Host Agent", Backend: "codebuddy"},
		&model.Agent{ID: "claude", Name: "Renamed Agent", Backend: "claude"},
	))

	_, speakers := ResolveSessionSpeakers(groupID)
	require.Contains(t, speakers, aliceID)
	assert.Equal(t, "Alice", speakers[aliceID].Name,
		"a member's past speech must keep its join-time name, not the renamed agent's")
}

// TestResolveSessionSpeakers_DuplicateInstancesDistinctNames pins the other
// regression: two instances of the SAME agent in one group must resolve to
// DIFFERENT names, or the share snapshot cannot tell them apart.
func TestResolveSessionSpeakers_DuplicateInstancesDistinctNames(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/speakertest-dup"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())

	withAgents(t, agentMap(
		&model.Agent{ID: "codebuddy", Name: "Host Agent", Backend: "codebuddy"},
		&model.Agent{ID: "claude", Name: "Claude", Backend: "claude"},
	))

	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	// Add two instances of the same agent via the batch path, which assigns
	// unique names.
	ids, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "claude", Backend: "claude"},
		{AgentID: "claude", Backend: "claude"},
	})
	require.NoError(t, err)
	require.Len(t, ids, 2)

	_, speakers := ResolveSessionSpeakers(groupID)
	require.Contains(t, speakers, ids[0])
	require.Contains(t, speakers, ids[1])
	// Both instances are pool names (a multi-instance agent keeps no bare name).
	assert.Equal(t, groupNamePool[0], speakers[ids[0]].Name)
	assert.Equal(t, groupNamePool[1], speakers[ids[1]].Name)
	assert.NotEqual(t, speakers[ids[0]].Name, speakers[ids[1]].Name,
		"two instances of one agent must have distinct names")
}

// TestResolveSessionSpeakers_CarriesAvatarOnSharePath pins the current share
// contract: the snapshot is frozen by the authenticated owner, so the custom
// avatars ARE carried for the anonymous viewer (which cannot resolve them any
// other way). This is the deliberate reversal of the earlier no-avatar policy.
func TestResolveSessionSpeakers_CarriesAvatarOnSharePath(t *testing.T) {
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

	sessionAgent, speakers := ResolveSessionSpeakers(groupID)
	require.NotNil(t, sessionAgent)
	assert.Equal(t, "<host/>", sessionAgent.Avatar, "the share path must carry the session agent's avatar")
	assert.Equal(t, "<host/>", speakers[hostMemberID].Avatar)
	assert.Equal(t, "<alice/>", speakers[aliceID].Avatar, "the share path must carry member avatars")
	// Names/backends still resolve so the viewer shows the right brand icon.
	assert.Equal(t, "claude", speakers[aliceID].Backend)
}

// TestResolveSessionSpeakers_UnknownSession is the defensive path: an unknown id
// yields nothing rather than a panic.
func TestResolveSessionSpeakers_UnknownSession(t *testing.T) {
	setupGroupDB(t)
	sessionAgent, speakers := ResolveSessionSpeakers("no-such-session")
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

	sessionAgent, _ := ResolveSessionSpeakers(sessionID)
	require.NotNil(t, sessionAgent)
	assert.Equal(t, "codex", sessionAgent.Backend, "falls back to the session row's backend")
	assert.Empty(t, sessionAgent.Avatar)
}

// TestSpeakerFromMember_PrefersMemberAvatarOverAgentAvatar pins the member
// avatar precedence: a member's OWN avatar snapshot wins over the agent's shared
// avatar. Here the member's display name equals the agent's, so AgentName stays
// empty (no redundant "Claude · Claude" disclosure).
func TestSpeakerFromMember_PrefersMemberAvatarOverAgentAvatar(t *testing.T) {
	setupGroupDB(t)
	withAgents(t, agentMap(&model.Agent{ID: "claude", Name: "Claude", Backend: "claude", Avatar: "<svg>agent</svg>"}))

	project := "/tmp/speaker-avatar"
	require.NoError(t, func() error { _, err := store.ProjectIDForPath(project); return err }())
	groupID, hostID, err := CreateGroup(project, "G", "claude", "claude", "Claude")
	require.NoError(t, err)

	const memberSvg = "<svg>member</svg>"
	_, err = store.WriteExec("UPDATE chat_sessions SET agent_avatar = ? WHERE id = ?", memberSvg, hostID)
	require.NoError(t, err)

	members, err := ListGroupMembers(groupID)
	require.NoError(t, err)
	require.NotEmpty(t, members)

	s := speakerFromMember(members[0])
	assert.Equal(t, memberSvg, s.Avatar, "the member's own avatar must win over the agent's")
	assert.Empty(t, s.AgentName, "name equals agent name → no disclosure")
}

// TestSpeakerFromMember_CarriesAgentNameWhenDifferent pins that when a member's
// display name was auto-derived (a pool nickname, or "<base> <n>"), the real
// agent name is carried so the UI can disclose which agent is speaking. The
// avatar is empty here only because the agent has no custom avatar configured.
func TestSpeakerFromMember_CarriesAgentNameWhenDifferent(t *testing.T) {
	withAgents(t, agentMap(&model.Agent{ID: "claude", Name: "Claude", Backend: "claude"}))

	s := speakerFromMember(GroupMember{AgentID: "claude", Name: "Alice", Backend: "claude"})
	assert.Equal(t, "Claude", s.AgentName)
	assert.Empty(t, s.Avatar)
}

// TestSpeakerFromMember_FallsBackToAgentAvatar pins the legacy behavior: a
// member with no per-member snapshot still resolves the agent's shared avatar.
func TestSpeakerFromMember_FallsBackToAgentAvatar(t *testing.T) {
	withAgents(t, agentMap(&model.Agent{ID: "claude", Name: "Claude", Backend: "claude", Avatar: "<svg>agent</svg>"}))

	s := speakerFromMember(GroupMember{AgentID: "claude", Name: "Alice", Backend: "claude"})
	assert.Equal(t, "<svg>agent</svg>", s.Avatar)
}
