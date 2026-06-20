package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// openCodeInputRemaps mirrors backends/openCodeInputRemaps for testing.
var openCodeInputRemaps = map[string]string{
	"oldString": "old_string", "newString": "new_string",
	"replaceAll": "replace_all", "include": "glob", "name": "skill",
}

// helper: build an OpenCodeStreamMessage with the given type and part payload
func newOpenCodeMsg(msgType string, part any) *OpenCodeStreamMessage {
	partBytes, _ := json.Marshal(part)
	return &OpenCodeStreamMessage{
		Type:      msgType,
		Timestamp: 1,
		SessionID: "ses_test",
		Part:      partBytes,
	}
}

func TestOpenCodeTool_CompletedWithInput(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "read",
		CallID: "call_123",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"filePath":"/tmp/test.go"}`),
			Output: "file content here",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Read", tc.Name)
	assert.Equal(t, "call_123", tc.ID)
	assert.True(t, tc.Done)
	assert.Equal(t, "success", tc.Status)
	assert.Equal(t, "file content here", tc.Output)

	// Verify input normalization: filePath → file_path
	var input map[string]any
	assert.NoError(t, json.Unmarshal([]byte(tc.Input), &input))
	assert.Equal(t, "/tmp/test.go", input["file_path"])
	_, hasCamel := input["filePath"]
	assert.False(t, hasCamel, "filePath should be normalized to file_path")
}

func TestOpenCodeTool_RunningNotDone(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_456",
		State: &OpenCodeToolState{
			Status: "running",
			Input:  json.RawMessage(`{"command":"ls"}`),
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
	assert.False(t, tc.Done)
	assert.Empty(t, tc.Status)
}

func TestOpenCodeTool_EditWithCamelCaseFields(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "edit",
		CallID: "call_edit",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"filePath":"main.go","oldString":"hello","newString":"world","replace_all":true}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Edit", tc.Name)
	assert.True(t, tc.Done)

	var input map[string]any
	assert.NoError(t, json.Unmarshal([]byte(tc.Input), &input))
	assert.Equal(t, "main.go", input["file_path"])
	assert.Equal(t, "hello", input["old_string"])
	assert.Equal(t, "world", input["new_string"])
	assert.Equal(t, true, input["replace_all"])

	// camelCase keys must not survive
	_, hasFilePath := input["filePath"]
	_, hasOldString := input["oldString"]
	_, hasNewString := input["newString"]
	assert.False(t, hasFilePath, "filePath should be normalized")
	assert.False(t, hasOldString, "oldString should be normalized")
	assert.False(t, hasNewString, "newString should be normalized")
}

func TestOpenCodeTool_NilState(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "read",
		CallID: "call_nil_state",
		State:  nil,
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "Read", tc.Name)
	assert.Equal(t, "call_nil_state", tc.ID)
	assert.False(t, tc.Done)
	assert.Equal(t, "{}", tc.Input) // default when no state
	assert.Empty(t, tc.Output)
	assert.Empty(t, tc.Status)
}

func TestOpenCodeTool_EmptyInput(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_empty",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  nil, // no input
			Output: "done",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "{}", tc.Input)
	assert.True(t, tc.Done)
	assert.Equal(t, "success", tc.Status)
}

func TestOpenCodeTool_NonObjectInput(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_arr",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`[1,2,3]`),
			Output: "done",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	// normalizeToolInput fails on non-object → fall back to raw input
	assert.Equal(t, "[1,2,3]", tc.Input)
}

func TestOpenCodeTool_OutputTruncation(t *testing.T) {
	longOutput := strings.Repeat("x", maxToolOutputBytes+1000)
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "bash",
		CallID: "call_long",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"command":"cat huge.log"}`),
			Output: longOutput,
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.True(t, tc.Done)
	assert.LessOrEqual(t, len(tc.Output), maxToolOutputBytes+100) // +100 for truncation marker
	assert.Contains(t, tc.Output, "[truncated:")
}

func TestOpenCodeTool_CompletedNoOutput(t *testing.T) {
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "glob",
		CallID: "call_no_out",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"pattern":"*.go"}`),
			Output: "", // empty output
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.True(t, tc.Done)
	assert.Empty(t, tc.Status) // status is "success" only when done && output != ""
	assert.Empty(t, tc.Output)
}

func TestOpenCodeTool_NonToolUseType(t *testing.T) {
	// Non-tool_use messages should return nil
	msg := newOpenCodeMsg("text", OpenCodeTextPart{
		Type: "text",
		Text: "hello",
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.Nil(t, tc)
}

func TestOpenCodeTool_UnparseablePart(t *testing.T) {
	msg := &OpenCodeStreamMessage{
		Type:      "tool_use",
		Timestamp: 1,
		SessionID: "ses_test",
		Part:      json.RawMessage(`not valid json`),
	}

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.Nil(t, tc)
}

func TestOpenCodeTool_ToolNameNormalization(t *testing.T) {
	tests := []struct {
		toolName string
		expected string
	}{
		{"read", "Read"},
		{"write", "Write"},
		{"edit", "Edit"},
		{"bash", "Bash"},
		{"glob", "Glob"},
		{"grep", "Grep"},
		{"ls", "LS"},
		{"webfetch", "WebFetch"},
		{"websearch", "WebSearch"},
		{"skill", "Skill"},
		{"task", "Agent"},
		{"todowrite", "TodoWrite"},
		{"look_at", "Read"},
		{"custom_tool", "custom_tool"},
	}
	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
				Type:   "tool",
				Tool:   tt.toolName,
				CallID: "call_test",
				State: &OpenCodeToolState{
					Status: "completed",
					Input:  json.RawMessage(`{}`),
					Output: "ok",
				},
			})
			tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
			assert.NotNil(t, tc)
			assert.Equal(t, tt.expected, tc.Name)
		})
	}
}

func TestOpenCodeTool_NullInput(t *testing.T) {
	// When Input is JSON null, it should fall through to "{}"
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "read",
		CallID: "call_null_input",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`null`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)
	assert.Equal(t, "{}", tc.Input, "null input should produce {}")
}

func TestOpenCodeTool_AlreadyCanonicalInput(t *testing.T) {
	// Input already using snake_case should pass through unchanged
	msg := newOpenCodeMsg("tool_use", OpenCodeToolPart{
		Type:   "tool",
		Tool:   "edit",
		CallID: "call_canonical",
		State: &OpenCodeToolState{
			Status: "completed",
			Input:  json.RawMessage(`{"file_path":"main.go","old_string":"foo","new_string":"bar"}`),
			Output: "ok",
		},
	})

	tc := parseOpenCodeToolEvent(msg, nil, openCodeInputRemaps)
	assert.NotNil(t, tc)

	var input map[string]any
	assert.NoError(t, json.Unmarshal([]byte(tc.Input), &input))
	assert.Equal(t, "main.go", input["file_path"])
	assert.Equal(t, "foo", input["old_string"])
	assert.Equal(t, "bar", input["new_string"])
}
