package codebuddy

import (
	"os/exec"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

func TestCodebuddyPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	if entry == nil {
		t.Fatal("codebuddy backend factory not registered")
	}
}

func TestCodebuddyPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "codebuddy" {
		t.Errorf("expected backend name 'codebuddy', got %q", backend.Name())
	}
}

func TestCodebuddyPlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	backend := entry.NewBackend()
	clib, ok := backend.(*ai.CLIBackend)
	if !ok {
		t.Fatal("expected *CLIBackend")
	}

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "test-session",
		WorkDir:   "/tmp/test",
	}
	args := clib.BuildArgsFn(req)

	// Should have --output-format stream-json
	hasStreamJSON := false
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "stream-json" {
			hasStreamJSON = true
		}
	}
	if !hasStreamJSON {
		t.Error("expected --output-format stream-json in args")
	}

	// Should NOT have --verbose
	for _, a := range args {
		if a == "--verbose" {
			t.Error("--verbose should NOT be in Codebuddy stream args")
		}
	}
}

func TestCodebuddyPlugin_FilterLine(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// Test BOM removal
	line := "\xEF\xBB\xBF{\"type\":\"assistant\"}"
	filtered, ok := clib.FilterLineFn(line)
	if !ok {
		t.Error("expected BOM-prefixed line to be accepted")
	}
	if strings.HasPrefix(filtered, "\xEF\xBB\xBF") {
		t.Error("BOM should be stripped")
	}

	// Test empty line
	_, ok = clib.FilterLineFn("")
	if ok {
		t.Error("empty line should be rejected")
	}
}

func TestCodebuddyPlugin_PreStart(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{Prompt: "hello"}
	cmd := exec.Command("echo")
	clib.PreStartFn(cmd, req)

	if cmd.Stdin == nil {
		t.Error("expected stdin to be set for Codebuddy preStart")
	}
}

func TestCodebuddyPlugin_DiscoverModelsFunc(t *testing.T) {
	models := DiscoverCodebuddyModels()
	// In CI/test environment, codebuddy CLI may not be installed, so nil is ok.
	// We just verify the function doesn't panic.
	_ = models
}

// An enterprise edition of CodeBuddy's own service carries its address in the
// provider row, not in an environment variable, so the CLI path must pass it as
// --settings exactly like the ACP path does.
func TestBuildCodebuddyStreamArgs_OwnServiceSettings(t *testing.T) {
	model.ReplaceProviders([]*model.Provider{{
		ID: 9, Name: "CB", Protocol: model.ProviderProtocolPrivate, APIKey: "ck-1",
		Deployment: model.ProviderDeploymentSelfhosted, BaseURL: "https://cb.example.com",
	}})
	providerID := int64(9)
	agent := &model.Agent{ID: "cb", Name: "cb", Backend: "codebuddy", ProviderID: &providerID}
	model.ReplaceAgents(map[string]*model.Agent{"cb": agent}, []*model.Agent{agent})
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})

	args := buildCodebuddyStreamArgs(ai.ChatRequest{AgentID: "cb"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--settings") {
		t.Fatalf("args = %v, want a --settings override", args)
	}
	if !strings.Contains(joined, `{"enterpriseEndpoint":"https://cb.example.com"}`) {
		t.Fatalf("args = %v, want the own-service address in the payload", args)
	}

	// A plain (non-own-service) request must not grow a --settings argument.
	args = buildCodebuddyStreamArgs(ai.ChatRequest{})
	if strings.Contains(strings.Join(args, " "), "--settings") {
		t.Fatalf("args = %v, want no --settings without an own-service provider", args)
	}
}
