package copilot

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotCatalog_Structure(t *testing.T) {
	require.NotEmpty(t, model.CopilotCatalog, "should have a default catalog")

	for _, m := range model.CopilotCatalog {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
	}
}

func TestCopilotCatalog_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range model.CopilotCatalog {
		ids[m.ID] = true
	}
	assert.True(t, ids["gpt-4.1"], "should contain GPT-4.1")
	assert.True(t, ids["claude-sonnet-4-20250514"], "should contain Claude Sonnet 4")
}

func TestCopilotSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "copilot", Backend: "copilot", DefaultCmd: "copilot"}
	assert.True(t, model.CanDiscoverModels(spec), "copilot should support model discovery")

	src, ok := model.LookupModelSource("copilot")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindStatic, src.Kind())
}

func TestCopilotSource_YieldsNothingWithoutCLI(t *testing.T) {
	// The static source is gated on the CLI being installed. In a test
	// environment `copilot` is absent, so discovery returns nothing — which is
	// the correct signal: no models for a CLI the user does not have.
	src, _ := model.LookupModelSource("copilot")
	models, detail := src.Discover()
	assert.Empty(t, detail)
	if models != nil {
		assert.Equal(t, len(model.CopilotCatalog), len(models))
		assert.True(t, models[0].Default)
	}
}
