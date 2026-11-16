package ai

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// Payloads below are verbatim shapes captured from a real `codebuddy --acp`
// process (see docs/dev/codebuddy_acp_team_integration.md §1 and the probe at
// codebuddy_acp_team_probe_integration_test.go). Keep them in sync with the
// wire — they are the regression anchor for the team bridge.

// teamUpdateMemberStatus is a member_status_change snapshot with two members,
// one running and one completed+idle.
const teamUpdateMemberStatus = `{
  "type": "member_status_change",
  "teamName": "clawbench-probe",
  "isAutoTeam": false,
  "hasLiveMembers": true,
  "members": [
    {"name":"probe-alpha","agentType":"general-purpose","color":"blue",
     "description":"alpha task","status":"running","activity":"working",
     "lifecycle":"alive","taskId":"agent-08d3","sessionId":"sess-a",
     "tokenUsage":{"inputTokens":56763,"outputTokens":229,"lastContextWindow":28430},
     "toolCallCount":2},
    {"name":"probe-beta","agentType":"general-purpose","color":"green",
     "description":"beta task","status":"completed","activity":"idle",
     "lifecycle":"alive","taskId":"agent-ced1","sessionId":"sess-b",
     "tokenUsage":{"inputTokens":85190,"outputTokens":205,"lastContextWindow":28520},
     "toolCallCount":2}
  ]
}`

// teamUpdateCreated is the minimal creation event.
const teamUpdateCreated = `{"type":"team_created","teamName":"clawbench-probe","hasLiveMembers":false}`

func teamInfoUpdate(meta map[string]any) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			SessionUpdate: "session_info_update",
			Meta:          meta,
		},
	}
}

// ---------------------------------------------------------------------------
// parseTeamState
// ---------------------------------------------------------------------------

func TestResolveMemberParent_UnknownMember(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	assert.Equal(t, "", resolveMemberParent(conn, map[string]any{metaKeyCodeBuddyMemberEvent: "nobody"}))
	assert.Equal(t, "", resolveMemberParent(nil, map[string]any{metaKeyCodeBuddyMemberEvent: "x"}))
}

// memberParentOrParent prefers an explicit parentToolCallId (ordinary
// sub-agent) and only falls back to the member join.
func TestMemberParentOrParent_PrefersExplicitParent(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.SetMemberToolCallID("probe-alpha", "call_member")

	meta := map[string]any{
		metaKeyCodeBuddyParentToolCallID: "call_parent",
		metaKeyCodeBuddyMemberEvent:      "probe-alpha",
	}
	assert.Equal(t, "call_parent", memberParentOrParent("codebuddy", conn, meta))

	// No explicit parent → member join.
	assert.Equal(t, "call_member", memberParentOrParent("codebuddy", conn,
		map[string]any{metaKeyCodeBuddyMemberEvent: "probe-alpha"}))
}

// ---------------------------------------------------------------------------
// Full mapping: member content is attributed and grouped
// ---------------------------------------------------------------------------

// A team member's text chunk must carry MemberName and a ParentToolCallID
// resolved from the spawn join (it carries no parentToolCallId on the wire).
func TestPermissionMemberAttribution(t *testing.T) {
	// Team member → attribution with name + color.
	got := permissionMemberAttribution(map[string]any{
		"codebuddy.ai/isTeamMember": true,
		"codebuddy.ai/memberName":   "probe-perm",
		"codebuddy.ai/agentColor":   "blue",
		"codebuddy.ai/toolName":     "Bash",
	})
	require.NotNil(t, got)
	assert.Equal(t, "probe-perm", got["name"])
	assert.Equal(t, "blue", got["color"])

	// Non-team request → nil.
	assert.Nil(t, permissionMemberAttribution(map[string]any{"codebuddy.ai/toolName": "Bash"}))
	// isTeamMember true but no name → nil (nothing to display).
	assert.Nil(t, permissionMemberAttribution(map[string]any{"codebuddy.ai/isTeamMember": true}))
	assert.Nil(t, permissionMemberAttribution(nil))
}

// ---------------------------------------------------------------------------
// Member color resolution (P6)
// ---------------------------------------------------------------------------

func TestMemberColorByName(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	// Seed a team snapshot with a colored member.
	conn.SetCachedTeamState(&TeamState{Members: []TeamMember{
		{Name: "probe-alpha", Color: "blue"},
		{Name: "probe-beta", Color: "green"},
	}})
	assert.Equal(t, "blue", memberColorByName(conn, "probe-alpha"))
	assert.Equal(t, "green", memberColorByName(conn, "probe-beta"))
	assert.Equal(t, "", memberColorByName(conn, "nobody"))
	assert.Equal(t, "", memberColorByName(nil, "probe-alpha"))
}

// A tool frame's member may arrive as memberName (the Agent spawn frame that
// HOSTS the timeline) rather than memberEvent (member-produced content).
func TestExtractTeamMemberNameForTool(t *testing.T) {
	// memberEvent wins when both present.
	assert.Equal(t, "a", extractTeamMemberNameForTool(map[string]any{
		metaKeyCodeBuddyMemberEvent: "a", metaKeyCodeBuddyMemberName: "b",
	}))
	// memberName-only (spawn frame) is still attributed.
	assert.Equal(t, "spawn-name", extractTeamMemberNameForTool(map[string]any{
		metaKeyCodeBuddyMemberName: "spawn-name", metaKeyCodeBuddySubagentType: "general-purpose",
	}))
	// Neither → empty.
	assert.Equal(t, "", extractTeamMemberNameForTool(map[string]any{"x": "y"}))
	assert.Equal(t, "", extractTeamMemberNameForTool(nil))
}

// The spawn frame (memberName) must resolve its color so the Agent card hosting
// the member's timeline can be tinted.
