package codebuddy

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/ai/acptest"
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

func codebuddyTcuForBridge(name, status string, rawResp map[string]any) acp.SessionToolCallUpdate {
	st := acp.ToolCallStatus(status)
	return acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_bridge"),
		Status:     acptest.Patch(st),
		Meta: acptest.PatchMeta(map[string]any{
			"codebuddy.ai/toolName":    name,
			"codebuddy.ai/rawResponse": rawResp,
		}),
	}
}

func cbCodebuddyAgent() *model.Agent {
	return &model.Agent{
		ID:        "codebuddy-bridge-test",
		Backend:   "codebuddy",
		Transport: "acp-stdio",
	}
}

func cbTaskMeta(rawResp map[string]any) map[string]any {
	return map[string]any{
		"codebuddy.ai/rawResponse": rawResp,
	}
}

func TestParseTeamState_MemberStatusChange(t *testing.T) {
	raw := mustMeta(t, teamUpdateMemberStatus)
	state := parseTeamState(raw)
	require.NotNil(t, state)

	assert.Equal(t, "member_status_change", state.Type)
	assert.Equal(t, "clawbench-probe", state.TeamName)
	assert.False(t, state.IsAutoTeam)
	require.NotNil(t, state.HasLive)
	assert.True(t, *state.HasLive)
	require.Len(t, state.Members, 2)

	a := state.Members[0]
	assert.Equal(t, "probe-alpha", a.Name)
	assert.Equal(t, "general-purpose", a.AgentType)
	assert.Equal(t, "blue", a.Color)
	assert.Equal(t, "running", a.Status)
	assert.Equal(t, "working", a.Activity)
	assert.Equal(t, "alive", a.Lifecycle)
	assert.Equal(t, "agent-08d3", a.TaskID)
	assert.Equal(t, "sess-a", a.SessionID)
	assert.Equal(t, 2, a.ToolCallCount)
	require.NotNil(t, a.TokenUsage)
	assert.Equal(t, 56763, a.TokenUsage.InputTokens)
	assert.Equal(t, 229, a.TokenUsage.OutputTokens)
	assert.Equal(t, 28430, a.TokenUsage.LastContextWindow)

	b := state.Members[1]
	assert.Equal(t, "completed", b.Status)
	assert.Equal(t, "idle", b.Activity)
}

func TestParseTeamState_CreatedMinimal(t *testing.T) {
	state := parseTeamState(mustMeta(t, teamUpdateCreated))
	require.NotNil(t, state)
	assert.Equal(t, "team_created", state.Type)
	assert.Empty(t, state.Members)
	// An explicit false must survive as a non-nil pointer (the panel treats it
	// as "team ended").
	require.NotNil(t, state.HasLive)
	assert.False(t, *state.HasLive)
}

// A missing hasLiveMembers must stay absent (nil) rather than collapsing to an
// explicit false — otherwise the panel ends the team prematurely. Mirrors
// TestParseTeamState_MissingIsAutoTeam.

func TestParseTeamState_MissingHasLiveMembers(t *testing.T) {
	state := parseTeamState(map[string]any{"type": "member_status_change", "teamName": "t"})
	require.NotNil(t, state)
	assert.Nil(t, state.HasLive)
}

// A missing isAutoTeam (observed on the first member_status_change snapshot)
// must not be mistaken for an explicit false — only an explicit true is trusted.

func TestParseTeamState_MissingIsAutoTeam(t *testing.T) {
	state := parseTeamState(map[string]any{"type": "member_status_change", "teamName": "t"})
	require.NotNil(t, state)
	assert.False(t, state.IsAutoTeam)
}

func TestParseTeamState_ExplicitAutoTeam(t *testing.T) {
	state := parseTeamState(map[string]any{"type": "team_created", "teamName": "_auto_x", "isAutoTeam": true})
	require.NotNil(t, state)
	assert.True(t, state.IsAutoTeam)
}

// Fail closed on malformed shapes so a bad extension never wipes a live panel.

