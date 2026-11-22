package codebuddy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

func newMemberConn() *ai.ACPConn {
	return ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
}

// A member whose spawn frame has not been seen yet has no parent link: the join
// map is empty and content may arrive before it is populated on replay.
func TestFrameAttribution_UnknownMember(t *testing.T) {
	conn := newMemberConn()
	attr := FrameAttribution(conn, map[string]any{metaKeyMemberEvent: "nobody"})
	require.NotNil(t, attr, "the member is still named even with no join yet")
	assert.Equal(t, "nobody", attr.MemberName)
	assert.Equal(t, "", attr.ParentToolCallID)
	assert.Equal(t, "", attr.MemberColor)

	// No connection: the name is still read off the frame, but nothing can be
	// joined or coloured.
	orphan := FrameAttribution(nil, map[string]any{metaKeyMemberEvent: "x"})
	require.NotNil(t, orphan)
	assert.Equal(t, "x", orphan.MemberName)
	assert.Equal(t, "", orphan.ParentToolCallID)
	assert.Equal(t, "", orphan.MemberColor)
}

// An explicit parentToolCallId (ordinary sub-agent content) must win over the
// member-name join: it is the link the wire actually provided.
func TestFrameAttribution_PrefersExplicitParent(t *testing.T) {
	conn := newMemberConn()
	conn.SetMemberToolCallID("probe-alpha", "call_member")

	attr := FrameAttribution(conn, map[string]any{
		"codebuddy.ai/parentToolCallId": "call_parent",
		metaKeyMemberEvent:              "probe-alpha",
	})
	require.NotNil(t, attr)
	assert.Equal(t, "call_parent", attr.ParentToolCallID)

	// No explicit parent → the member join resolves the spawning Agent card.
	attr = FrameAttribution(conn, map[string]any{metaKeyMemberEvent: "probe-alpha"})
	require.NotNil(t, attr)
	assert.Equal(t, "call_member", attr.ParentToolCallID)
}

// The Agent spawn frame that HOSTS a member's timeline carries memberName
// instead of memberEvent, and must still be attributed plus tinted.
func TestFrameAttribution_SpawnFrameIsAttributed(t *testing.T) {
	conn := newMemberConn()
	conn.SetCachedTeamState(&ai.TeamState{Members: []ai.TeamMember{
		{Name: "spawn-name", Color: "green"},
	}})

	attr := FrameAttribution(conn, map[string]any{
		metaKeyMemberName:   "spawn-name",
		metaKeySubagentType: "general-purpose",
	})
	require.NotNil(t, attr)
	assert.Equal(t, "spawn-name", attr.MemberName)
	assert.Equal(t, "green", attr.MemberColor, "the Agent card must be tinted like its member")
}

func TestFrameAttribution_PlainFrameIsNil(t *testing.T) {
	conn := newMemberConn()
	assert.Nil(t, FrameAttribution(conn, map[string]any{"x": "y"}))
	assert.Nil(t, FrameAttribution(conn, nil))
}

// A member's colour comes from the latest roster snapshot, so a member that has
// left (or was never listed) is left uncoloured rather than guessed.
func TestFrameAttribution_MemberColor(t *testing.T) {
	conn := newMemberConn()
	conn.SetCachedTeamState(&ai.TeamState{Members: []ai.TeamMember{
		{Name: "probe-alpha", Color: "blue"},
		{Name: "probe-beta", Color: "green"},
	}})

	assert.Equal(t, "blue", FrameAttribution(conn, map[string]any{metaKeyMemberEvent: "probe-alpha"}).MemberColor)
	assert.Equal(t, "green", FrameAttribution(conn, map[string]any{metaKeyMemberEvent: "probe-beta"}).MemberColor)
	assert.Equal(t, "", FrameAttribution(conn, map[string]any{metaKeyMemberEvent: "nobody"}).MemberColor)
	// No roster at all (team not announced yet) → uncoloured, not nil.
	assert.Equal(t, "", FrameAttribution(newMemberConn(),
		map[string]any{metaKeyMemberEvent: "probe-alpha"}).MemberColor)
}

func TestPermissionMemberAttribution(t *testing.T) {
	// Team member → attribution with name + color.
	got := PermissionMemberAttribution(map[string]any{
		metaKeyIsTeamMember:     true,
		metaKeyMemberName:       "probe-perm",
		metaKeyAgentColor:       "blue",
		"codebuddy.ai/toolName": "Bash",
	})
	require.NotNil(t, got)
	assert.Equal(t, "probe-perm", got["name"])
	assert.Equal(t, "blue", got["color"])

	// Non-team request → nil.
	assert.Nil(t, PermissionMemberAttribution(map[string]any{"codebuddy.ai/toolName": "Bash"}))
	// isTeamMember true but no name → nil (nothing to display).
	assert.Nil(t, PermissionMemberAttribution(map[string]any{metaKeyIsTeamMember: true}))
	assert.Nil(t, PermissionMemberAttribution(nil))
}
