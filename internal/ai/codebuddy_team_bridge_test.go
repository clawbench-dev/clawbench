package ai

import (
	"encoding/json"
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

func mustMeta(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	return m
}

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

func TestParseTeamState_MemberStatusChange(t *testing.T) {
	raw := mustMeta(t, teamUpdateMemberStatus)
	state := parseTeamState(raw)
	require.NotNil(t, state)

	assert.Equal(t, "member_status_change", state.Type)
	assert.Equal(t, "clawbench-probe", state.TeamName)
	assert.False(t, state.IsAutoTeam)
	assert.True(t, state.HasLive)
	require.Len(t, state.Members, 2)

	a := state.Members[0]
	assert.Equal(t, "probe-alpha", a.Name)
	assert.Equal(t, "general-purpose", a.AgentType)
	assert.Equal(t, "blue", a.Color)
	assert.Equal(t, "running", a.Status)
	assert.Equal(t, "working", a.Activity)
	assert.Equal(t, "alive", a.Lifecycle)
	assert.Equal(t, "agent-08d3", a.TaskID)
	assert.Equal(t, "sess-a", a.SessionID)
	assert.Equal(t, 2, a.ToolCallCount)
	require.NotNil(t, a.TokenUsage)
	assert.Equal(t, 56763, a.TokenUsage.InputTokens)
	assert.Equal(t, 229, a.TokenUsage.OutputTokens)
	assert.Equal(t, 28430, a.TokenUsage.LastContextWindow)

	b := state.Members[1]
	assert.Equal(t, "completed", b.Status)
	assert.Equal(t, "idle", b.Activity)
}

func TestParseTeamState_CreatedMinimal(t *testing.T) {
	state := parseTeamState(mustMeta(t, teamUpdateCreated))
	require.NotNil(t, state)
	assert.Equal(t, "team_created", state.Type)
	assert.Empty(t, state.Members)
	assert.False(t, state.HasLive)
}

// A missing isAutoTeam (observed on the first member_status_change snapshot)
// must not be mistaken for an explicit false — only an explicit true is trusted.
func TestParseTeamState_MissingIsAutoTeam(t *testing.T) {
	state := parseTeamState(map[string]any{"type": "member_status_change", "teamName": "t"})
	require.NotNil(t, state)
	assert.False(t, state.IsAutoTeam)
}

func TestParseTeamState_ExplicitAutoTeam(t *testing.T) {
	state := parseTeamState(map[string]any{"type": "team_created", "teamName": "_auto_x", "isAutoTeam": true})
	require.NotNil(t, state)
	assert.True(t, state.IsAutoTeam)
}

// Fail closed on malformed shapes so a bad extension never wipes a live panel.
func TestParseTeamState_FailsClosed(t *testing.T) {
	assert.Nil(t, parseTeamState(nil))
	assert.Nil(t, parseTeamState(map[string]any{}))
	assert.Nil(t, parseTeamState(map[string]any{"teamName": "no-type"}))
}

// Members without a name are skipped (the name is the join key).
func TestParseTeamState_SkipsNamelessMembers(t *testing.T) {
	state := parseTeamState(map[string]any{
		"type":     "member_status_change",
		"teamName": "t",
		"members":  []any{map[string]any{"status": "running"}, map[string]any{"name": "ok"}},
	})
	require.NotNil(t, state)
	require.Len(t, state.Members, 1)
	assert.Equal(t, "ok", state.Members[0].Name)
}

// ---------------------------------------------------------------------------
// bridgeCodeBuddyTeamUpdate
// ---------------------------------------------------------------------------

func TestBridgeTeamUpdate_EmitsAndCaches(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 4)

	meta := map[string]any{metaKeyCodeBuddyTeamUpdate: mustMeta(t, teamUpdateMemberStatus)}
	bridgeCodeBuddyTeamUpdate(ch, conn, meta)

	select {
	case ev := <-ch:
		assert.Equal(t, "team_update", ev.Type)
		require.NotNil(t, ev.Team)
		assert.Equal(t, "member_status_change", ev.Team.Type)
		assert.Len(t, ev.Team.Members, 2)
	default:
		t.Fatal("expected a team_update event")
	}
	cached := conn.GetCachedTeamState()
	require.NotNil(t, cached)
	assert.Equal(t, "clawbench-probe", cached.TeamName)
}

// A plain session_info_update (title/updatedAt, no teamUpdate key) must not
// produce an event — this is the non-team regression guard.
func TestBridgeTeamUpdate_NoopWithoutKey(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 4)

	bridgeCodeBuddyTeamUpdate(ch, conn, map[string]any{"codebuddy.ai/agentPhase": "running"})
	assert.Empty(t, ch, "no teamUpdate key must not emit")
	assert.Nil(t, conn.GetCachedTeamState())
}

func TestBridgeTeamUpdate_DeleteClearsCache(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 4)

	// Seed a live team first.
	bridgeCodeBuddyTeamUpdate(ch, conn, map[string]any{
		metaKeyCodeBuddyTeamUpdate: mustMeta(t, teamUpdateMemberStatus),
	})
	require.NotNil(t, conn.GetCachedTeamState())

	bridgeCodeBuddyTeamUpdate(ch, conn, map[string]any{
		metaKeyCodeBuddyTeamUpdate: map[string]any{"type": "team_deleted", "teamName": "clawbench-probe"},
	})
	assert.Nil(t, conn.GetCachedTeamState(), "team_deleted must clear the cache")
}

