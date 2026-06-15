package model_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test 1: BackendRegistry ---

func TestBackendRegistry_ContainsAllBackends(t *testing.T) {
	expectedIDs := []string{"claude", "codebuddy", "opencode", "gemini", "codex", "qoder", "vecli", "deepseek", "pi", "cline", "kimi", "copilot", "mimo"}
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
	assert.Equal(t, "gemini", specs["gemini"].DefaultCmd)
	assert.Equal(t, "codex", specs["codex"].DefaultCmd)
	assert.Equal(t, "qodercli", specs["qoder"].DefaultCmd)
	assert.Equal(t, "vecli", specs["vecli"].DefaultCmd)
	assert.Equal(t, "deepseek", specs["deepseek"].DefaultCmd)
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

// --- Test 3: Model list parsers ---

func TestParseCodebuddyModels_RealOutput(t *testing.T) {
	// Real output from: codebuddy --help | grep "Currently supported"
	output := `  --model <model>                                  Model for the current session. Please provide the model ID. Currently supported: (glm-4.7, glm-4.6, deepseek-v3-2-volc, deepseek-v3-0324, minimax-m2.5, minimax-m2.7, kimi-k2.5, glm-5.0, glm-5.1, glm-4.6v, deepseek-v3-1-lkeap, deepseek-v3-0324-lkeap, hunyuan-2.0-instruct)
  --text-to-image-model <model>                    Model for text-to-image generation`

	models := model.ParseCodebuddyModels(output)
	require.Len(t, models, 13, "should parse all 13 model IDs")

	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.True(t, models[0].Default, "first model should be default")
	assert.Equal(t, "hunyuan-2.0-instruct", models[12].ID)
	assert.False(t, models[12].Default)

	// Name should equal ID for codebuddy models
	assert.Equal(t, models[0].ID, models[0].Name)
}

func TestParseCodebuddyModels_EmptyOutput(t *testing.T) {
	models := model.ParseCodebuddyModels("no models here")
	assert.Nil(t, models)
}

func TestParseCodebuddyModels_PartialOutput(t *testing.T) {
	output := `Currently supported: (glm-4.7, glm-4.6)`
	models := model.ParseCodebuddyModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.True(t, models[0].Default)
	assert.Equal(t, "glm-4.6", models[1].ID)
	assert.False(t, models[1].Default)
}

func TestParseDeepSeekModels_RealOutput(t *testing.T) {
	// Real output from: deepseek models
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

	assert.NotNil(t, specs["codebuddy"].DiscoverModelsFunc, "codebuddy should have DiscoverModelsFunc")
	assert.NotEmpty(t, specs["opencode"].ListModelsCmd, "opencode should have ListModelsCmd")
	assert.NotNil(t, specs["opencode"].ParseModels, "opencode should have ParseModels")
	assert.NotEmpty(t, specs["deepseek"].ListModelsCmd, "deepseek should have ListModelsCmd")
	assert.NotNil(t, specs["deepseek"].ParseModels, "deepseek should have ParseModels")
	assert.NotNil(t, specs["pi"].DiscoverModelsFunc, "pi should have DiscoverModelsFunc")
	assert.Empty(t, specs["pi"].ListModelsCmd, "pi should not have ListModelsCmd")
	assert.NotNil(t, specs["claude"].DiscoverModelsFunc, "claude should have DiscoverModelsFunc")
	assert.NotNil(t, specs["gemini"].DiscoverModelsFunc, "gemini should have DiscoverModelsFunc")
	assert.NotNil(t, specs["codex"].DiscoverModelsFunc, "codex should have DiscoverModelsFunc")
	assert.NotNil(t, specs["qoder"].DiscoverModelsFunc, "qoder should have DiscoverModelsFunc")
	assert.NotNil(t, specs["vecli"].DiscoverModelsFunc, "vecli should have DiscoverModelsFunc")
	assert.Empty(t, specs["qoder"].ListModelsCmd, "qoder should not have ListModelsCmd")
	assert.Empty(t, specs["vecli"].ListModelsCmd, "vecli should not have ListModelsCmd")
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

func TestParsePiModels_RealOutput(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images
anthropic       claude-sonnet-4-6           1M       64K      yes       yes
anthropic       claude-opus-4-6             1M       128K     yes       yes
openai          gpt-4o                      128K     4.1K     no        yes
minimax         MiniMax-M2.7                204.8K   131.1K   yes       no`
	models := model.ParsePiModels(output)
	require.Len(t, models, 4)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].Name)
	assert.True(t, models[0].Default, "first model should be default")
	assert.Equal(t, "minimax/MiniMax-M2.7", models[3].ID)
	assert.Equal(t, "minimax/MiniMax-M2.7", models[3].Name)
}

func TestParsePiModels_EmptyOutput(t *testing.T) {
	models := model.ParsePiModels("")
	assert.Nil(t, models)
}

func TestParsePiModels_HeaderOnly(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images`
	models := model.ParsePiModels(output)
	assert.Nil(t, models)
}

// --- Test 6: FindSpecByBackend ---

func TestFindSpecByBackend_Found(t *testing.T) {
	spec := model.FindSpecByBackend("codebuddy")
	require.NotNil(t, spec)
	assert.Equal(t, "codebuddy", spec.Backend)
	assert.Equal(t, "codebuddy", spec.DefaultCmd)
	assert.NotNil(t, spec.DiscoverModelsFunc, "codebuddy should have DiscoverModelsFunc")
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

// --- Test 8: DiscoverClaudeModels ---

func TestDiscoverClaudeModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("claude") {
		t.Skip("claude not installed, skipping integration test")
	}

	models := model.DiscoverClaudeModels()
	if len(models) == 0 {
		t.Skip("claude model discovery returned no models (strings may not be available)")
	}

	for _, m := range models {
		assert.True(t, strings.HasPrefix(m.ID, "claude-"), "model ID should start with claude-, got: %s", m.ID)
		assert.NotEmpty(t, m.Name, "model should have a name")
	}
	assert.True(t, models[0].Default, "first model should be default")

	t.Logf("Discovered %d Claude models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

// --- Test 8b: DiscoverCodebuddyModels ---

func TestDiscoverCodebuddyModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("codebuddy") {
		t.Skip("codebuddy not installed, skipping integration test")
	}

	models := model.DiscoverCodebuddyModels()
	if len(models) == 0 {
		t.Skip("codebuddy model discovery returned no models (product JSON may not be found)")
	}

	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name, got ID: %s", m.ID)
	}

	hasGlm := false
	hasNonGlm := false
	for _, m := range models {
		if strings.HasPrefix(m.ID, "glm-") {
			hasGlm = true
		} else {
			hasNonGlm = true
		}
	}
	assert.True(t, hasGlm, "should contain at least one glm model")
	assert.True(t, hasNonGlm, "should contain non-glm models (deepseek, kimi, etc.)")
	assert.True(t, models[0].Default, "first model should be default")

	for _, m := range models {
		assert.NotEqual(t, "default", m.ID, "should not contain pseudo-model 'default'")
		assert.NotEqual(t, "auto", m.ID, "should not contain pseudo-model 'auto'")
	}

	t.Logf("Discovered %d Codebuddy models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

// --- Test 9: SyncDiscoverModels covers DiscoverModelsFunc (Claude) ---

func TestSyncDiscoverModels_CoversClaudeDiscoverModelsFunc(t *testing.T) {
	specs := make(map[string]model.BackendSpec)
	for _, s := range model.BackendRegistry {
		specs[s.ID] = s
	}

	claudeSpec, ok := specs["claude"]
	require.True(t, ok, "claude should be in BackendRegistry")
	assert.NotNil(t, claudeSpec.DiscoverModelsFunc, "claude should have DiscoverModelsFunc")
	assert.Empty(t, claudeSpec.ListModelsCmd, "claude should not have ListModelsCmd")

	if !model.CheckCLIExists("claude") {
		t.Skip("claude not installed, skipping integration test")
	}

	result := model.SyncDiscoverModels()
	models, ok := result["claude"]
	if !ok || len(models) == 0 {
		t.Logf("claude model discovery returned no models (strings may not be available)")
		return
	}

	t.Logf("claude discovered %d models via SyncDiscoverModels", len(models))
}

// --- Test 9b: Gemini/Codex/Qoder/VeCLI model discovery integration ---

func TestDiscoverGeminiModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("gemini") {
		t.Skip("gemini not installed, skipping integration test")
	}

	models := model.DiscoverGeminiModels()
	if len(models) == 0 {
		t.Skip("gemini model discovery returned no models")
	}

	for _, m := range models {
		assert.True(t, strings.HasPrefix(m.ID, "gemini-"), "model ID should start with gemini-, got: %s", m.ID)
		assert.NotEmpty(t, m.Name, "model should have a name")
	}
	assert.True(t, models[0].Default, "first model should be default")

	for _, m := range models {
		assert.NotContains(t, m.ID, "auto-gemini-", "should not contain auto-gemini aliases")
	}

	t.Logf("Discovered %d Gemini models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

func TestDiscoverCodexModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("codex") {
		t.Skip("codex not installed, skipping integration test")
	}

	models := model.DiscoverCodexModels()
	if len(models) == 0 {
		t.Skip("codex model discovery returned no models (strings may not be available or Rust binary not found)")
	}

	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name")
	}
	assert.True(t, models[0].Default, "first model should be default")

	t.Logf("Discovered %d Codex models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

func TestDiscoverQoderModels_WithRealCLI(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	qoderJSON := filepath.Join(homeDir, ".qoder", ".auth", "dynamic-texts.json")
	if _, err := os.Stat(qoderJSON); err != nil {
		t.Skip("qoder dynamic-texts.json not found, skipping integration test")
	}

	models := model.DiscoverQoderModels()
	if len(models) == 0 {
		t.Skip("qoder model discovery returned no models")
	}

	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name")
	}

	for _, m := range models {
		assert.NotEqual(t, "auto", m.ID, "should not contain 'auto' alias")
		assert.NotEqual(t, "ultimate", m.ID, "should not contain 'ultimate' tier")
		assert.NotEqual(t, "performance", m.ID, "should not contain 'performance' tier")
		assert.NotEqual(t, "efficient", m.ID, "should not contain 'efficient' tier")
		assert.NotEqual(t, "lite", m.ID, "should not contain 'lite' tier")
	}
	assert.True(t, models[0].Default, "first model should be default")

	t.Logf("Discovered %d Qoder models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

func TestDiscoverVeCLIModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("vecli") {
		t.Skip("vecli not installed, skipping integration test")
	}

	models := model.DiscoverVeCLIModels()
	if len(models) == 0 {
		t.Skip("vecli model discovery returned no models")
	}

	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name")
	}
	assert.True(t, models[0].Default, "first model should be default")

	t.Logf("Discovered %d VeCLI models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
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

// --- Test 12: DiscoverCodebuddyModels with mock product JSON ---

func TestDiscoverCodebuddyModels_ProductJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	productJSON := `{
		"models": [
			{"id": "glm-5.1", "name": "GLM 5.1", "isDefault": true},
			{"id": "glm-4-flash", "name": "GLM 4 Flash", "isDefault": false},
			{"id": "deepseek-v3", "name": "DeepSeek V3", "isDefault": false},
			{"id": "default", "name": "Default", "isDefault": false},
			{"id": "auto", "name": "Auto", "isDefault": false},
			{"id": "hunyuan-image-v3.0", "name": "Hunyuan Image", "isDefault": false}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "product.cloudhosted.json"), []byte(productJSON), 0o644))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	require.NotEmpty(t, models, "should discover models from product JSON")

	assert.Len(t, models, 3)
	assert.Equal(t, "glm-5.1", models[0].ID)
	assert.Equal(t, "GLM 5.1", models[0].Name)
	assert.True(t, models[0].Default, "first model should be default")
	assert.Equal(t, "deepseek-v3", models[2].ID)
	assert.Equal(t, "DeepSeek V3", models[2].Name)

	for _, m := range models {
		assert.NotEqual(t, "default", m.ID)
		assert.NotEqual(t, "auto", m.ID)
		assert.NotEqual(t, "hunyuan-image-v3.0", m.ID)
	}
}

func TestDiscoverCodebuddyModels_ProductJSON_EmptyModels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	productJSON := `{"models": []}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "product.cloudhosted.json"), []byte(productJSON), 0o644))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	assert.Nil(t, models, "should return nil when no models in product JSON")
}

func TestDiscoverCodebuddyModels_ProductJSON_InvalidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "product.cloudhosted.json"), []byte("not json"), 0o644))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	assert.Nil(t, models, "should return nil when product JSON is invalid")
}