func TestParseTeamState_FailsClosed(t *testing.T) {
	assert.Nil(t, parseTeamState(nil))
	assert.Nil(t, parseTeamState(map[string]any{}))
	assert.Nil(t, parseTeamState(map[string]any{"teamName": "no-type"}))
}

// Members without a name are skipped (the name is the join key).

func TestParseTeamState_SkipsNamelessMembers(t *testing.T) {
	state := parseTeamState(map[string]any{
		"type":     "member_status_change",
		"teamName": "t",
		"members":  []any{map[string]any{"status": "running"}, map[string]any{"name": "ok"}},
	})
	require.NotNil(t, state)
	require.Len(t, state.Members, 1)
	assert.Equal(t, "ok", state.Members[0].Name)
}

// ---------------------------------------------------------------------------
// bridgeCodeBuddyTeamUpdate
// ---------------------------------------------------------------------------

func TestBridgeTeamUpdate_EmitsAndCaches(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan ai.StreamEvent, 4)

	meta := map[string]any{metaKeyTeamUpdate: mustMeta(t, teamUpdateMemberStatus)}
	BridgeTeamUpdate(conn, ch, meta)

	select {
	case ev := <-ch:
		assert.Equal(t, "team_update", ev.Type)
		require.NotNil(t, ev.Team)
		assert.Equal(t, "member_status_change", ev.Team.Type)
		assert.Len(t, ev.Team.Members, 2)
	default:
		t.Fatal("expected a team_update event")
	}
	cached := conn.GetCachedTeamState()
	require.NotNil(t, cached)
	assert.Equal(t, "clawbench-probe", cached.TeamName)
}

// A plain session_info_update (title/updatedAt, no teamUpdate key) must not
// produce an event — this is the non-team regression guard.

func TestBridgeTeamUpdate_NoopWithoutKey(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan ai.StreamEvent, 4)

	BridgeTeamUpdate(conn, ch, map[string]any{"codebuddy.ai/agentPhase": "running"})
	assert.Empty(t, ch, "no teamUpdate key must not emit")
	assert.Nil(t, conn.GetCachedTeamState())
}

func TestBridgeTeamUpdate_DeleteClearsCache(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan ai.StreamEvent, 4)

	// Seed a live team first.
	BridgeTeamUpdate(conn, ch, map[string]any{
		metaKeyTeamUpdate: mustMeta(t, teamUpdateMemberStatus),
	})
	require.NotNil(t, conn.GetCachedTeamState())

	BridgeTeamUpdate(conn, ch, map[string]any{
		metaKeyTeamUpdate: map[string]any{"type": "team_deleted", "teamName": "clawbench-probe"},
	})
	assert.Nil(t, conn.GetCachedTeamState(), "team_deleted must clear the cache")
}

// ---------------------------------------------------------------------------
// Member-name ↔ tool-call join
// ---------------------------------------------------------------------------

func TestMemberSpawnJoin_RecordsAndResolves(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")

	spawnMeta := map[string]any{
		metaKeyMemberName:   "probe-alpha",
		metaKeySubagentType: "general-purpose",
	}
	NoteMemberSpawn(conn, spawnMeta, "call_00_gQeF1pbxpR25Ip7O4kwj0521")
	assert.Equal(t, "call_00_gQeF1pbxpR25Ip7O4kwj0521", conn.MemberToolCallID("probe-alpha"))

	// A member content frame carries only memberEvent; the join resolves the
	// parent tool-call id for grouping.
	contentMeta := map[string]any{metaKeyMemberEvent: "probe-alpha"}
	assert.Equal(t, "call_00_gQeF1pbxpR25Ip7O4kwj0521", ai.ResolveMemberParent(conn, contentMeta))
}

// memberName WITHOUT subagentType is not a spawn frame (some non-spawn frames
// carry memberName), so it must not pollute the join map.

func TestMemberSpawnJoin_RequiresSubagentType(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	NoteMemberSpawn(conn, map[string]any{metaKeyMemberName: "x"}, "call_1")
	assert.Equal(t, "", conn.MemberToolCallID("x"))
}

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

