package codebuddy

import (
	"os/exec"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ACP remaps ---

func TestCodebuddyACPRemaps_ContainsExpectedKeys(t *testing.T) {
	assert.Equal(t, "old_string", CodebuddyACPRemaps["oldString"])
	assert.Equal(t, "new_string", CodebuddyACPRemaps["newString"])
	assert.Equal(t, "path", CodebuddyACPRemaps["dirPath"])
	assert.Equal(t, "file_path", CodebuddyACPRemaps["filePath"])
	assert.Equal(t, "cell_index", CodebuddyACPRemaps["cellIndex"])
	assert.Equal(t, "cell_type", CodebuddyACPRemaps["cellType"])
}

// --- Backend plugin registration ---

func TestCodebuddyBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin, "codebuddy should be registered in backends registry")
	assert.Equal(t, "codebuddy", plugin.ID)
}

func TestCodebuddyBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	assert.Equal(t, "codebuddy", plugin.Spec.ID)
	assert.Equal(t, "codebuddy", plugin.Spec.Backend)
	assert.Equal(t, "codebuddy", plugin.Spec.DefaultCmd)
	assert.Equal(t, "Codebuddy", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.ThinkingEffortLevels)
	assert.Equal(t, "codebuddy --acp", plugin.Spec.AcpCommand)
}

func TestCodebuddyBackendPlugin_ACPPlugin(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP, "codebuddy should have ACP plugin")
	assert.Equal(t, CodebuddyACPRemaps, plugin.ACP.InputRemaps)
}

// --- CLI backend functionality ---

func TestCodebuddyBackend_BuildArgsWithSessionID(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	require.NotNil(t, entry)
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "sess-123",
		WorkDir:   "/tmp/test",
	}
	args := clib.BuildArgsFn(req)

	// Should contain session ID
	foundSession := false
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) && args[i+1] == "sess-123" {
			foundSession = true
		}
	}
	assert.True(t, foundSession, "expected --session-id in args")
}

func TestCodebuddyBackend_BuildArgsWithModel(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	require.NotNil(t, entry)
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:  "test prompt",
		Model:   "glm-5.1",
		WorkDir: "/tmp/test",
	}
	args := clib.BuildArgsFn(req)

	// Should contain model
	foundModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "glm-5.1" {
			foundModel = true
		}
	}
	assert.True(t, foundModel, "expected --model in args")
}

func TestCodebuddyBackend_FilterLine_BOMVariants(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	require.NotNil(t, entry)
	clib := entry.NewBackend().(*ai.CLIBackend)

	tests := []struct {
		name     string
		line     string
		accepted bool
		clean    string
	}{
		{"normal JSON", `{"type":"assistant"}`, true, `{"type":"assistant"}`},
		{"BOM prefixed", "\xEF\xBB\xBF{\"type\":\"assistant\"}", true, `{"type":"assistant"}`},
		{"empty line", "", false, ""},
		{"whitespace only", "   ", true, "   "}, // whitespace is accepted but not BOM
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered, ok := clib.FilterLineFn(tt.line)
			assert.Equal(t, tt.accepted, ok)
			if ok {
				assert.Equal(t, tt.clean, filtered)
			}
		})
	}
}

func TestCodebuddyBackend_NewParserFn(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	require.NotNil(t, entry)
	clib := entry.NewBackend().(*ai.CLIBackend)

	parser := clib.NewParserFn()
	assert.NotNil(t, parser, "NewParserFn should return a StreamParser")
}

func TestCodebuddyBackend_PreStart_SetsStdin(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codebuddy")
	require.NotNil(t, entry)
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{Prompt: "hello world"}
	cmd := exec.Command("echo", "test")
	clib.PreStartFn(cmd, req)

	assert.NotNil(t, cmd.Stdin, "stdin should be set for codebuddy preStart")
}

// --- DiscoverCodebuddyModels edge cases ---

func TestDiscoverCodebuddyModels_EmptyProductJSON(t *testing.T) {
	// This just verifies no panic when CLI is not installed
	models := DiscoverCodebuddyModels()
	_ = models // may be nil in CI
}

func TestParseCodebuddyModels_EmptyCommaSeparated(t *testing.T) {
	// Edge case: "Currently supported: (, , )" — empty parts
	output := "Currently supported: (, , )"
	models := ParseCodebuddyModels(output)
	// Should skip empty parts
	for _, m := range models {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
	}
}

func TestParseCodebuddyModels_NameFallback(t *testing.T) {
	// Verify Name equals ID for parsed models
	output := "Currently supported: (model-a, model-b)"
	models := ParseCodebuddyModels(output)
	require.Len(t, models, 2)
	for _, m := range models {
		assert.Equal(t, m.ID, m.Name)
	}
}

func TestCodebuddyBackendSpec_SortOrder(t *testing.T) {
	plugin := backends.Lookup("codebuddy")
	require.NotNil(t, plugin)
	assert.Equal(t, 2, plugin.Spec.SortOrder)
}
