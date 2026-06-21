package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- CodeBuddy ACP additional coverage ---

func TestParseCodeBuddyACPToolCall_WithCamelCaseRawInput(t *testing.T) {
	// Verify that defaultMappings (filePath → file_path) are applied to RawInput.
	// Note: codebuddy_acp remaps is {} — only defaultMappings apply.
	tc := acp.SessionUpdateToolCall{
		Meta:       map[string]any{"codebuddy.ai/toolName": "Edit"},
		ToolCallId: "cb-input-001",
		Title:      "Edit file",
		Kind:       acp.ToolKindEdit,
		RawInput:   map[string]any{"filePath": "/tmp/a.go", "oldString": "foo", "newString": "bar"},
	}
	result := parseCodeBuddyACPToolCall(tc)
	assert.Equal(t, "Edit", result.Name)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	// filePath is remapped by defaultMappings
	assert.Contains(t, input, "file_path")
	assert.NotContains(t, input, "filePath")
	// oldString and newString are NOT in codebuddy_acp remaps, so they stay as-is
	assert.Contains(t, input, "oldString")
	assert.Contains(t, input, "newString")
}

func TestParseCodeBuddyACPToolCall_NilMeta(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "cb-input-002",
		Title:      "Read",
		Kind:       acp.ToolKindRead,
		RawInput:   map[string]any{"filePath": "/tmp/test.go"},
	}
	result := parseCodeBuddyACPToolCall(tc)
	assert.Equal(t, "Read", result.Name)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	assert.Contains(t, input, "file_path")
}

func TestParseCodeBuddyACPToolCallUpdate_LowercaseNameUpdatedByTitle(t *testing.T) {
	// When tool.Name is already set but lowercase, title-based extraction should update it
	inProgress := acp.ToolCallStatusInProgress
	title := "Edit file.go"
	kind := acp.ToolKindEdit
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-lower-001",
		Title:      &title,
		Status:     &inProgress,
		Kind:       &kind,
	}
	result := parseCodeBuddyACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Edit", result.Name)
}

func TestParseCodeBuddyACPToolCallUpdate_CompletedWithCamelCaseInput(t *testing.T) {
	// Verify input remapping on update
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-update-001",
		Status:     &completed,
		RawInput:   map[string]any{"filePath": "/src/main.go", "oldString": "hello"},
		RawOutput:  "edit applied",
	}
	result := parseCodeBuddyACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "success", result.Status)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	assert.Contains(t, input, "file_path")
	assert.NotContains(t, input, "filePath")
	assert.Contains(t, input, "old_string")
}

// --- OpenCode ACP additional coverage ---

func TestParseOpenCodeACPToolCall_EmptyRawInput(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "oc-empty-001",
		Title:      "bash",
		Kind:       acp.ToolKindExecute,
		RawInput:   map[string]any{},
	}
	result := parseOpenCodeACPToolCall(tc)
	assert.Equal(t, "Bash", result.Name)
	// Empty RawInput → resolveACPToolInput marshals it to "{}"
	assert.NotEmpty(t, result.Input)
}

func TestParseOpenCodeACPToolCall_NilRawInput(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "oc-nil-001",
		Title:      "read",
		Kind:       acp.ToolKindRead,
	}
	result := parseOpenCodeACPToolCall(tc)
	assert.Equal(t, "Read", result.Name)
	// No RawInput → resolveACPToolInput tries Content/Locations fallback
}

func TestParseOpenCodeACPToolCall_AlreadySnakeCaseInput(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "oc-snake-001",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput:   map[string]any{"file_path": "/src/main.go", "old_string": "foo", "new_string": "bar"},
	}
	result := parseOpenCodeACPToolCall(tc)
	assert.Equal(t, "Edit", result.Name)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	assert.Equal(t, "/src/main.go", input["file_path"])
	assert.Equal(t, "foo", input["old_string"])
	assert.Equal(t, "bar", input["new_string"])
}

// --- Kimi ACP additional coverage ---

func TestParseKimiACPToolCall_WithLocations(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "read_file-1780629348181-10",
		Kind:       acp.ToolKindRead,
		Locations:  []acp.ToolCallLocation{{Path: "/src/main.go"}},
		Title:      "main.go",
	}
	result := parseKimiACPToolCall(tc)
	assert.Equal(t, "Read", result.Name)
	assert.Contains(t, result.Input, "file_path")
}

func TestParseKimiACPToolCall_ExecuteKindInInitialCall(t *testing.T) {
	// Execute-kind fallback in parseKimiACPToolCall (not just in update)
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "execute-1780629348181-20",
		Kind:       acp.ToolKindExecute,
		Title:      "go test ./...",
	}
	result := parseKimiACPToolCall(tc)
	assert.Equal(t, "Bash", result.Name)
	assert.Contains(t, result.Input, "command")
}

func TestParseKimiACPToolCallUpdate_DoneWithContent(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	title := "read_file-1780629348181-30"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "read_file-1780629348181-30",
		Title:      &title,
		Status:     &completed,
		Content: []acp.ToolCallContent{
			acp.ToolContent(acp.TextBlock("file content here")),
		},
	}
	result := parseKimiACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	// When tool is Done, mapToolCallName returns early — name won't be set
	// from title in the update path (it would have been set in the initial ToolCall)
	assert.Contains(t, result.Output, "file content here")
}

// --- OpenCode tool event with toolNameMap ---

func TestOpenCodeTool_WithToolNameMap_Hit(t *testing.T) {
	toolNameMap := map[string]string{
		"custom_read": "Read",
		"custom_edit": "Edit",
	}
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "custom_read",
		CallID: "call_tnm_001",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"filePath":"/tmp/test.go"}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, toolNameMap, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Read", tc.Name, "toolNameMap should map custom_read → Read")
}

func TestOpenCodeTool_WithToolNameMap_Miss(t *testing.T) {
	toolNameMap := map[string]string{
		"custom_read": "Read",
	}
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "unknown_tool",
		CallID: "call_tnm_002",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, toolNameMap, openCodeInputRemaps)
	assert.NotNil(t, tc)
	// Not in toolNameMap → falls through to normalizeToolName
	assert.Equal(t, "unknown_tool", tc.Name)
}

func TestOpenCodeTool_WithToolNameMap_Nil(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_tnm_003",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
}
