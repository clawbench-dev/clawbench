package grok

import "testing"

// `grok models` lists the catalogue even when nobody is signed in, which is what
// gives the picker a list before the first ACP handshake. Captured from grok
// 4.6 (2026-11-22).
func TestParseGrokModels_UnauthenticatedOutput(t *testing.T) {
	out := `You are not authenticated.

Default model: grok-4.6

Available models:
  * grok-4.6 (default)
  - grok-4.5
`
	models := parseGrokModels(out)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "grok-4.6" || !models[0].Default {
		t.Fatalf("first entry must be the default grok-4.6, got %+v", models[0])
	}
	if models[0].Name != "Grok 4.6" {
		t.Fatalf("display name must come from GrokModelNames, got %q", models[0].Name)
	}
	if models[1].ID != "grok-4.5" || models[1].Default {
		t.Fatalf("second entry must be grok-4.5 and not default, got %+v", models[1])
	}
}

// Diagnostics are not models: a header line without a bullet marker must never
// become a picker entry.
func TestParseGrokModels_IgnoresHeadersAndNoise(t *testing.T) {
	models := parseGrokModels("Available models:\nDefault model: grok-4.6\n")
	if len(models) != 0 {
		t.Fatalf("headers must not become models, got %+v", models)
	}
}
