package cursor

import (
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestCursorBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin, "cursor should be registered in backends registry")
	assert.Equal(t, "cursor", plugin.ID)
}

func TestCursorBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin)

	assert.Equal(t, "cursor", plugin.Spec.ID)
	assert.Equal(t, "cursor", plugin.Spec.Backend)
	assert.Equal(t, "cursor-agent", plugin.Spec.DefaultCmd)
	// No AltCmd: `agent` collides with the Grok CLI's own `agent` binary, so a
	// generic alias would create a phantom Cursor agent on Grok-only installs.
	assert.Empty(t, plugin.Spec.AltCmd, "`agent` collides with Grok's CLI; cursor-agent is the only safe name")
	assert.Equal(t, "Cursor", plugin.Spec.Name)
	assert.Equal(t, "cursor-agent acp", plugin.Spec.AcpCommand)
	assert.Equal(t, "curl https://cursor.com/install -fsS | bash", plugin.Spec.InstallCmd)
	assert.Equal(t, 9, plugin.Spec.SortOrder)
	assert.NotEmpty(t, plugin.Spec.NativeSkillsDirs)
}

// The ACP command must spawn the same binary that detection checks, or a user
// with only the `cursor-agent` name would pass detection and then fail to
// spawn. Both use the unambiguous `cursor-agent`, never the generic `agent`.
func TestCursorBackendPlugin_AcpCommandMatchesDefaultCmd(t *testing.T) {
	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin)
	assert.True(t, strings.HasPrefix(plugin.Spec.AcpCommand, plugin.Spec.DefaultCmd),
		"AcpCommand %q must invoke DefaultCmd %q", plugin.Spec.AcpCommand, plugin.Spec.DefaultCmd)
}

// cursor's initialize advertises loadSession:true, so the explicit acp-load
// endpoint may attempt session/load.
func TestCursorBackendPlugin_AdvertisesLoadSession(t *testing.T) {
	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.ACPLoadSession, "cursor advertises loadSession:true")
}

// No CLI factory is registered: chat must always go through the ACP stdio
// transport (the official `agent acp` subcommand).
func TestCursorBackendPlugin_IsACPIOnly(t *testing.T) {
	assert.False(t, ai.BackendSupportsCLI("cursor"), "cursor is ACP-only")

	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.ACP, "cursor needs no tool-name remapping yet: tools fall back to the shared alias table")
}

// cursor loads its own skills (verified empirically), so ClawBench must not
// re-inject them. The declared directories must cover every home dir cursor
// reads, or a skill from an undeclared one gets injected a second time.
func TestCursorBackendPlugin_DeclaresItsNativeSkillDirs(t *testing.T) {
	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.AutoLoadsNativeSkills, "cursor loads its own skills")
	assert.ElementsMatch(t, []string{
		".cursor/skills", ".claude/skills", ".codex/skills",
		".grok/skills", ".agents/skills",
	}, plugin.Spec.NativeSkillsDirs)
}

// ThinkingEffortLevels must stay empty: cursor advertises no thought_level
// config option (effort lives in the model id). A non-empty list here would
// surface a thinking-effort control that can never be set.
func TestCursorBackendPlugin_HasNoThinkingEffortLevels(t *testing.T) {
	plugin := backends.Lookup("cursor")
	require.NotNil(t, plugin)
	assert.Empty(t, plugin.Spec.ThinkingEffortLevels,
		"cursor encodes effort in the model id; a separate control would be dead")
}
