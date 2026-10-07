package service

import "strings"

// group_prompt.go holds the system-prompt fragments injected into a group
// chat's host agent so it emits parseable routing decisions. See
// docs/plans/2026-10-04-ai-group-chat-design.md §5.3.

// HostMemberInfo is one member as presented to the host: the display name it
// may route to, plus the context that makes routing sensible (decision #39).
//
// Specialty is the agent's short description ("代码编写与推理"); it is rendered
// as a parenthetical and omitted entirely when empty. Left marks a member that
// has left the group — its past speech stays on the timeline, so it is still
// listed, but visibly marked so the host does not route to it.
type HostMemberInfo struct {
	Name      string
	Specialty string
	Left      bool
}

// renderHostMember renders one entry of the host's routable-member list.
func renderHostMember(m HostMemberInfo) string {
	if m.Left {
		return m.Name + "（已离场）"
	}
	if m.Specialty != "" {
		return m.Name + "（" + m.Specialty + "）"
	}
	return m.Name
}

// BuildHostSystemPrompt returns the instruction appended to the host agent's
// system prompt so it emits a parseable routing decision. members are the
// display names the host may address, with their specialties.
func BuildHostSystemPrompt(members []HostMemberInfo) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 你是本次多智能体讨论的主持人。你的职责是控场，而不是代替成员回答问题。\n")
	b.WriteString("每次发言必须包含一个路由标签，指定接下来该谁发言：\n")
	b.WriteString("  <clawbench-speaker>成员名</clawbench-speaker> 给该成员的指令\n")
	b.WriteString("可一次点名多个成员（逗号分隔），他们将按顺序依次发言：\n")
	b.WriteString("  <clawbench-speaker>A,B</clawbench-speaker> 请分别表态\n")
	b.WriteString("当讨论已充分、可以收敛时，输出结束标签：\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("在结束标签之后，必须再写一段简短的讨论结论（最终汇总），供用户阅读。\n")
	b.WriteString("注意：你本人不在可点名名单里，**不要点名你自己**——只能点名下列其他成员。\n")
	b.WriteString("可选的成员名：")
	if len(members) == 0 {
		// Only the host is in the group: there is nobody to route to. Tell the
		// host to just answer directly instead of naming a non-existent member.
		// The bcc feature is omitted too — there is nobody to send a note to.
		b.WriteString("（暂无其他成员）\n")
		b.WriteString("当前群内没有其他成员，你无需路由，直接回答用户即可。\n")
		return b.String()
	}
	rendered := make([]string, 0, len(members))
	for _, m := range members {
		rendered = append(rendered, renderHostMember(m))
	}
	b.WriteString(strings.Join(rendered, "、"))
	b.WriteString("。\n")

	// Private notes (密送): a way to tell ONE member something the others must
	// not see. Documented only here (not in the summary prompt, where tags are
	// ignored) and only when there is someone to address.
	b.WriteString("\n可选：密送（只给个别成员看，其他成员看不到）\n")
	b.WriteString("若你想对个别成员单独交代、不希望其他成员看到，可在指令之后追加：\n")
	b.WriteString("  <clawbench-bcc targets=\"成员名\">只有该成员能看到的内容</clawbench-bcc>\n")
	b.WriteString("targets 可写多个成员，用逗号分隔，如 targets=\"A,B\"。\n")
	b.WriteString("密送的目标必须是本轮 <clawbench-speaker> 点名的成员；给未被点名的人写密送会被忽略。\n")
	b.WriteString("公共指令（所有被点名者都能看到）请照常写在 speaker 标签之后，密送可与它并存。\n")
	return b.String()
}

// BuildHostSummaryPrompt is the prompt variant for the max-rounds fallback: the
// loop ran out of rounds before the host chose to end, so we run the host once
// more purely to produce the final summary. It MUST NOT ask for a routing tag —
// the orchestrator ignores any tag in this turn to avoid re-entering the loop.
func BuildHostSummaryPrompt(members []HostMemberInfo) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 讨论轮数已达上限，现在只做收尾。\n")
	b.WriteString("请直接写一段简短的讨论结论（最终汇总），总结各成员观点与你的判断，供用户阅读。\n")
	b.WriteString("不要再输出任何路由标签或结束标签。\n")
	b.WriteString("参与成员：")
	rendered := make([]string, 0, len(members))
	for _, m := range members {
		rendered = append(rendered, renderHostMember(m))
	}
	b.WriteString(strings.Join(rendered, "、"))
	b.WriteString("。\n")
	return b.String()
}
