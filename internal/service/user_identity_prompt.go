package service

import (
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// user_identity_prompt.go injects the human user's identity into an agent's
// system prompt: the nickname agents should use, plus the free-form profile the
// user wrote about themselves (role, expertise, preferences).
//
// Both values live in the top-level `user` config section and are read at
// REQUEST time (not baked into model.Agent.RuntimeSystemPrompt, which is
// composed once at load time) so editing them takes effect on the next turn
// without a restart.
//
// Called from every system-prompt producer:
//   - service/chat_request.go (direct sends, queue drain, group member turns —
//     group members go through BuildChatRequest too)
//   - service/scheduler.go (scheduled tasks bypass BuildChatRequest)
//
// The section is appended to the SYSTEM PROMPT rather than the prompt. That is
// enough for the same reason the skills table is: an ACP/CLI resume may not
// re-send the system prompt, but the agent's own session already carries it, and
// compaction re-injection (ChatRequest.Compacted) restores it when the agent
// loses its context. Riding every prompt would pay the token cost on every turn
// for information that only changes when the user edits it.
const userIdentityHeader = "## About the User"

// BuildUserIdentitySection returns the system-prompt section, or "" when there
// is nothing worth injecting.
//
// The default nickname ("User") alone is deliberately NOT injected: it is a
// placeholder, not information, and would put a meaningless line in every
// prompt of every install that never configured one.
func BuildUserIdentitySection() string {
	nickname := strings.TrimSpace(model.UserNickname)
	profile := strings.TrimSpace(model.UserProfile)

	hasName := nickname != "" && nickname != model.DefaultUserNickname
	if !hasName && profile == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(userIdentityHeader)
	b.WriteString("\n\n")
	if hasName {
		b.WriteString("The user's name is ")
		b.WriteString(nickname)
		b.WriteString(". Use it when addressing them.\n\n")
	}
	if profile != "" {
		b.WriteString("The user describes themselves as follows (their own words):\n\n")
		b.WriteString(profile)
		b.WriteString("\n\n")
	}
	b.WriteString("Use this to tailor your answers to this person — their role, expertise and preferences. Do not repeat this section back to them.")
	return b.String()
}

// AppendUserIdentitySection appends the identity section to a system prompt.
// It is a no-op (returns the prompt unchanged) when there is nothing to inject,
// so callers can wire it unconditionally.
func AppendUserIdentitySection(systemPrompt string) string {
	section := BuildUserIdentitySection()
	if section == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return section
	}
	return systemPrompt + "\n\n" + section
}

// EnsureUserIdentityInPrompt prepends the identity section to req.Prompt when
// the request will NOT carry it in the system prompt.
//
// A resumed session does not re-send the system prompt (see
// ai.ChatRequest.ShouldInjectSystemPrompt), so on those turns the identity must
// ride the prompt — otherwise a profile edited mid-session would not reach the
// agent until a compaction. The two paths are mutually exclusive, so the agent
// sees exactly one copy.
//
// Slash commands are exempt: prepending anything hides the leading "/" from the
// ACP command dispatcher, which would send the command to the LLM instead of
// its executor. A command turn is not a model turn, so nothing is lost.
//
// Callers that change Resume/AssistantMessageCount/Compacted AFTER
// BuildChatRequest must call this again (applyMemberResumeOverrides does).
// Idempotent: a prompt that already starts with the section is left alone.
func EnsureUserIdentityInPrompt(req *ai.ChatRequest) {
	if req == nil || req.ShouldInjectSystemPrompt() || ai.IsACPSlashCommand(req.Prompt) {
		return
	}
	section := BuildUserIdentitySection()
	if section == "" || strings.HasPrefix(req.Prompt, section) {
		return
	}
	req.Prompt = section + "\n\n" + req.Prompt
}
