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
