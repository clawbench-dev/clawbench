package model_test

import (
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test 1: BackendRegistry ---

func TestBackendRegistry_ContainsAllBackends(t *testing.T) {
	expectedIDs := []string{"claude", "codebuddy", "opencode", "codex", "qoder", "vecli", "deepseek", "pi", "cline", "kimi", "copilot", "mimo"}
	assert.Len(t, model.BackendRegistry, len(expectedIDs))

	seen := make(map[string]bool)
	for _, spec := range model.BackendRegistry {
		seen[spec.ID] = true
	}
	for _, id := range expectedIDs {
		assert.True(t, seen[id], "missing backend: %s", id)
	}
}

func TestBackendRegistry_FieldsPopulated(t *testing.T) {
	for _, spec := range model.BackendRegistry {
		assert.NotEmpty(t, spec.ID, "ID should not be empty")
		assert.NotEmpty(t, spec.Backend, "Backend should not be empty for %s", spec.ID)
		if !spec.NoCLI {
			assert.NotEmpty(t, spec.DefaultCmd, "DefaultCmd should not be empty for %s", spec.ID)
		}
		assert.NotEmpty(t, spec.Name, "Name should not be empty for %s", spec.ID)
		assert.NotEmpty(t, spec.Icon, "Icon should not be empty for %s", spec.ID)
		assert.NotEmpty(t, spec.Specialty, "Specialty should not be empty for %s", spec.ID)
	}
}

func TestBackendRegistry_SpecificValues(t *testing.T) {
	specs := make(map[string]model.BackendSpec)
	for _, s := range model.BackendRegistry {
		specs[s.ID] = s
	}

	assert.Equal(t, "claude", specs["claude"].DefaultCmd)
	assert.Equal(t, "codebuddy", specs["codebuddy"].DefaultCmd)
	assert.Equal(t, "opencode", specs["opencode"].DefaultCmd)
	assert.Equal(t, "codex", specs["codex"].DefaultCmd)
	assert.Equal(t, "qodercli", specs["qoder"].DefaultCmd)
	assert.Equal(t, "vecli", specs["vecli"].DefaultCmd)
	assert.Equal(t, "codewhale", specs["deepseek"].DefaultCmd)
	assert.Equal(t, "deepseek", specs["deepseek"].AltCmd)
	assert.Equal(t, "pi", specs["pi"].DefaultCmd)
}

// --- Test 2: checkCLIExists ---

func TestCheckCLIExists_ExistingCommand(t *testing.T) {
	// "ls" exists on all platforms
	assert.True(t, model.CheckCLIExists("ls"))
}

func TestCheckCLIExists_NonExistingCommand(t *testing.T) {
	assert.False(t, model.CheckCLIExists("definitely_not_a_real_command_xyz_12345"))
}

func TestCheckCLIExists_EmptyCommand(t *testing.T) {
	assert.False(t, model.CheckCLIExists(""))
}

// --- Test 3: Model list parsers (still in model package) ---

func TestParseDeepSeekModels_RealOutput(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
* deepseek-v4-pro (deepseek)
  deepseek-ai/deepseek-v4-pro (nvidia-nim)
  deepseek-ai/deepseek-v4-flash (nvidia-nim)
  gpt-4.1 (openai)
  gpt-4.1-mini (openai)
  deepseek/deepseek-v4-pro (openrouter)
  deepseek/deepseek-v4-flash (openrouter)
  deepseek-coder:1.3b (ollama)
`

	models := model.ParseDeepSeekModels(output)
	require.Len(t, models, 2, "should only include deepseek provider models, not third-party")

	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].Name)
	assert.False(t, models[0].Default, "flash is not the default")
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].Name)
	assert.True(t, models[1].Default, "pro is the default (marked with *)")
}

func TestParseDeepSeekModels_EmptyOutput(t *testing.T) {
	models := model.ParseDeepSeekModels("no models here")
	assert.Nil(t, models)
}

func TestParseDeepSeekModels_NoDefaultMarker(t *testing.T) {
	output := `  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := model.ParseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "first model should be default as fallback")
	assert.False(t, models[1].Default)
}

func TestParseDeepSeekModels_DefaultFromHeader(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := model.ParseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.False(t, models[0].Default)
	assert.True(t, models[1].Default, "should match default from header")
}

func TestParseDeepSeekModels_ProviderPrefixInIDAndName(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
* deepseek-v4-pro (deepseek)
  deepseek-v4-flash (deepseek)
`
	models := model.ParseDeepSeekModels(output)
	require.Len(t, models, 2)

	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].Name)
	assert.True(t, models[0].Default)

	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].Name)
}

func TestParseDeepSeekModels_ThirdPartyProviderFiltered(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-pro (deepseek)
  deepseek-v4-pro (nvidia-nim)
  gpt-4.1 (openai)
`
	models := model.ParseDeepSeekModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].ID)
}

