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
		// No routable participant at all (the orchestrator always appends the
		// user, so this only happens for a caller that passes an empty list).
		// Tell the host to answer directly instead of naming a non-existent
		// member.
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

	// Addressing the human user: the user participates as a named participant
	// (the reserved name "User"). Naming the user ENDS this round — the user
	// answers in their own time and the next round resumes from their reply.
	b.WriteString("可点名 User（即用户本人）让其发言；点到 User 后本轮结束，等待用户发言，用户发言后你会在下一轮看到并继续主持。\n")

	// Private notes (密送): a way to tell ONE participant something the others
	// must not see. Documented only here (not in the summary prompt, where tags
	// are ignored) and only when there is someone to address.
	b.WriteString("\n可选：密送（只给个别成员看，其他成员看不到）\n")
	b.WriteString("若你想对个别成员或用户单独交代、不希望其他人看到，可在指令之后追加：\n")
	b.WriteString("  <clawbench-bcc targets=\"成员名\">只有该成员能看到的内容</clawbench-bcc>\n")
	b.WriteString("targets 可写多个，用逗号分隔，如 targets=\"A,B\"；targets=\"User\" 即密送给用户。\n")
	b.WriteString("密送可以发给本轮未被点名的成员：会在该成员下次被点名时送达。\n")
	b.WriteString("公共指令（所有被点名者都能看到）请照常写在 speaker 标签之后，密送可与它并存。\n")
	return b.String()
}

// BuildMemberSystemPrompt returns the instruction appended to a group MEMBER's
// system prompt so it knows its role. Without it a member inherits its default
// single-chat identity: it sees the host's routing tag in its context and
// imitates it, so several members each declared themselves the chair and fought
// over the microphone (real incident: three members opened with "🎙️ 主持人").
//
// members is the full roster (host + members); selfName is excluded from the
// "others" list so a member is not told it is talking to itself.
func BuildMemberSystemPrompt(members []HostMemberInfo, selfName string) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊成员] 你是一个多智能体群聊中的**参与者**，**不是主持人**。\n")
	b.WriteString("主持人负责控场、点名和汇总；你只负责在被点名时，就当前话题发表你自己的看法。\n")
	b.WriteString("你**不要**替主持人安排别人发言，也**不要**输出以下协议标签（那是主持人专用的，你输出会打乱秩序）：\n")
	b.WriteString("  <clawbench-speaker>…</clawbench-speaker>\n")
	b.WriteString("  <clawbench-bcc …>…</clawbench-bcc>\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("发言要求：紧扣话题、简洁、直接给出你的观点；不要复述或模仿主持人的指令格式。\n")

	others := make([]string, 0, len(members)+1)
	for _, m := range members {
		if strings.TrimSpace(m.Name) == "" || m.Name == selfName {
			continue
		}
		others = append(others, renderHostMember(m))
	}
	// The human user is a participant too; a member should know a person is in
	// the room (and may be addressed as "User").
	if selfName != groupUserTarget {
		others = append(others, groupUserTarget)
	}
	if len(others) > 0 {
		b.WriteString("本群其他成员：")
		b.WriteString(strings.Join(others, "、"))
		b.WriteString("。\n")
	}
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
