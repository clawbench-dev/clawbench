package model

import "testing"

// The presets are a generated snapshot of models.dev, so the assertions are
// about the shape the settings page relies on rather than about any particular
// entry (the file is regenerated, the contract is not).
func TestProviderPresets_EmbeddedCatalogIsUsable(t *testing.T) {
	presets := ProviderPresets()
	if len(presets) == 0 {
		t.Fatal("the embedded preset catalog must not be empty")
	}

	seen := make(map[string]struct{}, len(presets))
	for _, p := range presets {
		if p.ID == "" {
			t.Fatalf("preset without an id: %+v", p)
		}
		if _, dup := seen[p.ID]; dup {
			t.Fatalf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = struct{}{}
		// A preset exists to fill a provider form: an entry missing the name,
		// the protocol or the endpoint would leave the user with a row the
		// server rejects on save.
		if p.Name == "" {
			t.Fatalf("preset %q has no name", p.ID)
		}
		if !ValidProtocol(p.Protocol) {
			t.Fatalf("preset %q has an unknown protocol %q", p.ID, p.Protocol)
		}
		if p.BaseURL == "" {
			t.Fatalf("preset %q has no base URL", p.ID)
		}
	}
}

// The catalog is parsed once (sync.Once) and the slice is shared; a second call
// must not re-parse or return a different backing array.
func TestProviderPresets_SharedSnapshot(t *testing.T) {
	first := ProviderPresets()
	second := ProviderPresets()
	if len(first) != len(second) {
		t.Fatalf("len = %d then %d", len(first), len(second))
	}
	if len(first) == 0 {
		t.Fatal("catalog is empty")
	}
	if &first[0] != &second[0] {
		t.Fatal("presets must be parsed once and shared, not rebuilt per call")
	}
}
