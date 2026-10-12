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
// opencode_plan_bridge — unit tests
// ---------------------------------------------------------------------------
//
// The fixture shapes mirror what the wire probe actually observed from
// `opencode acp` (opencode_acp_plan_probe_integration_test.go): the in_progress
// tool_call_update carries rawInput.todos; the terminal completed frame carries
// rawOutput.metadata.todos (plus a `content` text block, which the bridge
// ignores).

func ocAgent(backend string) *model.Agent {
	return &model.Agent{
		ID:        backend + "-bridge-test",
		Backend:   backend,
		Transport: "acp-stdio",
	}
}

// ocTodos builds the OpenCode todos array shape (content/priority/status).
func ocTodos(items ...map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out
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

// --- parseOpenCodeTodoSnapshot ---

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
	conn := newACPConn(ocAgent("opencode"), "session-oc-bridge-1")
	ch := make(chan StreamEvent, 8)

	tcu := ocCompletedTcu(nil, map[string]any{
		"metadata": map[string]any{
			"todos": ocTodos(
				map[string]any{"content": "step 1", "priority": "high", "status": "pending"},
			),
		},
	})

	n := bridgeOpenCodePlanFromToolUpdate(ch, conn, tcu)
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
	conn := newACPConn(ocAgent("opencode"), "session-oc-bridge-2")
	ch := make(chan StreamEvent, 8)

	st := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_todo"),
		Status:     &st,
		RawInput:   map[string]any{"todos": ocTodos(map[string]any{"content": "x", "status": "pending"})},
	}
	n := bridgeOpenCodePlanFromToolUpdate(ch, conn, tcu)
	assert.Zero(t, n, "in_progress frame must not bridge (avoids duplicate plan_update)")
	assert.Nil(t, conn.GetCachedPlanState())
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeOpenCodePlanFromToolUpdate_SkipsNoSnapshot(t *testing.T) {
	conn := newACPConn(ocAgent("opencode"), "session-oc-bridge-3")
	ch := make(chan StreamEvent, 8)

	// A completed tool frame that is not a todowrite (e.g. a write) — no todos.
	tcu := ocCompletedTcu(
		map[string]any{"filePath": "/tmp/a.txt", "content": "a"},
		map[string]any{"metadata": map[string]any{"filepath": "/tmp/a.txt"}},
	)
	n := bridgeOpenCodePlanFromToolUpdate(ch, conn, tcu)
	assert.Zero(t, n)
	assert.Nil(t, conn.GetCachedPlanState())
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeOpenCodePlanFromToolUpdate_EmptySnapshotClears(t *testing.T) {
	conn := newACPConn(ocAgent("opencode"), "session-oc-bridge-4")
	ch := make(chan StreamEvent, 8)

	tcu := ocCompletedTcu(nil, map[string]any{
		"metadata": map[string]any{"todos": []any{}},
	})
	n := bridgeOpenCodePlanFromToolUpdate(ch, conn, tcu)
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

func TestMapACPSessionUpdate_OpenCodeTodoWrite_BridgesPlanUpdate(t *testing.T) {
	conn := newACPConn(ocAgent("opencode"), "session-oc-map-1")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()

	st := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("call_map"),
			Status:     &st,
			Title:      ptrStr("3 todos"),
			RawOutput: map[string]any{
				"metadata": map[string]any{
					"todos": ocTodos(
						map[string]any{"content": "map-step-1", "priority": "high", "status": "completed"},
						map[string]any{"content": "map-step-2", "priority": "high", "status": "in_progress"},
					),
				},
			},
		},
	}

	// nil debouncer → the ToolCallUpdate branch falls through to the fallback
	// forward path, then our bridge call (placed before deb) runs too.
	mapACPSessionUpdate(update, ch, ctx, conn, nil)

	planEvents := 0
	toolEvents := 0
drainLoop:
	for {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "plan_update":
				planEvents++
				require.NotNil(t, ev.Plan)
				assert.Len(t, ev.Plan.Entries, 2)
				assert.Equal(t, "map-step-1", ev.Plan.Entries[0].Content)
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
	assert.Len(t, cached.Entries, 2)
}

// TestMapACPSessionUpdate_OpenCodeTodoWrite_WithDebouncer exercises the bridge
// through the REAL production path: production always passes a non-nil debouncer
// (RegisterSession creates one), and the bridge sits before the debouncer branch.
// The terminal frame flushes immediately in the debouncer, so the bridge must
// still emit exactly one plan_update alongside the tool event.
func TestMapACPSessionUpdate_OpenCodeTodoWrite_WithDebouncer(t *testing.T) {
	conn := newACPConn(ocAgent("opencode"), "session-oc-map-deb")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()
	deb := newToolCallDebouncer(ch, conn)

	st := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("call_deb"),
			Status:     &st,
			Title:      ptrStr("3 todos"),
			RawOutput: map[string]any{
				"metadata": map[string]any{
					"todos": ocTodos(
						map[string]any{"content": "deb-step-1", "priority": "high", "status": "completed"},
					),
				},
			},
		},
	}
	mapACPSessionUpdate(update, ch, ctx, conn, deb)

	planEvents := 0
drainDeb:
	for {
		select {
		case ev := <-ch:
			if ev.Type == "plan_update" {
				planEvents++
				require.NotNil(t, ev.Plan)
				assert.Len(t, ev.Plan.Entries, 1)
			}
		default:
			break drainDeb
		}
	}
	assert.Equal(t, 1, planEvents, "debouncer path must still bridge exactly one plan_update")
	require.NotNil(t, conn.GetCachedPlanState())
}

func TestMapACPSessionUpdate_MimoTodoWrite_BridgesPlanUpdate(t *testing.T) {
	// mimo is an OpenCode fork with the same todowrite tool surface.
	conn := newACPConn(ocAgent("mimo"), "session-oc-map-2")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()

	st := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("call_mimo"),
			Status:     &st,
			RawOutput: map[string]any{
				"metadata": map[string]any{
					"todos": ocTodos(map[string]any{"content": "mimo-step", "status": "pending"}),
				},
			},
		},
	}
	mapACPSessionUpdate(update, ch, ctx, conn, nil)

	planEvents := 0
drainLoop:
	for {
		select {
		case ev := <-ch:
			if ev.Type == "plan_update" {
				planEvents++
			}
		default:
			break drainLoop
		}
	}
	assert.Equal(t, 1, planEvents)
	require.NotNil(t, conn.GetCachedPlanState())
}

func TestMapACPSessionUpdate_NonOpenCodeBackend_NoTodoBridge(t *testing.T) {
	// A claude agent must not bridge (its real ACP Plan path stays authoritative).
	agent := &model.Agent{ID: "claude-test", Backend: "claude", Transport: "acp-stdio"}
	conn := newACPConn(agent, "session-oc-map-3")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()

	st := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("call_claude"),
			Status:     &st,
			RawOutput: map[string]any{
				"metadata": map[string]any{
					"todos": ocTodos(map[string]any{"content": "x", "status": "pending"}),
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
	assert.False(t, hasPlan, "non-opencode backend must not bridge todowrite to plan")
	assert.Nil(t, conn.GetCachedPlanState())
}
