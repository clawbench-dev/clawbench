package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// --- config-file hooks ---

// A backend that declares a config FILE but no writer must be refused. The old
// shape (`if backend == "codex"` then `return nil`) reported success for every
// other backend, so a binding was persisted for a provider the CLI never
// learned about — visible only as "it just doesn't work" after a restart.
func TestApplyProviderConfigToBackend_RefusesConfigFileBackendWithoutWriter(t *testing.T) {
	registerHooklessConfigFileBackend(t, "test-cli-nofile")

	p := &model.Provider{
		ID: 3, Name: "P", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.example.com/v1", APIKey: "k", AuthMode: model.ProviderAuthAuto,
		Models: []string{"m1"},
	}
	err := service.ApplyProviderConfigToBackend("test-cli-nofile", p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config-file writer")
}

// The restorer is refused on the same grounds: pretending to undo a file it
// never knew how to undo leaves a stale block behind for good.
func TestRestoreProviderConfig_RefusesConfigFileBackendWithoutRestorer(t *testing.T) {
	registerHooklessConfigFileBackend(t, "test-cli-norestore")

	err := service.RestoreProviderConfig("test-cli-norestore")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config-file restorer")
}

// A backend whose hooks ARE declared must reach them. This is the wiring half:
// a registry entry that never reaches the writer would make every binding fail
// silently at turn time instead of at bind time.
func TestApplyProviderConfigToBackend_CallsTheDeclaredWriter(t *testing.T) {
	var applied *model.Provider
	model.RegisterProviderEnvSpec("test-cli-writer", model.ProviderEnvSpec{
		ConfigOnly:       true,
		ConfigProtocols:  []string{model.ProviderProtocolOpenAI},
		ConfigFileTarget: ".test-cli/config.toml",
		ConfigFileApply: func(p *model.Provider) error {
			applied = p
			return nil
		},
		ConfigFileRestore: func() error { return nil },
	})
	t.Cleanup(func() { model.RegisterProviderEnvSpec("test-cli-writer", model.ProviderEnvSpec{}) })

	p := &model.Provider{ID: 4, Name: "W", BaseURL: "https://w", APIKey: "k"}
	require.NoError(t, service.ApplyProviderConfigToBackend("test-cli-writer", p))
	require.NotNil(t, applied, "the declared writer must be called")
	assert.Equal(t, int64(4), applied.ID)
}

// registerHooklessConfigFileBackend declares a backend that says "my provider
// lives in a config file" without saying HOW to write it.
func registerHooklessConfigFileBackend(t *testing.T, id string) {
	t.Helper()
	model.RegisterProviderEnvSpec(id, model.ProviderEnvSpec{
		ConfigOnly:       true,
		ConfigProtocols:  []string{model.ProviderProtocolOpenAI},
		ConfigFileTarget: ".test-cli/config.toml",
	})
	t.Cleanup(func() { model.RegisterProviderEnvSpec(id, model.ProviderEnvSpec{}) })
}
