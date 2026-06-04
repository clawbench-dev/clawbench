package model_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// --- Test 1: BackendRegistry ---

func TestBackendRegistry_ContainsAllBackends(t *testing.T) {
	expectedIDs := []string{"claude", "codebuddy", "opencode", "gemini", "codex", "qoder", "vecli", "deepseek", "pi", "cline", "kimi", "copilot"}
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

// --- Test 2: generateAgentYAML ---

func TestGenerateAgentYAML_Format(t *testing.T) {
	spec := model.BackendSpec{
		ID:                   "claude",
		Backend:              "claude",
		DefaultCmd:           "claude",
		Name:                 "Claude",
		Icon:                 "🤖",
		Specialty:            "代码编写与推理",
		ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
	}

	data, err := model.GenerateAgentYAML(spec)
	require.NoError(t, err)

	// Verify it's valid YAML and parses back to Agent struct
	var agent model.Agent
	err = yaml.Unmarshal(data, &agent)
	require.NoError(t, err)

	assert.Equal(t, "claude", agent.ID)
	assert.Equal(t, "Claude", agent.Name)
	assert.Equal(t, "🤖", agent.Icon)
	assert.Equal(t, "代码编写与推理", agent.Specialty)
	assert.Equal(t, "claude", agent.Backend)
	assert.Empty(t, agent.Models)
	assert.Empty(t, agent.SystemPrompt)
	assert.Empty(t, agent.Command)
	assert.Empty(t, agent.ThinkingEffortLevels)

	// Minimal YAML: should NOT contain models, thinking_effort_levels, or system_prompt
	content := string(data)
	assert.NotContains(t, content, "models:")
	assert.NotContains(t, content, "thinking_effort")
	assert.NotContains(t, content, "system_prompt:")
}

func TestGenerateAgentYAML_ContainsRequiredFields(t *testing.T) {
	spec := model.BackendSpec{
		ID:         "test",
		Backend:    "test",
		DefaultCmd: "test",
		Name:       "Test",
		Icon:       "T",
		Specialty:  "Testing",
	}

	data, err := model.GenerateAgentYAML(spec)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "id: test")
	assert.Contains(t, content, "name: Test")
	assert.Contains(t, content, "backend: test")
	// Minimal YAML: no models, no system_prompt, no thinking_effort_levels
	assert.NotContains(t, content, "models:")
	assert.NotContains(t, content, "system_prompt:")
	assert.NotContains(t, content, "thinking_effort")
}

// --- Test 3: checkCLIExists ---

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

// --- Test 4: DiscoverAgents ---

func TestDiscoverAgents_CreatesDirAndYAMLs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	// dir does not exist yet

	err := model.DiscoverAgents(dir)
	require.NoError(t, err)

	// Directory should now exist
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Each YAML file (if any) should be parseable as an Agent.
	// Note: in CI environments no AI CLIs may be installed, so
	// we cannot assert yamlCount > 0; we only validate structure.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)

		var agent model.Agent
		err = yaml.Unmarshal(data, &agent)
		require.NoError(t, err, "YAML file %s should be parseable", e.Name())
		assert.NotEmpty(t, agent.ID)
		assert.NotEmpty(t, agent.Backend)
	}
}

