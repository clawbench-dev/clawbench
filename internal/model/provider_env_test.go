package model

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func providerForTest(protocol, baseURL, key, auth string) *Provider {
	p := Provider{ID: 1, Name: "Test", Protocol: protocol, BaseURL: baseURL, APIKey: key, AuthMode: auth}.Normalized()
	return &p
}

func TestBuildProviderEnv_AnthropicAuto(t *testing.T) {
	env, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolAnthropic, "https://api.minimax.io/anthropic", "sk-1", ProviderAuthAuto), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"ANTHROPIC_API_KEY=sk-1", "ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic"}
	if strings.Join(env, ",") != strings.Join(want, ",") {
		t.Fatalf("env = %v, want %v", env, want)
	}
}

func TestBuildProviderEnv_AnthropicBearer(t *testing.T) {
	env, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolAnthropic, "https://gw.example", "tok", ProviderAuthBearer), "")
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
	if _, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthAuto), ""); err == nil {
		t.Fatal("expected an error for an unsupported protocol")
	}
}

func TestBuildProviderEnv_RejectsUnsupportedBackend(t *testing.T) {
	if _, err := BuildProviderEnv("copilot", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthAuto), ""); err == nil {
		t.Fatal("expected an error for a backend that cannot take a provider")
	}
	if _, err := BuildProviderEnv("nonexistent", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthAuto), ""); err == nil {
		t.Fatal("expected an error for an unknown backend")
	}
}

