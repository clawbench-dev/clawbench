package model

import (
	"strings"
	"testing"
)

func providerForTest(protocol, baseURL, key, auth string) *Provider {
	p := Provider{ID: 1, Name: "Test", Protocol: protocol, BaseURL: baseURL, APIKey: key, AuthMode: auth}.Normalized()
	return &p
}

func TestBuildProviderEnv_AnthropicAuto(t *testing.T) {
	env, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolAnthropic, "https://api.minimax.io/anthropic", "sk-1", ProviderAuthAuto))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"ANTHROPIC_API_KEY=sk-1", "ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic"}
	if strings.Join(env, ",") != strings.Join(want, ",") {
		t.Fatalf("env = %v, want %v", env, want)
	}
}

func TestBuildProviderEnv_AnthropicBearer(t *testing.T) {
	env, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolAnthropic, "https://gw.example", "tok", ProviderAuthBearer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN=tok") {
		t.Fatalf("bearer mode must use the auth-token variable, got %v", env)
	}
	if strings.Contains(joined, "ANTHROPIC_API_KEY") {
		t.Fatalf("bearer mode must not set the api-key variable, got %v", env)
	}
}

func TestBuildProviderEnv_RejectsUnsupportedProtocol(t *testing.T) {
	// claude has no OpenAI path: silently doing nothing would look applied.
	if _, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthAuto)); err == nil {
		t.Fatal("expected an error for an unsupported protocol")
	}
}

func TestBuildProviderEnv_RejectsUnsupportedBackend(t *testing.T) {
	if _, err := BuildProviderEnv("copilot", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthAuto)); err == nil {
		t.Fatal("expected an error for a backend that cannot take a provider")
	}
	if _, err := BuildProviderEnv("nonexistent", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthAuto)); err == nil {
		t.Fatal("expected an error for an unknown backend")
	}
}

func TestBuildProviderEnv_KeyOnlyBackend(t *testing.T) {
	// cursor accepts a credential but no endpoint override.
	env, err := BuildProviderEnv("cursor", providerForTest(ProviderProtocolAnthropic, "", "cursor-key", ProviderAuthAuto))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "CURSOR_API_KEY=cursor-key" {
		t.Fatalf("env = %v, want CURSOR_API_KEY only", env)
	}
}

func TestBuildProviderEnv_NoBaseURLKeepsProtocolVariable(t *testing.T) {
	// An official-API key with no base URL must still reach the CLI.
	env, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolAnthropic, "", "sk-official", ProviderAuthAuto))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "ANTHROPIC_API_KEY=sk-official" {
		t.Fatalf("env = %v, want only the key", env)
	}
}

func TestBuildProviderEnv_BearerUnsupportedIsAnError(t *testing.T) {
	// opencode has no ANTHROPIC_AUTH_TOKEN slot in this table; asking for
	// Bearer must fail rather than send the key as x-api-key.
	if _, err := BuildProviderEnv("opencode", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthBearer)); err == nil {
		t.Fatal("expected an error when the backend cannot express Bearer auth")
	}
}

func TestBuildProviderEnv_ConfigOnlyCarriesSecret(t *testing.T) {
	env, err := BuildProviderEnv("codex", providerForTest(ProviderProtocolOpenAI, "https://api.deepseek.com/v1", "sk-ds", ProviderAuthAuto))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "CLAWBENCH_PROVIDER_API_KEY=sk-ds" {
		t.Fatalf("env = %v, want the config-referenced secret only", env)
	}
}

func TestBuildProviderEnv_NilProvider(t *testing.T) {
	env, err := BuildProviderEnv("claude", nil)
	if err != nil || env != nil {
		t.Fatalf("nil provider must be a no-op, got %v / %v", env, err)
	}
}

func TestBuildProviderEnv_Gemini(t *testing.T) {
	env, err := BuildProviderEnv("antigravity", providerForTest(ProviderProtocolGemini, "https://gw.example", "g-key", ProviderAuthAuto))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "GOOGLE_GEMINI_BASE_URL=https://gw.example") ||
		!strings.Contains(joined, "GEMINI_API_KEY=g-key") {
		t.Fatalf("gemini env = %v", env)
	}
}

