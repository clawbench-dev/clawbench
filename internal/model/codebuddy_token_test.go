package model

import (
	"strings"
	"testing"
)

// CodeBuddy's own-token mode: the CLI reads exactly two variables for an account
// token, and the token itself is server-wide config — every agent that turns the
// mode on shares one key.

// withCodeBuddyToken sets the shared config token for one test and restores it.
func withCodeBuddyToken(t *testing.T, key string) {
	t.Helper()
	prev := ConfigInstance.CodeBuddy.APIKey
	ConfigInstance.CodeBuddy.APIKey = key
	t.Cleanup(func() { ConfigInstance.CodeBuddy.APIKey = prev })
}

func TestCodeBuddyTokenEnv(t *testing.T) {
	withCodeBuddyToken(t, "ck-1")

	off := &Agent{ID: "a", Backend: CodeBuddyBackend}
	env, err := CodeBuddyTokenEnv(off)
	if err != nil || env != nil {
		t.Fatalf("mode off must be a no-op, got %v / %v", env, err)
	}

	on := &Agent{ID: "b", Backend: CodeBuddyBackend, CodeBuddyTokenMode: true}
	env, err = CodeBuddyTokenEnv(on)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "CODEBUDDY_API_KEY=ck-1") {
		t.Fatalf("env = %v, want the shared token", env)
	}
	if !strings.Contains(joined, "CODEBUDDY_INTERNET_ENVIRONMENT=cloudhosted") {
		t.Fatalf("env = %v, want the cloudhosted environment", env)
	}

	// The mode belongs to CodeBuddy only.
	other := &Agent{ID: "d", Backend: "claude", CodeBuddyTokenMode: true}
	if env, err := CodeBuddyTokenEnv(other); err != nil || env != nil {
		t.Fatalf("a non-codebuddy agent must be ignored, got %v / %v", env, err)
	}
}

// Mode on with no shared token must fail loudly: falling back to the CLI login
// would silently run the agent on a different account.
func TestCodeBuddyTokenEnv_MissingSharedKey(t *testing.T) {
	withCodeBuddyToken(t, "")
	on := &Agent{ID: "b", Backend: CodeBuddyBackend, CodeBuddyTokenMode: true}
	if _, err := CodeBuddyTokenEnv(on); err == nil {
		t.Fatal("expected an error when the shared token is not configured")
	}
}

func TestProviderEnvForAgent_CodeBuddyTokenMode(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	withCodeBuddyToken(t, "ck-9")

	agent := &Agent{ID: "cb", Backend: CodeBuddyBackend, CodeBuddyTokenMode: true}
	ReplaceProviders(nil)
	ReplaceAgents(map[string]*Agent{agent.ID: agent}, []*Agent{agent})

	env, err := ProviderEnvForAgent(agent.ID, CodeBuddyBackend)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if joined := strings.Join(env, ","); !strings.Contains(joined, "CODEBUDDY_API_KEY=ck-9") {
		t.Fatalf("env = %v, want the shared token", env)
	}

	// A provider and the token mode are exclusive; both set is hand-edited
	// state and must be reported instead of silently picking one.
	pid := int64(7)
	bound := &Agent{ID: "cb-bound", Backend: CodeBuddyBackend, ProviderID: &pid, CodeBuddyTokenMode: true}
	ReplaceProviders([]*Provider{{
		ID: 7, Name: "GW", Protocol: ProviderProtocolOpenAI,
		BaseURL: "https://gw.example", APIKey: "sk-7", AuthMode: ProviderAuthAuto,
	}})
	ReplaceAgents(map[string]*Agent{bound.ID: bound}, []*Agent{bound})
	if _, err := ProviderEnvForAgent(bound.ID, CodeBuddyBackend); err == nil {
		t.Fatal("expected an error when both a provider and token mode are set")
	}
}

func TestProviderEnvVersion_TokenModeFingerprints(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	withCodeBuddyToken(t, "ck-a")

	agent := &Agent{ID: "cb", Backend: CodeBuddyBackend, CodeBuddyTokenMode: true}
	ReplaceProviders(nil)
	ReplaceAgents(map[string]*Agent{agent.ID: agent}, []*Agent{agent})
	first := ProviderEnvVersion(agent.ID)
	if first == "" {
		t.Fatal("token mode must produce a fingerprint, or a live ACP connection would never respawn")
	}

	// Rotating the shared token must respawn the connection too.
	ConfigInstance.CodeBuddy.APIKey = "ck-b"
	if again := ProviderEnvVersion(agent.ID); again == first {
		t.Fatal("replacing the shared token must change the fingerprint")
	}
}
