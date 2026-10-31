package ai

import (
	"context"
	"os"
	"path/filepath"
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
	setupProviderAgent(t, "agent-claude", "claude", &model.Provider{
		ID: 1, Name: "P", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://gw.example", APIKey: "sk-x", AuthMode: model.ProviderAuthAuto,
	})

	// The fallback (b.BackendName) says codebuddy; the agent says claude. The
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

// TestCLIBackend_InjectsProviderEnvIntoProcess proves the variables actually
// reach the child process (append after os.Environ, last value wins).
func TestCLIBackend_InjectsProviderEnvIntoProcess(t *testing.T) {
	setupProviderAgent(t, "agent-cli", "claude", &model.Provider{
		ID: 3, Name: "P3", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://injected.example", APIKey: "sk-injected", AuthMode: model.ProviderAuthAuto,
	})

	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")
	script := `printf '%s|%s' "$ANTHROPIC_BASE_URL" "$ANTHROPIC_API_KEY" > ` + out

	b := &CLIBackend{
		BackendName: "codebuddy", // deliberately different from the agent's backend
		Cmd:         "/bin/sh",
		BuildArgsFn: func(ChatRequest) []string { return []string{"-c", script} },
		NewParserFn: func() LineParser { return &StreamParser{} },
	}
	ch, err := b.ExecuteStream(context.Background(), ChatRequest{
		AgentID: "agent-cli", WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	for range ch {
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read child env: %v", err)
	}
	if got := string(data); got != "https://injected.example|sk-injected" {
		t.Fatalf("child env = %q, want the injected provider values", got)
	}
}
