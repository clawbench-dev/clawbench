package service

import "strings"

// group_prompt.go holds the system-prompt fragments injected into a group
// chat's host agent so it emits parseable routing decisions. See
// docs/plans/2026-10-04-ai-group-chat-design.md §5.3.

// BuildHostSystemPrompt returns the instruction appended to the host agent's
// system prompt so it emits a parseable routing decision. members are the
// display names the host may address.
func BuildHostSystemPrompt(members []string) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 你是本次多智能体讨论的主持人。你的职责是控场，而不是代替成员回答问题。\n")
	b.WriteString("每次发言必须包含一个路由标签，指定接下来该谁发言：\n")
	b.WriteString("  <clawbench-speaker>成员名</clawbench-speaker> 给该成员的指令\n")
	b.WriteString("可一次点名多个成员（逗号分隔），他们将按顺序依次发言：\n")
	b.WriteString("  <clawbench-speaker>A,B</clawbench-speaker> 请分别表态\n")
	b.WriteString("当讨论已充分、可以收敛时，输出结束标签：\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("在结束标签之后，必须再写一段简短的讨论结论（最终汇总），供用户阅读。\n")
	b.WriteString("可选的成员名：")
	b.WriteString(strings.Join(members, "、"))
	b.WriteString("。\n")
	return b.String()
}

// BuildHostSummaryPrompt is the prompt variant for the max-rounds fallback: the
// loop ran out of rounds before the host chose to end, so we run the host once
// more purely to produce the final summary. It MUST NOT ask for a routing tag —
// the orchestrator ignores any tag in this turn to avoid re-entering the loop.
func BuildHostSummaryPrompt(members []string) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 讨论轮数已达上限，现在只做收尾。\n")
	b.WriteString("请直接写一段简短的讨论结论（最终汇总），总结各成员观点与你的判断，供用户阅读。\n")
	b.WriteString("不要再输出任何路由标签或结束标签。\n")
	b.WriteString("参与成员：")
	b.WriteString(strings.Join(members, "、"))
	b.WriteString("。\n")
	return b.String()
}
