package model

import (
	"reflect"
	"testing"
)

func TestValidProtocol(t *testing.T) {
	cases := []struct {
		protocol string
		want     bool
	}{
		{ProviderProtocolAnthropic, true},
		{ProviderProtocolOpenAI, true},
		{ProviderProtocolGemini, true},
		{"", false},
		{"cohere", false},
		{"Anthropic", false}, // callers normalize before validating
	}
	for _, tc := range cases {
		if got := ValidProtocol(tc.protocol); got != tc.want {
			t.Errorf("ValidProtocol(%q) = %v, want %v", tc.protocol, got, tc.want)
		}
	}
}

func TestValidAuthMode(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{ProviderAuthAuto, true},
		{ProviderAuthAPIKey, true},
		{ProviderAuthBearer, true},
		{"", false},
		{"oauth", false},
	}
	for _, tc := range cases {
		if got := ValidAuthMode(tc.mode); got != tc.want {
			t.Errorf("ValidAuthMode(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestValidWireAPI(t *testing.T) {
	cases := []struct {
		api  string
		want bool
	}{
		{ProviderWireResponses, true},
		{ProviderWireChat, true},
		{"", false},
		{"grpc", false},
	}
	for _, tc := range cases {
		if got := ValidWireAPI(tc.api); got != tc.want {
			t.Errorf("ValidWireAPI(%q) = %v, want %v", tc.api, got, tc.want)
		}
	}
}

// A default outside the curated list is replaced rather than stored: an agent
// inheriting it would ask the endpoint for a model it does not serve.
func TestProviderNormalized_CorrectsDefaultModel(t *testing.T) {
	p := Provider{
		Name:         "  GW  ",
		Protocol:     " OpenAI ",
		BaseURL:      " https://gw.example/v1 ",
		APIKey:       "  sk-1 ",
		AuthMode:     " BEARER ",
		PresetID:     " gw ",
		Models:       []string{"m1", "m2"},
		DefaultModel: "nope",
		WireAPI:      " CHAT ",
	}.Normalized()

	if p.Name != "GW" || p.Protocol != ProviderProtocolOpenAI || p.BaseURL != "https://gw.example/v1" {
		t.Fatalf("trimming/normalizing failed: %+v", p)
	}
	if p.APIKey != "sk-1" || p.AuthMode != ProviderAuthBearer || p.PresetID != "gw" {
		t.Fatalf("secret/auth normalization failed: %+v", p)
	}
	if p.WireAPI != ProviderWireChat {
		t.Fatalf("wire api = %q", p.WireAPI)
	}
	if p.DefaultModel != "m1" {
		t.Fatalf("default = %q, want the first curated model", p.DefaultModel)
	}

	// No curated list means no default at all (it would be a model the
	// endpoint was never asked about).
	empty := Provider{Models: nil, DefaultModel: "stale"}.Normalized()
	if empty.DefaultModel != "" {
		t.Fatalf("default = %q, want empty without a curated list", empty.DefaultModel)
	}
	if empty.AuthMode != ProviderAuthAuto || empty.WireAPI != ProviderWireResponses {
		t.Fatalf("unset optional fields must resolve to their defaults: %+v", empty)
	}
}

func TestNormalizeModelIDs(t *testing.T) {
	got := normalizeModelIDs([]string{" a ", "", "b", "a", "  "})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("normalizeModelIDs = %v, want [a b]", got)
	}
	if normalizeModelIDs(nil) != nil {
		t.Fatal("an empty list must stay nil")
	}
}

// ReplaceProviders is the snapshot every spawn path reads, so it has to be
// defensive: nil entries dropped, every row normalized, and a stable order that
// does not depend on the input order.
func TestReplaceProviders_NormalizesAndOrdersSnapshot(t *testing.T) {
	t.Cleanup(func() { ReplaceProviders(nil) })

	ReplaceProviders([]*Provider{
		{ID: 3, Name: "beta", Protocol: ProviderProtocolOpenAI},
		nil,
		{ID: 2, Name: "Alpha", Protocol: ProviderProtocolAnthropic},
		{ID: 1, Name: "Alpha", Protocol: ProviderProtocolAnthropic},
	})

	got := ListProviders()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (the nil entry must be dropped)", len(got))
	}
	// Same name: the id breaks the tie, so the order is deterministic.
	wantOrder := []int64{1, 2, 3}
	for i, p := range got {
		if p.ID != wantOrder[i] {
			t.Fatalf("order = %v, want %v", idsOf(got), wantOrder)
		}
	}
	if got[0].AuthMode != ProviderAuthAuto || got[0].WireAPI != ProviderWireResponses {
		t.Fatalf("rows must be stored normalized: %+v", got[0])
	}
	if GetProvider(2) == nil {
		t.Fatal("the by-id index must contain every row")
	}
}

