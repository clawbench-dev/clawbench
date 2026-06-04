package ai

import (
	"context"
	"fmt"
	"strings"
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
		Title:      "Read",
		Kind:       acp.ToolKindRead,
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
		Title:      "Write",
		Kind:       acp.ToolKindEdit,
		RawInput:   map[string]any{"path": "/tmp/test.txt", "content": "hello"},
	}
	event := mapACPToolCall(tc)

	assert.Equal(t, "Write", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "path")
	assert.Contains(t, event.Tool.Input, "hello")
}

func TestMapACPToolCall_NoTitleUsesKind(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-789"),
		Title:      "",
		Kind:       acp.ToolKindRead,
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
	assert.Contains(t, event.Tool.Output, "file contents")
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
		ID:        "test",
		Backend:   "claude",
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

// --- mapACPSessionUpdate AgentMessageChunk tests ---

func TestMapACPSessionUpdate_AgentMessageChunk(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: "hello world"},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// Should emit thinking_done + content (2 events)
	events := drainACPEvents(ch, 2)

	assert.Equal(t, "thinking_done", events[0].Type)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "hello world", events[1].Content)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_AgentMessageChunk_NilText(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{}, // Text is nil
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// Should emit thinking_done only (no content event when Text is nil)
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "thinking_done", events[0].Type)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate AgentThoughtChunk tests ---

func TestMapACPSessionUpdate_AgentThoughtChunk(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: "thinking about the problem"},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "thinking about the problem", events[0].Content)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_AgentThoughtChunk_NilText(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Content: acp.ContentBlock{}, // Text is nil
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	assertNoMoreACPEvents(ch, t) // no events when Text is nil
}

// --- mapACPSessionUpdate ToolCall tests ---

func TestMapACPSessionUpdate_ToolCall(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: acp.ToolCallId("tc-1"),
			Title:      "Read",
			Kind:       acp.ToolKindRead,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// thinking_done + tool_use (2 events)
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "thinking_done", events[0].Type)
	assert.Equal(t, "tool_use", events[1].Type)
	require.NotNil(t, events[1].Tool)
	assert.Equal(t, "Read", events[1].Tool.Name)
	assert.Equal(t, "tc-1", events[1].Tool.ID)
	assert.False(t, events[1].Tool.Done)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate ToolCallUpdate tests ---

