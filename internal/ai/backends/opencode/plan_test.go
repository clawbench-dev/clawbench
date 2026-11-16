package opencode

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

func assertNoMoreACPEvents(ch chan ai.StreamEvent, t *testing.T) {
	t.Helper()
	for {
		select {
		case event := <-ch:
			t.Fatalf("expected no more events on channel, got %q", event.Type)
		default:
			return
		}
	}
}

func drainACPEvents(ch chan ai.StreamEvent, count int) []ai.StreamEvent {
	events := make([]ai.StreamEvent, 0, count)
	for range count * 3 {
		select {
		case event := <-ch:
			events = append(events, event)
			if len(events) == count {
				return events
			}
		default:
			return events
		}
	}
	return events
}

func ocCompletedTcu(rawInput, rawOutput any) acp.SessionToolCallUpdate {
	st := acp.ToolCallStatusCompleted
	return acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_todo"),
		Status:     &st,
		Title:      ptrStr("3 todos"),
		RawInput:   rawInput,
		RawOutput:  rawOutput,
	}
}

func ptrStr(s string) *string { return &s }

func ocAgent(backend string) *model.Agent {
	return &model.Agent{
		ID:        backend + "-bridge-test",
		Backend:   backend,
		Transport: "acp-stdio",
	}
}

func TestParseOpenCodeTodoSnapshot_RawOutputMetadata(t *testing.T) {
	// The completed-frame shape: rawOutput.metadata.todos.
	rawOutput := map[string]any{
		"metadata": map[string]any{
			"todos": ocTodos(
				map[string]any{"content": "create a.txt", "priority": "high", "status": "pending"},
				map[string]any{"content": "create b.txt", "priority": "high", "status": "in_progress"},
			),
			"truncated": false,
		},
		"output": "[\n ...\n]",
	}
	entries, ok := parseOpenCodeTodoSnapshot(nil, rawOutput)
	require.True(t, ok)
	require.Len(t, entries, 2)
	assert.Equal(t, "create a.txt", entries[0].Content)
	assert.Equal(t, "pending", entries[0].Status)
	assert.Equal(t, "high", entries[0].Priority)
	assert.Equal(t, "in_progress", entries[1].Status)
}

func TestParseOpenCodeTodoSnapshot_RawInputTodos(t *testing.T) {
	// The in_progress-frame shape: rawInput.todos.
	rawInput := map[string]any{
		"todos": ocTodos(
			map[string]any{"content": "step 1", "priority": "medium", "status": "completed"},
		),
	}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, nil)
	require.True(t, ok)
	require.Len(t, entries, 1)
	assert.Equal(t, "step 1", entries[0].Content)
	assert.Equal(t, "completed", entries[0].Status)
	assert.Equal(t, "medium", entries[0].Priority)
}

func TestParseOpenCodeTodoSnapshot_StatusMapping(t *testing.T) {
	rawInput := map[string]any{
		"todos": ocTodos(
			map[string]any{"content": "a", "status": "pending"},
			map[string]any{"content": "b", "status": "in_progress"},
			map[string]any{"content": "c", "status": "completed"},
			map[string]any{"content": "d", "status": "blocked"}, // unknown
			map[string]any{"content": "e"},                      // missing
		),
	}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, nil)
	require.True(t, ok)
	require.Len(t, entries, 5)
	assert.Equal(t, []string{"pending", "in_progress", "completed", "pending", "pending"},
		[]string{entries[0].Status, entries[1].Status, entries[2].Status, entries[3].Status, entries[4].Status})
}

func TestParseOpenCodeTodoSnapshot_PriorityMapping(t *testing.T) {
	rawInput := map[string]any{
		"todos": ocTodos(
			map[string]any{"content": "a", "priority": "high"},
			map[string]any{"content": "b", "priority": "low"},
			map[string]any{"content": "c", "priority": "urgent"}, // unknown
			map[string]any{"content": "d"},                       // missing
		),
	}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, nil)
	require.True(t, ok)
	require.Len(t, entries, 4)
	assert.Equal(t, []string{"high", "low", "medium", "medium"},
		[]string{entries[0].Priority, entries[1].Priority, entries[2].Priority, entries[3].Priority})
}

func TestParseOpenCodeTodoSnapshot_NoTodos(t *testing.T) {
	_, ok := parseOpenCodeTodoSnapshot(nil, nil)
	assert.False(t, ok, "no rawInput/rawOutput → not ok")

	_, ok = parseOpenCodeTodoSnapshot(map[string]any{"other": "x"}, nil)
	assert.False(t, ok, "rawInput without todos → not ok")

	_, ok = parseOpenCodeTodoSnapshot(nil, map[string]any{"metadata": map[string]any{"truncated": true}})
	assert.False(t, ok, "metadata without todos → not ok")

	_, ok = parseOpenCodeTodoSnapshot(nil, map[string]any{"metadata": "not-a-map"})
	assert.False(t, ok, "non-map metadata → not ok")
}

