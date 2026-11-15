package backends_test

import (
	"context"
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	_ "clawbench/internal/ai/backends/all"
	"clawbench/internal/ai/backends/codebuddy"
	"clawbench/internal/model"
)

func ocAgent(backend string) *model.Agent {
	return &model.Agent{
		ID:        backend + "-bridge-test",
		Backend:   backend,
		Transport: "acp-stdio",
	}
}

// ocTodos builds the OpenCode todos array shape (content/priority/status).

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

func ptrStr(s string) *string { return &s }

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

// CodeBuddy _meta keys used by the team-attribution dispatch tests.
const (
	metaKeyMemberEvent       = "codebuddy.ai/memberEvent"
	metaKeyParentToolCallID  = "codebuddy.ai/parentToolCallId"
	metaKeyTeamUpdateLocal   = "codebuddy.ai/teamUpdate"
	metaKeyMemberNameLocal   = "codebuddy.ai/memberName"
	metaKeySubagentTypeLocal = "codebuddy.ai/subagentType"
)

func TestMapACPSessionUpdate_MemberContentAttributed(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.SetMemberToolCallID("probe-alpha", "call_agent")

	ch := make(chan ai.StreamEvent, 16)
	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.TextBlock("hello from alpha"),
			Meta:    map[string]any{metaKeyMemberEvent: "probe-alpha"},
		},
	}
	ai.MapACPSessionUpdateWithConnForTest(update, ch, t.Context(), conn, nil)

	var content *ai.StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "content" {
			e := ev
			content = &e
		}
	}
	require.NotNil(t, content, "expected a content event")
	assert.Equal(t, "hello from alpha", content.Content)
	assert.Equal(t, "probe-alpha", content.MemberName)
	assert.Equal(t, "call_agent", content.ParentToolCallID)
}

// A team member's tool call must be attributed too.

func TestMapACPSessionUpdate_MemberToolCallAttributed(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.SetMemberToolCallID("probe-beta", "call_beta_agent")

	ch := make(chan ai.StreamEvent, 16)
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "call_tool",
		Title:      "Bash",
		Meta: map[string]any{
			metaKeyMemberEvent:      "probe-beta",
			"codebuddy.ai/toolName": "Bash",
		},
	}
	ai.MapACPSessionUpdateWithConnForTest(acp.SessionUpdate{ToolCall: &tc}, ch, t.Context(), conn, nil)

	var tool *ai.StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "tool_use" {
			e := ev
			tool = &e
		}
	}
	require.NotNil(t, tool, "expected a tool_use event")
	assert.Equal(t, "probe-beta", tool.MemberName)
	require.NotNil(t, tool.Tool)
	assert.Equal(t, "call_beta_agent", tool.Tool.ParentToolCallID)
}

// The session_info_update branch must route a teamUpdate meta into a
// team_update event through the full mapper.

func TestMapACPSessionUpdate_TeamUpdateRouted(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan ai.StreamEvent, 16)

	update := teamInfoUpdate(map[string]any{
		metaKeyTeamUpdateLocal: mustMeta(t, teamUpdateMemberStatus),
	})
	ai.MapACPSessionUpdateWithConnForTest(update, ch, t.Context(), conn, nil)

	var team *ai.StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "team_update" {
			e := ev
			team = &e
		}
	}
	require.NotNil(t, team, "expected a team_update event")
	require.NotNil(t, team.Team)
	assert.Equal(t, "clawbench-probe", team.Team.TeamName)
	assert.Len(t, team.Team.Members, 2)
}

// A non-team session_info_update must produce no events (regression guard for
// the previously-dead branch).

func TestMapACPSessionUpdate_SessionInfoNoTeamNoEvent(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan ai.StreamEvent, 16)

	update := teamInfoUpdate(map[string]any{"codebuddy.ai/agentPhase": "running"})
	ai.MapACPSessionUpdateWithConnForTest(update, ch, t.Context(), conn, nil)
	assert.Empty(t, ch, "plain session_info_update must not emit events")
}

// A non-CodeBuddy backend must not run the team bridge.

func TestMapACPSessionUpdate_TeamUpdateIgnoredForOtherBackends(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	ch := make(chan ai.StreamEvent, 16)

	update := teamInfoUpdate(map[string]any{
		metaKeyTeamUpdateLocal: mustMeta(t, teamUpdateCreated),
	})
	ai.MapACPSessionUpdateWithConnForTest(update, ch, t.Context(), conn, nil)
	assert.Empty(t, ch, "team bridge is CodeBuddy-only")
}

// ---------------------------------------------------------------------------
// Permission attribution
// ---------------------------------------------------------------------------

