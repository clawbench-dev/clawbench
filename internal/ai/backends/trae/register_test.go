package trae

import (
	"testing"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegistration pins the fields the rest of the project depends on by name.
// The capability fields come from a real initialize against traecli 0.216.0
// (see docs/dev/trae_acp_probe_results.md); the wire shape of a TURN is still
// unverified because a session needs an enterprise login.
func TestRegistration(t *testing.T) {
	p := backends.Lookup("trae")
	require.NotNil(t, p, "the trae backend must register itself in init()")

	assert.Equal(t, "trae", p.Spec.ID)
	assert.Equal(t, "trae", p.Spec.Backend)
	assert.Equal(t, "traecli", p.Spec.DefaultCmd)
	assert.Equal(t, "traecli acp serve", p.Spec.AcpCommand)
	assert.Equal(t, 26, p.Spec.SortOrder)
	assert.Equal(t, "traecli login", p.Spec.LoginCmd, "login is the documented way past -32000 AUTH_REQUIRED")

	assert.True(t, p.Spec.ACPLoadSession, "initialize advertises loadSession:true (measured on 0.216.0)")
	assert.Equal(t, TraeInstallCmd, p.Spec.InstallCmd, "the official installer is the only install path")
	assert.NotEmpty(t, p.Spec.InstallCmd, "the installer is public: sh -c \"$(curl -fsSL https://trae.cn/trae-cli/install_v2.sh)\"")
	assert.True(t, p.Spec.AutoLoadsNativeSkills, "traecli loads ${TRAE_HOME:-$HOME/.trae}/skills itself")
	assert.Equal(t, []string{".trae/skills"}, p.Spec.NativeSkillsDirs)
	assert.Nil(t, p.ACP, "no tool mapping is justified without a captured frame")
	assert.Nil(t, p.Behavior, "no private wire shape is known")
}

// TestProviderEnvKeyOnly: the only credential traecli accepts from the
// environment is the enterprise login token. There is no endpoint slot, so a
// provider carrying a base URL must be refused rather than silently ignored.
func TestProviderEnvKeyOnly(t *testing.T) {
	p := backends.Lookup("trae")
	require.NotNil(t, p)
	require.NotNil(t, p.ProviderEnv, "the coverage test requires a classification")

	assert.True(t, model.HasProviderEnvEntry("trae"))
	assert.True(t, model.BackendIsKeyOnly("trae"))

	withEndpoint := (&model.Provider{
		ID: 1, Name: "gw", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "k",
	}).Normalized()
	if _, err := model.BuildProviderEnv("trae", &withEndpoint, ""); err == nil {
		t.Fatal("a provider with an endpoint must be refused: traecli has no endpoint slot")
	}

	keyOnly := (&model.Provider{
		ID: 2, Name: "pat", Protocol: model.ProviderProtocolOpenAI, APIKey: "tok",
	}).Normalized()
	env, err := model.BuildProviderEnv("trae", &keyOnly, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"TRAECLI_PERSONAL_ACCESS_TOKEN=tok"}, env)
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
