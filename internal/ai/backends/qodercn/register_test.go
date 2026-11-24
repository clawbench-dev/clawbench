package qodercn

import (
	"testing"

	"clawbench/internal/ai/backends"
	// Blank import for its init(): two tests below compare the CN spec
	// against the international build, which only exists once that package
	// registers itself.
	_ "clawbench/internal/ai/backends/qoder"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestQoderCNBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin, "qodercn should be registered in backends registry")
	assert.Equal(t, "qodercn", plugin.ID)
	assert.Equal(t, "qodercn", plugin.Spec.Backend,
		"Backend is what ACP event dispatch keys on; it must match the plugin id")
}

func TestQoderCNBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	assert.Equal(t, "Qoder CN", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.Specialty)
	assert.Equal(t, "qoderclicn", plugin.Spec.DefaultCmd)
	assert.Equal(t, "qodercn", plugin.Spec.AltCmd,
		"the CN package ships both bins; detection must accept either")
	assert.Equal(t, "qoderclicn --acp", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession, "initialize advertises loadSession:true")
	assert.Equal(t, "npm install -g @qodercn-ai/qoderclicn", plugin.Spec.InstallCmd)
	assert.Equal(t, 20, plugin.Spec.SortOrder)
}

// LoginCmd must name the CN binary. The two distributions keep separate
// credentials, so the international command would sign the user into the wrong
// product — and the CN binary pointed at the international config root deletes
// that credential outright.
func TestQoderCNBackendPlugin_LoginCmdIsCNDistinct(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	assert.Equal(t, "qoderclicn login", plugin.Spec.LoginCmd)

	intl := backends.Lookup("qoder")
	require.NotNil(t, intl)
	assert.NotEqual(t, intl.Spec.LoginCmd, plugin.Spec.LoginCmd,
		"the two Qoder builds must never share a login command")
	assert.Equal(t, "qodercli login", intl.Spec.LoginCmd)
}

// AcpCommand must spawn a binary detection also checks, or a user who passes
// detection would fail to spawn.
func TestQoderCNBackendPlugin_AcpCommandMatchesDetection(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	bins := []string{plugin.Spec.DefaultCmd, plugin.Spec.AltCmd}
	assert.Contains(t, bins, "qoderclicn",
		"AcpCommand starts with %q, which must be one of the detected bins", plugin.Spec.AcpCommand)
}

// The two builds are separate products: nothing about the CN spec may collide
// with the international one (id, binary, config-derived paths).
func TestQoderCNBackendPlugin_DoesNotCollideWithInternational(t *testing.T) {
	cn := backends.Lookup("qodercn")
	intl := backends.Lookup("qoder")
	require.NotNil(t, cn)
	require.NotNil(t, intl)

	assert.NotEqual(t, cn.ID, intl.ID)
	assert.NotEqual(t, cn.Spec.Backend, intl.Spec.Backend)
	assert.NotEqual(t, cn.Spec.DefaultCmd, intl.Spec.DefaultCmd)
	assert.NotEqual(t, cn.Spec.AcpCommand, intl.Spec.AcpCommand)
	assert.NotEqual(t, cn.Spec.InstallCmd, intl.Spec.InstallCmd)
	assert.NotEqual(t, cn.Spec.SortOrder, intl.Spec.SortOrder,
		"identical SortOrder makes the picker order depend on map iteration")
}

// The CN build's user skills root follows its brand (".qoder-cn/skills") while
// the project root stays ".qoder" — verified in the shipped bundle:
// `saa={user:".qoder-cn",project:".qoder"}`. Getting the user dir wrong makes
// ClawBench inject a second copy of every skill the CN build already loaded.
func TestQoderCNBackendPlugin_DeclaresCNSkillsDirs(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	assert.True(t, plugin.Spec.AutoLoadsNativeSkills)
	assert.Equal(t, []string{".qoder-cn/skills", ".agents/skills"}, plugin.Spec.NativeSkillsDirs)
}