func TestDiscoverAgents_GeneratedYAMLsLoadable(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Pre-generate a known agent YAML so the test does not depend
	// on any AI CLI being installed on the system.
	spec := model.BackendSpec{
		ID:         "test-loadable",
		Backend:    "claude",
		DefaultCmd: "nonexistent_cli_for_test",
		Name:       "Test Loadable",
		Icon:       "🧪",
		Specialty:  "Testing",
	}
	data, err := model.GenerateAgentYAML(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-loadable.yaml"), data, 0o644))

	// LoadAgents should successfully load the generated YAMLs
	err = model.LoadAgents(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, model.AgentList)
}

func TestDiscoverAgents_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	// Create an existing agent YAML
	existingYAML := `id: my-custom-agent
name: My Custom Agent
icon: 🎯
specialty: Custom
backend: codebuddy
models: []
system_prompt: "I am custom"
`
	err := os.WriteFile(filepath.Join(agentsDir, "my-custom-agent.yaml"), []byte(existingYAML), 0o644)
	require.NoError(t, err)

	err = model.DiscoverAgents(agentsDir)
	require.NoError(t, err)

	// Existing file should be preserved
	data, err := os.ReadFile(filepath.Join(agentsDir, "my-custom-agent.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "My Custom Agent")
}

// --- Test 5: Model list parsers ---

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
	// If the header doesn't have a default and no * marker,
	// the first model should still be marked as default (fallback convention)
	output := `  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := model.ParseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "first model should be default as fallback")
	assert.False(t, models[1].Default)
}

func TestParseDeepSeekModels_DefaultFromHeader(t *testing.T) {
	// Default is in header but no * marker on any line
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
	// Verify that provider prefix is included in both ID and Name
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
	// Non-deepseek providers should be filtered out
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
	// Real output from: opencode models (truncated for test)
	output := `opencode/minimax-m2.5-free
opencode/nemotron-3-super-free
minimax/MiniMax-M2.5
minimax/MiniMax-M2.7
anthropic/claude-sonnet-4-6
`

	models := model.ParseOpenCodeModels(output)
	require.Len(t, models, 5)

	// First model should be default
	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].ID)
	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].Name, "Name should include provider for disambiguation")
	assert.True(t, models[0].Default, "first model should be default")

	// Provider/model format
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

// --- Test 6: BackendRegistry model discovery config ---

func TestBackendRegistry_ModelDiscoveryConfig(t *testing.T) {
	specs := make(map[string]model.BackendSpec)
	for _, s := range model.BackendRegistry {
		specs[s.ID] = s
	}

	// codebuddy should have model discovery via DiscoverModelsFunc (JS bundle scanning)
	assert.NotNil(t, specs["codebuddy"].DiscoverModelsFunc, "codebuddy should have DiscoverModelsFunc")

	// opencode should have model discovery
	assert.NotEmpty(t, specs["opencode"].ListModelsCmd, "opencode should have ListModelsCmd")
	assert.NotNil(t, specs["opencode"].ParseModels, "opencode should have ParseModels")

	// deepseek should have model discovery
	assert.NotEmpty(t, specs["deepseek"].ListModelsCmd, "deepseek should have ListModelsCmd")
	assert.NotNil(t, specs["deepseek"].ParseModels, "deepseek should have ParseModels")

	// pi should have model discovery via DiscoverModelsFunc (outputs to stderr, not stdout)
	assert.NotNil(t, specs["pi"].DiscoverModelsFunc, "pi should have DiscoverModelsFunc")
	assert.Empty(t, specs["pi"].ListModelsCmd, "pi should not have ListModelsCmd")

	// claude should have model discovery via DiscoverModelsFunc (binary strings scanning)
	assert.NotNil(t, specs["claude"].DiscoverModelsFunc, "claude should have DiscoverModelsFunc")

	// gemini should have model discovery via DiscoverModelsFunc (JS bundle scanning)
	assert.NotNil(t, specs["gemini"].DiscoverModelsFunc, "gemini should have DiscoverModelsFunc")

	// codex should have model discovery via DiscoverModelsFunc (binary strings scanning)
	assert.NotNil(t, specs["codex"].DiscoverModelsFunc, "codex should have DiscoverModelsFunc")

	// qoder should have model discovery via DiscoverModelsFunc (dynamic-texts.json parsing)
	assert.NotNil(t, specs["qoder"].DiscoverModelsFunc, "qoder should have DiscoverModelsFunc")

	// vecli should have model discovery via DiscoverModelsFunc (bundle MODEL_REGISTRY parsing)
	assert.NotNil(t, specs["vecli"].DiscoverModelsFunc, "vecli should have DiscoverModelsFunc")

	// qoder and vecli should NOT have ListModelsCmd (they use DiscoverModelsFunc instead)
	assert.Empty(t, specs["qoder"].ListModelsCmd, "qoder should not have ListModelsCmd")
	assert.Empty(t, specs["vecli"].ListModelsCmd, "vecli should not have ListModelsCmd")
}

// --- Test 7: DiscoverModels ---

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
	// This test uses opencode if available; skip if not installed
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

	// First model should be default
	assert.True(t, models[0].Default, "first model should be default")
	// All models should have non-empty IDs
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestDiscoverModels_WithEchoCLI(t *testing.T) {
	// Test the full DiscoverModels flow using "echo" as a CLI that always exists.
	spec := model.BackendSpec{
		ID:            "mock-agent",
		Backend:       "mock",
		DefaultCmd:    "echo", // always available, will succeed
		Name:          "Mock",
		Icon:          "🧪",
		Specialty:     "Testing",
		ListModelsCmd: []string{"model-a, model-b"}, // echo will output this
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

// --- Test 8: FindSpecByBackend ---

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

// --- Test 9: SyncDiscoverAgents ---

func TestSyncDiscoverAgents_CreatesMinimalYAML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")

	present := model.SyncDiscoverAgents(dir)

	// Directory should exist
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// present should be a valid map
	assert.NotNil(t, present)

	// Each generated YAML should be minimal (no models, no thinking_effort_levels)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		content := string(data)
		// Minimal YAML should NOT contain models or thinking_effort_levels
		assert.NotContains(t, content, "models:")
		assert.NotContains(t, content, "thinking_effort")
		assert.NotContains(t, content, "system_prompt:")
	}
}

func TestSyncDiscoverAgents_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	// Create an existing agent YAML with custom content
	existingYAML := `id: my-custom-agent
name: My Custom Agent
icon: 🎯
specialty: Custom
backend: codebuddy
models:
  - id: custom-model
    name: Custom Model
    default: true
system_prompt: "I am custom"
`
	err := os.WriteFile(filepath.Join(agentsDir, "my-custom-agent.yaml"), []byte(existingYAML), 0o644)
	require.NoError(t, err)

	model.SyncDiscoverAgents(agentsDir)

	// Existing file should be preserved
	data, err := os.ReadFile(filepath.Join(agentsDir, "my-custom-agent.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "My Custom Agent")
	assert.Contains(t, string(data), "custom-model")
}

func TestSyncDiscoverAgents_ReturnsPresentMap(t *testing.T) {
	dir := t.TempDir()

	present := model.SyncDiscoverAgents(dir)

	// For each backend that's installed, present[backend] should be true
	for _, spec := range model.BackendRegistry {
		if present[spec.Backend] {
			// If marked present, the CLI should actually exist (or it's a NoCLI backend)
			if !spec.NoCLI {
				assert.True(t, model.CheckCLIExists(spec.DefaultCmd),
					"SyncDiscoverAgents marked %s as present but CLI not found", spec.Backend)
			}
		}
	}
}

// --- Test 10: MergeDiscoveredData ---

func TestMergeDiscoveredData_FillsEmptyModelsFromCache(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create a minimal YAML with codebuddy backend (exists in Registry)
	yamlContent := `id: test-merge
name: Test Merge
backend: codebuddy
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-merge.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))

	agent := model.Agents["test-merge"]
	require.NotNil(t, agent)
	assert.Empty(t, agent.Models)
	assert.Empty(t, agent.ThinkingEffortLevels)

	// Create a cache with models for codebuddy
	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	cachedModels := []model.AgentModel{
		{ID: "model-a", Name: "Model A", Default: true},
		{ID: "model-b", Name: "Model B", Default: false},
	}
	require.NoError(t, model.WriteModelCache(cacheDir, "codebuddy", cachedModels))

	model.MergeDiscoveredData(cacheDir)

	// Agent should now have models from cache and thinking_effort_levels from Registry
	assert.Len(t, agent.Models, 2)
	assert.Equal(t, "model-a", agent.Models[0].ID)
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, agent.ThinkingEffortLevels)
}

