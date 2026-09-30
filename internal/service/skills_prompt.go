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
// This is safe to call on every turn; the registry caches the per-agent
// result and only rescans on an explicit Invalidate.
func AppendSkillsSection(systemPrompt, agentID string) string {
	if agentID == "" {
		return systemPrompt
	}
	section := skill.BuildSystemPromptSection(skill.Global().InjectedFor(agentID))
	if section == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return section
	}
	return systemPrompt + "\n\n" + section
}
