package codebuddy

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	result := ParseACPToolCall(tc)
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
	result := ParseACPToolCall(tc)
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
	result := ParseACPToolCallUpdate(tcu)
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
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "success", result.Status)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	assert.Contains(t, input, "file_path")
	assert.NotContains(t, input, "filePath")
	assert.Contains(t, input, "old_string")
}

// --- OpenCode ACP additional coverage ---

func TestParseCodeBuddyACPToolCall(t *testing.T) {
	t.Run("meta flat key preferred", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta:       map[string]any{"codebuddy.ai/toolName": "Bash"},
			ToolCallId: "019e95ac",
			Title:      "Bash",
			Kind:       acp.ToolKindOther,
			RawInput:   map[string]any{},
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Bash", result.Name)
	})

	t.Run("fallback to extractToolName when no meta", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "019e95ad",
			Title:      "Read",
			Kind:       acp.ToolKindRead,
		}
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
	})
}

func TestParseCodeBuddyACPToolCallUpdate_MetaFlatKey(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		Meta:       map[string]any{"codebuddy.ai/toolName": "Edit"},
		ToolCallId: "cb-001",
		Status:     &completed,
		RawOutput:  "edit applied",
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "Edit", result.Name)
	assert.Contains(t, result.Output, "edit applied")
}

func TestParseCodeBuddyACPToolCallUpdate_TitleFallbackWhenNotDone(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "Read file.go"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-002",
		Title:      &title,
		Status:     &inProgress,
		Kind:       (*acp.ToolKind)(nil),
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	// When name is empty or lowercase and not done, title-based extraction runs
	assert.Equal(t, "Read", result.Name)
}

func TestParseCodeBuddyACPToolCallUpdate_TitleFallbackWithKind(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "main.go"
	kind := acp.ToolKindRead
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-003",
		Title:      &title,
		Status:     &inProgress,
		Kind:       &kind,
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
}

func TestParseCodeBuddyACPToolCallUpdate_MetaFlatKeyOverridesTitle(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "some title"
	tcu := acp.SessionToolCallUpdate{
		Meta:       map[string]any{"codebuddy.ai/toolName": "Bash"},
		ToolCallId: "cb-004",
		Title:      &title,
		Status:     &inProgress,
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.Equal(t, "Bash", result.Name)
}

func TestParseCodeBuddyACPToolCallUpdate_CompletedWithInput(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-005",
		Status:     &completed,
		RawInput:   map[string]any{"command": "ls -la"},
		RawOutput:  "file1.txt\nfile2.txt",
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "success", result.Status)
}

func TestParseCodeBuddyACPToolCallUpdate_Failed(t *testing.T) {
	failed := acp.ToolCallStatusFailed
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-006",
		Status:     &failed,
		RawOutput:  "permission denied",
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "error", result.Status)
}

func TestParseCodeBuddyACPToolCallUpdate_EmptyTitleWhenDone(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-007",
		Status:     &completed,
		RawOutput:  "done",
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	// No title, no meta — name stays empty
}

// --- parseOpenCodeACPToolCallUpdate ---