// ListProviders hands out a copy of the slice: a caller appending to it must not
// corrupt the snapshot (the next reader would see a truncated or grown list).
func TestListProviders_ReturnsIndependentSlice(t *testing.T) {
	t.Cleanup(func() { ReplaceProviders(nil) })
	ReplaceProviders([]*Provider{{ID: 1, Name: "only", Protocol: ProviderProtocolOpenAI}})

	first := ListProviders()
	first[0] = nil
	_ = append(first, &Provider{ID: 99, Name: "ghost"})

	second := ListProviders()
	if len(second) != 1 || second[0] == nil || second[0].ID != 1 {
		t.Fatalf("snapshot was mutated through the returned slice: %+v", second)
	}
}

func TestGetProvider_RejectsNonPositiveID(t *testing.T) {
	t.Cleanup(func() { ReplaceProviders(nil) })
	ReplaceProviders([]*Provider{{ID: 1, Name: "a", Protocol: ProviderProtocolOpenAI}})

	// 0 is the "no provider" sentinel on agents; it must never resolve to a row.
	if GetProvider(0) != nil {
		t.Fatal("id 0 must not resolve")
	}
	if GetProvider(-1) != nil {
		t.Fatal("a negative id must not resolve")
	}
}

// AgentProvider re-reads the agent every call: a refresh replaces agents
// wholesale, so a cached *Agent would keep a stale binding.
func TestAgentProvider_FollowsLiveAgentBinding(t *testing.T) {
	t.Cleanup(func() {
		ReplaceProviders(nil)
		ReplaceAgents(nil, nil)
	})
	ReplaceProviders([]*Provider{
		{ID: 1, Name: "one", Protocol: ProviderProtocolOpenAI, APIKey: "k"},
		{ID: 2, Name: "two", Protocol: ProviderProtocolOpenAI, APIKey: "k"},
	})

	pid := int64(1)
	ReplaceAgents(
		map[string]*Agent{"a": {ID: "a", Backend: "claude", ProviderID: &pid}},
		[]*Agent{{ID: "a", Backend: "claude", ProviderID: &pid}},
	)
	if p := AgentProvider("a"); p == nil || p.ID != 1 {
		t.Fatalf("AgentProvider = %+v, want provider 1", p)
	}

	// Rebinding the agent (a new snapshot) must be visible immediately.
	pid2 := int64(2)
	ReplaceAgents(
		map[string]*Agent{"a": {ID: "a", Backend: "claude", ProviderID: &pid2}},
		[]*Agent{{ID: "a", Backend: "claude", ProviderID: &pid2}},
	)
	if p := AgentProvider("a"); p == nil || p.ID != 2 {
		t.Fatalf("AgentProvider = %+v, want provider 2 after the rebind", p)
	}

	if AgentProvider("missing") != nil {
		t.Fatal("an unknown agent has no provider")
	}
}

func idsOf(list []*Provider) []int64 {
	out := make([]int64, 0, len(list))
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}
