package model

import (
	"strings"
	"testing"
)

// CodeBuddy's own account token is a provider row with the PRIVATE protocol: the
// request goes to CodeBuddy's own service, so the config-directory file (which
// needs a URL per model) is skipped and the credential plus the fixed cloudhosted
// switch are injected through the environment.

func ownServiceProvider() *Provider {
	return &Provider{
		ID: 9, Name: "CodeBuddy Token", Protocol: ProviderProtocolPrivate,
		APIKey: "ck-1", AuthMode: ProviderAuthAuto,
	}
}

func TestProviderOwnServiceMode(t *testing.T) {
	p := ownServiceProvider()
	if !ProviderOwnServiceMode("codebuddy", p) {
		t.Fatal("the private protocol must be own-service on codebuddy")
	}
	if ProviderOwnServiceMode("claude", p) {
		t.Fatal("claude has no own-service mode")
	}
	normal := *p
	normal.Protocol = ProviderProtocolOpenAI
	normal.BaseURL = "https://gw.example"
	if ProviderOwnServiceMode("codebuddy", &normal) {
		t.Fatal("a normal protocol is a custom-endpoint binding, not own-service")
	}
	// The protocol is authoritative even if a hand-edited row also carries a
	// URL: validation rejects that state, and ignoring the URL here keeps the
	// runtime from silently falling back to a config file.
	withURL := *p
	withURL.BaseURL = "https://ignored.example"
	if !ProviderOwnServiceMode("codebuddy", &withURL) {
		t.Fatal("the private protocol stays own-service regardless of a stray URL")
	}
}

// Only backends that declare their own official service may take the private
// protocol; everyone else must not offer it in the picker.
func TestProviderEnvSupportedProvider_PrivateProtocol(t *testing.T) {
	p := ownServiceProvider()
	if !ProviderEnvSupportedProvider("codebuddy", p) {
		t.Fatal("codebuddy must accept the private protocol")
	}
	if ProviderEnvSupportedProvider("claude", p) {
		t.Fatal("claude must not accept the private protocol")
	}
}

func TestBuildProviderEnv_OwnService(t *testing.T) {
	env, err := BuildProviderEnv("codebuddy", ownServiceProvider(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "CODEBUDDY_API_KEY=ck-1") {
		t.Fatalf("env = %v, want the account token", env)
	}
	if !strings.Contains(joined, "CODEBUDDY_INTERNET_ENVIRONMENT=cloudhosted") {
		t.Fatalf("env = %v, want the cloudhosted switch", env)
	}
}

// The binding must not go through the config-directory writer: it would demand a
// model list and write a models.json with an empty URL. No writer is registered
// here, so an accidental fall-through would fail loudly.
func TestBuildProviderEnvForAgent_OwnServiceSkipsConfigDir(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	prev := ConfigDirEnvForAgent
	ConfigDirEnvForAgent = nil
	t.Cleanup(func() { ConfigDirEnvForAgent = prev })

	p := ownServiceProvider()
	ReplaceProviders([]*Provider{p})
	agent := &Agent{ID: "cb", Backend: "codebuddy", ProviderID: &p.ID}
	ReplaceAgents(map[string]*Agent{agent.ID: agent}, []*Agent{agent})

	env, err := ProviderEnvForAgent(agent.ID, "codebuddy")
	if err != nil {
		t.Fatalf("own-service must bypass the config-dir writer: %v", err)
	}
	if !strings.Contains(strings.Join(env, ","), "CODEBUDDY_API_KEY=ck-1") {
		t.Fatalf("env = %v", env)
	}
}

// The private protocol needs no curated models: the backend's own service
// publishes them. A normal config-dir binding still does.
func TestProviderConfigDirModelsError_OwnService(t *testing.T) {
	if err := ProviderConfigDirModelsError("codebuddy", ownServiceProvider()); err != nil {
		t.Fatalf("the private protocol must not require models: %v", err)
	}
	normal := ownServiceProvider()
	normal.Protocol = ProviderProtocolOpenAI
	normal.BaseURL = "https://gw.example"
	if err := ProviderConfigDirModelsError("codebuddy", normal); err == nil {
		t.Fatal("a normal config-dir binding still requires at least one model")
	}
}

// The environment fingerprint includes the provider, so switching to the token
// provider (or rotating it) respawns a live ACP connection.
func TestProviderEnvVersion_OwnService(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	p := ownServiceProvider()
	ReplaceProviders([]*Provider{p})
	agent := &Agent{ID: "cb", Backend: "codebuddy", ProviderID: &p.ID}
	ReplaceAgents(map[string]*Agent{agent.ID: agent}, []*Agent{agent})

	first := ProviderEnvVersion(agent.ID)
	if first == "" {
		t.Fatal("an own-service binding must produce a fingerprint")
	}
	rotated := *p
	rotated.APIKey = "ck-2"
	ReplaceProviders([]*Provider{&rotated})
	if again := ProviderEnvVersion(agent.ID); again == first {
		t.Fatal("rotating the token must change the fingerprint")
	}
}

// SupportedProtocols is what the agent settings picker filters on: the private
// protocol must be listed for codebuddy and nowhere else.
func TestSupportedProtocols_PrivateOnlyForOwnService(t *testing.T) {
	if !containsString(SupportedProtocols("codebuddy"), ProviderProtocolPrivate) {
		t.Fatal("codebuddy must advertise the private protocol")
	}
	if containsString(SupportedProtocols("claude"), ProviderProtocolPrivate) {
		t.Fatal("claude must not advertise the private protocol")
	}
}
