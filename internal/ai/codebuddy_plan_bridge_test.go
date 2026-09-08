package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// codebuddy_plan_bridge — unit tests
// ---------------------------------------------------------------------------

// cbCodebuddyAgent returns a CodeBuddy ACP agent for bridge unit tests.
func cbCodebuddyAgent() *model.Agent {
	return &model.Agent{
		ID:        "codebuddy-bridge-test",
		Backend:   "codebuddy",
		Transport: "acp-stdio",
	}
}

// cbTaskMeta builds a _meta map carrying the rawResponse snapshot shape
// CodeBuddy actually emits (verified by the wire probe): rawResponse contains
// the full `todos` snapshot after the operation.
func cbTaskMeta(rawResp map[string]any) map[string]any {
	return map[string]any{
		"codebuddy.ai/rawResponse": rawResp,
	}
}

// --- parseCodeBuddyTaskMetaTodos ---

func TestParseCodeBuddyTaskMetaTodos_TaskCreate(t *testing.T) {
	meta := cbTaskMeta(map[string]any{
		"task": map[string]any{
			"id": "1", "subject": "probe", "status": "pending",
		},
		"todos": []any{
			map[string]any{
				"id": "1", "content": "probe", "status": "pending",
				"activeForm": "Probing", "createdAt": int64(1788856390803),
			},
		},
	})
	entries, ok := parseCodeBuddyTaskMetaTodos(meta)
	require.True(t, ok)
	require.Len(t, entries, 1)
	assert.Equal(t, "probe", entries[0].Content)
	assert.Equal(t, "pending", entries[0].Status)
	assert.Equal(t, "medium", entries[0].Priority)
}

func TestParseCodeBuddyTaskMetaTodos_StatusMapping(t *testing.T) {
	meta := cbTaskMeta(map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "a", "status": "pending"},
			map[string]any{"id": "2", "content": "b", "status": "in_progress"},
			map[string]any{"id": "3", "content": "c", "status": "completed"},
		},
	})
	entries, ok := parseCodeBuddyTaskMetaTodos(meta)
	require.True(t, ok)
	require.Len(t, entries, 3)
	assert.Equal(t, []string{"pending", "in_progress", "completed"},
		[]string{entries[0].Status, entries[1].Status, entries[2].Status})
}

func TestParseCodeBuddyTaskMetaTodos_TaskListTasksFallback(t *testing.T) {
	// TaskList returns BOTH `tasks` (raw rows, subject field) and `todos`
	// (flattened, content field). todos is preferred; tasks is the fallback.
	meta := cbTaskMeta(map[string]any{
		"tasks": []any{
			map[string]any{"id": "1", "subject": "raw-subject", "status": "in_progress"},
		},
	})
	entries, ok := parseCodeBuddyTaskMetaTodos(meta)
	require.True(t, ok)
	require.Len(t, entries, 1)
	assert.Equal(t, "raw-subject", entries[0].Content)
	assert.Equal(t, "in_progress", entries[0].Status)
}

func TestParseCodeBuddyTaskMetaTodos_UnknownStatusFallback(t *testing.T) {
	meta := cbTaskMeta(map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "blocked-task", "status": "blocked"},
			map[string]any{"id": "2", "content": "no-status"},
		},
	})
	entries, ok := parseCodeBuddyTaskMetaTodos(meta)
	require.True(t, ok)
	require.Len(t, entries, 2)
	assert.Equal(t, "pending", entries[0].Status, "unknown status falls back to pending")
	assert.Equal(t, "pending", entries[1].Status, "missing status falls back to pending")
}

func TestParseCodeBuddyTaskMetaTodos_NoRawResponse(t *testing.T) {
	_, ok := parseCodeBuddyTaskMetaTodos(nil)
	assert.False(t, ok, "nil meta → not ok")

	_, ok = parseCodeBuddyTaskMetaTodos(map[string]any{"other": "x"})
	assert.False(t, ok, "meta without rawResponse → not ok")

	_, ok = parseCodeBuddyTaskMetaTodos(map[string]any{
		"codebuddy.ai/rawResponse": "not-a-map",
	})
	assert.False(t, ok, "non-map rawResponse → not ok")
}