func TestMapACPSessionUpdate_SpawnFrameGetsMemberColor(t *testing.T) {
	conn := ai.NewACPConnForTest(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	codebuddy.BridgeTeamUpdate(conn, make(chan ai.StreamEvent, 4), map[string]any{
		metaKeyTeamUpdateLocal: mustMeta(t, teamUpdateMemberStatus),
	})

	ch := make(chan ai.StreamEvent, 16)
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "call_agent",
		Title:      "Agent",
		Meta: map[string]any{
			metaKeyMemberNameLocal:   "probe-alpha",
			metaKeySubagentTypeLocal: "general-purpose",
			"codebuddy.ai/toolName":  "Agent",
		},
	}
	ai.MapACPSessionUpdateWithConnForTest(acp.SessionUpdate{ToolCall: &tc}, ch, t.Context(), conn, nil)

	var tool *ai.StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "tool_use" {
			e := ev
			tool = &e
		}
	}
	require.NotNil(t, tool)
	assert.Equal(t, "probe-alpha", tool.MemberName)
	assert.Equal(t, "blue", tool.MemberColor)
}

func TestMapACPSessionUpdate_CodebuddyTaskCreate_BridgesPlanUpdate(t *testing.T) {
	conn := ai.NewACPConnForTest(cbCodebuddyAgent(), "session-map-1")
	ch := make(chan ai.StreamEvent, 16)
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
	ai.MapACPSessionUpdateWithConnForTest(update, ch, ctx, conn, nil)

	// tool_use/tool_result + plan_update. Gather events, look for plan_update.
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
	conn := ai.NewACPConnForTest(agent, "session-map-2")
	ch := make(chan ai.StreamEvent, 16)
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
	ai.MapACPSessionUpdateWithConnForTest(update, ch, ctx, conn, nil)

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

func TestMapACPSessionUpdate_OpenCodeTodoWrite_BridgesPlanUpdate(t *testing.T) {
	conn := ai.NewACPConnForTest(ocAgent("opencode"), "session-oc-map-1")
	ch := make(chan ai.StreamEvent, 16)
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
	ai.MapACPSessionUpdateWithConnForTest(update, ch, ctx, conn, nil)

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
	conn := ai.NewACPConnForTest(ocAgent("opencode"), "session-oc-map-deb")
	ch := make(chan ai.StreamEvent, 16)
	ctx := context.Background()
	deb := ai.NewToolCallDebouncerForTest(ch, conn)

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
	ai.MapACPSessionUpdateWithConnForTest(update, ch, ctx, conn, deb)

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
	conn := ai.NewACPConnForTest(ocAgent("mimo"), "session-oc-map-2")
	ch := make(chan ai.StreamEvent, 16)
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
	ai.MapACPSessionUpdateWithConnForTest(update, ch, ctx, conn, nil)

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
	conn := ai.NewACPConnForTest(agent, "session-oc-map-3")
	ch := make(chan ai.StreamEvent, 16)
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
	ai.MapACPSessionUpdateWithConnForTest(update, ch, ctx, conn, nil)

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

func teamInfoUpdate(meta map[string]any) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			SessionUpdate: "session_info_update",
			Meta:          meta,
		},
	}
}

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

func ocTodos(items ...map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out
}

func TestParseACPToolCall_RoutesPiToPiParser(t *testing.T) {
	tc := ai.ParseACPToolCallForTest("pi", acp.SessionUpdateToolCall{
		ToolCallId: "call_dispatch_1",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	})
	m := mustParsePiACPInput(t, tc.Input)
	if m["old_string"] != "a" || m["new_string"] != "b" {
		t.Errorf("dispatch did not reach the Pi parser (nested edits not flattened): %v", m)
	}
	if m["file_path"] != "f.txt" {
		t.Errorf("path not remapped: %v", m)
	}
}

func TestParseACPToolCall_NonPiBackendStaysGeneric(t *testing.T) {
	tc := ai.ParseACPToolCallForTest("grok", acp.SessionUpdateToolCall{
		ToolCallId: "call_dispatch_2",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	})
	if tc.Input == "" {
		t.Fatal("generic parser produced no input")
	}
	m := mustParsePiACPInput(t, tc.Input)
	// The generic flat remap does not recurse, so the flattened pair is absent.
	if _, flattened := m["old_string"]; flattened {
		t.Errorf("generic backend unexpectedly got Pi's nested-edits flattening: %v", m)
	}
}

func TestParseACPToolCallUpdate_RoutesPiToPiParser(t *testing.T) {
	title := "edit"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_dispatch_3",
		Title:      &title,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	}
	tc := ai.ParseACPToolCallUpdateForTest("pi", tcu)
	m := mustParsePiACPInput(t, tc.Input)
	if m["old_string"] != "a" || m["new_string"] != "b" {
		t.Errorf("update dispatch did not reach the Pi parser: %v", m)
	}
}

// TestParseACPToolCallUpdate_NonPiBackendStaysGeneric is the control for the
// update-path dispatch.

func TestParseACPToolCallUpdate_NonPiBackendStaysGeneric(t *testing.T) {
	title := "edit"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_dispatch_4",
		Title:      &title,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	}
	tc := ai.ParseACPToolCallUpdateForTest("grok", tcu)
	m := mustParsePiACPInput(t, tc.Input)
	if _, flattened := m["old_string"]; flattened {
		t.Errorf("generic update backend unexpectedly got Pi's flattening: %v", m)
	}
}

// ── normalizePiACPInput / parsePiACPToolCallUpdate branches ──────────────────

func mustParsePiACPInput(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("input is not valid JSON: %v (%s)", err, raw)
	}
	return m
}
