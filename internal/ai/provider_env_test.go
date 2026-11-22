package ai

import (
	"strings"
	"testing"

	"clawbench/internal/model"
)

func setupProviderAgent(t *testing.T, agentID, backend string, provider *model.Provider) {
	t.Helper()
	model.ReplaceProviders([]*model.Provider{provider})
	pid := provider.ID
	agent := &model.Agent{ID: agentID, Name: agentID, Backend: backend, ProviderID: &pid}
	model.ReplaceAgents(map[string]*model.Agent{agentID: agent}, []*model.Agent{agent})
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})
}

func TestProviderEnvForRequest_UsesAgentBackend(t *testing.T) {
	// zcode, not claude: claude is a config-directory backend now, so its
	// provider goes into settings.json and the only variable injected is
	// CLAUDE_CONFIG_DIR — there is no ANTHROPIC_BASE_URL to assert on. zcode is
	// still an env-only Anthropic backend, which is what this test is about.
	setupProviderAgent(t, "agent-claude", "zcode", &model.Provider{
		ID: 1, Name: "P", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://gw.example", APIKey: "sk-x", AuthMode: model.ProviderAuthAuto,
	})

	// The fallback (b.BackendName) says codebuddy; the agent says zcode. The
	// agent must win, otherwise a custom command wrapper gets the wrong vars.
	env, err := providerEnvForRequest(ChatRequest{AgentID: "agent-claude"}, "codebuddy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=https://gw.example") {
		t.Fatalf("expected ANTHROPIC_* variables, got %v", env)
	}
	if strings.Contains(joined, "CODEBUDDY_") {
		t.Fatalf("must not inject the fallback backend's variables, got %v", env)
	}
}

func TestProviderEnvForRequest_NoProviderIsNoop(t *testing.T) {
	model.ReplaceAgents(map[string]*model.Agent{
		"plain": {ID: "plain", Name: "plain", Backend: "claude"},
	}, []*model.Agent{{ID: "plain", Name: "plain", Backend: "claude"}})
	t.Cleanup(func() { model.ReplaceAgents(nil, nil) })

	env, err := providerEnvForRequest(ChatRequest{AgentID: "plain"}, "claude")
	if err != nil || env != nil {
		t.Fatalf("expected a no-op, got %v / %v", env, err)
	}
}

func TestProviderEnvForRequest_UnsupportedBackendFails(t *testing.T) {
	setupProviderAgent(t, "agent-copilot", "copilot", &model.Provider{
		ID: 2, Name: "P2", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto,
	})
	if _, err := providerEnvForRequest(ChatRequest{AgentID: "agent-copilot"}, "copilot"); err == nil {
		t.Fatal("an unsupported backend must fail loudly instead of starting with the wrong endpoint")
	}
}

// The enterprise editions' service address is not an environment variable: it
// has to reach the CLI as a --settings override. Only an own-service provider
// that carries an address produces one.
func TestOwnServiceSettingsArg(t *testing.T) {
	setupProviderAgent(t, "cb", "codebuddy", &model.Provider{
		ID: 1, Name: "CB", Protocol: model.ProviderProtocolPrivate, APIKey: "ck-1",
		Deployment: model.ProviderDeploymentSelfhosted, BaseURL: "https://cb.example.com",
	})
	if got := OwnServiceSettingsArg("cb"); got != `{"enterpriseEndpoint":"https://cb.example.com"}` {
		t.Fatalf("payload = %q", got)
	}

	// The hosted account has no address, so there is nothing to pass.
	setupProviderAgent(t, "cb2", "codebuddy", &model.Provider{
		ID: 2, Name: "CB2", Protocol: model.ProviderProtocolPrivate, APIKey: "ck-2",
	})
	if got := OwnServiceSettingsArg("cb2"); got != "" {
		t.Fatalf("a hosted account must produce no override, got %q", got)
	}

	// A third-party endpoint goes through CODEBUDDY_BASE_URL instead.
	setupProviderAgent(t, "cb3", "codebuddy", &model.Provider{
		ID: 3, Name: "GW", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example", APIKey: "k", AuthMode: model.ProviderAuthAuto,
	})
	if got := OwnServiceSettingsArg("cb3"); got != "" {
		t.Fatalf("a custom gateway must not be treated as an own-service address, got %q", got)
	}
}
