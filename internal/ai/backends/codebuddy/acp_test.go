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

func TestCodebuddyBackendSpec_SortOrder(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	assert.Equal(t, 2, plugin.Spec.SortOrder)
}
