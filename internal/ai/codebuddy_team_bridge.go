package ai

import (
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
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
// `codebuddy.ai/parentToolCallId`. ClawBench's existing sub-agent grouping
// (acp_parent_link.go → ContentBlocks.vue) keys entirely on that field, so it
// cannot route member content. The only join key the wire offers is the member
// NAME, which appears on both:
//   - the Agent spawn tool_call (`_meta.memberName` + `_meta.subagentType`),
//     whose toolCallId is the only place a member name and a tool-call id
//     co-occur;
//   - every member content frame (`_meta.memberEvent`).
//
// This bridge records member name → spawn tool-call id, then backfills
// ParentToolCallID on member content so the existing frontend grouping works
// unchanged (integration plan §3.2, "方案 A").

const (
	// metaKeyCodeBuddyTeamUpdate carries the team snapshot.
	metaKeyCodeBuddyTeamUpdate = "codebuddy.ai/teamUpdate"
	// metaKeyCodeBuddyMemberEvent marks a frame as produced by a team member;
	// its value is the member name.
	metaKeyCodeBuddyMemberEvent = "codebuddy.ai/memberEvent"
	// metaKeyCodeBuddyMemberName names the member on its Agent spawn frame.
	metaKeyCodeBuddyMemberName = "codebuddy.ai/memberName"
	// metaKeyCodeBuddySubagentType distinguishes an Agent spawn frame from
	// ordinary member content (only spawn frames carry both keys).
	metaKeyCodeBuddySubagentType = "codebuddy.ai/subagentType"
)

// extractTeamMemberName returns the team member that produced a frame, or ""
// for top-level / ordinary sub-agent content.
func extractTeamMemberName(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	return metaString(meta[metaKeyCodeBuddyMemberEvent])
}

// extractTeamMemberNameForTool resolves the team member for a TOOL frame. Member
// content frames carry `memberEvent`, but the Agent spawn frame that HOSTS the
// member's timeline carries `memberName` instead (plus `subagentType`). Reading
// both means the Agent card itself is attributed to its member, which is what
// lets the frontend colour that card with the member's own colour.
func extractTeamMemberNameForTool(meta map[string]any) string {
	if name := extractTeamMemberName(meta); name != "" {
		return name
	}
	if len(meta) == 0 {
		return ""
	}
	return metaString(meta[metaKeyCodeBuddyMemberName])
}

// memberColorByName returns the display colour for a team member from the
// latest cached team snapshot, or "" when unknown. Used to colour the member's
// timeline accent consistently with the roster.
func memberColorByName(conn *ACPConn, name string) string {
	if conn == nil || name == "" {
		return ""
	}
	state := conn.GetCachedTeamState()
	if state == nil {
		return ""
	}
	for _, m := range state.Members {
		if m.Name == name {
			return m.Color
		}
	}
	return ""
}

// parseTeamState converts the raw teamUpdate object into a TeamState. It fails
// closed (nil) when the shape is unrecognisable so a malformed extension never
// wipes a live team panel with an empty snapshot.
func parseTeamState(raw map[string]any) *TeamState {
	if len(raw) == 0 {
		return nil
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return nil
	}
	state := &TeamState{
		Type:     typ,
		TeamName: metaString(raw["teamName"]),
	}
	// isAutoTeam is absent on some snapshots (observed on the first
	// member_status_change), so only trust an explicit true.
	if v, ok := raw["isAutoTeam"].(bool); ok {
		state.IsAutoTeam = v
	}
	if v, ok := raw["hasLiveMembers"].(bool); ok {
		state.HasLive = v
	}
	if members, ok := raw["members"].([]any); ok {
		state.Members = make([]TeamMember, 0, len(members))
		for _, m := range members {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			name := metaString(mm["name"])
			if name == "" {
				continue
			}
			member := TeamMember{
				Name:        name,
				AgentType:   metaString(mm["agentType"]),
				Color:       metaString(mm["color"]),
				Description: metaString(mm["description"]),
				Status:      metaString(mm["status"]),
				Activity:    metaString(mm["activity"]),
				Lifecycle:   metaString(mm["lifecycle"]),
				TaskID:      metaString(mm["taskId"]),
				SessionID:   metaString(mm["sessionId"]),
			}
			if n, ok := mm["toolCallCount"].(float64); ok {
				member.ToolCallCount = int(n)
			}
			if tu, ok := mm["tokenUsage"].(map[string]any); ok {
				usage := &TeamTokenUsage{}
				if n, ok := tu["inputTokens"].(float64); ok {
					usage.InputTokens = int(n)
				}
				if n, ok := tu["outputTokens"].(float64); ok {
					usage.OutputTokens = int(n)
				}
				if n, ok := tu["lastContextWindow"].(float64); ok {
					usage.LastContextWindow = int(n)
				}
				member.TokenUsage = usage
			}
			state.Members = append(state.Members, member)
		}
	}
	return state
}

// bridgeCodeBuddyTeamUpdate synthesizes a team_update StreamEvent from a
// session_info_update carrying the teamUpdate extension. It is a no-op for any
// other session_info_update (title/updatedAt updates), so non-team sessions are
// unaffected.
//
// DEADLOCK SAFETY: runs on the SDK notification goroutine and only calls
// forwardACPEvent (non-blocking send) plus SetCachedTeamState (stateMu leaf
// lock) — the same safe pattern as the plan bridge.
func bridgeCodeBuddyTeamUpdate(ch chan<- StreamEvent, conn *ACPConn, meta map[string]any) {
	if len(meta) == 0 {
		return
	}
	raw, ok := meta[metaKeyCodeBuddyTeamUpdate].(map[string]any)
	if !ok {
		return
	}
	state := parseTeamState(raw)
	if state == nil {
		slog.Debug("codebuddy team bridge: unparseable teamUpdate", "meta", meta)
		return
	}
	forwardACPEvent(ch, StreamEvent{Type: "team_update", Team: state})
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

// noteCodeBuddyMemberSpawn records the member name → Agent tool-call id mapping
// when a frame is an Agent spawn for a team member. Both meta keys must be
// present: memberName alone also appears on some non-spawn frames, while
// subagentType alone appears on ordinary (non-team) sub-agents.
func noteCodeBuddyMemberSpawn(conn *ACPConn, meta map[string]any, toolCallID string) {
	if conn == nil || len(meta) == 0 {
		return
	}
	name := metaString(meta[metaKeyCodeBuddyMemberName])
	if name == "" {
		return
	}
	if metaString(meta[metaKeyCodeBuddySubagentType]) == "" {
		return
	}
	conn.SetMemberToolCallID(name, toolCallID)
}

// resolveMemberParent maps a frame's team member name to the Agent tool-call id
// that spawned it, so member content can reuse the existing sub-agent grouping.
// Returns "" when the frame is not team-member content or the spawn frame has
// not been seen yet (content can arrive before the map is populated on replay).
func resolveMemberParent(conn *ACPConn, meta map[string]any) string {
	name := extractTeamMemberName(meta)
	if name == "" || conn == nil {
		return ""
	}
	return conn.MemberToolCallID(name)
}

// memberParentOrParent resolves the parent tool-call id for a frame, preferring
// an explicit parentToolCallId (ordinary sub-agent) and falling back to the
// member-name join for Agent Team members.
func memberParentOrParent(backendID string, conn *ACPConn, meta map[string]any) string {
	if parent := extractParentToolCallID(backendID, meta); parent != "" {
		return parent
	}
	return resolveMemberParent(conn, meta)
}

// toolCallIDOf returns the string id of an ACP tool call.
func toolCallIDOf(id acp.ToolCallId) string { return string(id) }

// permissionMemberAttribution extracts the Agent Team attribution CodeBuddy
// stamps on a requestPermission's toolCall._meta. Returns nil when the request
// is not from a team member (or carries no name).
//
// Observed shape (verified on the wire, §1.5):
//
//	{"codebuddy.ai/isTeamMember": true,
//	 "codebuddy.ai/memberName": "probe-perm",
//	 "codebuddy.ai/agentColor": "blue",
//	 "codebuddy.ai/toolName": "Bash"}
func permissionMemberAttribution(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	isMember, _ := meta["codebuddy.ai/isTeamMember"].(bool)
	name := metaString(meta[metaKeyCodeBuddyMemberName])
	if !isMember || name == "" {
		return nil
	}
	out := map[string]any{"name": name}
	if color := metaString(meta["codebuddy.ai/agentColor"]); color != "" {
		out["color"] = color
	}
	return out
}
