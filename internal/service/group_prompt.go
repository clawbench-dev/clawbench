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

// selfIdentityLine states the acting agent's OWN group display name. Without it
// a member falls back to the CLI's own identity (its agent name), which is wrong
// when the same agent appears several times (duplicates get distinct
// auto-assigned pool names; in the niche pool-exhausted fallback they get
// "<base> <n>"): every instance would otherwise claim the same bare name (or
// invent a number it saw in the roster). Declaring the name up front is what
// keeps duplicate instances distinguishable to themselves. Returns "" for an
// empty name so a caller without one adds no confusing sentence.
func selfIdentityLine(selfName string) string {
	if strings.TrimSpace(selfName) == "" {
		return ""
	}
	return "你的群内显示名是「" + selfName + "」。在群里就用这个名字自称——不要用名单里别人的名字，也不要自己编造带数字的名字。\n"
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
// system prompt so it emits a parseable routing decision. selfName is the host's
// OWN group display name (declared so a duplicated host instance does not lose
// its own identity); members are the display names the host may address, with
// their specialties.
func BuildHostSystemPrompt(selfName string, members []HostMemberInfo) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 你是本次多智能体讨论的主持人。你的职责是控场，而不是代替成员回答问题。\n")
	b.WriteString(selfIdentityLine(selfName))
	b.WriteString("每次发言必须包含一个路由标签，指定接下来该谁发言，并把你对他们说的话写在标签里：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\">给该成员的指令</clawbench-mention>\n")
	b.WriteString("可一次点名多个成员（逗号分隔），默认**串行**：他们按顺序依次发言，后发言者能看到前者本轮内容。\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\">请分别表态</clawbench-mention>\n")
	b.WriteString("若你希望多人**同时**发言、各自独立互不影响（并发执行也更快），加 mode=\"parallel\"：\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\" mode=\"parallel\">请你们各自独立作答</clawbench-mention>\n")
	b.WriteString("如何选择：需要**互相补充、层层深入、后发言者回应前者**时用串行（默认）；需要**彼此不受影响的独立意见**时用 parallel——例如独立评审、各自出方案、分别估算，或你不希望后发言者被前者的观点带偏。\n")
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
	b.WriteString("成员之间**不能**互发密送——需要让某人知道什么，由你转达或公开说明。\n")
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
//
// hostName is the HOST's group display name. It is required, not cosmetic: a
// member's only allowed private channel is "to the host", and a mention target
// is resolved by display NAME — so a prompt that says 「targets="主持人名"」 gives
// the model a literal placeholder to copy, which resolves to no member and is
// silently dropped. Naming the actual host is what makes the instruction
// executable. Empty (a free group, which has no host) omits the whole
// private-channel paragraph.
func BuildMemberSystemPrompt(members []HostMemberInfo, selfName, hostName string) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊成员] 你是一个多智能体群聊中的**参与者**，**不是主持人**。\n")
	b.WriteString(selfIdentityLine(selfName))
	b.WriteString("主持人负责控场、点名和汇总；你只负责在被点名时，就当前话题发表你自己的看法。\n")
	b.WriteString("你**不要**替主持人安排别人发言，也**不要**输出以下协议标签（那是主持人专用的，你输出会打乱秩序）：\n")
	b.WriteString("  <clawbench-mention …>…</clawbench-mention>\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("发言要求：紧扣话题、简洁、直接给出你的观点；不要复述或模仿主持人的指令格式。\n")
	// The one private channel a member DOES have. Stating it explicitly keeps a
	// member from writing private tags to peers, which the orchestrator drops
	// silently (noteAllowed) — a silent drop the model would otherwise read as
	// "it worked". The host is named concretely (see hostName above).
	if host := strings.TrimSpace(hostName); host != "" {
		b.WriteString("若你有话只能让主持人或用户看到，用密送（给标签加 private 属性，targets 写主持人「" + host + "」或用户「" + groupUserTarget() + "」）：\n")
		b.WriteString("  <clawbench-mention targets=\"" + host + "\" private>只给主持人看的话</clawbench-mention>\n")
		b.WriteString("**不要**给其他成员发密送——成员之间不传递私密信息，会被系统丢弃。\n")
	}

	// The human user is a participant too; a member should know a person is in
	// the room (and may be addressed as "User").
	b.WriteString(othersLine(members, selfName))
	return b.String()
}

