package pi

import (
	"testing"

	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPiBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("pi")
	require.NotNil(t, plugin, "pi should be registered in backends registry")
	assert.Equal(t, "pi", plugin.ID)
}

func TestPiBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("pi")
	require.NotNil(t, plugin)
	assert.Equal(t, "pi", plugin.Spec.ID)
	assert.Equal(t, "pi", plugin.Spec.Backend)
	assert.Equal(t, "pi", plugin.Spec.DefaultCmd)
	assert.Equal(t, "Pi", plugin.Spec.Name)
	assert.Equal(t, "npx -y pi-acp@latest", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession, "pi-acp bridge supports session/load reattachment")
	assert.Equal(t, "npm install -g @earendil-works/pi-coding-agent", plugin.Spec.InstallCmd)
	assert.Equal(t, 8, plugin.Spec.SortOrder)
}

func TestPiBackendPlugin_ACPInputRemaps(t *testing.T) {
	plugin := backends.Lookup("pi")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP, "pi should register ACP mapping data")
	require.NotEmpty(t, plugin.ACP.InputRemaps, "pi should map ACP input field names")

	assert.Equal(t, "file_path", plugin.ACP.InputRemaps["path"], "pi edit/read uses path, canonical is file_path")
	assert.Equal(t, "old_string", plugin.ACP.InputRemaps["oldText"], "pi edit uses oldText, canonical is old_string")
	assert.Equal(t, "new_string", plugin.ACP.InputRemaps["newText"], "pi edit uses newText, canonical is new_string")
}

func TestPiBackendPlugin_ACPToolPrefixes(t *testing.T) {
	plugin := backends.Lookup("pi")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP, "pi should register ACP mapping data")
	require.NotEmpty(t, plugin.ACP.ToolCallIDPrefixes, "pi should map ACP tool names")

	assert.Equal(t, "Read", plugin.ACP.ToolCallIDPrefixes["read"])
	assert.Equal(t, "Edit", plugin.ACP.ToolCallIDPrefixes["edit"])
	assert.Equal(t, "Write", plugin.ACP.ToolCallIDPrefixes["write"])
	assert.Equal(t, "Bash", plugin.ACP.ToolCallIDPrefixes["bash"])
	assert.Equal(t, "Grep", plugin.ACP.ToolCallIDPrefixes["grep"])
	assert.Equal(t, "Glob", plugin.ACP.ToolCallIDPrefixes["glob"])
	assert.Equal(t, "LS", plugin.ACP.ToolCallIDPrefixes["ls"])
	assert.Equal(t, "AskUserQuestion", plugin.ACP.ToolCallIDPrefixes["ask"])
}