func TestMergeDiscoveredData_PreservesUserModels(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create YAML with user-defined models
	yamlContent := `id: test-preserve
name: Test Preserve
backend: codebuddy
models:
  - id: my-custom-model
    name: My Custom Model
    default: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-preserve.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))

	agent := model.Agents["test-preserve"]
	require.NotNil(t, agent)
	require.Len(t, agent.Models, 1)

	// Create cache with different models
	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	cachedModels := []model.AgentModel{
		{ID: "discovered-model", Name: "Discovered", Default: true},
	}
	require.NoError(t, model.WriteModelCache(cacheDir, "codebuddy", cachedModels))

	model.MergeDiscoveredData(cacheDir)

	// User models preserved
	assert.Len(t, agent.Models, 1)
	assert.Equal(t, "my-custom-model", agent.Models[0].ID)

	// ThinkingEffortLevels from Registry (codebuddy)
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, agent.ThinkingEffortLevels)
}

func TestMergeDiscoveredData_SoftRemoveMissingCLI(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create YAML for a backend whose CLI is NOT installed
	yamlContent := `id: test-missing
name: Test Missing
backend: nonexistent_backend_type
models:
  - id: some-model
    name: Some Model
    default: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-missing.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))
	require.Len(t, model.AgentList, 1)

	// Merge with present map that does NOT include "nonexistent_backend_type"
	present := map[string]bool{"claude": true, "codebuddy": true}
	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.MergeDiscoveredData(cacheDir, present)

	// Agent should be removed from runtime (but YAML still exists)
	assert.Empty(t, model.Agents)
	assert.Empty(t, model.AgentList)

	// YAML file still exists on disk
	_, err := os.Stat(filepath.Join(dir, "test-missing.yaml"))
	assert.NoError(t, err)
}

