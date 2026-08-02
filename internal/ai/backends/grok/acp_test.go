package grok

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai/backends"
)

// --- ACP remaps ---

func TestGrokACPRemaps_ContainsExpectedKeys(t *testing.T) {
	assert.Equal(t, "old_string", GrokACPRemaps["oldString"])
	assert.Equal(t, "new_string", GrokACPRemaps["newString"])
	assert.Equal(t, "path", GrokACPRemaps["dirPath"])
	assert.Equal(t, "file_path", GrokACPRemaps["filePath"])
	assert.Equal(t, "cell_index", GrokACPRemaps["cellIndex"])
	assert.Equal(t, "cell_type", GrokACPRemaps["cellType"])
}

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

func TestGrokBackendPlugin_ACPPlugin(t *testing.T) {
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP, "grok should have ACP plugin")
	assert.Equal(t, GrokACPRemaps, plugin.ACP.InputRemaps)
	assert.Nil(t, plugin.ACP.ToolCallIDPrefixes, "grok uses standard ACP tool names, no prefix map needed")
}

func TestGrokBackendPlugin_NoCLIFactory(t *testing.T) {
	// Grok is ACP-only: no CLI plugin and no CLI backend factory.
	plugin := backends.Lookup("grok")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.CLI, "grok should not have a CLI plugin")
	assert.Nil(t, plugin.Custom, "grok should not have a custom plugin")
}
