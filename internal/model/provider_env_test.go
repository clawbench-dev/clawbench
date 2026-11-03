package model

import (
	"sort"
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

// An auth mode the backend cannot express must be refused at bind time, not on
// the first turn. This is the predicate both the handler and the service use,
// so it has to agree with BuildProviderEnv exactly.
func TestProviderAuthModeError_MatchesBuildProviderEnv(t *testing.T) {
	cases := []struct {
		name     string
		backend  string
		protocol string
		auth     string
		wantErr  bool
		wantText string
	}{
		{name: "anthropic bearer is expressible", backend: "claude", protocol: ProviderProtocolAnthropic, auth: ProviderAuthBearer},
		{name: "openai bearer is dead", backend: "opencode", protocol: ProviderProtocolOpenAI, auth: ProviderAuthBearer, wantErr: true, wantText: "Bearer"},
		{name: "deepseek bearer is dead", backend: "deepseek", protocol: ProviderProtocolOpenAI, auth: ProviderAuthBearer, wantErr: true, wantText: "Bearer"},
		{name: "openai x-api-key stays available", backend: "deepseek", protocol: ProviderProtocolOpenAI, auth: ProviderAuthAPIKey},
		{name: "gemini bearer is dead", backend: "antigravity", protocol: ProviderProtocolGemini, auth: ProviderAuthBearer, wantErr: true, wantText: "Bearer"},
		// Key-only and config-file backends take the credential (or the file)
		// and never read an auth mode, so nothing can be wrong there.
		{name: "key-only backend ignores auth mode", backend: "cursor", protocol: ProviderProtocolAnthropic, auth: ProviderAuthBearer},
		{name: "config-file backend ignores auth mode", backend: "codex", protocol: ProviderProtocolOpenAI, auth: ProviderAuthBearer},
		{name: "backend without a provider slot", backend: "copilot", protocol: ProviderProtocolOpenAI, auth: ProviderAuthAuto, wantErr: true, wantText: "openai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ProviderAuthModeError(tc.backend, tc.protocol, tc.auth)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s/%s/%s must be rejected", tc.backend, tc.protocol, tc.auth)
				}
				if !strings.Contains(err.Error(), tc.wantText) {
					t.Fatalf("error %q must name %q", err, tc.wantText)
				}
			} else if err != nil {
				t.Fatalf("%s/%s/%s must be accepted, got %v", tc.backend, tc.protocol, tc.auth, err)
			}

			// The two must never disagree: for a binding the protocol check
			// accepts, a mode the predicate accepts must not blow up in
			// BuildProviderEnv, and vice versa.
			p := providerForTest(tc.protocol, "https://gw.example", "sk-1", tc.auth)
			if !ProviderEnvSupportedProvider(tc.backend, p) {
				return
			}
			_, buildErr := BuildProviderEnv(tc.backend, p)
			if (buildErr != nil) != tc.wantErr {
				t.Fatalf("BuildProviderEnv disagrees with ProviderAuthModeError: build=%v predicate=%v", buildErr, err)
			}
		})
	}
}

// A provider row is not tied to a backend, so create/update can only ask
// whether the protocol+auth pair is dead for every backend. It is for
// bearer on the two protocols whose backends carry no auth-token variable.
func TestProtocolSupportsAuthMode(t *testing.T) {
	cases := []struct {
		protocol string
		auth     string
		want     bool
	}{
		{ProviderProtocolAnthropic, ProviderAuthBearer, true},
		{ProviderProtocolAnthropic, ProviderAuthAuto, true},
		{ProviderProtocolAnthropic, ProviderAuthAPIKey, true},
		{ProviderProtocolOpenAI, ProviderAuthAuto, true},
		{ProviderProtocolOpenAI, ProviderAuthAPIKey, true},
		{ProviderProtocolOpenAI, ProviderAuthBearer, false},
		{ProviderProtocolGemini, ProviderAuthAuto, true},
		{ProviderProtocolGemini, ProviderAuthBearer, false},
	}
	for _, tc := range cases {
		if got := ProtocolSupportsAuthMode(tc.protocol, tc.auth); got != tc.want {
			t.Errorf("ProtocolSupportsAuthMode(%s,%s) = %v, want %v", tc.protocol, tc.auth, got, tc.want)
		}
	}
}

// wire_api is written into codex's config file, which the process reads once at
// startup: a live connection must be respawned when it changes.
func TestProviderEnvVersion_ChangesWithWireAPI(t *testing.T) {
	agentID := "provider-version-wire-agent"
	pid := int64(2)
	ReplaceAgents(map[string]*Agent{agentID: {ID: agentID, Backend: "codex", ProviderID: &pid}}, []*Agent{{ID: agentID, Backend: "codex", ProviderID: &pid}})
	ReplaceProviders([]*Provider{{ID: 2, Name: "W", Protocol: ProviderProtocolOpenAI, BaseURL: "https://w", APIKey: "k", AuthMode: ProviderAuthAuto, WireAPI: ProviderWireResponses}})
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})

	before := ProviderEnvVersion(agentID)
	if before == "" {
		t.Fatal("expected a fingerprint")
	}
	ReplaceProviders([]*Provider{{ID: 2, Name: "W", Protocol: ProviderProtocolOpenAI, BaseURL: "https://w", APIKey: "k", AuthMode: ProviderAuthAuto, WireAPI: ProviderWireChat}})
	if ProviderEnvVersion(agentID) == before {
		t.Fatal("changing wire_api must change the fingerprint")
	}
}

