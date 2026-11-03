package ai

import (
	"fmt"

	"clawbench/internal/model"
)

// providerEnvForRequest renders the provider environment for a chat request.
//
// The backend id is taken from the live agent when possible: an agent's
// `command` may point at a wrapper, but the provider variables are a property
// of the backend the agent declares (which is also what the settings UI
// validates against). fallback is used for requests with no resolvable agent
// (scheduled runs before the agent set is loaded, tests).
func providerEnvForRequest(req ChatRequest, fallback string) ([]string, error) {
	backend := fallback
	if agent := model.GetAgent(req.AgentID); agent != nil && agent.Backend != "" {
		backend = agent.Backend
	}
	env, err := model.ProviderEnvForAgent(req.AgentID, backend)
	if err != nil {
		return nil, fmt.Errorf("%s: model provider: %w", backend, err)
	}
	return env, nil
}

// providerEnvForAgentID is the ACP counterpart: ACP connections hold an *Agent
// snapshot from creation time, so the caller must pass the id and let this
// helper re-resolve both the agent (binding may have changed) and the provider.
func providerEnvForAgentID(agentID, fallbackBackend string) ([]string, error) {
	backend := fallbackBackend
	if agent := model.GetAgent(agentID); agent != nil && agent.Backend != "" {
		backend = agent.Backend
	}
	return model.ProviderEnvForAgent(agentID, backend)
}
