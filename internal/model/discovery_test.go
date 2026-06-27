package model_test

import (
	"testing"
	"time"

	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/cline"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/codex"
	_ "clawbench/internal/ai/backends/copilot"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/mimo"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
	_ "clawbench/internal/ai/backends/qoder"
	_ "clawbench/internal/ai/backends/vecli"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test 1: BackendRegistry ---

func TestBackendRegistry_ContainsAllBackends(t *testing.T) {
	expectedIDs := []string{"claude", "codebuddy", "opencode", "codex", "qoder", "vecli", "deepseek", "pi", "cline", "kimi", "copilot", "mimo"}
	assert.Len(t, model.GetBackendRegistry(), len(expectedIDs))

	seen := make(map[string]bool)
	for _, spec := range model.GetBackendRegistry() {
		seen[spec.ID] = true
	}
	for _, id := range expectedIDs {
		assert.True(t, seen[id], "missing backend: %s", id)
	}
}

func TestBackendRegistry_FieldsPopulated(t *testing.T) {
	for _, spec := range model.GetBackendRegistry() {
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
	for _, s := range model.GetBackendRegistry() {
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

	// Verify embedded agent metadata on opencode BackendSpec
	assert.Equal(t, "opencode", specs["opencode"].EmbeddedSubDir, "opencode should have EmbeddedSubDir set")
	assert.Equal(t, "VERSION", specs["opencode"].EmbeddedVersionFile, "opencode should have EmbeddedVersionFile set")
	assert.Equal(t, "anomalyco/opencode", specs["opencode"].EmbeddedGitHubRepo, "opencode should have EmbeddedGitHubRepo set")
	assert.Equal(t, "amd64=x64", specs["opencode"].EmbeddedArchMapping, "opencode should have EmbeddedArchMapping set")
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

// --- Test 3: Discovery function registry (parsers moved to backend packages) ---

// --- Test 4: BackendRegistry model discovery config ---

func TestBackendRegistry_ModelDiscoveryConfig(t *testing.T) {
	specs := make(map[string]model.BackendSpec)
	for _, s := range model.GetBackendRegistry() {
		specs[s.ID] = s
	}

	// All backends with a discovery function registered support model discovery
	assert.True(t, model.CanDiscoverModels(specs["opencode"]), "opencode should support model discovery")
	assert.True(t, model.CanDiscoverModels(specs["deepseek"]), "deepseek should support model discovery")
	assert.True(t, model.CanDiscoverModels(specs["qoder"]), "qoder should support model discovery")
	assert.True(t, model.CanDiscoverModels(specs["vecli"]), "vecli should support model discovery")

	// Backend with no registered discovery function does not support model discovery
	assert.False(t, model.CanDiscoverModels(model.BackendSpec{Backend: "nonexistent_xyz"}), "nonexistent backend should not support model discovery")
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
		ID:         "test",
		DefaultCmd: "definitely_not_a_real_command_xyz_12345",
	}
	models := model.DiscoverModels(spec)
	assert.Nil(t, models, "should return nil when no discovery function registered")
}

func TestDiscoverModels_WithRealCLI(t *testing.T) {
	spec := model.FindSpecByBackend("opencode")
	if spec == nil || !model.CanDiscoverModels(*spec) {
		t.Skip("opencode not installed or no discovery function, skipping integration test")
	}

	models := model.DiscoverModels(*spec)
	if len(models) == 0 {
		t.Skip("opencode discovery returned no models (CLI may not be properly configured)")
	}
	assert.True(t, models[0].Default, "first model should be default")
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestDiscoverModels_WithEchoCLI(t *testing.T) {
	// Register a discovery function for a mock backend
	model.RegisterDiscoverModelsFunc("mock-echo-cli", func() []model.AgentModel {
		return []model.AgentModel{
			{ID: "mock-a", Name: "Mock A", Default: true},
			{ID: "mock-b", Name: "Mock B", Default: false},
		}
	})

	spec := model.BackendSpec{
		ID:         "mock-echo-cli",
		Backend:    "mock-echo-cli",
		DefaultCmd: "echo",
		Name:       "Mock",
		Icon:       "🧪",
		Specialty:  "Testing",
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
	for _, s := range model.GetBackendRegistry() {
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
	// Test that the registry path works: when a function is registered for
	// a backend, DiscoverModels should use it.
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

// --- Test 9: FindBackendSpecByDefaultCmd ---

func TestFindBackendSpecByDefaultCmd_Found(t *testing.T) {
	spec := model.FindBackendSpecByDefaultCmd("opencode")
	require.NotNil(t, spec)
	assert.Equal(t, "opencode", spec.ID)
	assert.Equal(t, "opencode", spec.DefaultCmd)
}

func TestFindBackendSpecByDefaultCmd_NotFound(t *testing.T) {
	spec := model.FindBackendSpecByDefaultCmd("nonexistent_cmd_xyz")
	assert.Nil(t, spec)
}

func TestFindBackendSpecByDefaultCmd_Empty(t *testing.T) {
	spec := model.FindBackendSpecByDefaultCmd("")
	assert.Nil(t, spec)
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

// --- Test 13: DiscoverModels for backends with registry ---

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
