package dsh

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestDshBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("dsh")
	require.NotNil(t, plugin, "dsh should be registered in backends registry")
	assert.Equal(t, "dsh", plugin.ID)
}

func TestDshBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("dsh")
	require.NotNil(t, plugin)

	assert.Equal(t, "dsh", plugin.Spec.ID)
	assert.Equal(t, "dsh", plugin.Spec.Backend)
	assert.Equal(t, "dsh", plugin.Spec.DefaultCmd)
	assert.Equal(t, "DeepSeek Harness", plugin.Spec.Name)
	assert.Equal(t, "dsh --profile acp", plugin.Spec.AcpCommand)
	assert.Equal(t, "npm install -g @deepseek-ai/dsh", plugin.Spec.InstallCmd)
	assert.Equal(t, []string{"off", "low", "high", "max"}, plugin.Spec.ThinkingEffortLevels)
	assert.NotZero(t, plugin.Spec.SortOrder)
}

// dsh's session/load answers -32601: the automation-only ACP surface omits it.
// Keeping ACPLoadSession false makes the explicit acp-load endpoint return 501
// instead of attempting a method that never exists. Automatic crash recovery
// uses session/resume and is unaffected.
func TestDshBackendPlugin_DoesNotAdvertiseLoadSession(t *testing.T) {
	plugin := backends.Lookup("dsh")
	require.NotNil(t, plugin)
	assert.False(t, plugin.Spec.ACPLoadSession, "dsh has no session/load")
}

// No CLI factory is registered: `dsh` has no CLI transport, so chat must always
// go through the ACP stdio transport.
func TestDshBackendPlugin_IsACPIOnly(t *testing.T) {
	assert.False(t, ai.BackendSupportsCLI("dsh"), "dsh is ACP-only")

	plugin := backends.Lookup("dsh")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.ACP, "dsh needs no tool-name remapping: tools are reported canonically over ACP")
}

// TestDshToolTitlesResolveCanonically pins the assumption that justifies the
// missing ACPPlugin: dsh sends lowercase tool titles with kind=other, and the
// shared lowercase alias table must map them to the canonical names the
// frontend uses for icons and summaries. If dsh ever changes its title casing
// (or the alias table loses an entry) this fails instead of silently degrading
// every tool card to a generic wrench.
func TestDshToolTitlesResolveCanonically(t *testing.T) {
	cases := map[string]string{
		"read":  "Read",
		"write": "Write",
		"bash":  "Bash",
		"grep":  "Grep",
	}
	for title, want := range cases {
		got := ai.ExtractToolNameForTest(title, "other", "dsh")
		assert.Equal(t, want, got, "title %q should resolve to %q", title, want)
	}
}