func TestParseCodeBuddyTaskMetaTodos_NoTodosArray(t *testing.T) {
	meta := cbTaskMeta(map[string]any{
		"task": map[string]any{"id": "1", "subject": "solo"},
	})
	_, ok := parseCodeBuddyTaskMetaTodos(meta)
	assert.False(t, ok, "no todos/tasks array → not ok")

	// Empty todos array IS a valid snapshot (delete-all) — returns ok with 0 entries.
	meta2 := cbTaskMeta(map[string]any{
		"taskId": "1", "deleted": true, "todos": []any{},
	})
	entries, ok := parseCodeBuddyTaskMetaTodos(meta2)
	assert.True(t, ok, "empty todos array is a valid (empty) snapshot")
	assert.Empty(t, entries)
}

func TestParseCodeBuddyTaskMetaTodos_AllItemsSkipped(t *testing.T) {
	// Non-empty todos where every item lacks content/subject is an unexpected
	// shape — must fail closed (not ok), never wiping a real checklist with an
	// empty plan_update.
	meta := cbTaskMeta(map[string]any{
		"todos": []any{
			map[string]any{"id": "1"},
			map[string]any{"id": "2", "status": "pending"},
		},
	})
	entries, ok := parseCodeBuddyTaskMetaTodos(meta)
	assert.False(t, ok, "items present but all skipped → not ok")
	assert.Nil(t, entries)
}

// --- bridgeCodeBuddyPlanFromToolUpdate ---

// codebuddyTcuForBridge builds a CodeBuddy task-tool ToolCallUpdate with the
// given name/status and rawResponse snapshot, stamping the tool name into _meta
// exactly as the wire does (verified by the wire probe).
func codebuddyTcuForBridge(name, status string, rawResp map[string]any) acp.SessionToolCallUpdate {
	st := acp.ToolCallStatus(status)
	return acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_bridge"),
		Status:     &st,
		Meta: map[string]any{
			"codebuddy.ai/toolName":    name,
			"codebuddy.ai/rawResponse": rawResp,
		},
	}
}

