package qoder

import (
	"testing"

	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The international Qoder build is registered like any other backend. These
// assertions exist because the discovery source was removed: Qoder's local
// dynamic-texts table turned out to be a UI string table, and reverse-
// engineering model ids out of widget labels produced internal codenames
// (gmodel, q35model) instead of models. Discovery must stay with the ACP
// adapter, so a re-added ModelSource here would be a regression.

func TestQoderBackendPlugin_Registered(t *testing.T) {
	plugin := backends.Lookup("qoder")
	require.NotNil(t, plugin, "qoder should be registered in the backends registry")
	assert.Equal(t, "qoder", plugin.ID)
	assert.Equal(t, "qoder", plugin.Spec.Backend,
		"Backend is what ACP event dispatch keys on; it must match the plugin id")
}

func TestQoderBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("qoder")
	require.NotNil(t, plugin)

	assert.Equal(t, "Qoder", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.Specialty)
	assert.Equal(t, "qodercli", plugin.Spec.DefaultCmd)
	assert.Equal(t, "qodercli --acp", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession, "initialize advertises loadSession:true")
	assert.Equal(t, "qodercli login", plugin.Spec.LoginCmd)
}

// No local ModelSource: the removed one produced internal codenames from
// localized widget labels. The ACP adapter is the only source now.
func TestQoderBackendPlugin_HasNoLocalModelSource(t *testing.T) {
	plugin := backends.Lookup("qoder")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.ModelSource,
		"qoder must not re-add a local model source; its only one was a UI string table")
}

// Qoder is a Claude Code fork, so it reuses claude's parsers instead of
// copying them. Losing them would silently drop tool calls from the timeline.
func TestQoderBackendPlugin_ReusesClaudeParsers(t *testing.T) {
	plugin := backends.Lookup("qoder")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.Behavior, "qoder needs ACP behavior hooks")
	assert.NotNil(t, plugin.Behavior.ParseToolCall)
	assert.NotNil(t, plugin.Behavior.ParseToolCallUpdate)
	assert.NotNil(t, plugin.Behavior.ExtractMeta)
}

// It loads its own skills, so ClawBench must not inject them a second time.
func TestQoderBackendPlugin_LoadsNativeSkills(t *testing.T) {
	plugin := backends.Lookup("qoder")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.AutoLoadsNativeSkills)
	assert.Contains(t, plugin.Spec.NativeSkillsDirs, ".qoder/skills")
}
