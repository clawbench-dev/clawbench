package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeToolInput_DefaultFilePath(t *testing.T) {
	// filePath should be normalized to file_path via default mapping
	input := json.RawMessage(`{"filePath":"/tmp/test.go","content":"hello"}`)
	norm, err := normalizeToolInput(input, nil)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "/tmp/test.go", parsed["file_path"], "filePath should be remapped to file_path")
	assert.Nil(t, parsed["filePath"], "filePath key should be removed")
	assert.Equal(t, "hello", parsed["content"], "other fields should be preserved")
}

func TestNormalizeToolInput_CustomMappingOverridesDefault(t *testing.T) {
	// If caller maps filePath to a different target, caller's mapping wins
	input := json.RawMessage(`{"filePath":"/tmp/test.go"}`)
	norm, err := normalizeToolInput(input, map[string]string{"filePath": "custom_path"})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "/tmp/test.go", parsed["custom_path"], "filePath should be remapped to custom_path (caller override)")
	assert.Nil(t, parsed["filePath"], "filePath key should be removed")
	assert.Nil(t, parsed["file_path"], "file_path should NOT exist (default mapping was overridden)")
}

func TestNormalizeToolInput_NoDoubleRemap(t *testing.T) {
	// When both pathMappings and defaultMappings target the same field (filePath),
	// the field should only be remapped once (no double-remap bug).
	// This was the bug described in ISS-235.
	input := json.RawMessage(`{"filePath":"main.go","oldString":"foo","newString":"bar"}`)
	norm, err := normalizeToolInput(input, map[string]string{"oldString": "old_string", "newString": "new_string"})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "main.go", parsed["file_path"], "filePath should be remapped to file_path once")
	assert.Nil(t, parsed["filePath"], "filePath key should be removed")
	assert.Equal(t, "foo", parsed["old_string"], "oldString should be remapped")
	assert.Equal(t, "bar", parsed["new_string"], "newString should be remapped")
}

func TestNormalizeToolInput_AlreadyCanonical(t *testing.T) {
	// If input already uses snake_case, no remapping needed
	input := json.RawMessage(`{"file_path":"/tmp/test.go"}`)
	norm, err := normalizeToolInput(input, nil)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "/tmp/test.go", parsed["file_path"], "already-canonical file_path should be unchanged")
}

func TestNormalizeToolInput_EmptyInput(t *testing.T) {
	norm, err := normalizeToolInput([]byte{}, nil)
	assert.NoError(t, err)
	assert.Empty(t, norm)
}

func TestNormalizeToolInput_InvalidJSON(t *testing.T) {
	bad := json.RawMessage(`not valid json`)
	norm, err := normalizeToolInput(bad, nil)
	assert.Error(t, err, "invalid JSON should return error")
	assert.Equal(t, []byte(bad), norm, "invalid JSON input should be returned unchanged")
}

func TestNormalizeToolInput_AllPathMappings(t *testing.T) {
	// Multiple pathMappings applied correctly alongside defaults
	input := json.RawMessage(`{"filePath":"main.go","dirPath":"/tmp","oldString":"hello"}`)
	norm, err := normalizeToolInput(input, map[string]string{"dirPath": "path", "oldString": "old_string"})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "main.go", parsed["file_path"], "filePath → file_path (default)")
	assert.Equal(t, "/tmp", parsed["path"], "dirPath → path (custom)")
	assert.Equal(t, "hello", parsed["old_string"], "oldString → old_string (custom)")
	assert.Nil(t, parsed["filePath"], "filePath should be removed")
	assert.Nil(t, parsed["dirPath"], "dirPath should be removed")
	assert.Nil(t, parsed["oldString"], "oldString should be removed")
}

func TestNormalizeToolInput_CmdToCommand(t *testing.T) {
	// "cmd" should be normalized to "command" via default mapping
	input := json.RawMessage(`{"cmd":"ls -la","description":"List files"}`)
	norm, err := normalizeToolInput(input, nil)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "ls -la", parsed["command"], "cmd should be remapped to command")
	assert.Nil(t, parsed["cmd"], "cmd key should be removed")
	assert.Equal(t, "List files", parsed["description"])
}

func TestNormalizeToolInput_ExecToCommand(t *testing.T) {
	// "exec" should be normalized to "command" via default mapping
	input := json.RawMessage(`{"exec":"echo hello"}`)
	norm, err := normalizeToolInput(input, nil)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "echo hello", parsed["command"], "exec should be remapped to command")
	assert.Nil(t, parsed["exec"], "exec key should be removed")
}

