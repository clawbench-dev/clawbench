package opencode

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOpenCodeACPToolCall_EmptyRawInput(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "oc-empty-001",
		Title:      "bash",
		Kind:       acp.ToolKindExecute,
		RawInput:   map[string]any{},
	}
	result := ParseACPToolCall(tc)
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
	result := ParseACPToolCall(tc)
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
	result := ParseACPToolCall(tc)
	assert.Equal(t, "Edit", result.Name)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	assert.Equal(t, "/src/main.go", input["file_path"])
	assert.Equal(t, "foo", input["old_string"])
	assert.Equal(t, "bar", input["new_string"])
}

// --- Kimi ACP additional coverage ---

func TestParseOpenCodeACPToolCall(t *testing.T) {
	t.Run("camelCase rawInput normalized", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_00_xxx",
			Title:      "read",
			Kind:       acp.ToolKindRead,
			RawInput:   map[string]any{"filePath": "/home/user/file.go"},
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
		// filePath should be normalized to file_path
		var input map[string]any
		_ = json.Unmarshal([]byte(result.Input), &input)
		assert.Contains(t, input, "file_path")
		assert.NotContains(t, input, "filePath")
	})
}

func TestParseOpenCodeACPToolCallUpdate_InProgressWithInput(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "read"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "oc-001",
		Title:      &title,
		Status:     &inProgress,
		RawInput:   map[string]any{"filePath": "/tmp/test.go"},
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
	// filePath should be normalized to file_path
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	assert.Contains(t, input, "file_path")
}

func TestParseOpenCodeACPToolCallUpdate_CompletedWithOutput(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	title := "bash"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "oc-002",
		Title:      &title,
		Status:     &completed,
		RawInput:   map[string]any{"command": "go test ./..."},
		RawOutput:  "ok",
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	// When tool is Done, mapToolCallName is skipped (it returns early for done tools)
	// So the name will be empty unless set from a prior ToolCall event
	assert.Contains(t, result.Output, "ok")
}

func TestParseOpenCodeACPToolCallUpdate_Failed(t *testing.T) {
	failed := acp.ToolCallStatusFailed
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "oc-003",
		Status:     &failed,
		RawOutput:  "exit code 1",
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "error", result.Status)
}

func TestParseOpenCodeACPToolCallUpdate_NoTitleNotDone(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "oc-004",
		Status:     &inProgress,
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
}

// --- parseKimiACPToolCall additional cases ---
