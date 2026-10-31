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

// The legacy shape keeps working until the startup migration rewrites it; this
// is what makes an upgrade safe when the config write fails.
func TestSummaryEndpoint_LegacyFallback(t *testing.T) {
	cfg := AISummaryConfig{API: APIConfig{BaseURL: "https://legacy.example/v1", Key: "old"}}
	base, key, format, ok := cfg.SummaryEndpoint()
	if !ok || base != "https://legacy.example/v1" || key != "old" || format != "" {
		t.Fatalf("legacy endpoint = %q/%q/%q ok=%v", base, key, format, ok)
	}
}

// A bound provider wins over a leftover legacy endpoint, so a half-migrated
// config cannot keep calling the old service.
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
