package ai

import (
	"context"
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
	// Kind fallback now maps to PascalCase canonical name, not lowercase string(kind)
	assert.Equal(t, "Read", event.Tool.Name)
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
	assert.Equal(t, "MultiEdit", extractToolName("MultiEdit file", acp.ToolKindEdit))
	assert.Equal(t, "WebSearch", extractToolName("WebSearch query", acp.ToolKindSearch))
	assert.Equal(t, "Bash", extractToolName("Bash command", acp.ToolKindExecute))
	assert.Equal(t, "EnterPlanMode", extractToolName("EnterPlanMode", acp.ToolKindSwitchMode))
	assert.Equal(t, "AskUserQuestion", extractToolName("AskUserQuestion prompt", acp.ToolKindOther))
	assert.Equal(t, "TaskCreate", extractToolName("TaskCreate new task", acp.ToolKindOther))
	assert.Equal(t, "ComputerUse", extractToolName("ComputerUse action", acp.ToolKindOther))
	assert.Equal(t, "save_memory", extractToolName("save_memory", acp.ToolKindOther))
}

func TestExtractToolName_KindFallback(t *testing.T) {
	// When title is empty, fall back to ACP ToolKind → canonical mapping
	assert.Equal(t, "Read", extractToolName("", acp.ToolKindRead))
	assert.Equal(t, "Edit", extractToolName("", acp.ToolKindEdit))
	assert.Equal(t, "Bash", extractToolName("", acp.ToolKindExecute))
	assert.Equal(t, "Grep", extractToolName("", acp.ToolKindSearch))
	assert.Equal(t, "WebFetch", extractToolName("", acp.ToolKindFetch))
	assert.Equal(t, "DeepThink", extractToolName("", acp.ToolKindThink))
	assert.Equal(t, "EnterPlanMode", extractToolName("", acp.ToolKindSwitchMode))
	assert.Equal(t, "Edit", extractToolName("", acp.ToolKindDelete))
	assert.Equal(t, "Edit", extractToolName("", acp.ToolKindMove))
	assert.Equal(t, "Skill", extractToolName("", acp.ToolKindOther))
}

func TestExtractToolName_PrefixOrdering(t *testing.T) {
	// Longer prefixes must match before shorter ones
	assert.Equal(t, "MultiEdit", extractToolName("MultiEdit changes", acp.ToolKindEdit))
	assert.Equal(t, "WebSearch", extractToolName("WebSearch for golang", acp.ToolKindSearch))
	assert.Equal(t, "WebFetch", extractToolName("WebFetch url", acp.ToolKindFetch))
	assert.Equal(t, "NotebookEdit", extractToolName("NotebookEdit cell", acp.ToolKindEdit))
	assert.Equal(t, "EnterPlanMode", extractToolName("EnterPlanMode", acp.ToolKindSwitchMode))
	assert.Equal(t, "ExitPlanMode", extractToolName("ExitPlanMode", acp.ToolKindSwitchMode))
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

// --- mapACPSessionUpdate plan_update tests ---

func TestMapACPSessionUpdate_PlanUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	entries := []acp.PlanEntry{
		{Content: "Read project files", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusCompleted},
		{Content: "Implement feature", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress},
		{Content: "Write tests", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusPending},
	}

	update := acp.SessionUpdate{
		Plan: &acp.SessionUpdatePlan{
			Entries: entries,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// Assert exactly 1 event on channel
	select {
	case event := <-ch:
		assert.Equal(t, "plan_update", event.Type)
		require.NotNil(t, event.Plan)
		assert.Len(t, event.Plan.Entries, 3)

		// Verify each entry's fields
		assert.Equal(t, "Read project files", event.Plan.Entries[0].Content)
		assert.Equal(t, "high", event.Plan.Entries[0].Priority)
		assert.Equal(t, "completed", event.Plan.Entries[0].Status)

		assert.Equal(t, "Implement feature", event.Plan.Entries[1].Content)
		assert.Equal(t, "high", event.Plan.Entries[1].Priority)
		assert.Equal(t, "in_progress", event.Plan.Entries[1].Status)

		assert.Equal(t, "Write tests", event.Plan.Entries[2].Content)
		assert.Equal(t, "medium", event.Plan.Entries[2].Priority)
		assert.Equal(t, "pending", event.Plan.Entries[2].Status)
	default:
		t.Fatal("expected plan_update event on channel")
	}

	// Assert no extra events
	select {
	case <-ch:
		t.Fatal("expected only one event")
	default:
	}
}

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