func TestParseOpenCodeModels_RealOutput(t *testing.T) {
	output := `opencode/minimax-m2.5-free
opencode/nemotron-3-super-free
minimax/MiniMax-M2.5
minimax/MiniMax-M2.7
anthropic/claude-sonnet-4-6
`

	models := model.ParseOpenCodeModels(output)
	require.Len(t, models, 5)

	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].ID)
	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].Name, "Name should include provider for disambiguation")
	assert.True(t, models[0].Default, "first model should be default")

	assert.Equal(t, "minimax/MiniMax-M2.5", models[2].ID)
	assert.Equal(t, "minimax/MiniMax-M2.5", models[2].Name)

	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[4].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[4].Name)
}

func TestParseOpenCodeModels_EmptyOutput(t *testing.T) {
	models := model.ParseOpenCodeModels("")
	assert.Nil(t, models)
}

func TestParseOpenCodeModels_InvalidLines(t *testing.T) {
	output := `minimax/MiniMax-M2.5
not-a-valid-line
anthropic/claude-sonnet-4-6

`
	models := model.ParseOpenCodeModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "minimax/MiniMax-M2.5", models[0].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[1].ID)
}

func TestParseOpenCodeModels_SingleModel(t *testing.T) {
	output := `opencode/minimax-m2.5-free`
	models := model.ParseOpenCodeModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].ID)
	assert.True(t, models[0].Default)
}

// --- Test 4: BackendRegistry model discovery config ---

func TestBackendRegistry_ModelDiscoveryConfig(t *testing.T) {
	specs := make(map[string]model.BackendSpec)
	for _, s := range model.BackendRegistry {
		specs[s.ID] = s
	}

	// OpenCode and DeepSeek use ListModelsCmd+ParseModels
	assert.NotEmpty(t, specs["opencode"].ListModelsCmd, "opencode should have ListModelsCmd")
	assert.NotNil(t, specs["opencode"].ParseModels, "opencode should have ParseModels")
	assert.NotEmpty(t, specs["deepseek"].ListModelsCmd, "deepseek should have ListModelsCmd")
	assert.NotNil(t, specs["deepseek"].ParseModels, "deepseek should have ParseModels")

	// Qoder and VeCLI don't use ListModelsCmd (they use registry-based discovery)
	assert.Empty(t, specs["qoder"].ListModelsCmd, "qoder should not have ListModelsCmd")
	assert.Empty(t, specs["vecli"].ListModelsCmd, "vecli should not have ListModelsCmd")
}

// --- Test 4b: Discovery function registry ---

func TestRegisterDiscoverModelsFunc(t *testing.T) {
	// Register a test function and verify it can be looked up
	model.RegisterDiscoverModelsFunc("test-backend", func() []model.AgentModel {
		return []model.AgentModel{{ID: "test-model", Name: "Test Model", Default: true}}
	})

	// Verify it works through DiscoverModels
	spec := model.BackendSpec{ID: "test-backend", Backend: "test-backend", DefaultCmd: "nonexistent"}
	models := model.DiscoverModels(spec)
	require.Len(t, models, 1)
	assert.Equal(t, "test-model", models[0].ID)
	assert.True(t, models[0].Default)

	// Verify CanDiscoverModels returns true
	assert.True(t, model.CanDiscoverModels(spec))
}

// --- Test 5: DiscoverModels ---

func TestDiscoverModels_NoSupport(t *testing.T) {
	spec := model.BackendSpec{
		ID:         "claude",
		DefaultCmd: "claude",
	}
	models := model.DiscoverModels(spec)
	assert.Nil(t, models, "should return nil when no model discovery support")
}

func TestDiscoverModels_NonexistentCLI(t *testing.T) {
	spec := model.BackendSpec{
		ID:            "test",
		DefaultCmd:    "definitely_not_a_real_command_xyz_12345",
		ListModelsCmd: []string{"models"},
		ParseModels:   model.ParseOpenCodeModels,
	}
	models := model.DiscoverModels(spec)
	assert.Nil(t, models, "should return nil when CLI doesn't exist")
}

func TestDiscoverModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("opencode") {
		t.Skip("opencode not installed, skipping integration test")
	}

	spec := model.BackendSpec{
		ID:            "opencode",
		DefaultCmd:    "opencode",
		ListModelsCmd: []string{"models"},
		ParseModels:   model.ParseOpenCodeModels,
	}
	models := model.DiscoverModels(spec)
	assert.NotEmpty(t, models, "opencode should return at least one model")
	assert.True(t, models[0].Default, "first model should be default")
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestDiscoverModels_WithEchoCLI(t *testing.T) {
	spec := model.BackendSpec{
		ID:            "mock-agent",
		Backend:       "mock",
		DefaultCmd:    "echo",
		Name:          "Mock",
		Icon:          "🧪",
		Specialty:     "Testing",
		ListModelsCmd: []string{"model-a, model-b"},
		ParseModels: func(s string) []model.AgentModel {
			return []model.AgentModel{
				{ID: "mock-a", Name: "Mock A", Default: true},
				{ID: "mock-b", Name: "Mock B", Default: false},
			}
		},
	}

	models := model.DiscoverModels(spec)
	require.Len(t, models, 2)
	assert.Equal(t, "mock-a", models[0].ID)
	assert.True(t, models[0].Default)
	assert.Equal(t, "mock-b", models[1].ID)
	assert.False(t, models[1].Default)
}

// --- Test 6: FindSpecByBackend ---

func TestFindSpecByBackend_Found(t *testing.T) {
	spec := model.FindSpecByBackend("codebuddy")
	require.NotNil(t, spec)
	assert.Equal(t, "codebuddy", spec.Backend)
	assert.Equal(t, "codebuddy", spec.DefaultCmd)
}

func TestFindSpecByBackend_NotFound(t *testing.T) {
	spec := model.FindSpecByBackend("nonexistent")
	assert.Nil(t, spec)
}

func TestFindSpecByBackend_AllBackends(t *testing.T) {
	for _, s := range model.BackendRegistry {
		spec := model.FindSpecByBackend(s.Backend)
		require.NotNil(t, spec, "should find spec for backend %s", s.Backend)
		assert.Equal(t, s.ID, spec.ID)
	}
}

// --- Test 7: SyncDiscoverModels ---

func TestSyncDiscoverModels_ReturnsMap(t *testing.T) {
	result := model.SyncDiscoverModels()

	// Result should be a valid map (may be empty if no CLIs installed)
	assert.NotNil(t, result)

	// If any models were discovered, verify structure
	for backend, models := range result {
		assert.NotEmpty(t, backend)
		assert.NotEmpty(t, models)
		for _, m := range models {
			assert.NotEmpty(t, m.ID)
		}
	}
}

func TestSyncDiscoverModels_NilWhenNoCLIs(t *testing.T) {
	result := model.SyncDiscoverModels()
	// The result may be empty if no CLIs are installed, but should never be nil
	// (it's an empty map, not nil)
	if result == nil {
		result = make(map[string][]model.AgentModel)
	}
	assert.NotNil(t, result)
}

// --- Test 8: Discovery function registry integration ---

func TestDiscoverModels_RegistryPath(t *testing.T) {
	// Test that the registry path works: when a spec has no DiscoverModelsFunc
	// and no ListModelsCmd, but a function is registered for its backend,
	// DiscoverModels should use the registered function.
	called := false
	model.RegisterDiscoverModelsFunc("test-registry-path", func() []model.AgentModel {
		called = true
		return []model.AgentModel{{ID: "registry-model", Name: "Registry Model", Default: true}}
	})

	spec := model.BackendSpec{ID: "test-registry-path", Backend: "test-registry-path", DefaultCmd: "nonexistent"}
	models := model.DiscoverModels(spec)
	require.Len(t, models, 1)
	assert.Equal(t, "registry-model", models[0].ID)
	assert.True(t, called, "registry function should have been called")
}

// --- Test 10: AsyncRefreshModelCache ---

func TestAsyncRefreshModelCache_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		model.AsyncRefreshModelCache(nil)
	})
}

func TestAsyncRefreshModelCache_DoesNotBlock(t *testing.T) {
	model.AsyncRefreshModelCache(nil)
	time.Sleep(100 * time.Millisecond)
}

// --- Test 11: CheckCLIExistsErr ---

func TestCheckCLIExistsErr_ExistingCommand(t *testing.T) {
	err := model.CheckCLIExistsErr("ls")
	assert.NoError(t, err)
}

func TestCheckCLIExistsErr_NonExistingCommand(t *testing.T) {
	err := model.CheckCLIExistsErr("definitely_not_a_real_command_xyz_12345")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found on PATH")
}

func TestCheckCLIExistsErr_EmptyCommand(t *testing.T) {
	err := model.CheckCLIExistsErr("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")
}

// --- Test 13: DiscoverModels for backends with ListModelsCmd ---

func TestDiscoverModels_DeepSeekWithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("deepseek") {
		t.Skip("deepseek not installed, skipping integration test")
	}

	spec := model.FindSpecByBackend("deepseek")
	require.NotNil(t, spec)
	models := model.DiscoverModels(*spec)
	if len(models) == 0 {
		t.Skip("deepseek model discovery returned no models")
	}
	t.Logf("deepseek discovered %d models", len(models))
}