func TestProviderEnvSupported(t *testing.T) {
	cases := []struct {
		backend  string
		protocol string
		want     bool
	}{
		{"claude", ProviderProtocolAnthropic, true},
		{"claude", ProviderProtocolOpenAI, false},
		{"opencode", ProviderProtocolOpenAI, true},
		{"antigravity", ProviderProtocolGemini, true},
		{"cursor", ProviderProtocolAnthropic, true}, // key-only accepts any protocol
		{"copilot", ProviderProtocolOpenAI, false},
		{"copilot", "", false},
		{"codex", "", true},
	}
	for _, tc := range cases {
		if got := ProviderEnvSupported(tc.backend, tc.protocol); got != tc.want {
			t.Errorf("ProviderEnvSupported(%q,%q) = %v, want %v", tc.backend, tc.protocol, got, tc.want)
		}
	}
}

// The protocol-only check cannot see the endpoint, so for a key-only backend it
// answered "supported" for a provider that carries one — the binding was
// accepted and then every turn failed inside BuildProviderEnv.
func TestProviderEnvSupportedProvider(t *testing.T) {
	keyOnlyNoEndpoint := &Provider{Name: "k", Protocol: ProviderProtocolOpenAI, APIKey: "k"}
	keyOnlyWithEndpoint := &Provider{Name: "k", Protocol: ProviderProtocolOpenAI, APIKey: "k", BaseURL: "https://x/v1"}
	anthropic := &Provider{Name: "a", Protocol: ProviderProtocolAnthropic, APIKey: "k", BaseURL: "https://x"}
	openai := &Provider{Name: "o", Protocol: ProviderProtocolOpenAI, APIKey: "k", BaseURL: "https://x/v1"}

	cases := []struct {
		backend string
		p       *Provider
		want    bool
	}{
		{"cursor", keyOnlyNoEndpoint, true},
		{"cursor", keyOnlyWithEndpoint, false},
		{"commandcode", keyOnlyNoEndpoint, true},
		{"commandcode", keyOnlyWithEndpoint, false},
		{"claude", anthropic, true},
		{"claude", openai, false},
		{"copilot", openai, false},
		{"codex", openai, true},
		{"cursor", nil, false},
		{"nope", openai, false},
	}
	for _, tc := range cases {
		if got := ProviderEnvSupportedProvider(tc.backend, tc.p); got != tc.want {
			t.Errorf("ProviderEnvSupportedProvider(%q,%v) = %v, want %v", tc.backend, tc.p, got, tc.want)
		}
	}
}

func TestBackendIsKeyOnly(t *testing.T) {
	if !BackendIsKeyOnly("cursor") || !BackendIsKeyOnly("commandcode") {
		t.Fatal("cursor and commandcode take only a credential")
	}
	if BackendIsKeyOnly("claude") || BackendIsKeyOnly("codex") || BackendIsKeyOnly("copilot") {
		t.Fatal("env/config backends are not key-only")
	}
}

func TestRedactProviderEnv(t *testing.T) {
	got := RedactProviderEnv([]string{"A=secret", "MALFORMED"})
	if got[0] != "A=***" || got[1] != "MALFORMED" {
		t.Fatalf("redacted = %v", got)
	}
}

func TestProviderEnvVersion_ChangesWithProvider(t *testing.T) {
	ReplaceProviders([]*Provider{{ID: 1, Name: "A", Protocol: ProviderProtocolAnthropic, BaseURL: "https://a", APIKey: "k1", AuthMode: ProviderAuthAuto}})
	agentID := "provider-version-agent"
	pid := int64(1)
	ReplaceAgents(map[string]*Agent{agentID: {ID: agentID, Backend: "claude", ProviderID: &pid}}, []*Agent{{ID: agentID, Backend: "claude", ProviderID: &pid}})
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})

	first := ProviderEnvVersion(agentID)
	if first == "" {
		t.Fatal("expected a fingerprint")
	}
	ReplaceProviders([]*Provider{{ID: 1, Name: "A", Protocol: ProviderProtocolAnthropic, BaseURL: "https://a", APIKey: "k2", AuthMode: ProviderAuthAuto}})
	if ProviderEnvVersion(agentID) == first {
		t.Fatal("editing the key must change the fingerprint")
	}
	if ProviderEnvVersion("unknown-agent") != "" {
		t.Fatal("an agent without a provider must have an empty fingerprint")
	}
}
