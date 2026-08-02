package grok

import (
	"testing"

	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestGrokBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin, "grok should be registered in backends registry")
	assert.Equal(t, "grok", plugin.ID)
}

func TestGrokBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	assert.Equal(t, "grok", plugin.Spec.ID)
	assert.Equal(t, "grok", plugin.Spec.Backend)
	assert.Equal(t, "grok", plugin.Spec.DefaultCmd)
	assert.Equal(t, "Grok", plugin.Spec.Name)
	assert.Equal(t, "grok agent stdio", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession)
	assert.Equal(t, "curl -fsSL https://x.ai/cli/install.sh | bash", plugin.Spec.InstallCmd)
	assert.Equal(t, 13, plugin.Spec.SortOrder)
}

func TestGrokBackendPlugin_ThinkingEffortLevels(t *testing.T) {
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	assert.Equal(t, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		plugin.Spec.ThinkingEffortLevels)
}

func TestGrokBackendPlugin_NoACPPlugin(t *testing.T) {
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.ACP, "grok should have nil ACP plugin (redundant InputRemaps removed)")
}

func TestGrokBackendPlugin_NoCLIFactory(t *testing.T) {
	// Grok is ACP-only: no CLI plugin and no CLI backend factory.
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.CLI, "grok should not have a CLI plugin")
	assert.Nil(t, plugin.Custom, "grok should not have a custom plugin")
}