func TestParseOpenCodeTodoSnapshot_EmptyTodosIsValid(t *testing.T) {
	// An empty todos array is a valid (empty) snapshot — clears the panel.
	rawInput := map[string]any{"todos": []any{}}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, nil)
	assert.True(t, ok)
	assert.Empty(t, entries)
}

func TestParseOpenCodeTodoSnapshot_AllItemsSkipped(t *testing.T) {
	// Items present but every one lacks content → unexpected shape, fail closed
	// so we never wipe a real checklist with an empty plan_update.
	rawInput := map[string]any{
		"todos": ocTodos(
			map[string]any{"status": "pending"},
			map[string]any{"id": "2"},
		),
	}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, nil)
	assert.False(t, ok)
	assert.Nil(t, entries)
}

func TestParseOpenCodeTodoSnapshot_RawOutputPreferredOverRawInput(t *testing.T) {
	// A completed frame carries the APPLIED snapshot in rawOutput; rawInput (the
	// requested list) may be stale or empty. rawOutput must win, otherwise an
	// empty rawInput.todos would wipe a real checklist.
	rawInput := map[string]any{"todos": []any{}}
	rawOutput := map[string]any{
		"metadata": map[string]any{
			"todos": ocTodos(map[string]any{"content": "applied-step", "status": "completed"}),
		},
	}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, rawOutput)
	require.True(t, ok)
	require.Len(t, entries, 1, "rawOutput snapshot must win over empty rawInput")
	assert.Equal(t, "applied-step", entries[0].Content)
	assert.Equal(t, "completed", entries[0].Status)
}

func TestParseOpenCodeTodoSnapshot_RawInputFallbackWhenNoRawOutput(t *testing.T) {
	// If a completed frame omits rawOutput, fall back to rawInput.todos.
	rawInput := map[string]any{
		"todos": ocTodos(map[string]any{"content": "fallback-step", "status": "in_progress"}),
	}
	entries, ok := parseOpenCodeTodoSnapshot(rawInput, nil)
	require.True(t, ok)
	require.Len(t, entries, 1)
	assert.Equal(t, "fallback-step", entries[0].Content)
}

// --- bridgeOpenCodePlanFromToolUpdate ---

func TestBridgeOpenCodePlanFromToolUpdate_EmitsAndCaches(t *testing.T) {
	conn := ai.NewACPConnForTest(ocAgent("opencode"), "session-oc-bridge-1")
	ch := make(chan ai.StreamEvent, 8)

	tcu := ocCompletedTcu(nil, map[string]any{
		"metadata": map[string]any{
			"todos": ocTodos(
				map[string]any{"content": "step 1", "priority": "high", "status": "pending"},
			),
		},
	})

	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	require.Equal(t, 1, n, "one plan_update should be forwarded")

	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	assert.Equal(t, "plan_update", events[0].Type)
	require.NotNil(t, events[0].Plan)
	require.Len(t, events[0].Plan.Entries, 1)
	assert.Equal(t, "step 1", events[0].Plan.Entries[0].Content)

	cached := conn.GetCachedPlanState()
	require.NotNil(t, cached)
	require.Len(t, cached.Entries, 1)
	assert.Equal(t, "step 1", cached.Entries[0].Content)
}

func TestBridgeOpenCodePlanFromToolUpdate_SkipsNonCompleted(t *testing.T) {
	conn := ai.NewACPConnForTest(ocAgent("opencode"), "session-oc-bridge-2")
	ch := make(chan ai.StreamEvent, 8)

	st := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_todo"),
		Status:     &st,
		RawInput:   map[string]any{"todos": ocTodos(map[string]any{"content": "x", "status": "pending"})},
	}
	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	assert.Zero(t, n, "in_progress frame must not bridge (avoids duplicate plan_update)")
	assert.Nil(t, conn.GetCachedPlanState())
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeOpenCodePlanFromToolUpdate_SkipsNoSnapshot(t *testing.T) {
	conn := ai.NewACPConnForTest(ocAgent("opencode"), "session-oc-bridge-3")
	ch := make(chan ai.StreamEvent, 8)

	// A completed tool frame that is not a todowrite (e.g. a write) — no todos.
	tcu := ocCompletedTcu(
		map[string]any{"filePath": "/tmp/a.txt", "content": "a"},
		map[string]any{"metadata": map[string]any{"filepath": "/tmp/a.txt"}},
	)
	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	assert.Zero(t, n)
	assert.Nil(t, conn.GetCachedPlanState())
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeOpenCodePlanFromToolUpdate_EmptySnapshotClears(t *testing.T) {
	conn := ai.NewACPConnForTest(ocAgent("opencode"), "session-oc-bridge-4")
	ch := make(chan ai.StreamEvent, 8)

	tcu := ocCompletedTcu(nil, map[string]any{
		"metadata": map[string]any{"todos": []any{}},
	})
	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	require.Equal(t, 1, n, "empty snapshot still forwarded to clear the panel")
	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Plan)
	assert.Empty(t, events[0].Plan.Entries)
	cached := conn.GetCachedPlanState()
	require.NotNil(t, cached)
	assert.Empty(t, cached.Entries)
}

// --- mapACPSessionUpdate end-to-end bridge ---

func ocTodos(items ...map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out
}
