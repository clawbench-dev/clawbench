package ai

import (
	"encoding/json"
	"fmt"
	"strings"

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

// OwnServiceSettingsArg renders the `--settings` payload that points a CLI at
// its own service's customer-defined address, or "" when the agent's provider
// carries no such address.
//
// Only backends that declare ProviderEnvSpec.OwnServiceEndpoint (CodeBuddy's
// dedicated / self-hosted editions) get here, and they read the address from
// settings.json instead of the environment. `--settings` is the documented
// highest-priority per-process override, so nothing is written into the user's
// config directory. The key is `enterpriseEndpoint`, the field the CLI prefers
// for those two editions; for any other edition the address is not needed.
//
// Known limit: a build whose product capabilities fix the endpoint
// (customization.json) ignores settings overrides — the address then has no
// effect, which the spawn log shows as the CLI still talking to its default
// host.
func OwnServiceSettingsArg(agentID string) string {
	p := model.AgentProvider(agentID)
	if p == nil || p.Protocol != model.ProviderProtocolPrivate {
		return ""
	}
	url := strings.TrimSpace(p.BaseURL)
	if url == "" {
		return ""
	}
	payload, err := json.Marshal(map[string]string{"enterpriseEndpoint": url})
	if err != nil {
		return ""
	}
	return string(payload)
}
