package codebuddy

import (
	"encoding/json"
	"testing"

	"clawbench/internal/ai/acptest"
	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCodeBuddyACPToolCall_WithCamelCaseRawInput(t *testing.T) {
	// Verify the shared generic remaps apply to RawInput: defaultMappings
	// (filePath → file_path) plus the shared camelCase set (oldString →
	// old_string). Both the call and update paths use the same table, so an
	// Edit card renders the diff from either frame.
	tc := acp.SessionUpdateToolCall{
		Meta:       acptest.Meta(map[string]any{"codebuddy.ai/toolName": "Edit"}),
		ToolCallId: "cb-input-001",
		Title:      "Edit file",
		Kind:       acp.ToolKindEdit,
		RawInput:   acptest.Raw(map[string]any{"filePath": "/tmp/a.go", "oldString": "foo", "newString": "bar"}),
	}
	result := ParseACPToolCall(tc)
	assert.Equal(t, "Edit", result.Name)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Input), &input))
	// filePath is remapped by defaultMappings
	assert.Contains(t, input, "file_path")
	assert.NotContains(t, input, "filePath")
	// oldString/newString come from the shared generic table
	assert.Equal(t, "foo", input["old_string"])
	assert.Equal(t, "bar", input["new_string"])
	assert.NotContains(t, input, "oldString")
	assert.NotContains(t, input, "newString")
}

func TestParseCodeBuddyACPToolCall_NilMeta(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "cb-input-002",
		Title:      "Read",
		Kind:       acp.ToolKindRead,
		RawInput:   acptest.Raw(map[string]any{"filePath": "/tmp/test.go"}),
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
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(inProgress),
		Kind:       acptest.Patch(kind),
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
		Status:     acptest.Patch(completed),
		RawInput:   acptest.PatchRaw(map[string]any{"filePath": "/src/main.go", "oldString": "hello"}),
		RawOutput:  acptest.PatchRaw("edit applied"),
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
			Meta:       acptest.Meta(map[string]any{"codebuddy.ai/toolName": "Bash"}),
			ToolCallId: "019e95ac",
			Title:      "Bash",
			Kind:       acp.ToolKindOther,
			RawInput:   acptest.Raw(map[string]any{}),
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
		Meta:       acptest.PatchMeta(map[string]any{"codebuddy.ai/toolName": "Edit"}),
		ToolCallId: "cb-001",
		Status:     acptest.Patch(completed),
		RawOutput:  acptest.PatchRaw("edit applied"),
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
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(inProgress),
		Kind:       acp.Patch[acp.ToolKind]{},
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
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(inProgress),
		Kind:       acptest.Patch(kind),
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
}

func TestParseCodeBuddyACPToolCallUpdate_MetaFlatKeyOverridesTitle(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "some title"
	tcu := acp.SessionToolCallUpdate{
		Meta:       acptest.PatchMeta(map[string]any{"codebuddy.ai/toolName": "Bash"}),
		ToolCallId: "cb-004",
		Title:      acptest.Patch(title),
		Status:     acptest.Patch(inProgress),
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.Equal(t, "Bash", result.Name)
}

func TestParseCodeBuddyACPToolCallUpdate_CompletedWithInput(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-005",
		Status:     acptest.Patch(completed),
		RawInput:   acptest.PatchRaw(map[string]any{"command": "ls -la"}),
		RawOutput:  acptest.PatchRaw("file1.txt\nfile2.txt"),
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "success", result.Status)
}

func TestParseCodeBuddyACPToolCallUpdate_Failed(t *testing.T) {
	failed := acp.ToolCallStatusFailed
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-006",
		Status:     acptest.Patch(failed),
		RawOutput:  acptest.PatchRaw("permission denied"),
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "error", result.Status)
}

func TestParseCodeBuddyACPToolCallUpdate_EmptyTitleWhenDone(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "cb-007",
		Status:     acptest.Patch(completed),
		RawOutput:  acptest.PatchRaw("done"),
	}
	result := ParseACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	// No title, no meta — name stays empty
}

// --- parseOpenCodeACPToolCallUpdate ---