func TestMapACPSessionUpdate_ToolCallUpdate_Completed(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	completed := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-1"),
			Status:     &completed,
			RawOutput:  map[string]any{"result": "done"},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "tool_result", events[0].Type)
	require.NotNil(t, events[0].Tool)
	assert.Equal(t, "tc-1", events[0].Tool.ID)
	assert.True(t, events[0].Tool.Done)
	assert.Equal(t, "success", events[0].Tool.Status)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_ThinkCompleted(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thinkKind := acp.ToolKindThink
	completed := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-think"),
			Kind:       &thinkKind,
			Status:     &completed,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// tool_result + thinking_done (think tool completion emits thinking_done)
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "tool_result", events[0].Type)
	assert.Equal(t, "thinking_done", events[1].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_ThinkFailed(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thinkKind := acp.ToolKindThink
	failed := acp.ToolCallStatusFailed
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-think"),
			Kind:       &thinkKind,
			Status:     &failed,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// tool_result + thinking_done (think tool failure also emits thinking_done)
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "tool_result", events[0].Type)
	assert.Equal(t, "thinking_done", events[1].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_ThinkInProgress(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thinkKind := acp.ToolKindThink
	inProgress := acp.ToolCallStatusInProgress
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-think"),
			Kind:       &thinkKind,
			Status:     &inProgress,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// Only tool_use — thinking_done NOT emitted for in-progress think tool
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "tool_use", events[0].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_NonThinkCompleted(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	readKind := acp.ToolKindRead
	completed := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-read"),
			Kind:       &readKind,
			Status:     &completed,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// Only tool_result — non-think tool does NOT emit thinking_done
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "tool_result", events[0].Type)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate AvailableCommandsUpdate tests ---

func TestMapACPSessionUpdate_AvailableCommandsUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
			AvailableCommands: []acp.AvailableCommand{
				{
					Name:        "/compact",
					Description: "Compact conversation history",
				},
				{
					Name:        "/ask",
					Description: "Ask a question",
					Input: &acp.AvailableCommandInput{
						Unstructured: &acp.UnstructuredCommandInput{
							Hint: "your question",
						},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "commands_update", events[0].Type)
	require.Len(t, events[0].Commands, 2)

	assert.Equal(t, "/compact", events[0].Commands[0].Name)
	assert.Equal(t, "Compact conversation history", events[0].Commands[0].Description)
	assert.Equal(t, "", events[0].Commands[0].InputHint) // no input

	assert.Equal(t, "/ask", events[0].Commands[1].Name)
	assert.Equal(t, "Ask a question", events[0].Commands[1].Description)
	assert.Equal(t, "your question", events[0].Commands[1].InputHint) // has input hint

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate CurrentModeUpdate tests ---

func TestMapACPSessionUpdate_CurrentModeUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
			CurrentModeId: acp.SessionModeId("code"),
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "mode_update", events[0].Type)
	require.NotNil(t, events[0].Mode)
	assert.Equal(t, "code", events[0].Mode.CurrentModeID)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_CurrentModeUpdate_WithCacheEntry(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	entry := &ACPConnEntry{}
	// Pre-populate cached mode state directly so UpdateCachedCurrentMode can update it
	entry.cachedModeState = &ModeState{CurrentModeID: "architect"}

	update := acp.SessionUpdate{
		CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
			CurrentModeId: acp.SessionModeId("code"),
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "mode_update", events[0].Type)
	assert.Equal(t, "code", events[0].Mode.CurrentModeID)

	// Cache should be updated
	assert.Equal(t, "code", entry.cachedModeState.CurrentModeID)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate ConfigOptionUpdate tests ---

func TestMapACPSessionUpdate_ConfigOptionUpdate_Mode(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	modeCategory := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("code"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "config_update", events[0].Type)
	require.NotNil(t, events[0].Config)
	assert.Equal(t, "mode", events[0].Config.ConfigID)
	assert.Equal(t, "code", events[0].Config.CurrentID)
	require.Len(t, events[0].Config.Options, 1)
	assert.Equal(t, "mode", events[0].Config.Options[0].Category)
	require.Len(t, events[0].Config.Options[0].Values, 2)
	assert.Equal(t, "ask", events[0].Config.Options[0].Values[0].ID)
	assert.Equal(t, "Ask", events[0].Config.Options[0].Values[0].Name)
	assert.Equal(t, "code", events[0].Config.Options[0].Values[1].ID)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_ThoughtLevel(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thoughtCategory := acp.SessionConfigOptionCategoryThoughtLevel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Low", Value: acp.SessionConfigValueId("low")},
			{Name: "High", Value: acp.SessionConfigValueId("high")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("thinking"),
						Name:         "Thinking",
						Category:     &thoughtCategory,
						CurrentValue: acp.SessionConfigValueId("high"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "thinking_effort_update", events[0].Type)
	require.NotNil(t, events[0].ThinkingEffort)
	assert.Equal(t, "high", events[0].ThinkingEffort.CurrentID)
	require.Len(t, events[0].ThinkingEffort.AvailableLevels, 2)
	assert.Equal(t, "low", events[0].ThinkingEffort.AvailableLevels[0].ID)
	assert.Equal(t, "Low", events[0].ThinkingEffort.AvailableLevels[0].Name)
	assert.Equal(t, "high", events[0].ThinkingEffort.AvailableLevels[1].ID)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_Model(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	modelCategory := acp.SessionConfigOptionCategoryModel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Claude 3.5", Value: acp.SessionConfigValueId("claude-3.5")},
			{Name: "GPT-4o", Value: acp.SessionConfigValueId("gpt-4o")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("model"),
						Name:         "Model",
						Category:     &modelCategory,
						CurrentValue: acp.SessionConfigValueId("claude-3.5"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "model_list_update", events[0].Type)
	require.NotNil(t, events[0].ModelList)
	assert.Equal(t, "claude-3.5", events[0].ModelList.CurrentModelID)
	require.Len(t, events[0].ModelList.Models, 2)
	assert.Equal(t, "claude-3.5", events[0].ModelList.Models[0].ID)
	assert.Equal(t, "Claude 3.5", events[0].ModelList.Models[0].Name)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_MultipleCategories(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	modeCategory := acp.SessionConfigOptionCategoryMode
	thoughtCategory := acp.SessionConfigOptionCategoryThoughtLevel
	modeUngrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)
	thoughtUngrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "High", Value: acp.SessionConfigValueId("high")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("code"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &modeUngrouped},
					},
				},
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("thinking"),
						Name:         "Thinking",
						Category:     &thoughtCategory,
						CurrentValue: acp.SessionConfigValueId("high"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &thoughtUngrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	// Should emit both config_update and thinking_effort_update
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "config_update", events[0].Type)
	assert.Equal(t, "thinking_effort_update", events[1].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_SkipNoSelect(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{}, // Select is nil — should be skipped
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	assertNoMoreACPEvents(ch, t) // no events when Select is nil
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_SkipNoCategory(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("unknown"),
						Name:         "Unknown",
						Category:     nil, // no category — should be skipped
						CurrentValue: acp.SessionConfigValueId("val"),
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate Empty/SessionInfoUpdate tests ---

func TestMapACPSessionUpdate_Empty(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{} // all nil fields

	mapACPSessionUpdate(update, ch, ctx, nil)

	assertNoMoreACPEvents(ch, t) // no events for empty update
}

func TestMapACPSessionUpdate_SessionInfoUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{},
	}

	mapACPSessionUpdate(update, ch, ctx, nil)

	assertNoMoreACPEvents(ch, t) // SessionInfoUpdate emits no stream events
}

// --- extractACPToolOutput tests ---

func TestExtractACPToolOutput_String(t *testing.T) {
	assert.Equal(t, "hello", extractACPToolOutput("hello"))
}

func TestExtractACPToolOutput_Bool(t *testing.T) {
	assert.Equal(t, "true", extractACPToolOutput(true))
	assert.Equal(t, "false", extractACPToolOutput(false))
}

func TestExtractACPToolOutput_Number(t *testing.T) {
	assert.Equal(t, "42", extractACPToolOutput(42))
	assert.Equal(t, "3.14", extractACPToolOutput(3.14))
}

func TestExtractACPToolOutput_Map_ResultKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"result": "file contents"})
	assert.Equal(t, "file contents", result)
}

func TestExtractACPToolOutput_Map_OutputKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"output": "command output"})
	assert.Equal(t, "command output", result)
}

func TestExtractACPToolOutput_Map_ContentKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"content": "file content"})
	assert.Equal(t, "file content", result)
}

