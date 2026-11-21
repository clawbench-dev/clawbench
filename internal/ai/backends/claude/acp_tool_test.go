package claude

import (
	"testing"

	"clawbench/internal/ai/acptest"
	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
)

func TestParseClaudeACPToolCall(t *testing.T) {
	t.Run("meta.claudeCode.toolName preferred", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta:       acptest.Meta(map[string]any{"claudeCode": map[string]any{"toolName": "Edit"}}),
			ToolCallId: "call_001",
			Title:      "Edit file",
			Kind:       acp.ToolKindEdit,
			RawInput:   acptest.Raw(map[string]any{"file_path": "/tmp/a.txt", "old_string": "foo", "new_string": "bar"}),
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Edit", result.Name)
		assert.Equal(t, "call_001", result.ID)
		assert.False(t, result.Done)
		assert.Contains(t, result.Input, "file_path")
		assert.Contains(t, result.Input, "old_string")
	})

	t.Run("fallback to extractToolName when no meta", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_002",
			Title:      "Read",
			Kind:       acp.ToolKindRead,
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
	})

	t.Run("execute kind fallback to title as command", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_003",
			Title:      "ls -la",
			Kind:       acp.ToolKindExecute,
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Bash", result.Name)
		assert.Contains(t, result.Input, "command")
	})

	t.Run("locations and title fallback", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_004",
			Title:      "main.go",
			Kind:       acp.ToolKindRead,
			Locations:  []acp.ToolCallLocation{{Path: "/src/main.go"}},
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
		assert.Contains(t, result.Input, "file_path")
	})
}

func TestParseClaudeACPToolCallUpdate(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	failed := acp.ToolCallStatusFailed
	inProgress := acp.ToolCallStatusInProgress

	t.Run("completed with meta toolName", func(t *testing.T) {
		title := "Bash"
		tcu := acp.SessionToolCallUpdate{
			Meta:       acptest.PatchMeta(map[string]any{"claudeCode": map[string]any{"toolName": "Bash"}}),
			ToolCallId: "call_001",
			Title:      acptest.Patch(title),
			Status:     acptest.Patch(completed),
			RawOutput:  acptest.PatchRaw("command output"),
		}
		result := ParseACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "Bash", result.Name)
		assert.Contains(t, result.Output, "command output")
	})

	t.Run("failed", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "call_002",
			Status:     acptest.Patch(failed),
			RawOutput:  acptest.PatchRaw("error message"),
		}
		result := ParseACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "error", result.Status)
	})

	t.Run("in progress - no output extraction", func(t *testing.T) {
		title := "Read"
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "call_003",
			Title:      acptest.Patch(title),
			Status:     acptest.Patch(inProgress),
			RawInput:   acptest.PatchRaw(map[string]any{"file_path": "/tmp/test.go"}),
		}
		result := ParseACPToolCallUpdate(tcu)
		assert.False(t, result.Done)
		assert.Empty(t, result.Output)
		assert.Contains(t, result.Input, "file_path")
	})
}
