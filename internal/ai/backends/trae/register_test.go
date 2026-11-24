package trae

import (
	"testing"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegistration pins the fields the rest of the project depends on by name.
// It deliberately asserts nothing about wire behavior: no frame from a real
// traecli has ever been captured (see docs/dev/trae_acp_probe_results.md).
func TestRegistration(t *testing.T) {
	p := backends.Lookup("trae")
	require.NotNil(t, p, "the trae backend must register itself in init()")

	assert.Equal(t, "trae", p.Spec.ID)
	assert.Equal(t, "trae", p.Spec.Backend)
	assert.Equal(t, "traecli", p.Spec.DefaultCmd)
	assert.Equal(t, "traecli acp serve", p.Spec.AcpCommand)
	assert.Equal(t, 26, p.Spec.SortOrder)
	assert.Equal(t, "traecli login", p.Spec.LoginCmd, "login is the documented way past -32000 AUTH_REQUIRED")

	assert.False(t, p.Spec.ACPLoadSession, "loadSession support is unknown; it must stay off rather than be guessed on")
	assert.Empty(t, p.Spec.InstallCmd, "no public install path exists; do not invent one")
	assert.Nil(t, p.ACP, "no tool mapping is justified without a captured frame")
	assert.Nil(t, p.Behavior, "no private wire shape is known")
}

// TestProviderEnvCannotBind: an empty (but PRESENT) spec is how the project
// records "this backend cannot take a provider". A nil pointer would mean the
// backend was never classified.
func TestProviderEnvCannotBind(t *testing.T) {
	p := backends.Lookup("trae")
	require.NotNil(t, p)
	require.NotNil(t, p.ProviderEnv, "the coverage test requires a classification, even a negative one")

	assert.True(t, model.HasProviderEnvEntry("trae"))
	assert.False(t, model.ProviderEnvSupported("trae", model.ProviderProtocolOpenAI))
	assert.False(t, model.ProviderEnvSupported("trae", model.ProviderProtocolAnthropic))
}

func TestOrphanCmdline(t *testing.T) {
	p := backends.Lookup("trae")
	require.NotNil(t, p)
	assert.Equal(t, "traecli", p.OrphanCmdline.Binary)
	assert.Equal(t, "acp serve", p.OrphanCmdline.Arg)
}

func TestModelSourceReportsUnknown(t *testing.T) {
	src, ok := model.LookupModelSource("trae")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindPlugin, src.Kind())

	ms, detail := discoverTraeModels()
	assert.Nil(t, ms, "a guessed catalog is worse than none")
	assert.Contains(t, detail, "unknown", "the reason must say the list is unknown")
	assert.Contains(t, detail, "traecli")
}
