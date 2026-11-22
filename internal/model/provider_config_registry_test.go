package model

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerTestSpec registers a throwaway backend spec and removes it afterwards,
// so these cases cannot leak into the shared registry other tests read.
func registerTestSpec(t *testing.T, id string, spec ProviderEnvSpec) {
	t.Helper()
	RegisterProviderEnvSpec(id, spec)
	t.Cleanup(func() {
		providerEnvMu.Lock()
		delete(providerEnvRegistry, id)
		providerEnvMu.Unlock()
	})
}

func TestConfigFileBackends_ListsOnlyConfigFileDeclarations(t *testing.T) {
	registerTestSpec(t, "test-config-file", ProviderEnvSpec{
		ConfigOnly:       true,
		ConfigFileTarget: ".test/config.toml",
	})
	registerTestSpec(t, "test-config-dir", ProviderEnvSpec{
		ConfigDirFile: "settings.json",
	})
	registerTestSpec(t, "test-config-only-no-target", ProviderEnvSpec{ConfigOnly: true})

	got := ConfigFileBackends()
	assert.Contains(t, got, "test-config-file")
	assert.NotContains(t, got, "test-config-dir")
	assert.NotContains(t, got, "test-config-only-no-target")
}

func TestProviderConfigFileHooks(t *testing.T) {
	apply := func(*Provider) error { return nil }
	restore := func() error { return nil }
	registerTestSpec(t, "test-hooks", ProviderEnvSpec{
		ConfigFileTarget:  ".test/config.toml",
		ConfigFileApply:   apply,
		ConfigFileRestore: restore,
	})

	gotApply, gotRestore := ProviderConfigFileHooks("test-hooks")
	require.NotNil(t, gotApply)
	require.NotNil(t, gotRestore)

	noApply, noRestore := ProviderConfigFileHooks("no-such-backend")
	assert.Nil(t, noApply)
	assert.Nil(t, noRestore)
}

func TestProviderConfigSecretInFile(t *testing.T) {
	registerTestSpec(t, "test-secret-in-file", ProviderEnvSpec{ConfigDirSecretInFile: true})
	registerTestSpec(t, "test-env-ref", ProviderEnvSpec{})

	assert.True(t, ProviderConfigSecretInFile("test-secret-in-file"))
	assert.False(t, ProviderConfigSecretInFile("test-env-ref"))
	assert.False(t, ProviderConfigSecretInFile("no-such-backend"))
}

func TestProviderConfigDirDeclaresModels(t *testing.T) {
	registerTestSpec(t, "test-declares-models", ProviderEnvSpec{
		ConfigDirEnv:            "TEST_CONFIG_DIR",
		ConfigDirFile:           "models.json",
		ConfigDirDeclaresModels: true,
	})
	registerTestSpec(t, "test-no-model-list", ProviderEnvSpec{
		ConfigDirEnv:  "TEST_CONFIG_DIR",
		ConfigDirFile: "models.json",
	})

	assert.True(t, ProviderConfigDirDeclaresModels("test-declares-models"))
	assert.False(t, ProviderConfigDirDeclaresModels("test-no-model-list"))
	assert.False(t, ProviderConfigDirDeclaresModels("no-such-backend"))
}

func TestProviderConfigProviderID(t *testing.T) {
	registerTestSpec(t, "test-provider-id", ProviderEnvSpec{ConfigDirProviderID: "custom"})
	registerTestSpec(t, "test-default-provider-id", ProviderEnvSpec{})

	assert.Equal(t, "custom", ProviderConfigProviderID("test-provider-id"))
	assert.Equal(t, "clawbench", ProviderConfigProviderID("test-default-provider-id"),
		"a backend that names no provider key still gets the default")
	assert.Equal(t, "clawbench", ProviderConfigProviderID("no-such-backend"))
}

func TestProviderUserConfigDir(t *testing.T) {
	registerTestSpec(t, "test-user-dir", ProviderEnvSpec{UserConfigDir: ".testcli"})

	assert.Equal(t, ".testcli", ProviderUserConfigDir("test-user-dir"))
	assert.Equal(t, "", ProviderUserConfigDir("no-such-backend"))
}

func TestProviderHasConfigDirRenderer(t *testing.T) {
	registerTestSpec(t, "test-renderer", ProviderEnvSpec{
		ConfigDirRender: func(*Provider) ([]byte, error) { return []byte("x"), nil },
	})
	registerTestSpec(t, "test-no-renderer", ProviderEnvSpec{})

	assert.True(t, ProviderHasConfigDirRenderer("test-renderer"))
	assert.False(t, ProviderHasConfigDirRenderer("test-no-renderer"))
	assert.False(t, ProviderHasConfigDirRenderer("no-such-backend"))
}

func TestProviderRenderConfigDirFile(t *testing.T) {
	registerTestSpec(t, "test-render", ProviderEnvSpec{
		ConfigDirRender: func(p *Provider) ([]byte, error) { return []byte(p.Name), nil },
	})
	registerTestSpec(t, "test-render-nil", ProviderEnvSpec{})

	got, err := ProviderRenderConfigDirFile("test-render", &Provider{Name: "x"})
	require.NoError(t, err)
	assert.Equal(t, "x", string(got))

	_, err = ProviderRenderConfigDirFile("test-render-nil", &Provider{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config-dir writer")
}

func TestProviderConfigPaths_NeedsAConfigFileTargetAndHome(t *testing.T) {
	registerTestSpec(t, "test-paths", ProviderEnvSpec{
		ConfigOnly:       true,
		ConfigFileTarget: ".testcli/config.toml",
	})
	registerTestSpec(t, "test-paths-dir", ProviderEnvSpec{ConfigDirFile: "settings.json"})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	paths := ProviderConfigPaths("test-paths")
	require.Len(t, paths, 1)
	assert.Equal(t, filepath.Join(home, ".testcli", "config.toml"), paths[0])

	assert.Nil(t, ProviderConfigPaths("test-paths-dir"), "a config-directory backend has no shared file")
	assert.Nil(t, ProviderConfigPaths("no-such-backend"))

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	assert.Nil(t, ProviderConfigPaths("test-paths"), "no home directory means no path")
}

func TestProviderModelIDs(t *testing.T) {
	assert.Nil(t, ProviderModelIDs(nil))

	p := &Provider{Models: []string{"a", " ", "b "}}
	assert.Equal(t, []string{"a", "b"}, ProviderModelIDs(p), "blank entries are dropped and the rest trimmed")

	p = &Provider{DefaultModel: " only-default "}
	assert.Equal(t, []string{"only-default"}, ProviderModelIDs(p), "the default model fills in for an empty list")

	p = &Provider{Models: []string{"a"}, DefaultModel: "b"}
	assert.Equal(t, []string{"a"}, ProviderModelIDs(p), "the list wins when it is not empty")

	assert.Empty(t, ProviderModelIDs(&Provider{}))
}