// ---------------------------------------------------------------------------
// Member-name ↔ tool-call join
// ---------------------------------------------------------------------------

func TestMemberSpawnJoin_RecordsAndResolves(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")

	spawnMeta := map[string]any{
		metaKeyCodeBuddyMemberName:   "probe-alpha",
		metaKeyCodeBuddySubagentType: "general-purpose",
	}
	noteCodeBuddyMemberSpawn(conn, spawnMeta, "call_00_gQeF1pbxpR25Ip7O4kwj0521")
	assert.Equal(t, "call_00_gQeF1pbxpR25Ip7O4kwj0521", conn.MemberToolCallID("probe-alpha"))

	// A member content frame carries only memberEvent; the join resolves the
	// parent tool-call id for grouping.
	contentMeta := map[string]any{metaKeyCodeBuddyMemberEvent: "probe-alpha"}
	assert.Equal(t, "call_00_gQeF1pbxpR25Ip7O4kwj0521", resolveMemberParent(conn, contentMeta))
}

// memberName WITHOUT subagentType is not a spawn frame (some non-spawn frames
// carry memberName), so it must not pollute the join map.
func TestMemberSpawnJoin_RequiresSubagentType(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	noteCodeBuddyMemberSpawn(conn, map[string]any{metaKeyCodeBuddyMemberName: "x"}, "call_1")
	assert.Equal(t, "", conn.MemberToolCallID("x"))
}

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
func TestMapACPSessionUpdate_MemberContentAttributed(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.SetMemberToolCallID("probe-alpha", "call_agent")

	ch := make(chan StreamEvent, 16)
	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.TextBlock("hello from alpha"),
			Meta:    map[string]any{metaKeyCodeBuddyMemberEvent: "probe-alpha"},
		},
	}
	mapACPSessionUpdate(update, ch, t.Context(), conn, nil)

	var content *StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "content" {
			e := ev
			content = &e
		}
	}
	require.NotNil(t, content, "expected a content event")
	assert.Equal(t, "hello from alpha", content.Content)
	assert.Equal(t, "probe-alpha", content.MemberName)
	assert.Equal(t, "call_agent", content.ParentToolCallID)
}

// A team member's tool call must be attributed too.
func TestMapACPSessionUpdate_MemberToolCallAttributed(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.SetMemberToolCallID("probe-beta", "call_beta_agent")

	ch := make(chan StreamEvent, 16)
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "call_tool",
		Title:      "Bash",
		Meta: map[string]any{
			metaKeyCodeBuddyMemberEvent: "probe-beta",
			"codebuddy.ai/toolName":     "Bash",
		},
	}
	mapACPSessionUpdate(acp.SessionUpdate{ToolCall: &tc}, ch, t.Context(), conn, nil)

	var tool *StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "tool_use" {
			e := ev
			tool = &e
		}
	}
	require.NotNil(t, tool, "expected a tool_use event")
	assert.Equal(t, "probe-beta", tool.MemberName)
	require.NotNil(t, tool.Tool)
	assert.Equal(t, "call_beta_agent", tool.Tool.ParentToolCallID)
}

// The session_info_update branch must route a teamUpdate meta into a
// team_update event through the full mapper.
func TestMapACPSessionUpdate_TeamUpdateRouted(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 16)

	update := teamInfoUpdate(map[string]any{
		metaKeyCodeBuddyTeamUpdate: mustMeta(t, teamUpdateMemberStatus),
	})
	mapACPSessionUpdate(update, ch, t.Context(), conn, nil)

	var team *StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "team_update" {
			e := ev
			team = &e
		}
	}
	require.NotNil(t, team, "expected a team_update event")
	require.NotNil(t, team.Team)
	assert.Equal(t, "clawbench-probe", team.Team.TeamName)
	assert.Len(t, team.Team.Members, 2)
}

// A non-team session_info_update must produce no events (regression guard for
// the previously-dead branch).
func TestMapACPSessionUpdate_SessionInfoNoTeamNoEvent(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 16)

	update := teamInfoUpdate(map[string]any{"codebuddy.ai/agentPhase": "running"})
	mapACPSessionUpdate(update, ch, t.Context(), conn, nil)
	assert.Empty(t, ch, "plain session_info_update must not emit events")
}

// A non-CodeBuddy backend must not run the team bridge.
func TestMapACPSessionUpdate_TeamUpdateIgnoredForOtherBackends(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	ch := make(chan StreamEvent, 16)

	update := teamInfoUpdate(map[string]any{
		metaKeyCodeBuddyTeamUpdate: mustMeta(t, teamUpdateCreated),
	})
	mapACPSessionUpdate(update, ch, t.Context(), conn, nil)
	assert.Empty(t, ch, "team bridge is CodeBuddy-only")
}

// ---------------------------------------------------------------------------
// Permission attribution
// ---------------------------------------------------------------------------

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