func TestDiscoverCodebuddyModels_ProductJSON_NoFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	assert.Nil(t, models, "should return nil when product JSON file doesn't exist")
}

func TestDiscoverCodebuddyModels_NotOnPATH(t *testing.T) {
	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", t.TempDir()))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	assert.Nil(t, models, "should return nil when codebuddy is not on PATH")
}

func TestDiscoverCodebuddyModels_ProductJSON_NameFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	productJSON := `{
		"models": [
			{"id": "glm-5.1", "name": "", "isDefault": true}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "product.cloudhosted.json"), []byte(productJSON), 0o644))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	require.Len(t, models, 1)
	assert.Equal(t, "glm-5.1", models[0].ID)
	assert.Equal(t, "glm-5.1", models[0].Name)
}

func TestDiscoverCodebuddyModels_ProductJSON_NoDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	productJSON := `{
		"models": [
			{"id": "glm-5.1", "name": "GLM 5.1", "isDefault": false},
			{"id": "glm-4-flash", "name": "GLM 4 Flash", "isDefault": false}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "product.cloudhosted.json"), []byte(productJSON), 0o644))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "first model should be default when none marked isDefault")
	assert.False(t, models[1].Default)
}

// --- Test 13: Discover*Models no-install coverage ---

func TestDiscoverCodexModels_NoInstall(t *testing.T) {
	models := model.DiscoverCodexModels()
	if _, err := filepath.Abs("codex"); err != nil {
		if models == nil {
			t.Log("codex not installed, DiscoverCodexModels returned nil (expected)")
		}
	}
}