func TestBridgeCodeBuddyPlanFromToolUpdate_EmitsAndCaches(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-bridge-1")
	ch := make(chan StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskCreate", "completed", map[string]any{
		"task":  map[string]any{"id": "1", "status": "pending", "subject": "probe"},
		"todos": []any{map[string]any{"id": "1", "content": "probe", "status": "pending"}},
	})

	n := bridgeCodeBuddyPlanFromToolUpdate(ch, conn, tcu)
	require.Equal(t, 1, n, "one plan_update should be forwarded")

	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	assert.Equal(t, "plan_update", events[0].Type)
	require.NotNil(t, events[0].Plan)
	require.Len(t, events[0].Plan.Entries, 1)
	assert.Equal(t, "probe", events[0].Plan.Entries[0].Content)
	assert.Equal(t, "pending", events[0].Plan.Entries[0].Status)

	// Cache must be set so refresh/reconnect/REST repopulate the panel.
	cached := conn.GetCachedPlanState()
	require.NotNil(t, cached)
	require.Len(t, cached.Entries, 1)
	assert.Equal(t, "probe", cached.Entries[0].Content)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_TaskUpdateStatus(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-bridge-2")
	ch := make(chan StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskUpdate", "completed", map[string]any{
		"task":  map[string]any{"id": "1", "status": "in_progress", "subject": "probe"},
		"todos": []any{map[string]any{"id": "1", "content": "probe", "status": "in_progress"}},
	})

	n := bridgeCodeBuddyPlanFromToolUpdate(ch, conn, tcu)
	require.Equal(t, 1, n)
	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	assert.Equal(t, "in_progress", events[0].Plan.Entries[0].Status)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_DeleteLastEmitsEmptyPlan(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-bridge-3")
	ch := make(chan StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskUpdate", "completed", map[string]any{
		"taskId":  "1",
		"deleted": true,
		"todos":   []any{},
	})

	n := bridgeCodeBuddyPlanFromToolUpdate(ch, conn, tcu)
	require.Equal(t, 1, n, "empty snapshot still forwarded to clear the panel")
	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Plan)
	assert.Empty(t, events[0].Plan.Entries)
	cached := conn.GetCachedPlanState()
	require.NotNil(t, cached)
	assert.Empty(t, cached.Entries)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_SkipsNonBridgedTool(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-bridge-4")
	ch := make(chan StreamEvent, 8)

	// TaskGet returns a single task — not a full snapshot — so it must NOT bridge.
	tcu := codebuddyTcuForBridge("TaskGet", "completed", map[string]any{
		"task": map[string]any{"id": "1", "status": "pending", "subject": "solo"},
	})
	n := bridgeCodeBuddyPlanFromToolUpdate(ch, conn, tcu)
	assert.Zero(t, n)
	assert.Nil(t, conn.GetCachedPlanState(), "TaskGet must not touch the plan cache")
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_SkipsNonCompleted(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-bridge-5")
	ch := make(chan StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskCreate", "in_progress", map[string]any{
		"task": map[string]any{"id": "1"}, "todos": []any{map[string]any{"id": "1", "content": "p"}},
	})
	n := bridgeCodeBuddyPlanFromToolUpdate(ch, conn, tcu)
	assert.Zero(t, n)
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_NoRawResponse(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-bridge-6")
	ch := make(chan StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskCreate", "completed", nil)
	n := bridgeCodeBuddyPlanFromToolUpdate(ch, conn, tcu)
	assert.Zero(t, n)
	assert.Nil(t, conn.GetCachedPlanState())
	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate end-to-end bridge ---

// mapACPSessionUpdate emits a raw_output debug event (AppendRawOutput) and the
// bridge plan_update. Drain skipping raw_output, then check for plan_update.
func TestMapACPSessionUpdate_CodebuddyTaskCreate_BridgesPlanUpdate(t *testing.T) {
	conn := newACPConn(cbCodebuddyAgent(), "session-map-1")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()

	st := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("call_map"),
			Status:     &st,
			Meta: map[string]any{
				"codebuddy.ai/toolName": "TaskCreate",
				"codebuddy.ai/rawResponse": map[string]any{
					"task":  map[string]any{"id": "1", "subject": "map-probe", "status": "pending"},
					"todos": []any{map[string]any{"id": "1", "content": "map-probe", "status": "pending"}},
				},
			},
		},
	}

	// nil debouncer → the ToolCallUpdate branch falls through to the fallback
	// forward path, then our bridge call (placed before deb) runs too.
	mapACPSessionUpdate(update, ch, ctx, conn, nil)

	// raw_output (from AppendRawOutput) + tool_use/tool_result + plan_update.
	// Gather events skipping raw_output, look for plan_update.
	planEvents := 0
	toolEvents := 0
drainLoop:
	for {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "raw_output":
				continue
			case "plan_update":
				planEvents++
				require.NotNil(t, ev.Plan)
				assert.Len(t, ev.Plan.Entries, 1)
				assert.Equal(t, "map-probe", ev.Plan.Entries[0].Content)
			case "tool_use", "tool_result":
				toolEvents++
			}
		default:
			break drainLoop
		}
	}
	assert.Equal(t, 1, planEvents, "exactly one plan_update should be bridged")
	assert.GreaterOrEqual(t, toolEvents, 1, "the tool card event should also be present")
	cached := conn.GetCachedPlanState()
	require.NotNil(t, cached)
	assert.Len(t, cached.Entries, 1)
}

func TestMapACPSessionUpdate_NonCodebuddy_NoBridge(t *testing.T) {
	// Claude agent — must not bridge (its real ACP Plan path stays authoritative).
	agent := &model.Agent{ID: "claude-test", Backend: "claude", Transport: "acp-stdio"}
	conn := newACPConn(agent, "session-map-2")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()

	st := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("call_claude"),
			Status:     &st,
			Meta: map[string]any{
				"codebuddy.ai/toolName": "TaskCreate",
				"codebuddy.ai/rawResponse": map[string]any{
					"task":  map[string]any{"id": "1", "subject": "x"},
					"todos": []any{map[string]any{"id": "1", "content": "x", "status": "pending"}},
				},
			},
		},
	}
	mapACPSessionUpdate(update, ch, ctx, conn, nil)

	hasPlan := false
drainLoop2:
	for {
		select {
		case ev := <-ch:
			if ev.Type == "plan_update" {
				hasPlan = true
			}
		default:
			break drainLoop2
		}
	}
	assert.False(t, hasPlan, "non-codebuddy backend must not bridge task tools to plan")
	assert.Nil(t, conn.GetCachedPlanState())
}
