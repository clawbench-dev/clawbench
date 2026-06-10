package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGeminiToolUse_Basic(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_use",
		ToolName:   "read_file",
		ToolID:     "call_123",
		Parameters: json.RawMessage(`{"filePath":"/tmp/test.go"}`),
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "Read", tc.Name)
	assert.Equal(t, "call_123", tc.ID)
	assert.True(t, tc.Done, "Gemini tool_use should always have Done=true")
}

func TestParseGeminiToolUse_InputNormalization(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_use",
		ToolName:   "list_directory",
		ToolID:     "call_ls",
		Parameters: json.RawMessage(`{"dirPath":"./src"}`),
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)

	// dirPath should be remapped to path via getRemaps("gemini_cli")
	var input map[string]any
	err := json.Unmarshal([]byte(tc.Input), &input)
	assert.NoError(t, err)
	assert.Equal(t, "./src", input["path"])
	_, hasDirPath := input["dirPath"]
	assert.False(t, hasDirPath, "dirPath should be remapped to path")
}

func TestParseGeminiToolUse_FilePathNormalization(t *testing.T) {
	// filePath → file_path is in defaultMappings, but gemini_cli remaps are also merged
	msg := &GeminiStreamMessage{
		Type:       "tool_use",
		ToolName:   "read_file",
		ToolID:     "call_read",
		Parameters: json.RawMessage(`{"filePath":"/tmp/main.go"}`),
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)

	var input map[string]any
	err := json.Unmarshal([]byte(tc.Input), &input)
	assert.NoError(t, err)
	assert.Equal(t, "/tmp/main.go", input["file_path"])
	_, hasFilePath := input["filePath"]
	assert.False(t, hasFilePath, "filePath should be remapped to file_path")
}

func TestParseGeminiToolUse_CombinedNormalization(t *testing.T) {
	// Both filePath and dirPath in same input
	msg := &GeminiStreamMessage{
		Type:       "tool_use",
		ToolName:   "some_tool",
		ToolID:     "call_combo",
		Parameters: json.RawMessage(`{"filePath":"main.go","dirPath":"./src"}`),
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)

	var input map[string]any
	err := json.Unmarshal([]byte(tc.Input), &input)
	assert.NoError(t, err)
	assert.Equal(t, "main.go", input["file_path"])
	assert.Equal(t, "./src", input["path"])
}

func TestParseGeminiToolUse_EmptyParameters(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:     "tool_use",
		ToolName: "list_files",
		ToolID:   "call_456",
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "{}", tc.Input, "empty parameters should produce {}")
	assert.True(t, tc.Done)
}

func TestParseGeminiToolUse_NilParameters(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:     "tool_use",
		ToolName: "shell",
		ToolID:   "call_nil",
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
	assert.Equal(t, "{}", tc.Input)
}

func TestParseGeminiToolUse_NonObjectParameters(t *testing.T) {
	// When parameters is valid JSON but not an object, normalizeToolInput fails
	// and should fall back to raw string
	msg := &GeminiStreamMessage{
		Type:       "tool_use",
		ToolName:   "some_tool",
		ToolID:     "call_arr",
		Parameters: json.RawMessage(`[1,2,3]`),
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "[1,2,3]", tc.Input, "non-object parameters should fall back to raw string")
}

func TestParseGeminiToolUse_ToolNameNormalization(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected string
	}{
		{"read_file", "read_file", "Read"},
		{"write_file", "write_file", "Write"},
		{"edit_file", "edit_file", "Edit"},
		{"shell", "shell", "Bash"},
		{"replace", "replace", "Edit"},
		{"list_directory", "list_directory", "LS"},
		{"glob", "glob", "Glob"},
		{"web_fetch", "web_fetch", "WebFetch"},
		{"google_web_search", "google_web_search", "WebSearch"},
		{"invoke_agent", "invoke_agent", "Agent"},
		{"unknown_tool", "custom_tool", "custom_tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &GeminiStreamMessage{
				Type:       "tool_use",
				ToolName:   tt.toolName,
				ToolID:     "call_test",
				Parameters: json.RawMessage(`{}`),
			}
			tc := parseGeminiToolUse(msg)
			assert.NotNil(t, tc)
			assert.Equal(t, tt.expected, tc.Name)
		})
	}
}

func TestParseGeminiToolUse_AlwaysDone(t *testing.T) {
	// Gemini sends full tool input in one event, so Done is always true
	msg := &GeminiStreamMessage{
		Type:       "tool_use",
		ToolName:   "read_file",
		ToolID:     "call_done",
		Parameters: json.RawMessage(`{"filePath":"/tmp/test.go"}`),
	}

	tc := parseGeminiToolUse(msg)
	assert.NotNil(t, tc)
	assert.True(t, tc.Done, "Gemini tool_use should always have Done=true")
}

func TestParseGeminiToolResult_Basic(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "call_123",
		ToolOutput: "file content here",
		Status:     "success",
	}

	tc := parseGeminiToolResult(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "call_123", tc.ID)
	assert.Equal(t, "file content here", tc.Output)
	assert.Equal(t, "success", tc.Status)
}

func TestParseGeminiToolResult_EmptyToolID(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "",
		ToolOutput: "some output",
		Status:     "success",
	}

	tc := parseGeminiToolResult(msg)
	assert.Nil(t, tc, "tool_result with empty tool_id should return nil")
}

func TestParseGeminiToolResult_ErrorStatus(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "call_err",
		ToolOutput: "command failed",
		Status:     "error",
	}

	tc := parseGeminiToolResult(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "call_err", tc.ID)
	assert.Equal(t, "error", tc.Status)
	assert.Equal(t, "command failed", tc.Output)
}

func TestParseGeminiToolResult_OutputTruncation(t *testing.T) {
	// Create output larger than maxToolOutputBytes (51200)
	longOutput := strings.Repeat("x", 60000)
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "call_trunc",
		ToolOutput: longOutput,
		Status:     "success",
	}

	tc := parseGeminiToolResult(msg)
	assert.NotNil(t, tc)
	assert.True(t, len(tc.Output) < len(longOutput), "output should be truncated")
	assert.Contains(t, tc.Output, "[truncated:", "truncated output should contain marker")
}

func TestParseGeminiToolResult_ShortOutputNotTruncated(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "call_short",
		ToolOutput: "short output",
		Status:     "success",
	}

	tc := parseGeminiToolResult(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "short output", tc.Output, "short output should not be truncated")
}

func TestParseGeminiToolResult_EmptyOutput(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "call_empty",
		ToolOutput: "",
		Status:     "success",
	}

	tc := parseGeminiToolResult(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "", tc.Output)
}

func TestParseGeminiToolResult_EmptyStatus(t *testing.T) {
	msg := &GeminiStreamMessage{
		Type:       "tool_result",
		ToolID:     "call_nostatus",
		ToolOutput: "some output",
		Status:     "",
	}

	tc := parseGeminiToolResult(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "", tc.Status)
}