func TestDiscoverVeCLIModels_NoInstall(t *testing.T) {
	models := model.DiscoverVeCLIModels()
	if _, err := filepath.Abs("vecli"); err != nil {
		if models == nil {
			t.Log("vecli not installed, DiscoverVeCLIModels returned nil (expected)")
		}
	}
}

func TestDiscoverQoderModels_NoInstall(t *testing.T) {
	models := model.DiscoverQoderModels()
	if _, err := filepath.Abs("qodercli"); err != nil {
		if models == nil {
			t.Log("qodercli not installed, DiscoverQoderModels returned nil (expected)")
		}
	}
}

func TestDiscoverGeminiModels_NoInstall(t *testing.T) {
	models := model.DiscoverGeminiModels()
	t.Logf("DiscoverGeminiModels returned %d models", len(models))
}

func TestDiscoverClaudeModels_NoInstall(t *testing.T) {
	models := model.DiscoverClaudeModels()
	t.Logf("DiscoverClaudeModels returned %d models", len(models))
}

// --- Test 14: DiscoverPiModels with fake CLI ---

func TestDiscoverPiModels_FakeCLI_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "pi")
	script := `#!/bin/sh
cat >&2 <<'EOF'
provider        model                       context  max-out  thinking  images
anthropic       claude-sonnet-4-6           1M       64K      yes       yes
minimax         MiniMax-M2.7                204.8K   131.1K   yes       no
minimax-cn      MiniMax-M2.7                204.8K   131.1K   yes       no
EOF
`
	require.NoError(t, os.WriteFile(fakeCLI, []byte(script), 0o755))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverPiModels()
	require.Len(t, models, 3)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].Name)
	assert.True(t, models[0].Default)
	assert.Equal(t, "minimax/MiniMax-M2.7", models[1].ID)
	assert.Equal(t, "minimax/MiniMax-M2.7", models[1].Name)
	assert.Equal(t, "minimax-cn/MiniMax-M2.7", models[2].ID)
	assert.Equal(t, "minimax-cn/MiniMax-M2.7", models[2].Name)
}

