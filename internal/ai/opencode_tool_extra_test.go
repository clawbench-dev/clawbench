package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- parseOpenCodeToolEvent additional coverage ---

func TestOpenCodeTool_ToolNameMapLookup(t *testing.T) {
	// When toolNameMap has a canonical name for the tool, it should be used
	toolNameMap := map[string]string{
		"custom_read": "CustomRead",
	}
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "custom_read",
		CallID: "call_map_001",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, toolNameMap, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "CustomRead", tc.Name)
}

func TestOpenCodeTool_ToolNameMapMissUsesNormalize(t *testing.T) {
	// When toolNameMap doesn't have the tool, normalizeToolName should be used
	toolNameMap := map[string]string{
		"other_tool": "OtherTool",
	}
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "read",
		CallID: "call_map_002",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, toolNameMap, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Read", tc.Name) // normalizeToolName("read") = "Read"
}

func TestOpenCodeTool_NilRemaps(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_nil_remaps",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"command":"ls"}`),
			Output: "done",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, nil)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
}

func TestOpenCodeTool_NormalizeInputError(t *testing.T) {
	// Input that's not a valid JSON object should fall back to raw input
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_bad_input",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`"just a string"`),
			Output: "done",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	// normalizeToolInput fails on non-object → fallback to raw
	assert.Equal(t, `"just a string"`, tc.Input)
}

func TestOpenCodeTool_EmptyInputNotRaw(t *testing.T) {
	// Empty input json.RawMessage should produce "{}"
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_empty_input",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "{}", tc.Input)
}

func TestOpenCodeTool_StateWithNoOutput(t *testing.T) {
	// State with running status and input but no output
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "edit",
		CallID: "call_no_output",
		State: &OpenCodeToolState{
			Status: "running",
			Input:  json.RawMessage(`{"filePath":"a.go","oldString":"foo","newString":"bar"}`),
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Edit", tc.Name)
	assert.False(t, tc.Done)
	assert.Empty(t, tc.Output)
	assert.Empty(t, tc.Status)

	// Verify input normalization
	var input map[string]any
	assert.NoError(t, json.Unmarshal([]byte(tc.Input), &input))
	assert.Equal(t, "a.go", input["file_path"])
	assert.Equal(t, "foo", input["old_string"])
	assert.Equal(t, "bar", input["new_string"])
}

func TestOpenCodeTool_ToolNameMapWithMultipleEntries(t *testing.T) {
	toolNameMap := map[string]string{
		"custom_read":  "CustomRead",
		"custom_write": "CustomWrite",
		"custom_bash":  "CustomBash",
	}

	tests := []struct {
		toolName string
		expected string
	}{
		{"custom_read", "CustomRead"},
		{"custom_write", "CustomWrite"},
		{"custom_bash", "CustomBash"},
		{"read", "Read"}, // not in map → normalizeToolName
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
				Type:   "tool",
				Tool:   tt.toolName,
				CallID: "call_multi",
				State: &OpenCodeToolState{
					Status: "completed",
					Input:  json.RawMessage(`{}`),
					Output: "ok",
				},
			})
			tc := parseOpenCodeToolEvent(msg, toolNameMap, openCodeInputRemaps)
			assert.NotNil(t, tc)
			assert.Equal(t, tt.expected, tc.Name)
		})
	}
}
