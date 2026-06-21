package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseCodeBuddyACPToolCallUpdate ---

func TestParseCodeBuddyACPToolCallUpdate_MetaFlatKey(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		Meta:       map[string]any{"codebuddy.ai/toolName": "Edit"},
		ToolCallId: "cb-001",
		Status:     &completed,
		RawOutput:  "edit applied",
	}
	result := parseCodeBuddyACPToolCallUpdate(tcu)
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
	result := parseCodeBuddyACPToolCallUpdate(tcu)
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
	result := parseCodeBuddyACPToolCallUpdate(tcu)
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
	result := parseCodeBuddyACPToolCallUpdate(tcu)
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
	result := parseCodeBuddyACPToolCallUpdate(tcu)
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
	result := parseCodeBuddyACPToolCallUpdate(tcu)
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
	result := parseCodeBuddyACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	// No title, no meta — name stays empty
}

// --- parseOpenCodeACPToolCallUpdate ---

func TestParseOpenCodeACPToolCallUpdate_InProgressWithInput(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "read"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "oc-001",
		Title:      &title,
		Status:     &inProgress,
		RawInput:   map[string]any{"filePath": "/tmp/test.go"},
	}
	result := parseOpenCodeACPToolCallUpdate(tcu)
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
	result := parseOpenCodeACPToolCallUpdate(tcu)
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
	result := parseOpenCodeACPToolCallUpdate(tcu)
	assert.True(t, result.Done)
	assert.Equal(t, "error", result.Status)
}

func TestParseOpenCodeACPToolCallUpdate_NoTitleNotDone(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "oc-004",
		Status:     &inProgress,
	}
	result := parseOpenCodeACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
}

// --- parseKimiACPToolCall additional cases ---

func TestParseKimiACPToolCall_ExecuteKindFallback(t *testing.T) {
	// Kimi ACP: execute-kind tools with no locations fallback to title as command
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "execute-1780629348181-4",
		Kind:       acp.ToolKindExecute,
		Title:      "npm test",
	}
	result := parseKimiACPToolCall(tc)
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
	result := parseKimiACPToolCall(tc)
	// No locations, no execute kind — input should be empty
	assert.Empty(t, result.Input)
}

// --- parseKimiACPToolCallUpdate additional cases ---

func TestParseKimiACPToolCallUpdate_InProgressWithTitle(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "read_file-1780629348181-1"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "read_file-1780629348181-1",
		Title:      &title,
		Status:     &inProgress,
		Locations:  []acp.ToolCallLocation{{Path: "/tmp/test.go"}},
	}
	result := parseKimiACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
}

func TestParseKimiACPToolCallUpdate_InProgressNoTitle(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "unknown-call-id",
		Status:     &inProgress,
	}
	result := parseKimiACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	// No title — name extraction won't fire
}

func TestParseKimiACPToolCallUpdate_LocationsExtracted(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	title := "main.go"
	kind := acp.ToolKindRead
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "read_file-1780629348181-1",
		Title:      &title,
		Status:     &inProgress,
		Kind:       &kind,
		Locations:  []acp.ToolCallLocation{{Path: "/src/main.go"}},
	}
	result := parseKimiACPToolCallUpdate(tcu)
	assert.False(t, result.Done)
	assert.Equal(t, "Read", result.Name)
}
