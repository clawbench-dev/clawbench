package model

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// provider_presets.json is a snapshot of the models.dev catalog (the same
// data OpenCode embeds), reduced to the fields the settings page needs. It is
// generated, not hand-maintained: see scripts/gen-provider-presets.mjs.
//
// A preset only fills name / protocol / base URL. It never carries a key, and
// it is never the source of truth for a saved provider — the row always wins.
//
//go:embed provider_presets.json
var providerPresetsJSON []byte

// ProviderPreset is one catalog entry offered when creating a provider.
type ProviderPreset struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
}

var (
	presetsOnce sync.Once
	presets     []ProviderPreset
)

// ProviderPresets returns the embedded catalog. The slice is shared; callers
// must not modify it.
func ProviderPresets() []ProviderPreset {
	presetsOnce.Do(func() {
		var parsed []ProviderPreset
		if err := json.Unmarshal(providerPresetsJSON, &parsed); err != nil {
			// A corrupt embedded asset is a build defect, not a runtime input:
			// keep the endpoint usable with an empty list rather than panicking.
			presets = nil
			return
		}
		presets = parsed
	})
	return presets
}
