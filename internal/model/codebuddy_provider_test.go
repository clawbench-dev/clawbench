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
	// The protocol is authoritative even if the row also carries an address:
	// only the enterprise editions may do that (validation rejects the others),
	// and the runtime must not fall back to the config file when they do.
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

	// The edition becomes an environment variable too, so switching it has to
	// respawn rather than wait for a restart.
	editioned := *p
	editioned.Deployment = ProviderDeploymentInternal
	ReplaceProviders([]*Provider{&editioned})
	if again := ProviderEnvVersion(agent.ID); again == first {
		t.Fatal("changing the deployment must change the fingerprint")
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

// An enterprise edition is selected through the deployment switch, which
// overrides the hosted default in OwnServiceExtra. Without this the CLI would
// still assume the hosted service and reject an enterprise token.
func TestBuildProviderEnv_OwnServiceDeployment(t *testing.T) {
	p := ownServiceProvider()
	p.Deployment = ProviderDeploymentSelfhosted
	p.BaseURL = "https://cb.example.com"

	env, err := BuildProviderEnv("codebuddy", p, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "CODEBUDDY_INTERNET_ENVIRONMENT=selfhosted") {
		t.Fatalf("env = %v, want the self-hosted switch", env)
	}
	// The address is not an environment variable: it must not leak into the
	// env block (the spawn path passes it as --settings instead).
	if strings.Contains(joined, "cb.example.com") {
		t.Fatalf("env = %v, must not carry the service address", env)
	}

	// Empty deployment keeps the documented default.
	env, err = BuildProviderEnv("codebuddy", ownServiceProvider(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.Join(env, ","), "CODEBUDDY_INTERNET_ENVIRONMENT=cloudhosted") {
		t.Fatalf("env = %v, want the hosted default", env)
	}
}

// An own-service provider with an address is only usable where the backend can
// hand that address to its CLI. A backend that cannot must refuse the row
// rather than silently sending every request to its default host.
func TestProviderEnvSupportedProvider_OwnServiceAddress(t *testing.T) {
	t.Cleanup(func() {
		providerEnvMu.Lock()
		delete(providerEnvRegistry, "fake-own-service")
		providerEnvMu.Unlock()
	})
	RegisterProviderEnvSpec("fake-own-service", ProviderEnvSpec{
		OwnServiceEnv: "FAKE_TOKEN",
	})

	p := ownServiceProvider()
	p.BaseURL = "https://cb.example.com"
	if ProviderEnvSupportedProvider("fake-own-service", p) {
		t.Fatal("a backend without an address slot must reject a private provider carrying one")
	}
	if _, err := BuildProviderEnv("fake-own-service", p, ""); err == nil {
		t.Fatal("BuildProviderEnv must refuse the address instead of dropping it")
	}
	// Without an address the same backend accepts the credential.
	bare := ownServiceProvider()
	if !ProviderEnvSupportedProvider("fake-own-service", bare) {
		t.Fatal("a bare credential must still bind")
	}
}

// Four backends share the private protocol, so a credential belongs to exactly
// one of them. Binding a Cursor key to a CodeBuddy agent would be accepted by
// the protocol check and then fail on the first turn.
func TestProviderEnvSupportedProvider_OwnServiceOwner(t *testing.T) {
	p := &Provider{ID: 1, Name: "Cursor key", Protocol: ProviderProtocolPrivate, APIKey: "k", Backend: "cursor"}
	if ProviderEnvSupportedProvider("codebuddy", p) {
		t.Fatal("a cursor credential must not bind to codebuddy")
	}
	if !ProviderEnvSupportedProvider("cursor", p) {
		t.Fatal("it must bind to its own backend")
	}
	if _, err := BuildProviderEnv("codebuddy", p, ""); err == nil {
		t.Fatal("the env builder must refuse a foreign credential instead of injecting it")
	}
}
