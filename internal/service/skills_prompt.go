package service

import (
	"clawbench/internal/skill"
)

// skills_prompt.go holds the single place the cross-agent skill table is
// appended to an agent's system prompt.
//
// There are two producers of ai.ChatRequest.SystemPrompt and BOTH must call
// AppendSkillsSection: service/chat_request.go (direct sends, queue drain,
// push) and service/scheduler.go (scheduled tasks, which build the request
// directly instead of going through BuildChatRequest). Keeping the append in
// one helper is what makes those two paths unable to drift.
//
// The section is NOT baked into model.Agent.RuntimeSystemPrompt: that value is
// composed once at load time, so a skill installed afterwards (or a git sync)
// would never show up until a restart.

// AppendSkillsSection appends the deduplicated skill table for agentID to a
// system prompt. Returns the input unchanged when there is nothing to inject.
//
// projectPath scopes the table to the session's project: its
// <projectPath>/.agents/skills entries are included (at project priority) and
// no other project's are. An empty projectPath yields the global table only.
//
// This is safe to call on every turn; the registry caches the per-agent (and
// per-project) result and only rescans on an explicit Invalidate.
func AppendSkillsSection(systemPrompt, agentID, projectPath string) string {
	if agentID == "" {
		return systemPrompt
	}
	section := skill.BuildSystemPromptSection(skill.Global().InjectedForProject(agentID, projectPath))
	if section == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return section
	}
	return systemPrompt + "\n\n" + section
}
