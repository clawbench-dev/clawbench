package model

import (
	"strings"
	"testing"
)

// CodeBuddy's own account token is a provider row with NO endpoint: the request
// goes to CodeBuddy's own service, so the config-directory file (which needs a
// URL per model) is skipped and the credential plus the fixed cloudhosted switch
// are injected through the environment.

func credentialOnlyProvider() *Provider {
	return &Provider{
		ID: 9, Name: "CodeBuddy Token", Protocol: ProviderProtocolAnthropic,
		APIKey: "ck-1", AuthMode: ProviderAuthAuto,
	}
}

func TestProviderOwnServiceMode(t *testing.T) {
	p := credentialOnlyProvider()
	if !ProviderOwnServiceMode("codebuddy", p) {
		t.Fatal("a no-endpoint provider must be credential-only on codebuddy")
	}
	if ProviderOwnServiceMode("claude", p) {
		t.Fatal("claude has no credential-only mode")
	}
	withEndpoint := *p
	withEndpoint.BaseURL = "https://gw.example"
	if ProviderOwnServiceMode("codebuddy", &withEndpoint) {
		t.Fatal("a provider with an endpoint is a normal binding, not credential-only")
	}
}

// The protocol is irrelevant in this mode, so a key-only provider with any
// protocol must be bindable — otherwise the user could not bind a provider whose
// protocol they never chose.
func TestProviderEnvSupportedProvider_CredentialOnlyIgnoresProtocol(t *testing.T) {
	p := credentialOnlyProvider() // anthropic, while codebuddy speaks openai
	if !ProviderEnvSupportedProvider("codebuddy", p) {
		t.Fatal("a credential-only provider must be accepted whatever its protocol")
	}
}

func TestBuildProviderEnv_CredentialOnly(t *testing.T) {
	env, err := BuildProviderEnv("codebuddy", credentialOnlyProvider(), "")
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
func TestBuildProviderEnvForAgent_CredentialOnlySkipsConfigDir(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	prev := ConfigDirEnvForAgent
	ConfigDirEnvForAgent = nil
	t.Cleanup(func() { ConfigDirEnvForAgent = prev })

	p := credentialOnlyProvider()
	ReplaceProviders([]*Provider{p})
	agent := &Agent{ID: "cb", Backend: "codebuddy", ProviderID: &p.ID}
	ReplaceAgents(map[string]*Agent{agent.ID: agent}, []*Agent{agent})

	env, err := ProviderEnvForAgent(agent.ID, "codebuddy")
	if err != nil {
		t.Fatalf("credential-only must bypass the config-dir writer: %v", err)
	}
	if !strings.Contains(strings.Join(env, ","), "CODEBUDDY_API_KEY=ck-1") {
		t.Fatalf("env = %v", env)
	}
}

// A credential-only provider needs no curated models: the backend's own service
// publishes them. The config-dir model guard must not reject the binding.
func TestProviderConfigDirModelsError_CredentialOnly(t *testing.T) {
	if err := ProviderConfigDirModelsError("codebuddy", credentialOnlyProvider()); err != nil {
		t.Fatalf("a credential-only provider must not require models: %v", err)
	}
	withEndpoint := credentialOnlyProvider()
	withEndpoint.BaseURL = "https://gw.example"
	if err := ProviderConfigDirModelsError("codebuddy", withEndpoint); err == nil {
		t.Fatal("a normal config-dir binding still requires at least one model")
	}
}

// The environment fingerprint includes the provider, so switching to the token
// provider (or rotating it) respawns a live ACP connection.
func TestProviderEnvVersion_CredentialOnly(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	p := credentialOnlyProvider()
	ReplaceProviders([]*Provider{p})
	agent := &Agent{ID: "cb", Backend: "codebuddy", ProviderID: &p.ID}
	ReplaceAgents(map[string]*Agent{agent.ID: agent}, []*Agent{agent})

	first := ProviderEnvVersion(agent.ID)
	if first == "" {
		t.Fatal("a credential-only binding must produce a fingerprint")
	}
	rotated := *p
	rotated.APIKey = "ck-2"
	ReplaceProviders([]*Provider{&rotated})
	if again := ProviderEnvVersion(agent.ID); again == first {
		t.Fatal("rotating the token must change the fingerprint")
	}
}