// BuildHostSummaryPrompt is the prompt variant for the max-rounds fallback: the
// loop ran out of rounds before the host chose to end, so we run the host once
// more purely to produce the final summary. It MUST NOT ask for a routing tag —
// the orchestrator ignores any tag in this turn to avoid re-entering the loop.
// selfName is the host's own display name (see BuildHostSystemPrompt).
func BuildHostSummaryPrompt(selfName string, members []HostMemberInfo) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 讨论轮数已达上限，现在只做收尾。\n")
	b.WriteString(selfIdentityLine(selfName))
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
	b.WriteString(selfIdentityLine(selfName))
	b.WriteString("轮到你时，直接就当前话题发表你的看法即可（简洁、直接）。\n")
	b.WriteString("如果你希望某位成员接着发言（补充、反驳、回应你），就把话筒递给他——在你的发言里写：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\">你想对他说的话</clawbench-mention>\n")
	b.WriteString("可一次 @ 多个成员（逗号分隔），默认**串行**：他们会依次发言，后发言者能看到前者本轮内容。\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\">请你们分别表态</clawbench-mention>\n")
	b.WriteString("若你希望多人**同时**发言、各自独立互不影响（并发执行也更快），加 mode=\"parallel\"：\n")
	b.WriteString("  <clawbench-mention targets=\"A,B\" mode=\"parallel\">请你们各自独立作答</clawbench-mention>\n")
	b.WriteString("如何选择：需要**互相补充、层层深入、后发言者回应前者**时用串行（默认）；需要**彼此不受影响的独立意见**时用 parallel——例如独立评审、各自出方案、分别估算，或你不希望后发言者被前者的观点带偏。\n")
	b.WriteString("**如果没有人需要继续说，就不要输出任何 mention 标签**——讨论到此自然结束。\n")
	b.WriteString("你也可以 @ " + groupUserTarget() + "（即用户本人）把话筒交回给用户，用户回复后讨论继续。\n")
	// Private notes: same tag with the `private` attribute. Delivered on the
	// target's next turn (the group_pending_bcc contract), so a note to someone
	// not yet speaking still lands.
	//
	// Free mode has NO host, so every pair is allowed here (noteAllowed returns
	// true unconditionally) — unlike the host-mode member prompt, which limits a
	// member to the host and the user.
	b.WriteString("可选：密送（只给个别成员看，其他成员看不到）——给标签加 private 属性：\n")
	b.WriteString("  <clawbench-mention targets=\"成员名\" private>只有该成员能看到的内容</clawbench-mention>\n")
	b.WriteString("本群没有主持人，你可以密送给任何成员或用户。\n")

	b.WriteString(othersLine(members, selfName))
	return b.String()
}

// ── Auto-cruise prompts (design §15) ─────────────────────────────────────
//
// Cruise is an orthogonal switch on top of host/free. These fragments are
// appended by the orchestrator when the group's cruise switch is on; they are
// separate builders so the non-cruise prompts stay byte-identical (existing
// prompt tests and model behavior are unaffected).

// BuildCruiseHostAddendum is appended to the HOST's system prompt when the group
// runs in AUTO-CRUISE mode. It changes the END rule: the host's
// <clawbench-group-end/> no longer ends the discussion directly — it triggers a
// system confirmation round in which the host is asked whether the goal is met,
// and only <clawbench-goal-satisfied/> actually ends it. An UNSOLICITED
// <clawbench-goal-satisfied/> does not end it either (it just triggers the same
// confirmation round), so the host is told to wait to be asked. Without this
// addendum the host keeps emitting group-end expecting to stop, and the
// confirmation would look like an error to it.
func BuildCruiseHostAddendum() string {
	var b strings.Builder
	b.WriteString("\n\n[自动巡航模式] 本群已开启自动巡航：讨论将持续进行，直到你（主持人）确认任务已完成。\n")
	b.WriteString("注意：巡航模式下 <clawbench-group-end/> **不再直接结束讨论**——它会让系统向你发起一次「完成确认」，由你判断是否真正完成。\n")
	b.WriteString("当系统向你确认时：\n")
	b.WriteString("  - 若你认为已经满足用户的要求，输出结束信号：<clawbench-goal-satisfied/>，**并在同一条回复里写出最终结论**（供用户阅读，不要再点名）。\n")
	b.WriteString("  - 若尚未完成，就照常点名（<clawbench-mention targets=\"…\">）继续推进，不要输出 goal-satisfied。\n")
	b.WriteString("不要主动输出 <clawbench-goal-satisfied/>——只有系统向你确认时才判断；提前输出不会结束讨论，只会再触发一次确认。\n")
	b.WriteString("请持续组织成员推进工作，直到确实完成，不要为了收工而过早确认。\n")
	return b.String()
}

