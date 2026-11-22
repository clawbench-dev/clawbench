package codebuddy

import "clawbench/internal/ai"

// Agent Teams attribution.
//
// CodeBuddy exposes multi-agent teams over ACP without adding any method: team
// state rides on the standard session_info_update notification, and member
// content rides on the standard content/tool notifications. Both are marked
// with private _meta keys (docs/dev/codebuddy_acp_team_integration.md, verified
// against a real `codebuddy --acp` process):
//
//	team state      session_info_update._meta["codebuddy.ai/teamUpdate"]
//	member content  any frame._meta["codebuddy.ai/memberEvent"] = "<member name>"
//	member spawn    tool_call._meta["codebuddy.ai/memberName"] + ".../subagentType"
//
// The critical difference from ordinary sub-agents: member frames carry NO
// `codebuddy.ai/parentToolCallId`. The shared grouping keys entirely on that
// field, so it cannot route member content. The only join key the wire offers is
// the member NAME, which appears on both the Agent spawn tool_call and every
// member content frame. NoteMemberSpawn records name → spawn call id; the hook
// below backfills the parent link from it.

const (
	// metaKeyAgentColor carries the member's display color on a permission
	// request's toolCall._meta.
	metaKeyAgentColor = "codebuddy.ai/agentColor"
	// metaKeyIsTeamMember marks a permission request as coming from a team
	// member.
	metaKeyIsTeamMember = "codebuddy.ai/isTeamMember"
)

// FrameAttribution resolves a frame's team member, its color and the Agent
// card it belongs under.
//
// Content frames carry `memberEvent`; the Agent spawn frame that HOSTS a
// member's timeline carries `memberName` instead, so both are read — that is
// what lets the Agent card itself be attributed to its member and colored with
// the member's own color.
func FrameAttribution(conn *ai.ACPConn, meta map[string]any) *ai.FrameAttribution {
	if len(meta) == 0 {
		return nil
	}
	name := ai.MetaString(meta[metaKeyMemberEvent])
	if name == "" {
		// Not member content. Fall back to the spawn-frame shape so the Agent
		// card that hosts a member reads as that member.
		name = ai.MetaString(meta[metaKeyMemberName])
	}
	// The standard parent link wins when present (ordinary sub-agent content);
	// only member frames, which carry no link, need the name join.
	parent := ai.ExtractParentToolCallIDForHook(meta)
	if parent == "" && name != "" && conn != nil {
		parent = conn.MemberToolCallID(name)
	}
	color := ""
	if name != "" && conn != nil {
		if state := conn.GetCachedTeamState(); state != nil {
			for _, m := range state.Members {
				if m.Name == name {
					color = m.Color
					break
				}
			}
		}
	}
	if name == "" && parent == "" {
		return nil
	}
	return &ai.FrameAttribution{MemberName: name, MemberColor: color, ParentToolCallID: parent}
}

// PermissionMemberAttribution reads the Agent Team attribution CodeBuddy stamps
// on a requestPermission's toolCall._meta. Returns nil when the request is not
// from a team member.
//
// Observed shape (verified on the wire, §1.5):
//
//	{"codebuddy.ai/isTeamMember": true,
//	 "codebuddy.ai/memberName": "probe-perm",
//	 "codebuddy.ai/agentColor": "blue",
//	 "codebuddy.ai/toolName": "Bash"}
func PermissionMemberAttribution(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	isMember, _ := meta[metaKeyIsTeamMember].(bool)
	name := ai.MetaString(meta[metaKeyMemberName])
	if !isMember || name == "" {
		return nil
	}
	out := map[string]any{"name": name}
	if color := ai.MetaString(meta[metaKeyAgentColor]); color != "" {
		out["color"] = color
	}
	return out
}
