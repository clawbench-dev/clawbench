package grok

import (
	"testing"

	"clawbench/internal/ai"
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

func TestGrokBackendPlugin_ACPToolMapping(t *testing.T) {
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP, "grok should register ACP tool mapping data")
	assert.NotEmpty(t, plugin.ACP.ToolCallIDPrefixes, "grok should map ACP toolCallID prefixes")
	assert.NotEmpty(t, plugin.ACP.InputRemaps, "grok should map ACP input field names")
	assert.Equal(t, "Read", plugin.ACP.ToolCallIDPrefixes["read_file"])
	assert.Equal(t, "Edit", plugin.ACP.ToolCallIDPrefixes["search_replace"])
	assert.Equal(t, "old_string", plugin.ACP.InputRemaps["oldString"])
}

func TestGrokBackendPlugin_RegisteredCLIFactory(t *testing.T) {
	// Grok registers a streaming-json CLI fallback via ai.RegisterBackend.
	assert.NotNil(t, ai.LookupBackendFactoryForTest("grok"), "grok should have a CLI backend factory")
}