// BuildCruiseFreeMemberAddendum is appended to a FREE-mode member's system
// prompt when the group runs in AUTO-CRUISE mode. It teaches the completion
// declaration: a member that judges the goal met emits
// <clawbench-goal-satisfied/>. It also forbids asking the user, since the floor
// never returns to the human while cruising.
func BuildCruiseFreeMemberAddendum() string {
	var b strings.Builder
	b.WriteString("\n\n[自动巡航模式] 本群已开启自动巡航：讨论将持续进行，直到至少半数成员确认任务已完成。\n")
	b.WriteString("如果你认为已经满足用户的要求，输出结束信号：<clawbench-goal-satisfied/>（可与你的发言并存）。\n")
	b.WriteString("如果尚未完成，继续正常讨论与 @ 接力，不要输出 goal-satisfied。\n")
	b.WriteString("巡航期间**不要向用户提问**——用户不会立即回复，所有不明确之处请通过探索、实验、思考与讨论自行决定。\n")
	return b.String()
}

// BuildCruiseConfirmPrompt is the hidden instruction injected when a cruise turn
// reaches a point that would normally hand the floor back to the human (the
// host's end tag, a drained free relay, an @User, or a question). It is the
// "confirmation round" prompt: it is NOT persisted to the chat, only injected
// into the participants' context for that round.
//
// freeMode selects the wording: a free member is asked to evaluate and emit
// <clawbench-goal-satisfied/>; the host is asked to either confirm (goal tag,
// with the final conclusion written in the SAME reply) or keep routing. Both
// variants forbid asking the user.
//
// The host's confirm branch asks for the conclusion too: the confirmation round
// is the ONLY wrap-up turn (there is no separate summary call after it), so if
// the host just emitted the tag the user would be left without a conclusion.
// It is conditional, though: when the host's own PREVIOUS reply already carried
// the full conclusion (the common case — it wrote the wrap-up and then emitted
// the end tag), repeating it would duplicate a long block on the timeline, so
// the confirm round only has to emit the tag plus a one-line acknowledgement.
func BuildCruiseConfirmPrompt(freeMode bool) string {
	var b strings.Builder
	b.WriteString("\n\n[自动巡航·完成确认] 当前没有新的用户消息，请不要向用户提问，也不要等待用户确认。\n")
	b.WriteString("所有不明确之处，请通过自主探索、实验、思考与讨论自行决定。\n")
	b.WriteString("请评估：当前任务是否已经满足了用户最初的要求？\n")
	if freeMode {
		b.WriteString("若仍存在遗留工作没有完成，不要输出 <clawbench-goal-satisfied/>。\n")
		b.WriteString("若确认已完成，输出 <clawbench-goal-satisfied/>；否则继续推进尚未完成的部分。\n")
		b.WriteString("本轮只做表态，不要 @ 任何人。\n")
	} else {
		b.WriteString("若仍存在遗留工作没有完成（未改完的代码、未跑通的验证、未处理的报错、未回答的问题等），请先继续完成它们，不要输出 <clawbench-goal-satisfied/>。\n")
		b.WriteString("若确认已完成：输出 <clawbench-goal-satisfied/>，**并在同一条回复里直接写出最终结论**（汇总各成员观点与你的判断，供用户阅读，不要再提问或点名）。\n")
		b.WriteString("例外：若你上一条回复已经写出了完整的最终结论，本轮只需输出 <clawbench-goal-satisfied/> 并加一句确认（如「结论同上」），**不要重复展开**。\n")
		b.WriteString("若尚未完成，照常点名下一批成员（<clawbench-mention targets=\"…\">）继续推进，不要输出 goal-satisfied。\n")
	}
	return b.String()
}