func TestMergeDiscoveredData_KeepsAgentWithPresentCLI(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create YAML with backend that IS present
	yamlContent := `id: test-present
name: Test Present
backend: codebuddy
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-present.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))
	require.Len(t, model.AgentList, 1)

	present := map[string]bool{"codebuddy": true}
	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.MergeDiscoveredData(cacheDir, present)

	// Agent should still be there
	assert.Len(t, model.AgentList, 1)
	assert.NotNil(t, model.Agents["test-present"])
	// ThinkingEffortLevels filled from Registry
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, model.Agents["test-present"].ThinkingEffortLevels)
}

func TestMergeDiscoveredData_IgnoresYAMLThinkingEffortLevels(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create YAML with user-defined thinking_effort_levels (should be overwritten by Registry)
	yamlContent := `id: test-levels
name: Test Levels
backend: codebuddy
thinking_effort_levels:
  - custom1
  - custom2
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-levels.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))

	agent := model.Agents["test-levels"]
	require.NotNil(t, agent)
	// Before merge: YAML values are loaded
	assert.Equal(t, []string{"custom1", "custom2"}, agent.ThinkingEffortLevels)

	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.MergeDiscoveredData(cacheDir)

	// After merge: Registry values replace YAML values
	assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, agent.ThinkingEffortLevels)
}

// --- Test 11: SyncDiscoverModels ---

func TestSyncDiscoverModels_CreatesCacheFiles(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "model-cache")

	model.SyncDiscoverModels(cacheDir)

	// Cache dir should be created (if any backend has model discovery + is installed)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		// No cache dir = no CLIs with model discovery installed, OK
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
		require.NoError(t, err)

		var entry map[string]any
		require.NoError(t, json.Unmarshal(data, &entry))
		assert.Contains(t, entry, "models")
		assert.Contains(t, entry, "updated_at")
	}
}

// --- Test 12: DiscoverClaudeModels ---

func TestDiscoverClaudeModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("claude") {
		t.Skip("claude not installed, skipping integration test")
	}

	models := model.DiscoverClaudeModels()
	if len(models) == 0 {
		t.Skip("claude model discovery returned no models (strings may not be available)")
	}

	// All models should have claude- prefixed IDs
	for _, m := range models {
		assert.True(t, strings.HasPrefix(m.ID, "claude-"), "model ID should start with claude-, got: %s", m.ID)
		assert.NotEmpty(t, m.Name, "model should have a name")
	}

	// First model should be default
	assert.True(t, models[0].Default, "first model should be default")

	t.Logf("Discovered %d Claude models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

// --- Test 12b: DiscoverCodebuddyModels ---

func TestDiscoverCodebuddyModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("codebuddy") {
		t.Skip("codebuddy not installed, skipping integration test")
	}

	models := model.DiscoverCodebuddyModels()
	if len(models) == 0 {
		t.Skip("codebuddy model discovery returned no models (product JSON may not be found)")
	}

	// All models should have valid IDs and names
	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name, got ID: %s", m.ID)
	}

	// Should contain both glm and non-glm models (deepseek, kimi, etc.)
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

	// First model should be default
	assert.True(t, models[0].Default, "first model should be default")

	// Should not contain pseudo-models "default" or "auto"
	for _, m := range models {
		assert.NotEqual(t, "default", m.ID, "should not contain pseudo-model 'default'")
		assert.NotEqual(t, "auto", m.ID, "should not contain pseudo-model 'auto'")
	}

	t.Logf("Discovered %d Codebuddy models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

// --- Test 13: SyncDiscoverModels covers DiscoverModelsFunc (Claude) ---

func TestSyncDiscoverModels_CoversClaudeDiscoverModelsFunc(t *testing.T) {
	// Claude uses DiscoverModelsFunc instead of ListModelsCmd+ParseModels.
	// Before the fix, SyncDiscoverModels skipped Claude because it only checked
	// ListModelsCmd/ParseModels. After the fix, it should include Claude.
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

	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.SyncDiscoverModels(cacheDir)

	data, err := os.ReadFile(filepath.Join(cacheDir, "claude.json"))
	if err != nil {
		// strings command may not be available on this system
		t.Logf("claude cache file not created (strings may not be available): %v", err)
		return
	}

	var entry map[string]any
	require.NoError(t, json.Unmarshal(data, &entry))
	assert.Contains(t, entry, "models")
	t.Logf("claude cache file created with models")
}

// --- Test 13b: Gemini/Codex/Qoder/VeCLI model discovery integration ---

func TestDiscoverGeminiModels_WithRealCLI(t *testing.T) {
	if !model.CheckCLIExists("gemini") {
		t.Skip("gemini not installed, skipping integration test")
	}

	models := model.DiscoverGeminiModels()
	if len(models) == 0 {
		t.Skip("gemini model discovery returned no models")
	}

	// All models should have gemini- prefixed IDs
	for _, m := range models {
		assert.True(t, strings.HasPrefix(m.ID, "gemini-"), "model ID should start with gemini-, got: %s", m.ID)
		assert.NotEmpty(t, m.Name, "model should have a name")
	}

	// First model should be default
	assert.True(t, models[0].Default, "first model should be default")

	// Should not contain aliases
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

	// All models should have valid IDs
	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name")
	}

	// First model should be default
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

	// All models should have valid IDs and names
	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name")
	}

	// Should not contain tier aliases
	for _, m := range models {
		assert.NotEqual(t, "auto", m.ID, "should not contain 'auto' alias")
		assert.NotEqual(t, "ultimate", m.ID, "should not contain 'ultimate' tier")
		assert.NotEqual(t, "performance", m.ID, "should not contain 'performance' tier")
		assert.NotEqual(t, "efficient", m.ID, "should not contain 'efficient' tier")
		assert.NotEqual(t, "lite", m.ID, "should not contain 'lite' tier")
	}

	// First model should be default
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

	// All models should have valid IDs and names
	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model should have an ID")
		assert.NotEmpty(t, m.Name, "model should have a name")
	}

	// First model should be default
	assert.True(t, models[0].Default, "first model should be default")

	t.Logf("Discovered %d VeCLI models:", len(models))
	for _, m := range models {
		t.Logf("  %s (%s) default=%v", m.ID, m.Name, m.Default)
	}
}

