package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// --- mapACPToolCall tests ---

func TestMapACPToolCall_BasicFields(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-123"),
		Title:     "Read",
		Kind:      acp.ToolKindRead,
	}
	event := mapACPToolCall(tc)

	assert.Equal(t, "tool_use", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "Read", event.Tool.Name)
	assert.Equal(t, "tc-123", event.Tool.ID)
	assert.False(t, event.Tool.Done)
}

func TestMapACPToolCall_WithRawInput(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-456"),
		Title:     "Write",
		Kind:      acp.ToolKindEdit,
		RawInput:  map[string]any{"path": "/tmp/test.txt", "content": "hello"},
	}
	event := mapACPToolCall(tc)

	assert.Equal(t, "Write", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "path")
	assert.Contains(t, event.Tool.Input, "hello")
}

func TestMapACPToolCall_NoTitleUsesKind(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-789"),
		Title:     "",
		Kind:      acp.ToolKindRead,
	}
	event := mapACPToolCall(tc)
	assert.Equal(t, "read", event.Tool.Name)
}

// --- mapACPToolCallUpdate tests ---

func TestMapACPToolCallUpdate_Completed(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-123"),
		Status:     &completed,
		RawOutput:  map[string]any{"result": "file contents"},
	}
	event := mapACPToolCallUpdate(tcu)

	assert.Equal(t, "tool_result", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "tc-123", event.Tool.ID)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "success", event.Tool.Status)
	assert.Contains(t, event.Tool.Output, "result")
}

func TestMapACPToolCallUpdate_Failed(t *testing.T) {
	failed := acp.ToolCallStatusFailed
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-fail"),
		Status:     &failed,
		RawOutput:  map[string]any{"error": "permission denied"},
	}
	event := mapACPToolCallUpdate(tcu)

	assert.Equal(t, "tool_result", event.Type)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "error", event.Tool.Status)
}

func TestMapACPToolCallUpdate_InProgress(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-wip"),
		Status:     &inProgress,
	}
	event := mapACPToolCallUpdate(tcu)

	assert.Equal(t, "tool_use", event.Type)
	assert.False(t, event.Tool.Done)
	assert.Equal(t, "", event.Tool.Status)
}

func TestMapACPToolCallUpdate_Pending(t *testing.T) {
	pending := acp.ToolCallStatusPending
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-pend"),
		Status:     &pending,
	}
	event := mapACPToolCallUpdate(tcu)

	assert.Equal(t, "tool_use", event.Type)
	assert.False(t, event.Tool.Done)
}

// --- extractToolName tests ---

func TestExtractToolName_TitlePreferred(t *testing.T) {
	assert.Equal(t, "Read", extractToolName("Read", acp.ToolKindRead))
	assert.Equal(t, "MyCustomTool", extractToolName("MyCustomTool", acp.ToolKindEdit))
}

func TestExtractToolName_FallbackToKind(t *testing.T) {
	assert.Equal(t, "read", extractToolName("", acp.ToolKindRead))
	assert.Equal(t, "edit", extractToolName("", acp.ToolKindEdit))
}

// --- mapACPError tests ---

func TestMapACPError_ParseError(t *testing.T) {
	event := mapACPError(-32700, "parse error")
	assert.Equal(t, "error", event.Type)
	assert.Equal(t, ReasonParseError, event.Reason)
}

func TestMapACPError_InvalidRequest(t *testing.T) {
	event := mapACPError(-32600, "invalid request")
	assert.Equal(t, ReasonParseError, event.Reason)
}

func TestMapACPError_MethodNotFound(t *testing.T) {
	event := mapACPError(-32601, "method not found")
	assert.Equal(t, ReasonBackendExit, event.Reason)
}

func TestMapACPError_Cancelled(t *testing.T) {
	event := mapACPError(-32800, "cancelled")
	assert.Equal(t, ReasonContextCancel, event.Reason)
}

func TestMapACPError_AuthRequired(t *testing.T) {
	event := mapACPError(-32000, "auth required")
	assert.Equal(t, ReasonRequestFailed, event.Reason)
}

func TestMapACPError_UnknownCode(t *testing.T) {
	event := mapACPError(-99999, "unknown")
	assert.Equal(t, ReasonBackendExit, event.Reason) // default
}

// --- forwardACPEvent tests ---

func TestForwardACPEvent_Basic(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	forwardACPEvent(ch, StreamEvent{Type: "content", Content: "hello"})

	select {
	case event := <-ch:
		assert.Equal(t, "content", event.Type)
		assert.Equal(t, "hello", event.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestForwardACPEvent_ChannelFull(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "fill"} // fill buffer

	// Should not block — drops event
	forwardACPEvent(ch, StreamEvent{Type: "content", Content: "overflow"})
}

// --- NewACPBackend validation tests ---

func TestNewACPBackend_InvalidTransport(t *testing.T) {
	agent := &model.Agent{
		ID:       "test",
		Backend:  "claude",
		Transport: "cli",
	}
	_, err := NewACPBackend(agent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected acp-stdio")
}

func TestNewACPBackend_ValidStdio(t *testing.T) {
	agent := &model.Agent{
		ID:         "test",
		Backend:    "claude",
		Transport:  "acp-stdio",
		AcpCommand: "claude acp",
	}
	backend, err := NewACPBackend(agent)
	assert.NoError(t, err)
	assert.Equal(t, "claude", backend.Name())
}

func TestNewACPBackend_InvalidHTTP(t *testing.T) {
	agent := &model.Agent{
		ID:        "test",
		Backend:   "codebuddy",
		Transport: "acp-http",
	}
	_, err := NewACPBackend(agent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected acp-stdio")
}
