package antigravity

import "testing"

// A signed-in `agy models` prints one model per line; the status line and log
// noise must not become picker entries.
func TestParseAgyModels_SignedInOutput(t *testing.T) {
	out := `Fetching available models...
gemini-3.1-pro
gemini-3-flash
I0428 10:00:00.000000 1234 some log line
`
	models := parseAgyModels(out)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "gemini-3.1-pro" || models[0].Name != "Gemini 3.1 Pro" {
		t.Fatalf("first entry must be gemini-3.1-pro with its display name, got %+v", models[0])
	}
	if models[1].ID != "gemini-3-flash" {
		t.Fatalf("second entry must be gemini-3-flash, got %+v", models[1])
	}
}

// Unauthenticated output is diagnostics only: parsing must yield nothing so the
// probe falls back to AntigravityCatalog instead of showing "Error: ..." as a
// model.
func TestParseAgyModels_UnauthenticatedYieldsNothing(t *testing.T) {
	out := `Fetching available models...
Error: Please sign in to view available models. Launch the CLI without arguments to sign in.
`
	if models := parseAgyModels(out); len(models) != 0 {
		t.Fatalf("auth diagnostics must not become models, got %+v", models)
	}
}
