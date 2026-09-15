package kimi

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKimiCatalog_Structure(t *testing.T) {
	require.NotEmpty(t, model.KimiCatalog, "should have a default catalog")

	defaultCount := 0
	for _, m := range model.KimiCatalog {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be marked as default")
}

func TestKimiCatalog_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range model.KimiCatalog {
		ids[m.ID] = true
	}
	assert.True(t, ids["kimi-k3"], "should contain Kimi K3")
	assert.True(t, ids["kimi-k2-0711-chat"], "should contain Kimi K2")
	assert.True(t, ids["kimi-for-coding"], "should contain Kimi K2.7 Code")
	assert.True(t, ids["kimi-for-coding-highspeed"], "should contain Kimi K2.7 Code Highspeed")
	assert.True(t, ids["moonshot-v1-128k"], "should contain Moonshot v1 128K")
}

func TestKimiCatalog_K3IsDefault(t *testing.T) {
	found := false
	for _, m := range model.KimiCatalog {
		if m.ID == "kimi-k3" && m.Default {
			found = true
		}
	}
	assert.True(t, found, "kimi-k3 should be the default model")
}

func TestKimiSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "kimi", Backend: "kimi", DefaultCmd: "kimi"}
	assert.True(t, model.CanDiscoverModels(spec), "kimi should support model discovery")

	src, ok := model.LookupModelSource("kimi")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindStatic, src.Kind())
}

func TestKimiSource_DoesNotMutateCatalog(t *testing.T) {
	src, ok := model.LookupModelSource("kimi")
	require.True(t, ok)

	models, _ := src.Discover()
	if models == nil {
		return // CLI absent in this environment
	}
	models[0] = model.AgentModel{ID: "mutated"}
	assert.NotEqual(t, "mutated", model.KimiCatalog[0].ID, "catalog must not be mutated by a caller")
}