func TestBuildProviderEnv_KeyOnlyBackend(t *testing.T) {
	// cursor accepts a credential but no endpoint override.
	env, err := BuildProviderEnv("cursor", providerForTest(ProviderProtocolAnthropic, "", "cursor-key", ProviderAuthAuto), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "CURSOR_API_KEY=cursor-key" {
		t.Fatalf("env = %v, want CURSOR_API_KEY only", env)
	}
}

func TestBuildProviderEnv_NoBaseURLKeepsProtocolVariable(t *testing.T) {
	// An official-API key with no base URL must still reach the CLI.
	env, err := BuildProviderEnv("claude", providerForTest(ProviderProtocolAnthropic, "", "sk-official", ProviderAuthAuto), "")
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
	if _, err := BuildProviderEnv("opencode", providerForTest(ProviderProtocolOpenAI, "https://x", "k", ProviderAuthBearer), ""); err == nil {
		t.Fatal("expected an error when the backend cannot express Bearer auth")
	}
}

func TestBuildProviderEnv_ConfigOnlyCarriesSecret(t *testing.T) {
	env, err := BuildProviderEnv("codex", providerForTest(ProviderProtocolOpenAI, "https://api.deepseek.com/v1", "sk-ds", ProviderAuthAuto), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "CLAWBENCH_PROVIDER_API_KEY=sk-ds" {
		t.Fatalf("env = %v, want the config-referenced secret only", env)
	}
}

func TestBuildProviderEnv_NilProvider(t *testing.T) {
	env, err := BuildProviderEnv("claude", nil, "")
	if err != nil || env != nil {
		t.Fatalf("nil provider must be a no-op, got %v / %v", env, err)
	}
}

func TestBuildProviderEnv_Gemini(t *testing.T) {
	env, err := BuildProviderEnv("antigravity", providerForTest(ProviderProtocolGemini, "https://gw.example", "g-key", ProviderAuthAuto), "")
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
			_, buildErr := BuildProviderEnv(tc.backend, p, "")
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
	if _, err := BuildProviderEnv("claude", p, ""); err == nil {
		t.Fatal("expected an error for a provider without a key")
	} else if !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("error %q must say the key is missing", err)
	}
}

// The protocol reaches the env table as a string from a database row; an
// unknown one must fail loudly instead of silently producing no variables.
func TestBuildProviderEnv_UnknownProtocol(t *testing.T) {
	p := providerForTest("cohere", "https://gw.example", "k", ProviderAuthAuto)
	_, err := BuildProviderEnv("claude", p, "")
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

// ---------------------------------------------------------------------------
// Inline config content (issue #651)
// ---------------------------------------------------------------------------

// opencode resolves a model id against its own provider catalog before sending
// anything, so a provider redirect alone leaves a catalog-absent id
// unresolvable: the turn fails with "model not found" while the endpoint was
// injected correctly. Declaring the provider's models in the config content is
// what makes them resolvable.
func TestBuildProviderEnv_OpenCodeDeclaresProviderModels(t *testing.T) {
	p := providerForTest(ProviderProtocolOpenAI, "https://gw.example/v1", "sk-1", ProviderAuthAuto)
	p.Models = []string{"glm-5.3-flash", "glm-5.3"}

	env, err := BuildProviderEnv("opencode", p, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := inlineConfigContent(t, env)
	provider := content["provider"].(map[string]any)
	openai, ok := provider["clawbench-openai"].(map[string]any)
	if !ok {
		t.Fatalf("models must be declared under our provider id for this protocol, got %v", provider)
	}
	models := openai["models"].(map[string]any)
	for _, id := range []string{"glm-5.3-flash", "glm-5.3"} {
		spec, ok := models[id].(map[string]any)
		if !ok {
			t.Fatalf("model %q missing from the declared set: %v", id, models)
		}
		// A model without tool_call is advertised but cannot call tools, which
		// silently downgrades every coding turn to plain chat.
		if spec["tool_call"] != true {
			t.Fatalf("model %q must declare tool_call, got %v", id, spec)
		}
	}

	// The secret must never be written into the content: it is referenced by
	// the variable name the spawn already sets, so it cannot leak via a process
	// listing and stays a single source of truth. Only the content entry is
	// inspected — the separate OPENAI_API_KEY entry is supposed to carry it.
	raw := ""
	for _, entry := range env {
		if rest, ok := strings.CutPrefix(entry, "OPENCODE_CONFIG_CONTENT="); ok {
			raw = rest
		}
	}
	if strings.Contains(raw, "sk-1") {
		t.Fatalf("the credential must not be inlined into the config content: %s", raw)
	}
	opts := openai["options"].(map[string]any)
	if opts["apiKey"] != "{env:OPENAI_API_KEY}" || opts["baseURL"] != "{env:OPENAI_BASE_URL}" {
		t.Fatalf("options must reference the injected variables, got %v", opts)
	}
}

// A pinned model outside the provider's curated list must still be declared:
// PATCH accepts it on purpose (a bound provider makes the list advisory), so
// without this the "custom model" escape hatch saves an id the backend refuses
// to resolve — exactly the reported repro.
func TestBuildProviderEnv_OpenCodeDeclaresPinnedModel(t *testing.T) {
	p := providerForTest(ProviderProtocolOpenAI, "https://gw.example/v1", "sk-1", ProviderAuthAuto)
	p.Models = []string{"gw-a"}

	env, err := BuildProviderEnv("opencode", p, "gw-custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	models := inlineConfigContent(t, env)["provider"].(map[string]any)["clawbench-openai"].(map[string]any)["models"].(map[string]any)
	for _, id := range []string{"gw-a", "gw-custom"} {
		if _, ok := models[id]; !ok {
			t.Fatalf("model %q must be declared, got %v", id, models)
		}
	}
	if len(models) != 2 {
		t.Fatalf("declared set = %v, want exactly the curated list plus the pin", models)
	}
}

// A pin that names one of the agent's OWN models must not be declared: the
// backend resolves it without help, and declaring it under our provider would
// advertise "clawbench-openai/opencode/<model>" — an entry whose selection goes
// to an endpoint that never served that model (issue #651 group-chat follow-up).
func TestProviderEnvForAgent_SkipsDeclaringNativePin(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	pid := int64(6)
	ReplaceProviders([]*Provider{{
		ID: 6, Name: "GW", Protocol: ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "sk-6", AuthMode: ProviderAuthAuto,
		Models: []string{"glm-5.3-flash"},
	}})
	native := "opencode/deepseek-v4.1-flash-free"
	agent := &Agent{
		ID: "opencode-bound", Backend: "opencode", ProviderID: &pid,
		PreferredModel: native,
		Models:         []AgentModel{{ID: native, Name: "DeepSeek V4.1 Flash Free"}},
	}
	ReplaceAgents(map[string]*Agent{"opencode-bound": agent}, []*Agent{agent})

	env, err := ProviderEnvForAgent("opencode-bound", "opencode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	models := inlineConfigContent(t, env)["provider"].(map[string]any)["clawbench-openai"].(map[string]any)["models"].(map[string]any)
	if _, ok := models[native]; ok {
		t.Fatalf("the native pin must not be declared under the provider: %v", models)
	}
	if _, ok := models["glm-5.3-flash"]; !ok {
		t.Fatalf("the curated list is still declared, got %v", models)
	}

	// A custom (unlisted) pin is the escape hatch and must still be declared.
	agent.PreferredModel = "gw-custom"
	env, err = ProviderEnvForAgent("opencode-bound", "opencode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	models = inlineConfigContent(t, env)["provider"].(map[string]any)["clawbench-openai"].(map[string]any)["models"].(map[string]any)
	if _, ok := models["gw-custom"]; !ok {
		t.Fatalf("a custom pin must be declared, got %v", models)
	}
}

// Each protocol gets its own provider id: opencode addresses a declared model as
// "<provider id>/<declared key>", and the two protocols speak different wire
// formats, so they cannot share one block.
func TestBuildProviderEnv_OpenCodeAnthropicProviderID(t *testing.T) {
	p := providerForTest(ProviderProtocolAnthropic, "https://gw.example", "sk-ant", ProviderAuthAuto)
	p.Models = []string{"claude-custom"}

	env, err := BuildProviderEnv("opencode", p, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	provider := inlineConfigContent(t, env)["provider"].(map[string]any)
	if _, ok := provider["clawbench-anthropic"]; !ok {
		t.Fatalf("anthropic models must be declared under \"clawbench-anthropic\", got %v", provider)
	}
}

// Nothing to declare must mean no content at all: an empty provider block would
// shadow the user's own opencode configuration without adding anything.
func TestBuildProviderEnv_OpenCodeWithoutModelsHasNoContent(t *testing.T) {
	env, err := BuildProviderEnv("opencode", providerForTest(ProviderProtocolOpenAI, "https://gw.example/v1", "sk-1", ProviderAuthAuto), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "OPENCODE_CONFIG_CONTENT=") {
			t.Fatalf("an uncurated provider must not inject config content: %v", env)
		}
	}
}

// Backends with no inline-config slot are untouched by the new branch.
func TestBuildProviderEnv_InlineConfigOnlyForDeclaringBackends(t *testing.T) {
	p := providerForTest(ProviderProtocolAnthropic, "https://gw.example", "sk-1", ProviderAuthAuto)
	p.Models = []string{"m1"}

	env, err := BuildProviderEnv("claude", p, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "OPENCODE_CONFIG_CONTENT=") {
			t.Fatalf("claude must not receive opencode config content: %v", env)
		}
	}
}

// inlineConfigContent extracts and decodes the inline config entry, failing the
// test when it is absent or not valid JSON.
func inlineConfigContent(t *testing.T, env []string) map[string]any {
	t.Helper()
	const prefix = "OPENCODE_CONFIG_CONTENT="
	for _, entry := range env {
		raw, ok := strings.CutPrefix(entry, prefix)
		if !ok {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("inline config is not valid JSON: %v\n%s", err, raw)
		}
		return out
	}
	t.Fatalf("no %s entry in %v", prefix, env)
	return nil
}

// A provider with no base URL (an official API key) must not reference an
// endpoint variable the spawn never sets: an unresolved "${...}" would either
// fail to parse or replace opencode's own default endpoint with garbage.
func TestBuildProviderEnv_OpenCodeNoBaseURLSkipsEndpointReference(t *testing.T) {
	p := providerForTest(ProviderProtocolOpenAI, "", "sk-1", ProviderAuthAuto)
	p.Models = []string{"glm-5.3-flash"}

	env, err := BuildProviderEnv("opencode", p, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	opts := inlineConfigContent(t, env)["provider"].(map[string]any)["clawbench-openai"].(map[string]any)["options"].(map[string]any)
	if _, ok := opts["baseURL"]; ok {
		t.Fatalf("baseURL must be absent when the provider has none, got %v", opts)
	}
	if opts["apiKey"] != "{env:OPENAI_API_KEY}" {
		t.Fatalf("the credential reference must stay, got %v", opts)
	}
}

// A gateway may namespace its own ids ("openai/gpt-4o" is a real OpenRouter
// style id). Declaring it must not strip the namespace: the endpoint expects
// the full id, and opencode already keys the model under "openai".
func TestBuildProviderEnv_OpenCodeKeepsForeignNamespace(t *testing.T) {
	p := providerForTest(ProviderProtocolOpenAI, "https://gw.example/v1", "sk-1", ProviderAuthAuto)
	p.Models = []string{"openai/gpt-4o"}

	env, err := BuildProviderEnv("opencode", p, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	models := inlineConfigContent(t, env)["provider"].(map[string]any)["clawbench-openai"].(map[string]any)["models"].(map[string]any)
	if _, ok := models["openai/gpt-4o"]; !ok {
		t.Fatalf("the endpoint's own id must be declared verbatim, got %v", models)
	}
}

// Merging must leave exactly one default: `resolved` is already normalised (it
// carries the session's current model), while the provider entries carry their
// own flag. Two defaults would make a frontend `find(m => m.default)` fallback
// move a session off the model it is actually running (issue #651 review).
func TestMergeProviderModels_KeepsSingleDefault(t *testing.T) {
	pid := int64(652)
	ReplaceProviders([]*Provider{{
		ID: 652, Name: "GW", Protocol: ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{"m1", "m2"}, DefaultModel: "m1",
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{ID: "merge-default-agent", Backend: "opencode", ProviderID: &pid}
	resolved := []AgentModel{
		{ID: "clawbench-openai/m2", Name: "m2", Default: true},
		{ID: "clawbench-openai/m1", Name: "m1"},
	}

	merged := agent.MergeProviderModels(resolved)
	require := func(cond bool, format string, args ...any) {
		if !cond {
			t.Fatalf(format, args...)
		}
	}
	defaults := 0
	for _, m := range merged {
		if m.Default {
			defaults++
			require(m.ID == "clawbench-openai/m2", "default = %q, want the resolved list's pick", m.ID)
		}
	}
	require(defaults == 1, "exactly one default, got %d", defaults)
}

// A bound provider supplies models even when the agent's own discovery found
// nothing — the chat drawer reads this channel, so bailing out on an empty CLI
// list would hide them (issue #651 review).
func TestMergeProviderModels_WorksWithEmptyResolvedList(t *testing.T) {
	pid := int64(653)
	ReplaceProviders([]*Provider{{
		ID: 653, Name: "GW", Protocol: ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "k", Models: []string{"m1"},
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{ID: "merge-empty-agent", Backend: "opencode", ProviderID: &pid}
	merged := agent.MergeProviderModels(nil)
	if len(merged) != 1 || merged[0].ID != "clawbench-openai/m1" {
		t.Fatalf("merged = %v, want the provider model", merged)
	}
}

// A provider opencode does not know needs `npm` to pick a wire adapter. Without
// it the model LISTING and set_config_option both still succeed, but the real
// turn dies with "Unsupported package" / provider.no-route and not one byte
// reaches the endpoint — so this is the difference between "looks configured"
// and "actually works" (issue #651 review).
func TestBuildProviderEnv_OpenCodeDeclaresWireAdapter(t *testing.T) {
	cases := []struct {
		protocol string
		wantNPM  string
	}{
		{ProviderProtocolOpenAI, "@ai-sdk/openai-compatible"},
		{ProviderProtocolAnthropic, "@ai-sdk/anthropic"},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			p := providerForTest(tc.protocol, "https://gw.example/v1", "sk-1", ProviderAuthAuto)
			p.Models = []string{"m1"}

			env, err := BuildProviderEnv("opencode", p, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			provider := inlineConfigContent(t, env)["provider"].(map[string]any)
			var block map[string]any
			for _, v := range provider {
				block = v.(map[string]any)
			}
			if block["npm"] != tc.wantNPM {
				t.Fatalf("npm = %v, want %q (without it the turn has no route)", block["npm"], tc.wantNPM)
			}
		})
	}
}
