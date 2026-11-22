package codebuddy

import (
	"testing"

	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestCodebuddyBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin, "codebuddy should be registered in backends registry")
	assert.Equal(t, "codebuddy", plugin.ID)
}

func TestCodebuddyBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	assert.Equal(t, "codebuddy", plugin.Spec.ID)
	assert.Equal(t, "codebuddy", plugin.Spec.Backend)
	assert.Equal(t, "codebuddy", plugin.Spec.DefaultCmd)
	assert.Equal(t, "Codebuddy", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.ThinkingEffortLevels)
	assert.Equal(t, "codebuddy --acp", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession, "codebuddy should support LoadSession (verified by integration test)")
}

func TestCodebuddyBackendPlugin_SpawnHooksOnly(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP, "codebuddy needs an ACP plugin for its spawn hooks")
	assert.Nil(t, plugin.ACP.InputRemaps, "codebuddy's field names are the shared generic set")
	require.NotNil(t, plugin.ACP.SpawnHooks, "codebuddy's --mcp-config / --settings workarounds live in spawn hooks")
	assert.NotNil(t, plugin.ACP.SpawnHooks.Args)
	assert.Nil(t, plugin.ACP.SpawnHooks.Env, "codebuddy needs no extra env")
}

// --- CLI backend functionality ---

// --- DiscoverCodebuddyModels edge cases ---

func TestParseCodebuddyModels_EmptyCommaSeparated(t *testing.T) {
	// Edge case: "Currently supported: (, , )" — empty parts
	output := "Currently supported: (, , )"
	models := ParseCodebuddyModels(output)
	// Should skip empty parts
	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
	}
}

func TestParseCodebuddyModels_NameFallback(t *testing.T) {
	// Verify Name equals ID for parsed models
	output := "Currently supported: (model-a, model-b)"
	models := ParseCodebuddyModels(output)
	require.Len(t, models, 2)
	for _, m := range models {
		assert.Equal(t, m.ID, m.Name)
	}
}

func TestCodebuddyBackendSpec_SortOrder(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	assert.Equal(t, 2, plugin.Spec.SortOrder)
}
