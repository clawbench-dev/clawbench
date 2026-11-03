package model

import "testing"

func TestSummaryEndpoint_UsesBoundProvider(t *testing.T) {
	pid := int64(42)
	ReplaceProviders([]*Provider{{
		ID: 42, Name: "GW", Protocol: ProviderProtocolAnthropic,
		BaseURL: "https://gw.example/anthropic", APIKey: "sk-x", AuthMode: ProviderAuthAuto,
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	cfg := AISummaryConfig{ProviderID: &pid, Model: "m"}
	base, key, format, ok := cfg.SummaryEndpoint()
	if !ok {
		t.Fatal("expected a configured endpoint")
	}
	if base != "https://gw.example/anthropic" || key != "sk-x" || format != "anthropic" {
		t.Fatalf("endpoint = %q/%q/%q", base, key, format)
	}
	if !cfg.SummaryConfigured() {
		t.Fatal("SummaryConfigured must agree with SummaryEndpoint")
	}
}

func TestSummaryEndpoint_RejectsGeminiProvider(t *testing.T) {
	pid := int64(43)
	ReplaceProviders([]*Provider{{
		ID: 43, Name: "Gem", Protocol: ProviderProtocolGemini,
		BaseURL: "https://gem.example", APIKey: "k", AuthMode: ProviderAuthAuto,
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	cfg := AISummaryConfig{ProviderID: &pid}
	if _, _, _, ok := cfg.SummaryEndpoint(); ok {
		t.Fatal("the summarizer cannot speak the gemini protocol")
	}
	if cfg.SummaryConfigured() {
		t.Fatal("a gemini provider must read as unconfigured")
	}
}

func TestSummaryEndpoint_MissingProviderIsUnconfigured(t *testing.T) {
	pid := int64(999)
	ReplaceProviders(nil)
	cfg := AISummaryConfig{ProviderID: &pid}
	if cfg.SummaryConfigured() {
		t.Fatal("a dangling provider id must not count as configured")
	}
}

// The endpoint-block shape keeps working as a fallback for configs written
// before the provider binding existed; this is what keeps an upgraded install
// reading as configured instead of silently dropping its summary model.
func TestSummaryEndpoint_LegacyFallback(t *testing.T) {
	cfg := AISummaryConfig{API: APIConfig{BaseURL: "https://legacy.example/v1", Key: "old"}}
	base, key, format, ok := cfg.SummaryEndpoint()
	if !ok || base != "https://legacy.example/v1" || key != "old" || format != "" {
		t.Fatalf("legacy endpoint = %q/%q/%q ok=%v", base, key, format, ok)
	}
}

// A bound provider wins over the endpoint-block fallback, so a config with both
// cannot keep calling the old service.
func TestSummaryEndpoint_ProviderWinsOverLegacy(t *testing.T) {
	pid := int64(44)
	ReplaceProviders([]*Provider{{
		ID: 44, Name: "New", Protocol: ProviderProtocolOpenAI,
		BaseURL: "https://new.example/v1", APIKey: "new", AuthMode: ProviderAuthAuto,
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	cfg := AISummaryConfig{
		ProviderID: &pid,
		API:        APIConfig{BaseURL: "https://legacy.example/v1", Key: "old"},
	}
	base, _, _, ok := cfg.SummaryEndpoint()
	if !ok || base != "https://new.example/v1" {
		t.Fatalf("base = %q, want the provider endpoint", base)
	}
}

func TestDefaultModelID_PrefersCuratedProviderList(t *testing.T) {
	pid := int64(77)
	ReplaceProviders([]*Provider{{
		ID: 77, Name: "GW", Protocol: ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"m1", "m2"}, DefaultModel: "m2",
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{ProviderID: &pid, PreferredModel: "not-served", Models: []AgentModel{{ID: "cli-model", Default: true}}}
	if got := agent.DefaultModelID(); got != "m2" {
		t.Fatalf("default = %q, want the provider default (the preference is not served)", got)
	}

	agent.PreferredModel = "m1"
	if got := agent.DefaultModelID(); got != "m1" {
		t.Fatalf("default = %q, want the served preference", got)
	}

	// Without a curated list the old priority stands.
	agent.PreferredModel = ""
	ReplaceProviders(nil)
	if got := agent.DefaultModelID(); got != "cli-model" {
		t.Fatalf("default = %q, want the CLI default", got)
	}
}

// A bound provider with no endpoint is not configured: the summarizer would
// have a key and nowhere to send it.
func TestSummaryEndpoint_ProviderWithoutEndpointIsUnconfigured(t *testing.T) {
	pid := int64(45)
	ReplaceProviders([]*Provider{{
		ID: 45, Name: "KeyOnly", Protocol: ProviderProtocolOpenAI,
		APIKey: "k", AuthMode: ProviderAuthAuto,
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	cfg := AISummaryConfig{ProviderID: &pid}
	if _, _, _, ok := cfg.SummaryEndpoint(); ok {
		t.Fatal("a provider without a base URL must read as unconfigured")
	}
	if cfg.SummaryConfigured() {
		t.Fatal("SummaryConfigured must agree with SummaryEndpoint")
	}
}

// The model id is a free-form string: providers expose different catalogs and
// some gateways publish none, so it is passed through verbatim.
func TestSummaryModel_ReturnsConfiguredModel(t *testing.T) {
	if got := (AISummaryConfig{Model: "gpt-4o-mini"}).SummaryModel(); got != "gpt-4o-mini" {
		t.Fatalf("SummaryModel = %q", got)
	}
	if got := (AISummaryConfig{}).SummaryModel(); got != "" {
		t.Fatalf("SummaryModel = %q, want empty", got)
	}
}