// ── Single-chat auto-cruise prompts ──────────────────────────────────────
//
// Single chat has no host/relay/@User: its ONLY "hand the floor back to the
// human" point is the reply finishing. Cruise turns that into a confirmation
// round. These two fragments mirror the group ones but are written for a lone
// AI (no members to route to), so they are separate builders rather than a
// reuse of BuildCruiseHostAddendum / BuildCruiseConfirmPrompt.

// BuildCruiseSingleAddendum is appended to a SINGLE-CHAT turn's prompt when the
// session's cruise switch is on. It teaches the lone AI three things it cannot
// infer: the conversation will not return to the user until it confirms
// completion, it must not ask the user questions, and the completion signal is
// <clawbench-goal-satisfied/> — emitted ONLY when the system asks (an
// unsolicited tag does not end the run).
//
// It is prepended to the turn PROMPT (not the system prompt) on purpose: a CLI
// resume does not necessarily re-send the system prompt (see
// ChatRequest.ShouldInjectSystemPrompt), so only the prompt is guaranteed to
// reach the model on every turn. ChatRequest.Prompt is transient — the
// persisted user row is req.Message — so this never leaks into the bubble.
func BuildCruiseSingleAddendum() string {
	var b strings.Builder
	b.WriteString("\n\n[自动巡航模式] 本会话已开启自动巡航：在你（AI）确认任务已完成之前，对话不会交回用户。\n")
	b.WriteString("请持续自主推进用户的要求，直到确实完成，不要为了收工而过早收尾。\n")
	b.WriteString("巡航期间不要向用户提问，也不要等待用户确认——所有不明确之处，请通过自主探索、实验与思考自行决定。\n")
	b.WriteString("当系统向你发起「完成确认」时，若你认为已经满足用户的要求，输出结束信号 <clawbench-goal-satisfied/>，并在同一条回复里写出最终结论；若尚未完成，就继续推进尚未完成的部分，不要输出该信号。\n")
	b.WriteString("不要主动输出 <clawbench-goal-satisfied/>——只有系统向你确认时才判断（提前输出不算数）。\n")
	return b.String()
}

// BuildCruiseSingleConfirmPrompt is the hidden instruction injected at the start
// of every SINGLE-CHAT confirmation round. It is NOT persisted to the chat, only
// prepended to that round's prompt.
//
// It carries the SAME marker as the group confirmation prompt
// ("[自动巡航·完成确认]") on purpose: the e2e mock (cmd/acp-mock's
// cruiseConfirmReply) keys off that marker, so single-chat cruise reuses the
// mock's deterministic confirmation with no mock change.
//
// Like the host variant, the "write the conclusion" branch is conditional: the
// common path is that the AI already wrapped up in its previous reply (that is
// what triggers the confirmation), so restating it would duplicate the block.
func BuildCruiseSingleConfirmPrompt() string {
	var b strings.Builder
	b.WriteString("\n\n[自动巡航·完成确认] 当前没有新的用户消息，请不要向用户提问，也不要等待用户确认。\n")
	b.WriteString("所有不明确之处，请通过自主探索、实验、思考自行决定。\n")
	b.WriteString("请评估：当前任务是否已经满足了用户最初的要求？\n")
	b.WriteString("若仍存在遗留工作没有完成（未改完的代码、未跑通的验证、未处理的报错、未回答的问题等），请先继续完成它们，不要输出 <clawbench-goal-satisfied/>。\n")
	b.WriteString("若确认已完成：输出 <clawbench-goal-satisfied/>，并在同一条回复里直接写出最终结论（供用户阅读，不要再提问）。\n")
	b.WriteString("例外：若你上一条回复已经写出了完整的最终结论，本轮只需输出 <clawbench-goal-satisfied/> 并加一句确认（如「结论同上」），不要重复展开。\n")
	b.WriteString("若尚未完成：继续推进尚未完成的部分，不要输出 goal-satisfied。\n")
	return b.String()
}