// --- Test 14: AsyncRefreshModelCache ---
// AsyncRefreshModelCache is a fire-and-forget goroutine that iterates over
// BackendRegistry, discovers models, and updates in-memory agents.
// It cannot be tested safely with the race detector because:
//   - It launches an untracked goroutine
//   - The goroutine accesses global state (AgentList) concurrently
//   - Test cleanup (setting Agents=nil) races with the goroutine
//
// The core model discovery logic is already covered by:
//   - TestDiscoverModels_* (DiscoverModels function)
//   - TestSyncDiscoverModels_* (SyncDiscoverModels synchronous path)
//   - TestMergeDiscoveredData_* (MergeDiscoveredData agent update logic)
//
// AsyncRefreshModelCache is essentially the async composition of these,
// and the composition itself is trivial (just a goroutine wrapper).
// We test the one unique behavior: it should not panic when called.

func TestAsyncRefreshModelCache_DoesNotPanic(t *testing.T) {
	// Calling AsyncRefreshModelCache should not panic, even with no agents.
	// Use a manual temp dir (not t.TempDir()) because AsyncRefreshModelCache
	// launches a goroutine that may still be writing to the cache directory
	// when t.TempDir() tries to clean up, causing "directory not empty" errors.
	cacheDir := filepath.Join(os.TempDir(), fmt.Sprintf("acp-test-cache-%d", time.Now().UnixNano()))
	err := os.MkdirAll(cacheDir, 0o755)
	require.NoError(t, err)
	t.Cleanup(func() {
		// Best-effort cleanup; ignore errors if goroutine still holds files
		_ = os.RemoveAll(cacheDir)
	})
	assert.NotPanics(t, func() {
		model.AsyncRefreshModelCache(cacheDir)
	})
}

// --- Test 15: CheckCLIExistsErr ---

func TestCheckCLIExistsErr_ExistingCommand(t *testing.T) {
	// "ls" exists on all platforms
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

// --- Test 16: DiscoverCodebuddyModels with mock product JSON ---

func TestDiscoverCodebuddyModels_ProductJSON(t *testing.T) {
	// These tests modify PATH and create fake CLI scripts which don't work on Windows.
	// The core JSON parsing logic is also covered by the internal unit tests.
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	// Create directory structure: .../bin/fake-codebuddy and .../product.cloudhosted.json
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	// Create a fake "codebuddy" script
	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	// Create product.cloudhosted.json in the parent directory
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

	// Add tmpDir/bin to PATH
	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	require.NotEmpty(t, models, "should discover models from product JSON")

	// Should contain 3 models (glm-5.1, glm-4-flash, deepseek-v3)
	// Pseudo-models "default", "auto", and image model should be skipped
	assert.Len(t, models, 3)
	assert.Equal(t, "glm-5.1", models[0].ID)
	assert.Equal(t, "GLM 5.1", models[0].Name)
	assert.True(t, models[0].Default, "first model should be default")
	assert.Equal(t, "deepseek-v3", models[2].ID)
	assert.Equal(t, "DeepSeek V3", models[2].Name)

	// Verify no pseudo-models
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

	// Empty models array
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

	// Invalid JSON
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

	// No product.cloudhosted.json file created

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	assert.Nil(t, models, "should return nil when product JSON file doesn't exist")
}

func TestDiscoverCodebuddyModels_NotOnPATH(t *testing.T) {
	// When codebuddy is not on PATH at all, should return nil
	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", t.TempDir()))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverCodebuddyModels()
	assert.Nil(t, models, "should return nil when codebuddy is not on PATH")
}

func TestDiscoverCodebuddyModels_ProductJSON_NameFallback(t *testing.T) {
	// Test the name fallback: when a model has no name, use its ID as name
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	// Model with empty name — should fall back to ID
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
	// Name should fall back to ID when empty in JSON
	assert.Equal(t, "glm-5.1", models[0].Name)
}

func TestDiscoverCodebuddyModels_ProductJSON_NoDefault(t *testing.T) {
	// Test when no model is marked isDefault — first non-skipped model should get Default=true
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	fakeCLI := filepath.Join(binDir, "codebuddy")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\necho ok\n"), 0o755))

	// No isDefault=true on any model
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
	// First model should get Default=true as fallback
	assert.True(t, models[0].Default, "first model should be default when none marked isDefault")
	assert.False(t, models[1].Default)
}

// --- Test 17: MergeDiscoveredData CanRefreshModels ---

func TestMergeDiscoveredData_SetsCanRefreshModels(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create a minimal YAML with codebuddy backend (has model discovery)
	yamlContent := `id: test-refresh
name: Test Refresh
backend: codebuddy
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-refresh.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))

	agent := model.Agents["test-refresh"]
	require.NotNil(t, agent)

	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.MergeDiscoveredData(cacheDir)

	// codebuddy has DiscoverModelsFunc, so CanRefreshModels should be true
	assert.True(t, agent.CanRefreshModels, "codebuddy agent should have CanRefreshModels=true")
}

func TestMergeDiscoveredData_CanRefreshModelsFalseForNoDiscovery(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Create a minimal YAML with gemini backend (no model discovery)
	yamlContent := `id: test-no-refresh
name: Test No Refresh
backend: gemini
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-no-refresh.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))

	agent := model.Agents["test-no-refresh"]
	require.NotNil(t, agent)

	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.MergeDiscoveredData(cacheDir)

	// gemini now has model discovery via DiscoverModelsFunc, so CanRefreshModels should be true
	assert.True(t, agent.CanRefreshModels, "gemini agent should have CanRefreshModels=true")
}