// ProviderEnvBackends feeds the coverage test that keeps the env table and the
// backend registry in sync, so it must list every entry exactly once, sorted.
func TestProviderEnvBackends_SortedAndComplete(t *testing.T) {
	backends := ProviderEnvBackends()
	if len(backends) != len(providerEnvTable) {
		t.Fatalf("backends = %d, table = %d", len(backends), len(providerEnvTable))
	}
	if !sort.StringsAreSorted(backends) {
		t.Fatalf("backends must be sorted: %v", backends)
	}
	for _, id := range backends {
		if !HasProviderEnvEntry(id) {
			t.Errorf("HasProviderEnvEntry(%q) = false for a listed backend", id)
		}
	}
	// An unclassified backend is a real answer ("no provider support"), not an
	// entry, so it must not be reported.
	if HasProviderEnvEntry("not-a-backend") {
		t.Fatal("an unknown backend must not claim a classification")
	}
}

func TestProviderEnvSupported_UnknownInputs(t *testing.T) {
	if ProviderEnvSupported("not-a-backend", ProviderProtocolOpenAI) {
		t.Fatal("an unclassified backend cannot take a provider")
	}
	// A protocol no backend knows is not a protocol anyone supports; the
	// default arm of supportsProtocol is what answers this.
	if ProviderEnvSupported("claude", "cohere") {
		t.Fatal("an unknown protocol must not be reported as supported")
	}
	if ProviderEnvSupported("codex", "cohere") {
		t.Fatal("the config-file protocol list is exhaustive")
	}
}

func TestProviderAuthModeError_UnknownBackend(t *testing.T) {
	err := ProviderAuthModeError("not-a-backend", ProviderProtocolOpenAI, ProviderAuthAuto)
	if err == nil {
		t.Fatal("an unclassified backend must be rejected by name")
	}
	if !strings.Contains(err.Error(), "not-a-backend") {
		t.Fatalf("error %q must name the backend", err)
	}
}

// A provider with no key cannot be applied at all: starting the process with an
// empty credential would look applied while every request 401s.
func TestBuildProviderEnv_RequiresAPIKey(t *testing.T) {
	p := providerForTest(ProviderProtocolAnthropic, "https://gw.example", "", ProviderAuthAuto)
	if _, err := BuildProviderEnv("claude", p); err == nil {
		t.Fatal("expected an error for a provider without a key")
	} else if !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("error %q must say the key is missing", err)
	}
}

// The protocol reaches the env table as a string from a database row; an
// unknown one must fail loudly instead of silently producing no variables.
func TestBuildProviderEnv_UnknownProtocol(t *testing.T) {
	p := providerForTest("cohere", "https://gw.example", "k", ProviderAuthAuto)
	_, err := BuildProviderEnv("claude", p)
	if err == nil {
		t.Fatal("expected an error for an unknown protocol")
	}
	if !strings.Contains(err.Error(), "cohere") {
		t.Fatalf("error %q must name the protocol", err)
	}
}

// Every spawn site uses the one-step resolver; it must agree with
// BuildProviderEnv and be a no-op for an unbound agent.
func TestProviderEnvForAgent(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	pid := int64(5)
	ReplaceProviders([]*Provider{{
		ID: 5, Name: "GW", Protocol: ProviderProtocolAnthropic,
		BaseURL: "https://gw.example", APIKey: "sk-5", AuthMode: ProviderAuthAuto,
	}})
	ReplaceAgents(
		map[string]*Agent{
			"bound":   {ID: "bound", Backend: "claude", ProviderID: &pid},
			"unbound": {ID: "unbound", Backend: "claude"},
		},
		[]*Agent{
			{ID: "bound", Backend: "claude", ProviderID: &pid},
			{ID: "unbound", Backend: "claude"},
		},
	)

	env, err := ProviderEnvForAgent("bound", "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "ANTHROPIC_API_KEY=sk-5") || !strings.Contains(joined, "ANTHROPIC_BASE_URL=https://gw.example") {
		t.Fatalf("env = %v", env)
	}

	env, err = ProviderEnvForAgent("unbound", "claude")
	if err != nil || env != nil {
		t.Fatalf("an unbound agent must be a no-op, got %v / %v", env, err)
	}

	// A binding the backend cannot express is an error, not an empty list.
	// deepseek only speaks the OpenAI wire protocol.
	if _, err := ProviderEnvForAgent("bound", "deepseek"); err == nil {
		t.Fatal("claude's anthropic provider must not be applied to an openai-only backend")
	}
}