func TestNormalizeToolInput_AlreadyHasCommand(t *testing.T) {
	// If input already has "command", no remapping needed
	input := json.RawMessage(`{"command":"git status"}`)
	norm, err := normalizeToolInput(input, nil)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "git status", parsed["command"])
}

// --- normalizeToolName exhaustive test ---

func TestNormalizeToolName_Exhaustive(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Read
		{"read_file", "Read"},
		{"read", "Read"},
		{"look_at", "Read"},
		// Write
		{"write_file", "Write"},
		{"write", "Write"},
		// Edit
		{"edit_file", "Edit"},
		{"replace", "Edit"},
		{"edit", "Edit"},
		{"apply_patch", "Edit"},
		// Bash
		{"shell", "Bash"},
		{"run_command", "Bash"},
		{"bash", "Bash"},
		{"exec_shell", "Bash"},
		{"terminal", "Bash"},
		{"run_shell_command", "Bash"},
		// LS
		{"list_files", "LS"},
		{"list_directory", "LS"},
		{"ls", "LS"},
		{"list_dir", "LS"},
		{"list", "LS"},
		// Grep
		{"search_files", "Grep"},
		{"grep", "Grep"},
		{"grep_files", "Grep"},
		{"grep_search", "Grep"},
		{"search_file", "Grep"},
		{"search_directory", "Grep"},
		// Glob
		{"file_search", "Glob"},
		{"glob", "Glob"},
		{"find", "Glob"},
		// WebFetch
		{"web_fetch", "WebFetch"},
		{"webfetch", "WebFetch"},
		{"fetch_url", "WebFetch"},
		// WebSearch
		{"google_web_search", "WebSearch"},
		{"websearch", "WebSearch"},
		{"web_search", "WebSearch"},
		// Agent
		{"invoke_agent", "Agent"},
		{"task", "Agent"},
		{"agent_spawn", "Agent"},
		{"spawn_agent", "Agent"},
		{"delegate_to_agent", "Agent"},
		{"agent", "Agent"},
		// EnterPlanMode
		{"enter_plan_mode", "EnterPlanMode"},
		{"enterplanmode", "EnterPlanMode"},
		// ExitPlanMode
		{"exit_plan_mode", "ExitPlanMode"},
		{"exitplanmode", "ExitPlanMode"},
		// Skill
		{"activate_skill", "Skill"},
		{"skill", "Skill"},
		{"load_skill", "Skill"},
		// TodoWrite
		{"todowrite", "TodoWrite"},
		{"todo_write", "TodoWrite"},
		{"checklist_write", "TodoWrite"},
		// Git
		{"git_status", "Git"},
		{"git_diff", "Git"},
		{"git_log", "Git"},
		{"git_show", "Git"},
		{"git_blame", "Git"},
		// save_memory (no canonical)
		{"save_memory", "save_memory"},
		// Unknown tool names pass through
		{"custom_tool", "custom_tool"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeToolName(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// --- Sub-package InputRemaps integration via normalizeToolInput ---
// These tests mirror the InputRemaps defined in backend sub-packages.
// We duplicate the maps here to avoid circular imports (internal/ai → backends/* → internal/ai).

func TestNormalizeToolInput_KimiCliRemaps(t *testing.T) {
	input := json.RawMessage(`{"dirPath":"./src","dir_path":"./lib","allow_multiple":true,"is_background":true,"include_pattern":"*.go","name":"my_skill"}`)
	remaps := map[string]string{
		"dirPath": "path", "dir_path": "path",
		"allow_multiple": "replace_all", "is_background": "run_in_background",
		"include_pattern": "glob", "name": "skill",
	}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Nil(t, parsed["dirPath"])
	assert.Nil(t, parsed["dir_path"])
	assert.NotNil(t, parsed["path"])
	assert.Equal(t, true, parsed["replace_all"])
	assert.Nil(t, parsed["allow_multiple"])
	assert.Equal(t, true, parsed["run_in_background"])
	assert.Nil(t, parsed["is_background"])
	assert.Equal(t, "*.go", parsed["glob"])
	assert.Nil(t, parsed["include_pattern"])
	assert.Equal(t, "my_skill", parsed["skill"])
	assert.Nil(t, parsed["name"])
}

func TestNormalizeToolInput_OpenCodeCliRemaps(t *testing.T) {
	input := json.RawMessage(`{"include":"*.ts","name":"deploy_skill"}`)
	remaps := map[string]string{
		"oldString": "old_string", "newString": "new_string",
		"replaceAll": "replace_all", "include": "glob", "name": "skill",
	}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "*.ts", parsed["glob"])
	assert.Nil(t, parsed["include"])
	assert.Equal(t, "deploy_skill", parsed["skill"])
	assert.Nil(t, parsed["name"])
}

func TestNormalizeToolInput_DeepSeekCliRemaps(t *testing.T) {
	// Test edit_file remap: path→file_path, search→old_string, replace→new_string
	input := json.RawMessage(`{"path":"main.go","search":"old","replace":"new"}`)
	remaps := map[string]string{
		"path": "file_path", "search": "old_string", "replace": "new_string",
	}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "main.go", parsed["file_path"])
	assert.Nil(t, parsed["path"])
	assert.Equal(t, "old", parsed["old_string"])
	assert.Nil(t, parsed["search"])
	assert.Equal(t, "new", parsed["new_string"])
	assert.Nil(t, parsed["replace"])
}

func TestNormalizeToolInput_DeepSeekCliDirPathRemaps(t *testing.T) {
	// Test list_dir remap: dirPath→path
	input := json.RawMessage(`{"dirPath":"./src"}`)
	remaps := map[string]string{"dirPath": "path"}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "./src", parsed["path"])
	assert.Nil(t, parsed["dirPath"])
}

