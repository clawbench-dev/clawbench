package model_test

import (
	"testing"

	_ "clawbench/internal/ai/backends/antigravity"
	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/codex"
	_ "clawbench/internal/ai/backends/copilot"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/dsh"
	_ "clawbench/internal/ai/backends/grok"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/mimo"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
	_ "clawbench/internal/ai/backends/qoder"
	_ "clawbench/internal/ai/backends/vecli"
	_ "clawbench/internal/ai/backends/zcode"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test 1: BackendRegistry ---

func TestBackendRegistry_ContainsAllBackends(t *testing.T) {
	expectedIDs := []string{"claude", "codebuddy", "opencode", "codex", "qoder", "vecli", "deepseek", "pi", "kimi", "copilot", "mimo", "grok", "antigravity", "zcode", "dsh"}
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

	// Verify install command on opencode BackendSpec
	assert.Equal(t, "npm install -g opencode-ai", specs["opencode"].InstallCmd, "opencode should have InstallCmd set")

	// ACP-only backends: agy is detected via DefaultCmd, ACP via agy-acp bridge
	assert.Equal(t, "agy", specs["antigravity"].DefaultCmd)
	assert.Equal(t, "npx -y agy-acp@latest", specs["antigravity"].AcpCommand)
	assert.Equal(t, "grok agent stdio", specs["grok"].AcpCommand)

	// DeepSeek Harness is ACP-only and detected via the `dsh` binary. Its
	// session/load is unsupported (-32601), so ACPLoadSession must stay false
	// or the explicit acp-load endpoint would attempt a method that never exists.
	assert.Equal(t, "dsh", specs["dsh"].DefaultCmd)
	assert.Equal(t, "dsh --profile acp", specs["dsh"].AcpCommand)
	assert.False(t, specs["dsh"].ACPLoadSession, "dsh does not implement session/load")
	assert.Equal(t, "npm install -g @deepseek-ai/dsh", specs["dsh"].InstallCmd)
	assert.False(t, model.BackendSupportsCLI("dsh"), "dsh is ACP-only: no CLI factory")
}

func TestBackendSupportsCLI_ComputedFromFactoryRegistry(t *testing.T) {
	// Backends with a CLI factory report true
	assert.True(t, model.BackendSupportsCLI("claude"))
	assert.True(t, model.BackendSupportsCLI("opencode"))
	assert.True(t, model.BackendSupportsCLI("kimi"))
	// grok registers a streaming-json CLI fallback in addition to ACP
	assert.True(t, model.BackendSupportsCLI("grok"), "grok has a CLI fallback factory")

	// Truly ACP-only backends (e.g. antigravity) report false
	assert.False(t, model.BackendSupportsCLI("antigravity"), "antigravity is ACP-only and has no CLI factory")
}

func TestBackendSupportsCLI_UnwiredFnFallsBack(t *testing.T) {
	// When the function variable is not wired (isolated test), the helper
	// falls back to false instead of panicking.
	orig := model.BackendSupportsCLIFn
	t.Cleanup(func() { model.BackendSupportsCLIFn = orig })
	model.BackendSupportsCLIFn = nil

	assert.False(t, model.BackendSupportsCLI("claude"))
	assert.False(t, model.BackendSupportsCLI("grok"))
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

// --- Test 4b: Model source registry ---

func TestRegisterModelSource(t *testing.T) {
	model.RegisterModelSource(model.StaticSource("test-backend", "", []model.AgentModel{
		{ID: "test-model", Name: "Test Model", Default: true},
	}))

	spec := model.BackendSpec{ID: "test-backend", Backend: "test-backend", DefaultCmd: "nonexistent"}
	models := model.DiscoverModels("test-backend")
	require.Len(t, models, 1)
	assert.Equal(t, "test-model", models[0].ID)
	assert.True(t, models[0].Default)

	assert.True(t, model.CanDiscoverModels(spec))
}

func TestDiscoverWithDetail_ReportsProbeReason(t *testing.T) {
	model.RegisterModelSource(model.PluginSource("test-detail-backend", func() ([]model.AgentModel, string) {
		return nil, "no model list found; tried: /a, /b"
	}))

	models, detail := model.DiscoverWithDetail("test-detail-backend")
	assert.Empty(t, models)
	assert.Equal(t, "no model list found; tried: /a, /b", detail)
}

func TestDiscoverWithDetail_Unregistered(t *testing.T) {
	models, detail := model.DiscoverWithDetail("no-detail")
	assert.Nil(t, models)
	assert.Empty(t, detail, "a backend with no source has no failure story to tell")
}

// --- Test 5: DiscoverModels ---

func TestDiscoverModels_NoSupport(t *testing.T) {
	models := model.DiscoverModels("no-source-registered-xyz")
	assert.Nil(t, models, "should return nil when no model source is registered")
}

func TestDiscoverModels_NonexistentCLI(t *testing.T) {
	models := model.DiscoverModels("test-nonexistent-cli-xyz")
	assert.Nil(t, models, "should return nil when no model source is registered")
}

func TestDiscoverModels_WithRealCLI(t *testing.T) {
	if !model.HasModelSource("opencode") {
		t.Skip("opencode has no model source registered, skipping integration test")
	}

	models := model.DiscoverModels("opencode")
	if len(models) == 0 {
		t.Skip("opencode discovery returned no models (CLI may not be properly configured)")
	}
	assert.True(t, models[0].Default, "first model should be default")
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestDiscoverModels_StaticSource(t *testing.T) {
	model.RegisterModelSource(model.StaticSource("mock-static-source", "", []model.AgentModel{
		{ID: "mock-a", Name: "Mock A", Default: true},
		{ID: "mock-b", Name: "Mock B"},
	}))

	models := model.DiscoverModels("mock-static-source")
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

// --- Test 7: Registered model sources ---

func TestBackendsWithModelDiscovery_NonEmpty(t *testing.T) {
	backends := model.BackendsWithModelDiscovery()
	assert.NotEmpty(t, backends, "the linked backend packages register model sources at init")
	for _, b := range backends {
		assert.NotEmpty(t, b)
		assert.True(t, model.HasModelSource(b))
	}
}

// --- Test 8: Model source registry integration ---

func TestDiscoverModels_RegistryPath(t *testing.T) {
	called := false
	model.RegisterModelSource(model.PluginSource("test-registry-path", func() ([]model.AgentModel, string) {
		called = true
		return []model.AgentModel{{ID: "registry-model", Name: "Registry Model", Default: true}}, ""
	}))

	models := model.DiscoverModels("test-registry-path")
	require.Len(t, models, 1)
	assert.Equal(t, "registry-model", models[0].ID)
	assert.True(t, called, "registered source should have been called")
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

// --- Test 10: discovery cache invalidation ---

func TestInvalidateAllDiscoveredModels_ForcesReprobe(t *testing.T) {
	restore := model.SnapshotModelSourcesForTest()
	defer restore()
	model.ResetModelSourcesForTest()

	calls := 0
	model.RegisterModelSource(model.PluginSource("inv-test", func() ([]model.AgentModel, string) {
		calls++
		return []model.AgentModel{{ID: "m"}}, ""
	}))

	model.DiscoverModels("inv-test")
	model.DiscoverModels("inv-test")
	assert.Equal(t, 1, calls, "the second lookup should be served from cache")

	model.InvalidateAllDiscoveredModels()
	model.DiscoverModels("inv-test")
	assert.Equal(t, 2, calls, "an explicit rescan must re-probe")
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

// --- Test 13: DiscoverModels for backends with a registered source ---

func TestDiscoverModels_DeepSeekWithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("deepseek") {
		t.Skip("deepseek not installed, skipping integration test")
	}

	require.True(t, model.HasModelSource("deepseek"))
	models := model.DiscoverModels("deepseek")
	if len(models) == 0 {
		t.Skip("deepseek model discovery returned no models")
	}
	t.Logf("deepseek discovered %d models", len(models))
}
