package codebuddy

import (
	"log/slog"

	"clawbench/internal/ai"
)

// ---------------------------------------------------------------------------
// CodeBuddy Agent Teams bridge
// ---------------------------------------------------------------------------
//
// CodeBuddy exposes multi-agent teams over ACP without adding any method: team
// state rides on the standard `session_info_update` notification, and member
// content rides on the standard content/tool notifications. Both are marked
// with `_meta` keys (see docs/dev/codebuddy_acp_team_integration.md, verified
// against a real `codebuddy --acp` process):
//
//	team state   session_info_update._meta["codebuddy.ai/teamUpdate"]
//	             { type, teamName, isAutoTeam, hasLiveMembers, members: [...] }
//	member content  any frame._meta["codebuddy.ai/memberEvent"] = "<member name>"
//
// The critical difference from ordinary sub-agents: member frames carry NO
// `codebuddy.ai/parentToolCallId`. ClawBench's existing sub-agent grouping keys
// entirely on that field, so it cannot route member content. The only join key
// the wire offers is the member NAME, which appears on both:
//   - the Agent spawn tool_call (`_meta.memberName` + `_meta.subagentType`),
//     whose toolCallId is the only place a member name and a tool-call id
//     co-occur;
//   - every member content frame (`_meta.memberEvent`).
//
// This bridge records member name → spawn tool-call id, then backfills
// ParentToolCallID on member content so the existing frontend grouping works
// unchanged (integration plan §3.2, "方案 A").

const (
	// metaKeyTeamUpdate carries the team snapshot.
	metaKeyTeamUpdate = "codebuddy.ai/teamUpdate"
	// metaKeyMemberName names the member on its Agent spawn frame.
	metaKeyMemberName = "codebuddy.ai/memberName"
	// metaKeySubagentType distinguishes an Agent spawn frame from ordinary
	// member content (only spawn frames carry both keys).
	metaKeySubagentType = "codebuddy.ai/subagentType"
	// metaKeyMemberEvent marks a frame as produced by a team member; its value
	// is the member name.
	metaKeyMemberEvent = "codebuddy.ai/memberEvent"
)

// NoteMemberSpawn records the member name → Agent tool-call id mapping when a
// frame is an Agent spawn for a team member. Both meta keys must be present:
// memberName alone also appears on some non-spawn frames, while subagentType
// alone appears on ordinary (non-team) sub-agents.
func NoteMemberSpawn(conn *ai.ACPConn, meta map[string]any, toolCallID string) {
	if conn == nil || len(meta) == 0 {
		return
	}
	name := ai.MetaString(meta[metaKeyMemberName])
	if name == "" {
		return
	}
	if ai.MetaString(meta[metaKeySubagentType]) == "" {
		return
	}
	conn.SetMemberToolCallID(name, toolCallID)
}

// BridgeTeamUpdate synthesizes a team_update StreamEvent from a
// session_info_update carrying the teamUpdate extension. It is a no-op for any
// other session_info_update (title/updatedAt updates), so non-team sessions are
// unaffected.
//
// DEADLOCK SAFETY: runs on the SDK notification goroutine and only calls
// ai.ForwardACPEvent (non-blocking send) plus SetCachedTeamState (stateMu leaf
// lock) — the same safe pattern as the plan bridge.
func BridgeTeamUpdate(conn *ai.ACPConn, ch chan<- ai.StreamEvent, meta map[string]any) {
	if len(meta) == 0 {
		return
	}
	raw, ok := meta[metaKeyTeamUpdate].(map[string]any)
	if !ok {
		return
	}
	state := parseTeamState(raw)
	if state == nil {
		slog.Debug("codebuddy team bridge: unparseable teamUpdate", "meta", meta)
		return
	}
	ai.ForwardACPEvent(ch, ai.StreamEvent{Type: "team_update", Team: state})
	if conn != nil {
		if state.Type == "team_deleted" {
			// A deleted team has no members to show; clear the cache so a
			// reconnect does not resurrect the panel.
			conn.SetCachedTeamState(nil)
		} else {
			conn.SetCachedTeamState(state)
		}
	}
	slog.Debug("codebuddy team bridge: team_update", "type", state.Type, "team", state.TeamName, "members", len(state.Members))
}

func parseTeamState(raw map[string]any) *ai.TeamState {
	if len(raw) == 0 {
		return nil
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return nil
	}
	state := &ai.TeamState{
		Type:     typ,
		TeamName: ai.MetaString(raw["teamName"]),
	}
	// isAutoTeam and hasLiveMembers are both absent on some snapshots (observed
	// on the first member_status_change), so only trust an explicit bool. Keeping
	// HasLive as a pointer preserves the absent state through serialization.
	if v, ok := raw["isAutoTeam"].(bool); ok {
		state.IsAutoTeam = v
	}
	if v, ok := raw["hasLiveMembers"].(bool); ok {
		state.HasLive = &v
	}
	if members, ok := raw["members"].([]any); ok {
		state.Members = make([]ai.TeamMember, 0, len(members))
		for _, m := range members {
			if member, ok := parseTeamMember(m); ok {
				state.Members = append(state.Members, member)
			}
		}
	}
	return state
}

// parseTeamMember converts one entry of the teamUpdate members array. It reports
// false for entries that are not objects or lack a name, so the caller can skip
// them without duplicating the shape checks.
func parseTeamMember(m any) (ai.TeamMember, bool) {
	mm, ok := m.(map[string]any)
	if !ok {
		return ai.TeamMember{}, false
	}
	name := ai.MetaString(mm["name"])
	if name == "" {
		return ai.TeamMember{}, false
	}
	member := ai.TeamMember{
		Name:        name,
		AgentType:   ai.MetaString(mm["agentType"]),
		Color:       ai.MetaString(mm["color"]),
		Description: ai.MetaString(mm["description"]),
		Status:      ai.MetaString(mm["status"]),
		Activity:    ai.MetaString(mm["activity"]),
		Lifecycle:   ai.MetaString(mm["lifecycle"]),
		TaskID:      ai.MetaString(mm["taskId"]),
		SessionID:   ai.MetaString(mm["sessionId"]),
	}
	if n, ok := mm["toolCallCount"].(float64); ok {
		member.ToolCallCount = int(n)
	}
	if tu, ok := mm["tokenUsage"].(map[string]any); ok {
		member.TokenUsage = parseTeamTokenUsage(tu)
	}
	return member, true
}

// parseTeamTokenUsage extracts the token counters from a member's tokenUsage
// object; absent fields stay zero.
func parseTeamTokenUsage(tu map[string]any) *ai.TeamTokenUsage {
	usage := &ai.TeamTokenUsage{}
	if n, ok := tu["inputTokens"].(float64); ok {
		usage.InputTokens = int(n)
	}
	if n, ok := tu["outputTokens"].(float64); ok {
		usage.OutputTokens = int(n)
	}
	if n, ok := tu["lastContextWindow"].(float64); ok {
		usage.LastContextWindow = int(n)
	}
	return usage
}
