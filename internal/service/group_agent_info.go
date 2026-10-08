package service

import "clawbench/internal/model"

// group_agent_info.go resolves the descriptive metadata a group member
// contributes to the host prompt and the injection context (design §5.2 /
// decision #41). Only the agent's short Specialty is exposed — deliberately NOT
// its default model or its full runtime system prompt: the host needs enough to
// route well, not a second copy of every member's instructions.

// GetAgentSpecialty returns an agent's short specialty description, or "" when
// the agent is unknown or has none. Callers omit the parenthetical entirely on
// an empty result so a description-less member does not render as "Name（）".
func GetAgentSpecialty(agentID string) string {
	if agentID == "" {
		return ""
	}
	agent := model.GetAgent(agentID)
	if agent == nil {
		return ""
	}
	return agent.Specialty
}
