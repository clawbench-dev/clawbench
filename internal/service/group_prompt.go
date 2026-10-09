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

// renderMemberList renders a member list as "A（desc）、B、C" (no trailing
// punctuation). Shared by every prompt that lists the roster.
func renderMemberList(members []HostMemberInfo) string {
	rendered := make([]string, 0, len(members))
	for _, m := range members {
		rendered = append(rendered, renderHostMember(m))
	}
	return strings.Join(rendered, "、")
}

// othersLine renders the "本群其他成员：…。" line for a member-facing prompt,
// excluding selfName (a member is not told about itself) and appending the
// human user as a participant. Returns "" when there is nobody else.
func othersLine(members []HostMemberInfo, selfName string) string {
	others := make([]string, 0, len(members)+1)
	for _, m := range members {
		if strings.TrimSpace(m.Name) == "" || m.Name == selfName {
			continue
		}
		others = append(others, renderHostMember(m))
	}
	if selfName != groupUserTarget() {
		others = append(others, groupUserTarget())
	}
	if len(others) == 0 {
		return ""
	}
	return "本群其他成员：" + strings.Join(others, "、") + "。\n"
}

// BuildHostSystemPrompt returns the instruction appended to the host agent's
// system prompt so it emits a parseable routing decision. members are the
// display names the host may address, with their specialties.
func BuildHostSystemPrompt(members []HostMemberInfo) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 你是本次多智能体讨论的主持人。你的职责是控场，而不是代替成员回答问题。\n")
	b.WriteString("每次发言必须包含一个路由标签，指定接下来该谁发言，并把你对他们说的话写在标签里：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\">给该成员的指令</clawbench-mention>\n")
	b.WriteString("可一次点名多个成员（逗号分隔），他们将按顺序依次发言：\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\">请分别表态</clawbench-mention>\n")
	b.WriteString("若你希望多人**同时**发言、彼此看不到对方本轮内容（如同时行动/同时表态），加 mode=\"parallel\"：\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\" mode=\"parallel\">请你们各自独立作答</clawbench-mention>\n")
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
	b.WriteString(renderMemberList(members))
	b.WriteString("。\n")

	// Addressing the human user: the user participates as a named participant
	// (the configured nickname, default "User"). Naming the user ENDS this round
	// — the user answers in their own time and the next round resumes from their
	// reply.
	userTarget := groupUserTarget()
	b.WriteString("可点名 " + userTarget + "（即用户本人）让其发言；点到 " + userTarget + " 后本轮结束，等待用户发言，用户发言后你会在下一轮看到并继续主持。\n")

	// Private notes (密送): a way to tell ONE participant something the others
	// must not see. It is the SAME tag with the `private` attribute. Documented
	// only here (not in the summary prompt, where tags are ignored) and only
	// when there is someone to address.
	b.WriteString("\n可选：密送（只给个别成员看，其他成员看不到）\n")
	b.WriteString("若你想对个别成员或用户单独交代、不希望其他人看到，改用 private 属性：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\" private>只有该成员能看到的内容</clawbench-mention>\n")
	b.WriteString("targets 可写多个，用逗号分隔，如 targets=\"A,B\"；targets=\"" + userTarget + "\" 即密送给用户。\n")
	b.WriteString("密送可以发给本轮未被点名的成员：会在该成员下次被点名时送达。\n")
	b.WriteString("公共指令（所有被点名者都能看到）照常写在不带 private 的标签里，密送可与它并存。\n")
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
	b.WriteString("  <clawbench-mention …>…</clawbench-mention>\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("发言要求：紧扣话题、简洁、直接给出你的观点；不要复述或模仿主持人的指令格式。\n")

	// The human user is a participant too; a member should know a person is in
	// the room (and may be addressed as "User").
	b.WriteString(othersLine(members, selfName))
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
	b.WriteString(renderMemberList(members))
	b.WriteString("。\n")
	return b.String()
}

// BuildFreeMemberSystemPrompt returns the instruction appended to a FREE-mode
// member's system prompt. Unlike host mode (where only the host routes and
// members are forbidden from emitting tags), every free-mode participant may
// hand the floor to someone else by @-ing them with the SAME unified tag.
//
// It teaches three things a free-mode member cannot infer:
//   - how to @ someone (the tag shape, with its body carrying what it says to
//     them);
//   - that NOT @-ing anyone is a valid way to end the relay (otherwise a model
//     tends to always @ someone and the discussion never stops);
//   - that it may @ the human user ("User") to hand the floor back.
//
// members is the full roster; selfName is excluded from the "others" list.
func BuildFreeMemberSystemPrompt(members []HostMemberInfo, selfName string) string {
	var b strings.Builder
	b.WriteString("\n\n[自由群聊] 你是一个多智能体自由群聊中的参与者，没有主持人，大家平等对话。\n")
	b.WriteString("轮到你时，直接就当前话题发表你的看法即可（简洁、直接）。\n")
	b.WriteString("如果你希望某位成员接着发言（补充、反驳、回应你），就把话筒递给他——在你的发言里写：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\">你想对他说的话</clawbench-mention>\n")
	b.WriteString("可一次 @ 多个成员（逗号分隔），他们会依次发言：\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\">请你们分别表态</clawbench-mention>\n")
	b.WriteString("若你希望多人**同时**发言、彼此看不到对方本轮内容（如同时行动/同时表态），加 mode=\"parallel\"：\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\" mode=\"parallel\">请你们各自独立作答</clawbench-mention>\n")
	b.WriteString("**如果没有人需要继续说，就不要输出任何 mention 标签**——讨论到此自然结束。\n")
	b.WriteString("你也可以 @ " + groupUserTarget() + "（即用户本人）把话筒交回给用户，用户回复后讨论继续。\n")
	// Private notes: same tag with the `private` attribute. Delivered on the
	// target's next turn (the group_pending_bcc contract), so a note to someone
	// not yet speaking still lands.
	b.WriteString("可选：密送（只给个别成员看，其他成员看不到）——给标签加 private 属性：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\" private>只有该成员能看到的内容</clawbench-mention>\n")

	b.WriteString(othersLine(members, selfName))
	return b.String()
}
