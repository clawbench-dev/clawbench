package ai

import (
	"context"
	"os"
	"os/exec"
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
// reach the child process, and that they win over the same names already in the
// server's environment (cmd.Env is os.Environ() plus the provider vars, so the
// provider values must come last).
//
// The child is this test binary re-executed, not a shell: Windows has no
// /bin/sh, and `cmd /c echo %VAR%` cannot carry values containing cmd
// metacharacters. A re-executed helper observes the environment exactly as any
// real CLI backend would.
func TestCLIBackend_InjectsProviderEnvIntoProcess(t *testing.T) {
	setupProviderAgent(t, "agent-cli", "claude", &model.Provider{
		ID: 3, Name: "P3", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://injected.example", APIKey: "sk-injected", AuthMode: model.ProviderAuthAuto,
	})

	// Pre-existing values with the same names: without them the test cannot
	// tell "appended last" from "the only value present", and would pass even if
	// the ordering were reversed.
	t.Setenv("ANTHROPIC_BASE_URL", "https://from-parent.example")
	t.Setenv("ANTHROPIC_API_KEY", "sk-from-parent")

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")

	b := &CLIBackend{
		BackendName: "codebuddy", // deliberately different from the agent's backend
		Cmd:         self,
		BuildArgsFn: func(ChatRequest) []string {
			return []string{"-test.run=^TestProviderEnvHelperProcess$"}
		},
		NewParserFn: func() LineParser { return &StreamParser{} },
		PreStartFn: func(cmd *exec.Cmd, _ ChatRequest) {
			// The helper's selector and destination travel in the child's
			// environment: the provider vars are what is under test, and argv
			// would need quoting for a path that may contain spaces.
			cmd.Env = append(cmd.Env, providerEnvHelperEnv+"=1", providerEnvOutEnv+"="+out)
		},
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
		t.Fatalf("child env = %q, want the injected provider values to override the parent's", got)
	}
}

const (
	// providerEnvHelperEnv marks the re-executed child.
	providerEnvHelperEnv = "CLAWBENCH_TEST_PROVIDER_ENV_HELPER"
	// providerEnvOutEnv names the file the child writes its view of the
	// environment to.
	providerEnvOutEnv = "CLAWBENCH_TEST_PROVIDER_ENV_OUT"
)

// TestProviderEnvHelperProcess is not a test: it is the child process
// TestCLIBackend_InjectsProviderEnvIntoProcess re-executes. It exits without
// letting the testing package print anything, so the parent's stream parser
// sees an empty, successful run.
func TestProviderEnvHelperProcess(t *testing.T) {
	if os.Getenv(providerEnvHelperEnv) != "1" {
		t.Skip("helper process for TestCLIBackend_InjectsProviderEnvIntoProcess")
	}
	out := os.Getenv(providerEnvOutEnv)
	if out == "" {
		os.Exit(2)
	}
	body := os.Getenv("ANTHROPIC_BASE_URL") + "|" + os.Getenv("ANTHROPIC_API_KEY")
	if err := os.WriteFile(out, []byte(body), 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