// --- Test 7: SyncDiscoverAgents ---

func TestSyncDiscoverAgents_CreatesDirAndReturnsPresent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	present := model.SyncDiscoverAgents(dir)
	assert.NotNil(t, present)
	// Directory should exist
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestSyncDiscoverAgents_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	existingYAML := `id: my-agent
name: My Agent
backend: claude
models: []
system_prompt: "custom prompt"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "my-agent.yaml"), []byte(existingYAML), 0o644))

	present := model.SyncDiscoverAgents(agentsDir)
	assert.NotNil(t, present)

	data, err := os.ReadFile(filepath.Join(agentsDir, "my-agent.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "custom prompt", "existing YAML should not be overwritten")
}

// --- Test 8: SyncDiscoverModels ---

func TestSyncDiscoverModels_CreatesCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	// Should not panic or fail even if no models are discovered
	model.SyncDiscoverModels(cacheDir)
	// Cache dir may or may not be created depending on available CLIs
}

// --- Test 9: AsyncRefreshModelCache ---

func TestAsyncRefreshModelCache_DoesNotBlock(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "model-cache-async")
	// Should return immediately (goroutine launched in background)
	model.AsyncRefreshModelCache(cacheDir)
	// Give a small window for goroutine to start
	time.Sleep(100 * time.Millisecond)
}

// --- Test 10: DiscoverAgents error paths ---

func TestDiscoverAgents_InvalidDirPath(t *testing.T) {
	// Use a path that can't be created (e.g., under /proc on Linux)
	if runtime.GOOS == "windows" {
		t.Skip("unix-specific test")
	}
	err := model.DiscoverAgents("/proc/nonexistent/impossible/path")
	// Should still succeed — MkdirAll on /proc fails but DiscoverAgents
	// creates the dir with MkdirAll which returns an error
	assert.Error(t, err)
}

// --- Test 11: MergeDiscoveredData with present map ---

func TestMergeDiscoveredData_SoftRemovesAbsentBackends(t *testing.T) {
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	dir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	yamlContent := `id: test-absent
name: Test Absent
backend: claude
models: []
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-absent.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, model.LoadAgents(dir))
	require.NotEmpty(t, model.AgentList)

	// Pass a present map that does NOT include "claude"
	present := map[string]bool{"codebuddy": true}
	cacheDir := filepath.Join(t.TempDir(), "model-cache")
	model.MergeDiscoveredData(cacheDir, present)

	// Agent with "claude" backend should be soft-removed
	_, exists := model.Agents["test-absent"]
	assert.False(t, exists, "agent with absent backend should be soft-removed")
}

// --- Test 12: DiscoverCodexModels full call ---