func TestExtractACPToolOutput_Map_TextKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"text": "plain text"})
	assert.Equal(t, "plain text", result)
}

func TestExtractACPToolOutput_Map_MessageKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"message": "success"})
	assert.Equal(t, "success", result)
}

func TestExtractACPToolOutput_Map_StdoutWithStderr(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"stdout": "command output",
		"stderr": "some warnings",
	})
	assert.Equal(t, "command output\nsome warnings", result)
}

func TestExtractACPToolOutput_Map_StdoutNoStderr(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"stdout": "output only"})
	assert.Equal(t, "output only", result)
}

func TestExtractACPToolOutput_Map_ResultPriorityOverOutput(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"result": "from result key",
		"output": "from output key",
	})
	assert.Equal(t, "from result key", result)
}

func TestExtractACPToolOutput_Map_ErrorKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"error": "permission denied"})
	assert.Equal(t, "permission denied", result)
}

func TestExtractACPToolOutput_Map_ErrorKeyWithMessage(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"error": map[string]any{"message": "not found"},
	})
	assert.Equal(t, "not found", result)
}

func TestExtractACPToolOutput_Map_NestedValue(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"result": map[string]any{"key": "value"},
	})
	assert.Contains(t, result, `"key"`)
	assert.Contains(t, result, `"value"`)
}

func TestExtractACPToolOutput_Map_EmptyValue(t *testing.T) {
	// Empty string values in priority keys should be skipped, falling to next key
	result := extractACPToolOutput(map[string]any{
		"result": "",
		"output": "fallback",
	})
	assert.Equal(t, "fallback", result)
}

func TestExtractACPToolOutput_Map_NoKnownKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"custom_field": "custom value",
	})
	// Falls back to pretty-printed JSON of entire object
	assert.Contains(t, result, `"custom_field"`)
}

func TestExtractACPToolOutput_Array_AllStrings(t *testing.T) {
	result := extractACPToolOutput([]any{"line1", "line2", "line3"})
	assert.Equal(t, "line1\nline2\nline3", result)
}

func TestExtractACPToolOutput_Array_MixedTypes(t *testing.T) {
	result := extractACPToolOutput([]any{"text", 42})
	// Non-string elements → pretty-print as JSON
	assert.Contains(t, result, "text")
}

func TestExtractACPToolOutput_Array_Empty(t *testing.T) {
	result := extractACPToolOutput([]any{})
	assert.Equal(t, "[]", result)
}

func TestExtractACPToolOutput_NilValue(t *testing.T) {
	// nil interface → json.MarshalIndent produces "null"
	result := extractACPToolOutput(nil)
	assert.Equal(t, "null", result)
}

// --- truncateToolOutput tests ---

func TestTruncateToolOutput_Short(t *testing.T) {
	assert.Equal(t, "hello", truncateToolOutput("hello"))
}

func TestTruncateToolOutput_ExactLimit(t *testing.T) {
	s := strings.Repeat("x", maxToolOutputBytes)
	assert.Equal(t, s, truncateToolOutput(s))
}

func TestTruncateToolOutput_OverLimit(t *testing.T) {
	originalLen := maxToolOutputBytes + 100
	s := strings.Repeat("x", originalLen)
	result := truncateToolOutput(s)
	// First part is exactly maxToolOutputBytes chars, then newline + truncation marker
	prefix := result[:maxToolOutputBytes]
	assert.Equal(t, strings.Repeat("x", maxToolOutputBytes), prefix)
	assert.Contains(t, result, "\n[truncated:")
	assert.Contains(t, result, fmt.Sprintf("original %d bytes", originalLen))
}

func TestTruncateToolOutput_Empty(t *testing.T) {
	assert.Equal(t, "", truncateToolOutput(""))
}

// --- ACP test helpers ---

// drainACPEvents reads exactly count events from ch, failing the test if fewer are available.
func drainACPEvents(ch chan StreamEvent, count int) []StreamEvent {
	events := make([]StreamEvent, 0, count)
	for range count {
		select {
		case event := <-ch:
			events = append(events, event)
		default:
			// Return what we have; caller will assert length
		}
	}
	return events
}

// assertNoMoreACPEvents fails the test if there are pending events on ch.
func assertNoMoreACPEvents(ch chan StreamEvent, t *testing.T) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("expected no more events on channel")
	default:
	}
}
