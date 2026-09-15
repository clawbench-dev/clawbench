package mimo

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMimoCatalog_Structure(t *testing.T) {
	require.NotEmpty(t, model.MimoCatalog, "should have a default catalog")

	defaultCount := 0
	for _, m := range model.MimoCatalog {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be marked as default")
}

func TestMimoCatalog_FirstIsDefault(t *testing.T) {
	require.NotEmpty(t, model.MimoCatalog)
	assert.True(t, model.MimoCatalog[0].Default, "first model should be default")
	assert.Equal(t, "mimo/mimo-auto", model.MimoCatalog[0].ID)
}

func TestMimoCatalog_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range model.MimoCatalog {
		ids[m.ID] = true
	}
	assert.True(t, ids["mimo/mimo-auto"], "should contain MiMo Auto")
	assert.True(t, ids["xiaomi/mimo-v2.5-pro"], "should contain MiMo V2.5 Pro")
}

func TestMimoSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "mimo", Backend: "mimo", DefaultCmd: "mimo"}
	assert.True(t, model.CanDiscoverModels(spec), "mimo should support model discovery")

	src, ok := model.LookupModelSource("mimo")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindStatic, src.Kind())
}