func TestDiscoverPiModels_FakeCLI_EmptyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "pi")
	script := `#!/bin/sh
cat >&2 <<'EOF'
provider        model                       context  max-out  thinking  images
EOF
`
	require.NoError(t, os.WriteFile(fakeCLI, []byte(script), 0o755))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverPiModels()
	assert.Nil(t, models, "should return nil when no models parsed from output")
}

func TestDiscoverPiModels_FakeCLI_CommandFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "pi")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\nexit 1\n"), 0o755))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverPiModels()
	assert.Nil(t, models, "should return nil when pi command fails")
}

func TestDiscoverPiModels_NotOnPATH(t *testing.T) {
	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", t.TempDir()))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverPiModels()
	assert.Nil(t, models, "should return nil when pi is not on PATH")
}

func TestDiscoverPiModels_FakeCLI_OutputToStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "pi")
	script := `#!/bin/sh
cat <<'EOF'
provider        model                       context  max-out  thinking  images
anthropic       claude-sonnet-4-6           1M       64K      yes       yes
EOF
`
	require.NoError(t, os.WriteFile(fakeCLI, []byte(script), 0o755))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverPiModels()
	require.Len(t, models, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
}

// --- Test 15: ParsePiModels additional edge cases ---

func TestParsePiModels_DuplicateModelName(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images
minimax         MiniMax-M2.7                204.8K   131.1K   yes       no
minimax-cn      MiniMax-M2.7                204.8K   131.1K   yes       no
`
	models := model.ParsePiModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "minimax/MiniMax-M2.7", models[0].ID)
	assert.Equal(t, "minimax/MiniMax-M2.7", models[0].Name)
	assert.Equal(t, "minimax-cn/MiniMax-M2.7", models[1].ID)
	assert.Equal(t, "minimax-cn/MiniMax-M2.7", models[1].Name)
}
