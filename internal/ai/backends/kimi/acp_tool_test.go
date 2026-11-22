package kimi

import (
	"testing"

	"clawbench/internal/ai/acptest"
	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
)

func TestParseKimiACPToolCall(t *testing.T) {
	t.Run("read_file prefix from toolCallId", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "read_file-1780629348181-1",
			Kind:       acp.ToolKindRead,
			Locations:  []acp.ToolCallLocation{{Path: "/home/user/README.md"}},
			Title:      "README.md",
		}
		result := parseACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
		assert.Contains(t, result.Input, "file_path")
	})

	t.Run("glob prefix from toolCallId", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "glob-1780629348181-2",
			Kind:       acp.ToolKindSearch,
			Title:      "**/*.go",
		}
		result := parseACPToolCall(tc)
		assert.Equal(t, "Glob", result.Name)
		assert.Contains(t, result.Input, "pattern")
	})

	t.Run("list_directory prefix from toolCallId", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "list_directory-1780629348181-3",
			Kind:       acp.ToolKindSearch,
			Title:      "/home/user",
		}
		result := parseACPToolCall(tc)
		assert.Equal(t, "LS", result.Name)
		assert.Contains(t, result.Input, "path")
	})
}

func TestParseKimiACPToolCallUpdate(t *testing.T) {
	completed := acp.ToolCallStatusCompleted

	t.Run("completed with content", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "read_file-1780629348181-1",
			Status:     acptest.Patch(completed),
			Content: acptest.Patch([]acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock("file contents here")),
			}),
		}
		result := parseACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "success", result.Status)
		assert.Contains(t, result.Output, "file contents here")
	})

	t.Run("orphan completed update without prior tool_call", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "orphan_call_001",
			Status:     acptest.Patch(completed),
			Content: acptest.Patch([]acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock("result text")),
			}),
		}
		result := parseACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "orphan_call_001", result.ID)
		assert.Contains(t, result.Output, "result text")
	})
}

func TestParseKimiACPToolCall_ExecuteKindFallback(t *testing.T) {
	// Kimi ACP: execute-kind tools with no locations fallback to title as command
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "execute-1780629348181-4",
		Kind:       acp.ToolKindExecute,
		Title:      "npm test",
	}
	result := parseACPToolCall(tc)
	assert.Equal(t, "Bash", result.Name)
	assert.Contains(t, result.Input, "command")
}

func TestParseKimiACPToolCall_NoLocationsNoInput(t *testing.T) {
	// When there are no locations and it's not execute kind, input should be empty
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "unknown-1780629348181-5",
		Kind:       acp.ToolKindOther,
		Title:      "some action",
	}
	result := parseACPToolCall(tc)
	// No locations, no execute kind — input should be empty
	assert.Empty(t, result.Input)
}

// --- parseKimiACPToolCallUpdate additional cases ---

func TestParseKimiACPToolCallUpdate_InProgressWithTitle(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "read_file-1780629348181-1"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "read_file-1780629348181-1",
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(inProgress),
		Locations:  acptest.Patch([]acp.ToolCallLocation{{Path: "/tmp/test.go"}}),
	}
	result := parseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
}

func TestParseKimiACPToolCallUpdate_InProgressNoTitle(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "unknown-call-id",
		Status:     acptest.Patch(inProgress),
	}
	result := parseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	// No title — name extraction won't fire
}

func TestParseKimiACPToolCallUpdate_LocationsExtracted(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "main.go"
	kind := acp.ToolKindRead
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "read_file-1780629348181-1",
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(inProgress),
		Kind:       acptest.Patch(kind),
		Locations:  acptest.Patch([]acp.ToolCallLocation{{Path: "/src/main.go"}}),
	}
	result := parseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
}

func TestParseKimiACPToolCall_WithLocations(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "read_file-1780629348181-10",
		Kind:       acp.ToolKindRead,
		Locations:  []acp.ToolCallLocation{{Path: "/src/main.go"}},
		Title:      "main.go",
	}
	result := parseACPToolCall(tc)
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
	result := parseACPToolCall(tc)
	assert.Equal(t, "Bash", result.Name)
	assert.Contains(t, result.Input, "command")
}

func TestParseKimiACPToolCallUpdate_DoneWithContent(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	title := "read_file-1780629348181-30"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "read_file-1780629348181-30",
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(completed),
		Content: acptest.Patch([]acp.ToolCallContent{
			acp.ToolContent(acp.TextBlock("file content here")),
		}),
	}
	result := parseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	// When tool is Done, mapToolCallName returns early — name won't be set
	// from title in the update path (it would have been set in the initial ToolCall)
	assert.Contains(t, result.Output, "file content here")
}

// --- OpenCode tool event with toolNameMap ---
