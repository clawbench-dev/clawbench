package model

import "testing"

// Each protocol needs its own wire adapter. An unknown protocol must yield "",
// which makes the caller declare nothing rather than declare a broken block.
func TestNpmForProtocol(t *testing.T) {
	cases := []struct {
		protocol string
		want     string
	}{
		{ProviderProtocolOpenAI, "@ai-sdk/openai-compatible"},
		{ProviderProtocolAnthropic, "@ai-sdk/anthropic"},
		{ProviderProtocolGemini, "@ai-sdk/google"},
		{"unknown", ""},
	}
	for _, tc := range cases {
		if got := npmForProtocol(tc.protocol); got != tc.want {
			t.Errorf("npmForProtocol(%q) = %q, want %q", tc.protocol, got, tc.want)
		}
	}
}

// The namespace helpers must be total: every combination the table does not
// describe returns the id unchanged, so a caller can never produce "//m1".
func TestProviderModelIDFor_Fallbacks(t *testing.T) {
	cases := []struct {
		name     string
		backend  string
		protocol string
		id       string
		want     string
	}{
		{"unknown backend", "nope", ProviderProtocolOpenAI, "m1", "m1"},
		{"protocol without a mapping", "opencode", ProviderProtocolGemini, "m1", "m1"},
		{"empty id", "opencode", ProviderProtocolOpenAI, "", ""},
		{"already namespaced", "opencode", ProviderProtocolOpenAI, "clawbench-openai/m1", "clawbench-openai/m1"},
		{"bare id gets the namespace", "opencode", ProviderProtocolOpenAI, "m1", "clawbench-openai/m1"},
	}
	for _, tc := range cases {
		if got := ProviderModelIDFor(tc.backend, tc.protocol, tc.id); got != tc.want {
			t.Errorf("%s: ProviderModelIDFor(%q, %q, %q) = %q, want %q",
				tc.name, tc.backend, tc.protocol, tc.id, got, tc.want)
		}
	}
}

func TestProviderBareModelID(t *testing.T) {
	cases := []struct {
		name     string
		backend  string
		protocol string
		id       string
		want     string
	}{
		{"unknown backend", "nope", ProviderProtocolOpenAI, "clawbench-openai/m1", "clawbench-openai/m1"},
		{"protocol without a mapping", "opencode", ProviderProtocolGemini, "clawbench-openai/m1", "clawbench-openai/m1"},
		{"strips our namespace", "opencode", ProviderProtocolOpenAI, "clawbench-openai/m1", "m1"},
		{"leaves a bare id alone", "opencode", ProviderProtocolOpenAI, "m1", "m1"},
	}
	for _, tc := range cases {
		if got := ProviderBareModelID(tc.backend, tc.protocol, tc.id); got != tc.want {
			t.Errorf("%s: ProviderBareModelID(%q, %q, %q) = %q, want %q",
				tc.name, tc.backend, tc.protocol, tc.id, got, tc.want)
		}
	}
}

func TestProviderPickerModels_UnboundOrUncurated(t *testing.T) {
	unbound := &Agent{Backend: "opencode"}
	if got := unbound.ProviderPickerModels(); got != nil {
		t.Fatalf("unbound agent = %v, want nil", got)
	}

	pid := int64(9001)
	ReplaceProviders([]*Provider{{
		ID: 9001, Name: "GW", Protocol: ProviderProtocolOpenAI, Models: nil,
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	uncurated := &Agent{Backend: "opencode", ProviderID: &pid}
	if got := uncurated.ProviderPickerModels(); got != nil {
		t.Fatalf("provider without curated models = %v, want nil", got)
	}
}

// The merged list keeps the provider's order, drops duplicates from the
// resolved list and still falls back to the provider's default when the
// resolved list brought none of its own.
func TestMergeProviderModels_DedupesAndFallsBackToProviderDefault(t *testing.T) {
	pid := int64(9002)
	ReplaceProviders([]*Provider{{
		ID: 9002, Name: "GW", Protocol: ProviderProtocolOpenAI,
		Models: []string{"m1", "m2"}, DefaultModel: "m1",
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{Backend: "opencode", ProviderID: &pid}
	merged := agent.MergeProviderModels([]AgentModel{
		{ID: "clawbench-openai/m1", Name: "m1 (resolved)"},
		{ID: "other", Name: "other"},
	})

	wantIDs := []string{"clawbench-openai/m1", "clawbench-openai/m2", "other"}
	if len(merged) != len(wantIDs) {
		t.Fatalf("merged = %v, want %v", merged, wantIDs)
	}
	for i, want := range wantIDs {
		if merged[i].ID != want {
			t.Fatalf("merged[%d] = %q, want %q (merged: %v)", i, merged[i].ID, want, merged)
		}
	}

	defaults := 0
	for _, m := range merged {
		if m.Default {
			defaults++
			if m.ID != "clawbench-openai/m1" {
				t.Errorf("default = %q, want the provider's default model", m.ID)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("defaults = %d, want exactly one", defaults)
	}
}

func TestMergeProviderModels_WithoutProviderModelsIsIdentity(t *testing.T) {
	agent := &Agent{Backend: "opencode"}
	resolved := []AgentModel{{ID: "a", Name: "a"}}
	merged := agent.MergeProviderModels(resolved)
	if len(merged) != 1 || merged[0].ID != "a" {
		t.Fatalf("merged = %v, want the resolved list unchanged", merged)
	}
}