func TestNormalizeToolInput_PiCliRemaps(t *testing.T) {
	input := json.RawMessage(`{"path":"main.go"}`)
	remaps := map[string]string{"path": "file_path"}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "main.go", parsed["file_path"])
	assert.Nil(t, parsed["path"])
}

func TestNormalizeToolInput_OpenCodeAcpRemaps(t *testing.T) {
	input := json.RawMessage(`{"filePath":"main.go","oldString":"foo","newString":"bar","replaceAll":true}`)
	// Use the same remaps that backends.LookupACPRemaps("opencode") returns.
	// Test directly to avoid dependency on backends init.
	remaps := map[string]string{
		"oldString":  "old_string",
		"newString":  "new_string",
		"replaceAll": "replace_all",
	}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	// filePath → file_path via defaultMappings (opencode ACP doesn't override it)
	assert.Equal(t, "main.go", parsed["file_path"])
	assert.Nil(t, parsed["filePath"])
	assert.Equal(t, "foo", parsed["old_string"])
	assert.Nil(t, parsed["oldString"])
	assert.Equal(t, "bar", parsed["new_string"])
	assert.Nil(t, parsed["newString"])
	assert.Equal(t, true, parsed["replace_all"])
	assert.Nil(t, parsed["replaceAll"])
}

func TestNormalizeToolInput_GenericAcpRemaps(t *testing.T) {
	input := json.RawMessage(`{"filePath":"notebook.ipynb","oldString":"x","newString":"y","dirPath":"/tmp","cellIndex":0,"cellType":"code"}`)
	// Use the same remaps that acpRemapsForBackend("") returns (generic fallback).
	remaps := map[string]string{
		"oldString": "old_string", "newString": "new_string",
		"dirPath": "path", "filePath": "file_path",
		"cellIndex": "cell_index", "cellType": "cell_type",
	}
	norm, err := normalizeToolInput(input, remaps)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(norm, &parsed))

	assert.Equal(t, "notebook.ipynb", parsed["file_path"])
	assert.Nil(t, parsed["filePath"])
	assert.Equal(t, "x", parsed["old_string"])
	assert.Nil(t, parsed["oldString"])
	assert.Equal(t, "y", parsed["new_string"])
	assert.Nil(t, parsed["newString"])
	assert.Equal(t, "/tmp", parsed["path"])
	assert.Nil(t, parsed["dirPath"])
	assert.Equal(t, float64(0), parsed["cell_index"])
	assert.Nil(t, parsed["cellIndex"])
	assert.Equal(t, "code", parsed["cell_type"])
	assert.Nil(t, parsed["cellType"])
}

// --- execCommandJSON test ---

func TestExecCommandJSON(t *testing.T) {
	result := execCommandJSON("ls -la")
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "ls -la", parsed["command"])

	// Empty command
	result = execCommandJSON("")
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "", parsed["command"])

	// Command with special JSON characters
	result = execCommandJSON(`echo "hello\nworld"`)
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, `echo "hello\nworld"`, parsed["command"])
}