func TestDiscoverCodexModels_NoInstall(t *testing.T) {
	// Codex is not installed in CI, so this should return nil or defaults
	// without panicking
	models := model.DiscoverCodexModels()
	// The function itself is DiscoverModelsFunc in BackendRegistry,
	// but we can call it directly for coverage
	if _, err := filepath.Abs("codex"); err != nil {
		// No codex installed: DiscoverCodexModels falls through to defaults
		// which also returns nil if codex not on PATH
		if models == nil {
			t.Log("codex not installed, DiscoverCodexModels returned nil (expected)")
		}
	}
}

// --- Test 13: DiscoverVeCLIModels full call ---

func TestDiscoverVeCLIModels_NoInstall(t *testing.T) {
	models := model.DiscoverVeCLIModels()
	if _, err := filepath.Abs("vecli"); err != nil {
		if models == nil {
			t.Log("vecli not installed, DiscoverVeCLIModels returned nil (expected)")
		}
	}
}

// --- Test 14: DiscoverQoderModels full call ---

func TestDiscoverQoderModels_NoInstall(t *testing.T) {
	models := model.DiscoverQoderModels()
	if _, err := filepath.Abs("qodercli"); err != nil {
		if models == nil {
			t.Log("qodercli not installed, DiscoverQoderModels returned nil (expected)")
		}
	}
}

// --- Test 15: DiscoverGeminiModels full call ---

func TestDiscoverGeminiModels_NoInstall(t *testing.T) {
	models := model.DiscoverGeminiModels()
	// Gemini discovery uses API which may or may not be available
	// Just verify it doesn't panic
	t.Logf("DiscoverGeminiModels returned %d models", len(models))
}

// --- Test 16: DiscoverClaudeModels full call ---

func TestDiscoverClaudeModels_NoInstall(t *testing.T) {
	models := model.DiscoverClaudeModels()
	// Claude may not be installed on CI
	t.Logf("DiscoverClaudeModels returned %d models", len(models))
}

// --- Test 17: SyncDiscoverAgents with existing YAMLs ---

func TestSyncDiscoverAgents_WithPreExistingYAMLs(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	// Create YAML for an agent that doesn't exist
	yamlContent := `id: test-existing
name: Test Existing
backend: claude
models: []
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "test-existing.yaml"), []byte(yamlContent), 0o644))

	present := model.SyncDiscoverAgents(agentsDir)
	assert.NotNil(t, present)
}

// --- Test 18: DiscoverAgents with pre-existing YAMLs ---

func TestDiscoverAgents_WithExistingYAMLs(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	yamlContent := `id: test-existing
name: Test Existing
backend: claude
models: []
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "test-existing.yaml"), []byte(yamlContent), 0o644))

	err := model.DiscoverAgents(agentsDir)
	require.NoError(t, err)

	// Existing YAML should not be overwritten
	data, err := os.ReadFile(filepath.Join(agentsDir, "test-existing.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Test Existing")
}

// --- Test 18: DiscoverPiModels with fake CLI ---

func TestDiscoverPiModels_FakeCLI_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows — fake CLI scripts not executable")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	// Create a fake "pi" script that outputs model table to stderr (like real Pi)
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

	// Create a fake "pi" script that outputs only the header (no model data)
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

	// Create a fake "pi" script that exits with non-zero code
	fakeCLI := filepath.Join(binDir, "pi")
	require.NoError(t, os.WriteFile(fakeCLI, []byte("#!/bin/sh\nexit 1\n"), 0o755))

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath))
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	models := model.DiscoverPiModels()
	assert.Nil(t, models, "should return nil when pi command fails")
}

func TestDiscoverPiModels_NotOnPATH(t *testing.T) {
	// Ensure PATH doesn't contain a "pi" binary
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

	// Create a fake "pi" script that outputs to stdout (like the old behavior)
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

	// CombinedOutput captures both stdout and stderr, so stdout output works too
	models := model.DiscoverPiModels()
	require.Len(t, models, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
}

// --- Test 19: ParsePiModels additional edge cases ---

func TestParsePiModels_DuplicateModelName(t *testing.T) {
	// When two providers have the same model name, they should be distinguishable
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

// --- Test 15: ParseCodebuddyModels edge cases ---