func TestBridgeCodeBuddyPlanFromToolUpdate_EmitsAndCaches(t *testing.T) {
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-bridge-1")
	ch := make(chan ai.StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskCreate", "completed", map[string]any{
		"task":  map[string]any{"id": "1", "status": "pending", "subject": "probe"},
		"todos": []any{map[string]any{"id": "1", "content": "probe", "status": "pending"}},
	})

	n := BridgePlanFromToolUpdate(conn, ch, tcu)
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
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-bridge-2")
	ch := make(chan ai.StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskUpdate", "completed", map[string]any{
		"task":  map[string]any{"id": "1", "status": "in_progress", "subject": "probe"},
		"todos": []any{map[string]any{"id": "1", "content": "probe", "status": "in_progress"}},
	})

	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	require.Equal(t, 1, n)
	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	assert.Equal(t, "in_progress", events[0].Plan.Entries[0].Status)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_DeleteLastEmitsEmptyPlan(t *testing.T) {
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-bridge-3")
	ch := make(chan ai.StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskUpdate", "completed", map[string]any{
		"taskId":  "1",
		"deleted": true,
		"todos":   []any{},
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

func TestBridgeCodeBuddyPlanFromToolUpdate_SkipsNonBridgedTool(t *testing.T) {
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-bridge-4")
	ch := make(chan ai.StreamEvent, 8)

	// TaskGet returns a single task — not a full snapshot — so it must NOT bridge.
	tcu := codebuddyTcuForBridge("TaskGet", "completed", map[string]any{
		"task": map[string]any{"id": "1", "status": "pending", "subject": "solo"},
	})
	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	assert.Zero(t, n)
	assert.Nil(t, conn.GetCachedPlanState(), "TaskGet must not touch the plan cache")
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_SkipsNonCompleted(t *testing.T) {
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-bridge-5")
	ch := make(chan ai.StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskCreate", "in_progress", map[string]any{
		"task": map[string]any{"id": "1"}, "todos": []any{map[string]any{"id": "1", "content": "p"}},
	})
	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	assert.Zero(t, n)
	assertNoMoreACPEvents(ch, t)
}

func TestBridgeCodeBuddyPlanFromToolUpdate_NoRawResponse(t *testing.T) {
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-bridge-6")
	ch := make(chan ai.StreamEvent, 8)

	tcu := codebuddyTcuForBridge("TaskCreate", "completed", nil)
	n := BridgePlanFromToolUpdate(conn, ch, tcu)
	assert.Zero(t, n)
	assert.Nil(t, conn.GetCachedPlanState())
	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate end-to-end bridge ---

// mapACPSessionUpdate emits the bridge plan_update alongside tool events.
// Drain them and check for plan_update.

func mustMeta(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	return m
}

// teamUpdateMemberStatus is a member_status_change snapshot with two members,
// one running and one completed+idle.
const teamUpdateMemberStatus = `{
  "type": "member_status_change",
  "teamName": "clawbench-probe",
  "isAutoTeam": false,
  "hasLiveMembers": true,
  "members": [
    {"name":"probe-alpha","agentType":"general-purpose","color":"blue",
     "description":"alpha task","status":"running","activity":"working",
     "lifecycle":"alive","taskId":"agent-08d3","sessionId":"sess-a",
     "tokenUsage":{"inputTokens":56763,"outputTokens":229,"lastContextWindow":28430},
     "toolCallCount":2},
    {"name":"probe-beta","agentType":"general-purpose","color":"green",
     "description":"beta task","status":"completed","activity":"idle",
     "lifecycle":"alive","taskId":"agent-ced1","sessionId":"sess-b",
     "tokenUsage":{"inputTokens":85190,"outputTokens":205,"lastContextWindow":28520},
     "toolCallCount":2}
  ]
}`

// teamUpdateCreated is the minimal creation event.
const teamUpdateCreated = `{"type":"team_created","teamName":"clawbench-probe","hasLiveMembers":false}`
