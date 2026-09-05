package zcode

import (
	"testing"

	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestZcodeBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("zcode")
	require.NotNil(t, plugin, "zcode should be registered in backends registry")
	assert.Equal(t, "zcode", plugin.ID)
}

func TestZcodeBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("zcode")
	require.NotNil(t, plugin)
	assert.Equal(t, "zcode", plugin.Spec.ID)
	assert.Equal(t, "zcode", plugin.Spec.Backend)
	assert.Equal(t, "zcode-acp-server", plugin.Spec.DefaultCmd)
	assert.Equal(t, "ZCode", plugin.Spec.Name)
	assert.Equal(t, "npx -y zcode-acp-server", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession)
	assert.Equal(t, "npm install -g zcode-acp-server", plugin.Spec.InstallCmd)
	assert.Equal(t, []string{"low", "high", "max"}, plugin.Spec.ThinkingEffortLevels)
	assert.NotZero(t, plugin.Spec.SortOrder)
}

// The zcode bridge (zcode-acp-server) resolves the zcode CLI itself
// (ZCODE_BIN → PATH → desktop-app bundle), so no CLI factory is registered:
// chat must always go through the ACP stdio transport.
func TestZcodeBackendPlugin_NoCLIFactory(t *testing.T) {
	plugin := backends.Lookup("zcode")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.ACP, "zcode needs no tool-name remapping: tools are reported canonically over ACP")
}
